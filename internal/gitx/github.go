package gitx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shaungsimpson/git-tui/internal/termimg"
)

// CheckState is the combined result of a pull request's checks.
type CheckState int

const (
	ChecksNone CheckState = iota
	ChecksPassing
	ChecksPending
	ChecksFailing
)

// PR is an open pull request.
type PR struct {
	Repo    string
	Number  int
	Title   string
	Author  string
	Branch  string
	Base    string // the branch it merges into; another open PR's Branch when it is stacked
	Fork    bool   // its branch lives in another repository, so it never carries a stack
	URL     string
	Draft   bool
	Review  string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or empty when no review is required
	Checks  CheckState
	Updated time.Time
	Opened  time.Time // drives the row's age colour
	Body    string
	Files   []PRFile
	ForMe   bool // the viewer's review is requested, directly or through a team
}

// PRFile is one file a pull request changes.
type PRFile struct {
	Path                 string
	Additions, Deletions int
}

const (
	ghTimeout     = 20 * time.Second // network call, unlike the local git commands
	ghListTimeout = 60 * time.Second // a long list that gh fetches in several pages, one after another
)

// PullRequests lists open pull requests for the repository's origin remote using gh.
func (g Git) PullRequests(ctx context.Context, repo, dir string) ([]PR, error) {
	slug, err := g.Slug(ctx, dir)
	if err != nil {
		return nil, err
	}
	// GitHub search counts team requests too; a request is cleared once the viewer reviews.
	// Newest first, like the list below, so every listed PR that is requested falls inside the limit.
	mine := background(func() (string, error) {
		return command(ctx, dir, ghListTimeout, "gh", "pr", "list", "--repo", slug, "--state", "open", "--limit", "300",
			"--search", "review-requested:@me sort:created-desc", "--json", "number")
	})
	// shortcut: only the 300 most recently created open PRs per repository; raise it if a repository has more.
	out, err := command(ctx, dir, ghListTimeout, "gh", "pr", "list", "--repo", slug, "--state", "open", "--limit", "300",
		"--json", "number,title,author,headRefName,baseRefName,isCrossRepository,url,isDraft,reviewDecision,statusCheckRollup,updatedAt,createdAt,body,files")
	if err != nil {
		return nil, err
	}
	prs, err := ParsePRs(repo, out)
	if err != nil {
		return nil, err
	}
	// shortcut: a failed request search only hides the stars until the next refresh; show the error if that confuses.
	if mine, err := mine(); err == nil {
		_ = MarkRequested(prs, mine)
	}
	return prs, nil
}

// MarkRequested sets ForMe on the PRs listed in `gh pr list --json number` output.
func MarkRequested(prs []PR, out string) error {
	var raw []struct{ Number int }
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return fmt.Errorf("parse gh output: %w", err)
	}
	for _, r := range raw {
		for i := range prs {
			if prs[i].Number == r.Number {
				prs[i].ForMe = true
			}
		}
	}
	return nil
}

