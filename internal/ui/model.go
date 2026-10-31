// Package ui is the Bubble Tea model: state, key dispatch and asynchronous loads. Rendering lives in view.go.
package ui

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

type viewID int

const (
	viewOverview viewID = iota
	viewActivity
	viewPRs
	viewRepo
	viewTags
	viewEnvs
	viewRuns
	viewConnections
	viewCount
)

var viewNames = [viewCount]string{"Overview", "Activity", "Pull requests", "Repository", "Tags", "Environments", "Deploys", "Connections"}

type paneID int

const (
	paneSidebar paneID = iota
	paneMain
	paneDetail
)

const (
	logDepth   = 100
	seqTimeout = time.Second
	pollEvery  = 10 * time.Second
)

// slot holds one asynchronously loaded value. gen identifies the newest request; older results are dropped.
type slot[T any] struct {
	val             T
	err             error
	loaded, loading bool
	gen             int
	cancel          context.CancelFunc
	at, tried       time.Time // the last successful load, and the last attempt
	fails           int       // failures in a row, for the retry backoff
}

type repoState struct {
	cfg     config.Repo
	summary slot[gitx.Summary]
	log     slot[[]gitx.Commit]
	tags    slot[[]gitx.Tag]
	prs     slot[[]gitx.PR]
	deps    slot[[]gitx.Deployment]
	runs    slot[[]gitx.Run]
}

type dataKind int

const (
	kindSummary dataKind = iota
	kindLog
	kindTags
	kindPRs
	kindDeps
	kindRuns
	kindDiff
	kindCompare
	kindViewer
)

type resultMsg struct {
	kind      dataKind
	repo, gen int
	val       any
	err       error
}

type seqExpiredMsg struct{ gen int }

type pollMsg struct{ gen int }

type browserMsg struct {
	what string
	err  error
}

// openBrowser opens a web page in the default browser; a variable so tests never launch one.
var openBrowser = func(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Run()
}

type tabMsg struct {
	what string
	err  error
}

// ghosttyOpen opens a Ghostty tab, or splits the focused pane, in a directory.
// The path and mode are arguments, never part of the script text.
const ghosttyOpen = `on run argv
	set dir to item 1 of argv
	set mode to item 2 of argv
	tell application "Ghostty"
		set cfg to new surface configuration
		set initial working directory of cfg to dir
		if mode is "tab" then
			new tab in front window with configuration cfg
		else
			set here to focused terminal of selected tab of front window
			if mode is "right" then
				split here direction right with configuration cfg
			else
				split here direction down with configuration cfg
			end if
		end if
	end tell
end run`

// openModes names what each mode opens, for messages.
var openModes = map[actionID]struct{ mode, what string }{
	actTab:        {"tab", "a tab"},
	actSplitRight: {"right", "a split to the right"},
	actSplitDown:  {"down", "a split below"},
}

// openTab opens a terminal tab or split ("tab", "right" or "down") at dir; a variable so tests never launch one.
var openTab = func(dir, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) // long enough to answer a macOS permission prompt
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e", ghosttyOpen, dir, mode).CombinedOutput()
	if err != nil && len(out) > 0 {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return err
}

// compareState is a commit list between two revisions, opened from Tags (pick a base tag, then a head tag)
// or from Environments (a deployed commit against origin/HEAD). view says which one owns it.
type compareState struct {
	view             viewID
	repo             int // 1-based, like Model.repo
	base, head       string
	baseRev, headRev string
	title, empty     string
	open             bool
	res              slot[[]gitx.Commit]
	cur, off         int
}

// diffState is the full-width view of one repository's uncommitted changes.
type diffState struct {
	open  bool
	repo  int // indexes Model.repos
	title string
	res   slot[[]string]
	off   int
}

// picState is one PR-body picture: loading, stored in the terminal, or the reason it is not shown.
// placed is the width it is currently placed at; a resize sends a new placement, never the image again.
type picState struct {
	loading bool
	pic     termimg.Picture
	err     error
	placed  int
}

type picMsg struct {
	url string
	pic termimg.Picture
	err error
}

// picBase keeps this app's terminal image IDs ("gt", for git-tui) apart from other programs' in the same terminal.
const picBase = 0x677400

// fetchPic loads one picture from the source and encodes it; a variable so tests never touch the network.
var fetchPic = func(ctx context.Context, src gitx.Source, url string, id int) (termimg.Picture, error) {
	img, err := src.Image(ctx, url)
	if err != nil {
		return termimg.Picture{}, err
	}
	return termimg.EncodeImage(img, id)
}

// envRow is one environment of one repository; repo indexes Model.repos.
type envRow struct {
	repo int
	gitx.Deployment
}

