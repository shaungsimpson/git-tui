package gitx

import (
	"strings"
	"testing"
)

func TestRepoSlug(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/frontend.git\n":         "acme/frontend",
		"git@github.com:/acme/backend.git":           "acme/backend", // real remote form
		"https://github.com/acme/personal-site":      "acme/personal-site",
		"ssh://git@github.com/owner/repo.git":        "owner/repo",
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
	 {"number":1,"title":"Pass","author":{"login":"a"},"isDraft":false,"reviewDecision":"APPROVED","updatedAt":"2026-10-29T22:58:38Z",
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
	want := []CheckState{ChecksPassing, ChecksFailing, ChecksPending, ChecksNone, ChecksPending}
	for i, p := range prs {
		if p.Checks != want[i] {
			t.Errorf("#%d %q: checks %v, want %v", p.Number, p.Title, p.Checks, want[i])
		}
	}
	if prs[0].Author != "a" || prs[0].Review != "APPROVED" || prs[0].Updated.IsZero() || !prs[2].Draft || prs[0].Repo != "backend" {
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
