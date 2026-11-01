package ui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/gitx"
)

func TestFailedLoadBacksOff(t *testing.T) {
	now := fakeClock(t)
	m := setup(t, demoOpts(), 120, 30) // legacy-api (index 3) fails its summary
	if m.repos[3].summary.err == nil {
		t.Fatal("setup: legacy-api should have failed")
	}
	if cmd := m.load(3, kindSummary, false); cmd != nil {
		t.Fatal("navigation must not retry a failure straight away")
	}
	if !strings.Contains(m.statusLine(), "ctrl+r") {
		t.Fatal("status line should say how to retry")
	}
	*now = now.Add(time.Minute)
	drain(m, m.load(3, kindSummary, false))
	if m.repos[3].summary.fails != 2 {
		t.Fatalf("after a minute the failure retries by itself; fails = %d", m.repos[3].summary.fails)
	}
	*now = now.Add(time.Minute)
	if cmd := m.load(3, kindSummary, false); cmd != nil {
		t.Fatal("a second failure waits two minutes")
	}
	if cmd := m.load(3, kindSummary, true); cmd == nil {
		t.Fatal("Ctrl+R retries at once")
	}
	if backoff(1) != time.Minute || backoff(3) != 4*time.Minute || backoff(9) != maxBackoff {
		t.Fatalf("backoff: %v %v %v", backoff(1), backoff(3), backoff(9))
	}
}

func TestStaleDataReloadsOnKeys(t *testing.T) {
	now := fakeClock(t)
	src := &countingSource{}
	o := demoOpts()
	o.Source = src
	m := setup(t, o, 120, 30)
	press(m, "g", "a")
	calls := src.calls.Load()
	press(m, "j")
	if src.calls.Load() != calls {
		t.Fatal("fresh commits are not reloaded on a key")
	}
	*now = now.Add(localAge)
	press(m, "k")
	if src.calls.Load() == calls {
		t.Fatal("commits older than the local floor reload on the next key")
	}
	if got := m.freshness(); got != "Updated just now" {
		t.Fatalf("freshness: %q", got)
	}
	*now = now.Add(3 * time.Minute)
	if got := m.freshness(); got != "Updated 3m ago" {
		t.Fatalf("freshness after 3 minutes: %q", got)
	}
	m.repos[0].gh.prs = slot[[]gitx.PR]{loaded: true, at: clock()}
	press(m, "g", "p")
	m.repos[0].gh.prs.loading = true
	if got := m.freshness(); got != "Updating…" {
		t.Fatalf("freshness while loading: %q", got)
	}
}

func TestGitHubDataWaitsAMinute(t *testing.T) {
	now := fakeClock(t)
	m := setup(t, demoOpts(), 120, 30)
	press(m, "g", "p")
	if m.load(0, kindPRs, false) != nil {
		t.Fatal("PRs loaded just now are fresh")
	}
	*now = now.Add(30 * time.Second)
	if m.load(0, kindPRs, false) != nil {
		t.Fatal("PRs stay fresh for a minute")
	}
	*now = now.Add(30 * time.Second)
	if m.load(0, kindPRs, false) == nil {
		t.Fatal("PRs a minute old reload")
	}
}

func TestIdleTickFollowsFocusAndUse(t *testing.T) {
	now := fakeClock(t)
	m := setup(t, demoOpts(), 120, 30)
	m.ticking = true // as scheduled by Init

	m.Update(tea.BlurMsg{})
	if cmd := m.onRefresh(); cmd != nil || m.ticking {
		t.Fatal("the tick stops while the window is in the background")
	}
	*now = now.Add(5 * time.Minute)
	if _, cmd := m.Update(tea.FocusMsg{}); cmd == nil || !m.ticking {
		t.Fatal("coming back refreshes what is stale and restarts the tick")
	}
	if !m.lastUse.Equal(*now) {
		t.Fatal("coming back counts as use")
	}

	*now = now.Add(idleAfter)
	if cmd := m.onRefresh(); cmd != nil || m.ticking {
		t.Fatal("after ten minutes without a key the tick stops, even while focused")
	}
	press(m, "j")
	if !m.ticking {
		t.Fatal("a key restarts the tick")
	}
	if !New(demoOpts()).focused {
		t.Fatal("a terminal that never reports focus counts as focused")
	}
}

