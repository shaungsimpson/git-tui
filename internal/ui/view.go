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
	sideW, mainW, detailW int
	bodyH                 int
}

func (m *Model) layout() layout {
	l := layout{bodyH: m.height - 4 - m.profileRows()} // header, separator, status, footer, profile
	if m.width < minW || m.height < minH {
		l.small = true
		return l
	}
	w := m.width
	if m.width >= 100 {
		l.sideW = sideW
		w -= sideW + 1
	} else {
		l.bar = true
		l.bodyH--
	}
	if m.width >= 120 {
		l.detailW = w * 2 / 5
		w -= l.detailW + 1
	}
	l.mainW = w
	return l
}

// listHeight is the number of list rows visible in view v; Update uses it to keep the cursor on screen.
func (m *Model) listHeight(v viewID) int {
	h := m.layout().bodyH - 1
	if v == viewRepo {
		if info := m.repoInfo(); len(info) > 0 {
			h -= len(info) + 1
		}
	}
	if m.stacked(v) {
		return max(1, h/2)
	}
	return max(1, h)
}

// stackedBelow is the list width under which rows with a title move the title to a second line.
const stackedBelow = 100

// stacked reports whether view v draws each row on two lines.
func (m *Model) stacked(v viewID) bool {
	if m.layout().mainW >= stackedBelow {
		return false
	}
	return v == viewActivity || v == viewPRs || v == viewRepo || v == viewRuns || m.cmp.open && m.cmp.view == v
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = m.st.bg
	v.ForegroundColor = m.st.fg
	v.WindowTitle = "git-tui"
	return v
}

func (m *Model) render() string {
	if m.width == 0 {
		return ""
	}
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
		rows = append(rows, box(m.helpLines(), m.width, l.bodyH))
	} else if m.diff.open {
		rows = append(rows, box(m.diffLines(l.bodyH), m.width, l.bodyH))
	} else {
		var cols []string
		if l.sideW > 0 {
			cols = append(cols, box(m.sidebarLines(l.bodyH), l.sideW, l.bodyH))
		}
		if m.inspect && l.detailW == 0 {
			cols = append(cols, box(m.detailPane(l.mainW, l.bodyH), l.mainW, l.bodyH))
		} else {
			cols = append(cols, box(m.mainLines(l.mainW, l.bodyH), l.mainW, l.bodyH))
		}
		if l.detailW > 0 {
			cols = append(cols, box(m.detailPane(l.detailW, l.bodyH), l.detailW, l.bodyH))
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
func (m *Model) when(t time.Time) string {
	if m.utc {
		return t.UTC().Format(dateFmt)
	}
	return t.Local().Format(dateFmt)
}

// stamp is when for list rows: the full date on wide panes, month-day and time on narrower ones.
func (m *Model) stamp(t time.Time, w int) string {
	if w >= 70 {
		return m.when(t)
	}
	return m.when(t)[len("2006-"):]
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
			tabs = append(tabs, m.st.tab.Render(viewNames[v]))
		}
	}
	right := m.st.muted.Render(m.repoLabel() + " · " + m.zone() + " ")
	full := left + " " + strings.Join(tabs, "  ")
	if lipgloss.Width(full)+lipgloss.Width(right)+1 > m.width {
		full = left + " " + m.st.tabActive.Render(viewNames[m.view])
	}
	return fit(full, right, m.width)
}

