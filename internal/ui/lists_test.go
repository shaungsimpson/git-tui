package ui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
