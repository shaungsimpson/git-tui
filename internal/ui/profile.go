package ui

import (
	"context"
	"image"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

// Profile is how the header about the signed-in user is drawn. Each mode gives way to a simpler one
// when the terminal cannot show it: see profileShown and avatarArt.
type Profile int

const (
	ProfileOff   Profile = iota
	ProfileFull          // avatar (picture, colour blocks or initials) beside a banner of details and counts
	ProfileASCII         // the same banner in plain ASCII, the avatar drawn with characters
	ProfileText          // one line of counts
	profileCount
)

var profileNames = [profileCount]string{"off", "full", "ascii", "text"}

// ParseProfile reads a -profile flag value.
func ParseProfile(s string) (Profile, bool) {
	i := slices.Index(profileNames[:], strings.ToLower(s))
	return Profile(max(i, 0)), i >= 0
}

const (
	avatarCols = 10 // a square picture this many cells wide is bannerRows cells tall
	bannerRows = 5
	minBodyH   = 10  // the banner gives way before the panes get shorter than this
	avatarPx   = 128 // GitHub serves avatars at the requested size; the picture is scaled to the cells
	firstFrame = 150 * time.Millisecond
)

// avatarID is the terminal image ID of the avatar; PR pictures count up from picBase+1.
const avatarID = picBase

// avatarState is the viewer's GitHub picture. pic.ID is set once the terminal has stored it.
type avatarState struct {
	url     string
	loading bool
	img     image.Image
	pic     termimg.Picture
	err     error
}

type avatarMsg struct {
	url string
	img image.Image
	pic termimg.Picture
	err error
}

// fetchAvatar loads the viewer's picture from the source; a variable so tests never touch the network.
var fetchAvatar = func(ctx context.Context, src gitx.Source, raw string) (image.Image, error) {
	return src.Image(ctx, raw)
}

// profileShown is the mode that fits: a banner that would squeeze the panes becomes one line, then nothing.
func (m *Model) profileShown() Profile {
	body := m.height - 4 // header, separator, status, footer
	if m.width < 100 {
		body-- // the repository bar
	}
	switch {
	case m.profile == ProfileOff:
		return ProfileOff
	case m.profile != ProfileText && body-bannerRows >= minBodyH:
		return m.profile
	case body-1 >= minBodyH:
		return ProfileText
	}
	return ProfileOff
}

func (m *Model) profileRows() int {
	switch m.profileShown() {
	case ProfileOff:
		return 0
	case ProfileText:
		return 1
	}
	return bannerRows
}

func (m *Model) cycleProfile() tea.Cmd {
	m.profile = (m.profile + 1) % profileCount
	m.flash = "Profile header: " + profileNames[m.profile] + "."
	if shown := m.profileShown(); shown != m.profile {
		m.flash = "Profile header: " + profileNames[m.profile] + " (the window is too short; showing " + profileNames[shown] + ")."
	}
	m.clampAll()
	return tea.Batch(m.ensure(false), m.ensureAvatar())
}

// profileLoads starts what the header counts: the viewer, and every repository's PRs, commits and status.
// A refresh skips slots already loading, so it never cancels the view's own requests for the same data.
func (m *Model) profileLoads(force bool) []tea.Cmd {
	if m.profile == ProfileOff {
		return nil
	}
	dir := ""
	if i := slices.IndexFunc(m.repos, func(r repoState) bool { return r.cfg.Status == config.StatusOK }); i >= 0 {
		dir = m.repos[i].cfg.Path
	}
	src := m.opts.Source
	cmds := []tea.Cmd{start(m, &m.me, force, kindViewer, -1, func(ctx context.Context) (gitx.Viewer, error) {
		return src.Viewer(ctx, dir)
	})}
	for i := range m.repos {
		r := &m.repos[i]
		cmds = append(cmds, m.load(i, kindPRs, force && !r.prs.loading), m.load(i, kindLog, force && !r.log.loading),
			m.load(i, kindSummary, force && !r.summary.loading))
	}
	return cmds
}

// ensureAvatar downloads the viewer's picture once per address. With Kitty graphics the picture is also
// encoded here, off the update loop.
func (m *Model) ensureAvatar() tea.Cmd {
	raw := m.me.val.AvatarURL
	if m.profile == ProfileOff || raw == "" || raw == m.avatar.url {
		return nil
	}
	m.avatar = avatarState{url: raw, loading: true, pic: m.avatar.pic} // a new picture reuses the ID
	// shortcut: the border keeps the accent colour of the theme at load time; re-encode on F2 if that jars.
	ctx, src, kitty, border := m.ctx, m.opts.Source, m.opts.Images, m.st.accent.GetForeground()
	return func() tea.Msg {
		img, err := fetchAvatar(ctx, src, sized(raw, avatarPx))
		msg := avatarMsg{url: raw, img: img, err: err}
		if err == nil && kitty {
			// Only the picture is a circle: at 10×5 cells the block and character fallbacks are too coarse to show it.
			rounded := termimg.Rounded(img, 0.5, avatarPx/32, border) // a radius of half the side is a circle
			msg.pic, _ = termimg.EncodeImage(rounded, avatarID)       // without it the avatar falls back to blocks
		}
		return msg
	}
}

func (m *Model) applyAvatar(msg avatarMsg) tea.Cmd {
	if msg.url != m.avatar.url {
		return nil
	}
	m.avatar.loading, m.avatar.img, m.avatar.err = false, msg.img, msg.err
	if msg.pic.Transmit == "" {
		return nil
	}
	send := msg.pic.Transmit + msg.pic.Place(avatarCols) // fixed size, so it never needs placing again
	msg.pic.Transmit = ""
	m.avatar.pic = msg.pic
	// Raw output is written at once, but the switch to the alternate screen waits for the first frame, and the
	// terminal keeps separate images per screen. A picture that loads instantly (demo) would land on the
	// main screen and never show, so it is sent after the first frame has had time to draw.
	return tea.Tick(firstFrame, func(time.Time) tea.Msg { return tea.RawMsg{Msg: send} })
}

// sized asks GitHub for the avatar at px pixels square.
func sized(raw string, px int) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("s", strconv.Itoa(px))
	u.RawQuery = q.Encode()
	return u.String()
}