func (m *Model) repoBar() string {
	text := fmt.Sprintf(" Repository  < %s >  %d/%d", m.repoLabel(), m.repo, len(m.repos))
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
	lines := []string{m.paneTitle(paneSidebar, " Repositories")}
	names := []string{"All repositories"}
	glyphs := []string{" "}
	for i := range m.repos {
		names = append(names, clean(m.repos[i].cfg.Name))
		glyphs = append(glyphs, m.repoGlyph(&m.repos[i]))
	}
	start := max(0, m.repo-(h-2))
	for i := start; i < len(names) && len(lines) < h; i++ {
		switch {
		case i == m.repo && m.focus == paneSidebar:
			lines = append(lines, m.st.selected.Width(sideW).Render(ansi.Truncate("> "+names[i], sideW, "…")))
		case i == m.repo:
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
		if selected {
			var b strings.Builder
			b.WriteString(lead)
			for _, s := range l {
				b.WriteString(s.s)
			}
			st := m.st.marker
			if m.focus == paneMain {
				st = m.st.selected.Width(w)
			}
			out[i] = st.Render(ansi.Truncate(b.String(), w, "…"))
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
		if info := m.repoInfo(); len(info) > 0 {
			lines = append(lines, info...)
			lines = append(lines, m.st.title.Render(" Recent commits"))
		}
	}
	if n == 0 {
		msg, st := m.emptyMessage()
		for _, l := range m.wrap(msg, w, st) {
			lines = append(lines, " "+l)
		}
		return lines
	}
	end := min(n, *off+m.listHeight(m.view))
	for i := *off; i < end; i++ {
		lines = append(lines, m.row(rows(i), w, i == *cur, m.stacked(m.view))...)
	}
	return lines
}

func (m *Model) commitSegs(c gitx.Commit, w int, showRepo bool) []seg {
	segs := []seg{}
	segs = append(segs, seg{m.stamp(c.When, w) + "  ", m.st.muted})
	if showRepo {
		segs = append(segs, seg{pad(clean(c.Repo), 14), m.st.accent})
	}
	return append(segs,
		seg{short(c.Hash) + "  ", m.st.muted}, brk,
		seg{clean(c.Subject) + "  ", m.st.text},
		seg{clean(c.Author), m.st.muted})
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
		return "…", strings.ReplaceAll(r.Status, "_", " "), m.st.warn
	}
	took := ""
	if !r.Started.IsZero() && r.Updated.After(r.Started) {
		took = " " + r.Updated.Sub(r.Started).Round(time.Second).String()
	}
	switch r.Conclusion {
	case "success":
		return "·", "success" + took, m.st.ok
	case "skipped", "neutral":
		return "-", r.Conclusion, m.st.muted
	}
	return "!", strings.ReplaceAll(r.Conclusion, "_", " ") + took, m.st.bad
}

func (m *Model) runSegs(r gitx.Run, w int, showRepo bool) []seg {
	glyph, state, st := m.runState(r)
	segs := []seg{{glyph + " ", st}}
	if showRepo {
		segs = append(segs, seg{pad(clean(r.Repo), 14), m.st.accent})
	}
	// Status sits next to the workflow so it survives truncation on narrower panes.
	segs = append(segs, seg{pad(clean(r.Workflow), 20), m.st.text}, seg{pad(state, 16), st}, seg{pad(clean(r.Branch), 18), m.st.accent})
	segs = append(segs, seg{m.stamp(r.Started, w) + "  ", m.st.muted}, brk)
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
		m.kv("Commit", r.Commit, m.st.text),
		m.kv("Started", m.when(r.Started), m.st.text),
		m.kv("Link", clean(r.URL), m.st.muted),
		"",
		" " + clean(r.Title),
		"",
	}
	if r.Active() {
		lines = append(lines, m.st.muted.Render(" Reloaded every 10 seconds while a deploy is running."))
	}
	return append(lines, m.st.muted.Render(" Deploy runs: workflows named \"deploy\" (not PR checks) and workflows that recorded a GitHub deployment."))
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
		return "!", "last attempt " + strings.ToLower(e.Latest), m.st.bad
	}
	return "…", strings.ToLower(strings.ReplaceAll(e.Latest, "_", " ")), m.st.warn
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
	segs = append(segs, seg{pad(clean(e.Environment), 17), m.st.text}, seg{pad(short(e.Commit), 7), m.st.muted})
	if !e.When.IsZero() {
		segs = append(segs, seg{m.stamp(e.When, w) + "  ", m.st.muted})
	}
	return append(segs, seg{ref + "  ", m.st.accent}, seg{note, st})
}

