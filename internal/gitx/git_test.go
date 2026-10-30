package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	out := strings.Join([]string{
		"# branch.oid abc123",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +2 -3",
		"1 M. N... 100644 100644 100644 a b file1",
		"1 .M N... 100644 100644 100644 a b file2",
		"1 MM N... 100644 100644 100644 a b file3",
		"2 R. N... 100644 100644 100644 a b R100 new\told",
		"u UU N... 1 2 3 4 a b c conflict",
		"? untracked",
	}, "\n")
	s := ParseStatus(out)
	if s.Head != "abc123" || s.Branch != "main" || s.Upstream != "origin/main" {
		t.Fatalf("header: %+v", s)
	}
	if !s.HasAB || s.Ahead != 2 || s.Behind != 3 {
		t.Fatalf("ab: %+v", s)
	}
	if s.Staged != 3 || s.Unstaged != 2 || s.Conflicted != 1 || s.Untracked != 1 {
		t.Fatalf("counts: %+v", s)
	}
}

func TestParseStatusDetachedInitial(t *testing.T) {
	s := ParseStatus("# branch.oid (initial)\n# branch.head (detached)\n")
	if s.Head != "" || s.Branch != "" || s.HasAB || !s.Clean() {
		t.Fatalf("%+v", s)
	}
}

func TestParseLogSkipsMalformed(t *testing.T) {
	out := "h1\x1fAnn\x1f2026-01-02T03:04:05+10:00\x1fFirst\x1fBody line\n\n* item\x1fwith separator\n\x00" +
		"broken\x00" +
		"h2\x1fBo\x1f2026-01-01T00:00:00Z\x1fSecond\x1f\x00"
	c := ParseLog("r", out)
	if len(c) != 2 || c[0].Subject != "First" || c[1].Hash != "h2" || c[0].When.IsZero() {
		t.Fatalf("%+v", c)
	}
	if c[0].Body != "Body line\n\n* item\x1fwith separator" || c[1].Body != "" {
		t.Fatalf("bodies %q / %q", c[0].Body, c[1].Body)
	}
}

func TestParseTagsPeelsAnnotated(t *testing.T) {
	out := "v2\x1ftagobj\x1fcommit2\x1f2026-01-02T00:00:00Z\x1fRelease 2\nv1\x1fcommit1\x1f\x1f2026-01-01T00:00:00Z\x1f\n"
	tags := ParseTags(out)
	if len(tags) != 2 || tags[0].Commit != "commit2" || tags[1].Commit != "commit1" {
		t.Fatalf("%+v", tags)
	}
}

// gitRepo creates a repository with commits a, b, c and tags v1 (at a, annotated) and v2 (at c).
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// gitEnv strips an inherited GIT_DIR so setup can never write into another repository.
		cmd.Env = append(gitEnv(cmd.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "a")
	run("tag", "-a", "v1", "-m", "Release 1")
	run("commit", "-q", "--allow-empty", "-m", "b")
	run("commit", "-q", "--allow-empty", "-m", "c", "-m", "Why c exists.\n\n* detail one")
	run("tag", "v2")
	return dir
}

func TestGitAgainstTempRepo(t *testing.T) {
	dir := gitRepo(t)
	g := Git{}
	ctx := context.Background()

	s, err := g.Summary(ctx, "tmp", dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch != "main" || s.Last == nil || s.Last.Subject != "c" || !s.Clean() || s.HasAB {
		t.Fatalf("summary %+v", s)
	}

	tags, err := g.Tags(ctx, "tmp", dir)
	if err != nil || len(tags) != 2 {
		t.Fatalf("tags %v %+v", err, tags)
	}

	commits, err := g.Compare(ctx, "tmp", dir, "v1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Subject != "c" || commits[1].Subject != "b" {
		t.Fatalf("compare %+v", commits)
	}
	if commits[0].Body != "Why c exists.\n\n* detail one" || commits[1].Body != "" {
		t.Fatalf("bodies %q / %q", commits[0].Body, commits[1].Body)
	}

	if _, err := g.Log(ctx, "tmp", t.TempDir(), 5); err == nil {
		t.Fatal("expected error outside a repository")
	}
}

func TestLogEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	cmd.Env = gitEnv(cmd.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	commits, err := Git{}.Log(context.Background(), "new", dir, 5)
	if err != nil || len(commits) != 0 {
		t.Fatalf("a repository with no commits should give no history and no error: %v %v", commits, err)
	}
}

func TestDiffSections(t *testing.T) {
	dir := gitRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(gitEnv(cmd.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write("staged.txt", "new\n")
	git("add", "staged.txt")
	write("staged.txt", "new\nand edited\n") // also an unstaged change on top
	write("loose.txt", "untracked\n")

	lines, err := Git{}.Diff(context.Background(), "tmp", dir)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	for _, want := range []string{"## Staged changes", "+new", "## Unstaged changes", "+and edited", "## Untracked files", "+ loose.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "## Staged") > strings.Index(out, "## Unstaged") {
		t.Error("staged should come before unstaged")
	}
	if got := DiffSections("", "", ""); len(got) != 0 {
		t.Fatalf("a clean worktree has no sections: %q", got)
	}
}

func TestGitEnvDropsInheritedGitVars(t *testing.T) {
	env := gitEnv([]string{"PATH=/bin", "GIT_DIR=/elsewhere/.git", "GIT_WORK_TREE=/elsewhere", "HOME=/h"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			t.Fatalf("inherited %s", kv)
		}
	}
	if !slices.Contains(env, "PATH=/bin") || !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("env %v", env)
	}
}

func TestDemoIsDeterministic(t *testing.T) {
	a, _ := Demo{}.Log(context.Background(), "backend", "", 5)
	b, _ := Demo{}.Log(context.Background(), "backend", "", 5)
	if len(a) != 5 || a[0] != b[0] {
		t.Fatal("demo log must be deterministic")
	}
	if _, err := (Demo{}).Summary(context.Background(), "legacy-api", ""); err == nil {
		t.Fatal("legacy-api fixture should fail")
	}
	c, _ := Demo{}.Compare(context.Background(), "backend", "", "v1.7.0", "v1.9.0")
	if len(c) != 8 {
		t.Fatalf("compare want 8, got %d", len(c))
	}
}
