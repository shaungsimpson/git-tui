package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shaungsimpson/git-tui/internal/config"
)

type browserMsg struct {
	what string
	err  error
}

// openBrowser opens a web page in the default browser; a variable so tests never launch one.
var openBrowser = func(url string) error { return startBounded(exec.Command(browserCommand(), url), 10*time.Second) }

// browserCommand is the opener for this system: open on macOS, wslview on WSL when installed, else xdg-open.
func browserCommand() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	if _, err := exec.LookPath("wslview"); err == nil && os.Getenv("WSL_DISTRO_NAME") != "" {
		return "wslview" // xdg-open on WSL often has no browser behind it
	}
	return "xdg-open"
}

// startBounded runs cmd and reports its failure, waiting at most wait. Some openers are the browser itself and
// run until it closes, so one still running then is left open rather than killed.
func startBounded(cmd *exec.Cmd, wait time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1) // buffered: Wait still reaps the process after we stop listening
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(wait):
		return nil
	}
}

type tabMsg struct {
	what string
	err  error
}

// ghosttyOpen opens a Ghostty tab, or splits the focused pane, in a directory.
// The path and mode are arguments, never part of the script text.
const ghosttyOpen = `on run argv
	set dir to item 1 of argv
	set mode to item 2 of argv
	tell application "Ghostty"
		set cfg to new surface configuration
		set initial working directory of cfg to dir
		if mode is "tab" then
			new tab in front window with configuration cfg
		else
			set here to focused terminal of selected tab of front window
			if mode is "right" then
				split here direction right with configuration cfg
			else
				split here direction down with configuration cfg
			end if
		end if
	end tell
end run`

// wtSplits are Windows Terminal's split-pane flags: a vertical divider puts the new pane to the right.
var wtSplits = map[string]string{"right": "-V", "down": "-H"}

// tabCommand is the program and arguments that open a tab or split at dir: Windows Terminal on WSL (distro set),
// otherwise Ghostty's AppleScript.
func tabCommand(dir, mode, distro string) (string, []string, error) {
	if distro == "" {
		return "osascript", []string{"-e", ghosttyOpen, dir, mode}, nil
	}
	if strings.Contains(dir, ";") { // wt.exe splits its arguments into commands at each ;
		return "", nil, errors.New("a path containing ; cannot open in Windows Terminal")
	}
	args := []string{"-w", "0", "new-tab"}
	if mode != "tab" {
		args = []string{"-w", "0", "split-pane", wtSplits[mode]}
	}
	return "wt.exe", append(args, "wsl.exe", "-d", distro, "--cd", dir), nil
}

// CanOpenTabs reports whether t can open a tab here: Ghostty on macOS, or Windows Terminal on WSL, outside tmux.
func CanOpenTabs(getenv func(string) string) bool {
	if getenv("TMUX") != "" {
		return false // the tab would open in the terminal, not in tmux
	}
	if runtime.GOOS == "darwin" {
		return getenv("TERM_PROGRAM") == "ghostty"
	}
	_, err := exec.LookPath("wt.exe")
	return err == nil && getenv("WSL_DISTRO_NAME") != "" && getenv("WT_SESSION") != "" // WT_SESSION: inside Windows Terminal
}

// openModes names what each mode opens, for messages.
var openModes = map[actionID]struct{ mode, what string }{
	actTab:        {"tab", "a tab"},
	actSplitRight: {"right", "a split to the right"},
	actSplitDown:  {"down", "a split below"},
}

// openTab opens a terminal tab or split ("tab", "right" or "down") at dir; a variable so tests never launch one.
var openTab = func(dir, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) // long enough to answer a macOS permission prompt
	defer cancel()
	name, args, err := tabCommand(dir, mode, os.Getenv("WSL_DISTRO_NAME"))
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return err
}

// tabTarget is the repository t opens a tab in, or -1 when the selected one has no usable directory.
func (m *Model) tabTarget() int {
	i := m.selectedRepo()
	if i < 0 || m.repos[i].cfg.Status != config.StatusOK {
		return -1
	}
	return i
}

// canTab offers t where the terminal can open a tab; demo shows it everywhere but only says what it would do.
func (m *Model) canTab() bool {
	return (m.opts.Tabs || m.opts.Demo) && m.tabTarget() >= 0
}

// browseTarget is the web link for the selected row, and how to name it; empty where there is none.
func (m *Model) browseTarget() (url, what string) {
	if m.focus == paneSidebar { // the sidebar's row is a repository, as for d and t
		return "", ""
	}
	if r, ok := m.selectedRun(); ok {
		return r.URL, "the " + clean(r.Workflow) + " run"
	}
	if m.view != viewPRs {
		return "", ""
	}
	n, cur, _ := m.list()
	if n == 0 {
		return "", ""
	}
	p := m.prList()[*cur]
	return p.URL, clean(p.Repo) + " #" + strconv.Itoa(p.Number)
}

func (m *Model) canBrowse() bool {
	url, _ := m.browseTarget()
	return url != ""
}