// profileStats is what the header counts across every configured repository, whatever the sidebar selects.
type profileStats struct {
	requested, mine, failing, changes, commits, dirty int
	loading                                           bool
}

func (m *Model) profileStats(now time.Time) profileStats {
	v := m.me.val
	s := profileStats{loading: m.me.loading}
	week := now.AddDate(0, 0, -7)
	isMe := func(name string) bool { return name != "" && (name == v.GitName || name == v.Name) }
	for _, r := range m.repos {
		s.loading = s.loading || r.prs.loading || r.log.loading || r.summary.loading
		for _, p := range r.prs.val {
			if p.ForMe {
				s.requested++
			}
			if v.Login != "" && strings.EqualFold(p.Author, v.Login) {
				s.mine++
				if p.Checks == gitx.ChecksFailing {
					s.failing++
				}
				if p.Review == "CHANGES_REQUESTED" {
					s.changes++
				}
			}
		}
		for _, c := range r.log.val {
			if c.When.After(week) && isMe(c.Author) {
				s.commits++
			}
		}
		if r.summary.loaded && !r.summary.val.Clean() {
			s.dirty++
		}
	}
	return s
}

// glyphs mark each count; the ASCII set is for terminals without Unicode.
type glyphs struct{ star, mine, fail, changes, commit, dirty, sep, more string }

var (
	unicodeGlyphs = glyphs{"★", "↑", "✗", "↺", "●", "±", " · ", "…"}
	asciiGlyphs   = glyphs{"*", "^", "x", "~", "o", "+", " | ", "..."}
)

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// count is "★ 3 review requests": the number stands out when it is not zero.
func (m *Model) count(n int, glyph, label string, style lipgloss.Style) string {
	num := strconv.Itoa(n)
	if glyph != "" {
		num = glyph + " " + num
	}
	if n == 0 {
		return m.st.muted.Render(num + " " + label)
	}
	return style.Bold(true).Render(num) + " " + m.st.muted.Render(label)
}

// counts are the banner's six counts. The one-line text header has no glyphs and leaves out zeros to fit,
// except review requests, the count it is for.
func (m *Model) counts(s profileStats, g glyphs, text bool) []string {
	var out []string
	add := func(n int, glyph, label string, style lipgloss.Style) {
		switch {
		case !text:
			out = append(out, m.count(n, glyph, label, style))
		case n > 0 || len(out) == 0:
			out = append(out, m.count(n, "", label, style))
		}
	}
	add(s.requested, g.star, plural(s.requested, "review request", "review requests"), m.st.warn)
	add(s.mine, g.mine, plural(s.mine, "open PR by you", "open PRs by you"), m.st.accent)
	add(s.failing, g.fail, "failing", m.st.bad)
	add(s.changes, g.changes, "changes requested", m.st.warn)
	add(s.commits, g.commit, plural(s.commits, "commit this week", "commits this week"), m.st.ok)
	add(s.dirty, g.dirty, plural(s.dirty, "repository with changes", "repositories with changes"), m.st.warn)
	return out
}