// Options configures a Model. Tools maps executable names to their resolved path ("" if absent).
type Options struct {
	Source     gitx.Source
	Repos      []config.Repo
	ConfigPath string
	ConfigErr  error
	Demo       bool
	Images     bool // the terminal draws Kitty graphics placeholders
	Tabs       bool // the terminal can open a tab for a repository (Ghostty on macOS, outside tmux)
	Profile    Profile
	UTC        bool   // show times in UTC
	StatePath  string // where toggles are remembered; empty in demo mode and tests
	Version    string // as -version prints it
	Executable string // the running binary
	Tools      map[string]string
}

type Model struct {
	opts  Options
	repos []repoState

	width, height  int
	st             styles
	dark, themeSet bool
	utc            bool // show times in UTC instead of the local zone

	view    viewID
	focus   paneID
	repo    int // 0 = all repositories, i+1 = repos[i]
	cursor  [viewCount]int
	offset  [viewCount]int
	inspect bool // detail shown in place of the list on narrow layouts
	detOff  int
	cmp     compareState
	diff    diffState
	pics    map[string]*picState // by picture link
	picIDs  int

	profile Profile
	me      slot[gitx.Viewer]
	avatar  avatarState
	colours colorprofile.Profile // what the terminal reported; colour avatars need 256 or more

	filters   [viewCount]string
	input     textinput.Model
	filtering bool
	prevFilt  string

	help     bool
	pendingG bool
	seqGen   int
	flash    string

	polling bool // a Deploys reload tick is scheduled
	pollGen int  // bumped on view change so an old tick is ignored

	focused bool      // the terminal window has focus; assumed until it reports otherwise
	ticking bool      // the idle refresh tick is scheduled
	lastUse time.Time // the last key press or return to the window

	ctx    context.Context
	cancel context.CancelFunc
}

func New(o Options) *Model {
	m := &Model{opts: o, dark: true, focus: paneMain, pics: map[string]*picState{}, profile: o.Profile, utc: o.UTC, focused: true, lastUse: clock()}
	for _, r := range o.Repos {
		m.repos = append(m.repos, repoState{cfg: r})
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.input = textinput.New()
	m.input.Prompt = "/"
	m.input.CharLimit = 200
	m.setTheme(true)
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.ensure(false), m.scheduleRefresh())
}

func (m *Model) setTheme(dark bool) {
	m.dark = dark
	m.st = newStyles(dark)
	m.input.SetStyles(textinput.DefaultStyles(dark))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, msg.Width-32)) // leave room for the apply/cancel hint
		if m.focus == paneDetail && m.layout().detailW == 0 {
			m.inspect = true // the details pane is gone; show details in place of the list
		}
		m.clampAll()
		// Pictures are encoded for the pane width; a taller window can reveal the profile header and its data.
		return m, tea.Batch(m.ensurePics(), m.ensure(false))
	case tea.ColorProfileMsg:
		m.colours = msg.Profile
	case avatarMsg:
		return m, m.applyAvatar(msg)
	case tea.BackgroundColorMsg:
		if !m.themeSet {
			m.setTheme(msg.IsDark())
		}
	case tea.KeyPressMsg:
		// Any key can reveal a running deploy (view, repository, filter), so check whether to start watching.
		cmd := tea.Batch(m.handleKey(msg), m.schedulePoll(), m.ensurePics())
		if m.ctx.Err() != nil { // quitting
			return m, cmd
		}
		return m, tea.Batch(cmd, m.touch()) // any key refreshes what is stale
	case tea.FocusMsg:
		m.focused = true
		return m, m.touch()
	case tea.BlurMsg:
		m.focused = false // the idle tick stops at its next beat
	case refreshMsg:
		return m, m.onRefresh()
	case tea.PasteMsg:
		// Only an open text input accepts pasted text; it never reaches workspace shortcuts.
		if m.filtering {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.setFilter(m.input.Value())
			return m, tea.Batch(cmd, m.schedulePoll())
		}
	case picMsg:
		if st, ok := m.pics[msg.url]; ok {
			st.loading, st.pic, st.err = false, msg.pic, msg.err
			if msg.err == nil {
				cols := m.picCols()
				send := msg.pic.Transmit + msg.pic.Place(cols)
				st.pic.Transmit, st.placed = "", cols // stored in the terminal now; no need to keep the bytes
				return m, tea.Raw(send)
			}
		}
	case browserMsg:
		if msg.err != nil {
			m.flash = "Could not open the browser: " + msg.err.Error()
		} else {
			m.flash = "Opened " + msg.what + " in your browser."
		}
	case tabMsg:
		if msg.err != nil {
			m.flash = "Could not open " + msg.what + ": " + clean(msg.err.Error())
		}
	case seqExpiredMsg:
		if msg.gen == m.seqGen {
			m.pendingG = false
		}
	case pollMsg:
		if msg.gen == m.pollGen {
			m.polling = false
			// Reload only repositories with a run still going; their results schedule the next tick.
			var cmds []tea.Cmd
			for _, i := range m.scope() {
				if slices.ContainsFunc(m.repos[i].runs.val, gitx.Run.Active) {
					cmds = append(cmds, m.load(i, kindRuns, true))
				}
			}
			return m, tea.Batch(cmds...)
		}
	case resultMsg:
		return m, tea.Batch(m.applyResult(msg), m.ensurePics()) // a PR list arriving can reveal pictures
	default:
		if m.filtering {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()
	if s == "ctrl+c" {
		m.cancel()
		if free := m.freePics(); free != nil {
			return tea.Sequence(free, tea.Quit)
		}
		return tea.Quit
	}
	m.flash = ""
	if m.filtering {
		switch s {
		case "enter":
			m.filtering = false
			m.input.Blur()
		case "esc":
			m.filtering = false
			m.input.Blur()
			m.setFilter(m.prevFilt)
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.setFilter(m.input.Value())
			return cmd
		}
		return nil
	}
	if m.help {
		switch s {
		case "esc", "?":
			m.help = false
		case "f2":
			m.do(actTheme)
		}
		return nil
	}
	if m.diff.open {
		page := max(1, m.layout().bodyH-2)
		switch s {
		case "esc", "d":
			m.closeDiff()
		case "j", "down":
			m.scrollDiff(1)
		case "k", "up":
			m.scrollDiff(-1)
		case "pgdown", "space":
			m.scrollDiff(page)
		case "pgup":
			m.scrollDiff(-page)
		}
		return nil
	}
	if m.pendingG {
		m.resetSequence()
		for _, g := range goKeys {
			if g.key == s {
				return m.setView(g.view)
			}
		}
		m.flash = "No shortcut for g " + s
		return nil
	}
	for _, a := range actions {
		if a.available(m) && key.Matches(msg, a.key) {
			return m.do(a.id)
		}
	}
	return nil
}

