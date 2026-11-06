package gitx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
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

	Comments int       // conversation comments plus reviews with a summary
	LastBy   string    // who wrote the newest of them
	LastAt   time.Time // when
}

// Comment is a pull request's conversation comment, or a review's summary when State is set.
type Comment struct {
	Author string
	Bot    bool
	Body   string
	When   time.Time
	State  string // a review's APPROVED, CHANGES_REQUESTED, COMMENTED or DISMISSED; empty for a comment
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
	owner, name, _ := strings.Cut(slug, "/")
	counts := background(func() (string, error) {
		return command(ctx, dir, ghListTimeout, "gh", "api", "graphql", "--paginate", "-f", "owner="+owner, "-f", "name="+name,
			"-f", "query="+countQuery, "--jq", ".data.repository.pullRequests.nodes[]")
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
	// shortcut: a failed count only hides the comment marks until the next refresh, as for the stars.
	if counts, err := counts(); err == nil {
		_ = MarkComments(prs, counts)
	}
	return prs, nil
}

// countQuery reads each open PR's comment count and newest comment, and its reviews, which GitHub cannot count
// without the empty ones. shortcut: the newest 100 reviews per PR; page them if a PR collects more.
const countQuery = `query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){` +
	`pullRequests(states:OPEN,first:50,after:$endCursor){pageInfo{hasNextPage endCursor}nodes{number ` +
	`comments(last:1){totalCount nodes{author{login}createdAt}}reviews(last:100){nodes{author{login}body submittedAt}}}}}}`

// MarkComments sets each PR's comment count and newest commenter from the PR nodes, one JSON value each, that
// countQuery returns.
func MarkComments(prs []PR, out string) error {
	type note struct {
		Author                 struct{ Login string }
		Body                   string
		CreatedAt, SubmittedAt time.Time
	}
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var n struct {
			Number   int
			Comments struct {
				TotalCount int
				Nodes      []note
			}
			Reviews struct{ Nodes []note }
		}
		if err := dec.Decode(&n); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("parse gh output: %w", err)
		}
		i := slices.IndexFunc(prs, func(p PR) bool { return p.Number == n.Number })
		if i < 0 {
			continue
		}
		p := &prs[i]
		see := func(by string, at time.Time) {
			if at.After(p.LastAt) {
				p.LastBy, p.LastAt = by, at
			}
		}
		p.Comments = n.Comments.TotalCount
		for _, c := range n.Comments.Nodes {
			see(byName(c.Author.Login), c.CreatedAt)
		}
		for _, r := range n.Reviews.Nodes {
			if summary(r.Body, r.SubmittedAt) {
				p.Comments++
				see(byName(r.Author.Login), r.SubmittedAt)
			}
		}
	}
}

// summary reports a review that counts as a comment: submitted, not pending, and with a summary.
func summary(body string, at time.Time) bool { return strings.TrimSpace(body) != "" && !at.IsZero() }

// byName is an author's login, or "ghost", GitHub's name for a deleted account, which has none.
func byName(login string) string {
	if login == "" {
		return "ghost"
	}
	return login
}

// PRComments reads a pull request's conversation comments and review summaries, oldest first.
func (g Git) PRComments(ctx context.Context, repo, dir string, number int) ([]Comment, error) {
	slug, err := g.Slug(ctx, dir)
	if err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(slug, "/")
	out, err := command(ctx, dir, ghTimeout, "gh", "api", "graphql", "-f", "owner="+owner, "-f", "name="+name,
		"-F", "number="+strconv.Itoa(number), "-f", "query="+commentsQuery, "--jq", ".data.repository.pullRequest")
	if err != nil {
		return nil, err
	}
	return ParseComments(out)
}

// commentsQuery reads one PR's comments and reviews; __typename says which authors are bots.
// shortcut: the newest 100 of each; page them if a PR collects more.
const commentsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){` +
	`comments(last:100){nodes{author{login __typename}body createdAt}}` +
	`reviews(last:100){nodes{author{login __typename}body state submittedAt}}}}}`

// ParseComments parses the pull request commentsQuery returns: its comments, and its reviews with a summary.
func ParseComments(out string) ([]Comment, error) {
	type author struct {
		Login    string
		Typename string `json:"__typename"`
	}
	var raw struct {
		Comments struct {
			Nodes []struct {
				Author    author
				Body      string
				CreatedAt time.Time
			}
		}
		Reviews struct {
			Nodes []struct {
				Author      author
				Body, State string
				SubmittedAt time.Time
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	var cs []Comment
	for _, c := range raw.Comments.Nodes {
		cs = append(cs, Comment{Author: byName(c.Author.Login), Bot: c.Author.Typename == "Bot", Body: c.Body, When: c.CreatedAt})
	}
	for _, r := range raw.Reviews.Nodes {
		if summary(r.Body, r.SubmittedAt) {
			cs = append(cs, Comment{Author: byName(r.Author.Login), Bot: r.Author.Typename == "Bot", Body: r.Body, When: r.SubmittedAt, State: r.State})
		}
	}
	slices.SortStableFunc(cs, func(a, b Comment) int { return a.When.Compare(b.When) })
	return cs, nil
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
