package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

// TestMain leaves out the idle refresh tick, which would make drain wait out its timeout in every test;
// the refresh tests drive onRefresh directly.
func TestMain(m *testing.M) {
	refreshTick = func() tea.Cmd { return nil }
	os.Exit(m.Run())
}

func demoOpts() Options {
	o := Options{Source: gitx.Demo{}, Demo: true, Tools: map[string]string{"git": "/usr/bin/git"}}
	for _, n := range gitx.DemoRepos {
		o.Repos = append(o.Repos, config.Repo{Name: n, Path: "/demo/" + n, Status: config.StatusOK})
	}
	o.Repos = append(o.Repos, config.Repo{Name: "infra", Path: "/demo/infra", Status: config.StatusMissing, Err: "directory not found"})
	return o
}

// drain runs a command tree synchronously and feeds results back. Commands that block (ticks, cursor blink) are skipped.
func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drain(m, c)
			}
			return
		}
		if msg != nil {
			_, next := m.Update(msg)
			drain(m, next)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func setup(t *testing.T, o Options, w, h int) *Model {
	t.Helper()
	m := New(o)
	_, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	drain(m, tea.Batch(cmd, m.Init())) // as Bubble Tea would: a resize can start loads too
	return m
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "f2":
		return tea.KeyPressMsg{Code: tea.KeyF2}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		if cmd != nil && k != "g" && k != "/" { // g starts a 1s timer; / starts cursor blink
			drain(m, cmd)
		}
	}
}

func TestKeyStringsMatchBindings(t *testing.T) {
	for _, s := range []string{"tab", "shift+tab", "enter", "esc", "f2", "ctrl+r", "j", "?", "/", "pgdown", "pgup"} {
		if got := keyMsg(s).String(); got != s {
			t.Errorf("key %q stringifies as %q", s, got)
		}
	}
}

func TestNavigationAndFocus(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "j", "j")
	if m.cursor[viewOverview] != 2 {
		t.Fatalf("cursor %d, want 2", m.cursor[viewOverview])
	}
	press(m, "k")
	if m.cursor[viewOverview] != 1 {
		t.Fatalf("cursor %d, want 1", m.cursor[viewOverview])
	}
	press(m, "tab")
	if m.focus != paneDetail {
		t.Fatalf("wide layout: tab from main should reach detail, got %v", m.focus)
	}
	press(m, "tab", "j")
	if m.focus != paneSidebar || m.repo != 1 {
		t.Fatalf("sidebar j should select first repo: focus %v repo %d", m.focus, m.repo)
	}
	press(m, "shift+tab")
	if m.focus != paneDetail {
		t.Fatalf("shift+tab wraps backwards, got %v", m.focus)
	}
	// Enter on an overview row opens the Repository view for it.
	press(m, "esc", "enter")
	if m.view != viewRepo || m.repo != 2 {
		t.Fatalf("enter: view %v repo %d", m.view, m.repo)
	}
}

func TestNarrowInspectAndBack(t *testing.T) {
	m := setup(t, demoOpts(), 80, 24)
	press(m, "g", "a")
	press(m, "enter")
	if !m.inspect || m.focus != paneDetail {
		t.Fatal("enter should open the detail level on narrow layouts")
	}
	if !strings.Contains(m.render(), "Details") {
		t.Fatal("inspect should render details in place of the list")
	}
	press(m, "esc")
	if m.inspect || m.focus != paneMain {
		t.Fatal("esc should restore the list")
	}
}

func TestFilterInputConsumesKeys(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	press(m, "g", "a", "/")
	press(m, "g", "j", "?", "tab")
	if m.pendingG || m.help || m.focus != paneMain || m.view != viewActivity {
		t.Fatal("typing in the filter must not trigger shortcuts")
	}
	m.Update(tea.PasteMsg{Content: "x\x1b[31m"})
	if !strings.HasPrefix(m.filters[viewActivity], "gj?") {
		t.Fatalf("filter %q", m.filters[viewActivity])
	}
	press(m, "esc")
	if m.filtering || m.filters[viewActivity] != "" {
		t.Fatalf("esc should cancel and restore the previous filter, got %q", m.filters[viewActivity])
	}
	press(m, "/", "y", "a", "k", "enter")
	if m.filters[viewActivity] != "yak" || len(m.activity()) == 0 {
		t.Fatalf("filter %q matched %d", m.filters[viewActivity], len(m.activity()))
	}
	for _, c := range m.activity() {
		if !strings.Contains(strings.ToLower(c.Subject), "yak") {
			t.Fatalf("unfiltered row %q", c.Subject)
		}
	}
	// Paste outside an input is ignored.
	m.Update(tea.PasteMsg{Content: "gc"})
	if m.view != viewActivity {
		t.Fatal("paste must not trigger navigation")
	}
}

func TestGoSequenceTimeoutAndReset(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	press(m, "g", "t")
	if m.view != viewTags || m.pendingG {
		t.Fatalf("g t: view %v pending %v", m.view, m.pendingG)
	}

	press(m, "g")
	old := m.seqGen
	m.Update(seqExpiredMsg{gen: old})
	if m.pendingG {
		t.Fatal("expiry should clear the pending sequence")
	}
	press(m, "o")
	if m.view != viewTags {
		t.Fatal("o after expiry must not navigate")
	}

	press(m, "g")
	m.Update(seqExpiredMsg{gen: old}) // expiry from the earlier sequence
	if !m.pendingG {
		t.Fatal("a stale expiry must not cancel a newer sequence")
	}
	press(m, "tab")
	if m.pendingG {
		t.Fatal("changing focus resets the sequence")
	}
}

