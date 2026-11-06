package gitx

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRepoSlug(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/frontend.git\n":         "acme/frontend",
		"git@github.com:/acme/backend.git":           "acme/backend", // real remote form
		"https://github.com/acme/personal-site":      "acme/personal-site",
		"ssh://git@github.com/owner/repo.git":        "owner/repo",
		"https://github.com/owner/repo.git/":         "owner/repo",   // git accepts the trailing slash
		"git@github.com-work:acme/backend.git":       "acme/backend", // SSH host alias
		"ssh://git@github.com-work/owner/repo.git":   "owner/repo",
		"git@gitlab.com:owner/repo.git":              "",
		"https://github.com/owner":                   "",
		"https://github.com.evil.example/owner/repo": "", // github.com must be the whole host
		"https://notgithub.com/owner/repo":           "",
		"https://evil.example/github.com/owner/repo": "",
		"https://github.com-work/owner/repo":         "", // host aliases are an SSH feature
		"git@evil.example:github.com/repo":           "",
	}
	for in, want := range cases {
		got, ok := RepoSlug(in)
		if got != want || ok != (want != "") {
			t.Errorf("RepoSlug(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestParsePRsCheckRollup(t *testing.T) {
	out := `[
	 {"number":1,"title":"Pass","author":{"login":"a"},"baseRefName":"main","isCrossRepository":true,"isDraft":false,"reviewDecision":"APPROVED","updatedAt":"2026-10-29T22:58:38Z","createdAt":"2026-10-27T01:00:00Z",
	  "statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
	                       {"__typename":"CheckRun","status":"COMPLETED","conclusion":"SKIPPED"},
	                       {"__typename":"StatusContext","state":"SUCCESS"}]},
	 {"number":2,"title":"Fail beats pending","author":{"login":"b"},
	  "statusCheckRollup":[{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""},
	                       {"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"},
	                       {"__typename":"CheckRun","status":"QUEUED","conclusion":""}]},
	 {"number":3,"title":"Running","author":{"login":"c"},"isDraft":true,
	  "statusCheckRollup":[{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""},
	                       {"__typename":"StatusContext","state":"SUCCESS"}]},
	 {"number":4,"title":"None","author":{"login":"d"},"statusCheckRollup":[],
	  "body":"Why\n\n![shot](https://github.com/user-attachments/assets/x)",
	  "files":[{"path":"a.go","additions":3,"deletions":1,"changeType":"MODIFIED"}]},
	 {"number":5,"title":"Stale","author":{"login":"e"},
	  "statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
	                       {"__typename":"CheckRun","status":"COMPLETED","conclusion":"STALE"}]}
	]`
	prs, err := ParsePRs("backend", out)
	if err != nil {
		t.Fatal(err)
	}
	if !prs[0].Opened.Equal(time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)) {
		t.Errorf("Opened = %v, want createdAt", prs[0].Opened)
	}
	want := []CheckState{ChecksPassing, ChecksFailing, ChecksPending, ChecksNone, ChecksPending}
	for i, p := range prs {
		if p.Checks != want[i] {
			t.Errorf("#%d %q: checks %v, want %v", p.Number, p.Title, p.Checks, want[i])
		}
	}
	if prs[0].Author != "a" || prs[0].Review != "APPROVED" || prs[0].Updated.IsZero() || !prs[2].Draft || prs[0].Repo != "backend" ||
		prs[0].Base != "main" || !prs[0].Fork || prs[1].Fork {
		t.Fatalf("fields not mapped: %+v", prs[0])
	}
	if !strings.Contains(prs[3].Body, "![shot]") || len(prs[3].Files) != 1 || prs[3].Files[0] != (PRFile{"a.go", 3, 1}) {
		t.Fatalf("body and files: %+v", prs[3])
	}
	if _, err := ParsePRs("x", "To get started with GitHub CLI"); err == nil {
		t.Fatal("non-JSON output must be an error")
	}
}

func TestMarkRequested(t *testing.T) {
	prs := []PR{{Number: 1}, {Number: 2}, {Number: 3}}
	if err := MarkRequested(prs, `[{"number":3},{"number":9}]`); err != nil {
		t.Fatal(err)
	}
	if prs[0].ForMe || prs[1].ForMe || !prs[2].ForMe {
		t.Fatalf("only #3 is requested: %+v", prs)
	}
	if MarkRequested(prs, "not json") == nil {
		t.Fatal("non-JSON output must be an error")
	}
}

func TestParseViewer(t *testing.T) {
	v, err := ParseViewer(`{"login":"octo","name":"Octo Cat","avatar_url":"https://avatars.githubusercontent.com/u/1?v=4","followers":3}`)
	if err != nil || v.Login != "octo" || v.Name != "Octo Cat" || v.Followers != 3 || v.AvatarURL == "" {
		t.Fatalf("got %+v, %v", v, err)
	}
	if _, err := ParseViewer(`{"message":"Bad credentials"}`); err == nil {
		t.Fatal("a response without a login is an error")
	}
}

func TestSlugWithoutOrigin(t *testing.T) {
	if _, err := (Git{}).Slug(context.Background(), gitRepo(t)); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("a repository with no origin remote should report ErrNoOrigin, got %v", err)
	}
}