// savePrefs remembers one toggle for the next run, leaving the others as saved: a -profile flag lasts one run
// and must not be saved by pressing u. The file is tiny, so it is written here, in order.
func (m *Model) savePrefs(change func(*config.State)) {
	if m.opts.StatePath == "" {
		return
	}
	st, _ := config.LoadState(m.opts.StatePath) // an unreadable file is replaced
	change(&st)
	if err := config.SaveState(m.opts.StatePath, st); err != nil {
		m.flash = "Could not save your preferences: " + clean(err.Error())
	}
}

func (m *Model) do(id actionID) tea.Cmd {
	switch id {
	case actUp:
		return m.move(-1)
	case actDown:
		return m.move(1)
	case actFocusNext:
		m.cycleFocus(1)
	case actFocusPrev:
		m.cycleFocus(-1)
	case actOpen:
		return m.open()
	case actBack:
		m.back()
	case actFilter:
		m.filtering = true
		m.prevFilt = m.filters[m.view]
		m.input.SetValue(m.prevFilt)
		m.input.CursorEnd()
		return m.input.Focus()
	case actGo:
		m.pendingG = true
		m.seqGen++
		gen := m.seqGen
		return tea.Tick(seqTimeout, func(time.Time) tea.Msg { return seqExpiredMsg{gen} })
	case actRefresh:
		m.flash = "Refreshing " + viewNames[m.view]
		// Failed pictures retry too: the key's ensurePics fetches them again, and the viewer reload the avatar.
		for url, st := range m.pics {
			if st.err != nil {
				delete(m.pics, url)
			}
		}
		if m.avatar.err != nil {
			m.avatar.url = ""
		}
		return m.ensure(true)
	case actHelp:
		m.help = true
	case actTheme:
		m.themeSet = true
		m.setTheme(!m.dark)
	case actProfile:
		cmd := m.cycleProfile()
		m.savePrefs(func(s *config.State) { s.Profile = profileNames[m.profile] })
		return cmd
	case actTime:
		m.utc = !m.utc
		m.flash = "Times now shown in " + m.zone() + "."
		m.savePrefs(func(s *config.State) { s.UTC = m.utc })
	case actDiff:
		src := m.opts.Source
		if p, ok := m.selectedPR(); ok {
			i := slices.IndexFunc(m.repos, func(r repoState) bool { return r.cfg.Name == p.Repo })
			if i < 0 {
				return nil
			}
			name, dir, num := m.repos[i].cfg.Name, m.repos[i].cfg.Path, p.Number
			return m.openDiff(i, "Pull request #"+strconv.Itoa(num)+" · "+clean(name), func(ctx context.Context) ([]string, error) {
				return src.PRDiff(ctx, name, dir, num)
			})
		}
		i := m.diffTarget()
		if i < 0 {
			return nil
		}
		name, dir := m.repos[i].cfg.Name, m.repos[i].cfg.Path
		return m.openDiff(i, "Uncommitted changes · "+clean(name), func(ctx context.Context) ([]string, error) { return src.Diff(ctx, name, dir) })
	case actBrowse:
		url, what := m.browseTarget()
		if !strings.HasPrefix(url, "https://") { // the link comes from GitHub data; never hand other schemes to the OS
			m.flash = "Not opening a link that is not https."
			return nil
		}
		return func() tea.Msg { return browserMsg{what, openBrowser(url)} }
	case actTab, actSplitRight, actSplitDown:
		i := m.tabTarget()
		if i < 0 {
			return nil
		}
		dir, o := m.repos[i].cfg.Path, openModes[id]
		if m.opts.Demo {
			m.flash = "Demo: would open " + o.what + " in " + clean(dir) + "."
			return nil
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs // Ghostty would read a relative path from its own directory, not ours
		}
		return func() tea.Msg { return tabMsg{o.what, openTab(dir, o.mode)} }
	}
	return nil
}

