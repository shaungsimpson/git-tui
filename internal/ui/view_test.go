package ui

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

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

	r := m.repos[0] // loaded earlier; now a refresh fails
	r.summary.err = context.DeadlineExceeded
	glyph := m.overviewSegs(r)[0]
	if glyph.st.Render(glyph.s) != m.st.bad.Render(glyph.s) || !strings.HasPrefix(glyph.s, "!") {
		t.Fatalf("a failed refresh should keep a red !, got %q", glyph.s)
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

func TestHelpListsEveryKey(t *testing.T) {
	for _, sz := range [][2]int{{60, 16}, {80, 24}, {140, 30}} {
		m := setup(t, demoOpts(), sz[0], sz[1])
		press(m, "g", "o", "?") // Overview offers few shortcuts, but help lists them all
		seen := ""
		for range 40 { // scroll to the end, keeping every page
			seen += ansi.Strip(m.render())
			press(m, "j")
		}
		if !strings.Contains(m.footer(), "close help") {
			t.Errorf("%dx%d: the footer should say how to close help", sz[0], sz[1])
		}
		if !strings.Contains(seen, "c Connections") {
			t.Errorf("%dx%d: every view jump should be listed, including the last", sz[0], sz[1])
		}
		for _, want := range append(groupNames, "UTC / local times", "split right/down", "move repository up/down", "preview pane", "refresh", "quit") {
			// "UTC / local times" and the longest descriptions seen whole mean no column was cut at the edge.
			if !strings.Contains(seen, want) {
				t.Errorf("%dx%d: help should list %q:\n%s", sz[0], sz[1], want, seen)
			}
		}
		if strings.Index(seen, "Everywhere") > strings.Index(seen, "Detail page") {
			t.Errorf("%dx%d: the keys that work everywhere come first", sz[0], sz[1])
		}
	}
	m := setup(t, demoOpts(), 60, 16)
	press(m, "?", "pgdown", "pgdown", "pgdown", "esc", "?")
	if m.helpOff != 0 {
		t.Fatal("help should open at the top")
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
			press(m, "v")
			check(viewNames[g.view] + " detail page")
			press(m, "esc")
		}
		press(m, "tab", "j", "j", "g", "r")
		check("repository")
		press(m, "?")
		check("help")
		press(m, "pgdown")
		check("help scrolled")
		press(m, "esc", "p")
		check("preview hidden")
		press(m, "p", "?")
		press(m, "esc", "f2")
		check("light")
		for _, mode := range []string{"full", "ascii", "text"} { // the badges in each header mode
			press(m, "b")
			check("badges in the " + mode + " header")
		}
		press(m, "b")
		press(m, "g", "c", "a", "enter")
		check("add repositories")
		press(m, "esc")
	}
}

func TestTooSmall(t *testing.T) {
	m := setup(t, demoOpts(), 50, 10)
	if !strings.Contains(m.render(), "too small") {
		t.Fatal("expected resize message")
	}
}

