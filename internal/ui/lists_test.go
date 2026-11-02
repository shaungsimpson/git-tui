package ui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/gitx"
)

func TestPullRequestsViewAndConnection(t *testing.T) {
	m := New(demoOpts()) // not started, so no PRs have loaded yet
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	knowSlugs(m)
	github := func() connRow {
		rows := m.connections()
		return rows[slices.IndexFunc(rows, func(c connRow) bool { return c.name == "GitHub (pull requests)" })]
	}
	if github().state != "not checked yet" {
		t.Fatalf("before loading: %q", github().state)
	}
	m.view = viewPRs
	first, second := m.load(0, kindPRs, true), m.load(1, kindPRs, true)
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
	drain(m, m.load(3, kindPRs, true)) // legacy-api fixture fails
	if github().level != levelWarn || !strings.Contains(m.render(), "Pull requests") {
		t.Fatalf("failure not reported: %+v", github())
	}
	press(m, "/", "#", "4", "7")
	got := m.prList()
	if len(got) == 0 || len(got) == len(m.repos[0].gh.prs.val)+len(m.repos[1].gh.prs.val) {
		t.Fatalf("filter #47 kept %d rows", len(got))
	}
	for _, p := range got {
		if !strings.HasPrefix("#"+strconv.Itoa(p.Number), "#47") {
			t.Fatalf("filter let through #%d", p.Number)
		}
	}
}

func TestSortCyclesEachListAndKeepsTheSelection(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	// For each sort, the key every row must be in order by; rows equal on it must stay newest first.
	type check struct {
		key  func(i int) string
		when func(i int) int64
	}
	checks := map[viewID]map[string]check{
		viewActivity: {
			"repository": {func(i int) string { return strings.ToLower(m.activity()[i].Repo) }, func(i int) int64 { return m.activity()[i].When.Unix() }},
			"author":     {func(i int) string { return strings.ToLower(m.activity()[i].Author) }, func(i int) int64 { return m.activity()[i].When.Unix() }},
		},
		viewPRs: {
			"status":     {func(i int) string { return strconv.Itoa(m.prRank(m.prList()[i])) }, func(i int) int64 { return m.prList()[i].Updated.Unix() }},
			"repository": {func(i int) string { return strings.ToLower(m.prList()[i].Repo) }, func(i int) int64 { return m.prList()[i].Updated.Unix() }},
			"author":     {func(i int) string { return strings.ToLower(m.prList()[i].Author) }, func(i int) int64 { return m.prList()[i].Updated.Unix() }},
		},
		viewEnvs: {
			"repository":  {func(i int) string { return strings.ToLower(m.envList()[i].Repo) }, func(i int) int64 { return m.envList()[i].When.Unix() }},
			"environment": {func(i int) string { return strings.ToLower(m.envList()[i].Environment) }, func(i int) int64 { return m.envList()[i].When.Unix() }},
		},
		viewRuns: {
			"status":     {func(i int) string { return strconv.Itoa(runRank(m.runList()[i])) }, func(i int) int64 { return m.runList()[i].Started.Unix() }},
			"repository": {func(i int) string { return strings.ToLower(m.runList()[i].Repo) }, func(i int) int64 { return m.runList()[i].Started.Unix() }},
			"workflow":   {func(i int) string { return strings.ToLower(m.runList()[i].Workflow) }, func(i int) int64 { return m.runList()[i].Started.Unix() }},
		},
	}
	for v, byName := range checks {
		drain(m, m.setView(v))
		n, cur, _ := m.list()
		if n < 3 {
			t.Fatalf("%s: demo needs rows to sort, has %d", viewNames[v], n)
		}
		*cur = 2
		for range len(sortsOf[v]) - 1 {
			want := m.rowKeys(v)[*cur]
			press(m, "s")
			name := m.sortName(v)
			if got := m.rowKeys(v)[*cur]; got != want {
				t.Fatalf("%s by %s: selection moved from %s to %s", viewNames[v], name, want, got)
			}
			c := byName[name]
			for i := 1; i < n; i++ {
				if a, b := c.key(i-1), c.key(i); a > b || a == b && c.when(i-1) < c.when(i) {
					t.Fatalf("%s by %s: rows %d and %d out of order (%q, %q)", viewNames[v], name, i-1, i, a, b)
				}
			}
			if title, _ := m.mainContent(140); !strings.Contains(title, "by "+name) || m.flash != "Sorted by "+name+"." {
				t.Fatalf("%s: title %q, flash %q", viewNames[v], title, m.flash)
			}
		}
		press(m, "s") // back to the default
		if title, _ := m.mainContent(140); m.sorts[v] != 0 || strings.Contains(title, " by ") {
			t.Fatalf("%s: s should cycle back to newest first, title %q", viewNames[v], title)
		}
	}
	m.setView(viewOverview)
	if m.canSort() {
		t.Fatal("Overview keeps its configured order")
	}
}

func TestPRStatusRank(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	m.me.val.Login = "me"
	prs := []gitx.PR{
		{Number: 1, Draft: true, ForMe: true},
		{Number: 2, Review: "APPROVED"},
		{Number: 3, Review: "REVIEW_REQUIRED"},
		{Number: 4, Review: "CHANGES_REQUESTED"},
		{Number: 5, Checks: gitx.ChecksFailing, Review: "APPROVED"},
		{Number: 6, Author: "me", Review: "APPROVED", Checks: gitx.ChecksPassing},
		{Number: 7, ForMe: true},
		{Number: 8},
	}
	sortBy(prs, m.prRank)
	var got []int
	for _, p := range prs {
		got = append(got, p.Number)
	}
	if want := []int{7, 6, 5, 4, 3, 2, 8, 1}; !slices.Equal(got, want) {
		t.Fatalf("status order %v, want %v (a draft is last even when it asks for your review)", got, want)
	}
}

func TestTabsUnderlineTheirGoLetter(t *testing.T) {
	for _, g := range goKeys {
		if i := goLetter(g.view); i < 0 || strings.ToLower(viewNames[g.view][i:i+1]) != g.key {
			t.Errorf("%s: g %s, underlined position %d", viewNames[g.view], g.key, i)
		}
	}
}
