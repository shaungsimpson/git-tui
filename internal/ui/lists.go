package ui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/shaungsimpson/git-tui/internal/gitx"
)

// envRow is one environment of one repository; repo indexes Model.repos.
type envRow struct {
	repo int
	gitx.Deployment
}

// selected returns the chosen repository, or nil when "All repositories" is selected.
func (m *Model) selected() *repoState {
	if m.repo == 0 || m.repo > len(m.repos) {
		return nil
	}
	return m.repos[m.repo-1]
}

// scope is the repositories the current repository selection covers.
func (m *Model) scope() []int {
	if m.repo > 0 {
		return []int{m.repo - 1}
	}
	var all []int
	for i := range m.repos {
		all = append(all, i)
	}
	return all
}

// ghScope is the selected repositories with each GitHub repository once: the first folder cloned from it
// stands for the others.
func (m *Model) ghScope() []int {
	seen := map[*ghState]bool{}
	var out []int
	for _, i := range m.scope() {
		if g := m.repos[i].gh; !seen[g] {
			seen[g] = true
			out = append(out, i)
		}
	}
	return out
}

// selectedRun is the deploy run under the cursor in Deploys.
func (m *Model) selectedRun() (gitx.Run, bool) {
	if n, cur, _ := m.list(); m.view == viewRuns && !m.cmpOpen() && *cur < n {
		return m.runList()[*cur], true
	}
	return gitx.Run{}, false
}

func (m *Model) list() (int, *int, *int) { return m.listFor(m.view) }

func (m *Model) listFor(v viewID) (int, *int, *int) {
	cur, off := &m.cursor[v], &m.offset[v]
	switch v {
	case viewOverview:
		return len(m.overviewRows()), cur, off
	case viewActivity:
		return len(m.activity()), cur, off
	case viewPRs:
		return len(m.prList()), cur, off
	case viewRuns:
		return len(m.runList()), cur, off
	case viewRepo:
		return len(m.repoCommits()), cur, off
	case viewTags, viewEnvs:
		if m.cmp.open && m.cmp.view == v {
			return len(m.cmp.res.val), &m.cmp.cur, &m.cmp.off
		}
		if v == viewEnvs {
			return len(m.envList()), cur, off
		}
		return len(m.tagList()), cur, off
	default:
		return len(m.connections()), cur, off
	}
}

func match(filter string, fields ...string) bool {
	if filter == "" {
		return true
	}
	filter = strings.ToLower(filter)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), filter) {
			return true
		}
	}
	return false
}

// sortsOf is how each sortable list can be ordered; the first is its default, newest first.
var sortsOf = map[viewID][]string{
	viewActivity: {"date", "repository", "author"},
	viewPRs:      {"updated", "status", "repository", "author"},
	viewRepo:     {"date", "author"},
	viewEnvs:     {"deployed", "repository", "environment"},
	viewRuns:     {"started", "status", "repository", "workflow"},
}

// sortKeys name the sortable views in the preferences file, so renaming a view keeps its saved sort.
var sortKeys = map[viewID]string{viewActivity: "activity", viewPRs: "prs", viewRepo: "repo", viewEnvs: "envs", viewRuns: "runs"}

// sortName is the current sort of a view, or "" for a view that does not sort.
func (m *Model) sortName(v viewID) string {
	if names := sortsOf[v]; len(names) > 0 {
		return names[m.sorts[v]]
	}
	return ""
}

// sortBy reorders a newest-first list by one key. The sort is stable, so each group stays newest first.
func sortBy[T any, K cmp.Ordered](s []T, key func(T) K) {
	slices.SortStableFunc(s, func(a, b T) int { return cmp.Compare(key(a), key(b)) })
}

// prRank orders pull requests by what needs you: your review, then yours ready to merge, then by state; drafts last.
func (m *Model) prRank(p gitx.PR) int {
	switch {
	case p.Draft:
		return 7
	case p.ForMe:
		return 0
	case m.ready(p):
		return 1
	case p.Checks == gitx.ChecksFailing:
		return 2
	case p.Review == "CHANGES_REQUESTED":
		return 3
	case p.Review == "REVIEW_REQUIRED":
		return 4
	case p.Review == "APPROVED":
		return 5
	}
	return 6
}

// runRank orders deploy runs: running, then stopped (failed or cancelled), then succeeded, then the rest.
func runRank(r gitx.Run) int {
	switch {
	case r.Active():
		return 0
	case r.Stopped():
		return 1
	case r.Conclusion == "success":
		return 2
	}
	return 3
}

func (m *Model) overviewRows() []int {
	var rows []int
	for i, r := range m.repos {
		if match(m.filters[viewOverview], r.cfg.Name, r.summary.val.Branch) {
			rows = append(rows, i)
		}
	}
	return rows
}

