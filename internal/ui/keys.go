package ui

import "charm.land/bubbles/v2/key"

type actionID int

const (
	actUp actionID = iota
	actDown
	actFocusNext
	actFocusPrev
	actOpen
	actBrowse
	actDiff
	actBack
	actFilter
	actGo
	actRefresh
	actHelp
	actTheme
	actTime
	actProfile
	actQuit
)

// action is the single source for a key's binding, its help label and when it applies.
// Dispatch and the help/footer rendering both read this table.
type action struct {
	id   actionID
	key  key.Binding
	hint bool              // shown in the footer
	when func(*Model) bool // nil means always available
}

func bind(label, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, desc))
}

// The down action carries no help label: "j/k move" on the up action describes both.
var actions = []action{
	{id: actUp, key: bind("j/k", "move", "k", "up"), hint: true},
	{id: actDown, key: bind("", "", "j", "down")},
	{id: actFocusNext, key: bind("tab", "next pane", "tab"), hint: true},
	{id: actFocusPrev, key: bind("shift+tab", "previous pane", "shift+tab")},
	{id: actOpen, key: bind("enter", "open", "enter"), hint: true, when: (*Model).canOpen},
	{id: actBrowse, key: bind("o", "open in browser", "o"), hint: true, when: (*Model).canBrowse},
	{id: actDiff, key: bind("d", "diff", "d"), hint: true, when: (*Model).canDiff},
	{id: actBack, key: bind("esc", "back", "esc"), hint: true, when: (*Model).canBack},
	{id: actFilter, key: bind("/", "filter", "/"), hint: true, when: (*Model).filterable},
	{id: actGo, key: bind("g", "go to view…", "g")},
	{id: actHelp, key: bind("?", "help", "?"), hint: true},
	{id: actProfile, key: bind("b", "profile header: full, ASCII, text, off", "b")}, // pinned right in the footer
	{id: actRefresh, key: bind("ctrl+r", "refresh", "ctrl+r"), hint: true},          // after help, so help survives a narrow footer
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
