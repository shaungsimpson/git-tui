package ui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

const (
	minW, minH = 60, 16
	sideW      = 24
	dateFmt    = "2006-01-02 15:04"
)

type layout struct {
	small                 bool
	bar                   bool // narrow: the repository sidebar collapses to a one-line selector
	sideW, mainW, detailW int  // detailW is the read-only preview beside the list
	bodyH                 int
}

func (m *Model) layout() layout {
	l := layout{bodyH: m.bodyFor(m.footerRows(), m.profileRows())}
	if m.width < minW || m.height < minH {
		l.small = true
		return l
	}
	l.sideW, l.mainW, l.detailW = m.widths()
	l.bar = l.sideW == 0
	return l
}

// bodyFor is the panes' height under a profile header and above a footer of these heights: the window less the
// header, separator and status lines, and the repository bar where the sidebar collapses to one.
func (m *Model) bodyFor(footer, profile int) int {
	body := m.height - 3 - footer - profile
	if side, _, _ := m.widths(); side == 0 {
		body--
	}
	return body
}

// widths are the sidebar, list and preview widths. They depend on the window width alone, so per-row callers
// such as stacked use this rather than layout, whose height needs the footer and with it every list.
func (m *Model) widths() (side, main, detail int) {
	main = m.width
	if m.width >= 100 {
		side = sideW
		main -= sideW + 1
	}
	if m.width >= 120 && !m.noPreview {
		detail = main * 2 / 5
		main -= detail + 1
	}
	return side, main, detail
}

// listHeight is the number of list rows visible in view v; Update uses it to keep the cursor on screen.
func (m *Model) listHeight(v viewID) int { return m.rowsIn(v, m.layout().bodyH) }

// rowsIn is listHeight for a known body height, so clampAll works the layout out once for every view.
func (m *Model) rowsIn(v viewID, bodyH int) int {
	h := bodyH - 1
	if v == viewRepo {
		if info := m.repoInfo(bodyH); len(info) > 0 {
			h -= len(info) + 1
		}
		if m.incomingRows() > 0 {
			h-- // the heading between incoming and recent commits
		}
	}
	if m.stacked(v) {
		return max(1, h/2)
	}
	return max(1, h)
}

// stackedBelow is the list width under which rows move their title, subject or notes to a second line.
const stackedBelow = 100

// stacked reports whether view v draws each row on two lines.
func (m *Model) stacked(v viewID) bool {
	if _, w, _ := m.widths(); w >= stackedBelow {
		return false
	}
	return v != viewConnections || m.cmp.open && m.cmp.view == v
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.ReportFocus = true // the refresh cadence pauses while the window is in the background
	v.BackgroundColor = m.st.bg
	v.ForegroundColor = m.st.fg
	v.WindowTitle = "git-tui"
	if n := m.profileStats(clock()).requested; n > 0 {
		v.WindowTitle += " (" + capped(n) + ")" // seen from another tab or window
	}
	return v
}

func (m *Model) render() string {
	if m.width == 0 {
		return ""
	}
	m.footerMemo = m.footerRows() // it asks every shortcut whether it applies, and a frame needs it often
	defer func() { m.footerMemo = 0 }()
	l := m.layout()
	if l.small {
		msg := fmt.Sprintf("Terminal too small (%d×%d).\nResize to at least %d×%d.", m.width, m.height, minW, minH)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.st.muted.Render(msg))
	}
	var rows []string
	if p := m.profileLines(); p != nil {
		rows = append(rows, box(p, m.width, len(p)))
	}
	rows = append(rows, m.header(), m.st.sep.Render(strings.Repeat("─", m.width)))
	if l.bar {
		rows = append(rows, m.repoBar())
	}
	if m.help {
		rows = append(rows, box(m.helpLines(l.bodyH), m.width, l.bodyH))
	} else if m.diff.open {
		rows = append(rows, box(m.diffLines(l.bodyH), m.width, l.bodyH))
	} else if m.scan.open {
		rows = append(rows, box(m.scanLines(l.bodyH), m.width, l.bodyH))
	} else if m.inspect {
		rows = append(rows, box(m.detailPane(m.width, l.bodyH), m.width, l.bodyH))
	} else {
		var cols []string
		if l.sideW > 0 {
			cols = append(cols, box(m.sidebarLines(l.bodyH), l.sideW, l.bodyH))
		}
		cols = append(cols, box(m.mainLines(l.mainW, l.bodyH), l.mainW, l.bodyH))
		if l.detailW > 0 {
			cols = append(cols, box(m.previewLines(l.detailW, l.bodyH), l.detailW, l.bodyH))
		}
		rows = append(rows, m.join(cols, l.bodyH))
	}
	rows = append(rows, m.statusLine(), m.footer())
	return strings.Join(rows, "\n")
}

// box truncates and pads lines to exactly w×h cells.
func box(lines []string, w, h int) string {
	out := make([]string, h)
	for i := range h {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], w, "…")
		}
		out[i] = line + strings.Repeat(" ", max(0, w-lipgloss.Width(line)))
	}
	return strings.Join(out, "\n")
}

func (m *Model) join(cols []string, h int) string {
	split := make([][]string, len(cols))
	for i, c := range cols {
		split[i] = strings.Split(c, "\n")
	}
	sep := m.st.sep.Render("│")
	out := make([]string, h)
	for y := range h {
		parts := make([]string, len(cols))
		for i := range cols {
			parts[i] = split[i][y]
		}
		out[y] = strings.Join(parts, sep)
	}
	return strings.Join(out, "\n")
}

// when formats a time in the zone the user chose with u: local by default, or UTC.
// when is a date on a detail page: exact, and how long ago.
func (m *Model) when(t time.Time) string {
	return m.zoned(t).Format(dateFmt) + " (" + age(m.now().Sub(t)) + ")"
}

// zoned is t in the zone times are shown in.
func (m *Model) zoned(t time.Time) time.Time {
	if m.utc {
		return t.UTC()
	}
	return t.Local()
}

// now is the current time, or the moment the fixtures describe in demo mode, so their ages read as recent.
func (m *Model) now() time.Time {
	if m.opts.Demo {
		return gitx.DemoNow
	}
	return clock()
}

// age is how long ago, in minutes, hours or days.
func age(d time.Duration) string {
	if d < 24*time.Hour {
		return ago(d)
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
}

// dateFormats are the ways list rows show dates, cycled with w. Each pads to one width so columns line up.
var dateFormats = []string{"numeric", "relative", "short", "weekday"}

// stamp is a date in a list row, in the chosen format. Numeric drops the year on lists under 70 columns.
func (m *Model) stamp(t time.Time, w int) string {
	l, thisYear := m.zoned(t), m.zoned(t).Year() == m.zoned(m.now()).Year()
	var s string
	switch dateFormats[m.dates] {
	case "relative":
		if d := m.now().Sub(t); d < 7*24*time.Hour {
			return fmt.Sprintf("%-8s", age(d))
		}
		s = "Jan 2006"
		if thisYear {
			s = "2 Jan"
		}
		return fmt.Sprintf("%-8s", l.Format(s))
	case "short":
		s = "_2 Jan 2006"
		if thisYear {
			s = "_2 Jan 15:04"
		}
		return fmt.Sprintf("%-12s", l.Format(s))
	case "weekday":
		s = "Mon _2 Jan 2006"
		if thisYear {
			s = "Mon _2 Jan 15:04"
		}
		return fmt.Sprintf("%-16s", l.Format(s))
	}
	if w >= 70 {
		return l.Format(dateFmt)
	}
	return l.Format(dateFmt)[len("2006-"):]
}

// zone names the zone times are shown in, such as "AEDT" or "UTC".
func (m *Model) zone() string {
	if m.utc {
		return "UTC"
	}
	return time.Now().Format("MST")
}

// fit places left and right on one line of width w, truncating left first.
func fit(left, right string, w int) string {
	rw := lipgloss.Width(right)
	left = ansi.Truncate(left, max(0, w-rw-1), "…")
	gap := max(1, w-lipgloss.Width(left)-rw)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, w, "")
}

