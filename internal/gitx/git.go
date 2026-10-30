// Package gitx reads repository state through the installed git and gh executables. Every call is read-only.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Commit is one log entry.
type Commit struct {
	Repo    string
	Hash    string
	Author  string
	When    time.Time
	Subject string
	Body    string // message after the subject line, empty for one-line messages
}

// Tag is a tag pointing (possibly via an annotated tag object) at a commit.
type Tag struct {
	Name    string
	Commit  string
	When    time.Time
	Subject string
}

// Summary is the worktree and tracking state of a repository.
type Summary struct {
	Branch     string // empty when detached
	Head       string
	Upstream   string
	Ahead      int
	Behind     int
	HasAB      bool // ahead/behind is only known when an upstream exists
	Staged     int
	Unstaged   int
	Untracked  int
	Conflicted int
	Last       *Commit
}

// Clean reports whether the worktree has no changes.
func (s Summary) Clean() bool {
	return s.Staged+s.Unstaged+s.Untracked+s.Conflicted == 0
}

// Source supplies repository data. Git reads real repositories; Demo returns fixtures.
type Source interface {
	Summary(ctx context.Context, repo, dir string) (Summary, error)
	Log(ctx context.Context, repo, dir string, n int) ([]Commit, error)
	Tags(ctx context.Context, repo, dir string) ([]Tag, error)
	Compare(ctx context.Context, repo, dir, base, head string) ([]Commit, error)
	PullRequests(ctx context.Context, repo, dir string) ([]PR, error)
	Deployments(ctx context.Context, repo, dir string) ([]Deployment, error)
	DeployRuns(ctx context.Context, repo, dir string) ([]Run, error)
	Diff(ctx context.Context, repo, dir string) ([]string, error)
	PRDiff(ctx context.Context, repo, dir string, number int) ([]string, error)
	Viewer(ctx context.Context, dir string) (Viewer, error)
	Image(ctx context.Context, url string) (image.Image, error) // a picture already checked with termimg.Allowed
}

// Git runs the git (and, for pull requests, gh) executables with a per-call timeout.
type Git struct {
	Timeout time.Duration
}

// logFormat is used with -z: records end in NUL, which git never allows inside a commit message.
const logFormat = "--format=%H%x1f%an%x1f%aI%x1f%s%x1f%b"

func (g Git) run(ctx context.Context, dir string, args ...string) (string, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return command(ctx, dir, timeout, "git", args...)
}

// background starts f now and returns a function that waits for its result, so independent calls overlap.
func background(f func() (string, error)) func() (string, error) {
	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1) // buffered: the goroutine never blocks if the caller returns early
	go func() {
		out, err := f()
		ch <- result{out, err}
	}()
	return func() (string, error) {
		r := <-ch
		return r.out, r.err
	}
}

// command runs a read-only tool in dir and returns stdout, or stderr as the error.
func command(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// No credential prompts, no index refresh writes, stable output.
	cmd.Env = gitEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s %s timed out after %s", name, args[0], timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", name, args[0], msg)
	}
	return stdout.String(), nil
}

func (g Git) Summary(ctx context.Context, repo, dir string) (Summary, error) {
	out, err := g.run(ctx, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	if err != nil {
		return Summary{}, err
	}
	s := ParseStatus(out)
	if s.Head == "" {
		return s, nil // unborn branch: nothing to log yet
	}
	commits, err := g.Log(ctx, repo, dir, 1)
	if err != nil {
		return s, err
	}
	if len(commits) > 0 {
		s.Last = &commits[0]
	}
	return s, nil
}

func (g Git) Log(ctx context.Context, repo, dir string, n int) ([]Commit, error) {
	out, err := g.run(ctx, dir, "log", "-z", "-n", strconv.Itoa(n), logFormat)
	if err != nil && strings.Contains(err.Error(), "does not have any commits yet") {
		return nil, nil // a new repository: no history is not a failure
	}
	if err != nil {
		return nil, err
	}
	return ParseLog(repo, out), nil
}

// Diff is the uncommitted changes as lines: a "## " heading, then the patch, for staged changes,
// unstaged changes and untracked files. External diff and textconv programs are never run.
func (g Git) Diff(ctx context.Context, repo, dir string) ([]string, error) {
	noTools := []string{"--no-color", "--no-ext-diff", "--no-textconv"}
	staged, err := g.run(ctx, dir, append([]string{"diff", "--cached"}, noTools...)...)
	if err != nil {
		return nil, err
	}
	unstaged, err := g.run(ctx, dir, append([]string{"diff"}, noTools...)...)
	if err != nil {
		return nil, err
	}
	untracked, err := g.run(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(untracked), "\n") {
		if f != "" {
			files = append(files, "+ "+f)
		}
	}
	return DiffSections(staged, unstaged, strings.Join(files, "\n")), nil
}

// DiffSections joins the three parts under headings, leaving out empty ones.
func DiffSections(staged, unstaged, untracked string) []string {
	var lines []string
	for _, s := range []struct{ title, body string }{
		{"Staged changes", staged}, {"Unstaged changes", unstaged}, {"Untracked files", untracked},
	} {
		if body := strings.TrimRight(s.body, "\n"); body != "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, "## "+s.title)
			lines = append(lines, strings.Split(body, "\n")...)
		}
	}
	return lines
}

