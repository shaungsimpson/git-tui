package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

const (
	pulseBeat  = 400 * time.Millisecond
	pulseBeats = 8 // about three seconds of bright and dim
)

type pulseMsg struct{}

// ready reports whether a PR is the viewer's own and can be merged: approved, checks passing, not a draft.
func (m *Model) ready(p gitx.PR) bool {
	login := m.me.val.Login
	return login != "" && strings.EqualFold(p.Author, login) && !p.Draft && p.Review == "APPROVED" && p.Checks == gitx.ChecksPassing
}

// capped keeps a badge short: anything above 9 is 9+.
func capped(n int) string {
	if n > 9 {
		return "9+"
	}
	return strconv.Itoa(n)
}

// pill draws text on the style's background with half-block ends, or in brackets for plain-character headers.
func pill(st lipgloss.Style, text string, ascii bool) string {
	if ascii {
		return st.Render("[" + text + "]")
	}
	end := lipgloss.NewStyle().Foreground(st.GetBackground())
	return end.Render("▐") + st.Render(text) + end.Render("▌")
}

// badges are the loud counts of review requests and PRs ready to merge; empty when both are zero.
func (m *Model) badges(ascii bool) string {
	s := m.profileStats(clock())
	review, ready := "🔔 ", "✔ "
	if ascii {
		review, ready = "! ", "ok "
	}
	var out []string
	if s.requested > 0 {
		st := m.st.alert
		if m.pulse%2 == 1 {
			st = m.st.alertDim
		}
		out = append(out, pill(st, review+capped(s.requested), ascii))
	}
	if s.ready > 0 {
		out = append(out, pill(m.st.readyBadge, ready+capped(s.ready), ascii))
	}
	return strings.Join(out, " ")
}

// withBadges puts the badges at the right end of a header line.
func (m *Model) withBadges(line string, ascii bool) string {
	b := m.badges(ascii)
	if b == "" {
		return line
	}
	return fit(line, b+" ", m.width)
}

// watchReviews pulses the review badge when the count rises. The count climbing while every repository's PRs
// first load is not news, so nothing pulses until each has been read once.
func (m *Model) watchReviews() tea.Cmd {
	for _, r := range m.repos {
		if r.cfg.Status == config.StatusOK && r.gh.prs.tried.IsZero() {
			m.alertSet = false
			return nil
		}
	}
	n := m.profileStats(clock()).requested
	rose := m.alertSet && n > m.alertBase
	m.alertBase, m.alertSet = n, true
	if !rose {
		return nil
	}
	running := m.pulse > 0
	m.pulse = pulseBeats
	if running {
		return nil // the running tick carries on with the fresh count
	}
	return pulseTick()
}

func pulseTick() tea.Cmd {
	return tea.Tick(pulseBeat, func(time.Time) tea.Msg { return pulseMsg{} })
}

func (m *Model) onPulse() tea.Cmd {
	if m.pulse > 0 {
		m.pulse--
	}
	if m.pulse == 0 {
		return nil
	}
	return pulseTick()
}

// nextAlertFilter is what n shows next in Pull requests: review requests, then PRs ready to merge, then all.
func nextAlertFilter(f string) string {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "is:review":
		return "is:ready"
	case "is:ready":
		return ""
	}
	return "is:review"
}

func (m *Model) showAlerts() tea.Cmd {
	next := nextAlertFilter(m.filters[viewPRs])
	if next != "" && m.repo != 0 {
		m.selectRepo(0) // review requests and ready PRs can be in any repository
	}
	cmd := m.setView(viewPRs)
	m.setFilter(next)
	switch next {
	case "is:review":
		m.flash = "Your review requests. Press n again for your PRs ready to merge."
	case "is:ready":
		m.flash = "Your PRs ready to merge. Press n again to show every PR."
	}
	return cmd
}
