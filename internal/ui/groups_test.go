package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/shaungsimpson/git-tui/internal/config"
)

func TestGroupsCycleAndHideRepositories(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	if got := strings.Join(m.groups(), ","); got != "personal,work" {
		t.Fatalf("groups should be A to Z, got %s", got)
	}
	m.repo = 1 // backend, in work
	press(m, "]")
	if m.group != "personal" || m.repo != 0 {
		t.Fatalf("] should show personal and leave the hidden backend: group %q, repo %d", m.group, m.repo)
	}
	for _, w := range m.wanted() {
		if w.repo >= 0 && m.repos[w.repo].cfg.Group != "personal" {
			t.Fatalf("a hidden repository should load nothing, but %s wants %v", m.repos[w.repo].cfg.Name, w.kind)
		}
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, " personal 1/5") || strings.Contains(out, "frontend") {
		t.Fatalf("the sidebar should name the group and list only its repositories:\n%s", out)
	}
	src := &countingSource{}
	m.opts.Source = src
	press(m, "]")
	if m.group != "work" || src.calls.Load() != 3 { // the missing infra has nothing to read
		t.Fatalf("showing a group should reload its git logs at once: group %q, %d logs", m.group, src.calls.Load())
	}
	press(m, "]")
	if m.group != "" {
		t.Fatalf("] should wrap round to all, got %q", m.group)
	}
	press(m, "[")
	if m.group != "work" {
		t.Fatalf("[ should go back to the last group, got %q", m.group)
	}
	press(m, "g", "e") // an undeployed-commits list for a work repository, opened while all are selected
	rows := m.envList()
	m.cursor[viewEnvs] = slices.IndexFunc(rows, func(e envRow) bool { return m.repos[e.repo].cfg.Group == "work" })
	press(m, "enter", "]", "]")
	if m.group != "personal" || m.cmp.open {
		t.Fatalf("showing a group should close a commit list of a hidden repository: group %q", m.group)
	}
	press(m, "]", "g", "o") // back to work
	m.focus = paneSidebar
	if press(m, "j", "j", "j"); m.repo != 4 { // backend, frontend, then legacy-api past the hidden personal-site
		t.Fatalf("j in the sidebar should skip hidden repositories: repo %d", m.repo)
	}
}

// On a detail page ] would change the list under the page, so it waits until the page is closed.
func TestGroupKeysWaitForTheDetailPage(t *testing.T) {
	m := setup(t, demoOpts(), 140, 40)
	press(m, "g", "p", "v")
	open := m.prList()[m.cursor[viewPRs]]
	press(m, "]", "]")
	if got := m.prList()[m.cursor[viewPRs]]; m.group != "" || !m.inspect || got.Repo != open.Repo || got.Number != open.Number {
		t.Fatalf("] on a detail page moved it from %s #%d to %s #%d (group %q)", open.Repo, open.Number, got.Repo, got.Number, m.group)
	}
}

func TestSetGroupInConnections(t *testing.T) {
	m := setup(t, demoOpts(), 120, 30)
	press(m, "]") // personal, whose only repository is personal-site
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections()) // backend, hidden by the group
	if c := m.connections()[m.cursor[viewConnections]]; c.state != "hidden by the group" {
		t.Fatalf("Connections should list a hidden repository as hidden, got %q", c.state)
	}
	press(m, "e")
	m.input.SetValue("PER")
	press(m, "tab", "enter")
	if g := m.repos[0].cfg.Group; g != "personal" {
		t.Fatalf("tab should complete an existing group whatever its case, got %q", g)
	}
	m.cursor[viewConnections] += 2 // personal-site
	press(m, "e")
	m.input.SetValue(" side ")
	press(m, "enter")
	if g := m.repos[2].cfg.Group; g != "side" || m.group != "personal" {
		t.Fatalf("a typed group should be new and trimmed: %q, showing %q", g, m.group)
	}
	m.cursor[viewConnections] -= 2
	press(m, "e")
	m.input.SetValue("")
	press(m, "enter")
	if m.repos[0].cfg.Group != "" || m.group != "" {
		t.Fatalf("emptying the last repository of the group shown should clear it and show all: %q", m.group)
	}
}

func TestGroupSavesAndFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	o := Options{Source: demoOpts().Source, ConfigPath: path, StatePath: filepath.Join(t.TempDir(), "state.json"), Group: "gone",
		Repos: []config.Repo{{Name: "api", Path: "/demo/api", Group: "work", Status: config.StatusOK}}}
	if err := config.Save(path, config.Config{Repositories: o.Repos}); err != nil {
		t.Fatal(err)
	}
	m := setup(t, o, 120, 30)
	if m.group != "" {
		t.Fatalf("a saved group that no longer exists should show all, got %q", m.group)
	}
	press(m, "]")
	if st, _ := config.LoadState(o.StatePath); st.Group != "work" {
		t.Fatalf("the group shown should be remembered, got %q", st.Group)
	}
	press(m, "g", "c")
	m.cursor[viewConnections] = repoRows(m.connections())
	press(m, "e")
	m.input.SetValue("home")
	press(m, "enter")
	if cfg, err := config.Load(path); err != nil || cfg.Repositories[0].Group != "home" {
		t.Fatalf("the group should be saved to the configuration: %+v, %v", cfg, err)
	}
}