func TestHiddenHeaderLoadsOnlyTheView(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	m := setup(t, o, 120, 30)
	press(m, "g", "t") // Tags with no repository selected wants nothing of its own
	if !slices.Contains(m.wanted(), want{-1, kindViewer}) || !slices.Contains(m.wanted(), want{0, kindPRs}) {
		t.Fatal("the header refreshes the viewer and every repository's PRs")
	}
	press(m, "b", "b", "b") // ascii, text, off
	for _, w := range m.wanted() {
		if w.kind != kindViewer && w.kind != kindPRs {
			t.Fatalf("with the header off only the view's data and the badges' PRs refresh, got %v", m.wanted())
		}
	}
}

func TestDeployRunsPolling(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "d")
	if m.view != viewRuns || len(m.runList()) == 0 {
		t.Fatalf("view %v, %d runs", m.view, len(m.runList()))
	}
	if !m.polling {
		t.Fatal("a running deploy should start polling")
	}
	if !strings.Contains(m.statusLine(), "watching") {
		t.Fatal("status line should say deploys are being watched")
	}

	gen := m.pollGen
	if _, cmd := m.Update(pollMsg{gen}); cmd == nil || m.polling {
		t.Fatal("a current tick should reload runs and wait for the results to reschedule")
	}
	for i := range m.repos {
		if want := i == 0; m.repos[i].gh.runs.loading != want { // only backend has a running demo deploy
			t.Fatalf("repo %d reloading=%v; only repositories with a running deploy should reload", i, m.repos[i].gh.runs.loading)
		}
	}
	m.Update(answer(&m.repos[0].gh.runs, kindRuns, 0, nil, context.DeadlineExceeded)) // backend has the running demo deploy
	if m.polling {
		t.Fatal("a failed reload must stop polling")
	}

	press(m, "g", "a")
	if _, cmd := m.Update(pollMsg{gen}); cmd != nil || m.polling {
		t.Fatal("a tick from before leaving the view must be ignored")
	}

	press(m, "shift+tab", "j", "j", "tab", "g", "d") // frontend: no running deploy
	if m.repo != 2 || m.polling {
		t.Fatalf("repo %d polling %v: no running deploy, so no polling", m.repo, m.polling)
	}
	press(m, "shift+tab", "k") // backend, whose running deploy is already loaded: nothing reloads
	if m.repo != 1 || !m.polling {
		t.Fatalf("repo %d polling %v: selecting a repository with a running deploy should start watching", m.repo, m.polling)
	}
}

func TestOpenCommitListRefreshes(t *testing.T) {
	now := fakeClock(t)
	src := &countingSource{}
	o := demoOpts()
	o.Source = src
	m := setup(t, o, 140, 40)
	press(m, "shift+tab", "j", "tab", "g", "e", "enter") // "Not yet in" list for backend's newest environment
	if !m.cmpOpen() || src.compares.Load() != 1 {
		t.Fatalf("setup: list open %v, loads %d", m.cmpOpen(), src.compares.Load())
	}
	press(m, "j")
	if src.compares.Load() != 1 {
		t.Fatal("a fresh list is not reloaded on a key")
	}
	*now = now.Add(localAge)
	if got := m.freshness(); got != "Updated just now" {
		t.Fatalf("freshness: %q", got)
	}
	m.cmp.res.at = now.Add(-2 * time.Minute) // the list is older than everything else on screen
	if got := m.freshness(); got != "Updated 2m ago" {
		t.Fatalf("the open list counts toward the age: %q", got)
	}
	press(m, "k")
	if src.compares.Load() != 2 {
		t.Fatalf("a stale list reloads on the next key, like other git data; loads %d", src.compares.Load())
	}
}

func TestTimesOfAnUnlistedKindAreZero(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	if at, loading := m.times(want{0, kindDiff}); !at.IsZero() || loading {
		t.Fatalf("a kind times() does not list reports nothing, got %v %v", at, loading)
	}
}
