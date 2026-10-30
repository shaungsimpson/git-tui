package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
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
	l := layout{bodyH: m.height - 4} // header, separator, status, footer
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
	return max(1, h)
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
	rows := []string{m.header(), m.st.sep.Render(strings.Repeat("─", m.width))}
	if l.bar {
		rows = append(rows, m.repoBar())
	}
	if m.help {
		rows = append(rows, box(m.helpLines(), m.width, l.bodyH))
	} else {
		var cols []string
		var widths []int
		if l.sideW > 0 {
			cols, widths = append(cols, box(m.sidebarLines(l.bodyH), l.sideW, l.bodyH)), append(widths, l.sideW)
		}
		if m.inspect && l.detailW == 0 {
			cols, widths = append(cols, box(m.detailPane(l.mainW, l.bodyH), l.mainW, l.bodyH)), append(widths, l.mainW)
		} else {
			cols, widths = append(cols, box(m.mainLines(l.mainW, l.bodyH), l.mainW, l.bodyH)), append(widths, l.mainW)
		}
		if l.detailW > 0 {
			cols, widths = append(cols, box(m.detailPane(l.detailW, l.bodyH), l.detailW, l.bodyH)), append(widths, l.detailW)
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
	right := m.st.muted.Render(m.repoLabel() + " ")
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

func (m *Model) row(segs []seg, w int, selected bool) string {
	if selected {
		var b strings.Builder
		b.WriteString("> ")
		for _, s := range segs {
			b.WriteString(s.s)
		}
		st := m.st.marker
		if m.focus == paneMain {
			st = m.st.selected.Width(w)
		}
		return st.Render(ansi.Truncate(b.String(), w, "…"))
	}
	parts := []string{"  "}
	for _, s := range segs {
		parts = append(parts, s.st.Render(s.s))
	}
	return strings.Join(parts, "")
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
		lines = append(lines, m.row(rows(i), w, i == *cur))
	}
	return lines
}

func (m *Model) commitSegs(c gitx.Commit, w int, showRepo bool) []seg {
	segs := []seg{}
	if w >= 70 {
		segs = append(segs, seg{c.When.Local().Format(dateFmt) + "  ", m.st.muted})
	}
	if showRepo {
		segs = append(segs, seg{pad(clean(c.Repo), 14), m.st.accent})
	}
	return append(segs,
		seg{short(c.Hash) + "  ", m.st.muted},
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
	if w >= 70 && !e.When.IsZero() {
		segs = append(segs, seg{e.When.Local().Format(dateFmt) + "  ", m.st.muted})
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
		lines = append(lines, m.kv("Deployed", e.When.Local().Format(dateFmt), m.st.text))
	}
	if e.Latest != "" {
		lines = append(lines, m.kv("Latest", note+" at "+e.LatestWhen.Local().Format(dateFmt), st))
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
	segs := []seg{{glyph + " ", m.checkStyle(p.Checks)}}
	if showRepo {
		segs = append(segs, seg{pad(clean(p.Repo), 14), m.st.accent})
	}
	segs = append(segs, seg{pad("#"+strconv.Itoa(p.Number), 6), m.st.muted}, seg{pad(review, 8), rst})
	if w >= 70 {
		segs = append(segs, seg{p.Updated.Local().Format(dateFmt) + "  ", m.st.muted})
	}
	return append(segs, seg{clean(p.Title) + "  ", m.st.text}, seg{clean(p.Author), m.st.muted})
}

func (m *Model) prDetail(p gitx.PR) []string {
	review, rst := m.reviewText(p)
	return []string{
		" " + m.st.text.Bold(true).Render(clean(p.Title)),
		m.kv("PR", "#"+strconv.Itoa(p.Number)+" in "+clean(p.Repo), m.st.accent),
		m.kv("Author", clean(p.Author), m.st.text),
		m.kv("Branch", clean(p.Branch), m.st.text),
		m.kv("Review", review, rst),
		m.kv("Checks", checkText[p.Checks], m.checkStyle(p.Checks)),
		m.kv("Updated", p.Updated.Local().Format(dateFmt), m.st.text),
		m.kv("Link", clean(p.URL), m.st.muted),
	}
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
			if w >= 70 {
				segs = append(segs, seg{t.When.Local().Format(dateFmt) + "  ", m.st.muted})
			}
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
		m.kv("Date", c.When.Local().Format(dateFmt), m.st.text),
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
			m.kv("Date", t.When.Local().Format(dateFmt), m.st.text),
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

func (m *Model) helpLines() []string {
	lines := []string{m.st.titleFocus.Render(" Keyboard shortcuts · " + viewNames[m.view]), ""}
	for _, a := range actions {
		h := a.key.Help()
		if h.Key == "" || !a.available(m) {
			continue
		}
		lines = append(lines, "  "+m.st.key.Render(pad(h.Key, 10))+m.st.text.Render(h.Desc))
	}
	var seq []string
	for _, g := range goKeys {
		seq = append(seq, m.st.key.Render(g.key)+" "+m.st.text.Render(viewNames[g.view]))
	}
	lines = append(lines, "  "+m.st.key.Render(pad("g then", 10))+strings.Join(seq, m.st.muted.Render(" · ")))
	return append(lines, "", m.st.muted.Render("  Esc or ? closes this help."))
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
	if m.pendingG {
		right += m.st.key.Render("g… ")
	}
	return fit(left, right, m.width)
}

func (m *Model) footer() string {
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
	return ansi.Truncate(" "+strings.Join(hints, "  "), m.width, "…")
}