// PRDiff is a pull request's code changes as patch lines.
func (g Git) PRDiff(ctx context.Context, repo, dir string, number int) ([]string, error) {
	slug, err := g.Slug(ctx, dir)
	if err != nil {
		return nil, err
	}
	out, err := command(ctx, dir, ghTimeout, "gh", "pr", "diff", strconv.Itoa(number), "--repo", slug, "--color", "never")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

// GHToken is the login token of the GitHub CLI, for downloads gh cannot make itself.
func GHToken(ctx context.Context) (string, error) {
	out, err := command(ctx, "", 5*time.Second, "gh", "auth", "token")
	return strings.TrimSpace(out), err
}

// ErrNoOrigin is a repository with no origin remote, usually one not pushed to GitHub yet. It is not a failure.
var ErrNoOrigin = errors.New("no origin remote yet (not pushed to GitHub)")

// Slug is the GitHub "owner/name" of the repository's origin remote.
// Unfetched counts the commits on origin's branch on GitHub after base, the commit the last fetch saw there.
// Both go into a GitHub API path, so base must be a commit ID and branch a plain branch name.
func (g Git) Unfetched(ctx context.Context, dir, base, branch string) (int, error) {
	if !IsCommitID(base) || !plainBranch(branch) {
		return 0, fmt.Errorf("cannot compare %q with branch %q on GitHub", base, branch)
	}
	slug, err := g.Slug(ctx, dir)
	if err != nil {
		return 0, err
	}
	out, err := command(ctx, dir, ghTimeout, "gh", "api", "repos/"+slug+"/compare/"+base+"..."+branch, "--jq", ".ahead_by")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// plainBranch reports a branch name of letters, digits and . _ - / only, with no "..", safe in a URL path.
func plainBranch(b string) bool {
	return b != "" && !strings.Contains(b, "..") && strings.Trim(b, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-/") == ""
}

func (g Git) Slug(ctx context.Context, dir string) (string, error) {
	url, err := g.run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		if strings.Contains(err.Error(), "No such remote") { // LC_ALL=C keeps git's wording stable
			return "", ErrNoOrigin
		}
		return "", err
	}
	slug, ok := RepoSlug(url)
	if !ok {
		return "", fmt.Errorf("origin is not a GitHub remote: %s", strings.TrimSpace(url))
	}
	return slug, nil
}

// RepoSlug returns "owner/name" for a github.com remote URL in SSH, scp-like or HTTPS form. The host must be
// github.com itself, or over SSH a host alias such as "github.com-work".
func RepoSlug(remote string) (string, bool) {
	s := strings.TrimSpace(remote)
	var host, path string
	web := false
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		host, path, web = u.Hostname(), u.Path, u.Scheme != "ssh"
	} else if h, p, ok := strings.Cut(s, ":"); ok { // scp-like: git@github.com:owner/name
		host, path = h[strings.LastIndex(h, "@")+1:], p
	}
	host = strings.ToLower(host)
	if host != "github.com" && (web || !strings.HasPrefix(host, "github.com-")) {
		return "", false
	}
	s = strings.TrimSuffix(strings.Trim(path, "/"), ".git") // remotes may be written "github.com:/owner/name" or ".../name.git/"
	if parts := strings.Split(s, "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return s, true
}

// ParsePRs parses `gh pr list --json` output produced by PullRequests.
func ParsePRs(repo, out string) ([]PR, error) {
	var raw []struct {
		Number            int
		Title             string
		HeadRefName       string
		BaseRefName       string
		IsCrossRepository bool
		URL               string
		IsDraft           bool
		ReviewDecision    string
		UpdatedAt         time.Time
		CreatedAt         time.Time
		Body              string
		Files             []PRFile
		Author            struct{ Login string }
		StatusCheckRollup []struct{ Status, Conclusion, State string }
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	prs := make([]PR, 0, len(raw))
	for _, r := range raw {
		checks := ChecksNone
		for _, c := range r.StatusCheckRollup {
			result := c.Conclusion // check runs report a conclusion; commit statuses report a state
			if result == "" {
				result = c.State
			}
			switch result {
			case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
				checks = ChecksFailing
			case "", "PENDING", "EXPECTED", "STALE": // running, or (STALE) left unfinished for 14 days: not a pass
				checks = max(checks, ChecksPending)
			default:
				checks = max(checks, ChecksPassing)
			}
		}
		prs = append(prs, PR{
			Repo: repo, Number: r.Number, Title: r.Title, Author: r.Author.Login, Branch: r.HeadRefName,
			Base: r.BaseRefName, Fork: r.IsCrossRepository,
			URL: r.URL, Draft: r.IsDraft, Review: r.ReviewDecision, Checks: checks, Updated: r.UpdatedAt,
			Opened: r.CreatedAt, Body: r.Body, Files: r.Files,
		})
	}
	return prs, nil
}

// Stack is every PR stacked with p, depth-first from the bottom, with each one's depth; nil when p stands alone.
// prs are one repository's open PRs.
func Stack(prs []PR, p PR) ([]PR, []int) {
	prs = slices.SortedFunc(slices.Values(prs), func(a, b PR) int { return a.Number - b.Number }) // siblings oldest first
	byBranch := map[string]PR{}
	for _, q := range prs {
		if !q.Fork {
			byBranch[q.Branch] = q
		}
	}
	parent := func(q PR) (PR, bool) {
		b, ok := byBranch[q.Base]
		return b, ok && b.Number != q.Number
	}
	root, seen := p, map[int]bool{p.Number: true}
	for b, ok := parent(root); ok && !seen[b.Number]; b, ok = parent(root) { // seen stops a loop of bases
		root, seen[b.Number] = b, true
	}
	var out []PR
	var depth []int
	done := map[int]bool{}
	var walk func(q PR, d int)
	walk = func(q PR, d int) {
		done[q.Number] = true
		out, depth = append(out, q), append(depth, d)
		for _, c := range prs {
			if b, ok := parent(c); ok && b.Number == q.Number && !done[c.Number] {
				walk(c, d+1)
			}
		}
	}
	walk(root, 0)
	if len(out) < 2 {
		return nil, nil
	}
	return out, depth
}

// Image downloads a GitHub-hosted picture. The gh login token goes to github.com only, where attachments in
// private repositories need it.
func (Git) Image(ctx context.Context, raw string) (image.Image, error) {
	token := ""
	if termimg.SendsToken(raw) {
		token, _ = GHToken(ctx) // without it, attachments in private repositories are a 404
	}
	data, err := termimg.Download(ctx, raw, token)
	if err != nil {
		return nil, err
	}
	return termimg.Decode(data)
}