func (m *Model) envDetail(e envRow) []string {
	_, note, st := m.envState(e)
	lines := []string{
		" " + m.st.text.Bold(true).Render(clean(e.Repo)+" · "+clean(e.Environment)),
		m.kv("Commit", e.Commit, m.st.accent),
	}
	if e.Ref != "" && e.Ref != e.Commit {
		lines = append(lines, m.kv("Ref", clean(e.Ref), m.st.text))
	}
	if !e.When.IsZero() {
		lines = append(lines, m.kv("Deployed", m.when(e.When), m.st.text))
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

func (m *Model) prSegs(p gitx.PR, w int, showRepo bool) []seg {
	glyph := map[gitx.CheckState]string{gitx.ChecksNone: " ", gitx.ChecksPassing: "·", gitx.ChecksPending: "…", gitx.ChecksFailing: "!"}[p.Checks]
	review, rst := m.reviewText(p)
	star := " " // marks PRs waiting on the viewer's review
	if p.ForMe {
		star = "★"
	}
	segs := []seg{{glyph + " ", m.checkStyle(p.Checks)}, {star + " ", m.st.accent}}
	if showRepo {
		segs = append(segs, seg{pad(clean(p.Repo), 14), m.st.accent})
	}
	segs = append(segs, seg{pad("#"+strconv.Itoa(p.Number), 6), m.st.muted}, seg{pad(review, 8), rst})
	segs = append(segs, seg{m.stamp(p.Updated, w) + "  ", m.st.muted}, brk)
	return append(segs, seg{clean(p.Title) + "  ", m.st.text}, seg{clean(p.Author), m.st.muted})
}

func (m *Model) prDetail(p gitx.PR) []string {
	review, rst := m.reviewText(p)
	lines := []string{
		" " + m.st.text.Bold(true).Render(clean(p.Title)),
		m.kv("PR", "#"+strconv.Itoa(p.Number)+" in "+clean(p.Repo), m.st.accent),
		m.kv("Author", clean(p.Author), m.st.text),
		m.kv("Branch", clean(p.Branch), m.st.text),
		m.kv("Review", review, rst),
		m.kv("Checks", checkText[p.Checks], m.checkStyle(p.Checks)),
		m.kv("Updated", m.when(p.Updated), m.st.text),
		m.kv("Link", clean(p.URL), m.st.muted),
	}
	if p.ForMe {
		lines = slices.Insert(lines, 5, m.kv("Requested", "★ your review", m.st.accent))
	}
	if strings.TrimSpace(p.Body) != "" {
		lines = append(lines, "", m.st.title.Render(" Description"))
		shown, more := bodyLines(p.Body)
		for _, l := range shown {
			lines = append(lines, " "+clean(l))
			for _, link := range termimg.Links(l) {
				lines = append(lines, m.picLines(link)...)
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
	filter := ""
	if f := m.filters[m.view]; f != "" {
		filter = " · filter: " + clean(f)
	}
	if m.cmpOpen() {
		cs := m.cmp.res.val
		return fmt.Sprintf("%s · %d commits", m.cmp.title, len(cs)), func(i int) []seg { return m.commitSegs(cs[i], w, false) }
	}
	switch m.view {
	case viewOverview:
		rows := m.overviewRows()
		return fmt.Sprintf("Overview · %d repositories%s", len(m.repos), filter), func(i int) []seg {
			return m.overviewSegs(&m.repos[rows[i]])
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
			title += " · base " + clean(base)
		}
		nameW := 8
		for _, t := range tags {
			nameW = max(nameW, min(28, len(t.Name)))
		}
		return title, func(i int) []seg {
			t := tags[i]
			name := pad(clean(t.Name), nameW)
			st := m.st.text
			if t.Name == m.tagBase() {
				st = m.st.warn
			}
			segs := []seg{{name, st}}
			segs = append(segs, seg{m.stamp(t.When, w) + "  ", m.st.muted})
			return append(segs, seg{short(t.Commit) + "  ", m.st.muted}, seg{clean(t.Subject), m.st.text})
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
	return []seg{glyph, name, {pad(branch, 24), m.st.accent}, {pad(state, 10), stStyle}, {pad(tracking, 11), m.st.muted}, {subject, m.st.muted}}
}

func (m *Model) emptyMessage() (string, lipgloss.Style) {
	if m.view != viewConnections && len(m.repos) == 0 {
		switch {
		case errors.Is(m.opts.ConfigErr, config.ErrNotFound):
			return "No configuration found at " + clean(m.opts.ConfigPath) +
				". Copy examples/config.example.json there, pass -config FILE, or run with -demo to explore fixture data.", m.st.muted
		case m.opts.ConfigErr != nil:
			return "Configuration error: " + clean(m.opts.ConfigErr.Error()), m.st.bad
		}
		return "The configuration lists no repositories.", m.st.muted
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

// ---- repository info and detail pane ----

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
		m.kv("Tracking", tracking, m.st.text),
		m.kv("Worktree", work, wst))
	if r.summary.err != nil {
		lines = append(lines, m.kv("Refresh", "failed: "+clean(r.summary.err.Error()), m.st.bad))
	}
	return lines
}

// repoInfo is the fixed block above the commit list in the Repository view.
func (m *Model) repoInfo() []string {
	r := m.selected()
	if m.view != viewRepo || r == nil || r.cfg.Status != config.StatusOK {
		return nil
	}
	return m.summaryLines(r)
}

func (m *Model) commitDetail(c gitx.Commit) []string {
	lines := []string{
		m.kv("Commit", c.Hash, m.st.accent),
		m.kv("Repo", clean(c.Repo), m.st.text),
		m.kv("Author", clean(c.Author), m.st.text),
		m.kv("Date", m.when(c.When), m.st.text),
		"",
		" " + m.st.text.Bold(true).Render(clean(c.Subject)),
	}
	if c.Body != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(c.Body, "\n") {
			lines = append(lines, " "+clean(l)) // clean per line so the body keeps its line breaks
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
		r := &m.repos[m.overviewRows()[i]]
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
		hint := "Enter marks this tag as the comparison base."
		switch base := m.tagBase(); {
		case t.Name == base:
			hint = "This is the comparison base. Enter clears it."
		case base != "":
			hint = "Enter lists commits between " + clean(base) + " and this tag."
		}
		return []string{
			m.kv("Tag", clean(t.Name), m.st.accent),
			m.kv("Commit", t.Commit, m.st.text),
			m.kv("Date", m.when(t.When), m.st.text),
			m.kv("Message", clean(t.Subject), m.st.text),
			"",
			m.st.muted.Render(" " + hint),
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

func (m *Model) detailBody(w int) []string {
	var body []string
	for _, l := range m.detailLines() {
		wrapped := lipgloss.NewStyle().Width(max(2, w-1)).Render(strings.TrimPrefix(l, " "))
		for _, part := range strings.Split(wrapped, "\n") {
			body = append(body, " "+part)
		}
	}
	return body
}

// maxDetailOff is how far the detail pane can scroll at the current size.
func (m *Model) maxDetailOff() int {
	l := m.layout()
	w := l.detailW
	if w == 0 {
		w = l.mainW
	}
	return max(0, len(m.detailBody(w))-(l.bodyH-1))
}

func (m *Model) detailPane(w, h int) []string {
	body := m.detailBody(w)
	off := min(m.detOff, max(0, len(body)-(h-1)))
	return append([]string{m.paneTitle(paneDetail, " Details")}, body[off:]...)
}

// ---- help, status and footer ----

// helpLines puts the view jumps first, then the shortcuts in one column, or in two when the terminal is wide
// or one column would not fit. If two still do not fit, the blank separator goes. The footer says how to
// close help, so it needs no line here.
func (m *Model) helpLines() []string {
	lines := []string{fit(m.st.titleFocus.Render(" Keyboard shortcuts · "+viewNames[m.view]), m.st.muted.Render("git-tui "+m.version()+" "), m.width)}
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
	lines = append(lines, "  "+m.st.key.Render(label)+row, "")
	var keys []string
	for _, a := range actions {
		h := a.key.Help()
		if h.Key == "" || !a.available(m) {
			continue
		}
		// 9 fits "shift+tab"; with the longest description a column is 30 cells, half the 60-column minimum.
		keys = append(keys, "  "+m.st.key.Render(pad(h.Key, 9))+m.st.text.Render(h.Desc))
	}
	bodyH := m.layout().bodyH
	if m.width < 100 && len(lines)+len(keys) <= bodyH {
		return append(lines, keys...)
	}
	half, col := (len(keys)+1)/2, m.width/2
	if len(lines)+half > bodyH {
		lines = lines[:len(lines)-1] // drop the blank separator before dropping a shortcut
	}
	for i := range half {
		row := keys[i] + strings.Repeat(" ", max(0, col-lipgloss.Width(keys[i])))
		if i+half < len(keys) {
			row += keys[i+half]
		}
		lines = append(lines, row)
	}
	return lines
}

type health struct {
	loading int
	errs    []string
}

func addHealth[T any](h *health, name string, s *slot[T]) {
	if s.loading {
		h.loading++
	}
	if s.err != nil {
		h.errs = append(h.errs, clean(name)+": "+clean(s.err.Error()))
	}
}

func (m *Model) health() health {
	var h health
	switch m.view {
	case viewOverview:
		for i := range m.repos {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].summary)
		}
	case viewActivity:
		for _, i := range m.scope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].log)
		}
	case viewPRs:
		for _, i := range m.scope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].prs)
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
		for _, i := range m.scope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].deps)
		}
	case viewRuns:
		for _, i := range m.scope() {
			addHealth(&h, m.repos[i].cfg.Name, &m.repos[i].runs)
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
	switch {
	case m.flash != "":
		left = m.st.accent.Render(" " + clean(m.flash))
	case h.loading > 0:
		left = m.st.muted.Render(fmt.Sprintf(" Loading (%d pending)…", h.loading))
	case len(h.errs) == 1:
		left = m.st.bad.Render(" " + h.errs[0])
	case len(h.errs) > 1:
		left = m.st.bad.Render(fmt.Sprintf(" %d reads failed. First: %s", len(h.errs), h.errs[0]))
	case m.opts.Demo:
		left = m.st.warn.Render(" Demo mode: fixture data only, no repositories are read")
	case m.opts.ConfigErr != nil:
		left = m.st.muted.Render(" No repositories loaded")
	default:
		left = m.st.muted.Render(" Read-only local git · " + clean(m.opts.ConfigPath))
	}
	right := ""
	if len(h.errs) > 0 && h.loading == 0 { // failed loads only retry on Ctrl+R, so say so
		right = m.st.key.Render("ctrl+r") + m.st.muted.Render(" retry ")
	}
	if m.polling {
		right += m.st.muted.Render("watching running deploys ")
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

func (m *Model) footer() string {
	if m.help {
		return " " + m.st.key.Render("esc") + m.st.muted.Render(" or ") + m.st.key.Render("?") + m.st.muted.Render(" close help")
	}
	if m.diff.open {
		return ansi.Truncate(" "+m.st.key.Render("j/k")+m.st.muted.Render(" scroll  ")+m.st.key.Render("pgup/pgdn")+
			m.st.muted.Render(" page  ")+m.st.key.Render("esc")+m.st.muted.Render(" close"), m.width, "…")
	}
	if m.filtering {
		return ansi.Truncate(" "+m.input.View()+m.st.muted.Render("  enter apply · esc cancel"), m.width, "…")
	}
	var hints []string
	for _, a := range actions {
		if a.hint && a.available(m) {
			h := a.key.Help()
			hints = append(hints, m.st.key.Render(h.Key)+" "+m.st.muted.Render(h.Desc))
		}
	}
	// The profile toggle stays visible on narrow footers, naming the current mode; the hints give way first.
	right := m.st.key.Render("b") + m.st.muted.Render(" profile: "+profileNames[m.profile]+" ")
	return fit(" "+strings.Join(hints, "  "), right, m.width)
}
