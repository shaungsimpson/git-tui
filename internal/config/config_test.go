package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestLoadBadJSON(t *testing.T) {
	if _, err := Load(write(t, "{")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestLoadRejectsTrailingContent(t *testing.T) {
	for _, body := range []string{`{"repositories":[]}}`, `{"repositories":[]} {"repositories":[]}`} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("want an error for %q", body)
		}
	}
	if _, err := Load(write(t, "{\"repositories\":[]}\n\n")); err != nil {
		t.Fatalf("trailing whitespace is fine: %v", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	_, err := Load(write(t, `{"repos":[{"name":"x","path":"/tmp"}]}`))
	if err == nil || !strings.Contains(err.Error(), "repos") {
		t.Fatalf("want an error naming the unknown key, got %v", err)
	}
}

func TestLoadValidatesEachRepo(t *testing.T) {
	plain := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(write(t, `{"repositories":[
		{"name":"gone","path":"/definitely/not/here"},
		{"name":"plain","path":"`+plain+`"},
		{"path":"`+repo+`"},
		{"name":"empty","path":""}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{StatusMissing, StatusNotGit, StatusOK, StatusInvalid}
	for i, r := range cfg.Repositories {
		if r.Status != want[i] {
			t.Errorf("%s: status %v, want %v", r.Name, r.Status, want[i])
		}
	}
	if cfg.Repositories[2].Name != filepath.Base(repo) {
		t.Errorf("name should default to directory base, got %q", cfg.Repositories[2].Name)
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expand("~/Code/x"); got != filepath.Join(home, "Code/x") {
		t.Fatalf("got %q", got)
	}
	if got := expand("/abs"); got != "/abs" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveOrder(t *testing.T) {
	t.Setenv("GIT_TUI_CONFIG", "/env.json")
	if Resolve("/flag.json") != "/flag.json" || Resolve("") != "/env.json" {
		t.Fatal("flag must win over env")
	}
	t.Setenv("GIT_TUI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if Resolve("") != "/xdg/git-tui/config.json" {
		t.Fatal("xdg fallback")
	}
}

func TestStateRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "git-tui", "state.json")
	if s, err := LoadState(p); err != nil || s != (State{}) {
		t.Fatalf("a missing file is the zero state: %+v, %v", s, err)
	}
	want := State{UTC: true, Profile: "ascii"}
	if err := SaveState(p, want); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadState(p); err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(p)); len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestStatePath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if got := StatePath(); got != "/x/state/git-tui/state.json" {
		t.Fatal(got)
	}
}
