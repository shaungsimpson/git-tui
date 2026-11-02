package ui

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

func TestKeyStringsMatchBindings(t *testing.T) {
	for _, s := range []string{"tab", "shift+tab", "enter", "esc", "f2", "ctrl+r", "ctrl+t", "T", "j", "?", "/", "pgdown", "pgup"} {
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
	press(m, "tab", "j")
	if m.focus != paneSidebar || m.repo != 1 {
		t.Fatalf("sidebar j should select first repo: focus %v repo %d", m.focus, m.repo)
	}
	press(m, "shift+tab")
	if m.focus != paneMain {
		t.Fatalf("shift+tab wraps backwards, got %v", m.focus)
	}
	// Enter on an overview row opens the Repository view for it.
	press(m, "enter")
	if m.view != viewRepo || m.repo != 2 {
		t.Fatalf("enter: view %v repo %d", m.view, m.repo)
	}
}

func TestDetailPage(t *testing.T) {
	for _, w := range []int{80, 140} {
		m := setup(t, demoOpts(), w, 24)
		press(m, "g", "a", "enter")
		if m.inspect {
			t.Fatalf("%d: enter has no job in Activity; v opens the page", w)
		}
		press(m, "v")
		out := ansi.Strip(m.render())
		if !m.inspect || m.focus != paneDetail || !strings.Contains(out, "Details · Activity · 1 of") || strings.Contains(out, "Repositories") {
			t.Fatalf("%d: v should open the page across the full width:\n%s", w, out)
		}
		press(m, "tab")
		if m.focus != paneDetail {
			t.Fatalf("%d: tab must not leave the page for a hidden pane", w)
		}
		press(m, "J", "J", "K")
		if m.cursor[viewActivity] != 1 || m.detOff != 0 || !strings.Contains(ansi.Strip(m.render()), "2 of") {
			t.Fatalf("%d: J/K should step through the list behind the page: cursor %d", w, m.cursor[viewActivity])
		}
		press(m, "esc")
		if m.inspect || m.focus != paneMain || m.cursor[viewActivity] != 1 {
			t.Fatalf("%d: esc should restore the list on the last item viewed", w)
		}
	}
}

func TestDetailPageKeepsItsItemOnRefresh(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "p", "v")
	p := m.prList()[0]
	i := slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == p.Repo })
	newer := gitx.PR{Repo: p.Repo, Number: 9999, Title: "Newer", Updated: p.Updated.Add(time.Hour)}
	m.Update(answer(&m.repos[i].gh.prs, kindPRs, i, append([]gitx.PR{newer}, m.repos[i].gh.prs.val...), nil))
	if got := m.prList()[m.cursor[viewPRs]]; got.Number != p.Number {
		t.Fatalf("a refresh swapped the open page from #%d to #%d", p.Number, got.Number)
	}
}

func TestDetailPageEmptiedList(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "d", "v")
	for _, r := range m.repos { // a refresh that finds no runs
		if r.gh != nil {
			r.gh.runs.val = nil
		}
	}
	press(m, "J", "K")
	if m.cursor[viewRuns] < 0 {
		t.Fatalf("J/K on an emptied list moved the cursor to %d", m.cursor[viewRuns])
	}
	_ = m.render()
}

func TestPreviewPane(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "p")
	out := ansi.Strip(m.render())
	for _, want := range []string{"Preview", "Review", "Shortcuts", "view details", "open in browser"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the preview should show %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Description") || strings.Contains(out, "Files changed") || strings.Contains(out, "Link") {
		t.Fatal("the preview keeps to the short head; long sections are for the page")
	}
	press(m, "g", "o", "tab")
	if out := ansi.Strip(m.render()); m.focus != paneSidebar || strings.Contains(out, "new tab") || strings.Contains(out, "Shortcuts") {
		t.Fatal("from the sidebar, t acts on its repository, so the preview of a list row must not offer it")
	}
	press(m, "tab")
	if m.focus != paneMain {
		t.Fatalf("the preview never takes focus: tab twice should return to the list, got %v", m.focus)
	}
	m.Update(tea.WindowSizeMsg{Width: 119, Height: 40})
	if strings.Contains(ansi.Strip(m.render()), "Preview") {
		t.Fatal("the preview gives way under 120 columns")
	}
}