func (m *Model) repoLabel() string {
	if r := m.selected(); r != nil {
		return clean(r.cfg.Name)
	}
	return "All repositories"
}

// goLetter is where in a view's name the letter its g shortcut uses sits, or -1.
func goLetter(v viewID) int {
	for _, g := range goKeys {
		if g.view == v {
			return strings.Index(strings.ToLower(viewNames[v]), g.key)
		}
	}
	return -1
}

// tabLabel is an unselected tab with its g letter underlined, so the tabs show their own shortcuts.
func (m *Model) tabLabel(v viewID) string {
	name, i := viewNames[v], goLetter(v)
	if i < 0 {
		return m.st.tab.Render(name)
	}
	return m.st.tab.Render(name[:i]) + m.st.tab.Underline(true).Render(name[i:i+1]) + m.st.tab.Render(name[i+1:])
}

func (m *Model) header() string {
	left := m.st.marker.Render(" git ")
	if m.opts.Demo {
		left += m.st.badge.Render("DEMO") + " "
	}
	var tabs []string
	for v := range viewCount {
		if v == m.view {
			tabs = append(tabs, m.st.tabActive.Render(viewNames[v]))
		} else {
			tabs = append(tabs, m.tabLabel(v))
		}
	}
	fresh := m.freshness()
	badges := "" // with no header to hold them, the badges sit here
	if m.profileShown() == ProfileOff {
		if b := m.badges(false); b != "" {
			badges = b + "  "
		}
	}
	right := func(parts ...string) string { return badges + m.st.muted.Render(joinSet(" · ", parts...)+" ") }
	full := left + " " + strings.Join(tabs, "  ")
	// The tabs matter more than the data's age: shorten the age, then drop it, before collapsing the tabs.
	// Badges outrank the repository and zone labels, which go next.
	short := strings.TrimPrefix(fresh, "Updated ") // "Just now" or "3m ago"; a number has no word to capitalise
	if short != "" {
		short = strings.ToUpper(short[:1]) + short[1:]
	}
	label, zone := m.repoLabel(), m.zone()
	tries := [][]string{{label, zone, fresh}, {label, zone, short}, {label, zone}}
	if badges != "" {
		tries = append(tries, []string{zone, short}, []string{zone}, nil)
	}
	active := left + " " + m.st.tabActive.Render(viewNames[m.view])
	for _, bar := range []string{full, active} { // with the tabs collapsed, the right side gives way again
		for _, parts := range tries {
			if r := right(parts...); lipgloss.Width(bar)+lipgloss.Width(r)+1 <= m.width {
				return fit(bar, r, m.width)
			}
		}
	}
	return fit(active, right(label, zone, fresh), m.width)
}

func (m *Model) repoBar() string {
	shown := m.shown()
	text := fmt.Sprintf(" Repository  < %s >  %d/%d", m.repoLabel(), slices.Index(shown, m.repo-1)+1, len(shown))
	if m.focus == paneSidebar {
		return m.st.selected.Width(m.width).Render(ansi.Truncate(text, m.width, "…"))
	}
	return m.st.muted.Render(ansi.Truncate(text, m.width, "…"))
}

func (m *Model) paneTitle(p paneID, text string) string {
	if m.focus == p && !m.help {
		return m.st.titleFocus.Render(text)
	}
	return m.st.title.Render(text)
}

// ---- sidebar ----

func (m *Model) repoGlyph(r *repoState) string {
	switch {
	case r.cfg.Status != config.StatusOK:
		return m.st.bad.Render("x")
	case r.summary.err != nil || r.log.err != nil || r.tags.err != nil:
		return m.st.bad.Render("!")
	case !r.summary.loaded:
		return m.st.muted.Render("·")
	case !r.summary.val.Clean():
		return m.st.warn.Render("+")
	}
	return m.st.ok.Render("·")
}

func (m *Model) sidebarLines(h int) []string {
	title := " Repositories"
	shown := m.shown()
	if m.group != "" { // the count always shows; "Repositories" and then the group's name give way to it
		count := fmt.Sprintf(" %d/%d", len(shown), len(m.repos))
		if title = " Repositories · " + clean(m.group) + count; lipgloss.Width(title) > sideW {
			title = " " + ansi.Truncate(clean(m.group), sideW-1-len(count), "…") + count
		}
	}
	lines := []string{m.paneTitle(paneSidebar, title)}
	names := []string{"All repositories"}
	glyphs := []string{" "}
	for _, i := range shown {
		names = append(names, clean(m.repos[i].cfg.Name))
		glyphs = append(glyphs, m.repoGlyph(m.repos[i]))
	}
	pos := slices.Index(shown, m.repo-1) + 1
	start := max(0, pos-(h-2))
	for i := start; i < len(names) && len(lines) < h; i++ {
		switch {
		case i == pos && m.focus == paneSidebar:
			lines = append(lines, m.st.selected.Width(sideW).Render(ansi.Truncate("> "+names[i], sideW, "…")))
		case i == pos:
			lines = append(lines, m.st.marker.Render("> ")+m.st.text.Bold(true).Render(names[i]))
		default:
			lines = append(lines, glyphs[i]+" "+m.st.text.Render(names[i]))
		}
	}
	return lines
}

// ---- main pane ----

type seg struct {
	s  string
	st lipgloss.Style
}

// brk marks where a stacked row continues on its second line; unstacked rows ignore it.
var brk = seg{s: "\n"}

