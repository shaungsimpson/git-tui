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
	logDepth    = 100
	incomingCap = 50 // the Repository view's heading still counts every incoming commit
	seqTimeout  = time.Second
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
	Tabs         bool // the terminal can open a tab for a repository (Ghostty on macOS or Windows Terminal on WSL, outside tmux)
	Profile      Profile
	UTC          bool          // show times in UTC
	NoPreview    bool          // hide the preview pane beside the list
	Dates        string        // how list rows show dates, one of dateFormats
	Status       string        // how PR rows show review status, one of statusStyles
	StatePath    string        // where toggles are remembered; empty in demo mode and tests
	Version      string        // as -version prints it
	Executable   string        // the running binary
	Build        update.Build  // how the running binary was made, which decides whether it is checked for updates
	Release      update.Latest // the last successful update check, from the cache
	ReleaseCache string        // where update checks are remembered; empty in demo mode and tests
	Tools        map[string]string
	Sorts        map[string]string // each list's saved sort, by sortKeys
	Group        string            // the saved group to show; one that no longer exists shows all
	RawMarkdown  bool              // PR descriptions show their markdown source instead of formatted
}

type Model struct {
	opts  Options
	repos []*repoState
	gh    map[string]*ghState // by slug, shared by every folder cloned from that GitHub repository

	width, height  int
	st             styles
	dark, themeSet bool
	utc            bool // show times in UTC instead of the local zone
	noPreview      bool // the list takes the preview pane's width
	dates          int  // index into dateFormats
	status         int  // index into statusStyles

	view     viewID
	focus    paneID
	repo     int                // 0 = all repositories, i+1 = repos[i]
	rawMD    bool               // PR descriptions show their markdown source
	openFor  string             // the PR whose <details> sections are open, by URL
	mdc      map[mdKey][]mdLine // formatted descriptions and comments
	group    string             // only repositories in this group are shown and loaded; empty shows all
	cursor   [viewCount]int
	sorts    [viewCount]int // each view's position in sortsOf
	offset   [viewCount]int
	inspect  bool // the detail page: the selected row's details fill the body
	detOff   int
	cmp      compareState
	diff     diffState
	why      map[int64]*slot[[]string]    // why each stopped deploy run stopped, by run ID, read when first selected
	comments map[string]*slot[prComments] // each PR's comments, by URL, read while its detail page is open
	pics     map[string]*picState         // by picture link
	picIDs   int
	picPage  string             // the row whose pictures are loading
	picCtx   context.Context    // those loads'; nil until one starts
	picStop  context.CancelFunc // cancels them when the page changes

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
	grouping  bool     // the selected repository's group is being typed
	groupTabs []string // the groups tab cycles through, matching what was typed before the first tab
	groupTab  int

	scan      scanState
	confirm   string         // a yes/no question in the footer
	onYes     func() tea.Cmd // what y does
	alertBase int            // the review count the pulse compares against
	alertSet  bool           // alertBase is known: every repository's PRs have been read once
	pulse     int            // beats left of the review badge's pulse

	gens int // issues slot generations, unique across slots so a result never lands on another repository

	help       bool
	helpOff    int // how far help is scrolled
	pendingG   bool
	seqGen     int
	footerMemo int  // footerRows while a frame renders; 0 outside one
	settleGen  int  // the newest key's; an older settledMsg is dropped
	forceLoad  bool // Ctrl+R was pressed: the next settle reloads even fresh data
	flash      string

	prMemo struct {
		key  string // prList's inputs; empty when it must rebuild
		list []gitx.PR
	}

	polling  bool          // a Deploys reload tick is scheduled
	pollWait time.Duration // how far apart those ticks are now
	pollGen  int           // bumped on view change so an old tick is ignored

	focused bool      // the terminal window has focus; assumed until it reports otherwise
	ticking bool      // the idle refresh tick is scheduled
	lastUse time.Time // the last key press or return to the window

	ctx    context.Context
	cancel context.CancelFunc
}