// countingSource counts loads: logs return a commit whose subject records which call produced it,
// and commit lists come from the demo.
type countingSource struct {
	gitx.Demo
	calls, compares atomic.Int32
}

func (c *countingSource) Log(_ context.Context, repo, _ string, _ int) ([]gitx.Commit, error) {
	n := c.calls.Add(1)
	return []gitx.Commit{{Repo: repo, Hash: "abc", Subject: "call " + string('0'+n)}}, nil
}

func (c *countingSource) Compare(ctx context.Context, repo, dir, base, head string) ([]gitx.Commit, error) {
	c.compares.Add(1)
	return c.Demo.Compare(ctx, repo, dir, base, head)
}

func TestStaleResultsDropped(t *testing.T) {
	src := &countingSource{}
	o := demoOpts()
	o.Source = src
	m := setup(t, o, 120, 30)
	m.selectRepo(1)
	m.view = viewRepo
	c1 := m.load(0, kindLog, false)
	c2 := m.load(0, kindLog, true) // refresh supersedes c1
	msg1, msg2 := c1(), c2()
	m.Update(msg2)
	m.Update(msg1)
	got := m.repos[0].log.val[0].Subject
	if got != "call 2" {
		t.Fatalf("stale result overwrote newer one: %q", got)
	}

	// A comparison started for one repository is ignored after switching to another.
	m.view = viewTags
	m.cmp = compareState{repo: m.repo, base: "v1.7.0", head: "v1.9.0", open: true}
	stale := m.loadCompare()()
	m.selectRepo(2)
	m.Update(stale)
	if m.cmp.res.loaded || len(m.cmp.res.val) != 0 {
		t.Fatal("comparison result leaked into another repository")
	}
}

func TestSelectionSurvivesReorder(t *testing.T) {
	top := setup(t, demoOpts(), 120, 30)
	top.view = viewActivity
	a, b := top.load(0, kindLog, false), top.load(1, kindLog, false)
	top.Update(b()) // frontend first; backend's rows then sort above it
	top.Update(a())
	if top.cursor[viewActivity] != 0 {
		t.Fatalf("an untouched cursor should stay on the newest row, got %d", top.cursor[viewActivity])
	}

	m := setup(t, demoOpts(), 120, 30)
	m.view = viewActivity
	first, second := m.load(0, kindLog, false), m.load(1, kindLog, false)
	m.Update(first())
	m.cursor[viewActivity] = 3
	want := m.activity()[3]
	m.Update(second()) // interleaves newer commits above the selection
	if got := m.activity()[m.cursor[viewActivity]]; got.Hash != want.Hash || got.Repo != want.Repo {
		t.Fatalf("selection moved from %s/%s to %s/%s", want.Repo, want.Hash, got.Repo, got.Hash)
	}
	if m.cursor[viewActivity] == 3 {
		t.Fatal("fixture should have shifted the selected row")
	}
}

func TestEscHintOnlyWhereItActs(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	m.selectRepo(1)
	drain(m, m.setView(viewTags))
	press(m, "enter")
	if m.cmp.base == "" || !m.canBack() {
		t.Fatal("tags with a base should offer esc")
	}
	press(m, "g", "a")
	if m.canBack() {
		t.Fatal("esc must not be offered on Activity for Tags state")
	}
}

func TestShrinkingKeepsDetailFocusVisible(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "tab")
	if m.focus != paneDetail {
		t.Fatal("setup: tab should focus details")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if !m.inspect || !slices.Contains(m.panes(), m.focus) {
		t.Fatalf("focus %v is not a visible pane", m.focus)
	}
	press(m, "esc")
	if m.inspect || m.focus != paneMain {
		t.Fatal("esc should return to the list")
	}
}

// fakeClock moves the refresh cadence's clock for one test.
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	real := clock
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = real })
	return &now
}

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
	m.repos[0].prs = slot[[]gitx.PR]{loaded: true, at: clock()}
	press(m, "g", "p")
	m.repos[0].prs.loading = true
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
	if got := m.wanted(); len(got) != 0 {
		t.Fatalf("with the header off only the view's data refreshes, got %v", got)
	}
}

func TestPullRequestsViewAndConnection(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	github := func() connRow { return m.connections()[2] } // GitHub (pull requests)
	if github().state != "not checked yet" {
		t.Fatalf("before loading: %q", github().state)
	}
	m.view = viewPRs
	first, second := m.load(0, kindPRs, false), m.load(1, kindPRs, false)
	m.Update(first())
	m.cursor[viewPRs] = 1
	want := m.prList()[1]
	m.Update(second()) // newer PRs from another repository sort above the selection
	if got := m.prList()[m.cursor[viewPRs]]; got.Repo != want.Repo || got.Number != want.Number {
		t.Fatalf("selection moved from %s#%d to %s#%d", want.Repo, want.Number, got.Repo, got.Number)
	}
	if github().state != "available" {
		t.Fatalf("after loading: %q", github().state)
	}
	drain(m, m.load(3, kindPRs, false)) // legacy-api fixture fails
	if github().level != levelWarn || !strings.Contains(m.render(), "Pull requests") {
		t.Fatalf("failure not reported: %+v", github())
	}
	press(m, "/", "#", "4", "7")
	got := m.prList()
	if len(got) == 0 || len(got) == len(m.repos[0].prs.val)+len(m.repos[1].prs.val) {
		t.Fatalf("filter #47 kept %d rows", len(got))
	}
	for _, p := range got {
		if !strings.HasPrefix("#"+strconv.Itoa(p.Number), "#47") {
			t.Fatalf("filter let through #%d", p.Number)
		}
	}
}

