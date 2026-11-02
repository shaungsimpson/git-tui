package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOpenPullRequestInBrowser(t *testing.T) {
	var opened []string
	real := openBrowser
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openBrowser = real })

	m := setup(t, demoOpts(), 140, 40)
	if m.canBrowse() {
		t.Fatal("o should only be offered on pull requests and deploy runs")
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
	if !strings.Contains(m.render(), "open in browser") {
		t.Fatal("the preview should offer o on pull requests")
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

	press(m, "g", "d", "j")
	run := m.runList()[1]
	press(m, "o")
	if len(opened) != 2 || opened[1] != run.URL || !strings.Contains(m.flash, run.Workflow) {
		t.Fatalf("o should open the selected deploy run: %v, flash %q", opened, m.flash)
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

func TestTabCommand(t *testing.T) {
	if name, args, err := tabCommand("/r/backend", "right", ""); err != nil || name != "osascript" || !slices.Equal(args[2:], []string{"/r/backend", "right"}) {
		t.Fatalf("macOS: %s %q %v", name, args, err)
	}
	for mode, want := range map[string]string{
		"tab":   "-w 0 new-tab wsl.exe -d Ubuntu --cd /r/my backend",
		"right": "-w 0 split-pane -V wsl.exe -d Ubuntu --cd /r/my backend",
		"down":  "-w 0 split-pane -H wsl.exe -d Ubuntu --cd /r/my backend",
	} {
		if name, args, err := tabCommand("/r/my backend", mode, "Ubuntu"); err != nil || name != "wt.exe" || strings.Join(args, " ") != want {
			t.Errorf("WSL %s: %s %q %v", mode, name, args, err)
		}
	}
	if _, _, err := tabCommand("/r/a;new-tab cmd.exe", "tab", "Ubuntu"); err == nil {
		t.Fatal("a ; would start another Windows Terminal command, so refuse it")
	}
}

func TestCanOpenTabs(t *testing.T) {
	env := map[string]string{"TERM_PROGRAM": "ghostty", "WSL_DISTRO_NAME": "Ubuntu", "WT_SESSION": "abc"}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "wt.exe"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if !CanOpenTabs(func(k string) string { return env[k] }) {
		t.Fatal("Ghostty on macOS, or Windows Terminal on WSL, can open tabs")
	}
	env["TMUX"] = "/tmp/tmux"
	if CanOpenTabs(func(k string) string { return env[k] }) {
		t.Fatal("not inside tmux")
	}
	if runtime.GOOS == "darwin" {
		return
	}
	delete(env, "TMUX")
	delete(env, "WT_SESSION")
	if CanOpenTabs(func(k string) string { return env[k] }) {
		t.Fatal("WSL outside Windows Terminal cannot open its tabs")
	}
}

func TestBrowserOpener(t *testing.T) {
	if err := startBounded(exec.Command("sh", "-c", "exit 3"), time.Second); err == nil {
		t.Fatal("an opener that fails should say so")
	}
	start := time.Now()
	if err := startBounded(exec.Command("sleep", "5"), 50*time.Millisecond); err != nil || time.Since(start) > time.Second {
		t.Fatalf("an opener still running is left open without waiting for it: %v after %v", err, time.Since(start))
	}
	if runtime.GOOS == "darwin" {
		if got := browserCommand(); got != "open" {
			t.Fatalf("macOS opens with open, got %s", got)
		}
		return
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "wslview"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if got := browserCommand(); got != "wslview" {
		t.Fatalf("WSL with wslview should use it, got %s", got)
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	if got := browserCommand(); got != "xdg-open" {
		t.Fatalf("Linux should use xdg-open, got %s", got)
	}
}
