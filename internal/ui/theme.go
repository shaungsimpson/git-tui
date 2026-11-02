package ui

import (
	"hash/fnv"
	"image/color"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type styles struct {
	bg, fg                                 color.Color
	text, muted, accent, title, titleFocus lipgloss.Style
	selected, marker, sep, ok, warn, bad   lipgloss.Style
	badge, tab, tabActive, key             lipgloss.Style
	date, id                               lipgloss.Style   // row data by kind: when, and commit or PR numbers
	alert, alertDim, readyBadge            lipgloss.Style   // the header's review and ready-to-merge pills
	updateBadge                            lipgloss.Style   // the header's pill for a newer release
	authors                                []lipgloss.Style // one per person, picked by authorStyle
}

func newStyles(dark bool) styles {
	pick := lipgloss.LightDark(dark)
	c := func(light, dark string) color.Color { return pick(lipgloss.Color(light), lipgloss.Color(dark)) }
	var (
		bg     = c("#fafaf8", "#1a1b1e")
		fg     = c("#23262b", "#d8d9dc")
		muted  = c("#6b6f76", "#8a8d93")
		accent = c("#1f7a68", "#5fb3a1")
		selBg  = c("#e2e6e4", "#2b3033")
		border = c("#d4d6d9", "#363a3f")
		ok     = c("#2f7d32", "#7cc47f")
		warn   = c("#8a5d00", "#e0b45c")
		bad    = c("#b3261e", "#e27d7d")
		date   = c("#56627e", "#8e9bbd") // blue-grey, calmer than titles yet apart from the plain grey
		id     = c("#7d6a32", "#c4b37f") // sand, a softer hue than the amber warning
	)
	s := lipgloss.NewStyle
	return styles{
		bg: bg, fg: fg,
		text:       s().Foreground(fg),
		muted:      s().Foreground(muted),
		accent:     s().Foreground(accent),
		title:      s().Foreground(muted).Bold(true),
		titleFocus: s().Foreground(accent).Bold(true),
		selected:   s().Foreground(fg).Background(selBg).Bold(true),
		marker:     s().Foreground(accent).Bold(true),
		sep:        s().Foreground(border),
		ok:         s().Foreground(ok),
		warn:       s().Foreground(warn),
		bad:        s().Foreground(bad),
		badge:      s().Foreground(bg).Background(warn).Bold(true).Padding(0, 1),
		// Solid backgrounds with white text, stronger than any other colour on screen so the pills read as alerts.
		alert:       s().Foreground(lipgloss.Color("#ffffff")).Background(c("#c62828", "#e5484d")).Bold(true),
		alertDim:    s().Foreground(lipgloss.Color("#ffffff")).Background(c("#e8a09c", "#6e2b2d")),
		readyBadge:  s().Foreground(lipgloss.Color("#ffffff")).Background(c("#2e7d32", "#2f9e57")).Bold(true),
		updateBadge: s().Foreground(bg).Background(accent).Bold(true),
		tab:         s().Foreground(muted),
		tabActive:   s().Foreground(accent).Bold(true).Underline(true),
		key:         s().Foreground(accent),
		date:        s().Foreground(date),
		id:          s().Foreground(id),
		// Hues apart from the status colours (red, green, amber) and the accent, each readable on its theme.
		authors: []lipgloss.Style{
			s().Foreground(c("#3b62c4", "#7aa2f7")), // blue
			s().Foreground(c("#7646c2", "#b394f0")), // violet
			s().Foreground(c("#a8327f", "#e08ac2")), // magenta
			s().Foreground(c("#17758f", "#6cc7dc")), // cyan
			s().Foreground(c("#8a5432", "#d4a27c")), // brown
			s().Foreground(c("#4f5f86", "#a3b4d9")), // slate
		},
	}
}

// clean makes external text (commit subjects, branch names, git errors) safe to print in a cell grid.
func clean(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		case (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069): // direction overrides could reorder text
			return -1
		}
		return r
	}, s)
}

// authorStyle colours a person's name the same way everywhere: the viewer in the accent colour, bots muted,
// and everyone else by a hash of the name with case and punctuation dropped, so "Jane Doe" and
// "jane.doe" match. A GitHub login and a git name for the same person still differ, except for the viewer.
// login says name is a GitHub login (a PR author): the viewer then matches only their own login exactly,
// since anna-lee and annalee are different accounts.
func (m *Model) authorStyle(name string, login bool) lipgloss.Style {
	key := nameKey(name)
	v := m.me.val
	lower := strings.ToLower(name)
	switch {
	case key == "":
		return m.st.muted
	case strings.HasSuffix(lower, "[bot]") || strings.HasPrefix(lower, "app/"): // gh reports GitHub Apps as app/<name>
		return m.st.muted
	case login && v.Login != "" && strings.EqualFold(name, v.Login):
		return m.st.accent
	case !login && (key == nameKey(v.Login) || key == nameKey(v.Name) || key == nameKey(v.GitName)):
		return m.st.accent
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key)) // a hash never fails to write
	return m.st.authors[h.Sum32()%uint32(len(m.st.authors))]
}

// nameKey is a name with only its lowercase letters and digits.
func nameKey(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}