// commitsMatching is a view's commits that pass its filter, in its sort; cs is never reordered.
func (m *Model) commitsMatching(v viewID, cs []gitx.Commit) []gitx.Commit {
	var out []gitx.Commit
	for _, c := range cs {
		if match(m.filters[v], c.Subject, c.Author, c.Repo, c.Hash) {
			out = append(out, c)
		}
	}
	switch m.sortName(v) {
	case "repository":
		sortBy(out, func(c gitx.Commit) string { return strings.ToLower(c.Repo) })
	case "author":
		sortBy(out, func(c gitx.Commit) string { return strings.ToLower(c.Author) })
	}
	return out
}

func (m *Model) activity() []gitx.Commit {
	var all []gitx.Commit
	for _, i := range m.scope() {
		all = append(all, m.repos[i].log.val...)
	}
	slices.SortStableFunc(all, func(a, b gitx.Commit) int { return b.When.Compare(a.When) })
	return m.commitsMatching(viewActivity, all)
}

// prList is the open pull requests of the selected repositories, most recently updated first.
func (m *Model) prList() []gitx.PR {
	f := m.filters[viewPRs]
	keep := func(p gitx.PR) bool {
		switch strings.ToLower(strings.TrimSpace(f)) { // the keywords n sets; they work typed after / too
		case "is:review":
			return p.ForMe
		case "is:ready":
			return m.ready(p)
		}
		return match(f, p.Title, p.Author, p.Branch, p.Repo, "#"+strconv.Itoa(p.Number))
	}
	var out []gitx.PR
	for _, i := range m.ghScope() {
		for _, p := range m.repos[i].gh.prs.val {
			p.Repo = m.repos[i].cfg.Name // the folder's current name, whichever clone loaded it
			if keep(p) {
				out = append(out, p)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b gitx.PR) int { return b.Updated.Compare(a.Updated) })
	switch m.sortName(viewPRs) {
	case "status":
		sortBy(out, m.prRank)
	case "repository":
		sortBy(out, func(p gitx.PR) string { return strings.ToLower(p.Repo) })
	case "author":
		sortBy(out, func(p gitx.PR) string { return strings.ToLower(p.Author) })
	}
	return out
}

// envList is every recorded environment of the selected repositories, most recently deployed first.
func (m *Model) envList() []envRow {
	var out []envRow
	for _, i := range m.ghScope() {
		for _, d := range m.repos[i].gh.deps.val {
			d.Repo = m.repos[i].cfg.Name
			if match(m.filters[viewEnvs], d.Environment, d.Repo, d.Ref, d.Commit) {
				out = append(out, envRow{i, d})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b envRow) int { return b.When.Compare(a.When) })
	switch m.sortName(viewEnvs) {
	case "repository":
		sortBy(out, func(e envRow) string { return strings.ToLower(e.Repo) })
	case "environment":
		sortBy(out, func(e envRow) string { return strings.ToLower(e.Environment) })
	}
	return out
}

// runList is the deploy runs of the selected repositories, most recently started first.
func (m *Model) runList() []gitx.Run {
	var out []gitx.Run
	for _, i := range m.ghScope() {
		for _, r := range m.repos[i].gh.runs.val {
			r.Repo = m.repos[i].cfg.Name
			if match(m.filters[viewRuns], r.Workflow, r.Title, r.Branch, r.Repo, r.Status, r.Conclusion) {
				out = append(out, r)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b gitx.Run) int { return b.Started.Compare(a.Started) })
	switch m.sortName(viewRuns) {
	case "status":
		sortBy(out, runRank)
	case "repository":
		sortBy(out, func(r gitx.Run) string { return strings.ToLower(r.Repo) })
	case "workflow":
		sortBy(out, func(r gitx.Run) string { return strings.ToLower(r.Workflow) })
	}
	return out
}

func (m *Model) repoCommits() []gitx.Commit {
	r := m.selected()
	if r == nil {
		return nil
	}
	return m.commitsMatching(viewRepo, r.log.val)
}

func (m *Model) tagList() []gitx.Tag {
	r := m.selected()
	if r == nil {
		return nil
	}
	var out []gitx.Tag
	for _, t := range r.tags.val {
		if match(m.filters[viewTags], t.Name, t.Subject) {
			out = append(out, t)
		}
	}
	return out
}

// rowKeys identifies each row of the lists that merge or reorder on reload, so the selection can follow its row.
func (m *Model) rowKeys(v viewID) []string {
	var keys []string
	switch v {
	case viewActivity:
		for _, c := range m.activity() {
			keys = append(keys, c.Repo+"/"+c.Hash)
		}
	case viewRepo:
		for _, c := range m.repoCommits() {
			keys = append(keys, c.Repo+"/"+c.Hash)
		}
	case viewEnvs: // the open commit list keeps its own cursor, so this one always tracks environment rows
		for _, e := range m.envList() {
			keys = append(keys, e.Repo+"/"+e.Environment)
		}
	case viewRuns:
		for _, r := range m.runList() {
			keys = append(keys, r.Repo+"/"+strconv.FormatInt(r.ID, 10))
		}
	case viewPRs:
		for _, p := range m.prList() {
			keys = append(keys, p.Repo+"#"+strconv.Itoa(p.Number))
		}
	}
	return keys
}