// row draws one list item: one line, or two when stacked splits it at brk.
func (m *Model) row(segs []seg, w int, selected, stacked bool) []string {
	lines := [][]seg{nil}
	for _, s := range segs {
		switch {
		case s.s != brk.s:
			lines[len(lines)-1] = append(lines[len(lines)-1], s)
		case stacked:
			lines = append(lines, []seg{{"  ", m.st.text}})
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		lead := "  "
		if selected && i == 0 {
			lead = "> "
		}
		if selected { // a background across the row, keeping each part's colour; fainter when the list lacks focus
			bg, bold := m.st.selDim, false
			if m.focus == paneMain {
				bg, bold = m.st.selBg, true
			}
			on := func(st lipgloss.Style) lipgloss.Style { return st.Background(bg).Bold(bold) }
			line := on(m.st.marker).Render(lead)
			for _, s := range l {
				line += on(s.st).Render(s.s)
			}
			line = ansi.Truncate(line, w, on(m.st.text).Render("…"))
			out[i] = line + on(m.st.text).Render(strings.Repeat(" ", max(0, w-lipgloss.Width(line))))
			continue
		}
		parts := []string{lead}
		for _, s := range l {
			parts = append(parts, s.st.Render(s.s))
		}
		out[i] = strings.Join(parts, "")
	}
	return out
}

func pad(s string, n int) string {
	s = ansi.Truncate(s, n, "…")
	return s + strings.Repeat(" ", max(0, n-lipgloss.Width(s))+2)
}

func short(hash string) string { return hash[:min(7, len(hash))] }

func (m *Model) wrap(text string, w int, st lipgloss.Style) []string {
	return strings.Split(st.Width(max(1, w-2)).Render(text), "\n")
}

func (m *Model) mainLines(w, h int) []string {
	n, cur, off := m.list()
	title, rows := m.mainContent(w)
	lines := []string{m.paneTitle(paneMain, " "+title)}
	if m.view == viewRepo {
		if info := m.repoInfo(h); len(info) > 0 {
			lines = append(lines, info...)
			lines = append(lines, m.st.title.Render(" "+m.repoHeading(*off)))
		}
	}
	in := m.incomingRows()
	if n == 0 {
		msg, st := m.emptyMessage()
		for _, l := range m.wrap(msg, w, st) {
			lines = append(lines, " "+l)
		}
		return lines
	}
	end := min(n, *off+m.listHeight(m.view))
	for i := *off; i < end; i++ {
		if m.view == viewRepo && i == in && i > *off { // the incoming commits end on screen
			lines = append(lines, m.st.title.Render(" Recent commits"))
		}
		lines = append(lines, m.row(rows(i), w, i == *cur, m.stacked(m.view))...)
	}
	return lines
}

func (m *Model) commitSegs(c gitx.Commit, w int, showRepo bool) []seg {
	segs := []seg{}
	segs = append(segs, seg{m.stamp(c.When, w) + "  ", m.st.date})
	if showRepo {
		segs = append(segs, seg{pad(clean(c.Repo), 14), m.st.accent})
	}
	segs = append(segs, seg{short(c.Hash) + "  ", m.st.id})
	return titled(segs, seg{clean(c.Subject) + "  ", m.st.text}, seg{clean(c.Author), m.authorStyle(c.Author, false)}, m.stacked(m.view), w)
}

// titled ends a row with its title and author. Stacked, the author joins the details on the first line when
// it fits in w, so the second line is the title alone; a long author goes after the title instead of being cut.
func titled(segs []seg, title, author seg, stacked bool, w int) []seg {
	if !stacked {
		return append(segs, title, author)
	}
	first := 2 + lipgloss.Width(author.s) // row draws a two-cell lead before each line
	for _, s := range segs {
		first += lipgloss.Width(s.s)
	}
	if first <= w {
		return append(segs, author, brk, title)
	}
	return append(segs, brk, title, author)
}

// tagBase is the base tag chosen in Tags, or "" when the comparison state belongs to another view.
func (m *Model) tagBase() string {
	if m.cmp.view == viewTags {
		return m.cmp.base
	}
	return ""
}

// runState is the marker, label and style for a run: still going, or how it finished and how long it took.
func (m *Model) runState(r gitx.Run) (string, string, lipgloss.Style) {
	if r.Active() {
		return "…", strings.ReplaceAll(clean(r.Status), "_", " "), m.st.warn
	}
	took := ""
	if !r.Started.IsZero() && r.Updated.After(r.Started) {
		took = " " + r.Updated.Sub(r.Started).Round(time.Second).String()
	}
	switch {
	case r.Stopped():
		return "!", strings.ReplaceAll(clean(r.Conclusion), "_", " ") + took, m.st.bad
	case r.Conclusion == "success":
		return "·", "success" + took, m.st.ok
	}
	return "-", clean(r.Conclusion), m.st.muted
}

func (m *Model) runSegs(r gitx.Run, w int, showRepo bool) []seg {
	glyph, state, st := m.runState(r)
	segs := []seg{{glyph + " ", st}}
	if showRepo {
		segs = append(segs, seg{pad(clean(r.Repo), 14), m.st.accent})
	}
	// Status sits next to the workflow so it survives truncation on narrower panes.
	segs = append(segs, seg{pad(clean(r.Workflow), 20), m.st.text}, seg{pad(state, 16), st}, seg{pad(clean(r.Branch), 18), m.st.accent})
	segs = append(segs, seg{m.stamp(r.Started, w) + "  ", m.st.date}, brk)
	return append(segs, seg{clean(r.Title), m.st.muted})
}

func (m *Model) runDetail(r gitx.Run) []string {
	_, state, st := m.runState(r)
	lines := []string{
		" " + m.st.text.Bold(true).Render(clean(r.Workflow)),
		m.kv("Repo", clean(r.Repo), m.st.text),
		m.kv("Status", state, st),
		m.kv("Trigger", clean(r.Event), m.st.text),
		m.kv("Branch", clean(r.Branch), m.st.accent),
		m.kv("Commit", clean(r.Commit), m.st.id),
		m.kv("Started", m.when(r.Started), m.st.date),
		"",
		m.kv("Link", "", m.st.muted) + m.linkify(clean(r.URL), m.st.muted), // after the blank, so only the page shows it
		"",
		" " + clean(r.Title),
		"",
	}
	if r.Stopped() {
		lines = append(lines, m.whyLines(r)...)
	}
	if r.Active() {
		lines = append(lines, m.st.muted.Render(" Reloaded every minute while a deploy is running, less often once it has gone on for 30 minutes."))
	}
	return append(lines, m.st.muted.Render(" Deploy runs: workflows named \"deploy\" (not PR checks) and workflows that recorded a GitHub deployment."))
}

// whyLines is the reason section of a stopped run, read from its job annotations.
func (m *Model) whyLines(r gitx.Run) []string {
	lines := []string{m.st.title.Render(" Why it stopped")}
	s := m.why[r.ID]
	switch {
	case s == nil || !s.loaded && s.err == nil:
		lines = append(lines, m.st.muted.Render(" Loading from GitHub…"))
	case !s.loaded:
		lines = append(lines, m.st.bad.Render(" Could not read why: "+clean(s.err.Error())))
	case len(s.val) == 0:
		lines = append(lines, m.st.muted.Render(" GitHub recorded no reason. Press o to read the run's logs."))
	default:
		for _, reason := range s.val {
			lines = append(lines, m.st.bad.Render(" "+clean(reason)))
		}
	}
	return append(lines, "")
}

// envState is the short marker for an environment row: a newer attempt that failed or is still running.
func (m *Model) envState(e envRow) (string, string, lipgloss.Style) {
	switch e.Latest {
	case "":
		if e.Commit == "" {
			return "-", "no successful deployment", m.st.muted
		}
		return "·", "", m.st.ok
	case "FAILURE", "ERROR":
		return "!", "last attempt " + strings.ToLower(clean(e.Latest)), m.st.bad
	}
	return "…", strings.ToLower(strings.ReplaceAll(clean(e.Latest), "_", " ")), m.st.warn
}

func (m *Model) envSegs(e envRow, w int, showRepo bool) []seg {
	glyph, note, st := m.envState(e)
	segs := []seg{{glyph + " ", st}}
	if showRepo {
		segs = append(segs, seg{pad(clean(e.Repo), 14), m.st.accent})
	}
	ref := clean(e.Ref)
	if e.Ref == e.Commit { // deployed by commit ID; the ID is already in its own column
		ref = ""
	}
	segs = append(segs, seg{pad(clean(e.Environment), 17), m.st.text}, seg{pad(short(clean(e.Commit)), 7), m.st.id})
	if !e.When.IsZero() {
		segs = append(segs, seg{m.stamp(e.When, w) + "  ", m.st.date})
	}
	return append(segs, brk, seg{ref + "  ", m.st.accent}, seg{note, st})
}

func (m *Model) envDetail(e envRow) []string {
	_, note, st := m.envState(e)
	lines := []string{
		" " + m.st.text.Bold(true).Render(clean(e.Repo)+" · "+clean(e.Environment)),
		m.kv("Commit", clean(e.Commit), m.st.id),
	}
	if e.Ref != "" && e.Ref != e.Commit {
		lines = append(lines, m.kv("Ref", clean(e.Ref), m.st.text))
	}
	if !e.When.IsZero() {
		lines = append(lines, m.kv("Deployed", m.when(e.When), m.st.date))
	}
	if e.Latest != "" {
		lines = append(lines, m.kv("Latest", note+" at "+m.when(e.LatestWhen), st))
	}
	return append(lines, "",
		m.st.muted.Render(" As recorded by GitHub Deployments; not verified against the running service."),
		m.st.muted.Render(" Enter lists commits on origin/HEAD that this deployment does not contain."))
}

var checkText = map[gitx.CheckState]string{
	gitx.ChecksNone: "no checks", gitx.ChecksPassing: "passing", gitx.ChecksPending: "pending", gitx.ChecksFailing: "failing",
}

func (m *Model) checkStyle(c gitx.CheckState) lipgloss.Style {
	switch c {
	case gitx.ChecksPassing:
		return m.st.ok
	case gitx.ChecksPending:
		return m.st.warn
	case gitx.ChecksFailing:
		return m.st.bad
	}
	return m.st.muted
}

// reviewText is the short review label and its style; drafts are not ready for review yet.
func (m *Model) reviewText(p gitx.PR) (string, lipgloss.Style) {
	switch {
	case p.Draft:
		return "draft", m.st.muted
	case p.Review == "APPROVED":
		return "approved", m.st.ok
	case p.Review == "CHANGES_REQUESTED":
		return "changes", m.st.bad
	case p.Review == "REVIEW_REQUIRED":
		return "review", m.st.warn
	}
	return "-", m.st.muted
}

var checkGlyph = map[gitx.CheckState]string{gitx.ChecksNone: " ", gitx.ChecksPassing: "·", gitx.ChecksPending: "…", gitx.ChecksFailing: "!"}

// prStack is p's stack among all of its repository's open PRs, whatever the filter hides.
func (m *Model) prStack(p gitx.PR) ([]gitx.PR, []int) {
	for _, r := range m.repos {
		if r.cfg.Name == p.Repo {
			return gitx.Stack(r.gh.prs.val, p)
		}
	}
	return nil, nil
}

// stackMark marks a stacked PR; the ASCII and text headers say the terminal may not draw emoji, as for 🔔.
func (m *Model) stackMark() string {
	if m.profile == ProfileASCII || m.profile == ProfileText {
		return "="
	}
	return "🥞"
}

func (m *Model) prSegs(p gitx.PR, w int, showRepo bool) []seg {
	glyph := checkGlyph[p.Checks]
	review, rst := m.reviewText(p)
	star, sst := " ", m.st.accent // marks PRs waiting on the viewer's review, or the viewer's ready to merge
	switch {
	case p.ForMe:
		star = "★"
	case m.ready(p):
		star, sst = "✔", m.st.ok
	}
	mark := m.stackMark()
	pancake := strings.Repeat(" ", lipgloss.Width(mark)+1)
	if s, _ := m.prStack(p); s != nil {
		pancake = mark + " "
	}
	segs := []seg{{glyph + " ", m.checkStyle(p.Checks)}, {star + " ", sst}, {pancake, m.st.text}}
	if showRepo {
		segs = append(segs, seg{pad(clean(p.Repo), 14), m.st.accent})
	}
	segs = append(segs, seg{pad("#"+strconv.Itoa(p.Number), 6), m.st.id}, seg{pad(review, 8), rst})
	segs = append(segs, seg{m.stamp(p.Updated, w) + "  ", m.st.date})
	return titled(segs, seg{clean(p.Title) + "  ", m.st.text}, seg{clean(p.Author), m.authorStyle(p.Author, true)}, m.stacked(m.view), w)
}

func (m *Model) prDetail(p gitx.PR) []string {
	review, rst := m.reviewText(p)
	lines := []string{
		" " + m.st.text.Bold(true).Render(clean(p.Title)),
		m.kv("PR", "#"+strconv.Itoa(p.Number)+" in "+clean(p.Repo), m.st.accent),
		m.kv("Author", clean(p.Author), m.authorStyle(p.Author, true)),
		m.kv("Branch", clean(p.Branch), m.st.text),
		m.kv("Review", review, rst),
		m.kv("Checks", checkText[p.Checks], m.checkStyle(p.Checks)),
		m.kv("Updated", m.when(p.Updated), m.st.date),
	}
	if p.ForMe {
		lines = slices.Insert(lines, 5, m.kv("Requested", "★ your review", m.st.accent))
	}
	if m.ready(p) {
		lines = slices.Insert(lines, 5, m.kv("Ready", "✔ approved, checks passing", m.st.ok))
	}
	if s, depth := m.prStack(p); s != nil {
		lines = append(lines, m.kv("Stack", m.stackMark()+fmt.Sprintf(" %d PRs, merge in this order", len(s)), m.st.text))
		for i, q := range s {
			mark := "  "
			if q.Number == p.Number {
				mark = "▸ "
			}
			review, rst := m.reviewText(q)
			lines = append(lines, "   "+m.st.accent.Render(mark)+strings.Repeat("  ", depth[i])+m.st.id.Render(pad("#"+strconv.Itoa(q.Number), 5))+
				m.checkStyle(q.Checks).Render(checkGlyph[q.Checks]+" ")+rst.Render(pad(review, 8))+m.st.text.Render(clean(q.Title)))
		}
	}
	lines = append(lines, "", m.kv("Link", "", m.st.muted)+m.linkify(clean(p.URL), m.st.muted)) // after the blank, so only the page shows it
	if strings.TrimSpace(p.Body) != "" {
		title := " Description"
		if m.rawMD {
			title += " · raw"
		}
		lines = append(lines, "", m.st.title.Render(title))
		desc, more := m.descLines(p)
		for _, l := range desc {
			if l.pic != nil {
				lines = append(lines, m.picLines(*l.pic)...)
			} else {
				lines = append(lines, l.text)
			}
		}
		if more > 0 {
			lines = append(lines, m.st.muted.Render(fmt.Sprintf(" … %d more lines. Press o to read the rest on GitHub.", more)))
		}
	}
	if len(p.Files) > 0 {
		lines = append(lines, "", m.st.title.Render(fmt.Sprintf(" Files changed (%d) · d shows the diff", len(p.Files))))
		for _, f := range p.Files {
			lines = append(lines, " "+m.st.ok.Render(pad("+"+strconv.Itoa(f.Additions), 5))+m.st.bad.Render(pad("-"+strconv.Itoa(f.Deletions), 5))+m.st.text.Render(clean(f.Path)))
		}
	}
	return lines
}

// maxBodyLines caps a PR description in Details; bot PRs can carry tens of thousands of characters.
const maxBodyLines = 200

// bodyLines is the part of a PR description that Details shows, and how many lines are left out.
func bodyLines(body string) ([]string, int) {
	all := strings.Split(strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")), "\n")
	if len(all) <= maxBodyLines {
		return all, 0
	}
	return all[:maxBodyLines], len(all) - maxBodyLines
}

// picLines draws a PR-body picture, or says why it is not shown.
func (m *Model) picLines(l termimg.Link) []string {
	label := "image"
	if l.Alt != "" {
		label = "image: " + clean(l.Alt)
	}
	note := func(why string) []string { return []string{m.st.muted.Render(" [" + label + " · " + why + "]")} }
	if !m.opts.Images {
		return note("this terminal cannot draw images; press o to view on GitHub")
	}
	if !termimg.Allowed(l.URL) {
		return note("not hosted on GitHub, so not loaded")
	}
	st, ok := m.pics[l.URL]
	cols := m.picCols()
	switch {
	case ok && st.err != nil: // before the loading check: a failed picture is never placed
		return note("not shown: " + clean(st.err.Error()))
	case !ok || st.loading || st.placed != cols: // placed != cols only until the new size is sent
		return note("loading…")
	}
	return st.pic.Lines(cols)
}

// mainContent returns the pane title and a row builder for the current list.
func (m *Model) mainContent(w int) (string, func(int) []seg) {
	filter := "" // the sort, when not the default, and the filter
	if name := m.sortName(m.view); name != "" && m.sorts[m.view] > 0 {
		filter = " · by " + name
	}
	if f := m.filters[m.view]; f != "" {
		filter += " · filter: " + clean(f)
	}
	if m.cmpOpen() {
		cs := m.cmp.res.val
		return fmt.Sprintf("%s · %d commits", m.cmp.title, len(cs)), func(i int) []seg { return m.commitSegs(cs[i], w, false) }
	}
	switch m.view {
	case viewOverview:
		rows := m.overviewRows()
		return fmt.Sprintf("Overview · %d repositories%s", len(m.shown()), filter), func(i int) []seg {
			return m.overviewSegs(m.repos[rows[i]])
		}
	case viewActivity:
		cs := m.activity()
		return fmt.Sprintf("Activity · %s · %d commits%s", m.repoLabel(), len(cs), filter), func(i int) []seg {
			return m.commitSegs(cs[i], w, m.repo == 0)
		}
	case viewPRs:
		prs := m.prList()
		return fmt.Sprintf("Pull requests · %s · %d open%s", m.repoLabel(), len(prs), filter), func(i int) []seg {
			return m.prSegs(prs[i], w, m.repo == 0)
		}
	case viewRepo:
		cs := m.repoCommits()
		return "Repository · " + m.repoLabel() + filter, func(i int) []seg { return m.commitSegs(cs[i], w, false) }
	case viewRuns:
		runs := m.runList()
		active := 0
		for _, r := range runs {
			if r.Active() {
				active++
			}
		}
		return fmt.Sprintf("Deploys · %s · %d runs, %d running%s", m.repoLabel(), len(runs), active, filter), func(i int) []seg {
			return m.runSegs(runs[i], w, m.repo == 0)
		}
	case viewEnvs:
		envs := m.envList()
		return fmt.Sprintf("Environments · %s · %d recorded%s", m.repoLabel(), len(envs), filter), func(i int) []seg {
			return m.envSegs(envs[i], w, m.repo == 0)
		}
	case viewTags:
		tags := m.tagList()
		title := "Tags · " + m.repoLabel() + filter
		if base := m.tagBase(); base != "" {
			title += " · base " + clean(base) + " · enter compares, or on the base clears it"
		} else {
			title += " · enter picks the comparison base"
		}
		nameW := 8
		for _, t := range tags {
			nameW = max(nameW, min(28, len(t.Name)))
		}
		return title, func(i int) []seg {
			t := tags[i]
			name := pad(clean(t.Name), nameW)
			st := m.st.accent
			if t.Name == m.tagBase() {
				st = m.st.warn
			}
			segs := []seg{{name, st}}
			segs = append(segs, seg{m.stamp(t.When, w) + "  ", m.st.date})
			return append(segs, seg{short(t.Commit) + "  ", m.st.id}, brk, seg{clean(t.Subject), m.st.text})
		}
	default:
		rows := m.connections()
		return "Connections", func(i int) []seg {
			c := rows[i]
			return []seg{{c.glyph + " ", m.levelStyle(c.level)}, {pad(c.name, 24), m.st.text}, {c.state, m.levelStyle(c.level)}}
		}
	}
}

func (m *Model) overviewSegs(r *repoState) []seg {
	name := seg{pad(clean(r.cfg.Name), 16), m.st.text}
	glyph := seg{ansi.Strip(m.repoGlyph(r)) + " ", m.st.muted}
	switch {
	case r.cfg.Status != config.StatusOK:
		return []seg{{"x ", m.st.bad}, name, {"unavailable: " + r.cfg.Status.String(), m.st.bad}}
	case r.summary.err != nil && !r.summary.loaded:
		return []seg{{"! ", m.st.bad}, name, {"error: " + clean(r.summary.err.Error()), m.st.bad}}
	case !r.summary.loaded:
		return []seg{glyph, name, {"loading…", m.st.muted}}
	}
	s := r.summary.val
	branch := clean(s.Branch)
	if branch == "" {
		branch = "(detached)"
	}
	state, stStyle := "clean", m.st.ok
	if !s.Clean() {
		state, stStyle = fmt.Sprintf("%d changed", s.Staged+s.Unstaged+s.Untracked+s.Conflicted), m.st.warn
		glyph.st = m.st.warn
	} else {
		glyph.st = m.st.ok
	}
	if strings.HasPrefix(glyph.s, "!") { // repoGlyph saw a failed load; keep the error colour over clean/changed
		glyph.st = m.st.bad
	}
	tracking := "no upstream"
	if s.HasAB {
		tracking = fmt.Sprintf("↑%d ↓%d", s.Ahead, s.Behind)
	}
	subject := ""
	if s.Last != nil {
		subject = clean(s.Last.Subject)
	}
	segs := []seg{glyph, name, {pad(branch, 24), m.st.accent}, {pad(state, 10), stStyle}, {pad(tracking, 11), m.st.muted}, brk}
	if f, st := m.fetched(s); f != "" {
		segs = append(segs, seg{f + " · ", st})
	}
	return append(segs, seg{subject, m.st.muted})
}

// fetched is how long ago a clone last fetched, or "" for one with no upstream that never has.
func (m *Model) fetched(s gitx.Summary) (string, lipgloss.Style) {
	switch {
	case !s.Fetched.IsZero():
		d := m.now().Sub(s.Fetched)
		return "fetched " + age(d), m.fetchStyle(d)
	case s.Upstream != "":
		return "not fetched since clone", m.st.muted
	}
	return "", m.st.muted
}

// fetchStyle colours a fetch's age: amber after a day, red after a week.
func (m *Model) fetchStyle(d time.Duration) lipgloss.Style {
	switch {
	case d >= 7*24*time.Hour:
		return m.st.bad
	case d >= 24*time.Hour:
		return m.st.warn
	}
	return m.st.date
}

// newer is how many commits GitHub has past this clone's last fetch, once asked since that fetch.
func newer(r *repoState) int {
	if g := r.unfetched; g.loaded && g.val.base == r.summary.val.UpstreamID {
		return g.val.n
	}
	return 0
}

// staleNote warns in the status line that the selected repository's commits are behind GitHub's, or may be:
// GitHub has newer ones, or the last fetch is a day old.
func (m *Model) staleNote() (string, lipgloss.Style) {
	i := m.selectedRepo()
	if i < 0 || !m.repos[i].summary.loaded {
		return "", m.st.muted
	}
	r := m.repos[i]
	s, name := r.summary.val, clean(r.cfg.Name)
	if n := newer(r); n > 0 {
		return fmt.Sprintf(" %s: %d newer commits on GitHub; run git fetch to see them", name, n), m.st.warn
	}
	if d := m.now().Sub(s.Fetched); !s.Fetched.IsZero() && d >= 24*time.Hour {
		return " " + name + " last fetched " + age(d) + ": run git fetch for newer commits", m.fetchStyle(d)
	}
	return "", m.st.muted
}

func (m *Model) emptyMessage() (string, lipgloss.Style) {
	if m.view != viewConnections && len(m.repos) == 0 {
		switch {
		case errors.Is(m.opts.ConfigErr, config.ErrNotFound):
			return "No repositories yet. Press a to add some, pass -config FILE, or run with -demo to explore fixture data.", m.st.muted
		case m.opts.ConfigErr != nil:
			return "Configuration error: " + clean(m.opts.ConfigErr.Error()), m.st.bad
		}
		return "The configuration lists no repositories. Press a to add some.", m.st.muted
	}
	if f := m.filters[m.view]; f != "" && m.filterable() {
		return "No matches for \"" + clean(f) + "\". Esc clears the filter.", m.st.muted
	}
	if m.cmpOpen() {
		msg, st := slotMessage(m, &m.cmp.res, "Loading commits…", m.cmp.empty)
		if m.cmp.view == viewEnvs && m.cmp.res.err != nil {
			msg += " The deployed commit may be newer than your clone: run git fetch, then Ctrl+R."
		}
		return msg, st
	}
	needRepo := func() (string, lipgloss.Style, bool) {
		r := m.selected()
		switch {
		case r == nil:
			return "Select a repository in the sidebar (Tab to focus it, j/k to choose).", m.st.muted, true
		case r.cfg.Status != config.StatusOK && m.canEdit():
			return "Unavailable: " + clean(r.cfg.Err) + " In the sidebar, f finds a moved folder, x removes it.", m.st.bad, true
		case r.cfg.Status != config.StatusOK:
			return "Unavailable: " + clean(r.cfg.Err), m.st.bad, true
		}
		return "", m.st.muted, false
	}
	switch m.view {
	case viewActivity:
		h := m.health()
		if h.loading > 0 {
			return "Loading commits…", m.st.muted
		}
		if len(h.errs) > 0 {
			return "Could not read history: " + h.errs[0], m.st.bad
		}
		return "No commits found.", m.st.muted
	case viewPRs:
		h := m.health()
		if h.loading > 0 {
			return "Loading pull requests from GitHub…", m.st.muted
		}
		if len(h.errs) > 0 {
			return "Could not read pull requests: " + h.errs[0], m.st.bad
		}
		return "No open pull requests.", m.st.muted
	case viewRepo:
		if msg, st, ok := needRepo(); ok {
			return msg, st
		}
		return slotMessage(m, &m.selected().log, "Loading commits…", "No commits yet.")
	case viewTags:
		if msg, st, ok := needRepo(); ok {
			return msg, st
		}
		return slotMessage(m, &m.selected().tags, "Loading tags…", "No tags in this repository.")
	case viewRuns:
		h := m.health()
		if h.loading > 0 {
			return "Loading deploy runs from GitHub…", m.st.muted
		}
		if len(h.errs) > 0 {
			return "Could not read workflow runs: " + h.errs[0], m.st.bad
		}
		return "No recent deploy runs.", m.st.muted
	case viewEnvs:
		h := m.health()
		if h.loading > 0 {
			return "Loading deployments from GitHub…", m.st.muted
		}
		if len(h.errs) > 0 {
			return "Could not read deployments: " + h.errs[0], m.st.bad
		}
		return "No deployments are recorded on GitHub for these repositories.", m.st.muted
	}
	return "Nothing to show.", m.st.muted
}

func slotMessage[T any](m *Model, s *slot[T], loading, empty string) (string, lipgloss.Style) {
	switch {
	case s.loading && !s.loaded:
		return loading, m.st.muted
	case s.err != nil:
		return "Error: " + clean(s.err.Error()), m.st.bad
	}
	return empty, m.st.muted
}

// ---- repository info and detail page ----

func (m *Model) kv(label, value string, st lipgloss.Style) string {
	return " " + m.st.muted.Render(pad(label, 9)) + st.Render(value)
}

func (m *Model) summaryLines(r *repoState) []string {
	lines := []string{m.kv("Path", clean(r.cfg.Path), m.st.text)}
	if r.cfg.Status != config.StatusOK {
		return append(lines, m.kv("Status", clean(r.cfg.Err), m.st.bad))
	}
	if !r.summary.loaded {
		if r.summary.err != nil {
			return append(lines, m.kv("Status", clean(r.summary.err.Error()), m.st.bad))
		}
		return append(lines, m.kv("Status", "loading…", m.st.muted))
	}
	s := r.summary.val
	branch := clean(s.Branch)
	if branch == "" {
		branch = "detached HEAD"
	}
	head := "(no commits)"
	if s.Last != nil {
		head = short(s.Head) + "  " + clean(s.Last.Subject)
	}
	upstream, tracking := "none", "not tracking a remote branch"
	if s.Upstream != "" {
		upstream = clean(s.Upstream)
	}
	if s.HasAB {
		tracking = fmt.Sprintf("%d ahead, %d behind (as of last fetch)", s.Ahead, s.Behind)
	}
	work, wst := "clean", m.st.ok
	if !s.Clean() {
		work = fmt.Sprintf("%d staged, %d unstaged, %d untracked, %d conflicted", s.Staged, s.Unstaged, s.Untracked, s.Conflicted)
		wst = m.st.warn
	}
	lines = append(lines,
		m.kv("Branch", branch, m.st.accent),
		m.kv("HEAD", head, m.st.text),
		m.kv("Upstream", upstream, m.st.text),
		m.kv("Tracking", tracking, m.st.text))
	if f, st := m.fetched(s); f != "" {
		if !s.Fetched.IsZero() {
			f = m.when(s.Fetched)
		}
		if n := newer(r); n > 0 {
			f += fmt.Sprintf(" · %d newer on GitHub", n)
		}
		lines = append(lines, m.kv("Fetched", f, st))
	}
	lines = append(lines, m.kv("Worktree", work, wst))
	if r.summary.err != nil {
		lines = append(lines, m.kv("Refresh", "failed: "+clean(r.summary.err.Error()), m.st.bad))
	}
	return lines
}

// repoHeading titles the Repository list from row off: its incoming commits first, then its recent ones.
func (m *Model) repoHeading(off int) string {
	n := m.incomingRows()
	if off >= n {
		return "Recent commits"
	}
	s := m.selected().summary.val
	count := strconv.Itoa(n)
	if s.Behind > n && m.filters[viewRepo] == "" {
		count += " of " + strconv.Itoa(s.Behind) // the list stops at incomingCap
	}
	return "Incoming from " + clean(s.Upstream) + " · " + count
}

// repoInfo is the fixed block above the commit list in the Repository view, for a pane bodyH tall. It is left out
// when it would leave fewer than 2 commit rows under the pane title and the list's two headings.
// It never checks m.view: clampAll sizes the Repository list while another view shows.
func (m *Model) repoInfo(bodyH int) []string {
	r := m.selected()
	if r == nil || r.cfg.Status != config.StatusOK {
		return nil
	}
	if info := m.summaryLines(r); bodyH-3-len(info) >= 2 {
		return info
	}
	return nil
}

func (m *Model) commitDetail(c gitx.Commit) []string {
	lines := []string{
		m.kv("Commit", c.Hash, m.st.id),
		m.kv("Repo", clean(c.Repo), m.st.text),
		m.kv("Author", clean(c.Author), m.authorStyle(c.Author, false)),
		m.kv("Date", m.when(c.When), m.st.date),
		"",
		" " + m.st.text.Bold(true).Render(clean(c.Subject)),
	}
	if c.Body != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(c.Body, "\n") {
			lines = append(lines, " "+m.linkify(clean(l), lipgloss.NewStyle())) // clean per line so the body keeps its line breaks
		}
	}
	return lines
}

func (m *Model) detailLines() []string {
	n, cur, _ := m.list()
	if n == 0 {
		return []string{m.st.muted.Render("Nothing selected.")}
	}
	i := *cur
	if m.cmpOpen() {
		return m.commitDetail(m.cmp.res.val[i])
	}
	switch m.view {
	case viewOverview:
		r := m.repos[m.overviewRows()[i]]
		return append([]string{" " + m.st.text.Bold(true).Render(clean(r.cfg.Name))}, m.summaryLines(r)...)
	case viewActivity:
		return m.commitDetail(m.activity()[i])
	case viewPRs:
		return m.prDetail(m.prList()[i])
	case viewRepo:
		return m.commitDetail(m.repoCommits()[i])
	case viewEnvs:
		return m.envDetail(m.envList()[i])
	case viewRuns:
		return m.runDetail(m.runList()[i])
	case viewTags:
		t := m.tagList()[i]
		return []string{
			m.kv("Tag", clean(t.Name), m.st.accent),
			m.kv("Commit", t.Commit, m.st.id),
			m.kv("Date", m.when(t.When), m.st.date),
			m.kv("Message", clean(t.Subject), m.st.text),
		}
	default:
		c := m.connections()[i]
		lines := []string{" " + m.st.text.Bold(true).Render(c.name), m.kv("Status", c.state, m.levelStyle(c.level)), ""}
		for _, d := range c.detail {
			lines = append(lines, " "+d)
		}
		return lines
	}
}

// wrapLines wraps detail lines to w columns; a wrapped line continues under its own indent, so nesting stays visible.
func wrapLines(lines []string, w int) []string {
	var body []string
	for _, l := range lines {
		plain := ansi.Strip(l)
		cut := min(len(plain)-len(strings.TrimLeft(plain, " ")), w/2) // at most half, so text still fits
		wrapped := lipgloss.NewStyle().Width(max(2, w-max(1, cut))).Render(ansi.TruncateLeft(l, cut, ""))
		for _, part := range strings.Split(wrapped, "\n") {
			body = append(body, strings.Repeat(" ", max(1, cut))+part)
		}
	}
	return body
}

// maxDetailOff is how far the detail page can scroll at the current size.
func (m *Model) maxDetailOff() int {
	return max(0, len(wrapLines(m.detailLines(), m.width))-(m.layout().bodyH-1))
}

// detailPane is the detail page; its title says where the item sits in the list, for stepping with J/K.
func (m *Model) detailPane(w, h int) []string {
	body := wrapLines(m.detailLines(), w)
	off := min(m.detOff, max(0, len(body)-(h-1)))
	title := " Details · " + viewNames[m.view]
	if m.cmpOpen() {
		title = " Details · " + m.cmp.title
	}
	if n, cur, _ := m.list(); n > 0 {
		title += fmt.Sprintf(" · %d of %d", *cur+1, n)
	}
	return append([]string{m.paneTitle(paneDetail, title)}, body[off:]...)
}

// previewLines is the pane beside the list: the head of the selected row's details, up to their first long
// section, then the shortcuts that act on it, which the footer leaves out, so the head gives way to them at
// height h. It never takes focus; v opens the whole detail page.
func (m *Model) previewLines(w, h int) []string {
	head := m.detailLines()
	if m.view != viewConnections || m.cmpOpen() { // a connection's details are all short notes
		if i := slices.Index(head, ""); i >= 0 {
			head = head[:i]
		}
	}
	lines := append([]string{m.st.title.Render(" Preview")}, wrapLines(head, w)...)
	var keys []string
	for _, a := range actions {
		if a.item && m.focus == paneMain && a.available(m) { // from the sidebar, t and T act on its repository instead
			h := a.key.Help()
			keys = append(keys, " "+m.st.key.Render(pad(h.Key, 9))+m.st.muted.Render(h.Desc))
		}
	}
	if len(keys) > 0 {
		tail := append([]string{"", m.st.title.Render(" Shortcuts")}, keys...)
		lines = append(lines[:max(1, min(len(lines), h-len(tail)))], tail...)
	}
	return lines
}

// ---- help, status and footer ----

// helpLines is the help page at height h: a fixed title, then the view jumps and every shortcut by group,
// scrolled by helpOff.
func (m *Model) helpLines(h int) []string {
	title := fit(m.st.titleFocus.Render(" Keyboard shortcuts · "+viewNames[m.view]), m.st.muted.Render("git-tui "+m.version()+" "), m.width)
	body := m.helpBody()
	off := min(m.helpOff, max(0, len(body)-(h-1)))
	return append([]string{title}, body[off:]...)
}

// helpBody lists every key, available here or not, so help is the whole map: the view jumps, then each group
// in one column, or in two when the terminal is wide.
func (m *Model) helpBody() []string {
	var lines []string
	row, label := "", pad("g then", 10)
	for _, g := range goKeys { // wrap the jumps rather than cut them off
		item := m.st.key.Render(g.key) + " " + m.st.text.Render(viewNames[g.view])
		if row != "" && lipgloss.Width(row)+lipgloss.Width(item)+3 > m.width-14 {
			lines = append(lines, "  "+m.st.key.Render(label)+row)
			row, label = "", pad("", 10)
		}
		if row != "" {
			row += m.st.muted.Render(" · ")
		}
		row += item
	}
	lines = append(lines, "  "+m.st.key.Render(label)+row)
	for g, name := range groupNames {
		var keys []string
		for _, a := range actions {
			if h := a.key.Help(); h.Key != "" && a.group == helpGroup(g) {
				// 9 fits "pgup/pgdn"; two columns need 100 cells, as the longest entry is 49.
				keys = append(keys, "  "+m.st.key.Render(pad(h.Key, 9))+m.st.text.Render(h.Desc))
			}
		}
		lines = append(lines, "", m.st.title.Render(" "+name))
		if m.width < 100 {
			lines = append(lines, keys...)
			continue
		}
		half, col := (len(keys)+1)/2, m.width/2
		for i := range half {
			row := keys[i] + strings.Repeat(" ", max(0, col-lipgloss.Width(keys[i])))
			if i+half < len(keys) {
				row += keys[i+half]
			}
			lines = append(lines, row)
		}
	}
	return lines
}

// scrollHelp moves help by d lines, stopping with its last line at the bottom.
func (m *Model) scrollHelp(d int) {
	last := max(0, len(m.helpBody())-(m.layout().bodyH-1))
	m.helpOff = min(max(0, m.helpOff+d), last)
}

type health struct {
	loading int
	errs    []string
	notes   []string // not failures, such as a repository not pushed to GitHub yet
}

func addHealth[T any](h *health, name string, s *slot[T]) {
	if s.loading {
		h.loading++
	}
	switch {
	case errors.Is(s.err, gitx.ErrNoOrigin):
		h.notes = append(h.notes, clean(name)+": "+s.err.Error())
	case s.err != nil:
		h.errs = append(h.errs, clean(name)+": "+clean(s.err.Error()))
	}
}

func (m *Model) health() health {
	var h health
	switch m.view {
	case viewOverview:
		for _, i := range m.shown() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].summary)
		}
	case viewActivity:
		for _, i := range m.scope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].log)
		}
	case viewPRs:
		for _, i := range m.ghScope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].gh.prs)
		}
	case viewRepo:
		if r := m.selected(); r != nil {
			addHealth(&h, r.cfg.Name, &r.summary)
			addHealth(&h, r.cfg.Name, &r.log)
		}
	case viewTags:
		if r := m.selected(); r != nil {
			addHealth(&h, r.cfg.Name, &r.tags)
		}
	case viewEnvs:
		for _, i := range m.ghScope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].gh.deps)
		}
	case viewRuns:
		for _, i := range m.ghScope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].gh.runs)
		}
	}
	if m.cmpOpen() && m.cmp.repo > 0 && m.cmp.repo <= len(m.repos) {
		addHealth(&h, m.repos[m.cmp.repo-1].cfg.Name, &m.cmp.res)
	}
	return h
}