// selectedRepo is the sidebar's repository, the Overview row's, or the Repository view's; -1 when there is none.
func (m *Model) selectedRepo() int {
	i := -1
	switch {
	case m.focus == paneSidebar || m.view == viewRepo:
		i = m.repo - 1
	case m.view == viewOverview:
		if rows := m.overviewRows(); m.cursor[viewOverview] < len(rows) {
			i = rows[m.cursor[viewOverview]]
		}
	}
	if i >= len(m.repos) {
		return -1
	}
	return i
}

// diffTarget is the repository whose changes d shows, or -1 when nothing with uncommitted changes is selected.
func (m *Model) diffTarget() int {
	i := m.selectedRepo()
	if i < 0 || !m.repos[i].summary.loaded || m.repos[i].summary.val.Clean() {
		return -1
	}
	return i
}

// tabTarget is the repository t opens a tab in, or -1 when the selected one has no usable directory.
func (m *Model) tabTarget() int {
	i := m.selectedRepo()
	if i < 0 || m.repos[i].cfg.Status != config.StatusOK {
		return -1
	}
	return i
}

// canTab offers t where the terminal can open a tab; demo shows it everywhere but only says what it would do.
func (m *Model) canTab() bool {
	return (m.opts.Tabs || m.opts.Demo) && m.tabTarget() >= 0
}

func (m *Model) canDiff() bool {
	_, pr := m.selectedPR()
	return pr || m.diffTarget() >= 0
}

// selectedPR is the pull request under the cursor when the PR list (not the sidebar) has focus.
func (m *Model) selectedPR() (gitx.PR, bool) {
	if m.view != viewPRs || m.focus == paneSidebar {
		return gitx.PR{}, false
	}
	n, cur, _ := m.list()
	if n == 0 {
		return gitx.PR{}, false
	}
	return m.prList()[*cur], true
}

func (m *Model) openDiff(repo int, title string, fetch func(context.Context) ([]string, error)) tea.Cmd {
	m.diff = diffState{open: true, repo: repo, title: title, res: slot[[]string]{gen: m.diff.res.gen + 1}}
	return start(m, &m.diff.res, true, kindDiff, repo, fetch)
}

// picCols is how wide PR-body pictures are drawn: the details pane, or the list area when details replace it.
func (m *Model) picCols() int {
	l := m.layout()
	w := l.detailW
	if w == 0 {
		w = l.mainW
	}
	return min(80, max(10, w-3))
}

// ensurePics starts loading the selected PR's GitHub pictures, and re-places loaded ones after a width change.
func (m *Model) ensurePics() tea.Cmd {
	if !m.opts.Images || m.view != viewPRs {
		return nil
	}
	n, cur, _ := m.list()
	if n == 0 {
		return nil
	}
	cols := m.picCols()
	shown, _ := bodyLines(m.prList()[*cur].Body)
	var cmds []tea.Cmd
	var place strings.Builder
	for _, line := range shown {
		for _, link := range termimg.Links(line) {
			url := link.URL
			if st, seen := m.pics[url]; seen {
				if !st.loading && st.err == nil && st.placed != cols {
					place.WriteString(st.pic.Place(cols))
					st.placed = cols
				}
				continue
			}
			if !termimg.Allowed(url) {
				continue
			}
			m.pics[url] = &picState{loading: true}
			m.picIDs++
			id, ctx, src := picBase+m.picIDs, m.ctx, m.opts.Source
			cmds = append(cmds, func() tea.Msg {
				pic, err := fetchPic(ctx, src, url, id)
				return picMsg{url, pic, err}
			})
		}
	}
	if place.Len() > 0 {
		cmds = append(cmds, tea.Raw(place.String()))
	}
	return tea.Batch(cmds...)
}

