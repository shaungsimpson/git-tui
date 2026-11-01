// Package ui is the Bubble Tea app. This file holds the model, its state and key dispatch; loads live in load.go
// and rendering in view.go.
package ui

import (
	"context"
	"path/filepath"
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
	"github.com/shaungsimpson/git-tui/internal/update"
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

type seqExpiredMsg struct{ gen int }

type pollMsg struct{ gen int }

// Options configures a Model. Tools maps executable names to their resolved path ("" if absent).
type Options struct {
	Source       gitx.Source
	Repos        []config.Repo
	ConfigPath   string
	ConfigErr    error
	Demo         bool
	Images       bool // the terminal draws Kitty graphics placeholders
	Tabs         bool // the terminal can open a tab for a repository (Ghostty on macOS, outside tmux)
	Profile      Profile
	UTC          bool          // show times in UTC
	StatePath    string        // where toggles are remembered; empty in demo mode and tests
	Version      string        // as -version prints it
	Executable   string        // the running binary
	Build        update.Build  // how the running binary was made, which decides whether it is checked for updates
	Release      update.Latest // the last successful update check, from the cache
	ReleaseCache string        // where update checks are remembered; empty in demo mode and tests
	Tools        map[string]string
}

type Model struct {
	opts  Options
	repos []*repoState
	gh    map[string]*ghState // by slug, shared by every folder cloned from that GitHub repository

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
	release slot[update.Latest] // the newest stable release of git-tui itself
	avatar  avatarState
	colours colorprofile.Profile // what the terminal reported; colour avatars need 256 or more

	filters   [viewCount]string
	input     textinput.Model // shared: filtering, renaming and the folder to scan never overlap
	filtering bool
	prevFilt  string
	renaming  bool

	scan      scanState
	confirm   string         // a yes/no question in the footer
	onYes     func() tea.Cmd // what y does
	alertBase int            // the review count the pulse compares against
	alertSet  bool           // alertBase is known: every repository's PRs have been read once
	pulse     int            // beats left of the review badge's pulse

	gens int // issues slot generations, unique across slots so a result never lands on another repository

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
	m := &Model{opts: o, dark: true, focus: paneMain, pics: map[string]*picState{}, profile: o.Profile, utc: o.UTC, focused: true, lastUse: clock(), scan: scanState{fix: -1}}
	m.gh = map[string]*ghState{}
	if o.Release.Version != "" {
		m.release = slot[update.Latest]{val: o.Release, loaded: true, at: o.Release.Checked}
	}
	for _, r := range o.Repos {
		m.repos = append(m.repos, newRepo(r))
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
		if m.typing() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if m.filtering {
				m.setFilter(m.input.Value())
			}
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
			for _, i := range m.ghScope() {
				if slices.ContainsFunc(m.repos[i].gh.runs.val, gitx.Run.Active) {
					cmds = append(cmds, m.load(i, kindRuns, true))
				}
			}
			return m, tea.Batch(cmds...)
		}
	case pulseMsg:
		return m, m.onPulse()
	case resultMsg:
		return m, tea.Batch(m.applyResult(msg), m.ensurePics()) // a PR list arriving can reveal pictures
	default:
		if m.typing() {
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
	if m.confirm != "" {
		yes := m.onYes
		m.confirm, m.onYes = "", nil
		if s == "y" {
			return yes()
		}
		m.flash = "Nothing changed."
		return nil
	}
	if m.renaming {
		switch s {
		case "enter", "esc":
			m.renaming = false
			m.input.Blur()
			if s == "enter" {
				return m.rename(m.input.Value())
			}
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	if m.scan.open && m.scan.typing {
		switch s {
		case "enter":
			return m.runScan()
		case "esc":
			m.input.Blur()
			if !m.scan.res.loaded && !m.scan.res.loading {
				m.closeScan() // nothing scanned yet, so esc leaves
			}
			m.scan.typing = false
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	if m.scan.open {
		return m.scanKey(s)
	}
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
		m.input.Prompt = "/"
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
	case actAdd:
		return m.openScan(-1)
	case actFix:
		return m.openScan(m.connRepo())
	case actRename:
		return m.startRename()
	case actRemove:
		m.askRemove()
	case actMoveUp:
		return m.moveRepo(-1)
	case actMoveDown:
		return m.moveRepo(1)
	case actAlerts:
		return m.showAlerts()
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
			i := slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == p.Repo })
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

// typing reports whether a text input has the keyboard.
func (m *Model) typing() bool { return m.filtering || m.renaming || m.scan.typing }

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