func TestEnvironmentPendingCommits(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "shift+tab", "j", "tab", "g", "e") // pick backend first: switching repository would reset the state anyway
	envs := m.envList()
	if m.view != viewEnvs || len(envs) == 0 {
		t.Fatalf("view %v, %d environments", m.view, len(envs))
	}
	press(m, "enter")
	if !m.cmpOpen() || m.cmp.baseRev != envs[0].Commit || m.cmp.headRev != "origin/HEAD" {
		t.Fatalf("enter should list commits since the deployed commit: %+v", m.cmp)
	}
	if !strings.Contains(m.render(), "Not yet in") || m.filterable() {
		t.Fatal("pending list title missing, or filter offered on it")
	}
	// A reload showing a newer deployed commit reopens the list against it.
	r := &m.repos[envs[0].repo]
	moved := slices.Clone(r.deps.val)
	for i := range moved {
		if moved[i].Environment == envs[0].Environment {
			moved[i].Commit = strings.Repeat("f", 40)
		}
	}
	r.deps.gen++
	if _, cmd := m.Update(resultMsg{kind: kindDeps, repo: envs[0].repo, gen: r.deps.gen, val: moved}); cmd == nil || m.cmp.baseRev != strings.Repeat("f", 40) {
		t.Fatalf("open list should follow the new deployed commit: %q", m.cmp.baseRev)
	}
	if !strings.Contains(m.cmp.title, "fffffff") {
		t.Fatalf("title should name the new commit: %q", m.cmp.title)
	}

	press(m, "esc")
	if m.cmpOpen() || m.view != viewEnvs {
		t.Fatal("esc should return to the environment list")
	}

	// Leftover environment state must not become a base tag in Tags.
	press(m, "g", "t", "enter")
	if m.cmp.view != viewTags || m.cmp.base != m.tagList()[0].Name || m.cmp.open {
		t.Fatalf("first Enter in Tags should only pick a base tag: %+v", m.cmp)
	}

	// No successful deployment: nothing opens and nothing reaches git.
	m.cmp = compareState{}
	if cmd := m.openPending(envRow{0, gitx.Deployment{Repo: "backend", Environment: "old", Latest: "FAILURE"}}); cmd != nil || m.cmp.open {
		t.Fatal("an environment without a deployed commit must not open")
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
		if want := i == 0; m.repos[i].runs.loading != want { // only backend has a running demo deploy
			t.Fatalf("repo %d reloading=%v; only repositories with a running deploy should reload", i, m.repos[i].runs.loading)
		}
	}
	r := &m.repos[0].runs // backend has the running demo deploy
	r.gen++
	m.Update(resultMsg{kind: kindRuns, repo: 0, gen: r.gen, err: context.DeadlineExceeded})
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

func TestOpenPullRequestInBrowser(t *testing.T) {
	var opened []string
	real := openBrowser
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openBrowser = real })

	m := setup(t, demoOpts(), 140, 40)
	if m.canBrowse() {
		t.Fatal("o should only be offered on pull requests")
	}
	press(m, "g", "p", "j")
	want := m.prList()[1]
	press(m, "o")
	if len(opened) != 1 || opened[0] != want.URL {
		t.Fatalf("opened %v, want %s", opened, want.URL)
	}
	if !strings.Contains(m.flash, "#"+strconv.Itoa(want.Number)) {
		t.Fatalf("flash %q", m.flash)
	}
	if !strings.Contains(m.footer(), "open in browser") {
		t.Fatal("footer should offer o on pull requests")
	}

	m.repos[0].prs.val[0].URL = "file:///etc/passwd"
	for i, p := range m.prList() {
		if p.URL == "file:///etc/passwd" {
			m.cursor[viewPRs] = i
		}
	}
	press(m, "o")
	if len(opened) != 1 {
		t.Fatalf("a non-https link must not be opened: %v", opened)
	}
}

func TestShortDatesAndFailedRefreshMark(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	m.utc = true
	at := time.Date(2026, 10, 30, 3, 12, 0, 0, time.UTC)
	if m.stamp(at, 80) != "2026-10-30 03:12" || m.stamp(at, 60) != "10-30 03:12" {
		t.Fatalf("stamp: %q / %q", m.stamp(at, 80), m.stamp(at, 60))
	}
	press(m, "g", "a")
	if !regexp.MustCompile(`\d\d-\d\d \d\d:\d\d`).MatchString(m.render()) {
		t.Fatal("Activity at 140 columns should still show dates")
	}

	r := &m.repos[0] // loaded earlier; now a refresh fails
	r.summary.err = context.DeadlineExceeded
	glyph := m.overviewSegs(r)[0]
	if glyph.st.Render(glyph.s) != m.st.bad.Render(glyph.s) || !strings.HasPrefix(glyph.s, "!") {
		t.Fatalf("a failed refresh should keep a red !, got %q", glyph.s)
	}
}

func TestToggleUTC(t *testing.T) {
	// Pin the local zone: CI machines run in UTC, where local and UTC look the same.
	real := time.Local
	time.Local = time.FixedZone("AEDT", 11*60*60)
	t.Cleanup(func() { time.Local = real })

	m := setup(t, demoOpts(), 140, 40)
	at := time.Date(2026, 10, 30, 3, 12, 0, 0, time.UTC)
	local := "2026-10-30 14:12"
	if m.when(at) != local || !strings.Contains(m.header(), "AEDT") || strings.Contains(m.header(), " UTC") {
		t.Fatalf("times should start in the local zone: %q, header %q", m.when(at), m.header())
	}
	press(m, "u")
	if m.when(at) != "2026-10-30 03:12" || !strings.Contains(m.header(), "UTC") || !strings.Contains(m.flash, "UTC") {
		t.Fatalf("after u: %q, header %q", m.when(at), m.header())
	}
	press(m, "u")
	if m.when(at) != local {
		t.Fatal("u again should switch back to local time")
	}
}

