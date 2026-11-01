package ui

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

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

func TestRefreshKeepsContentOnError(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	m.selectRepo(1)
	drain(m, m.setView(viewRepo))
	before := len(m.repos[0].log.val)
	m.Update(answer(&m.repos[0].log, kindLog, 0, nil, context.DeadlineExceeded))
	if len(m.repos[0].log.val) != before || m.repos[0].log.err == nil {
		t.Fatal("failed refresh should keep previous commits and record the error")
	}
}

func TestClonesShareGitHubData(t *testing.T) {
	src := &countingSource{}
	o := demoOpts()
	o.Source, o.Profile = src, ProfileFull // the header counts every repository's PRs, so they load
	o.Repos = append(o.Repos, config.Repo{Name: "backend-hotfix", Path: "/hotfix/backend", Status: config.StatusOK})
	m := setup(t, o, 120, 30)
	clone := m.repos[len(m.repos)-1]
	if clone.gh != m.repos[0].gh {
		t.Fatal("two clones of one GitHub repository should share its data")
	}
	if got := src.prs.Load(); got != int32(len(gitx.DemoRepos)) {
		t.Fatalf("PRs should load once per GitHub repository, got %d loads", got)
	}
	press(m, "g", "p")
	urls := map[string]bool{}
	for _, p := range m.prList() {
		if urls[p.URL] {
			t.Fatalf("%s is listed twice", p.URL)
		}
		urls[p.URL] = true
	}
	press(m, "ctrl+r")
	if got := src.prs.Load(); got != 2*int32(len(gitx.DemoRepos)) {
		t.Fatalf("a forced refresh should still load each GitHub repository once, got %d loads", got)
	}
}

func TestUnknownGitHubRepository(t *testing.T) {
	now := fakeClock(t)
	ok := &atomic.Bool{}
	o := demoOpts()
	o.Source, o.Profile = noSlug{ok: ok}, ProfileFull
	o.Repos = append(o.Repos, config.Repo{Name: "legacy-hotfix", Path: "/hotfix/legacy-api", Status: config.StatusOK})
	m := setup(t, o, 120, 30)
	clone, legacy := m.repos[len(m.repos)-1], m.repos[3]
	if clone.slug.err == nil || !clone.gh.prs.loaded {
		t.Fatal("a failed GitHub-name lookup should not hold back the folder's own GitHub loads")
	}
	if clone.gh == legacy.gh {
		t.Fatal("folders whose GitHub repository is unknown cannot share")
	}
	ok.Store(true) // the next lookup works, as after a one-off timeout
	*now = now.Add(maxBackoff)
	drain(m, m.ensure(false))
	if clone.gh != legacy.gh {
		t.Fatal("a failed lookup should retry after the backoff, and then share")
	}
	rows := m.connections()
	if row := rows[slices.IndexFunc(rows, func(c connRow) bool { return c.name == "GitHub (pull requests)" })]; row.state != "1 failed" {
		t.Fatalf("Connections should count a shared failed load once: %q", row.state)
	}
}