func TestStack(t *testing.T) {
	prs := []PR{
		{Number: 1, Branch: "a", Base: "main"},
		{Number: 2, Branch: "b", Base: "a"},
		{Number: 3, Branch: "c", Base: "b"},
		{Number: 4, Branch: "d", Base: "a"},
		{Number: 5, Branch: "e", Base: "main"},
		{Number: 6, Branch: "z", Base: "main", Fork: true},
		{Number: 9, Branch: "f", Base: "z"}, // a fork's branch is no parent, though it shares a name
		{Number: 7, Branch: "x", Base: "y"},
		{Number: 8, Branch: "y", Base: "x"}, // a loop of bases still ends
	}
	got, depth := Stack(prs, prs[2])
	var nums []int
	for _, p := range got {
		nums = append(nums, p.Number)
	}
	if !slices.Equal(nums, []int{1, 2, 3, 4}) || !slices.Equal(depth, []int{0, 1, 2, 1}) {
		t.Fatalf("stack of #3 = %v at depths %v", nums, depth)
	}
	for _, i := range []int{4, 6} {
		if s, _ := Stack(prs, prs[i]); s != nil {
			t.Fatalf("#%d stands alone, got %v", prs[i].Number, s)
		}
	}
	if s, _ := Stack(prs, prs[7]); len(s) != 2 {
		t.Fatalf("a loop should list both PRs once, got %v", s)
	}
}

func TestMarkComments(t *testing.T) {
	prs := []PR{{Number: 1}, {Number: 2}}
	// gh prints one node per line; a review needs a summary to count, and a pending one never counts.
	out := `{"number":1,"comments":{"totalCount":2,"nodes":[{"author":{"login":"a"},"createdAt":"2026-10-01T00:00:00Z"}]},
	 "reviews":{"nodes":[{"author":{"login":"b"},"body":"Looks good","submittedAt":"2026-10-02T00:00:00Z"},
	  {"author":{"login":"c"},"body":"","submittedAt":"2026-10-03T00:00:00Z"},{"author":{"login":"d"},"body":"Draft note","submittedAt":null}]}}
{"number":9,"comments":{"totalCount":5,"nodes":[]},"reviews":{"nodes":[]}}`
	if err := MarkComments(prs, out); err != nil {
		t.Fatal(err)
	}
	if p := prs[0]; p.Comments != 3 || p.LastBy != "b" || !p.LastAt.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("PR 1 should count 2 comments and 1 review, the review newest: %+v", p)
	}
	if prs[1].Comments != 0 {
		t.Fatal("a PR with no node keeps no count")
	}
	if MarkComments(prs, "{") == nil {
		t.Fatal("broken output should be an error")
	}
}

func TestParseComments(t *testing.T) {
	out := `{"comments":{"nodes":[{"author":{"login":"coderabbitai","__typename":"Bot"},"body":"Walkthrough","createdAt":"2026-10-03T00:00:00Z"},
	  {"author":null,"body":"From a deleted account","createdAt":"2026-10-01T00:00:00Z"}]},
	 "reviews":{"nodes":[{"author":{"login":"b","__typename":"User"},"body":"Needs a test","state":"CHANGES_REQUESTED","submittedAt":"2026-10-02T00:00:00Z"},
	  {"author":{"login":"c","__typename":"User"},"body":"","state":"APPROVED","submittedAt":"2026-10-04T00:00:00Z"}]}}`
	cs, err := ParseComments(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 || cs[0].Author != "ghost" || cs[1].State != "CHANGES_REQUESTED" || !cs[2].Bot || cs[1].Bot {
		t.Fatalf("want oldest first, the empty approval left out and the bot marked: %+v", cs)
	}
}
