// Package update says which build of git-tui is running and whether a newer stable release exists.
// It never installs anything: it only reports, and suggests the commands to run by hand.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"time"
)

const (
	Module = "github.com/shaungsimpson/git-tui"
	Repo   = "shaungsimpson/git-tui" // the GitHub repository whose releases are checked
	MaxAge = 24 * time.Hour          // how long a successful check is trusted, across launches
)

// Kind is how a build was made, as far as its metadata shows.
type Kind int

const (
	Unknown Kind = iota // no metadata says how it was made
	Release             // a stable version downloaded as a module, such as go install …@v0.5.0
	Source              // built from a checkout, even one at a release tag
)

// Build is the running binary's identity, from the metadata Go stamps into it.
type Build struct {
	Kind                           Kind
	Module, Version, Sum, Revision string
	Dirty, Replaced                bool
}

// Current reads the running binary's build metadata.
func Current() Build {
	info, _ := debug.ReadBuildInfo()
	return Read(info)
}

// Read classifies build metadata conservatively: only a checksummed, unreplaced module download at a stable
// version is a release, and any sign of a checkout makes it a source build. Everything else is unknown.
func Read(info *debug.BuildInfo) Build {
	if info == nil {
		return Build{}
	}
	b := Build{Module: info.Main.Path, Version: info.Main.Version, Sum: info.Main.Sum, Replaced: info.Main.Replace != nil}
	vcs := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Revision, vcs = s.Value, true
		case "vcs.modified":
			b.Dirty = s.Value == "true"
		}
	}
	switch {
	case vcs || b.Version == "(devel)": // module downloads carry no VCS settings; checkouts do
		b.Kind = Source
	case b.Module == Module && b.Sum != "" && !b.Replaced && Stable(b.Version):
		b.Kind = Release
	}
	return b
}

var stableRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// parse reads vMAJOR.MINOR.PATCH; prereleases, build metadata and pseudo-versions are not stable.
func parse(v string) ([3]int, bool) {
	var n [3]int
	m := stableRe.FindStringSubmatch(v)
	if m == nil {
		return n, false
	}
	for i := range n {
		var err error
		if n[i], err = strconv.Atoi(m[i+1]); err != nil { // too large to be a real version
			return n, false
		}
	}
	return n, true
}

// Stable reports whether v is a stable release version such as v0.5.0.
func Stable(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer reports whether latest is a later stable version than current; false unless both are stable.
func Newer(current, latest string) bool {
	c, ok := parse(current)
	l, ok2 := parse(latest)
	return ok && ok2 && slices.Compare(l[:], c[:]) > 0
}

// Latest is the newest stable release, as a check found it.
type Latest struct {
	Version string    `json:"version"`
	Checked time.Time `json:"checked"`
}

// Releases asks GitHub for the newest release; gitx.Git and gitx.Demo provide it.
type Releases interface {
	LatestRelease(ctx context.Context) (Latest, error)
}

// Check asks for the latest stable release now, and remembers a successful answer in the cache at path
// (none when path is empty).
func Check(ctx context.Context, src Releases, path string, now time.Time) (Latest, error) {
	l, err := src.LatestRelease(ctx)
	if err != nil {
		return Latest{}, err
	}
	if !Stable(l.Version) {
		return Latest{}, fmt.Errorf("latest release %q is not a stable version", l.Version)
	}
	l.Checked = now
	if path != "" {
		_ = saveCache(path, l) // a cache that cannot be written only costs another check next launch
	}
	return l, nil
}

// State is what is known about updates for a build.
type State int

const (
	NotChecked State = iota
	Checking
	UpToDate
	Available
	Unavailable   // the check failed and nothing earlier is known
	NotComparable // not a stable release build, so never called out of date
)

var stateNames = [...]string{"not checked", "checking", "up to date", "update available", "check failed", "not compared"}

func (s State) String() string { return stateNames[s] }

// Assess combines a build with what the checks found. A failed check keeps an earlier result.
func Assess(b Build, l Latest, checking bool, err error) State {
	switch {
	case checking && l.Version == "": // any build, so an explicit check on a source build shows it is running
		return Checking
	case b.Kind != Release:
		return NotComparable
	case l.Version != "" && Newer(b.Version, l.Version):
		return Available
	case l.Version != "":
		return UpToDate
	case err != nil:
		return Unavailable
	}
	return NotChecked
}

// CachePath is $XDG_CACHE_HOME/git-tui/release.json, which defaults to ~/.cache/git-tui/release.json.
func CachePath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			return "" // no cache; never the current folder, which may be a repository
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "git-tui", "release.json")
}

// LoadCache reads the last successful check; anything missing, unreadable or implausible is no result.
func LoadCache(path string, now time.Time) Latest {
	var l Latest
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &l) != nil || !Stable(l.Version) || l.Checked.IsZero() || l.Checked.After(now) {
		return Latest{} // a time in the future would never go stale
	}
	return l
}

// saveCache writes straight to the file: a torn write fails LoadCache's checks and is only a cache miss.
func saveCache(path string, l Latest) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
