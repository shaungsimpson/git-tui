package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
)

// groups is every group a repository is in, A to Z.
func (m *Model) groups() []string {
	var gs []string
	for _, r := range m.repos {
		if g := r.cfg.Group; g != "" && !slices.Contains(gs, g) {
			gs = append(gs, g)
		}
	}
	slices.SortFunc(gs, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return gs
}

func (m *Model) hasGroups() bool { return len(m.groups()) > 0 }

// inGroup reports whether repos[i] is shown: hidden repositories are neither drawn nor loaded.
func (m *Model) inGroup(i int) bool { return m.group == "" || m.repos[i].cfg.Group == m.group }

// shown is the repositories the group shows, in list order.
func (m *Model) shown() []int {
	var out []int
	for i := range m.repos {
		if m.inGroup(i) {
			out = append(out, i)
		}
	}
	return out
}

// cycleGroup shows all, then each group in turn. Git data for the newly shown repositories reloads at once, since
// it costs no network; GitHub data reloads only if stale, so flicking between groups never floods gh.
func (m *Model) cycleGroup(d int) tea.Cmd {
	gs := append([]string{""}, m.groups()...)
	i := slices.Index(gs, m.group) // -1 only if the group just went away, which lands on all or the first group
	m.setGroup(gs[(i+d+len(gs))%len(gs)])
	m.flash = "Showing all repositories."
	if m.group != "" {
		m.flash = "Showing the " + clean(m.group) + " group."
	}
	m.savePrefs(func(s *config.State) { s.Group = m.group })
	var cmds []tea.Cmd
	for _, i := range m.shown() {
		cmds = append(cmds, m.load(i, kindSummary, true), m.load(i, kindLog, true))
	}
	return tea.Batch(append(cmds, m.ensure(false))...)
}

// setGroup shows a group, moving the selection to all repositories, and closing a commit list, of a hidden one.
func (m *Model) setGroup(g string) {
	if g != m.group {
		m.alertSet = false // the review count changes with the group, which is not news
	}
	m.group = g
	if m.repo > 0 && !m.inGroup(m.repo-1) {
		m.selectRepo(0)
	}
	if m.cmp.repo > 0 && !m.inGroup(m.cmp.repo-1) { // Environments opens one for a row while all are selected
		m.resetCompare(viewOverview)
	}
	m.closeDiff()
	m.clampAll()
}

func (m *Model) startGroup() tea.Cmd {
	i := m.connRepo()
	if i < 0 {
		return nil
	}
	m.grouping, m.groupTabs = true, nil
	m.input.Prompt = "Group: "
	m.input.SetValue(m.repos[i].cfg.Group)
	m.input.CursorEnd()
	return m.input.Focus()
}

// groupKey edits the group in the footer: tab cycles the existing groups that start with what was typed.
func (m *Model) groupKey(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); s {
	case "enter", "esc":
		m.grouping = false
		m.input.Blur()
		if s == "enter" {
			return m.regroup(m.input.Value())
		}
		return nil
	case "tab":
		if m.groupTabs == nil {
			typed := strings.ToLower(strings.TrimSpace(m.input.Value()))
			m.groupTabs = []string{}
			for _, g := range m.groups() {
				if strings.HasPrefix(strings.ToLower(g), typed) {
					m.groupTabs = append(m.groupTabs, g)
				}
			}
			m.groupTab = 0
		}
		if len(m.groupTabs) > 0 {
			m.input.SetValue(m.groupTabs[m.groupTab%len(m.groupTabs)])
			m.input.CursorEnd()
			m.groupTab++
		}
		return nil
	}
	m.groupTabs = nil // typing starts a new completion
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// regroup saves the selected repository's group; a name matching an existing group in another case joins it.
func (m *Model) regroup(g string) tea.Cmd {
	i := m.connRepo()
	g = strings.TrimSpace(g)
	if j := slices.IndexFunc(m.groups(), func(e string) bool { return strings.EqualFold(e, g) }); j >= 0 {
		g = m.groups()[j]
	}
	if i < 0 || g == m.repos[i].cfg.Group {
		return nil
	}
	next := m.repoConfigs()
	next[i].Group = g
	done := "Removed " + clean(next[i].Name) + " from its group."
	if g != "" {
		done = "Moved " + clean(next[i].Name) + " to the " + clean(g) + " group."
	}
	return m.applyRepos(next, done)
}
