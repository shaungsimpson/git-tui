package ui

import (
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

// mdLine is one drawn line of a PR description, or a picture drawn where it stands.
type mdLine struct {
	text string
	pic  *termimg.Link
}

// mdKey is one formatting of a description or comment, kept so scrolling and redraws do not parse it again.
type mdKey struct {
	body       string
	w          int
	dark, open bool
}

var mdParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

var (
	commentRE    = regexp.MustCompile(`(?s)<!--.*?-->`)
	summaryRE    = regexp.MustCompile(`(?is)<summary[^>]*>(.*?)</summary>`)
	detailsTagRE = regexp.MustCompile(`(?i)</?details\b`)
	anchorRE     = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	imgRE        = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	tagRE        = regexp.MustCompile(`<[^>]*>`)
	brRE         = regexp.MustCompile(`(?i)<br\s*/?>`)
	alertRE      = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*$`)
)

// descLines is a PR's description as the detail page draws it, formatted or raw, and how many lines are left out.
func (m *Model) descLines(p gitx.PR) ([]mdLine, int) { return m.mdLines(p.Body, m.openFor == p.URL) }

// mdLines is a description or comment, formatted or raw, and how many lines are left out.
func (m *Model) mdLines(body string, open bool) ([]mdLine, int) {
	if m.rawMD {
		shown, more := bodyLines(body)
		var out []mdLine
		for _, l := range shown {
			out = append(out, mdLine{text: " " + m.linkify(clean(l), lipgloss.NewStyle())})
			for _, link := range termimg.Links(l) {
				out = append(out, mdLine{pic: &link})
			}
		}
		return out, more
	}
	lines := m.markdown(body, m.width, open)
	if len(lines) > maxBodyLines {
		return lines[:maxBodyLines], len(lines) - maxBodyLines
	}
	return lines, 0
}

// markdown formats a description w columns wide; open shows the contents of its <details> sections.
func (m *Model) markdown(body string, w int, open bool) []mdLine {
	k := mdKey{body, w, m.dark, open}
	if lines, ok := m.mdc[k]; ok {
		return lines
	}
	if len(m.mdc) > 200 { // old widths and pages; what is on screen formats again once
		clear(m.mdc)
	}
	src := []byte(body)
	r := &mdRender{m: m, src: src, w: w, open: open, out: []mdLine{}}
	r.blocks(mdParser.Parse(text.NewReader(src)), " ", " ", false)
	for len(r.out) > 0 && r.out[len(r.out)-1].pic == nil && strings.TrimSpace(ansi.Strip(r.out[len(r.out)-1].text)) == "" {
		r.out = r.out[:len(r.out)-1]
	}
	m.mdc[k] = r.out
	return r.out
}

type mdRender struct {
	m         *Model
	src       []byte
	w         int
	open      bool
	out       []mdLine
	href      string // the target of an open <a href>, for the text up to its </a>
	skipAlert bool   // the next paragraph starts with a > [!NOTE] marker, drawn as a label instead
}

// span is a run of inline text in one style, linked when url is set.
type span struct {
	text string
	st   lipgloss.Style
	url  string
}

func (r *mdRender) line(s string) { r.out = append(r.out, mdLine{text: s}) }

// blocks draws n's children: first prefixes the first line drawn, rest every other. Blocks are a blank line apart,
// except in a tight list. <details> sections show their summary, and their contents only when open.
func (r *mdRender) blocks(n ast.Node, first, rest string, tight bool) {
	drawn, skip := 0, 0
	// draw runs one block's drawing after a blank line, and takes both back when the block drew nothing.
	draw := func(f func(first string)) {
		before, blank := len(r.out), drawn > 0 && !tight
		if blank {
			r.line(rest)
		}
		f(r.pick(drawn, first, rest))
		if len(r.out) == before || blank && len(r.out) == before+1 {
			r.out = r.out[:before] // such as a comment: no blank line for it either
			return
		}
		drawn++
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		h, ok := c.(*ast.HTMLBlock)
		if !ok {
			if skip == 0 {
				draw(func(p string) { r.block(c, p, rest) })
			} else if raw := r.innerHTML(c); closedAt(raw, skip) >= 0 { // a </details> within a paragraph or list
				skip = 0
			} else {
				skip += detailsDepth(raw)
			}
			continue
		}
		raw := r.htmlText(h)
		if skip > 0 { // inside a closed section: what follows its end in this block still shows
			at := closedAt(raw, skip)
			if at < 0 {
				skip += detailsDepth(raw)
				continue
			}
			skip, raw = 0, raw[at:]
		}
		if depth := detailsDepth(raw); depth > 0 {
			// A section starts here, its contents following as blocks of their own.
			if !r.open {
				skip = depth
			}
			draw(func(p string) { r.section(raw, p, rest) })
			continue
		}
		draw(func(p string) { r.html(raw, p, rest) })
	}
}

// closedAt is where in raw the sections being skipped, depth deep, have all closed, just past the last </details>
// they need; -1 when they are still open at its end.
func closedAt(raw string, depth int) int {
	for _, loc := range detailsTagRE.FindAllStringIndex(raw, -1) {
		if raw[loc[0]+1] == '/' {
			depth--
		} else {
			depth++
		}
		if depth == 0 {
			if end := strings.IndexByte(raw[loc[1]:], '>'); end >= 0 {
				return loc[1] + end + 1
			}
			return len(raw)
		}
	}
	return -1
}

// detailsDepth is how many more <details> raw opens than it closes.
func detailsDepth(raw string) int {
	return strings.Count(strings.ToLower(raw), "<details") - strings.Count(strings.ToLower(raw), "</details")
}

// innerHTML is the raw HTML anywhere inside n, in order.
func (r *mdRender) innerHTML(n ast.Node) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.HTMLBlock:
			b.WriteString(r.htmlText(c))
		case *ast.RawHTML:
			for i := range c.Segments.Len() {
				seg := c.Segments.At(i)
				b.Write(seg.Value(r.src))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func (r *mdRender) pick(drawn int, first, rest string) string {
	if drawn == 0 {
		return first
	}
	return rest
}

// avail is the width left after a prefix.
func (r *mdRender) avail(prefix string) int { return max(4, r.w-lipgloss.Width(prefix)) }

func (r *mdRender) block(n ast.Node, first, rest string) {
	st := r.m.st
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		r.para(n, first, rest, st.text)
	case *ast.Heading:
		h := st.text.Bold(true)
		if n.Level <= 2 {
			h = st.accent.Bold(true)
		}
		r.para(n, first, rest, h)
	case *ast.ThematicBreak:
		r.line(first + st.muted.Render(strings.Repeat("─", min(r.avail(first), 40))))
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		lines := n.Lines()
		for i := range lines.Len() {
			l := clean(strings.ReplaceAll(strings.TrimRight(string(segValue(lines.At(i), r.src)), "\r\n"), "\t", "    "))
			r.line(r.pick(i, first, rest) + "  " + st.id.Render(ansi.Truncate(l, r.avail(rest)-2, "…")))
		}
	case *ast.Blockquote:
		bar := st.muted.Render("│ ")
		if p := n.FirstChild(); p != nil && p.Lines().Len() > 0 {
			if a := alertRE.FindStringSubmatch(strings.TrimSpace(string(segValue(p.Lines().At(0), r.src)))); a != nil {
				label := map[string]lipgloss.Style{"NOTE": st.accent, "TIP": st.ok, "IMPORTANT": st.authors[1], "WARNING": st.warn, "CAUTION": st.bad}[a[1]]
				r.line(first + bar + label.Bold(true).Render(strings.ToUpper(a[1][:1])+strings.ToLower(a[1][1:])))
				r.skipAlert, first = true, rest
			}
		}
		r.blocks(n, first+bar, rest+bar, false)
	case *ast.List:
		num := n.Start
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			marker := "• "
			if n.IsOrdered() {
				marker = strconv.Itoa(num) + ". "
				num++
			}
			if box := taskBox(item); box != nil {
				marker = "☐ "
				if box.IsChecked {
					marker = "☑ "
				}
			}
			if item != n.FirstChild() {
				if !n.IsTight {
					r.line(rest)
				}
				first = rest
			}
			r.blocks(item, first+st.muted.Render(marker), rest+strings.Repeat(" ", lipgloss.Width(marker)), n.IsTight)
		}
	case *east.Table:
		r.table(n, first, rest)
	default: // anything not drawn otherwise shows as its text, so nothing silently disappears
		lines := n.Lines()
		for i := range lines.Len() {
			r.wrap(r.pick(i, first, rest), rest, st.text.Render(clean(string(segValue(lines.At(i), r.src)))))
		}
	}
}

func segValue(s text.Segment, src []byte) []byte { return s.Value(src) }

// canMarkdown offers m on a PR's detail page when it has a description or comments.
func (m *Model) canMarkdown() bool {
	p, ok := m.selectedPR()
	return ok && m.inspect && (strings.TrimSpace(p.Body) != "" || len(m.shownComments(p)) > 0)
}

// canSections offers z when the formatted page has <details> sections or bots' comments to open or close.
func (m *Model) canSections() bool {
	p, _ := m.selectedPR()
	return m.canMarkdown() && !m.rawMD && (strings.Contains(strings.ToLower(p.Body), "<details") ||
		slices.ContainsFunc(m.shownComments(p), func(c gitx.Comment) bool { return c.Bot }))
}

// shownComments is a PR's loaded comments, or none.
func (m *Model) shownComments(p gitx.PR) []gitx.Comment {
	if s := m.comments[p.URL]; s != nil {
		return s.val.list
	}
	return nil
}

func taskBox(item ast.Node) *east.TaskCheckBox {
	if p := item.FirstChild(); p != nil {
		if b, ok := p.FirstChild().(*east.TaskCheckBox); ok {
			return b
		}
	}
	return nil
}

// para draws inline content in style st, wrapped to the width, a hard line break starting a new line. A line
// holding only pictures draws them; a picture sharing its line with text shows its alt text.
func (r *mdRender) para(n ast.Node, first, rest string, st lipgloss.Style) {
	var spans []span
	var line []ast.Node
	drawn := 0
	flush := func() {
		for i, l := range r.draw(spans) {
			if i > 0 || strings.TrimSpace(ansi.Strip(l)) != "" {
				r.wrap(r.pick(drawn, first, rest), rest, l)
				drawn++
			}
		}
		spans = nil
	}
	end := func() {
		if pics := r.soleImages(line); pics != nil {
			flush()
			for i := range pics {
				r.out = append(r.out, mdLine{pic: &pics[i]})
			}
			drawn++
		} else {
			for _, c := range line {
				r.inline(c, st, "", &spans)
			}
		}
		line = nil
	}
	skip := r.skipAlert             // the [!NOTE] marker is the first line; the label stands for it
	r.skipAlert, r.href = false, "" // an <a> left open, as in a body GitHub cut short, ends with its paragraph
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		line = append(line, c)
		if t, ok := c.(*ast.Text); ok && (t.SoftLineBreak() || t.HardLineBreak()) {
			if skip {
				line, skip = nil, false
				continue
			}
			end()
		}
	}
	if skip { // the marker was the whole paragraph, as when a > line on its own follows it
		line = nil
	}
	end()
	flush()
}

// wrap draws s on lines that fit the width, first before the first, rest before the others.
func (r *mdRender) wrap(first, rest, s string) {
	for i, l := range strings.Split(lipgloss.Wrap(s, r.avail(rest), ""), "\n") {
		r.line(r.pick(i, first, rest) + l)
	}
}

// inline adds the spans of one inline node in style st, linked to url when it is set.
func (r *mdRender) inline(n ast.Node, st lipgloss.Style, url string, out *[]span) {
	add := func(s string, st lipgloss.Style, url string) {
		if url == "" {
			url = r.href
		}
		*out = append(*out, span{s, st, url})
	}
	switch n := n.(type) {
	case *ast.Text:
		v := string(n.Value(r.src))
		if !n.IsRaw() {
			v = html.UnescapeString(string(util.UnescapePunctuations([]byte(v))))
		}
		add(clean(v), st, url)
		switch {
		case n.HardLineBreak():
			add("\n", st, "")
		case n.SoftLineBreak():
			add(" ", st, url)
		}
	case *ast.String:
		add(clean(string(n.Value)), st, url)
	case *ast.CodeSpan:
		r.children(n, r.m.st.id, url, out)
	case *ast.Emphasis:
		if n.Level >= 2 {
			r.children(n, st.Bold(true), url, out)
		} else {
			r.children(n, st.Italic(true), url, out)
		}
	case *east.Strikethrough:
		r.children(n, st.Strikethrough(true), url, out)
	case *ast.Link:
		r.children(n, st, target(string(n.Destination)), out)
	case *ast.AutoLink:
		add(clean(string(n.Label(r.src))), st, target(string(n.URL(r.src))))
	case *ast.Image: // a picture within text shows its alt text; one on its own is drawn (soleImages)
		var alt []span
		r.children(n, r.m.st.muted, "", &alt)
		s := strings.TrimSpace(ansi.Strip(strings.Join(r.draw(alt), " ")))
		if s == "" {
			s = "image"
		}
		add(s, r.m.st.muted, url)
	case *ast.RawHTML:
		tag := clean(string(n.Segments.Value(r.src)))
		lower := strings.ToLower(tag)
		switch {
		case strings.HasPrefix(lower, "<br"):
			add("\n", st, "")
		case strings.HasPrefix(lower, "<img"):
			s := clean(termimg.Attr(tag, "alt"))
			if s == "" {
				s = "image"
			}
			add(s, r.m.st.muted, url)
		case strings.HasPrefix(lower, "<a "):
			r.href = target(termimg.Attr(tag, "href"))
		case strings.HasPrefix(lower, "</a"):
			r.href = ""
		}
	case *east.TaskCheckBox: // its list item's marker shows it
	default:
		r.children(n, st, url, out)
	}
}

func (r *mdRender) children(n ast.Node, st lipgloss.Style, url string, out *[]span) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.inline(c, st, url, out)
	}
}

// target is a link's destination when it may be opened: https only, as everywhere in the app.
func target(dest string) string {
	if d := clean(dest); strings.HasPrefix(d, "https://") && len(d) > len("https://") {
		return d
	}
	return ""
}

// draw renders spans, one string per line between hard line breaks.
func (r *mdRender) draw(spans []span) []string {
	var lines []string
	var b strings.Builder
	for _, s := range spans {
		if s.text == "\n" {
			lines = append(lines, b.String())
			b.Reset()
			continue
		}
		st := s.st
		if s.url != "" {
			st = r.m.linkStyle(s.url).Bold(st.GetBold()).Italic(st.GetItalic()).Strikethrough(st.GetStrikethrough())
		}
		b.WriteString(st.Render(s.text))
	}
	return append(lines, b.String())
}

// soleImages is the pictures of a line made of nothing else, linked or not; nil for any other line.
func (r *mdRender) soleImages(line []ast.Node) []termimg.Link {
	var pics []termimg.Link
	for _, c := range line {
		if l, ok := c.(*ast.Link); ok && l.ChildCount() == 1 {
			c = l.FirstChild()
		}
		switch c := c.(type) {
		case *ast.Image:
			var alt []span
			r.children(c, r.m.st.muted, "", &alt)
			pics = append(pics, termimg.Link{Alt: strings.TrimSpace(ansi.Strip(strings.Join(r.draw(alt), " "))), URL: string(c.Destination)})
		case *ast.RawHTML:
			tag := strings.ToLower(string(c.Segments.Value(r.src)))
			switch {
			case strings.HasPrefix(tag, "<img"):
				raw := string(c.Segments.Value(r.src))
				pics = append(pics, termimg.Link{Alt: termimg.Attr(raw, "alt"), URL: termimg.Attr(raw, "src")})
			case !strings.HasPrefix(tag, "<a ") && !strings.HasPrefix(tag, "</a") && !strings.HasPrefix(tag, "<br"):
				return nil
			}
		case *ast.Text:
			if strings.TrimSpace(string(c.Value(r.src))) != "" {
				return nil
			}
		default:
			return nil
		}
	}
	return pics
}

func (r *mdRender) htmlText(h *ast.HTMLBlock) string {
	s := string(h.Lines().Value(r.src))
	if h.HasClosure() {
		s += string(h.ClosureLine.Value(r.src))
	}
	return commentRE.ReplaceAllString(s, "")
}

// html draws a block of raw HTML. Each whole <details> section in it, as Dependabot writes them with no blank
// lines, shows its summary, and its contents only when open.
func (r *mdRender) html(raw, first, rest string) {
	drawn, last := 0, 0
	for _, loc := range detailsTagRE.FindAllStringIndex(raw, -1) {
		if loc[0] < last || raw[loc[0]+1] == '/' { // inside the section just drawn, or a stray </details>
			continue
		}
		end := closedAt(raw[loc[0]:], 0) // counted, so a nested section's </details> does not end this one
		if end < 0 {
			break
		}
		drawn = r.htmlLines(raw[last:loc[0]], first, rest, drawn)
		r.section(raw[loc[0]:loc[0]+end], r.pick(drawn, first, rest), rest)
		drawn++
		last = loc[0] + end
	}
	r.htmlLines(raw[last:], first, rest, drawn)
}

// section draws a <details> section's summary, ▸ closed or ▾ open, then when open the HTML after the summary.
func (r *mdRender) section(raw, first, rest string) {
	label, after := "Details", raw
	if s := summaryRE.FindStringSubmatchIndex(raw); s != nil {
		label, after = strings.TrimSpace(htmlPlain(raw[s[2]:s[3]])), raw[s[1]:]
	}
	marker := "▸ "
	if r.open {
		marker = "▾ "
	}
	r.line(first + r.m.st.muted.Render(marker) + r.m.st.text.Bold(true).Render(ansi.Truncate(label, r.avail(rest)-2, "…")))
	if r.open {
		r.htmlLines(after, rest, rest, 1)
	}
}

// htmlLines draws HTML's pictures, then its text line by line with the tags left out and its https links kept;
// drawn is how many lines came before, for the prefix. It returns the new count.
func (r *mdRender) htmlLines(raw, first, rest string, drawn int) int {
	for _, tag := range imgRE.FindAllString(raw, -1) {
		r.out = append(r.out, mdLine{pic: &termimg.Link{Alt: termimg.Attr(tag, "alt"), URL: termimg.Attr(tag, "src")}})
		drawn++
	}
	for _, l := range strings.Split(brRE.ReplaceAllString(raw, "\n"), "\n") {
		var spans []span
		last := 0
		for _, a := range anchorRE.FindAllStringSubmatchIndex(l, -1) {
			spans = append(spans, span{htmlPlain(l[last:a[0]]), r.m.st.text, ""}, span{htmlPlain(l[a[4]:a[5]]), r.m.st.text, target(termimg.Attr(l[a[2]:a[3]], "href"))})
			last = a[1]
		}
		spans = append(spans, span{htmlPlain(l[last:]), r.m.st.text, ""})
		spans[0].text = strings.TrimLeft(spans[0].text, " ")
		spans[len(spans)-1].text = strings.TrimRight(spans[len(spans)-1].text, " ")
		spans = slices.DeleteFunc(spans, func(s span) bool { return s.text == "" })
		if len(spans) > 0 {
			r.wrap(r.pick(drawn, first, rest), rest, r.draw(spans)[0])
			drawn++
		}
	}
	return drawn
}

func htmlPlain(s string) string { return clean(html.UnescapeString(tagRE.ReplaceAllString(s, ""))) }

// table lines up a table's columns in the width, shrinking the widest with …; when a column would get narrower
// than minCol, each row is shown as its own block of header and value lines instead.
func (r *mdRender) table(t *east.Table, first, rest string) {
	const gap, minCol = 2, 8
	var rows [][]string
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		st := r.m.st.text
		if _, head := row.(*east.TableHeader); head {
			st = st.Bold(true)
		}
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			var spans []span
			r.children(c, st, "", &spans)
			cells = append(cells, strings.TrimSpace(strings.Join(r.draw(spans), " ")))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for j, c := range row {
			if j < len(widths) {
				widths[j] = max(widths[j], lipgloss.Width(c))
			}
		}
	}
	avail := r.avail(rest)
	if cols, ok := fitColumns(widths, avail, gap, minCol); ok {
		for i, row := range rows {
			var b strings.Builder
			for j, w := range cols {
				c := ""
				if j < len(row) {
					c = ansi.Truncate(row[j], w, "…")
				}
				b.WriteString(c)
				if j < len(cols)-1 {
					b.WriteString(strings.Repeat(" ", w-lipgloss.Width(c)+gap))
				}
			}
			r.line(r.pick(i, first, rest) + b.String())
		}
		return
	}
	heads := make([]string, len(rows[0]))
	hw := 0
	for j, h := range rows[0] {
		heads[j] = ansi.Strip(h)
		hw = max(hw, lipgloss.Width(heads[j]))
	}
	hw = min(hw, avail/3)
	drawn := 0
	for _, row := range rows[1:] {
		if drawn > 0 {
			r.line(rest)
		}
		for j, c := range row {
			if j < len(heads) && c != "" {
				h := ansi.Truncate(heads[j], hw, "…")
				r.line(r.pick(drawn, first, rest) + r.m.st.muted.Render(h+strings.Repeat(" ", hw-lipgloss.Width(h)+gap)) + ansi.Truncate(c, avail-hw-gap, "…"))
				drawn++
			}
		}
	}
}

// fitColumns shrinks the widest columns until the row fits in avail, or reports false when one would drop below minCol.
func fitColumns(widths []int, avail, gap, minCol int) ([]int, bool) {
	cols := append([]int(nil), widths...)
	total := gap * (len(cols) - 1)
	for _, w := range cols {
		total += w
	}
	for total > avail {
		j := 0
		for k, w := range cols {
			if w > cols[j] {
				j = k
			}
		}
		if cols[j] <= minCol {
			return nil, false
		}
		cols[j]--
		total--
	}
	return cols, true
}