func (m *Model) statusLine() string {
	var left string
	h := m.health()
	stale, staleSt := m.staleNote()
	switch {
	case m.flash != "":
		left = m.st.accent.Render(" " + clean(m.flash))
	case h.loading > 0:
		left = m.st.muted.Render(fmt.Sprintf(" Loading (%d pending)…", h.loading))
	case len(h.errs) == 1:
		left = m.st.bad.Render(" " + h.errs[0])
	case len(h.errs) > 1:
		left = m.st.bad.Render(fmt.Sprintf(" %d reads failed. First: %s", len(h.errs), h.errs[0]))
	case len(h.notes) == 1:
		left = m.st.muted.Render(" " + h.notes[0])
	case len(h.notes) > 1:
		left = m.st.muted.Render(fmt.Sprintf(" %d repositories have no origin remote yet (not pushed to GitHub)", len(h.notes)))
	case stale != "":
		left = staleSt.Render(stale)
	case m.opts.Demo:
		left = m.st.warn.Render(" Demo mode: fixture data only, no repositories are read")
	case m.opts.ConfigErr != nil:
		left = m.st.muted.Render(" No repositories loaded")
	default:
		left = m.st.muted.Render(" Read-only local git · " + clean(m.opts.ConfigPath))
	}
	right := ""
	if len(h.errs) > 0 && h.loading == 0 { // failed loads retry with a backoff of minutes; Ctrl+R retries now
		right = m.st.key.Render("ctrl+r") + m.st.muted.Render(" retry ")
	}
	if m.polling {
		right += m.st.muted.Render("watching running deploys · every " + strconv.Itoa(int(m.pollWait/time.Minute)) + "m ")
	}
	if m.pendingG {
		right += m.st.key.Render("g… ")
	}
	return fit(left, right, m.width)
}

