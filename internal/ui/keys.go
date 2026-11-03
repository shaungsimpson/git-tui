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
	actGroup
	actNextGroup
	actPrevGroup
	actRemove
	actMoveUp
	actMoveDown
	actSort
	actPreview
	actDates
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
	group helpGroup         // where help lists it
	when  func(*Model) bool // nil means always available
}

// helpGroup is a heading on the help page: keys that work everywhere first, then by where they apply.
type helpGroup int

const (
	groupEverywhere helpGroup = iota
	groupLists
	groupDetail
	groupRepos
)

var groupNames = []string{"Everywhere", "Lists (selected row)", "Detail page", "Repositories"}

func bind(label, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, desc))
}

// The down action carries no help label: "j/k move" on the up action describes both.
var actions = []action{
	{id: actUp, key: bind("j/k", "move", "k", "up"), hint: true},
	{id: actDown, key: bind("", "", "j", "down")},
	{id: actFocusNext, key: bind("tab", "next / previous pane", "tab"), hint: true, when: (*Model).notPaging},
	{id: actFocusPrev, key: bind("", "", "shift+tab"), when: (*Model).notPaging}, // tab's label covers it, as j/k does for down
	{id: actOpen, key: bind("enter", "open", "enter"), hint: true, item: true, group: groupLists, when: (*Model).canOpen},
	{id: actView, key: bind("v", "view details", "v"), hint: true, item: true, group: groupLists, when: (*Model).canView},
	{id: actPageDown, key: bind("pgup/pgdn", "page", "pgdown", "space"), group: groupDetail, when: (*Model).paging},
	{id: actPageUp, key: bind("", "", "pgup"), when: (*Model).paging},
	{id: actNextItem, key: bind("J/K", "next / previous item", "J", "shift+j"), group: groupDetail, when: (*Model).paging},
	{id: actPrevItem, key: bind("", "", "K", "shift+k"), when: (*Model).paging},
	{id: actBrowse, key: bind("o", "open in browser", "o"), hint: true, item: true, group: groupLists, when: (*Model).canBrowse},
	{id: actDiff, key: bind("d", "diff", "d"), hint: true, item: true, group: groupLists, when: (*Model).canDiff},
	{id: actTab, key: bind("t", "new tab", "t"), hint: true, item: true, group: groupLists, when: (*Model).canTab},
	{id: actSplitRight, key: bind("T/ctrl+t", "split right/down", "T", "shift+t"), item: true, group: groupLists, when: (*Model).canTab},
	{id: actSplitDown, key: bind("", "", "ctrl+t"), when: (*Model).canTab},
	{id: actBack, key: bind("esc", "back", "esc"), hint: true, when: (*Model).canBack},
	{id: actFilter, key: bind("/", "filter", "/"), hint: true, group: groupLists, when: (*Model).filterable},
	{id: actSort, key: bind("s", "sort", "s"), hint: true, group: groupLists, when: (*Model).canSort},
	{id: actAdd, key: bind("a", "add repositories", "a"), hint: true, group: groupRepos, when: (*Model).canAdd},
	{id: actFix, key: bind("f", "find moved folder", "f"), hint: true, item: true, group: groupRepos, when: (*Model).canFix},
	{id: actRename, key: bind("r", "rename repository", "r"), item: true, group: groupRepos, when: (*Model).canManage},
	{id: actGroup, key: bind("e", "set group", "e"), item: true, group: groupRepos, when: (*Model).canManage},
	{id: actRemove, key: bind("x", "remove repository", "x"), item: true, group: groupRepos, when: (*Model).canManage},
	{id: actMoveUp, key: bind("K/J", "move repository up/down", "K", "shift+k"), item: true, group: groupRepos, when: (*Model).canManage},
	{id: actMoveDown, key: bind("", "", "J", "shift+j"), when: (*Model).canManage},
	{id: actNextGroup, key: bind("[/]", "next / previous group", "]"), hint: true, when: (*Model).hasGroups},
	{id: actPrevGroup, key: bind("", "", "["), when: (*Model).hasGroups},
	{id: actGo, key: bind("g", "go to view…", "g")},
	{id: actAlerts, key: bind("n", "reviews / ready to merge", "n"), hint: true, group: groupLists, when: (*Model).hasAlerts},
	{id: actHelp, key: bind("?", "help", "?"), hint: true, basic: true},
	{id: actProfile, key: bind("b", "profile header: full, ASCII, text, off", "b")}, // pinned right in the footer
	{id: actRefresh, key: bind("ctrl+r", "refresh", "ctrl+r")},                      // loads refresh and retry on their own
	{id: actPreview, key: bind("p", "preview pane", "p"), hint: true, when: (*Model).canPreview},
	{id: actTheme, key: bind("f2", "theme", "f2")},
	{id: actTime, key: bind("u", "UTC / local times", "u")},
	{id: actDates, key: bind("w", "dates: numeric, relative, short, weekday", "w")},
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
