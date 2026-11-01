package gitx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shaungsimpson/git-tui/internal/update"
)

// DemoRepos are the repository names the demo fixtures know about. "legacy-api" deliberately fails.
var DemoRepos = []string{"backend", "frontend", "personal-site", "legacy-api"}

// DemoFolders are what adding repositories finds in demo mode, relative to the folder it looks in: one already
// configured, two new, one whose name clashes, and the missing "infra" repository moved to a new folder.
var DemoFolders = []string{"backend", "acme-mobile", "acme-web", "client-x/backend", "platform/infra"}

// Demo returns deterministic fixture data. It is only used when the user passes -demo.
type Demo struct{}

var demoBase = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// DemoNow is the moment the fixtures describe; "this week" in demo mode counts back from it.
var DemoNow = demoBase

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
	base, head = strings.TrimPrefix(base, "refs/tags/"), strings.TrimPrefix(head, "refs/tags/")
	if !strings.HasPrefix(base, "v1.") { // an environment's commit compared with origin/HEAD
		return demoCommits(repo, len(repo)%4, 0), nil
	}
	var b, h int
	_, _ = fmt.Sscanf(base, "v1.%d.0", &b) // fixture tags always match; a mismatch leaves 0
	_, _ = fmt.Sscanf(head, "v1.%d.0", &h)
	if h <= b {
		return nil, nil
	}
	return demoCommits(repo, (h-b)*4, (9-h)*4), nil
}

func (Demo) PullRequests(_ context.Context, repo, _ string) ([]PR, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	// Offset from authors, so the demo viewer (alice) has a failing PR with changes requested.
	reviews := []string{"CHANGES_REQUESTED", "REVIEW_REQUIRED", "", "APPROVED"}
	checks := []CheckState{ChecksFailing, ChecksPending, ChecksNone, ChecksPassing}
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
			Body: "## Summary\n\n" + demoBodies[(k%2)+1] + "\n\n![Screenshot of the change](https://github.com/user-attachments/assets/demo-" +
				fmt.Sprint(k) + ")\n\n- [x] Tests added",
			Files: []PRFile{{"app/maths.ts", 12, 3}, {"app/maths.test.ts", 40, 0}},
			ForMe: i == 1,
		})
		if i == 2 { // the demo viewer's PR, ready to merge
			p := &prs[len(prs)-1]
			p.Author, p.Review, p.Checks, p.Draft = "alice", "APPROVED", ChecksPassing, false
		}
	}
	return prs, nil
}

func (Demo) Deployments(_ context.Context, repo, _ string) ([]Deployment, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	envs := []string{"prod", "stage", "dev-2"}
	var deps []Deployment
	for i, env := range envs {
		c := demoCommits(repo, 1, len(repo)%4+i*2)[0]
		d := Deployment{Repo: repo, Environment: env, Commit: c.Hash, When: c.When.Add(time.Hour), Ref: "main"}
		if env == "prod" {
			d.Ref = "v1.9.0"
		}
		if repo == "frontend" && env == "stage" {
			d.Latest, d.LatestWhen = "FAILURE", demoBase
		}
		deps = append(deps, d)
	}
	return deps, nil
}

func (Demo) DeployRuns(_ context.Context, repo, _ string) ([]Run, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	states := [][2]string{{"in_progress", ""}, {"completed", "success"}, {"completed", "failure"}, {"completed", "success"}}
	var runs []Run
	for i, st := range states {
		if repo != "backend" && i == 0 {
			continue // only one demo deploy is still running
		}
		c := demoCommits(repo, 1, i*3)[0]
		runs = append(runs, Run{
			Repo: repo, ID: int64(9000 + len(repo)*10 + i), Workflow: "Deploy " + []string{"stage", "stage", "prod", "stage"}[i],
			Title: c.Subject, Event: "push", Branch: "main", Commit: c.Hash, Status: st[0], Conclusion: st[1],
			URL:     fmt.Sprintf("https://github.com/example/%s/actions/runs/%d", repo, 9000+len(repo)*10+i),
			Started: c.When, Updated: c.When.Add(time.Duration(4+i) * time.Minute),
		})
	}
	return runs, nil
}

func (Demo) Diff(_ context.Context, repo, _ string) ([]string, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	return DiffSections(
		"diff --git a/app/maths.ts b/app/maths.ts\n--- a/app/maths.ts\n+++ b/app/maths.ts\n@@ -10,3 +10,3 @@\n-  return round(a) + round(b)\n+  return round(a + b)\n",
		"diff --git a/app/colours.css b/app/colours.css\n--- a/app/colours.css\n+++ b/app/colours.css\n@@ -1,2 +1,2 @@\n-.error { colour: #c00; }\n+.error { colour: #b3261e; }\n",
		"+ notes/todo.md\n+ scratch.txt",
	), nil
}

func (Demo) PRDiff(_ context.Context, repo, _ string, number int) ([]string, error) {
	if err := demoErr(repo); err != nil {
		return nil, err
	}
	return []string{
		"diff --git a/app/maths.ts b/app/maths.ts", "--- a/app/maths.ts", "+++ b/app/maths.ts",
		"@@ -10,3 +10,3 @@", fmt.Sprintf("-  // PR #%d", number), "-  return round(a) + round(b)", "+  return round(a + b)",
	}, nil
}

// Viewer is a fixture account; its picture is drawn by Demo.Image.
func (Demo) Viewer(context.Context, string) (Viewer, error) {
	return Viewer{
		Login: "alice", Name: "Alice", Bio: "Platform engineer. Has tried turning it off and on again.",
		Company: "Acme", Location: "Remote", Followers: 128, Following: 40, AvatarURL: DemoAvatarURL,
		GitName: "Alice", GitEmail: "alice@example.com",
	}, nil
}

func (Demo) Slug(_ context.Context, dir string) (string, error) {
	return "acme/" + dir[strings.LastIndex(dir, "/")+1:], nil
}

// DemoBuild is the release demo mode pretends to run, one behind Demo.LatestRelease, so the update badge shows.
var DemoBuild = update.Build{Kind: update.Release, Module: update.Module, Version: "v0.4.1", Sum: "h1:demo"}

func (Demo) LatestRelease(context.Context) (update.Latest, error) {
	return update.Latest{Version: "v0.5.0"}, nil
}
