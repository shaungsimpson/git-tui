package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/shaungsimpson/git-tui/internal/gitx"
)

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
	r := m.repos[envs[0].repo]
	moved := slices.Clone(r.gh.deps.val)
	for i := range moved {
		if moved[i].Environment == envs[0].Environment {
			moved[i].Commit = strings.Repeat("f", 40)
		}
	}
	if _, cmd := m.Update(answer(&r.gh.deps, kindDeps, envs[0].repo, moved, nil)); cmd == nil || m.cmp.baseRev != strings.Repeat("f", 40) {
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
	// A short or abbreviated hash is not a commit ID: it must not open, or reach git or the title's [:7].
	if cmd := m.openPending(envRow{0, gitx.Deployment{Repo: "backend", Environment: "short", Commit: "abc"}}); cmd != nil || m.cmp.open {
		t.Fatal("an environment with a short hash must not open")
	}
}

func TestDiffView(t *testing.T) {
	m := setup(t, demoOpts(), 140, 30)
	if m.canDiff() {
		t.Fatal("backend is clean, so d should not be offered")
	}
	press(m, "j")                                              // frontend has changes in the demo data
	if !m.canDiff() || !strings.Contains(m.render(), "diff") { // in the preview pane
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
	m.Update(resultMsg{kind: kindDiff, repo: m.diff.repo, gen: gen, val: []string{"late"}, store: func(msg resultMsg) { finish(&m.diff.res, msg) }})
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
	press(m, "end")
	if last := 40 - (m.layout().bodyH - 1); m.diff.off != last {
		t.Fatalf("end should reach the bottom: off %d, want %d", m.diff.off, last)
	}
	press(m, "home")
	if m.diff.off != 0 {
		t.Fatalf("home should reach the top: off %d", m.diff.off)
	}
}

func TestTagComparison(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "tab", "j", "tab", "g", "t")
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
