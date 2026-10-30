// Package config loads the explicit repository list for git-tui.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Status Status `json:"-"`
	Err    string `json:"-"`
}

// Config is the parsed configuration file.
type Config struct {
	Source       string `json:"-"`
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
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "git-tui", "config.json")
}

// Load reads and validates the file. Per-repository problems are recorded on each Repo, not returned.
func Load(path string) (Config, error) {
	cfg := Config{Source: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, ErrNotFound
	}
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // a misspelt key such as "repos" should say so, not load nothing
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF { // a decoder stops after one value; anything after it is a mistake
		return cfg, fmt.Errorf("parse %s: unexpected content after the configuration", path)
	}
	cfg.Source = path
	for i := range cfg.Repositories {
		validate(&cfg.Repositories[i])
	}
	return cfg, nil
}

func validate(r *Repo) {
	r.Path = expand(strings.TrimSpace(r.Path))
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
