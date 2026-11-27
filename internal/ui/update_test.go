package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/shaungsimpson/git-tui/internal/update"
)

// releaseSource answers update checks with a newer version on each call, or fails while fail is set.
type releaseSource struct {
	countingSource
	checks atomic.Int32
	fail   atomic.Bool
}

func (s *releaseSource) LatestRelease(context.Context) (update.Latest, error) {
	n := s.checks.Add(1)
	if s.fail.Load() {
		return update.Latest{}, errors.New("gh api: HTTP 401: Bad credentials")
	}
	return update.Latest{Version: "v0.5." + string('0'+n-1)}, nil
}

func (m *Model) releaseDetail() string { return strings.Join(m.releaseRow().detail, "\n") }

func TestReleaseBuildChecksDailyAndKeepsResultOnFailure(t *testing.T) {
	now := fakeClock(t)
	src := &releaseSource{}
	o := demoOpts()
	o.Source = src
	m := setup(t, o, 120, 30)
	if src.checks.Load() != 1 || m.updateState() != update.Available || !strings.Contains(ansi.Strip(m.render()), "Update v0.5.0") {
		t.Fatalf("a release build checks at start and shows the badge: %d checks, %s", src.checks.Load(), m.updateState())
	}
	*now = now.Add(update.MaxAge - time.Minute)
	press(m, "j")
	if src.checks.Load() != 1 {
		t.Fatal("a check is trusted for a day")
	}
	*now = now.Add(time.Minute)
	src.fail.Store(true)
	press(m, "k")
	if src.checks.Load() != 2 || m.updateState() != update.Available {
		t.Fatalf("a failed refresh keeps the earlier result: %d checks, %s", src.checks.Load(), m.updateState())
	}
	if d := m.releaseDetail(); !strings.Contains(d, "checked 24h ago") || !strings.Contains(d, "Bad credentials") {
		t.Fatalf("details should show the old result's age and the failure:\n%s", d)
	}
	press(m, "j")
	if src.checks.Load() != 2 {
		t.Fatal("a failed check waits for its backoff")
	}
	src.fail.Store(false)
	press(m, "ctrl+r")
	if src.checks.Load() != 3 || m.release.val.Version != "v0.5.2" {
		t.Fatalf("Ctrl+R checks at once: %d checks, %+v", src.checks.Load(), m.release.val)
	}
}

func TestOtherBuildsCheckOnlyWhenAsked(t *testing.T) {
	src := &releaseSource{}
	o := demoOpts()
	o.Source, o.Build = src, update.Build{Kind: update.Source, Version: "v0.4.1", Revision: "7f4b020574b1abcdef", Dirty: true}
	m := setup(t, o, 120, 30)
	press(m, "ctrl+r")
	if src.checks.Load() != 0 {
		t.Fatal("a source build is not checked automatically")
	}
	press(m, "g", "c", "ctrl+r")
	if src.checks.Load() != 1 || m.updateState() != update.NotComparable || strings.Contains(ansi.Strip(m.render()), "Update v") {
		t.Fatalf("Ctrl+R in Connections checks, but a source build is never out of date: %d, %s", src.checks.Load(), m.updateState())
	}
	if d := m.releaseDetail(); !strings.Contains(d, "Latest stable release: v0.5.0") || !strings.Contains(d, "source build of 7f4b020574b1 with uncommitted changes") ||
		!strings.Contains(d, "mise run install-cli") || strings.Contains(d, "go install github.com") {
		t.Fatalf("source details:\n%s", d)
	}
}

func TestCachedCheckIsUsedAtStart(t *testing.T) {
	now := fakeClock(t)
	src := &releaseSource{}
	o := demoOpts()
	o.Source, o.Release = src, update.Latest{Version: "v0.4.1", Checked: now.Add(-time.Hour)}
	m := setup(t, o, 120, 30)
	if src.checks.Load() != 0 || m.updateState() != update.UpToDate || !strings.Contains(m.releaseDetail(), "This is the latest stable release.") {
		t.Fatalf("an hour-old check is used as it is: %d checks, %s", src.checks.Load(), m.updateState())
	}
}

func TestSupersededCheckIsDropped(t *testing.T) {
	src := &releaseSource{}
	o := demoOpts()
	o.Source = src
	m := New(o)
	first, second := m.load(-1, kindRelease, true), m.load(-1, kindRelease, true)
	older, newer := first(), second()
	m.Update(newer)
	m.Update(older)
	if m.release.val.Version != "v0.5.1" {
		t.Fatalf("the older check's answer replaced the newer one: %+v", m.release.val)
	}
}

func TestUpdateReport(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	l := update.Latest{Version: "v0.5.0", Checked: now.Add(-3 * time.Hour)}
	out := strings.Join(UpdateReport(update.Build{Kind: update.Release, Version: "v0.4.1"}, "/bin/git-tui", l, nil, now), "\n")
	for _, want := range []string{"git-tui: update available", "Executable: /bin/git-tui", "checked 3h ago",
		"go install github.com/shaungsimpson/git-tui/cmd/git-tui@v0.5.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("release report is missing %q:\n%s", want, out)
		}
	}
	home, _ := os.UserHomeDir()
	out = strings.Join(UpdateReport(update.Build{}, filepath.Join(home, "go", "bin", "git-tui"), l, nil, now), "\n")
	if !strings.Contains(out, "Executable: ~/go/bin/git-tui") || (home != "" && strings.Contains(out, home)) {
		t.Errorf("a path in the home folder is shown from ~, so a shared screenshot does not name the account:\n%s", out)
	}
	if !strings.Contains(out, "With go install: ") || !strings.Contains(out, "From a checkout: ") {
		t.Errorf("an unknown build should get both routes:\n%s", out)
	}
	out = strings.Join(UpdateReport(update.Build{Kind: update.Release, Version: "v0.5.0"}, "", l, errors.New("offline"), now), "\n")
	if !strings.Contains(out, "No newer release at the last successful check.") || strings.Contains(out, "This is the latest") {
		t.Errorf("after a failed check, up to date is only as of the last good one:\n%s", out)
	}
	out = strings.Join(UpdateReport(update.Build{Kind: update.Release, Version: "v0.4.1"}, "", update.Latest{}, errors.New("gh: \x1b[31mbad\x1b[0m"), now), "\n")
	if !strings.HasPrefix(out, "git-tui: check failed") || strings.Contains(out, "\x1b") || strings.Contains(out, "@") || !strings.Contains(out, "gh auth status") {
		t.Errorf("a failed check is said plainly and sanitised, with no install command:\n%q", out)
	}
}
