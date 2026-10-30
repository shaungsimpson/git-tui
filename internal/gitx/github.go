package gitx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	URL     string
	Draft   bool
	Review  string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or empty when no review is required
	Checks  CheckState
	Updated time.Time
	Body    string
	Files   []PRFile
}

// PRFile is one file a pull request changes.
type PRFile struct {
	Path                 string
	Additions, Deletions int
}

const ghTimeout = 20 * time.Second // network call, unlike the local git commands

// PullRequests lists open pull requests for the repository's origin remote using gh.
func (g Git) PullRequests(ctx context.Context, repo, dir string) ([]PR, error) {
	slug, err := g.slug(ctx, dir)
	if err != nil {
		return nil, err
	}
	// shortcut: only the 100 most recently created open PRs per repository; page if a repository has more.
	out, err := command(ctx, dir, ghTimeout, "gh", "pr", "list", "--repo", slug, "--state", "open", "--limit", "100",
		"--json", "number,title,author,headRefName,url,isDraft,reviewDecision,statusCheckRollup,updatedAt,body,files")
	if err != nil {
		return nil, err
	}
	return ParsePRs(repo, out)
}

// PRDiff is a pull request's code changes as patch lines.
func (g Git) PRDiff(ctx context.Context, repo, dir string, number int) ([]string, error) {
	slug, err := g.slug(ctx, dir)
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

// slug is the GitHub "owner/name" of the repository's origin remote.
func (g Git) slug(ctx context.Context, dir string) (string, error) {
	url, err := g.run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	slug, ok := RepoSlug(url)
	if !ok {
		return "", fmt.Errorf("origin is not a GitHub remote: %s", strings.TrimSpace(url))
	}
	return slug, nil
}

// RepoSlug returns "owner/name" for a github.com remote URL in SSH, scp-like or HTTPS form.
func RepoSlug(remote string) (string, bool) {
	s := strings.TrimSpace(remote)
	i := strings.Index(s, "github.com")
	if i < 0 {
		return "", false
	}
	s = strings.TrimSuffix(s[i+len("github.com"):], ".git")
	if j := strings.IndexAny(s, ":/"); j >= 0 {
		s = s[j:] // skip an SSH host alias suffix such as "github.com-work:owner/name"
	}
	s = strings.Trim(strings.TrimLeft(s, ":/"), "/") // remotes may be written "github.com:/owner/name"
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
		URL               string
		IsDraft           bool
		ReviewDecision    string
		UpdatedAt         time.Time
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
			case "", "PENDING", "EXPECTED": // a running check run has no conclusion yet
				checks = max(checks, ChecksPending)
			default:
				checks = max(checks, ChecksPassing)
			}
		}
		prs = append(prs, PR{
			Repo: repo, Number: r.Number, Title: r.Title, Author: r.Author.Login, Branch: r.HeadRefName,
			URL: r.URL, Draft: r.IsDraft, Review: r.ReviewDecision, Checks: checks, Updated: r.UpdatedAt,
			Body: r.Body, Files: r.Files,
		})
	}
	return prs, nil
}
