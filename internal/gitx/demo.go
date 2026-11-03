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

// DemoGroups puts the demo repositories in groups, by name; the missing "infra" is in work too.
var DemoGroups = map[string]string{"backend": "work", "frontend": "work", "legacy-api": "work", "infra": "work", "personal-site": "personal"}

// DemoFolders are what adding repositories finds in demo mode, relative to the folder it looks in: one already
// configured, two new (acme-mobile not pushed to GitHub yet), one whose name clashes, and the missing "infra"
// repository moved to a new folder.
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
	"Requested by the team that needed it yesterday (https://example.com/tickets/YAK-42) so changes can be traced back to a person.",
}

var demoAuthors = []string{"Alice", "Bob", "Chuck", "Dave"}

func demoErr(repo string) error {
	if repo == "legacy-api" {
		return errors.New("demo: simulated unreadable repository")
	}
	return nil
}

// demoUnpushed reports the acme-mobile folder, which has no origin remote. It goes by folder, not name, so
// renaming the repository keeps it unpushed.
func demoUnpushed(dir string) bool { return dir[strings.LastIndex(dir, "/")+1:] == "acme-mobile" }

// demoGHErr is demoErr for GitHub reads, which also fail on a folder with no origin remote.
func demoGHErr(repo, dir string) error {
	if demoUnpushed(dir) {
		return ErrNoOrigin
	}
	return demoErr(repo)
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

func (Demo) Summary(_ context.Context, repo, dir string) (Summary, error) {
	if err := demoErr(repo); err != nil {
		return Summary{}, err
	}
	last := demoCommits(repo, 1, 0)[0]
	s := Summary{Branch: "main", Head: last.Hash, Upstream: "origin/main", HasAB: true, Last: &last, UpstreamID: last.Hash}
	// Fresh, a few days old and weeks old, so every shade of the fetch age shows.
	s.Fetched = demoBase.Add(-map[string]time.Duration{"frontend": 3 * 24 * time.Hour, "personal-site": 12 * 24 * time.Hour}[repo] - 2*time.Hour)
	if demoUnpushed(dir) {
		s.Upstream, s.HasAB, s.UpstreamID, s.Fetched = "", false, "", time.Time{}
	}
	if repo == "backend" {
		s.Behind = demoIncoming
	}
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

// demoIncoming is how many commits backend's last fetch saw that it has not pulled; GitHub has demoUnfetched more.
const demoIncoming, demoUnfetched = 3, 5

func (Demo) Incoming(_ context.Context, repo, dir string, n int) ([]Commit, error) {
	s, err := Demo{}.Summary(context.Background(), repo, dir)
	if err != nil {
		return nil, err
	}
	commits := demoCommits(repo, min(n, s.Behind), 50) // offset past the local history, so the hashes differ
	for i := range commits {
		commits[i].When = s.Fetched.Add(-time.Duration(i+1) * 90 * time.Minute) // pushed before the fetch saw them
	}
	return commits, nil
}

func (Demo) Unfetched(_ context.Context, dir, _, branch string) (int, error) {
	if demoUnpushed(dir) {
		return 0, ErrNoOrigin
	}
	if strings.HasSuffix(dir, "/backend") && branch == "main" {
		return demoUnfetched, nil
	}
	return 0, nil
}

func (Demo) PullRequests(_ context.Context, repo, dir string) ([]PR, error) {
	if err := demoGHErr(repo, dir); err != nil {
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
			Body: "## Summary\n\n" + demoBodies[(k%2)+1] + "\n\nNotes: https://example.com/docs/off-by-one. See the [runbook](https://example.com/runbook).\n\n![Screenshot of the change](https://github.com/user-attachments/assets/demo-" +
				fmt.Sprint(k) + ")\n\n- [x] Tests added",
			Files: []PRFile{{"app/maths.ts", 12, 3}, {"app/maths.test.ts", 40, 0}},
			ForMe: i == 1,
		})
		if i == 2 { // the demo viewer's PR, ready to merge
			p := &prs[len(prs)-1]
			p.Author, p.Review, p.Checks, p.Draft = "alice", "APPROVED", ChecksPassing, false
		}
	}
	if repo == "backend" { // a dependency bot's PR, whose description shows the markdown formatting
		prs = append(prs, PR{
			Repo: repo, Number: 499, Title: "chore(deps): update weekly dependencies", Author: "app/renovate",
			Branch: "renovate/weekly", URL: "https://github.com/example/backend/pull/499", Review: "REVIEW_REQUIRED",
			Checks: ChecksPassing, Updated: demoBase.Add(-200 * time.Hour), Body: demoRenovate,
			Files: []PRFile{{"package.json", 2, 2}, {"pnpm-lock.yaml", 24, 24}},
		})
	}
	return prs, nil
}