// freePics deletes every picture this app sent, so the terminal does not keep them after exit.
func (m *Model) freePics() tea.Cmd {
	var b strings.Builder
	if m.avatar.pic.ID != 0 {
		b.WriteString(termimg.Delete(m.avatar.pic.ID))
	}
	for _, st := range m.pics {
		if st.pic.ID != 0 {
			b.WriteString(termimg.Delete(st.pic.ID))
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return tea.Raw(b.String())
}

func (m *Model) closeDiff() {
	if m.diff.res.cancel != nil {
		m.diff.res.cancel()
	}
	m.diff = diffState{res: slot[[]string]{gen: m.diff.res.gen + 1}} // a late result is dropped
}

func (m *Model) scrollDiff(d int) {
	last := max(0, len(m.diff.res.val)-(m.layout().bodyH-1))
	m.diff.off = min(max(0, m.diff.off+d), last)
}

// browseTarget is the web link for the selected row, and how to name it; empty where there is none.
func (m *Model) browseTarget() (url, what string) {
	if m.view != viewPRs {
		return "", ""
	}
	n, cur, _ := m.list()
	if n == 0 {
		return "", ""
	}
	p := m.prList()[*cur]
	return p.URL, clean(p.Repo) + " #" + strconv.Itoa(p.Number)
}

func (m *Model) canBrowse() bool {
	url, _ := m.browseTarget()
	return url != ""
}

func (m *Model) resetSequence() {
	m.pendingG = false
	m.seqGen++
}

func (m *Model) setView(v viewID) tea.Cmd {
	m.resetSequence()
	m.view, m.inspect, m.detOff = v, false, 0
	if m.focus == paneDetail {
		m.focus = paneMain
	}
	m.pollGen++
	m.polling = false // Update schedules a fresh tick after the key that changed view
	return m.ensure(false)
}

// schedulePoll reloads Deploys every pollEvery while a run in view is still going.
// It stops when none is, when the view changes, or when a reload fails.
func (m *Model) schedulePoll() tea.Cmd {
	if m.polling || m.view != viewRuns || !slices.ContainsFunc(m.runList(), gitx.Run.Active) {
		return nil
	}
	m.polling = true
	gen := m.pollGen
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{gen} })
}

func (m *Model) panes() []paneID {
	if m.layout().detailW > 0 {
		return []paneID{paneSidebar, paneMain, paneDetail}
	}
	if m.inspect {
		return []paneID{paneSidebar, paneDetail}
	}
	return []paneID{paneSidebar, paneMain}
}

func (m *Model) cycleFocus(dir int) {
	m.resetSequence()
	ps := m.panes()
	i := max(0, slices.Index(ps, m.focus))
	m.focus = ps[(i+dir+len(ps))%len(ps)]
	if m.focus == paneMain {
		m.inspect = false
	}
}

// move handles j/k in whichever pane has focus.
func (m *Model) move(d int) tea.Cmd {
	switch m.focus {
	case paneSidebar:
		next := min(max(m.repo+d, 0), len(m.repos))
		if next == m.repo {
			return nil
		}
		m.selectRepo(next)
		return m.ensure(false)
	case paneDetail:
		m.detOff = min(max(0, m.detOff+d), m.maxDetailOff())
	default:
		n, cur, off := m.list()
		if n == 0 {
			return nil
		}
		*cur = min(max(*cur+d, 0), n-1)
		m.keepVisible(m.view, cur, off, n)
		m.detOff = 0
	}
	return nil
}

func (m *Model) selectRepo(i int) {
	m.repo = i
	m.cancelCompare()
	m.cmp = compareState{res: slot[[]gitx.Commit]{gen: m.cmp.res.gen + 1}}
	for _, v := range []viewID{viewActivity, viewPRs, viewRepo, viewTags, viewEnvs, viewRuns} {
		m.cursor[v], m.offset[v] = 0, 0
	}
	m.detOff = 0
}

func (m *Model) cancelCompare() {
	if m.cmp.res.cancel != nil {
		m.cmp.res.cancel()
	}
}

func (m *Model) keepVisible(v viewID, cur, off *int, n int) {
	h := m.listHeight(v)
	*off = min(max(*off, *cur-h+1), *cur)
	*off = max(0, min(*off, max(0, n-h)))
}

func (m *Model) clampAll() {
	for v := range viewCount {
		n, cur, off := m.listFor(v)
		*cur = min(*cur, max(0, n-1))
		m.keepVisible(v, cur, off, n)
	}
}

func (m *Model) open() tea.Cmd {
	if m.focus == paneSidebar {
		m.focus = paneMain
		return nil
	}
	n, cur, _ := m.list()
	if n == 0 {
		return nil
	}
	switch {
	case m.view == viewOverview:
		rows := m.overviewRows()
		m.selectRepo(rows[*cur] + 1)
		return m.setView(viewRepo)
	case m.view == viewTags && !m.cmpOpen():
		return m.pickTag(m.tagList()[*cur].Name)
	case m.view == viewEnvs && !m.cmpOpen():
		return m.openPending(m.envList()[*cur])
	}
	m.detOff = 0
	if m.layout().detailW > 0 {
		m.focus = paneDetail
	} else {
		m.inspect, m.focus = true, paneDetail
	}
	return nil
}