func TestPreviewKeepsRowShortcuts(t *testing.T) {
	for _, h := range []int{16, 18, 40} { // short windows cut the preview's head, never its shortcuts
		for _, g := range goKeys {
			m := setup(t, demoOpts(), 150, h)
			press(m, "g", g.key)
			out := ansi.Strip(m.render())
			for _, a := range actions {
				if d := a.key.Help().Desc; a.item && a.available(m) && !strings.Contains(out, d) {
					t.Errorf("150x%d %s: %q is in neither the preview nor the footer", h, viewNames[g.view], d)
				}
			}
			rows := m.footerRows()
			if press(m, "tab"); m.footerRows() != rows {
				t.Errorf("150x%d %s: tabbing to the sidebar should keep the footer's height", h, viewNames[g.view])
			}
		}
	}
}

func TestStackedRowsKeepTheirEnd(t *testing.T) {
	m := setup(t, demoOpts(), 150, 40) // the preview leaves the list 74 columns
	for _, c := range []struct{ keys, want string }{
		{"o", "Remove v1 API, for real this time"}, // Overview: the last commit's subject
		{"e", "last attempt failure"},              // Environments: why a deployment needs a look
	} {
		press(m, "g", c.keys)
		if out := ansi.Strip(m.render()); !strings.Contains(out, c.want) {
			t.Errorf("%s should keep %q on a second line:\n%s", viewNames[m.view], c.want, out)
		}
	}
	press(m, "tab", "j", "tab", "g", "t")
	if !m.stacked(viewTags) || !strings.Contains(ansi.Strip(m.render()), "Release v1.9") {
		t.Error("Tags should put each tag's subject on a second line")
	}
	if m.stacked(viewConnections) {
		t.Error("Connections rows fit on one line")
	}
}

func TestTogglePreview(t *testing.T) {
	o := demoOpts()
	o.StatePath = t.TempDir() + "/state.json"
	m := setup(t, o, 150, 40) // a 14-inch laptop: the preview leaves the list too narrow for one-line rows
	press(m, "g", "p")
	if !m.stacked(viewPRs) || strings.Contains(ansi.Strip(m.footer()), "open in browser") {
		t.Fatal("with the preview shown, rows take two lines and the footer leaves its shortcuts to the preview")
	}
	press(m, "p")
	if out := ansi.Strip(m.render()); strings.Contains(out, "Preview") || m.stacked(viewPRs) {
		t.Fatal("p should hide the preview and give the list one-line rows")
	}
	if !strings.Contains(ansi.Strip(m.footer()), "open in browser") {
		t.Fatal("with the preview hidden, the footer offers the row's shortcuts again")
	}
	if got, _ := config.LoadState(o.StatePath); !got.NoPreview {
		t.Fatal("hiding the preview should be remembered")
	}
	o.NoPreview = true
	if m = setup(t, o, 150, 40); strings.Contains(ansi.Strip(m.render()), "Preview") {
		t.Fatal("a saved hidden preview should stay hidden on the next run")
	}
	press(m, "p")
	if got, _ := config.LoadState(o.StatePath); got.NoPreview || !strings.Contains(ansi.Strip(m.render()), "Preview") {
		t.Fatal("p should bring the preview back and remember it")
	}
	m.Update(tea.WindowSizeMsg{Width: 119, Height: 40})
	if strings.Contains(ansi.Strip(m.footer()), "preview pane") {
		t.Fatal("under 120 columns there is no preview to toggle")
	}
}

func TestDetailPageEnterKeepsItsJobs(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "v")
	if !m.inspect || !strings.Contains(ansi.Strip(m.render()), "Details · Overview") {
		t.Fatal("v should open an Overview row's page")
	}
	press(m, "esc", "enter")
	if m.view != viewRepo || m.inspect {
		t.Fatal("enter on Overview still opens the Repository view")
	}
	press(m, "g", "t", "v")
	if !m.inspect {
		t.Fatal("v should open a tag's page")
	}
	press(m, "esc", "enter")
	if m.inspect || m.tagBase() == "" {
		t.Fatal("enter on Tags still picks the base")
	}
}

