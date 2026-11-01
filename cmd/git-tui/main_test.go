package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/shaungsimpson/git-tui/internal/update"
)

type fakeReleases struct {
	version string
	err     error
}

func (f fakeReleases) LatestRelease(context.Context) (update.Latest, error) {
	return update.Latest{Version: f.version}, f.err
}

func TestCheckForUpdateExitCode(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "release.json")
	build := update.Build{Kind: update.Release, Version: "v0.4.1"}
	if code := checkForUpdate(build, fakeReleases{version: "v0.5.0"}, cache, ""); code != 0 {
		t.Fatalf("a newer release is a successful check, got exit %d", code)
	}
	if code := checkForUpdate(build, fakeReleases{err: errors.New("HTTP 401")}, cache, ""); code != 1 {
		t.Fatalf("a failed check exits 1, got %d", code)
	}
	if code := checkForUpdate(build, fakeReleases{version: "v0.6.0-rc.1"}, cache, ""); code != 1 {
		t.Fatalf("a release that is not stable is a failed check, got %d", code)
	}
}
