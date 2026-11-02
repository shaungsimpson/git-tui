// Package gitx reads repository state through the installed git and gh executables. Every call is read-only.
package gitx

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shaungsimpson/git-tui/internal/update"
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
	Fetched    time.Time // when the clone last fetched; zero when it has not since it was cloned
	UpstreamID string    // the commit the upstream branch was at in the last fetch; empty without one
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
	Incoming(ctx context.Context, repo, dir string, n int) ([]Commit, error) // on the upstream branch, not HEAD
	Unfetched(ctx context.Context, dir, base, branch string) (int, error)    // on GitHub's branch, after base
	PullRequests(ctx context.Context, repo, dir string) ([]PR, error)
	Deployments(ctx context.Context, repo, dir string) ([]Deployment, error)
	DeployRuns(ctx context.Context, repo, dir string) ([]Run, error)
	RunReasons(ctx context.Context, dir string, run Run) ([]string, error) // why a stopped run stopped
	Diff(ctx context.Context, repo, dir string) ([]string, error)
	PRDiff(ctx context.Context, repo, dir string, number int) ([]string, error)
	Viewer(ctx context.Context, dir string) (Viewer, error)
	Slug(ctx context.Context, dir string) (string, error)       // the GitHub "owner/name" of origin
	Image(ctx context.Context, url string) (image.Image, error) // a picture already checked with termimg.Allowed
	LatestRelease(ctx context.Context) (update.Latest, error)   // git-tui's newest stable release
}

// Git runs the git (and, for pull requests, gh) executables with a per-call timeout.
type Git struct{}

const gitTimeout = 5 * time.Second // local commands; gh calls use ghTimeout

// logFormat is used with -z: records end in NUL, which git never allows inside a commit message.
const logFormat = "--format=%H%x1f%an%x1f%aI%x1f%s%x1f%b"

func (g Git) run(ctx context.Context, dir string, args ...string) (string, error) {
	return command(ctx, dir, gitTimeout, "git", args...)
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
	cmd.WaitDelay = time.Second // a program git started (a clean filter) can hold the output open after git is killed
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
	args := []string{"rev-parse", "--git-common-dir"} // FETCH_HEAD lives there, shared by a clone's worktrees
	if s.HasAB {
		args = append(args, "@{upstream}")
	}
	// shortcut: a failed rev-parse only leaves the fetch time and upstream commit unknown; show it if that confuses.
	if out, err := g.run(ctx, dir, args...); err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		common := lines[0]
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		if fi, err := os.Stat(filepath.Join(common, "FETCH_HEAD")); err == nil {
			s.Fetched = fi.ModTime()
		}
		if len(lines) > 1 {
			s.UpstreamID = lines[1]
		}
	}
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
	// The three reads are independent, so they overlap: in a large worktree each can take seconds.
	stagedF := background(func() (string, error) { return g.run(ctx, dir, append([]string{"diff", "--cached"}, noTools...)...) })
	unstagedF := background(func() (string, error) { return g.run(ctx, dir, append([]string{"diff"}, noTools...)...) })
	untracked, err := g.run(ctx, dir, "ls-files", "--others", "--exclude-standard")
	staged, serr := stagedF()
	unstaged, uerr := unstagedF()
	if err := cmp.Or(serr, uerr, err); err != nil { // the first failure, in the order they are shown
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

// Incoming lists up to n commits on the upstream branch, as of the last fetch, that HEAD does not have.
func (g Git) Incoming(ctx context.Context, repo, dir string, n int) ([]Commit, error) {
	out, err := g.run(ctx, dir, "log", "-z", "-n", strconv.Itoa(n), logFormat, "HEAD..@{upstream}", "--")
	if err != nil {
		return nil, err
	}
	return ParseLog(repo, out), nil
}

// gitEnv drops inherited GIT_* variables (GIT_DIR would point every repository at one repo),
// then disables git and gh prompts and optional index writes. It also turns off core.fsmonitor, which a
// repository's own config could otherwise set to a program that git status runs.
// shortcut: other programs a repository's config names, such as clean filters (Git LFS is one), still run;
// configured repositories are trusted, as in your shell. Adding repositories also runs `git remote get-url` in
// folders not yet configured, which runs none of them. Revisit before running anything else in such a folder.
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
