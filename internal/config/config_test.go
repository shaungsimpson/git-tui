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
	cfg, _ := Load(write(t, `{"repositories":[{"name":"a","path":"~/a"}]} junk`))
	if len(cfg.Repositories) != 0 {
		t.Fatalf("a file that fails to parse should give no repositories, got %+v", cfg.Repositories)
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

func TestLoadTidiesPaths(t *testing.T) {
	cfg, err := Load(write(t, `{"repositories":[{"name":"a","path":"/srv/x/../a/"}]}`))
	if err != nil || cfg.Repositories[0].Path != "/srv/a" {
		t.Fatalf("a path should be tidied, so the same folder always compares equal: %+v, %v", cfg, err)
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

func TestSaveRoundTripsAndKeepsBackup(t *testing.T) {
	home, _ := os.UserHomeDir()
	p := write(t, `{"repositories":[{"name":"old","path":"/old"}]}`)
	want := []Repo{{Name: "api", Path: filepath.Join(home, "Code", "api")}, {Name: "web", Path: "/srv/web"}}
	if err := Save(p, Config{Repositories: want}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), `"~/Code/api"`) {
		t.Fatalf("a path under home should be written with ~:\n%s", data)
	}
	cfg, err := Load(p)
	if err != nil || len(cfg.Repositories) != 2 || cfg.Repositories[0].Path != want[0].Path || cfg.Repositories[1].Name != "web" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	if bak, _ := os.ReadFile(p + ".bak"); !strings.Contains(string(bak), `"old"`) {
		t.Fatalf("the previous file should be kept as .bak, got %q", bak)
	}
}

func TestSaveRefusesBadLists(t *testing.T) {
	p := write(t, `{"repositories":[]}`)
	for _, repos := range [][]Repo{
		{{Name: "a", Path: "/a"}, {Name: "a", Path: "/b"}},
		{{Name: "", Path: "/a"}},
		{{Name: "a", Path: " "}},
	} {
		if err := Save(p, Config{Repositories: repos}); err == nil {
			t.Errorf("want an error for %+v", repos)
		}
	}
	if data, _ := os.ReadFile(p); string(data) != `{"repositories":[]}` {
		t.Fatalf("a refused save must leave the file alone, got %q", data)
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Fatal("a refused save must not write a backup")
	}
}

func TestSaveCreatesTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new", "config.json")
	if err := Save(p, Config{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), `"repositories": []`) {
		t.Fatalf("an empty list should be written as []: %q", data)
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a/.git", "a/inner/.git", "group/b/.git", "x/y/z/deep/.git", ".hidden/c/.git", "node_modules/d/.git", "plain"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "group", "wt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(t.Context(), root, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "group", "b")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v (not inside a repository, hidden or node_modules folders, nor deeper than 3)", got, want)
	}
	link := filepath.Join(t.TempDir(), "Code")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if got, _ := Scan(t.Context(), link, 3); len(got) != 2 || got[0] != filepath.Join(link, "a") {
		t.Fatalf("a linked folder should be searched and named as typed, got %v", got)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(filepath.Join(elsewhere, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if got, _ := Scan(t.Context(), root, 3); len(got) != 3 || got[2] != filepath.Join(root, "linked") {
		t.Fatalf("a repository linked into the folder should be found, got %v", got)
	}
	t.Chdir(filepath.Dir(root))
	if got, _ := Scan(t.Context(), filepath.Base(root), 3); len(got) == 0 || got[0] != filepath.Join(root, "a") {
		t.Fatalf("a relative folder should give absolute paths, got %v", got)
	}
	if _, err := Scan(t.Context(), filepath.Join(root, "missing"), 3); err == nil {
		t.Fatal("a missing root should be an error")
	}
}

func TestTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{filepath.Join(home, "Code"): "~/Code", home: "~", "/srv/x": "/srv/x", home + "x": home + "x"} {
		if got := Tilde(in); got != want {
			t.Errorf("Tilde(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveWritesThroughALink(t *testing.T) {
	real := write(t, `{"repositories":[]}`)
	link := filepath.Join(t.TempDir(), "config.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Save(link, Config{Repositories: []Repo{{Name: "x", Path: "/x"}}}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link should stay a link")
	}
	if data, _ := os.ReadFile(real); !strings.Contains(string(data), `"x"`) {
		t.Fatalf("the linked file should hold the change, got %s", data)
	}
	if _, err := os.Stat(real + ".bak"); err != nil {
		t.Fatal("the backup belongs beside the linked file")
	}
}

func TestSaveWritesThroughALinkToAMissingFile(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(filepath.Join("dotfiles", "config.json"), link); err != nil { // relative, and not there yet
		t.Fatal(err)
	}
	if err := Save(link, Config{Repositories: []Repo{{Name: "x", Path: "/x"}}}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link should stay a link")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "dotfiles", "config.json")); !strings.Contains(string(data), `"x"`) {
		t.Fatalf("the link's target should be created with the change, got %q", data)
	}
}

func TestSaveFollowsAChainOfLinks(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	if err := os.Symlink(other, filepath.Join(dir, "dirlink")); err != nil {
		t.Fatal(err)
	}
	// config.json -> next -> dirlink/../target: the ".." goes up from other, where dirlink leads, not from dir.
	if err := os.Symlink("next", filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dirlink/../target", filepath.Join(dir, "next")); err != nil { // not filepath.Join, which drops ".."
		t.Fatal(err)
	}
	if err := Save(filepath.Join(dir, "config.json"), Config{Repositories: []Repo{{Name: "x", Path: "/x"}}}); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"config.json", "next"} {
		if fi, _ := os.Lstat(filepath.Join(dir, l)); fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s should stay a link", l)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(filepath.Dir(other), "target")); !strings.Contains(string(data), `"x"`) {
		t.Fatalf("the end of the chain should hold the change, got %q", data)
	}
	if err := os.Symlink("loop", filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(dir, "loop"), Config{}); err == nil {
		t.Fatal("a link loop should be refused, not replaced")
	}
}