func TestDiffView(t *testing.T) {
	m := setup(t, demoOpts(), 140, 30)
	if m.canDiff() {
		t.Fatal("backend is clean, so d should not be offered")
	}
	press(m, "j") // frontend has changes in the demo data
	if !m.canDiff() || !strings.Contains(m.footer(), "diff") {
		t.Fatal("d should be offered for a repository with changes")
	}
	press(m, "d")
	if !m.diff.open || !strings.Contains(m.render(), "Uncommitted changes · frontend") {
		t.Fatal("d should open the diff view")
	}
	out := strings.Join(m.diff.res.val, "\n")
	if !strings.Contains(out, "## Staged changes") || !strings.Contains(out, "## Untracked files") {
		t.Fatalf("diff content: %q", out)
	}
	press(m, "g", "a") // workspace keys do nothing while the diff is open
	if m.view != viewOverview || !m.diff.open {
		t.Fatal("keys other than scrolling and closing must not act behind the diff")
	}
	press(m, "esc")
	if m.diff.open {
		t.Fatal("esc should close the diff")
	}

	// A result that arrives after closing is dropped.
	press(m, "d")
	gen := m.diff.res.gen
	press(m, "esc")
	m.Update(resultMsg{kind: kindDiff, repo: m.diff.repo, gen: gen, val: []string{"late"}})
	if m.diff.open || len(m.diff.res.val) != 0 {
		t.Fatal("a late diff must not reopen or fill the view")
	}

	press(m, "shift+tab", "j", "j") // sidebar: frontend
	if !m.canDiff() {
		t.Fatal("d should work from the sidebar")
	}
	press(m, "k")
	if m.canDiff() {
		t.Fatal("not for a clean repository in the sidebar")
	}
}

func TestDiffScrolls(t *testing.T) {
	m := setup(t, demoOpts(), 80, 16)
	m.diff = diffState{open: true, repo: 1, res: slot[[]string]{loaded: true, val: make([]string, 40)}}
	press(m, "j", "j")
	if m.diff.off != 2 {
		t.Fatalf("j should scroll: off %d", m.diff.off)
	}
	press(m, "pgdown", "pgdown", "pgdown", "pgdown")
	if last := 40 - (m.layout().bodyH - 1); m.diff.off != last {
		t.Fatalf("scrolling stops at the end: off %d, want %d", m.diff.off, last)
	}
	press(m, "pgup", "pgup", "pgup", "pgup", "pgup")
	if m.diff.off != 0 {
		t.Fatalf("scrolling stops at the top: off %d", m.diff.off)
	}
}

func TestPullRequestBodyFilesAndDiff(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "p")
	p := m.prList()[0]
	detail := strings.Join(m.detailLines(), "\n")
	for _, want := range []string{"Description", "## Summary", "Files changed (2)", "+12", "app/maths.ts",
		"[image: Screenshot of the change · this terminal cannot draw images"} {
		if !strings.Contains(detail, want) {
			t.Errorf("PR details missing %q", want)
		}
	}
	press(m, "d")
	if !m.diff.open || !strings.Contains(m.diff.title, "Pull request #"+strconv.Itoa(p.Number)) ||
		!strings.Contains(strings.Join(m.diff.res.val, "\n"), "PR #"+strconv.Itoa(p.Number)) {
		t.Fatalf("d on a PR should open its diff: %q %v", m.diff.title, m.diff.res.val)
	}
	press(m, "esc")

	long := strings.Repeat("line\n", maxBodyLines+50)
	if shown, more := bodyLines(long); len(shown) != maxBodyLines || more != 50 {
		t.Fatalf("body cap: %d shown, %d more", len(shown), more)
	}
}

