package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

func TestManageRepositories(t *testing.T) {
	root := gitDirs(t, "a", "b", "group/a")
	path := filepath.Join(t.TempDir(), "git-tui", "config.json")
	m := setup(t, Options{Source: gitx.Demo{}, ConfigPath: path, ConfigErr: config.ErrNotFound}, 120, 30)

	press(m, "a") // offered while nothing is configured
	if !m.scan.open || !m.scan.typing {
		t.Fatal("a should open the add screen with the folder to edit")
	}
	m.input.SetValue(root)
	press(m, "enter")
	if len(m.scan.res.val) != 3 {
		t.Fatalf("want 3 repositories found, got %+v", m.scan.res.val)
	}
	press(m, "space", "j", "space", "j", "space", "enter")
	if got := savedNames(t, path); got != "a,b,group/a" {
		t.Fatalf("a name already in use should get its parent folder, got %s", got)
	}
	if m.scan.open || len(m.repos) != 3 || m.opts.ConfigErr != nil {
		t.Fatal("adding should close the screen and show the new list")
	}

	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections()) // the row for a
	press(m, "r")
	m.input.SetValue("b")
	press(m, "enter")
	if !strings.Contains(m.flash, "already called b") {
		t.Fatalf("a duplicate name should be refused: %q", m.flash)
	}
	press(m, "r")
	m.input.SetValue("alpha")
	press(m, "enter", "J")
	if got := savedNames(t, path); got != "b,alpha,group/a" {
		t.Fatalf("rename then J: got %s", got)
	}
	if m.connRepo() != 1 {
		t.Fatal("the cursor should follow the moved repository")
	}
	press(m, "x", "n")
	if len(m.repos) != 3 {
		t.Fatal("anything but y keeps the repository")
	}
	press(m, "x", "y")
	if got := savedNames(t, path); got != "b,group/a" {
		t.Fatalf("remove: got %s", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("the previous file should be kept as .bak")
	}

	hand := `{"repositories":[{"name":"by-hand","path":"/by/hand"}]}`
	if err := os.WriteFile(path, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	m.cursor[viewConnections] = repoRows(m.connections())
	press(m, "J")
	if data, _ := os.ReadFile(path); string(data) != hand || !strings.Contains(m.flash, "changed outside git-tui") {
		t.Fatalf("an edit made by hand must not be saved over: %q, %s", m.flash, data)
	}
}

func TestReorderKeepsLoadedData(t *testing.T) {
	src := &countingSource{}
	o := demoOpts()
	o.Source = src
	m := setup(t, o, 120, 30)
	first, prs := m.repos[0], src.prs.Load()
	inFlight := m.load(0, kindLog, true)
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections())
	press(m, "J")
	if m.repos[1] != first || src.prs.Load() != prs {
		t.Fatal("a reorder should keep each repository's data and reload nothing from GitHub")
	}
	m.Update(inFlight()) // started before the reorder, when it was first in the list
	if first.log.loading || !strings.HasPrefix(first.log.val[0].Subject, "call") {
		t.Fatal("a load running during a reorder should land on its own repository")
	}
}

func TestRenameDuringGitHubLookup(t *testing.T) {
	m := New(demoOpts())                // not started, so no lookup has run yet
	lookup := m.load(0, kindPRs, false) // the GitHub name comes first
	if !m.repos[0].slug.loading {
		t.Fatal("setup: the GitHub-name lookup should be running")
	}
	next := m.repoConfigs()
	next[0].Name = "renamed"
	m.applyRepos(next, "")
	m.Update(lookup())
	if r := m.repos[0]; r.cfg.Name != "renamed" || !r.slug.loaded || r.gh.slug == "" {
		t.Fatalf("a lookup running during a rename should still land: %+v", r.slug)
	}
}

func TestOneFolderListedTwice(t *testing.T) {
	o := demoOpts()
	o.Repos = []config.Repo{{Name: "api", Path: "/demo/api", Status: config.StatusOK}, {Name: "api-again", Path: "/demo/api", Status: config.StatusOK}}
	m := setup(t, o, 120, 30)
	first, second := m.repos[0], m.repos[1]
	next := m.repoConfigs()
	next[0], next[1] = next[1], next[0]
	m.applyRepos(next, "")
	if m.repos[0] != second || m.repos[1] != first || first.cfg.Name != "api" || second.cfg.Name != "api-again" {
		t.Fatalf("each entry should keep its own state and name: %q, %q", m.repos[0].cfg.Name, m.repos[1].cfg.Name)
	}
}

func TestFindMovedFolder(t *testing.T) {
	root := gitDirs(t, "moved/api")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, config.Config{Repositories: []config.Repo{{Name: "api", Path: "/gone/api"}}}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)
	m := setup(t, Options{Source: gitx.Demo{}, ConfigPath: path, Repos: cfg.Repositories}, 120, 30)
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections())
	press(m, "f")
	m.input.SetValue(root)
	press(m, "enter")
	if !strings.Contains(m.flash, "moved/api") {
		t.Fatalf("f should find the folder with the old name: %q", m.flash)
	}
	press(m, "enter")
	saved, _ := config.Load(path)
	if r := saved.Repositories[0]; r.Path != filepath.Join(root, "moved", "api") || r.Status != config.StatusOK {
		t.Fatalf("the new path should be saved and usable: %+v", r)
	}
}

func TestRepositoryEditsNeedAReadableConfig(t *testing.T) {
	m := setup(t, Options{Source: gitx.Demo{}, ConfigPath: "/x/config.json", ConfigErr: errors.New("parse: bad")}, 120, 30)
	press(m, "g", "c", "a")
	if m.scan.open {
		t.Fatal("a configuration that failed to load must not be edited")
	}

	o := demoOpts()
	o.Demo, o.ConfigPath = false, t.TempDir() // a folder cannot be read as a file, so nothing is saved
	m = setup(t, o, 120, 30)
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections())
	press(m, "x", "y")
	if len(m.repos) != len(o.Repos) || !strings.Contains(m.flash, "Nothing saved") {
		t.Fatalf("an unreadable file should keep the list and say so: %d, %q", len(m.repos), m.flash)
	}

	// A file that loads but whose folder cannot be written reaches Save itself, and its error.
	dir, root := t.TempDir(), gitDirs(t, "a", "b")
	path := filepath.Join(dir, "config.json")
	if err := config.Save(path, config.Config{Repositories: []config.Repo{{Name: "a", Path: filepath.Join(root, "a")}, {Name: "b", Path: filepath.Join(root, "b")}}}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	m = setup(t, Options{Source: gitx.Demo{}, ConfigPath: path, Repos: cfg.Repositories}, 120, 30)
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections())
	press(m, "x", "y")
	if len(m.repos) != 2 || !strings.Contains(m.flash, "Could not save") {
		t.Fatalf("a failed save should keep the list and say so: %d, %q", len(m.repos), m.flash)
	}
}
