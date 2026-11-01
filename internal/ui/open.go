package ui

import (
	"context"
	"errors"
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
var openBrowser = func(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Run()
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
	out, err := exec.CommandContext(ctx, "osascript", "-e", ghosttyOpen, dir, mode).CombinedOutput()
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