// demoRenovate is a dependency bot's PR description: an alert, a table with badges, release notes in a
// <details> section, a task list and code.
const demoRenovate = `> [!NOTE]
> Updates are grouped weekly. See the [merge confidence](https://docs.renovatebot.com/merge-confidence/) docs.

This PR contains the following updates:

| Package | Change | Age | Confidence | Type | Update |
|---|---|---|---|---|---|
| [@left-pad/is-even](https://github.com/left-pad/core) ([source](https://github.com/left-pad/core/tree/HEAD/packages/is-even)) | [` + "`3.5.43` → `3.5.44`" + `](https://renovatebot.com/diffs/npm/@left-pad%2fis-even/3.5.43/3.5.44) | ![age](https://developer.mend.io/api/mc/badges/age/npm/@left-pad%2fis-even/3.5.44) | ![confidence](https://developer.mend.io/api/mc/badges/confidence/npm/@left-pad%2fis-even/3.5.43/3.5.44) | devDependencies | patch |
| [acme/yak-shaver-3k](https://example.com/yak-shaver) | ` + "`d263541` → `afc8e39`" + ` | | | stage | digest |

---

### Release notes

<details>
<summary>left-pad/core (@&#8203;left-pad/is-even)</summary>

#### [` + "`v3.5.44`" + `](https://github.com/left-pad/core/releases/tag/v3.5.44)

- **is-even:** keep comments in ` + "`<script setup>`" + ` ([#13401](https://github.com/left-pad/core/issues/13401))
- **runtime-core:** avoid a double mount on fast re-renders

</details>
<details>
<summary>Commits</summary>
<ul>
<li><a href="https://github.com/left-pad/core/commit/4f1c2a9"><code>4f1c2a9</code></a> release: v3.5.44</li>
<li>See <a href="https://github.com/left-pad/core/compare/v3.5.43...v3.5.44">full diff</a> in compare view</li>
</ul>
</details>

### Configuration

- [x] Automerge is off
- [ ] Rebase when conflicted

` + "```" + `json
{ "schedule": ["before 6am on monday"] }
` + "```" + `

<!-- renovate-debug:eyJjcmVhdGVkSW5WZXIiOiI0MS4wLjAifQ== -->
`

func (Demo) Deployments(_ context.Context, repo, dir string) ([]Deployment, error) {
	if err := demoGHErr(repo, dir); err != nil {
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

func (Demo) DeployRuns(_ context.Context, repo, dir string) ([]Run, error) {
	if err := demoGHErr(repo, dir); err != nil {
		return nil, err
	}
	states := [][2]string{{"in_progress", ""}, {"completed", "success"}, {"completed", "failure"}, {"completed", "cancelled"}}
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

func (Demo) RunReasons(_ context.Context, dir string, run Run) ([]string, error) {
	if err := demoGHErr(run.Repo, dir); err != nil {
		return nil, err
	}
	if run.Conclusion == "cancelled" {
		return []string{"Canceling since a higher priority waiting request for deploy-stage exists"}, nil
	}
	return []string{"Process completed with exit code 1."}, nil
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

func (Demo) PRDiff(_ context.Context, repo, dir string, number int) ([]string, error) {
	if err := demoGHErr(repo, dir); err != nil {
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
	if demoUnpushed(dir) {
		return "", ErrNoOrigin
	}
	return "acme/" + dir[strings.LastIndex(dir, "/")+1:], nil
}

// DemoBuild is the release demo mode pretends to run, the newest tag this binary was built from. DemoLatest is the
// next minor release, so screenshots taken in a pull request before its tag offer the version about to ship.
var DemoBuild, DemoLatest = demoVersions(update.Current().Version)

// demoVersions reads Go's stamped version: vX.Y.Z on a tag, or a pseudo-version vX.Y.(Z+1)-0.… after tag vX.Y.Z.
// Builds with no tag behind them, such as tests, get fixed versions.
func demoVersions(v string) (update.Build, string) {
	b := update.Build{Kind: update.Release, Module: update.Module, Version: "v0.4.1", Sum: "h1:demo"}
	var major, minor, patch int
	if n, _ := fmt.Sscanf(v, "v%d.%d.%d", &major, &minor, &patch); n != 3 || strings.HasPrefix(v, "v0.0.0-") {
		return b, "v0.5.0"
	}
	if strings.Contains(v, "-") {
		patch = max(patch-1, 0) // a prerelease such as v0.11.0-rc.1 has no tag below it to step back to
	}
	b.Version = fmt.Sprintf("v%d.%d.%d", major, minor, patch)
	return b, fmt.Sprintf("v%d.%d.0", major, minor+1)
}

func (Demo) LatestRelease(context.Context) (update.Latest, error) {
	return update.Latest{Version: DemoLatest}, nil
}
