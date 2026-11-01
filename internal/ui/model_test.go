package ui

import (
	"context"
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
	if !strings.Contains(ansi.Strip(m.footer()), "n reviews / ready to merge") {
		t.Fatal("demo footer should say how to reach the review and ready badges")
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
	press(m, "g", "d")
	m.cursor[viewRuns] = slices.IndexFunc(m.runList(), func(r gitx.Run) bool { return r.Conclusion == "cancelled" })
	drain(m, m.ensure(false))
	if d := ansi.Strip(strings.Join(m.detailLines(), "\n")); !strings.Contains(d, "Why it stopped") || !strings.Contains(d, "higher priority") {
		t.Fatalf("demo should show why a deploy run was cancelled:\n%s", d)
	}
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
		if !strings.Contains(ansi.Strip(m.render()), "Update v0.5.0") {
			t.Fatalf("demo should show the newer release in the %s header", profileNames[mode])
		}
	}
	if r := m.releaseRow(); r.state != "update available" || !strings.Contains(strings.Join(r.detail, "\n"), "@v0.5.0") {
		t.Fatalf("Connections should say how to install the newer release: %+v", r)
	}
}
