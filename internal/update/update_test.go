package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"
)

func info(version, sum string, settings ...string) *debug.BuildInfo {
	i := &debug.BuildInfo{Path: Module + "/cmd/git-tui", Main: debug.Module{Path: Module, Version: version, Sum: sum}}
	for j := 0; j < len(settings); j += 2 {
		i.Settings = append(i.Settings, debug.BuildSetting{Key: settings[j], Value: settings[j+1]})
	}
	return i
}

func TestReadClassifiesConservatively(t *testing.T) {
	replaced := info("v0.5.0", "h1:x")
	replaced.Main.Replace = &debug.Module{Path: "../fork"}
	other := info("v0.5.0", "h1:x")
	other.Main.Path = "github.com/someone/fork"
	for _, c := range []struct {
		name string
		info *debug.BuildInfo
		want Kind
	}{
		{"no metadata", nil, Unknown},
		{"go install at a release", info("v0.5.0", "h1:x"), Release},
		{"checkout at a release tag", info("v0.5.0", "", "vcs.revision", "abc", "vcs.modified", "false"), Source},
		{"checkout with changes", info("v0.5.1-0.20261101041235-7f4b020574b1+dirty", "", "vcs.revision", "abc", "vcs.modified", "true"), Source},
		{"devel without VCS", info("(devel)", ""), Source},
		{"go install of a commit", info("v0.5.1-0.20261101041235-7f4b020574b1", "h1:x"), Unknown},
		{"go install of a prerelease", info("v0.6.0-rc.1", "h1:x"), Unknown},
		{"no checksum", info("v0.5.0", ""), Unknown},
		{"replaced", replaced, Unknown},
		{"another module", other, Unknown},
	} {
		if got := Read(c.info); got.Kind != c.want {
			t.Errorf("%s: kind %d, want %d", c.name, got.Kind, c.want)
		}
	}
	if b := Read(info("v0.5.0", "", "vcs.revision", "abc", "vcs.modified", "true")); b.Revision != "abc" || !b.Dirty {
		t.Fatalf("VCS settings not read: %+v", b)
	}
}

func TestNewerComparesStableVersionsOnly(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"v0.4.1", "v0.5.0", true},
		{"v0.9.0", "v0.10.0", true}, // numerically, not as text
		{"v1.0.0", "v0.99.99", false},
		{"v0.5.0", "v0.5.0", false},
		{"v0.5.0", "v0.6.0-rc.1", false},
		{"v0.5.0", "v0.6.0+meta", false},
		{"v0.5.0", "0.6.0", false},
		{"v0.5.0", "v0.06.0", false},
		{"v0.5.0", "v0.6", false},
		{"v0.5.0", "v99999999999999999999.0.0", false},
		{"(devel)", "v0.6.0", false},
	} {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.current, c.latest, got)
		}
	}
}

func TestAssess(t *testing.T) {
	rel := Build{Kind: Release, Version: "v0.4.1"}
	failed := errors.New("offline")
	for _, c := range []struct {
		b        Build
		l        Latest
		checking bool
		err      error
		want     State
	}{
		{rel, Latest{}, false, nil, NotChecked},
		{rel, Latest{}, true, nil, Checking},
		{rel, Latest{}, false, failed, Unavailable},
		{rel, Latest{Version: "v0.5.0"}, false, failed, Available}, // a failed refresh keeps the earlier result
		{rel, Latest{Version: "v0.4.1"}, true, nil, UpToDate},
		{Build{Kind: Source, Version: "v0.4.1"}, Latest{Version: "v0.5.0"}, false, nil, NotComparable},
		{Build{}, Latest{Version: "v0.5.0"}, false, nil, NotComparable},
		{Build{Kind: Source}, Latest{}, true, nil, Checking}, // an explicit check on a source build
	} {
		if got := Assess(c.b, c.l, c.checking, c.err); got != c.want {
			t.Errorf("Assess(%+v, %+v, %v, %v) = %s, want %s", c.b, c.l, c.checking, c.err, got, c.want)
		}
	}
}

type fakeReleases struct {
	l   Latest
	err error
}

func (f fakeReleases) LatestRelease(context.Context) (Latest, error) { return f.l, f.err }

func TestCheckCachesOnlySuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git-tui", "release.json")
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	if _, err := Check(context.Background(), fakeReleases{err: errors.New("HTTP 401")}, path, now); err == nil {
		t.Fatal("a failed request is an error")
	}
	if _, err := Check(context.Background(), fakeReleases{l: Latest{Version: "v1.0.0-beta"}}, path, now); err == nil {
		t.Fatal("a version that is not stable is an error")
	}
	if l := LoadCache(path, now); l.Version != "" {
		t.Fatalf("failures must not be cached: %+v", l)
	}
	if _, err := Check(context.Background(), fakeReleases{l: Latest{Version: "v0.5.0"}}, path, now); err != nil {
		t.Fatal(err)
	}
	if l := LoadCache(path, now.Add(time.Hour)); l.Version != "v0.5.0" || !l.Checked.Equal(now) {
		t.Fatalf("cache round trip: %+v", l)
	}
	if l := LoadCache(path, now.Add(-time.Hour)); l.Version != "" {
		t.Fatal("a check from the future would never go stale")
	}
	for _, bad := range []string{"", "{", `{"version":"v0.5.0"}`, `{"version":"latest","checked":"2026-11-01T09:00:00Z"}`} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if l := LoadCache(path, now); l.Version != "" {
			t.Errorf("cache %q should be ignored, got %+v", bad, l)
		}
	}
	if l := LoadCache(filepath.Join(t.TempDir(), "missing.json"), now); l.Version != "" {
		t.Fatal("a missing cache is no result")
	}
}
