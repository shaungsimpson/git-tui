package ui

import (
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The refresh cadence: data on screen reloads when it is older than its tier allows, checked on every key,
// on returning to the window, on resize and on an idle tick. Checking is a timestamp comparison; only stale
// data costs a git or gh call.
const (
	localAge   = 5 * time.Second  // git takes milliseconds; the floor only stops held keys re-running it
	githubAge  = time.Minute      // about 2 gh calls per repository; fresh enough to catch a review request
	viewerAge  = 30 * time.Minute // a GitHub profile rarely changes
	tickEvery  = time.Minute      // the idle tick, while the window is focused
	idleAfter  = 10 * time.Minute // no key for this long and the idle tick stops until the next one
	maxBackoff = 10 * time.Minute // a failing load retries after 1, 2, 4, 8, then every 10 minutes
)

// clock is the time the cadence reads; a variable so tests can move it.
var clock = time.Now

// ageOf is how long data of a kind stays fresh.
func ageOf(k dataKind) time.Duration {
	switch k {
	case kindPRs, kindDeps, kindRuns:
		return githubAge
	case kindViewer:
		return viewerAge
	}
	return localAge
}

// backoff is how long after its last attempt a load that has failed fails times in a row is retried.
func backoff(fails int) time.Duration {
	return min(time.Minute<<min(max(fails-1, 0), 4), maxBackoff)
}

// due reports whether a slot should load without being forced: never loaded, older than its tier,
// or failed and past its backoff. A load in flight is never due.
func due[T any](s *slot[T], age time.Duration) bool {
	switch {
	case s.loading:
		return false
	case s.err != nil:
		return clock().Sub(s.tried) >= backoff(s.fails)
	case !s.loaded:
		return true
	}
	return clock().Sub(s.at) >= age
}

// want is one piece of data on screen; repo is -1 for the viewer's profile.
type want struct {
	repo int
	kind dataKind
}

// wanted is the data on screen: the view's, plus every repository's PRs, commits and status while the
// profile header shows, since it counts all of them. A hidden header costs nothing.
func (m *Model) wanted() []want {
	var ws []want
	add := func(k dataKind, repos ...int) {
		for _, i := range repos {
			if w := (want{i, k}); !slices.Contains(ws, w) {
				ws = append(ws, w)
			}
		}
	}
	all := make([]int, len(m.repos))
	for i := range all {
		all[i] = i
	}
	switch m.view {
	case viewOverview:
		add(kindSummary, all...)
	case viewActivity:
		add(kindLog, m.scope()...)
	case viewPRs:
		add(kindPRs, m.scope()...)
	case viewRepo:
		if m.repo > 0 {
			add(kindSummary, m.repo-1)
			add(kindLog, m.repo-1)
		}
	case viewTags:
		if m.repo > 0 {
			add(kindTags, m.repo-1)
		}
	case viewEnvs:
		add(kindDeps, m.scope()...)
	case viewRuns:
		add(kindRuns, m.scope()...)
	}
	if m.profileShown() != ProfileOff {
		add(kindViewer, -1)
		add(kindPRs, all...)
		add(kindLog, all...)
		add(kindSummary, all...)
	}
	return ws
}

// times is when a piece of data last loaded, and whether a load is running; zero for unconfigured repositories.
func (m *Model) times(w want) (at time.Time, loading bool) {
	if w.kind == kindViewer {
		return m.me.at, m.me.loading
	}
	if w.repo < 0 || w.repo >= len(m.repos) {
		return time.Time{}, false
	}
	r := &m.repos[w.repo]
	switch w.kind {
	case kindSummary:
		return r.summary.at, r.summary.loading
	case kindLog:
		return r.log.at, r.log.loading
	case kindPRs:
		return r.prs.at, r.prs.loading
	case kindDeps:
		return r.deps.at, r.deps.loading
	case kindRuns:
		return r.runs.at, r.runs.loading
	case kindTags:
		return r.tags.at, r.tags.loading
	}
	return time.Time{}, false // a kind missing here shows no age rather than another kind's
}

// freshness is how old the data on screen is, going by its oldest load (an open commit list included),
// or "Updating…" while a load runs.
// Minutes are enough: the screen redraws at least once a minute while the idle tick runs.
func (m *Model) freshness() string {
	var oldest time.Time
	see := func(at time.Time, loading bool) bool {
		if !at.IsZero() && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
		return loading
	}
	for _, w := range m.wanted() {
		if see(m.times(w)) {
			return "Updating…"
		}
	}
	if m.cmpOpen() && see(m.cmp.res.at, m.cmp.res.loading) {
		return "Updating…"
	}
	switch d := clock().Sub(oldest); {
	case oldest.IsZero():
		return ""
	case d < time.Minute:
		return "Updated just now"
	case d < time.Hour:
		return "Updated " + strconv.Itoa(int(d/time.Minute)) + "m ago"
	default:
		return "Updated " + strconv.Itoa(int(d/time.Hour)) + "h ago"
	}
}

type refreshMsg struct{}

// inUse reports whether someone has used the app recently enough to keep the idle tick going.
func (m *Model) inUse() bool { return clock().Sub(m.lastUse) < idleAfter }

// scheduleRefresh keeps one idle tick going while the window is focused and in use. A terminal that never
// reports focus counts as focused, so there the idle pause is what stops an abandoned window calling gh.
func (m *Model) scheduleRefresh() tea.Cmd {
	if m.ticking || !m.focused || !m.inUse() {
		return nil
	}
	m.ticking = true
	return refreshTick()
}

// refreshTick is the idle tick; a variable so tests, which cannot wait a minute, can leave it out.
var refreshTick = func() tea.Cmd {
	return tea.Tick(tickEvery, func(time.Time) tea.Msg { return refreshMsg{} })
}

// onRefresh reloads stale data on the idle tick. When the window lost focus or went idle the chain stops;
// returning to the window or pressing a key starts it again.
func (m *Model) onRefresh() tea.Cmd {
	m.ticking = false
	if !m.focused || !m.inUse() {
		return nil
	}
	return tea.Batch(m.ensure(false), m.scheduleRefresh())
}

// touch records use: a key press, or coming back to the window.
func (m *Model) touch() tea.Cmd {
	m.lastUse = clock()
	return tea.Batch(m.ensure(false), m.scheduleRefresh())
}