func (m *Model) pickTag(name string) tea.Cmd {
	c := &m.cmp
	if c.view != viewTags { // state left by Environments; start a fresh tag selection
		m.cancelCompare()
		*c = compareState{view: viewTags, res: slot[[]gitx.Commit]{gen: c.res.gen + 1}}
	}
	if c.base == "" {
		c.base, c.repo = name, m.repo
		m.flash = "Base " + name + " selected. Choose another tag and press Enter."
		return nil
	}
	if c.base == name {
		c.base = ""
		return nil
	}
	tags := m.selected().tags.val
	idx := func(n string) int { return slices.IndexFunc(tags, func(t gitx.Tag) bool { return t.Name == n }) }
	base, head := c.base, name
	if idx(base) < idx(head) { // tags are newest first, so the base is the one further down
		base, head = head, base
	}
	c.view, c.base, c.head, c.open, c.cur, c.off = viewTags, base, head, true, 0, 0
	c.baseRev, c.headRev = "refs/tags/"+base, "refs/tags/"+head
	c.title = "Commits between " + clean(base) + " and " + clean(head)
	c.empty = "No commits between " + clean(base) + " and " + clean(head) + "."
	return m.loadCompare()
}

// openPending lists commits on origin/HEAD that the environment's deployed commit does not contain.
func (m *Model) openPending(e envRow) tea.Cmd {
	if !gitx.IsCommitID(e.Commit) { // also guards the git revision: only a plain object ID reaches git
		m.flash = "No successful deployment is recorded for " + clean(e.Environment) + "."
		return nil
	}
	m.cancelCompare()
	env := clean(e.Repo) + " " + clean(e.Environment)
	m.cmp = compareState{
		view: viewEnvs, repo: e.repo + 1, open: true,
		base: e.Environment, baseRev: e.Commit, headRev: "origin/HEAD",
		title: "Not yet in " + env + " · on origin/HEAD after " + e.Commit[:7],
		empty: "Nothing to release: " + env + " already has everything on origin/HEAD (as of your last fetch).",
		res:   slot[[]gitx.Commit]{gen: m.cmp.res.gen + 1},
	}
	return m.loadCompare()
}

// cmpOpen reports whether the current view is showing its commit list.
func (m *Model) cmpOpen() bool { return m.cmp.open && m.cmp.view == m.view }

func (m *Model) canOpen() bool {
	if m.focus == paneSidebar {
		return true
	}
	n, _, _ := m.list()
	return m.focus == paneMain && n > 0
}

func (m *Model) canBack() bool {
	baseSet := m.view == viewTags && m.cmp.view == viewTags && m.cmp.base != ""
	return m.inspect || m.focus == paneDetail || m.cmpOpen() || baseSet || m.filters[m.view] != ""
}

func (m *Model) back() {
	switch {
	case m.inspect || m.focus == paneDetail:
		m.inspect, m.focus = false, paneMain
	case m.cmpOpen():
		m.cancelCompare()
		m.cmp.open, m.cmp.head = false, ""
		m.cmp.res = slot[[]gitx.Commit]{gen: m.cmp.res.gen + 1}
	case m.view == viewTags && m.cmp.view == viewTags && m.cmp.base != "":
		m.cmp.base = ""
	case m.filters[m.view] != "":
		m.setFilter("")
	}
}

func (m *Model) filterable() bool {
	switch m.view {
	case viewConnections:
		return false
	}
	return !m.cmpOpen()
}

func (m *Model) setFilter(f string) {
	m.filters[m.view] = f
	_, cur, off := m.list()
	*cur, *off = 0, 0
}

// selected returns the chosen repository, or nil when "All repositories" is selected.
func (m *Model) selected() *repoState {
	if m.repo == 0 || m.repo > len(m.repos) {
		return nil
	}
	return &m.repos[m.repo-1]
}

// scope is the repositories the current repository selection covers.
func (m *Model) scope() []int {
	if m.repo > 0 {
		return []int{m.repo - 1}
	}
	var all []int
	for i := range m.repos {
		all = append(all, i)
	}
	return all
}

// ---- lists ----

func (m *Model) list() (int, *int, *int) { return m.listFor(m.view) }

func (m *Model) listFor(v viewID) (int, *int, *int) {
	cur, off := &m.cursor[v], &m.offset[v]
	switch v {
	case viewOverview:
		return len(m.overviewRows()), cur, off
	case viewActivity:
		return len(m.activity()), cur, off
	case viewPRs:
		return len(m.prList()), cur, off
	case viewRuns:
		return len(m.runList()), cur, off
	case viewRepo:
		return len(m.repoCommits()), cur, off
	case viewTags, viewEnvs:
		if m.cmp.open && m.cmp.view == v {
			return len(m.cmp.res.val), &m.cmp.cur, &m.cmp.off
		}
		if v == viewEnvs {
			return len(m.envList()), cur, off
		}
		return len(m.tagList()), cur, off
	default:
		return len(m.connections()), cur, off
	}
}

func match(filter string, fields ...string) bool {
	if filter == "" {
		return true
	}
	filter = strings.ToLower(filter)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), filter) {
			return true
		}
	}
	return false
}

func (m *Model) overviewRows() []int {
	var rows []int
	for i, r := range m.repos {
		if match(m.filters[viewOverview], r.cfg.Name, r.summary.val.Branch) {
			rows = append(rows, i)
		}
	}
	return rows
}

