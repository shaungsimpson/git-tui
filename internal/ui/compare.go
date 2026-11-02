package ui

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/gitx"
)

// compareState is a commit list between two revisions, opened from Tags (pick a base tag, then a head tag)
// or from Environments (a deployed commit against origin/HEAD). view says which one owns it.
type compareState struct {
	view             viewID
	repo             int // 1-based, like Model.repo
	base, head       string
	baseRev, headRev string
	title, empty     string
	open             bool
	res              slot[[]gitx.Commit]
	cur, off         int
}

// diffState is the full-width view of one repository's uncommitted changes.
type diffState struct {
	open  bool
	repo  int // indexes Model.repos
	title string
	res   slot[[]string]
	off   int
}

// diffTarget is the repository whose changes d shows, or -1 when nothing with uncommitted changes is selected.
func (m *Model) diffTarget() int {
	i := m.selectedRepo()
	if i < 0 || !m.repos[i].summary.loaded || m.repos[i].summary.val.Clean() {
		return -1
	}
	return i
}

func (m *Model) canDiff() bool {
	_, pr := m.selectedPR()
	return pr || m.diffTarget() >= 0
}

func (m *Model) openDiff(repo int, title string, fetch func(context.Context) ([]string, error)) tea.Cmd {
	m.diff = diffState{open: true, repo: repo, title: title, res: slot[[]string]{gen: m.diff.res.gen + 1}}
	return start(m, &m.diff.res, true, kindDiff, repo, fetch)
}

func (m *Model) closeDiff() {
	if m.diff.res.cancel != nil {
		m.diff.res.cancel()
	}
	m.diff = diffState{res: slot[[]string]{gen: m.diff.res.gen + 1}} // a late result is dropped
}

func (m *Model) scrollDiff(d int) {
	last := max(0, len(m.diff.res.val)-(m.layout().bodyH-1))
	m.diff.off = min(max(0, m.diff.off+d), last)
}

func (m *Model) cancelCompare() {
	if m.cmp.res.cancel != nil {
		m.cmp.res.cancel()
	}
}

// resetCompare drops the commit list and any load of it, leaving a fresh state owned by view v. The new
// generation drops a result that arrives late.
func (m *Model) resetCompare(v viewID) {
	m.cancelCompare()
	m.cmp = compareState{view: v, res: slot[[]gitx.Commit]{gen: m.cmp.res.gen + 1}}
}

func (m *Model) pickTag(name string) tea.Cmd {
	c := &m.cmp
	if c.view != viewTags { // state left by Environments; start a fresh tag selection
		m.resetCompare(viewTags)
	}
	if c.base == "" {
		c.base, c.repo = name, m.repo
		m.flash = "Base " + name + " selected. Choose another tag and press Enter."
		return nil
	}
	if c.base == name {
		c.base = ""
		return nil
	}
	tags := m.selected().tags.val
	idx := func(n string) int { return slices.IndexFunc(tags, func(t gitx.Tag) bool { return t.Name == n }) }
	base, head := c.base, name
	if idx(base) < idx(head) { // tags are newest first, so the base is the one further down
		base, head = head, base
	}
	c.view, c.base, c.head, c.open, c.cur, c.off = viewTags, base, head, true, 0, 0
	c.baseRev, c.headRev = "refs/tags/"+base, "refs/tags/"+head
	c.title = "Commits between " + clean(base) + " and " + clean(head)
	c.empty = "No commits between " + clean(base) + " and " + clean(head) + "."
	return m.loadCompare()
}

// openPending lists commits on origin/HEAD that the environment's deployed commit does not contain.
func (m *Model) openPending(e envRow) tea.Cmd {
	if !gitx.IsCommitID(e.Commit) { // also guards the git revision: only a plain object ID reaches git
		m.flash = "No successful deployment is recorded for " + clean(e.Environment) + "."
		return nil
	}
	m.cancelCompare()
	env := clean(e.Repo) + " " + clean(e.Environment)
	m.cmp = compareState{
		view: viewEnvs, repo: e.repo + 1, open: true,
		base: e.Environment, baseRev: e.Commit, headRev: "origin/HEAD",
		title: "Not yet in " + env + " · on origin/HEAD after " + e.Commit[:7],
		empty: "Nothing to release: " + env + " already has everything on origin/HEAD (as of your last fetch).",
		res:   slot[[]gitx.Commit]{gen: m.cmp.res.gen + 1},
	}
	return m.loadCompare()
}

// cmpOpen reports whether the current view is showing its commit list.
func (m *Model) cmpOpen() bool { return m.cmp.open && m.cmp.view == m.view }

// followDeploy reopens an open "not yet deployed" list when reloaded deployments show a newer deployed commit.
func (m *Model) followDeploy(repo int) tea.Cmd {
	if !m.cmp.open || m.cmp.view != viewEnvs || m.cmp.repo != repo+1 {
		return nil
	}
	if repo >= len(m.repos) {
		return nil
	}
	for _, d := range m.repos[repo].gh.deps.val {
		if d.Environment == m.cmp.base && d.Commit != m.cmp.baseRev && gitx.IsCommitID(d.Commit) {
			return m.openPending(envRow{repo, d})
		}
	}
	return nil
}