func TestPullRequestPictures(t *testing.T) {
	var fetched []string
	real := fetchPic
	fetchPic = func(_ context.Context, _ gitx.Source, url string, id int) (termimg.Picture, error) {
		fetched = append(fetched, url)
		return termimg.Picture{ID: id, Width: 400, Height: 200, Transmit: "\x1b_Ga=t,q=2\x1b\\"}, nil
	}
	t.Cleanup(func() { fetchPic = real })

	o := demoOpts()
	o.Images = true
	m := setup(t, o, 140, 40)
	m.repos[0].prs.val = []gitx.PR{{Repo: "backend", Number: 1, Title: "UI change", URL: "https://github.com/o/r/pull/1",
		Body: "Before:\n![before](https://github.com/user-attachments/assets/aaa)\n<img src=\"https://evil.example/track.png\">"}}
	m.repos[0].prs.loaded, m.repos[0].prs.at = true, clock() // fresh, so the cadence keeps these PRs
	m.selectRepo(1)
	press(m, "g", "p")
	if len(fetched) != 1 || fetched[0] != "https://github.com/user-attachments/assets/aaa" {
		t.Fatalf("only the GitHub picture should be fetched, got %v", fetched)
	}
	detail := strings.Join(m.detailLines(), "\n")
	if !strings.ContainsRune(detail, kitty.Placeholder) {
		t.Fatal("a loaded picture should be drawn with placeholders")
	}
	if !strings.Contains(detail, "not hosted on GitHub") {
		t.Fatal("a picture on another host should be named but not loaded")
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := lipgloss.Width(l); w > 140 {
			t.Fatalf("line %d is %d wide with a picture", i, w)
		}
	}
	press(m, "j", "k") // moving again must not fetch the same picture twice
	if len(fetched) != 1 {
		t.Fatalf("picture fetched again: %v", fetched)
	}
	st := m.pics["https://github.com/user-attachments/assets/aaa"]
	if st.pic.Transmit != "" {
		t.Fatal("the encoded picture should be dropped once it is sent")
	}

	// A resize re-places the stored picture at the new width; it is not downloaded or sent again.
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if len(fetched) != 1 || st.placed != m.picCols() || cmd == nil {
		t.Fatalf("resize: fetched %v, placed %d, want %d", fetched, st.placed, m.picCols())
	}
	if raw, ok := cmd().(tea.RawMsg); !ok || !strings.Contains(fmt.Sprint(raw.Msg), "a=p") || strings.Contains(fmt.Sprint(raw.Msg), "a=t") {
		t.Fatalf("resize should send only a placement, got %#v", cmd())
	}
	if !strings.ContainsRune(strings.Join(m.detailLines(), ""), kitty.Placeholder) {
		t.Fatal("the picture should still draw after a resize")
	}
	if free := m.freePics(); free == nil || !strings.Contains(fmt.Sprint(free()), "a=d") {
		t.Fatal("quitting should delete the pictures from the terminal")
	}
}

func TestPullRequestPictureError(t *testing.T) {
	real := fetchPic
	fetchPic = func(context.Context, gitx.Source, string, int) (termimg.Picture, error) {
		return termimg.Picture{}, errors.New("HTTP 404")
	}
	t.Cleanup(func() { fetchPic = real })

	o := demoOpts()
	o.Images = true
	m := setup(t, o, 140, 40)
	m.repos[0].prs.val = []gitx.PR{{Repo: "backend", Number: 1, Title: "x",
		Body: `<img alt="shot" src="https://github.com/user-attachments/assets/aaa">`}}
	m.repos[0].prs.loaded, m.repos[0].prs.at = true, clock() // fresh, so the cadence keeps these PRs
	m.selectRepo(1)
	press(m, "g", "p")
	if detail := strings.Join(m.detailLines(), "\n"); !strings.Contains(detail, "[image: shot · not shown: HTTP 404]") {
		t.Fatalf("a failed picture should say why, not keep loading:\n%s", detail)
	}
}

func TestHelpFitsCommonSizes(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {60, 16}, {140, 30}} {
		m := setup(t, demoOpts(), sz[0], sz[1])
		press(m, "g", "p", "?") // Pull requests offers the most shortcuts
		out := ansi.Strip(m.render())
		if !strings.Contains(out, "g then") || !strings.Contains(out, "c Connections") {
			t.Errorf("%dx%d: every view jump should be visible, including the last", sz[0], sz[1])
		}
		if !strings.Contains(m.footer(), "close help") {
			t.Errorf("%dx%d: the footer should say how to close help", sz[0], sz[1])
		}
		if !strings.Contains(out, "quit") || !strings.Contains(out, "UTC / local times") {
			t.Errorf("%dx%d: every shortcut should fit, including u and ctrl+c:\n%s", sz[0], sz[1], out)
		}
	}

	// The most shortcuts at once: an active filter adds esc. It must still fit at the minimum size.
	m := setup(t, demoOpts(), 60, 16)
	press(m, "g", "p", "/", "x", "enter", "?")
	out := ansi.Strip(m.render())
	// "UTC / local times" is the longest description; seeing it whole means no column was cut at the edge.
	for _, want := range []string{"back", "quit", "UTC / local times", "c Connections"} {
		if !strings.Contains(out, want) {
			t.Errorf("60x16 with every shortcut available is missing %q:\n%s", want, out)
		}
	}
}

func TestRefreshKeepsContentOnError(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	m.selectRepo(1)
	drain(m, m.setView(viewRepo))
	before := len(m.repos[0].log.val)
	m.repos[0].log.gen++
	m.Update(resultMsg{kind: kindLog, repo: 0, gen: m.repos[0].log.gen, err: context.DeadlineExceeded})
	if len(m.repos[0].log.val) != before || m.repos[0].log.err == nil {
		t.Fatal("failed refresh should keep previous commits and record the error")
	}
}

func TestTagComparison(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "tab", "tab", "j", "tab", "g", "t")
	if m.repo != 1 || m.view != viewTags || m.focus != paneMain {
		t.Fatalf("setup: repo %d view %v focus %v", m.repo, m.view, m.focus)
	}
	press(m, "j", "j", "enter", "k", "k", "enter")
	if !m.cmp.open || m.cmp.base != "v1.7.0" || m.cmp.head != "v1.9.0" {
		t.Fatalf("compare %+v", m.cmp)
	}
	if len(m.cmp.res.val) != 8 || !strings.Contains(m.render(), "Commits between v1.7.0 and v1.9.0") {
		t.Fatalf("compare results %d", len(m.cmp.res.val))
	}
	press(m, "esc")
	if m.cmp.open || m.cmp.base != "v1.7.0" {
		t.Fatal("esc closes the comparison but keeps the base")
	}
	press(m, "esc")
	if m.cmp.base != "" {
		t.Fatal("second esc clears the base")
	}
}