// profileLines is the header above the tabs, or nil when it is hidden.
func (m *Model) profileLines() []string {
	shown := m.profileShown()
	if shown == ProfileOff {
		return nil
	}
	now := time.Now()
	if m.opts.Demo {
		now = gitx.DemoNow
	}
	v, s := m.me.val, m.profileStats(now)
	g := unicodeGlyphs
	if shown != ProfileFull {
		g = asciiGlyphs
	}
	sep := m.st.muted.Render(g.sep)
	busy := ""
	if s.loading {
		busy = m.st.muted.Render(" " + g.more)
	}
	if shown == ProfileText {
		who := m.st.marker.Render(" " + clean(firstOf(prefix("@", v.Login), v.GitName, "You")))
		return []string{who + sep + strings.Join(m.counts(s, g, true), sep) + busy}
	}

	name := m.st.marker.Render(clean(firstOf(v.Name, v.GitName, v.Login, "You")))
	if v.Login != "" {
		name += " " + m.st.muted.Render("@"+clean(v.Login))
	}
	if where := joinSet(g.sep, clean(v.Company), clean(v.Location)); where != "" {
		name += "   " + m.st.muted.Render(where)
	}
	about := m.st.text.Render(clean(v.Bio))
	switch {
	case m.me.err != nil:
		about = m.st.bad.Render("Could not load your profile: " + clean(m.me.err.Error()))
	case v.GitHubErr != "":
		about = m.st.warn.Render("GitHub not connected: " + clean(v.GitHubErr))
	case m.me.loading && !m.me.loaded:
		about = m.st.muted.Render("Loading your profile" + g.more)
	}
	c := m.counts(s, g, false)
	social := []string{}
	if v.Login != "" {
		social = append(social, m.count(v.Followers, "", plural(v.Followers, "follower", "followers"), m.st.text),
			m.count(v.Following, "", "following", m.st.text))
	}
	id := clean(v.GitName)
	if v.GitEmail != "" {
		id = joinSet(" ", id, "<"+clean(v.GitEmail)+">")
	}
	if id != "" {
		social = append(social, m.st.muted.Render("git "+id))
	}
	info := []string{name, about, strings.Join(c[:4], sep) + busy, strings.Join(c[4:], sep), strings.Join(social, sep)}
	art := m.avatarArt(shown == ProfileASCII)
	lines := make([]string, bannerRows)
	for i := range lines {
		lines[i] = " " + art[i] + "  " + info[i]
	}
	return lines
}

// avatarArt is the best avatar the terminal can draw, bannerRows lines of avatarCols cells: the picture with
// Kitty graphics, colour blocks with 256 colours or more, characters otherwise, and initials until it loads.
func (m *Model) avatarArt(ascii bool) []string {
	a := m.avatar
	var art []string
	switch {
	case !ascii && a.pic.ID != 0 && a.img != nil:
		art = a.pic.Lines(avatarCols)
	case !ascii && a.img != nil && m.colours >= colorprofile.ANSI256:
		art = termimg.Blocks(a.img, avatarCols, bannerRows)
	case a.img != nil:
		art = termimg.ASCII(a.img, avatarCols, bannerRows, m.dark)
		for i := range art {
			art[i] = m.st.accent.Render(art[i])
		}
	default:
		art = m.monogram(ascii)
	}
	for len(art) < bannerRows { // a picture that is not square
		art = append(art, strings.Repeat(" ", avatarCols))
	}
	return art[:bannerRows]
}

// monogram is a framed pair of initials.
func (m *Model) monogram(ascii bool) []string {
	v := m.me.val
	initials := ""
	for _, w := range strings.Fields(clean(firstOf(v.Name, v.GitName, v.Login, "?"))) {
		if r := []rune(w); len(initials) < 2 {
			initials += strings.ToUpper(string(r[0]))
		}
	}
	tl, tr, bl, br, h, vt := "╭", "╮", "╰", "╯", "─", "│"
	if ascii {
		tl, tr, bl, br, h, vt = "+", "+", "+", "+", "-", "|"
	}
	in := avatarCols - 2
	pad := (in - lipgloss.Width(initials)) / 2
	mid := strings.Repeat(" ", pad) + initials + strings.Repeat(" ", in-pad-lipgloss.Width(initials))
	blank := vt + strings.Repeat(" ", in) + vt
	return []string{
		m.st.accent.Render(tl + strings.Repeat(h, in) + tr),
		m.st.accent.Render(blank),
		m.st.accent.Render(vt) + m.st.marker.Render(mid) + m.st.accent.Render(vt),
		m.st.accent.Render(blank),
		m.st.accent.Render(bl + strings.Repeat(h, in) + br),
	}
}

// version is the build's version, or "(devel)" when it was not stamped.
func (m *Model) version() string { return firstOf(m.opts.Version, "(devel)") }

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// joinSet joins the non-empty parts.
func joinSet(sep string, parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), sep)
}

// prefix is p+s, or empty when s is.
func prefix(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}