// diffLines is the visible part of the open diff, coloured by line kind.
func (m *Model) diffLines(h int) []string {
	lines := []string{m.st.titleFocus.Render(" " + m.diff.title)}
	res := &m.diff.res
	switch {
	case res.loading && !res.loaded:
		return append(lines, m.st.muted.Render(" Loading diff…"))
	case res.err != nil:
		return append(lines, m.st.bad.Render(" Could not read the diff: "+clean(res.err.Error())))
	case len(res.val) == 0 || len(res.val) == 1 && res.val[0] == "":
		return append(lines, m.st.muted.Render(" No changes."))
	}
	end := min(len(res.val), m.diff.off+h-1)
	for _, l := range res.val[m.diff.off:end] {
		st := m.st.text
		switch {
		case strings.HasPrefix(l, "## "):
			st = m.st.title
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "diff --git"), strings.HasPrefix(l, "index "):
			st = m.st.muted
		case strings.HasPrefix(l, "+"):
			st = m.st.ok
		case strings.HasPrefix(l, "-"):
			st = m.st.bad
		case strings.HasPrefix(l, "@@"):
			st = m.st.accent
		}
		lines = append(lines, " "+st.Render(clean(l)))
	}
	return lines
}

// footer keeps its height while a question or an input shows one line, so the panes do not jump.
func (m *Model) footer() string {
	f := m.footerLines()
	if m.footerRows() == 2 && !strings.Contains(f, "\n") {
		return "\n" + f
	}
	return f
}

