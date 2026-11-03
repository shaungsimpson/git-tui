package ui

import (
	"context"
	"errors"
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
		if w.kind != kindViewer && w.kind != kindPRs && w.kind != kindRelease {
			t.Fatalf("with the header off only the view's data and the badges' PRs and release refresh, got %v", m.wanted())
		}
	}
}

func TestDeployRunsPolling(t *testing.T) {
	now := fakeClock(t)
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
	if _, cmd := m.Update(pollMsg{gen}); cmd == nil || !m.polling || m.repos[0].gh.runs.loading {
		t.Fatal("a tick while the runs are still fresh, say from the idle refresh, should only wait again")
	}
	m.polling = false
	*now = now.Add(m.pollWait)
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

	// In the background and unused, the next tick stops polling; the next key starts it again.
	m.Update(tea.BlurMsg{})
	*now = now.Add(idleAfter)
	if _, cmd := m.Update(pollMsg{m.pollGen}); cmd != nil || m.polling {
		t.Fatal("a window in the background and unused should stop polling")
	}
	press(m, "k")
	if !m.polling {
		t.Fatal("a key should start polling again")
	}
}

func TestPollWaitBacksOff(t *testing.T) {
	for _, c := range []struct{ running, want time.Duration }{
		{0, time.Minute}, {29 * time.Minute, time.Minute},
		{30 * time.Minute, 5 * time.Minute}, {2*time.Hour - time.Second, 5 * time.Minute},
		{2 * time.Hour, 15 * time.Minute}, {72 * time.Hour, 15 * time.Minute},
	} {
		if got := pollWait(c.running); got != c.want {
			t.Errorf("running %s: poll every %s, want %s", c.running, got, c.want)
		}
	}

	// The newest unfinished run sets the pace, so an old stuck run does not slow down watching a new one.
	now := fakeClock(t)
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "d")
	runs := &m.repos[0].gh.runs
	runs.val = []gitx.Run{
		{ID: 1, Status: "waiting", Started: now.Add(-3 * 24 * time.Hour)},
		{ID: 2, Status: "in_progress", Started: now.Add(-5 * time.Minute)},
	}
	m.polling = false
	if m.schedulePoll(); m.pollWait != time.Minute {
		t.Fatalf("a run started 5 minutes ago should poll every minute, got %s", m.pollWait)
	}
	runs.val = runs.val[:1]
	m.polling = false
	if m.schedulePoll(); m.pollWait != 15*time.Minute {
		t.Fatalf("only a run waiting for days should poll every 15 minutes, got %s", m.pollWait)
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

func TestRunReasonRetriesOnCtrlR(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "d")
	m.cursor[viewRuns] = slices.IndexFunc(m.runList(), gitx.Run.Stopped)
	r, _ := m.selectedRun()
	m.why[r.ID] = &slot[[]string]{err: errors.New("timed out"), tried: clock(), fails: 1}
	press(m, "v") // reasons show, and load, on the detail page
	if m.loadWhy(false) != nil {
		t.Fatal("a failed reason read should wait for its backoff")
	}
	press(m, "ctrl+r")
	if s := m.why[r.ID]; !s.loaded || len(s.val) == 0 {
		t.Fatalf("Ctrl+R should read the reason again at once: %+v", s)
	}
}

// Holding j through the sidebar loads only the repository it stops on.
func TestHeldKeysLoadWhereTheyStop(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "t", "shift+tab")
	if m.focus != paneSidebar || m.repo != 0 {
		t.Fatalf("setup: focus %v repo %d", m.focus, m.repo)
	}
	settleAfter = time.Hour // keys arrive faster than they settle
	t.Cleanup(func() { settleAfter = 0 })
	m.Update(keyMsg("j"))
	m.Update(keyMsg("j"))
	if m.repo != 2 || m.repos[0].tags.loading || m.repos[1].tags.loading {
		t.Fatalf("a held key should not load each repository it passes: repo %d", m.repo)
	}
	if _, cmd := m.Update(settledMsg{m.settleGen - 1}); cmd != nil {
		t.Fatal("an earlier key's pause should start nothing")
	}
	_, cmd := m.Update(settledMsg{m.settleGen})
	drain(m, cmd)
	if !m.repos[1].tags.loaded || m.repos[0].tags.loaded {
		t.Fatal("once keys pause, only the repository they stopped on should load")
	}
}
