package ui

import (
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestOpenPullRequestInBrowser(t *testing.T) {
	var opened []string
	real := openBrowser
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openBrowser = real })

	m := setup(t, demoOpts(), 140, 40)
	if m.canBrowse() {
		t.Fatal("o should only be offered on pull requests")
	}
	press(m, "g", "p", "j")
	want := m.prList()[1]
	press(m, "o")
	if len(opened) != 1 || opened[0] != want.URL {
		t.Fatalf("opened %v, want %s", opened, want.URL)
	}
	if !strings.Contains(m.flash, "#"+strconv.Itoa(want.Number)) {
		t.Fatalf("flash %q", m.flash)
	}
	if !strings.Contains(m.footer(), "open in browser") {
		t.Fatal("footer should offer o on pull requests")
	}

	m.repos[0].gh.prs.val[0].URL = "file:///etc/passwd"
	for i, p := range m.prList() {
		if p.URL == "file:///etc/passwd" {
			m.cursor[viewPRs] = i
		}
	}
	press(m, "o")
	if len(opened) != 1 {
		t.Fatalf("a non-https link must not be opened: %v", opened)
	}
}

func TestOpenTab(t *testing.T) {
	var opened []string
	real := openTab
	openTab = func(dir, mode string) error { opened = append(opened, mode+" "+dir); return nil }
	t.Cleanup(func() { openTab = real })

	o := demoOpts()
	o.Demo = false
	m := setup(t, o, 140, 30)
	if m.canTab() {
		t.Fatal("t should only be offered where the terminal can open tabs")
	}
	m.opts.Tabs = true
	press(m, "t", "T", "ctrl+t") // Overview's first row: backend
	if !slices.Equal(opened, []string{"tab /demo/backend", "right /demo/backend", "down /demo/backend"}) {
		t.Fatalf("opened %v", opened)
	}
	m.repos[0].cfg.Path = "backend" // relative paths in the config are read from our directory
	press(m, "t")
	if want, _ := filepath.Abs("backend"); opened[3] != "tab "+want {
		t.Fatalf("opened %q, want the absolute path %s", opened[3], want)
	}
	press(m, "shift+tab", "k") // sidebar: all repositories
	if m.canTab() {
		t.Fatal("not for all repositories")
	}
	press(m, "j", "j", "j", "j", "j") // sidebar: infra, whose directory is missing
	if m.repos[m.repo-1].cfg.Name != "infra" || m.canTab() {
		t.Fatalf("repo %d: not for a repository without a directory", m.repo)
	}
	openTab = func(string, string) error { return errors.New("not allowed") }
	press(m, "k", "T")
	if !strings.Contains(m.flash, "Could not open a split to the right: not allowed") {
		t.Fatalf("flash %q", m.flash)
	}
}