func (m *Model) footerLines() string {
	if m.help {
		f := " " + m.st.key.Render("esc") + m.st.muted.Render(" or ") + m.st.key.Render("?") + m.st.muted.Render(" close help")
		if len(m.helpBody()) > m.layout().bodyH-1 {
			f += "  " + m.st.key.Render("j/k") + m.st.muted.Render(" scroll")
		}
		return f
	}
	if m.diff.open {
		return ansi.Truncate(" "+m.st.key.Render("j/k")+m.st.muted.Render(" scroll  ")+m.st.key.Render("pgup/pgdn")+
			m.st.muted.Render(" page  ")+m.st.key.Render("esc")+m.st.muted.Render(" close"), m.width, "…")
	}
	if m.confirm != "" {
		return ansi.Truncate(" "+m.st.warn.Render(m.confirm)+"  "+m.st.key.Render("y")+m.st.muted.Render(" yes · any other key no"), m.width, "…")
	}
	if m.typing() {
		hint := "  enter apply · esc cancel"
		if m.scan.typing {
			hint = "  enter look · esc cancel"
		}
		return ansi.Truncate(" "+m.input.View()+m.st.muted.Render(hint), m.width, "…")
	}
	if m.scan.open {
		pick := m.st.key.Render("space") + m.st.muted.Render(" choose  ") + m.st.key.Render("enter") + m.st.muted.Render(" add  ")
		if m.scan.fix >= 0 {
			pick = m.st.key.Render("enter") + m.st.muted.Render(" use this folder  ")
		}
		return ansi.Truncate(" "+m.st.key.Render("j/k")+m.st.muted.Render(" move  ")+pick+
			m.st.key.Render("tab")+m.st.muted.Render(" look elsewhere  ")+m.st.key.Render("esc")+m.st.muted.Render(" close"), m.width, "…")
	}
	hints, right := m.footerHints()
	room := m.width - lipgloss.Width(right) - 2
	if m.footerRows() == 1 {
		return fit(" "+pack(hints, room), right, m.width)
	}
	// The first line takes the other shortcuts in order while they fit; the second the rest and the basic ones,
	// beside the profile toggle.
	n, used := 0, 1
	for n < len(hints) && !hints[n].basic && used+lipgloss.Width(hints[n].text) < m.width {
		used += lipgloss.Width(hints[n].text) + 2
		n++
	}
	return fit(" "+joinHints(hints[:n]), "", m.width) + "\n" + fit(" "+pack(hints[n:], room), right, m.width)
}

