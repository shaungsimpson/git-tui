package gitx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DemoRepos are the repository names the demo fixtures know about. "legacy-api" deliberately fails.
var DemoRepos = []string{"backend", "frontend", "personal-site", "legacy-api"}

// Demo returns deterministic fixture data. It is only used when the user passes -demo.
type Demo struct{}

var demoBase = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

var demoSubjects = []string{
	"Shave the yak before adding the feature",
	"Fix off-by-one error in the off-by-one fix",
	"Bump dependencies",
	"Refactor the refactor of the wizard",
	"Add logging to find out why logging is slow",
	"Improve error colour contrast in forms",
	"Invalidate the cache, attempt 7",
	"Remove v1 API, for real this time",
	"Add retry to the flaky test instead of fixing it",
	"Tidy release notes template",
}

var demoBodies = []string{
	"",
	"The previous fix was off by one, in the other direction.\n\n* Count from zero this time\n* Add a regression test so it stays fixed",
	"Requested by the team that needed it yesterday so changes can be traced back to a person.",
}

var demoAuthors = []string{"Alice", "Bob", "Chuck", "Dave"}

func demoErr(repo string) error {
	if repo == "legacy-api" {
		return errors.New("demo: simulated unreadable repository")
	}
	return nil
}

func demoCommits(repo string, n, offset int) []Commit {
	seed := len(repo)
	commits := make([]Commit, 0, n)
	for i := range n {
		k := i + offset
		commits = append(commits, Commit{
			Repo:    repo,
			Hash:    fmt.Sprintf("%07x%033x", (seed*7919+k*104729)%0xfffffff, k),
			Author:  demoAuthors[(seed+k)%len(demoAuthors)],
			When:    demoBase.Add(-time.Duration(k*seed+k*3) * time.Hour),
			Subject: demoSubjects[(seed+k)%len(demoSubjects)],
			Body:    demoBodies[k%len(demoBodies)],
		})
	}
	return commits
}

func (Demo) Summary(_ context.Context, repo, _ string) (Summary, error) {
	if err := demoErr(repo); err != nil {
		return Summary{}, err
	}
	last := demoCommits(repo, 1, 0)[0]
	s := Summary{Branch: "main", Head: last.Hash, Upstream: "origin/main", HasAB: true, Last: &last}
	if repo == "frontend" {
		s.Branch, s.Upstream, s.Ahead, s.Behind = "feature/dark-mode-v9", "origin/feature/dark-mode-v9", 2, 1
		s.Staged, s.Unstaged, s.Untracked = 1, 3, 2
	}
	return s, nil
}

func (Demo) Log(_ context.Context, repo, _ string, n int) ([]Commit, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	return demoCommits(repo, min(n, 25), 0), nil
}

func (Demo) Tags(_ context.Context, repo, _ string) ([]Tag, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	var tags []Tag
	for i := range 6 {
		c := demoCommits(repo, 1, i*4)[0]
		tags = append(tags, Tag{Name: fmt.Sprintf("v1.%d.0", 9-i), Commit: c.Hash, When: c.When, Subject: "Release v1." + fmt.Sprint(9-i)})
	}
	return tags, nil
}

func (Demo) Compare(_ context.Context, repo, _, base, head string) ([]Commit, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	var b, h int
	fmt.Sscanf(base, "v1.%d.0", &b)
	fmt.Sscanf(head, "v1.%d.0", &h)
	if h <= b {
		return nil, nil
	}
	return demoCommits(repo, (h-b)*4, (9-h)*4), nil
}

func (Demo) PullRequests(_ context.Context, repo, _ string) ([]PR, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	reviews := []string{"APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED", ""}
	checks := []CheckState{ChecksPassing, ChecksFailing, ChecksPending, ChecksNone}
	authors := []string{"alice", "bob", "chuck", "dave"}
	seed := len(repo)
	var prs []PR
	for i := range seed%4 + 2 {
		k := seed + i
		prs = append(prs, PR{
			Repo:    repo,
			Number:  400 + seed*10 + i,
			Title:   demoSubjects[k%len(demoSubjects)],
			Author:  authors[k%len(authors)],
			Branch:  fmt.Sprintf("feature/demo-%d", i+1),
			URL:     fmt.Sprintf("https://github.com/example/%s/pull/%d", repo, 400+seed*10+i),
			Draft:   i == 3,
			Review:  reviews[k%len(reviews)],
			Checks:  checks[k%len(checks)],
			Updated: demoBase.Add(-time.Duration(k*5) * time.Hour),
		})
	}
	return prs, nil
}