func (m *Model) commitsMatching(v viewID, cs []gitx.Commit) []gitx.Commit {
	var out []gitx.Commit
	for _, c := range cs {
		if match(m.filters[v], c.Subject, c.Author, c.Repo, c.Hash) {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) activity() []gitx.Commit {
	var all []gitx.Commit
	for _, i := range m.scope() {
		all = append(all, m.repos[i].log.val...)
	}
	slices.SortStableFunc(all, func(a, b gitx.Commit) int { return b.When.Compare(a.When) })
	return m.commitsMatching(viewActivity, all)
}

// prList is the open pull requests of the selected repositories, most recently updated first.
func (m *Model) prList() []gitx.PR {
	var out []gitx.PR
	for _, i := range m.scope() {
		for _, p := range m.repos[i].prs.val {
			if match(m.filters[viewPRs], p.Title, p.Author, p.Branch, p.Repo, "#"+strconv.Itoa(p.Number)) {
				out = append(out, p)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b gitx.PR) int { return b.Updated.Compare(a.Updated) })
	return out
}

// envList is every recorded environment of the selected repositories, most recently deployed first.
func (m *Model) envList() []envRow {
	var out []envRow
	for _, i := range m.scope() {
		for _, d := range m.repos[i].deps.val {
			if match(m.filters[viewEnvs], d.Environment, d.Repo, d.Ref, d.Commit) {
				out = append(out, envRow{i, d})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b envRow) int { return b.When.Compare(a.When) })
	return out
}

// runList is the deploy runs of the selected repositories, most recently started first.
func (m *Model) runList() []gitx.Run {
	var out []gitx.Run
	for _, i := range m.scope() {
		for _, r := range m.repos[i].runs.val {
			if match(m.filters[viewRuns], r.Workflow, r.Title, r.Branch, r.Repo, r.Status, r.Conclusion) {
				out = append(out, r)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b gitx.Run) int { return b.Started.Compare(a.Started) })
	return out
}

func (m *Model) repoCommits() []gitx.Commit {
	r := m.selected()
	if r == nil {
		return nil
	}
	return m.commitsMatching(viewRepo, r.log.val)
}

func (m *Model) tagList() []gitx.Tag {
	r := m.selected()
	if r == nil {
		return nil
	}
	var out []gitx.Tag
	for _, t := range r.tags.val {
		if match(m.filters[viewTags], t.Name, t.Subject) {
			out = append(out, t)
		}
	}
	return out
}

// ---- loading ----

// ensure starts the loads for the data on screen that is stale, or all of it when force is set (Ctrl+R).
func (m *Model) ensure(force bool) tea.Cmd {
	var cmds []tea.Cmd
	for _, w := range m.wanted() {
		cmds = append(cmds, m.load(w.repo, w.kind, force))
	}
	if m.cmpOpen() && (force || due(&m.cmp.res, localAge)) { // an open commit list is git data on screen too
		cmds = append(cmds, m.loadCompare())
	}
	return tea.Batch(cmds...)
}

func start[T any](m *Model, s *slot[T], force bool, kind dataKind, repo int, fetch func(context.Context) (T, error)) tea.Cmd {
	if !force && !due(s, ageOf(kind)) {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.gen++
	ctx, cancel := context.WithCancel(m.ctx)
	s.cancel, s.loading = cancel, true
	gen := s.gen
	return func() tea.Msg {
		defer cancel()
		v, err := fetch(ctx)
		return resultMsg{kind: kind, repo: repo, gen: gen, val: v, err: err}
	}
}

// finish stores a result if it answers the newest request. loaded means a value is present;
// a failed reload keeps the previous value visible alongside the error.
func finish[T any](s *slot[T], msg resultMsg) {
	if msg.gen != s.gen {
		return
	}
	s.loading, s.err, s.tried = false, msg.err, clock()
	if msg.err != nil {
		s.fails++
		return
	}
	s.val, s.loaded, s.at, s.fails = msg.val.(T), true, s.tried, 0
}

func (m *Model) load(i int, kind dataKind, force bool) tea.Cmd {
	if kind == kindViewer {
		dir := "" // the first usable repository, so per-directory git identities apply
		if j := slices.IndexFunc(m.repos, func(r repoState) bool { return r.cfg.Status == config.StatusOK }); j >= 0 {
			dir = m.repos[j].cfg.Path
		}
		src := m.opts.Source
		return start(m, &m.me, force, kind, -1, func(ctx context.Context) (gitx.Viewer, error) { return src.Viewer(ctx, dir) })
	}
	r := &m.repos[i]
	if r.cfg.Status != config.StatusOK {
		return nil
	}
	src, name, dir := m.opts.Source, r.cfg.Name, r.cfg.Path
	switch kind {
	case kindSummary:
		return start(m, &r.summary, force, kind, i, func(ctx context.Context) (gitx.Summary, error) { return src.Summary(ctx, name, dir) })
	case kindLog:
		return start(m, &r.log, force, kind, i, func(ctx context.Context) ([]gitx.Commit, error) { return src.Log(ctx, name, dir, logDepth) })
	case kindPRs:
		return start(m, &r.prs, force, kind, i, func(ctx context.Context) ([]gitx.PR, error) { return src.PullRequests(ctx, name, dir) })
	case kindDeps:
		return start(m, &r.deps, force, kind, i, func(ctx context.Context) ([]gitx.Deployment, error) { return src.Deployments(ctx, name, dir) })
	case kindRuns:
		return start(m, &r.runs, force, kind, i, func(ctx context.Context) ([]gitx.Run, error) { return src.DeployRuns(ctx, name, dir) })
	default:
		return start(m, &r.tags, force, kind, i, func(ctx context.Context) ([]gitx.Tag, error) { return src.Tags(ctx, name, dir) })
	}
}

// loadCompare always starts a fresh request; any in-flight comparison is cancelled and its result ignored.
func (m *Model) loadCompare() tea.Cmd {
	if m.cmp.repo < 1 || m.cmp.repo > len(m.repos) {
		return nil
	}
	r := &m.repos[m.cmp.repo-1]
	src, name, dir, base, head := m.opts.Source, r.cfg.Name, r.cfg.Path, m.cmp.baseRev, m.cmp.headRev
	return start(m, &m.cmp.res, true, kindCompare, m.cmp.repo, func(ctx context.Context) ([]gitx.Commit, error) {
		return src.Compare(ctx, name, dir, base, head)
	})
}

// rowKeys identifies each row of the lists that merge or reorder on reload, so the selection can follow its row.
func (m *Model) rowKeys(v viewID) []string {
	var keys []string
	switch v {
	case viewActivity:
		for _, c := range m.activity() {
			keys = append(keys, c.Repo+"/"+c.Hash)
		}
	case viewRepo:
		for _, c := range m.repoCommits() {
			keys = append(keys, c.Repo+"/"+c.Hash)
		}
	case viewEnvs: // the open commit list keeps its own cursor, so this one always tracks environment rows
		for _, e := range m.envList() {
			keys = append(keys, e.Repo+"/"+e.Environment)
		}
	case viewRuns:
		for _, r := range m.runList() {
			keys = append(keys, r.Repo+"/"+strconv.FormatInt(r.ID, 10))
		}
	case viewPRs:
		for _, p := range m.prList() {
			keys = append(keys, p.Repo+"#"+strconv.Itoa(p.Number))
		}
	}
	return keys
}

// applyResult stores a load result and keeps the same row selected if the list reorders around it.
func (m *Model) applyResult(msg resultMsg) tea.Cmd {
	var cmd tea.Cmd
	keep := "" // a cursor on the first row stays on top, so newest-first lists show new rows on load
	if keys := m.rowKeys(m.view); m.cursor[m.view] > 0 && m.cursor[m.view] < len(keys) {
		keep = keys[m.cursor[m.view]]
	}
	if msg.kind == kindDiff {
		if m.diff.open && msg.repo == m.diff.repo {
			finish(&m.diff.res, msg)
		}
	} else if msg.kind == kindViewer {
		finish(&m.me, msg)
		cmd = m.ensureAvatar()
	} else if msg.kind == kindCompare {
		if msg.repo == m.cmp.repo {
			finish(&m.cmp.res, msg)
		}
	} else if msg.repo >= 0 && msg.repo < len(m.repos) {
		r := &m.repos[msg.repo]
		switch msg.kind {
		case kindSummary:
			finish(&r.summary, msg)
		case kindLog:
			finish(&r.log, msg)
		case kindTags:
			finish(&r.tags, msg)
		case kindPRs:
			finish(&r.prs, msg)
		case kindDeps:
			finish(&r.deps, msg)
			cmd = m.followDeploy(msg.repo)
		case kindRuns:
			finish(&r.runs, msg)
			if msg.err == nil { // a failed reload stops polling until Ctrl+R
				cmd = m.schedulePoll()
			}
		}
	}
	if keys := m.rowKeys(m.view); keep != "" {
		if i := slices.Index(keys, keep); i >= 0 {
			m.cursor[m.view] = i
			m.keepVisible(m.view, &m.cursor[m.view], &m.offset[m.view], len(keys))
		}
	}
	m.clampAll()
	return cmd
}

// followDeploy reopens an open "not yet deployed" list when reloaded deployments show a newer deployed commit.
func (m *Model) followDeploy(repo int) tea.Cmd {
	if !m.cmp.open || m.cmp.view != viewEnvs || m.cmp.repo != repo+1 {
		return nil
	}
	for _, d := range m.repos[repo].deps.val {
		if d.Environment == m.cmp.base && d.Commit != m.cmp.baseRev && gitx.IsCommitID(d.Commit) {
			return m.openPending(envRow{repo, d})
		}
	}
	return nil
}