type hint struct {
	text  string
	basic bool
}

// footerHints are the shortcuts the footer offers, in table order, and the profile toggle pinned at its right,
// naming the current mode.
func (m *Model) footerHints() ([]hint, string) {
	var hints []hint
	for _, a := range actions {
		if a.hint && a.available(m) && (!a.item || !m.previewKeys()) {
			h := a.key.Help()
			hints = append(hints, hint{m.st.key.Render(h.Key) + " " + m.st.muted.Render(h.Desc), a.basic})
		}
	}
	return hints, m.st.key.Render("b") + m.st.muted.Render(" profile: "+profileNames[m.profile]+" ")
}

// footerRows is 2 when the shortcuts need a second line and the panes can spare it. The profile header that
// fits above a one-line footer keeps its rows: the footer's second line gives way first.
func (m *Model) footerRows() int {
	if m.footerMemo > 0 {
		return m.footerMemo
	}
	if m.help || m.diff.open || m.scan.open { // they replace the panes and need every row
		return 1
	}
	body := m.bodyFor(2, rowsOf(m.profileFits(1)))
	hints, right := m.footerHints()
	if body < minBodyH || 1+lipgloss.Width(joinHints(hints))+1+lipgloss.Width(right) <= m.width {
		return 1
	}
	return 2
}

func joinHints(hints []hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = h.text
	}
	return strings.Join(parts, "  ")
}

// pack fits the hints into w cells in order; when they do not all fit, the others give way before the basic ones.
func pack(hints []hint, w int) string {
	if lipgloss.Width(joinHints(hints)) <= w {
		return joinHints(hints)
	}
	need := 1 // the closing "  …", less the separator the last hint does not need
	for _, h := range hints {
		if h.basic {
			need += lipgloss.Width(h.text) + 2
		}
	}
	var kept []hint
	full := false
	for _, h := range hints {
		if !h.basic {
			if full || need+lipgloss.Width(h.text)+2 > w {
				full = true
				continue
			}
			need += lipgloss.Width(h.text) + 2
		}
		kept = append(kept, h)
	}
	return joinHints(kept) + "  …"
}