func New(o Options) *Model {
	m := &Model{opts: o, dark: true, focus: paneMain, pics: map[string]*picState{}, profile: o.Profile, utc: o.UTC, noPreview: o.NoPreview, focused: true, lastUse: clock(), scan: scanState{fix: -1}}
	m.gh = map[string]*ghState{}
	m.why = map[int64]*slot[[]string]{}
	m.comments = map[string]*slot[prComments]{}
	m.mdc = map[mdKey][]mdLine{}
	m.dates = max(0, slices.Index(dateFormats, o.Dates)) // an unknown saved name keeps the default
	m.status = max(0, slices.Index(statusStyles, o.Status))
	m.rawMD = o.RawMarkdown
	for v, k := range sortKeys {
		if i := slices.Index(sortsOf[v], o.Sorts[k]); i >= 0 { // an unknown saved name keeps the default
			m.sorts[v] = i
		}
	}
	if o.Release.Version != "" {
		m.release = slot[update.Latest]{val: o.Release, loaded: true, at: o.Release.Checked}
	}
	for _, r := range o.Repos {
		m.repos = append(m.repos, newRepo(r))
	}
	if slices.Contains(m.groups(), o.Group) {
		m.group = o.Group
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
	m.prMemo.key = "" // a filter's time terms move with the clock
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, msg.Width-32)) // leave room for the apply/cancel hint
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
		cmd := tea.Batch(m.handleKey(msg), m.schedulePoll())
		if m.ctx.Err() != nil { // quitting
			return m, cmd
		}
		// What a key reveals loads once keys pause, so a held j or J loads only where it stops.
		return m, tea.Batch(cmd, m.touch(), debounce(&m.settleGen, settleAfter, func(gen int) tea.Msg { return settledMsg{gen} }))
	case settledMsg:
		if msg.gen == m.settleGen {
			force := m.forceLoad
			m.forceLoad = false
			return m, tea.Batch(m.ensurePics(), m.ensure(force)) // pictures first only for tests, whose drain runs a batch in order
		}
	case tea.FocusMsg:
		m.focused = true
		return m, tea.Batch(m.touch(), m.ensure(false))
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
			m.groupTabs = nil // as typing does, pasting starts a new completion
			return m, tea.Batch(cmd, m.schedulePoll())
		}
	case picMsg:
		if st, ok := m.pics[msg.url]; ok && st.pic.ID == msg.id { // not a load cancelled before this one
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
			if !m.focused && !m.inUse() {
				return m, nil // a key or returning to the window reloads stale runs, and their result polls again
			}
			// Reload only repositories with a run still going and not reloaded since the last tick, say by the
			// idle refresh; their results schedule the next tick.
			var cmds []tea.Cmd
			for _, i := range m.ghScope() {
				runs := &m.repos[i].gh.runs
				if slices.ContainsFunc(runs.val, gitx.Run.Active) && due(runs, m.pollWait/2) {
					cmds = append(cmds, m.load(i, kindRuns, true))
				}
			}
			if len(cmds) == 0 {
				return m, m.schedulePoll()
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
	if m.grouping {
		return m.groupKey(msg)
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
	if m.help || m.diff.open {
		scroll, page := m.scrollDiff, max(1, m.layout().bodyH-2)
		if m.help {
			scroll = m.scrollHelp
		}
		switch {
		case m.help && (s == "esc" || s == "?"):
			m.help = false
		case m.help && s == "f2":
			m.do(actTheme)
		case !m.help && (s == "esc" || s == "d"):
			m.closeDiff()
		case s == "j" || s == "down":
			scroll(1)
		case s == "k" || s == "up":
			scroll(-1)
		case s == "pgdown":
			scroll(page)
		case s == "pgup":
			scroll(-page)
		case s == "home":
			scroll(-far)
		case s == "end":
			scroll(far)
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

// canSort offers s on the lists that sort, but not on an open commit list or diff (their own order) or the detail page.
func (m *Model) canSort() bool {
	_, ok := sortsOf[m.view]
	return ok && !m.cmpOpen() && !m.diff.open && !m.inspect
}

// canPreview offers p where the window is wide enough for the preview pane and the list is on screen.
func (m *Model) canPreview() bool { return m.width >= 120 && !m.inspect }

// previewKeys reports whether the preview pane is beside the list, where it lists the selected row's shortcuts, so
// the footer leaves them out. It ignores focus, so tabbing to the sidebar never changes the footer's height.
func (m *Model) previewKeys() bool {
	_, _, detail := m.widths()
	return detail > 0 && !m.inspect
}

// cycleSort moves the view to its next sort, keeps the selected row selected and remembers the choice.
func (m *Model) cycleSort() {
	keys, cur := m.rowKeys(m.view), m.cursor[m.view]
	names := sortsOf[m.view]
	m.sorts[m.view] = (m.sorts[m.view] + 1) % len(names)
	if cur < len(keys) {
		m.follow(keys[cur])
	}
	name, k := names[m.sorts[m.view]], sortKeys[m.view]
	m.flash = "Sorted by " + name + "."
	m.savePrefs(func(s *config.State) {
		if s.Sorts == nil {
			s.Sorts = map[string]string{}
		}
		s.Sorts[k] = name
	})
}

// follow moves the cursor to the row with this key, if the list still has it.
func (m *Model) follow(key string) {
	keys := m.rowKeys(m.view)
	if i := slices.Index(keys, key); i >= 0 {
		m.cursor[m.view] = i
		keepVisible(&m.cursor[m.view], &m.offset[m.view], len(keys), m.listHeight(m.view))
	}
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
		m.move(-1)
	case actDown:
		m.move(1)
	case actFocusNext:
		m.cycleFocus(1)
	case actFocusPrev:
		m.cycleFocus(-1)
	case actOpen:
		return m.open()
	case actView:
		m.inspect, m.focus, m.detOff = true, paneDetail, 0
		return tea.Batch(m.ensurePics(), m.ensure(false)) // a stopped run's reasons load once the page shows them
	case actPageDown:
		m.move(m.page())
	case actPageUp:
		m.move(-m.page())
	case actTop:
		m.move(-far)
	case actBottom:
		m.move(far)
	case actNextItem, actPrevItem:
		n, cur, off := m.list()
		if n == 0 { // a refresh can empty the list behind the page
			return nil
		}
		d := 1
		if id == actPrevItem {
			d = -1
		}
		*cur = min(max(*cur+d, 0), n-1)
		keepVisible(cur, off, n, m.listHeight(m.view))
		m.detOff, m.openFor = 0, "" // a new row: its sections start closed; it loads once keys settle
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
		return debounce(&m.seqGen, seqTimeout, func(gen int) tea.Msg { return seqExpiredMsg{gen} })
	case actRefresh:
		m.flash = "Refreshing " + viewNames[m.view]
		// Failed pictures retry too: ensurePics fetches them again once keys settle, and the viewer reload the avatar.
		for url, st := range m.pics {
			if st.err != nil {
				delete(m.pics, url)
			}
		}
		if m.avatar.err != nil {
			m.avatar.url = ""
		}
		m.forceLoad = true // once keys settle, so a held Ctrl+R reloads once rather than on every repeat
	case actAdd:
		return m.openScan(-1)
	case actFix:
		return m.openScan(m.sideRepo())
	case actRename:
		return m.startRename()
	case actMarkdown:
		m.rawMD = !m.rawMD
		m.detOff = min(m.detOff, m.maxDetailOff())
		m.flash = "Descriptions shown formatted."
		if m.rawMD {
			m.flash = "Descriptions shown as markdown source."
		}
		m.savePrefs(func(s *config.State) { s.RawMarkdown = m.rawMD })
	case actSections:
		if p, ok := m.selectedPR(); ok {
			if m.openFor == p.URL {
				m.openFor = ""
			} else {
				m.openFor = p.URL
			}
			m.detOff = min(m.detOff, m.maxDetailOff())
		}
	case actGroup:
		return m.startGroup()
	case actNextGroup:
		return m.cycleGroup(1)
	case actPrevGroup:
		return m.cycleGroup(-1)
	case actRemove:
		m.askRemove()
	case actMoveUp:
		return m.moveRepo(-1)
	case actMoveDown:
		return m.moveRepo(1)
	case actAlerts:
		return m.showAlerts()
	case actHelp:
		m.help, m.helpOff = true, 0
	case actPreview:
		m.noPreview = !m.noPreview
		m.clampAll() // rows change between one and two lines with the list's width
		m.savePrefs(func(s *config.State) { s.NoPreview = m.noPreview })
	case actTheme:
		m.themeSet = true
		m.setTheme(!m.dark)
	case actProfile:
		cmd := m.cycleProfile()
		m.savePrefs(func(s *config.State) { s.Profile = profileNames[m.profile] })
		return cmd
	case actSort:
		m.cycleSort()
	case actTime:
		m.utc = !m.utc
		m.flash = "Times now shown in " + m.zone() + "."
		m.savePrefs(func(s *config.State) { s.UTC = m.utc })
	case actDates:
		m.dates = (m.dates + 1) % len(dateFormats)
		m.flash = "Dates shown as " + dateFormats[m.dates] + "."
		m.savePrefs(func(s *config.State) { s.Dates = dateFormats[m.dates] })
	case actStatus:
		m.status = (m.status + 1) % len(statusStyles)
		m.flash = "PR status shown as " + statusStyles[m.status] + "."
		m.savePrefs(func(s *config.State) { s.Status = statusStyles[m.status] })
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
			dir = abs // the terminal would read a relative path from its own directory, not ours
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

// schedulePoll reloads Deploys while a run in view is still going, less often the longer it has gone on.
// It stops when none is, when the view changes, when a reload fails, or while the window is in the background
// and unused; the next key, or returning to the window, starts it again.
func (m *Model) schedulePoll() tea.Cmd {
	if m.polling || m.view != viewRuns || !m.focused && !m.inUse() {
		return nil
	}
	newest, active := time.Time{}, false
	for _, r := range m.runList() {
		if r.Active() {
			active = true
			started := r.Started
			if started.IsZero() {
				started = clock() // not started yet, so as young as a run can be
			}
			if started.After(newest) {
				newest = started
			}
		}
	}
	if !active {
		return nil
	}
	m.polling, m.pollWait = true, pollWait(clock().Sub(newest))
	gen := m.pollGen
	return tea.Tick(m.pollWait, func(time.Time) tea.Msg { return pollMsg{gen} })
}

// pollWait is how often Deploys reloads while its newest unfinished run has been going this long. Most deploys
// finish within half an hour; one waiting for an approval or a free runner can wait for days.
func pollWait(running time.Duration) time.Duration {
	switch {
	case running < 30*time.Minute:
		return time.Minute
	case running < 2*time.Hour:
		return 5 * time.Minute
	}
	return 15 * time.Minute
}

func (m *Model) panes() []paneID {
	if m.inspect {
		return []paneID{paneDetail}
	}
	return []paneID{paneSidebar, paneMain}
}

func (m *Model) cycleFocus(dir int) {
	m.resetSequence()
	ps := m.panes()
	i := max(0, slices.Index(ps, m.focus))
	m.focus = ps[(i+dir+len(ps))%len(ps)]
}

// move handles j/k in whichever pane has focus. What a move reveals loads once keys settle.
func (m *Model) move(d int) {
	switch m.focus {
	case paneSidebar:
		shown := m.shown()
		pos := slices.Index(shown, m.repo-1) + 1
		next := min(max(pos+d, 0), len(shown))
		if next == pos {
			return
		}
		if next == 0 {
			m.selectRepo(0)
		} else {
			m.selectRepo(shown[next-1] + 1)
		}
	case paneDetail:
		m.detOff = min(max(0, m.detOff+d), m.maxDetailOff())
	default:
		n, cur, off := m.list()
		if n == 0 {
			return
		}
		*cur = min(max(*cur+d, 0), n-1)
		keepVisible(cur, off, n, m.listHeight(m.view))
		m.detOff, m.openFor = 0, ""
	}
}

// far is a move past either end; moves clamp, so it lands on the first or last row.
const far = 1 << 30

// page is a screenful for the focused pane: a list's visible rows, or text less a line kept for context.
func (m *Model) page() int {
	if m.focus == paneMain && !m.inspect {
		return max(1, m.listHeight(m.view))
	}
	return max(1, m.layout().bodyH-2)
}

func (m *Model) selectRepo(i int) {
	m.repo = i
	m.resetCompare(viewOverview) // no view owns it
	for _, v := range []viewID{viewActivity, viewPRs, viewRepo, viewTags, viewEnvs, viewRuns} {
		m.cursor[v], m.offset[v] = 0, 0
	}
	m.detOff, m.openFor = 0, ""
}

// keepVisible scrolls a list of n rows, h of them on screen, so the cursor shows.
func keepVisible(cur, off *int, n, h int) {
	*off = min(max(*off, *cur-h+1), *cur)
	*off = max(0, min(*off, max(0, n-h)))
}

func (m *Model) clampAll() {
	bodyH := m.layout().bodyH
	for v := range viewCount {
		n, cur, off := m.listFor(v)
		keepVisible(cur, off, n, m.rowsIn(v, bodyH))
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
	return nil
}

// canOpen offers enter where it has a job: leaving the sidebar, or a row in Overview, Tags or Environments.
// Elsewhere v opens the detail page.
func (m *Model) canOpen() bool {
	if m.focus == paneSidebar {
		return true
	}
	return m.canView() && (m.view == viewOverview || (m.view == viewTags || m.view == viewEnvs) && !m.cmpOpen())
}

// canView offers v on a list with rows.
func (m *Model) canView() bool {
	n, _, _ := m.list()
	return m.focus == paneMain && !m.inspect && n > 0
}

func (m *Model) paging() bool { return m.inspect }

func (m *Model) notPaging() bool { return !m.inspect }

func (m *Model) canBack() bool {
	baseSet := m.view == viewTags && m.cmp.view == viewTags && m.cmp.base != ""
	return m.inspect || m.cmpOpen() || baseSet || m.filters[m.view] != ""
}

func (m *Model) back() {
	switch {
	case m.inspect:
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
func (m *Model) typing() bool { return m.filtering || m.renaming || m.grouping || m.scan.typing }

func (m *Model) filterable() bool {
	switch m.view {
	case viewConnections:
		return false
	}
	return !m.cmpOpen() && !m.inspect // the list sits behind the detail page
}

func (m *Model) setFilter(f string) {
	m.filters[m.view] = f
	_, cur, off := m.list()
	*cur, *off = 0, 0
}
