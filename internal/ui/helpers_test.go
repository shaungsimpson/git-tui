package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

// TestMain leaves out the idle refresh tick, which would make drain wait out its timeout in every test;
// the refresh tests drive onRefresh directly. Keys settle at once, so a key's loads finish within press.
func TestMain(m *testing.M) {
	refreshTick = func() tea.Cmd { return nil }
	settleAfter = 0
	os.Exit(m.Run())
}

func demoOpts() Options {
	o := Options{Source: gitx.Demo{}, Demo: true, Build: gitx.DemoBuild, Tools: map[string]string{"git": "/usr/bin/git"}}
	for _, n := range gitx.DemoRepos {
		o.Repos = append(o.Repos, config.Repo{Name: n, Path: "/demo/" + n, Group: gitx.DemoGroups[n], Status: config.StatusOK})
	}
	o.Repos = append(o.Repos, config.Repo{Name: "infra", Path: "/demo/infra", Group: gitx.DemoGroups["infra"], Status: config.StatusMissing, Err: "directory not found"})
	return o
}

// drain runs a command tree synchronously and feeds results back. Commands that block (ticks, cursor blink) are skipped.
func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drain(m, c)
			}
			return
		}
		if msg != nil {
			_, next := m.Update(msg)
			drain(m, next)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

// answer is the newest result for slot s, as a finished load would send it.
func answer[T any](s *slot[T], kind dataKind, repo int, val any, err error) resultMsg {
	s.gen++
	return resultMsg{kind: kind, repo: repo, gen: s.gen, val: val, err: err, store: func(msg resultMsg) { finish(s, msg) }}
}

// knowSlugs gives every repository its GitHub name without loading it, for tests that start loads by hand.
func knowSlugs(m *Model) {
	for _, r := range m.repos {
		r.slug = slot[string]{loaded: true, val: "acme/" + r.cfg.Name}
	}
	m.shareGitHub()
}

func setup(t *testing.T, o Options, w, h int) *Model {
	t.Helper()
	m := New(o)
	_, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	drain(m, tea.Batch(cmd, m.Init())) // as Bubble Tea would: a resize can start loads too
	return m
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "f2":
		return tea.KeyPressMsg{Code: tea.KeyF2}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	case "ctrl+t":
		return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	case "T":
		return tea.KeyPressMsg{Code: 't', ShiftedCode: 'T', Text: "T", Mod: tea.ModShift}
	case "J":
		return tea.KeyPressMsg{Code: 'j', ShiftedCode: 'J', Text: "J", Mod: tea.ModShift}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		if cmd != nil && k != "g" && k != "/" { // g starts a 1s timer; / starts cursor blink
			drain(m, cmd)
		}
	}
}

// countingSource counts loads: logs return a commit whose subject records which call produced it,
// and commit lists come from the demo.
type countingSource struct {
	gitx.Demo
	calls, compares, prs atomic.Int32
}

func (c *countingSource) PullRequests(ctx context.Context, repo, dir string) ([]gitx.PR, error) {
	c.prs.Add(1)
	return c.Demo.PullRequests(ctx, repo, dir)
}

func (c *countingSource) Log(_ context.Context, repo, _ string, _ int) ([]gitx.Commit, error) {
	n := c.calls.Add(1)
	return []gitx.Commit{{Repo: repo, Hash: "abc", Subject: "call " + string('0'+n)}}, nil
}

func (c *countingSource) Compare(ctx context.Context, repo, dir, base, head string) ([]gitx.Commit, error) {
	c.compares.Add(1)
	return c.Demo.Compare(ctx, repo, dir, base, head)
}

// fakeClock moves the refresh cadence's clock for one test.
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	real := clock
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = real })
	return &now
}

// gitDirs makes a git repository folder for each path under a new root.
func gitDirs(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(root, p, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func savedNames(t *testing.T, path string) string {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range cfg.Repositories {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
}

// noSlug is a source whose folders have no GitHub remote it can read, until ok is set.
type noSlug struct {
	gitx.Demo
	ok *atomic.Bool
}

func (s noSlug) Slug(ctx context.Context, dir string) (string, error) {
	if s.ok.Load() {
		return s.Demo.Slug(ctx, dir)
	}
	return "", errors.New("origin is not a GitHub remote")
}