func (g Git) Tags(ctx context.Context, repo, dir string) ([]Tag, error) {
	out, err := g.run(ctx, dir, "for-each-ref", "refs/tags", "--sort=-creatordate",
		"--format=%(refname:short)%1f%(objectname)%1f%(*objectname)%1f%(creatordate:iso-strict)%1f%(contents:subject)")
	if err != nil {
		return nil, err
	}
	return ParseTags(out), nil
}

// Compare lists commits reachable from head but not from base. Both are git revisions such as
// "refs/tags/v1" or a commit ID; --end-of-options stops either being read as an option.
func (g Git) Compare(ctx context.Context, repo, dir, base, head string) ([]Commit, error) {
	out, err := g.run(ctx, dir, "log", "-z", logFormat, "--end-of-options", base+".."+head, "--")
	if err != nil {
		return nil, err
	}
	return ParseLog(repo, out), nil
}

// gitEnv drops inherited GIT_* variables (GIT_DIR would point every repository at one repo),
// then disables git and gh prompts and optional index writes. It also turns off core.fsmonitor, which a
// repository's own config could otherwise set to a program that git status runs.
// shortcut: other programs a repository's config names, such as clean filters (Git LFS is one), still run;
// configured repositories are trusted, as in your shell. Revisit if the app ever reads repositories it did not clone.
func gitEnv(env []string) []string {
	out := make([]string, 0, len(env)+7)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GIT_") {
			out = append(out, kv)
		}
	}
	return append(out, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GH_PROMPT_DISABLED=1", "LC_ALL=C",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.fsmonitor", "GIT_CONFIG_VALUE_0=false")
}

// ParseLog parses output produced with logFormat.
func ParseLog(repo, out string) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(out, "\x00") {
		f := strings.SplitN(rec, "\x1f", 5) // the body is last, so anything inside it stays in the body
		if len(f) != 5 {
			continue
		}
		when, _ := time.Parse(time.RFC3339, f[2])
		commits = append(commits, Commit{Repo: repo, Hash: f[0], Author: f[1], When: when, Subject: f[3], Body: strings.TrimSpace(f[4])})
	}
	return commits
}

// ParseTags parses the for-each-ref output used by Git.Tags.
func ParseTags(out string) []Tag {
	var tags []Tag
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 5 {
			continue
		}
		commit := f[1]
		if f[2] != "" {
			commit = f[2] // annotated: peel to the tagged commit
		}
		when, _ := time.Parse(time.RFC3339, f[3])
		tags = append(tags, Tag{Name: f[0], Commit: commit, When: when, Subject: f[4]})
	}
	return tags
}

// ParseStatus parses `git status --porcelain=v2 --branch`.
func ParseStatus(out string) Summary {
	var s Summary
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			if oid := strings.TrimPrefix(line, "# branch.oid "); oid != "(initial)" {
				s.Head = oid
			}
		case strings.HasPrefix(line, "# branch.head "):
			if b := strings.TrimPrefix(line, "# branch.head "); b != "(detached)" {
				s.Branch = b
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			s.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			var a, b int
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &a, &b); err == nil {
				s.Ahead, s.Behind, s.HasAB = a, b, true
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			if len(line) >= 4 {
				if line[2] != '.' {
					s.Staged++
				}
				if line[3] != '.' {
					s.Unstaged++
				}
			}
		case strings.HasPrefix(line, "u "):
			s.Conflicted++
		case strings.HasPrefix(line, "? "):
			s.Untracked++
		}
	}
	return s
}