func TestDetailPageScrollsAndStacksDiff(t *testing.T) {
	m := setup(t, demoOpts(), 140, 16)
	press(m, "g", "p")
	m.cursor[viewPRs] = slices.IndexFunc(m.prList(), func(p gitx.PR) bool { return len(p.Files) > 0 })
	press(m, "v")
	end := m.maxDetailOff()
	if end == 0 {
		t.Fatal("setup: the PR page should need scrolling at this height")
	}
	press(m, "pgdown", "space", "pgdown", "pgdown", "pgdown")
	if m.detOff != end {
		t.Fatalf("paging stops at the end: off %d, want %d", m.detOff, end)
	}
	press(m, "pgup", "pgup", "pgup", "pgup", "pgup")
	if m.detOff != 0 {
		t.Fatalf("paging stops at the top: off %d", m.detOff)
	}
	press(m, "d")
	if !m.diff.open || !m.inspect {
		t.Fatal("d on the PR page should open its diff above the page")
	}
	press(m, "esc")
	if m.diff.open || !m.inspect {
		t.Fatal("esc on the diff should return to the PR page")
	}
	press(m, "esc")
	if m.inspect {
		t.Fatal("esc on the page should return to the list")
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

func TestDetailPageSurvivesResize(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "v")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.inspect || !slices.Contains(m.panes(), m.focus) {
		t.Fatalf("focus %v is not a visible pane", m.focus)
	}
	press(m, "esc")
	if m.inspect || m.focus != paneMain {
		t.Fatal("esc should return to the list")
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

func TestTogglesAreRemembered(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	o.StatePath = t.TempDir() + "/state.json"
	m := setup(t, o, 120, 30)
	press(m, "u", "b", "g", "p", "s")
	got, err := config.LoadState(o.StatePath)
	if want := (config.State{UTC: true, Profile: "ascii", Sorts: map[string]string{"prs": "status"}}); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("saved %+v, %v", got, err)
	}
	o.UTC, o.Profile, o.Sorts = got.UTC, ProfileASCII, got.Sorts
	if m := setup(t, o, 120, 30); !m.utc || m.profile != ProfileASCII || m.sortName(viewPRs) != "status" {
		t.Fatal("a new run starts from the saved toggles and sorts")
	}
	o.Sorts = map[string]string{"prs": "no-such-sort"}
	if m := setup(t, o, 120, 30); m.sortName(viewPRs) != "updated" {
		t.Fatal("an unknown saved sort keeps the default")
	}

	// A run started with -profile off must not save that override when another toggle changes.
	o.Profile = ProfileOff
	m = setup(t, o, 120, 30)
	press(m, "u")
	if got, _ := config.LoadState(o.StatePath); got.UTC || got.Profile != "ascii" {
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
	if !strings.Contains(ansi.Strip(m.footer()), "n reviews / ready to merge") {
		t.Fatal("demo footer should say how to reach the review and ready badges")
	}

	press(m, "g", "p")
	if !strings.Contains(ansi.Strip(m.render()), "Preview") {
		t.Fatal("demo should show the preview pane beside the list at 140 columns")
	}
	press(m, "p")
	if strings.Contains(ansi.Strip(m.render()), "Preview") {
		t.Fatal("demo should hide the preview pane with p")
	}
	press(m, "p")
	colours := map[any]bool{}
	for _, p := range m.prList() {
		colours[m.authorStyle(p.Author, true).GetForeground()] = true
	}
	if len(colours) < 3 {
		t.Fatalf("demo authors should show several author colours, got %d", len(colours))
	}
	press(m, "v")                 // pictures load once the page is open
	for url, st := range m.pics { // a fetch drain gave up on under -race; finish it here, through the same demo source
		if st.loading {
			pic, err := fetchPic(context.Background(), m.opts.Source, url, picBase+99)
			m.Update(picMsg{url, pic, err})
		}
	}
	if page := m.render(); !strings.ContainsRune(page, kitty.Placeholder) || !strings.Contains(ansi.Strip(page), "Details · Pull requests") {
		t.Fatal("demo PRs should draw their pictures on the detail page")
	}
	press(m, "J", "d")
	if m.cursor[viewPRs] != 1 || !m.diff.open {
		t.Fatalf("on the page, J should step to the next PR and d open its diff: cursor %d", m.cursor[viewPRs])
	}
	press(m, "esc", "K", "esc")
	press(m, "g", "d")
	m.cursor[viewRuns] = slices.IndexFunc(m.runList(), func(r gitx.Run) bool { return r.Conclusion == "cancelled" })
	drain(m, m.ensure(false))
	if s := m.why[m.runList()[m.cursor[viewRuns]].ID]; s != nil && (s.loaded || s.loading) {
		t.Fatal("a run's reasons load only for the detail page")
	}
	press(m, "v")
	drain(m, m.ensure(false))
	if d := ansi.Strip(strings.Join(m.detailLines(), "\n")); !strings.Contains(d, "Why it stopped") || !strings.Contains(d, "higher priority") {
		t.Fatalf("demo should show why a deploy run was cancelled:\n%s", d)
	}
	press(m, "g", "p", "s")
	if title, _ := m.mainContent(140); !strings.Contains(title, "by status") || !m.prList()[0].ForMe {
		t.Fatalf("demo PRs by status should start with a review request: %q", title)
	}
	m.sorts[viewPRs] = 0
	press(m, "g", "o", "t")
	if !strings.Contains(m.flash, "Demo: would open a tab in") {
		t.Fatalf("demo should show what t does: %q", m.flash)
	}
	// Repositories: add from the demo folders (one already configured, one whose name clashes), then find the
	// missing infra repository in the folder it moved to.
	press(m, "g", "c", "a", "enter")
	if len(m.scan.res.val) != len(gitx.DemoFolders) || !m.configured(m.scan.res.val[0].path) {
		t.Fatalf("demo should find its folders, one already configured: %+v", m.scan.res.val)
	}
	press(m, "j", "space", "j", "j", "space", "enter")
	if m.repos[len(m.repos)-1].cfg.Name != "client-x/backend" {
		t.Fatalf("demo should show a clashing name prefixed, got %q", m.repos[len(m.repos)-1].cfg.Name)
	}
	m.cursor[viewConnections] = repoRows(m.connections()) + slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == "infra" })
	press(m, "f", "enter")
	if !strings.Contains(m.flash, "platform/infra") {
		t.Fatalf("demo should find infra's new folder: %q", m.flash)
	}
	press(m, "enter")
	if i := slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == "infra" }); m.repos[i].cfg.Status != config.StatusOK {
		t.Fatal("infra should be available after f")
	}
	// A repository not pushed to GitHub yet is a note, not a failure.
	m.selectRepo(1 + slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == "acme-mobile" }))
	press(m, "g", "p")
	if s := ansi.Strip(m.statusLine()); !strings.Contains(s, "acme-mobile: no origin remote yet (not pushed to GitHub)") || strings.Contains(s, "retry") {
		t.Fatalf("demo should show acme-mobile as not pushed yet, without a retry: %q", s)
	}
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections()) + slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == "acme-mobile" })
	press(m, "r", "ctrl+u")
	press(m, strings.Split("mobile", "")...)
	press(m, "enter", "g", "p", "ctrl+r") // reload under the new name
	m.flash = ""
	if m.repos[slices.IndexFunc(m.repos, func(r *repoState) bool { return strings.HasSuffix(r.cfg.Path, "acme-mobile") })].cfg.Name != "mobile" {
		t.Fatal("r should rename acme-mobile")
	}
	if s := ansi.Strip(m.statusLine()); !strings.Contains(s, "mobile: no origin remote yet") {
		t.Fatalf("a renamed unpushed repository should stay unpushed: %q", s)
	}
	m.selectRepo(0)
	// Badges: review requests and ready-to-merge, in the banner, and n to list each.
	if h := ansi.Strip(strings.Join(m.profileLines(), "\n")); !strings.Contains(h, "🔔") || !strings.Contains(h, "✔") {
		t.Fatalf("demo should show both badges in the banner:\n%s", h)
	}
	press(m, "n")
	if m.view != viewPRs || len(m.prList()) == 0 || !m.prList()[0].ForMe {
		t.Fatal("n should list the review requests")
	}
	press(m, "n")
	if len(m.prList()) == 0 || !m.ready(m.prList()[0]) || !strings.Contains(ansi.Strip(m.render()), "✔") {
		t.Fatal("n again should list PRs ready to merge, marked ✔")
	}
	press(m, "n")
	if m.filters[viewPRs] != "" {
		t.Fatal("a third n should show every PR")
	}
	for _, mode := range []Profile{ProfileASCII, ProfileText, ProfileOff, ProfileFull} {
		press(m, "b")
		if m.profile != mode {
			t.Fatalf("b should cycle to %s", profileNames[mode])
		}
		if !strings.Contains(ansi.Strip(m.render()), "Update "+gitx.DemoLatest) {
			t.Fatalf("demo should show the newer release in the %s header", profileNames[mode])
		}
	}
	if r := m.releaseRow(); r.state != "update available" || !strings.Contains(strings.Join(r.detail, "\n"), "@"+gitx.DemoLatest) {
		t.Fatalf("Connections should say how to install the newer release: %+v", r)
	}
}
