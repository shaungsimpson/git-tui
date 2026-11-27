// Package config loads and saves the explicit repository list for git-tui.
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Status describes whether a configured repository can be inspected.
type Status int

const (
	StatusOK Status = iota
	StatusMissing
	StatusNotGit
	StatusInvalid
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusMissing:
		return "missing"
	case StatusNotGit:
		return "not a git repository"
	default:
		return "invalid"
	}
}

// Repo is one configured repository after validation.
type Repo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Group  string `json:"group,omitempty"` // repositories with the same group can be shown on their own
	Status Status `json:"-"`
	Err    string `json:"-"`
}

// Config is the parsed configuration file.
type Config struct {
	Repositories []Repo `json:"repositories"`
}

// ErrNotFound means no configuration file exists at the resolved path.
var ErrNotFound = errors.New("configuration file not found")

// Resolve picks the config path: explicit flag, then $GIT_TUI_CONFIG, then the user config dir.
func Resolve(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("GIT_TUI_CONFIG"); env != "" {
		return env
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			return "" // Load refuses it, so nothing is ever saved into the current folder
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "git-tui", "config.json")
}

// Load reads and validates the file. Per-repository problems are recorded on each Repo, not returned.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("no configuration file: set HOME or GIT_TUI_CONFIG, or pass -config")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err := decode(data)
	if err != nil { // nothing half-read reaches the app: its repositories were never validated
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	written := map[string]bool{} // a name taken from the folder yields to one the file gives, wherever it is
	for _, r := range cfg.Repositories {
		written[r.Name] = r.Name != ""
	}
	taken := map[string]bool{}
	for i := range cfg.Repositories {
		r := &cfg.Repositories[i]
		r.Group = strings.TrimSpace(r.Group) // as saving does, so "work " written by hand is not a second work
		named := r.Name != ""
		// A relative path is read from the file's own folder, not wherever git-tui was started.
		if r.Path = expand(strings.TrimSpace(r.Path)); r.Path != "" && !filepath.IsAbs(r.Path) {
			r.Path = filepath.Join(filepath.Dir(path), r.Path)
		}
		validate(r)
		// Two repositories called api, named or not: as when adding, the later one gets parent folders until it
		// is unique, since the app finds a repository by its name.
		for dir := filepath.Dir(r.Path); taken[r.Name] || !named && written[r.Name]; dir = filepath.Dir(dir) {
			if dir == filepath.Dir(dir) { // out of parents: the path itself
				r.Name = r.Path
				break
			}
			r.Name = filepath.Base(dir) + "/" + r.Name
		}
		taken[r.Name] = true
	}
	return cfg, nil
}

func decode(data []byte) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // a misspelt key such as "repos" should say so, not load nothing
	if err := dec.Decode(&cfg); err != nil {
		return cfg, err
	}
	if _, err := dec.Token(); err != io.EOF { // a decoder stops after one value; anything after it is a mistake
		return cfg, errors.New("unexpected content after the configuration")
	}
	return cfg, nil
}

// Save writes the repository list, keeping the previous file as path.bak. Each file is written beside the old
// one, flushed to disk and read back before it replaces anything (writeFile).
func Save(path string, c Config) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // write through a link, such as one into a dotfiles repository, rather than replace it
	} else {
		// A link to a file not made yet, such as dotfiles not checked out: follow the chain to its end. Targets are
		// joined as text, never cleaned, so the system resolves a ".." after a linked folder through that folder.
		for range 40 {
			target, err := os.Readlink(path)
			if err != nil {
				break
			}
			if !filepath.IsAbs(target) {
				dir, _ := filepath.Split(path) // unlike filepath.Dir, keeps ".." and knows both Windows separators
				target = dir + target
			}
			path = target
		}
		if _, err := os.Readlink(path); err == nil {
			return fmt.Errorf("too many links from %s", path)
		}
	}
	out := Config{Repositories: []Repo{}} // an empty list is written as [], not null
	seen := map[string]bool{}
	for _, r := range c.Repositories {
		if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Path) == "" {
			return errors.New("every repository needs a name and a path")
		}
		if seen[r.Name] {
			return fmt.Errorf("two repositories are called %s", r.Name)
		}
		seen[r.Name] = true
		out.Repositories = append(out.Repositories, Repo{Name: r.Name, Path: Tilde(r.Path), Group: strings.TrimSpace(r.Group)})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	old, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := writeFile(path+".bak", old); err != nil {
			return err
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	return writeFile(path, data)
}

// Validate checks one repository the way Load does.
func Validate(r Repo) Repo {
	validate(&r)
	return r
}

// maxScan bounds a scan of a large tree.
// shortcut: stops at 500 repositories, raise it if someone keeps more under one folder.
const maxScan = 500

// Scan lists the git repositories under root, at most depth folders down. It does not look inside a repository
// it found, nor inside hidden, node_modules or vendor folders.
func Scan(ctx context.Context, root string, depth int) ([]string, error) {
	root, err := filepath.Abs(expand(strings.TrimSpace(root))) // a relative folder would save paths that only work from here
	if err != nil {
		return nil, err
	}
	walk := root
	if real, err := filepath.EvalSymlinks(root); err == nil {
		walk = real // WalkDir does not follow a linked root; results still name the path as typed
	}
	var found []string
	err = filepath.WalkDir(walk, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == walk {
				return err
			}
			return nil // an unreadable folder is skipped, not fatal
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, _ := filepath.Rel(walk, p)
		if d.Type()&fs.ModeSymlink != 0 { // a project linked in from elsewhere: list it, never walk into it
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil && len(found) < maxScan {
				found = append(found, filepath.Join(root, rel))
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if rel != "." {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return fs.SkipDir
			}
		}
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			found = append(found, filepath.Join(root, rel))
			if len(found) >= maxScan {
				return fs.SkipAll
			}
			return fs.SkipDir
		}
		if rel != "." && strings.Count(rel, string(filepath.Separator)) >= depth-1 {
			return fs.SkipDir
		}
		return nil
	})
	return found, err
}

// Tilde writes a path under the home folder as ~/..., the way people write it by hand.
func Tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return p
}

func validate(r *Repo) {
	r.Path = expand(strings.TrimSpace(r.Path))
	if r.Path != "" {
		r.Path = filepath.Clean(r.Path) // "~/Code/api/" and a found ~/Code/api are the same folder
	}
	if r.Name == "" {
		r.Name = filepath.Base(r.Path)
	}
	if r.Path == "" {
		r.Status, r.Err = StatusInvalid, "path is empty"
		return
	}
	info, err := os.Stat(r.Path)
	if err != nil || !info.IsDir() {
		r.Status, r.Err = StatusMissing, "directory not found: "+r.Path
		return
	}
	// .git is a directory in normal clones and a file in worktrees/submodules.
	if _, err := os.Stat(filepath.Join(r.Path, ".git")); err != nil {
		r.Status, r.Err = StatusNotGit, "no .git in "+r.Path
		return
	}
	r.Status = StatusOK
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