func TestRenderFitsTerminal(t *testing.T) {
	sizes := [][2]int{{60, 16}, {80, 24}, {100, 30}, {160, 50}}
	for _, sz := range sizes {
		m := setup(t, demoOpts(), sz[0], sz[1])
		check := func(label string) {
			out := m.render()
			lines := strings.Split(out, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%dx%d %s: %d lines", sz[0], sz[1], label, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%dx%d %s: line %d is %d wide", sz[0], sz[1], label, i, w)
				}
			}
		}
		for _, g := range goKeys {
			press(m, "g", g.key)
			check(viewNames[g.view])
		}
		press(m, "tab", "j", "j", "g", "r")
		check("repository")
		press(m, "?")
		check("help")
		press(m, "esc", "f2")
		check("light")
	}
}

func TestTooSmall(t *testing.T) {
	m := setup(t, demoOpts(), 50, 10)
	if !strings.Contains(m.render(), "too small") {
		t.Fatal("expected resize message")
	}
}

func TestMissingConfigMessage(t *testing.T) {
	m := setup(t, Options{Source: gitx.Git{}, ConfigPath: "/x/config.json", ConfigErr: config.ErrNotFound}, 100, 30)
	if out := m.render(); !strings.Contains(out, "No configuration found") {
		t.Fatalf("missing-config message not shown:\n%s", out)
	}
	press(m, "g", "c")
	if !strings.Contains(m.render(), "not loaded") {
		t.Fatal("connections should report the configuration as not loaded")
	}
}

func TestUnavailableRepoDoesNotBlock(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	if !m.repos[0].summary.loaded {
		t.Fatal("healthy repositories should load")
	}
	if m.repos[3].summary.err == nil {
		t.Fatal("legacy-api fixture should report an error")
	}
	if m.repos[4].summary.loading || m.repos[4].summary.loaded {
		t.Fatal("missing repositories must not be queried")
	}
}

func TestClean(t *testing.T) {
	in := "ok\x1b[31mred\x1b[0m\x1b]0;title\x07\x07bell\u009b\ttab"
	if got := clean(in); got != "okredbell tab" {
		t.Fatalf("clean = %q", got)
	}
}

func TestStackedRows(t *testing.T) {
	m := setup(t, demoOpts(), 140, 16) // details on, so the list is under stackedBelow
	m.view, m.focus = viewPRs, paneMain
	for i := range m.repos {
		drain(m, m.load(i, kindPRs, false))
	}
	prs := m.prList()
	if !m.stacked(viewPRs) || m.listHeight(viewPRs) >= len(prs) {
		t.Fatalf("want stacked rows that scroll: stacked %v, height %d, %d PRs", m.stacked(viewPRs), m.listHeight(viewPRs), len(prs))
	}
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	num := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "#"+strconv.Itoa(prs[0].Number)) })
	if num < 0 || num+1 == len(lines) || !strings.Contains(lines[num+1], prs[0].Title[:10]) {
		t.Fatalf("title should sit under its number at line %d:\n%s", num, strings.Join(lines, "\n"))
	}
	for range prs {
		press(m, "j")
	}
	if !strings.Contains(ansi.Strip(m.render()), prs[len(prs)-1].Title[:10]) {
		t.Fatal("the last PR should scroll into view")
	}
	i := slices.IndexFunc(prs, func(p gitx.PR) bool { return p.ForMe })
	if i < 0 {
		t.Fatal("demo should request a review from the viewer")
	}
	star := func(p gitx.PR) bool {
		return strings.Contains(ansi.Strip(m.row(m.prSegs(p, 80, true), 80, false, true)[0]), "★")
	}
	if !star(prs[i]) || star(gitx.PR{Repo: "x", Number: 1}) {
		t.Fatal("only PRs waiting on the viewer get a star")
	}
}

func TestProfileHeader(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	m := setup(t, o, 120, 30)
	out := ansi.Strip(m.render())
	for _, want := range []string{"Alice", "@alice", "Acme · Remote", "review request", "128 followers"} {
		if !strings.Contains(out, want) {
			t.Fatalf("full header lacks %q:\n%s", want, out)
		}
	}
	lines := strings.Split(m.render(), "\n")
	if len(lines) != 30 || !strings.Contains(ansi.Strip(lines[bannerRows]), "Overview") {
		t.Fatalf("the tabs should sit under the banner in a 30-line frame (%d lines)", len(lines))
	}

	press(m, "b") // ASCII
	for i, l := range strings.Split(ansi.Strip(m.render()), "\n")[:bannerRows] {
		for _, r := range l {
			if r > 0x7e {
				t.Fatalf("ASCII header line %d has %q: %s", i, r, l)
			}
		}
	}
	press(m, "b") // text
	if m.profileRows() != 1 || !strings.Contains(ansi.Strip(m.render()), "@alice | ") {
		t.Fatal("text mode is one line")
	}
	press(m, "b") // off
	if m.profileRows() != 0 || strings.Contains(ansi.Strip(m.render()), "@alice") {
		t.Fatal("off hides the header")
	}
}