func TestMissingConfigMessage(t *testing.T) {
	m := setup(t, Options{Source: gitx.Demo{}, ConfigPath: "/x/config.json", ConfigErr: config.ErrNotFound}, 100, 30)
	if out := m.render(); !strings.Contains(out, "No repositories yet. Press a") {
		t.Fatalf("missing-config message not shown:\n%s", out)
	}
	press(m, "g", "c")
	if !strings.Contains(m.render(), "no file yet") {
		t.Fatal("connections should report that the configuration does not exist yet")
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
	in := "ok\x1b[31mred\x1b[0m\x1b]0;title\x07\x07bell\u009b\ttab\u202egnirts\u2066x\u2069"
	if got := clean(in); got != "okredbell tabgnirtsx" {
		t.Fatalf("clean = %q", got)
	}
}

func TestStackedRows(t *testing.T) {
	m := setup(t, demoOpts(), 99, 16) // the list is under stackedBelow
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

func TestVersionShown(t *testing.T) {
	o := demoOpts()
	o.Version, o.Build.Version, o.Executable = "v9.9.9", "v9.9.9", "/usr/local/bin/git-tui"
	m := setup(t, o, 120, 30)
	press(m, "g", "c")
	m.cursor[viewConnections] = len(m.connections()) - 1 // the git-tui row's details
	press(m, "v")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "git-tui") || !strings.Contains(out, "v9.9.9") {
		t.Fatalf("Connections should list the version:\n%s", out)
	}
	press(m, "esc", "?")
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
	o := demoOpts()
	o.Profile = ProfileFull // the badges sit in the banner, leaving the tab bar as it was
	m := setup(t, o, 200, 30)
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

	// With the header off the badges join the tab bar and outrank the age, the repository and the zone,
	// whichever of those fit; how many do depends on the time zone's name, so only the order is checked.
	m = setup(t, demoOpts(), 140, 30)
	if h := ansi.Strip(m.header()); !strings.Contains(h, "Connections") || !strings.Contains(h, "🔔 3") || !strings.Contains(h, "✔ 2") {
		t.Fatalf("at 140 columns the tabs and both badges stay: %q", h)
	}
	press(m, "g", "p")
	for _, w := range []int{80, 60} { // the tabs collapse to the current one, which the labels must not cut
		m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		if h := ansi.Strip(m.header()); !strings.Contains(h, "Pull requests") || !strings.Contains(h, "🔔 3") || !strings.Contains(h, "✔ 2") {
			t.Fatalf("at %d columns the current view and both badges stay: %q", w, h)
		}
	}
}

func TestFooterWrapsAndKeepsBasics(t *testing.T) {
	m := setup(t, demoOpts(), 100, 40)
	press(m, "g", "p")
	f := ansi.Strip(m.footer())
	if m.footerRows() != 2 || strings.Count(f, "\n") != 1 || !strings.Contains(f, "? help") || !strings.Contains(f, "open in browser") {
		t.Fatalf("a crowded footer should wrap to two lines with every shortcut:\n%s", f)
	}
	body := m.layout().bodyH
	press(m, "/")
	if m.layout().bodyH != body || strings.Count(m.footer(), "\n") != 1 {
		t.Fatal("typing a filter should keep the footer's two lines, so the panes do not jump")
	}
	press(m, "esc")
	o := demoOpts()
	o.Profile = ProfileFull
	m = setup(t, o, 120, 19) // the banner just fits above a one-line footer
	if m.profileShown() != ProfileFull || m.footerRows() != 1 {
		t.Fatalf("the banner should keep its rows before the footer takes a second line: %v, %d rows", m.profileShown(), m.footerRows())
	}
	o = demoOpts()
	o.Profile = ProfileText
	m = setup(t, o, 80, 16) // the header's one line leaves no room for a second footer line
	press(m, "g", "p")
	f = ansi.Strip(m.footer())
	if m.footerRows() != 1 || !strings.Contains(f, "? help") || !strings.Contains(f, "…") {
		t.Fatalf("a one-line footer should drop other shortcuts before help:\n%s", f)
	}
}

// BenchmarkManyRepos measures a frame, and a load result with its frame, for 30 repositories of 100 commits each.
// Work done per row, such as rebuilding every list to size the footer, shows up here first.
func BenchmarkManyRepos(b *testing.B) {
	o := Options{Source: gitx.Demo{}, Demo: true}
	for i := range 30 {
		name := "r" + strconv.Itoa(i)
		o.Repos = append(o.Repos, config.Repo{Name: name, Path: "/demo/" + name, Status: config.StatusOK})
	}
	m := New(o)
	m.width, m.height, m.view = 160, 50, viewActivity
	now := time.Now()
	for i, r := range m.repos {
		var cs []gitx.Commit
		for j := range logDepth {
			cs = append(cs, gitx.Commit{Repo: r.cfg.Name, Hash: strconv.Itoa(i*1000 + j), Subject: "subject", Author: "me", When: now.Add(-time.Duration(j) * time.Hour)})
		}
		r.log = slot[[]gitx.Commit]{val: cs, loaded: true, at: now}
	}
	b.Run("frame", func(b *testing.B) {
		for b.Loop() {
			_ = m.render()
		}
	})
	b.Run("result", func(b *testing.B) {
		for b.Loop() {
			m.Update(resultMsg{kind: kindSummary, gen: -1}) // a superseded result: only the bookkeeping and clamping run
			_ = m.render()
		}
	})
}
