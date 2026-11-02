package ui

import "charm.land/bubbles/v2/key"

type actionID int

const (
	actUp actionID = iota
	actDown
	actFocusNext
	actFocusPrev
	actOpen
	actView
	actPageDown
	actPageUp
	actNextItem
	actPrevItem
	actBrowse
	actDiff
	actTab
	actSplitRight
	actSplitDown
	actBack
	actFilter
	actGo
	actRefresh
	actHelp
	actTheme
	actTime
	actProfile
	actAdd
	actAlerts
	actFix
	actRename
	actRemove
	actMoveUp
	actMoveDown
	actSort
	actQuit
)

// action is the single source for a key's binding, its help label and when it applies.
// Dispatch and the help/footer rendering both read this table.
type action struct {
	id    actionID
	key   key.Binding
	hint  bool              // shown in the footer
	basic bool              // kept when the footer runs out of room
	item  bool              // acts on the selected row, so the preview pane lists it
	when  func(*Model) bool // nil means always available
}

func bind(label, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, desc))
}

// The down action carries no help label: "j/k move" on the up action describes both.
var actions = []action{
	{id: actUp, key: bind("j/k", "move", "k", "up"), hint: true},
	{id: actDown, key: bind("", "", "j", "down")},
	{id: actFocusNext, key: bind("tab", "next / previous pane", "tab"), hint: true, when: (*Model).notPaging},
	{id: actFocusPrev, key: bind("", "", "shift+tab"), when: (*Model).notPaging}, // tab's label covers it, as j/k does for down
	{id: actOpen, key: bind("enter", "open", "enter"), hint: true, item: true, when: (*Model).canOpen},
	{id: actView, key: bind("v", "view details", "v"), hint: true, item: true, when: (*Model).canView},
	{id: actPageDown, key: bind("pgup/pgdn", "page", "pgdown", "space"), hint: true, when: (*Model).paging},
	{id: actPageUp, key: bind("", "", "pgup"), when: (*Model).paging},
	{id: actNextItem, key: bind("J/K", "next / previous item", "J", "shift+j"), hint: true, when: (*Model).paging},
	{id: actPrevItem, key: bind("", "", "K", "shift+k"), when: (*Model).paging},
	{id: actBrowse, key: bind("o", "open in browser", "o"), hint: true, item: true, when: (*Model).canBrowse},
	{id: actDiff, key: bind("d", "diff", "d"), hint: true, item: true, when: (*Model).canDiff},
	{id: actTab, key: bind("t", "new tab", "t"), hint: true, item: true, when: (*Model).canTab},
	{id: actSplitRight, key: bind("T/ctrl+t", "split right/down", "T", "shift+t"), item: true, when: (*Model).canTab},
	{id: actSplitDown, key: bind("", "", "ctrl+t"), when: (*Model).canTab},
	{id: actBack, key: bind("esc", "back", "esc"), hint: true, when: (*Model).canBack},
	{id: actFilter, key: bind("/", "filter", "/"), hint: true, when: (*Model).filterable},
	{id: actSort, key: bind("s", "sort", "s"), hint: true, when: (*Model).canSort},
	{id: actAdd, key: bind("a", "add repositories", "a"), hint: true, when: (*Model).canAdd},
	{id: actFix, key: bind("f", "find moved folder", "f"), hint: true, item: true, when: (*Model).canFix},
	{id: actRename, key: bind("r", "rename repository", "r"), item: true, when: (*Model).canManage},
	{id: actRemove, key: bind("x", "remove repository", "x"), item: true, when: (*Model).canManage},
	{id: actMoveUp, key: bind("K/J", "move repository up/down", "K", "shift+k"), item: true, when: (*Model).canManage},
	{id: actMoveDown, key: bind("", "", "J", "shift+j"), when: (*Model).canManage},
	{id: actGo, key: bind("g", "go to view…", "g")},
	{id: actAlerts, key: bind("n", "reviews / ready to merge", "n"), hint: true, when: (*Model).hasAlerts},
	{id: actHelp, key: bind("?", "help", "?"), hint: true, basic: true},
	{id: actProfile, key: bind("b", "profile header: full, ASCII, text, off", "b")}, // pinned right in the footer
	{id: actRefresh, key: bind("ctrl+r", "refresh", "ctrl+r"), hint: true, basic: true},
	{id: actTheme, key: bind("f2", "theme", "f2")},
	{id: actTime, key: bind("u", "UTC / local times", "u")},
	{id: actQuit, key: bind("ctrl+c", "quit", "ctrl+c")}, // handled first in handleKey; listed here for help
}

// goKeys are the second keys of the g sequences.
var goKeys = []struct {
	key  string
	view viewID
}{
	{"o", viewOverview},
	{"a", viewActivity},
	{"p", viewPRs},
	{"r", viewRepo},
	{"t", viewTags},
	{"e", viewEnvs},
	{"d", viewRuns},
	{"c", viewConnections},
}

func (a action) available(m *Model) bool { return a.when == nil || a.when(m) }