func TestProfileGivesWayOnShortWindows(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	m := setup(t, o, 60, 16)
	if m.profileShown() != ProfileText {
		t.Fatalf("a 16-line window shows %v, want the one-line header", profileNames[m.profileShown()])
	}
	press(m, "g", "p")
	if f := ansi.Strip(m.footer()); !strings.HasSuffix(strings.TrimSpace(f), "b profile: full") {
		t.Fatalf("the profile toggle stays visible on a narrow footer: %q", f)
	}
	if lines := strings.Split(m.render(), "\n"); len(lines) != 16 {
		t.Fatalf("%d lines", len(lines))
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	if m.profileShown() != ProfileOff {
		t.Fatal("a window too short for the panes drops the header")
	}
}

func TestProfileStatsCountTheViewer(t *testing.T) {
	m := New(demoOpts())
	now := time.Now()
	m.me.val = gitx.Viewer{Login: "me", GitName: "Me"}
	m.repos[0].prs.val = []gitx.PR{
		{Author: "me", Checks: gitx.ChecksFailing, Review: "CHANGES_REQUESTED"},
		{Author: "other", ForMe: true},
		{Author: "other"},
	}
	m.repos[1].log.val = []gitx.Commit{{Author: "Me", When: now.Add(-time.Hour)}, {Author: "Me", When: now.AddDate(0, 0, -8)}, {Author: "You", When: now}}
	m.repos[2].summary = slot[gitx.Summary]{loaded: true, val: gitx.Summary{Unstaged: 1}}
	got := m.profileStats(now)
	want := profileStats{requested: 1, mine: 1, failing: 1, changes: 1, commits: 1, dirty: 1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestProfileAvatarFallbacks(t *testing.T) {
	real := fetchAvatar
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fetchAvatar = func(context.Context, gitx.Source, string) (image.Image, error) { return img, nil }
	t.Cleanup(func() { fetchAvatar = real })

	viewer := gitx.Viewer{Login: "me", Name: "Me", AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4"}
	run := func(images bool, colours colorprofile.Profile) (*Model, tea.Cmd) {
		m := New(demoOpts())
		m.opts.Images, m.profile, m.colours = images, ProfileFull, colours
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m.me = slot[gitx.Viewer]{val: viewer, loaded: true}
		_, cmd := m.Update(m.ensureAvatar()())
		return m, cmd
	}

	m, cmd := run(true, colorprofile.TrueColor)
	if cmd == nil || !strings.Contains(fmt.Sprint(cmd()), "a=p") || !strings.ContainsRune(m.avatarArt(false)[0], kitty.Placeholder) {
		t.Fatal("with Kitty graphics the avatar is a stored picture")
	}
	if free := m.freePics(); free == nil || !strings.Contains(fmt.Sprint(free()), "a=d") {
		t.Fatal("quitting deletes the avatar")
	}
	if m, _ = run(false, colorprofile.TrueColor); !strings.Contains(m.avatarArt(false)[0], "▀") {
		t.Fatal("without graphics, colour terminals get half blocks")
	}
	if m, _ = run(false, colorprofile.ANSI); strings.ContainsAny(ansi.Strip(m.avatarArt(false)[0]), "▀╭") {
		t.Fatal("16-colour terminals get characters")
	}
	if got := m.ensureAvatar(); got != nil {
		t.Fatal("the same avatar is not fetched twice")
	}
	m = New(demoOpts())
	m.me.val = gitx.Viewer{Name: "Mo Bell"} // no picture
	if art := ansi.Strip(strings.Join(m.avatarArt(false), "\n")); !strings.Contains(art, "╭") || !strings.Contains(art, "MB") {
		t.Fatalf("without a picture the avatar is framed initials:\n%s", art)
	}
	if got := sized(viewer.AvatarURL, 64); got != "https://avatars.githubusercontent.com/u/1?s=64&v=4" {
		t.Fatalf("sized: %s", got)
	}
}

func TestTogglesAreRemembered(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	o.StatePath = t.TempDir() + "/state.json"
	m := setup(t, o, 120, 30)
	press(m, "u", "b")
	got, err := config.LoadState(o.StatePath)
	if err != nil || got != (config.State{UTC: true, Profile: "ascii"}) {
		t.Fatalf("saved %+v, %v", got, err)
	}
	o.UTC, o.Profile = got.UTC, ProfileASCII
	if m := setup(t, o, 120, 30); !m.utc || m.profile != ProfileASCII {
		t.Fatal("a new run starts from the saved toggles")
	}

	// A run started with -profile off must not save that override when another toggle changes.
	o.Profile = ProfileOff
	m = setup(t, o, 120, 30)
	press(m, "u")
	if got, _ := config.LoadState(o.StatePath); got != (config.State{UTC: false, Profile: "ascii"}) {
		t.Fatalf("pressing u saved %+v; the -profile flag should not be remembered", got)
	}
}

// TestDemoShowsEveryFeature keeps -demo a full showcase: when a feature is added, give the fixtures what it needs
// and add it here.
func TestDemoShowsEveryFeature(t *testing.T) {
	o := demoOpts()
	o.Profile, o.Images = ProfileFull, true
	m := setup(t, o, 140, 40)
	drain(m, m.ensureAvatar())

	s := m.profileStats(gitx.DemoNow)
	if s.requested == 0 || s.mine == 0 || s.failing == 0 || s.changes == 0 || s.commits == 0 || s.dirty == 0 {
		t.Fatalf("every profile count should be non-zero in demo: %+v", s)
	}
	if m.avatar.img == nil || !strings.ContainsRune(m.avatarArt(false)[0], kitty.Placeholder) {
		t.Fatal("demo should draw an avatar picture")
	}
	if !strings.Contains(ansi.Strip(m.header()), "Just now") { // shortened at 140 columns to keep every tab
		t.Fatal("demo should show how fresh its data is")
	}

	press(m, "g", "p")
	colours := map[any]bool{}
	for _, p := range m.prList() {
		colours[m.authorStyle(p.Author, true).GetForeground()] = true
	}
	if len(colours) < 3 {
		t.Fatalf("demo authors should show several author colours, got %d", len(colours))
	}
	for url, st := range m.pics { // a fetch drain gave up on under -race; finish it here, through the same demo source
		if st.loading {
			pic, err := fetchPic(context.Background(), m.opts.Source, url, picBase+99)
			m.Update(picMsg{url, pic, err})
		}
	}
	if !strings.ContainsRune(strings.Join(m.detailLines(), ""), kitty.Placeholder) {
		t.Fatal("demo PRs should draw their pictures")
	}
	for _, mode := range []Profile{ProfileASCII, ProfileText, ProfileOff, ProfileFull} {
		press(m, "b")
		if m.profile != mode {
			t.Fatalf("b should cycle to %s", profileNames[mode])
		}
	}
}

func TestVersionShown(t *testing.T) {
	o := demoOpts()
	o.Version, o.Executable = "v9.9.9", "/usr/local/bin/git-tui"
	m := setup(t, o, 120, 30)
	press(m, "g", "c")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "git-tui") || !strings.Contains(out, "v9.9.9") {
		t.Fatalf("Connections should list the version:\n%s", out)
	}
	press(m, "?")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "git-tui v9.9.9") {
		t.Fatal("help should show the version")
	}
}

func TestAuthorColours(t *testing.T) {
	m := New(demoOpts())
	fg := func(name string) any { return m.authorStyle(name, false).GetForeground() }
	login := func(name string) any { return m.authorStyle(name, true).GetForeground() }
	if fg("Jane Doe") != fg("jane.doe") || fg("Bob") != fg("b.o.b") {
		t.Fatal("case and punctuation do not change a person's colour")
	}
	if fg("renovate[bot]") != m.st.muted.GetForeground() || fg("") != m.st.muted.GetForeground() ||
		login("app/dependabot") != m.st.muted.GetForeground() {
		t.Fatal("bots (including GitHub Apps, which gh reports as app/<name>) and empty names are muted")
	}
	m.me.val = gitx.Viewer{Login: "janedoe", Name: "Jane DOe", GitName: "Jane Doe"}
	for _, you := range []string{"janedoe", "Jane DOe", "Jane Doe"} {
		if fg(you) != m.st.accent.GetForeground() {
			t.Fatalf("%q is the viewer and gets the accent colour", you)
		}
	}
	if login("JaneDoe") != m.st.accent.GetForeground() {
		t.Fatal("the viewer's own login matches whatever its case")
	}
	m.me.val.Login = "anna-lee"
	if login("annalee") == m.st.accent.GetForeground() {
		t.Fatal("a PR login matches the viewer's login exactly: annalee is someone else's account")
	}
	seen := map[any]bool{}
	for _, name := range []string{"Bob", "Chuck", "Dave", "bob", "chuck", "dave"} {
		seen[fg(name)] = true
		if fg(name) == m.st.muted.GetForeground() || fg(name) == m.st.accent.GetForeground() {
			t.Fatalf("%q should get a palette colour", name)
		}
	}
	if len(seen) < 3 {
		t.Fatalf("the demo's people should spread across the palette, got %d colours", len(seen))
	}
}

func TestStackedRowsGiveTheTitleItsOwnLine(t *testing.T) {
	m := setup(t, demoOpts(), 90, 30) // a list narrower than stackedBelow
	press(m, "g", "a")
	if !m.stacked(viewActivity) {
		t.Fatal("setup: rows should be stacked")
	}
	c := m.activity()[1]
	lines := m.row(m.commitSegs(c, m.layout().mainW, true), m.layout().mainW, false, true)
	if len(lines) != 2 || !strings.Contains(ansi.Strip(lines[0]), c.Author) {
		t.Fatalf("stacked: the author belongs on the first line: %q", lines)
	}
	if got := strings.TrimSpace(ansi.Strip(lines[1])); got != c.Subject {
		t.Fatalf("stacked: the second line is the title alone, got %q", got)
	}

	m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	wide := m.row(m.commitSegs(c, m.layout().mainW, true), m.layout().mainW, false, false)
	s := ansi.Strip(wide[0])
	ti, ai := strings.Index(s, c.Subject), strings.Index(s, c.Author)
	if len(wide) != 1 || ti < 0 || ai < 0 || ti > ai {
		t.Fatalf("wide: one line with both, title before author: %q", wide)
	}

	// A first line too narrow for the author keeps it whole, after the title on the second line.
	long := []seg{{strings.Repeat("x", 40), m.st.text}}
	fits := titled(long, seg{"Title  ", m.st.text}, seg{"bob", m.st.muted}, true, 50)
	cut := titled(long, seg{"Title  ", m.st.text}, seg{"renovate[bot]", m.st.muted}, true, 50)
	if fits[1].s != "bob" || fits[2].s != brk.s || cut[1].s != brk.s || cut[3].s != "renovate[bot]" {
		t.Fatalf("author placement: fits %v, too long %v", fits, cut)
	}
}

func TestHeaderKeepsTabsBeforeTheAge(t *testing.T) {
	m := setup(t, demoOpts(), 200, 30)
	if h := ansi.Strip(m.header()); !strings.Contains(h, "Connections") || !strings.Contains(h, "Updated just now") {
		t.Fatalf("a wide header has every tab and the full age: %q", h)
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	if h := ansi.Strip(m.header()); !strings.Contains(h, "Connections") || !strings.Contains(h, "Just now") {
		t.Fatalf("at 140 columns the tabs stay and the age shortens: %q", h)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if h := ansi.Strip(m.header()); strings.Contains(h, "Connections") || !strings.Contains(h, "Updated just now") {
		t.Fatalf("when the tabs cannot fit anyway, the full age shows: %q", h)
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
