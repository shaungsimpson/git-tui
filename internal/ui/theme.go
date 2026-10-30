package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type styles struct {
	bg, fg                                 color.Color
	text, muted, accent, title, titleFocus lipgloss.Style
	selected, marker, sep, ok, warn, bad   lipgloss.Style
	badge, tab, tabActive, key             lipgloss.Style
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
		tab:        s().Foreground(muted),
		tabActive:  s().Foreground(accent).Bold(true).Underline(true),
		key:        s().Foreground(accent),
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
		}
		return r
	}, s)
}
