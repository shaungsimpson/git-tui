package gitx

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestParseDeployments(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 40) }
	out := `{"data":{"repository":{
	 "e0":{"nodes":[{"state":"FAILURE","commitOid":"` + sha("b") + `","createdAt":"2026-10-30T05:00:00Z","ref":{"name":"main"}},
	                {"state":"ACTIVE","commitOid":"` + sha("a") + `","createdAt":"2026-10-29T05:00:00Z","ref":{"name":"v6.3.6"}},
	                {"state":"ACTIVE","commitOid":"` + sha("9") + `","createdAt":"2026-10-28T05:00:00Z","ref":{"name":"v6.3.5"}}]},
	 "e1":{"nodes":[{"state":"ACTIVE","commitOid":"` + sha("c") + `","createdAt":"2026-10-30T06:00:00Z","ref":null}]},
	 "e2":{"nodes":[{"state":"ERROR","commitOid":"` + sha("d") + `","createdAt":"2026-01-01T00:00:00Z","ref":{"name":"main"}}]},
	 "e3":{"nodes":[]},
	 "e4":{"nodes":[{"state":"IN_PROGRESS","commitOid":"` + sha("e") + `","createdAt":"2026-10-31T00:00:00Z","ref":{"name":"main"}}]}}}}`
	deps, err := ParseDeployments("backend", []string{"prod", "stage", "old", "unused", "fresh"}, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 4 {
		t.Fatalf("environments without deployments should be skipped: %+v", deps)
	}
	var order []string
	for _, d := range deps {
		order = append(order, d.Environment)
	}
	// fresh's first deploy is still running, so it sorts by that attempt, not to the bottom.
	if strings.Join(order, ",") != "fresh,stage,prod,old" {
		t.Fatalf("want most recent activity first, got %v", order)
	}
	deps = deps[1:]
	prod := deps[1]
	if prod.Commit != sha("a") || prod.Ref != "v6.3.6" || prod.Latest != "FAILURE" || prod.LatestWhen.IsZero() {
		t.Fatalf("prod should keep the newest success and flag the newer failure: %+v", prod)
	}
	if deps[0].Latest != "" || deps[0].Ref != "" {
		t.Fatalf("stage: %+v", deps[0])
	}
	if deps[2].Commit != "" || deps[2].Latest != "ERROR" {
		t.Fatalf("an environment with no success has no deployed commit: %+v", deps[2])
	}
}

func TestDeploymentsPassNamesAsVariables(t *testing.T) {
	hostile := `x"){a}`
	// The first call lists the environments; the second answers e0 with commit a… and e1 with commit b….
	log := fakeGH(t, `case "$*" in
*"environments(first"*) printf '%s\n' 'x"){a}' prod ;;
*) a=$(printf 'a%.0s' $(seq 40)); b=$(printf 'b%.0s' $(seq 40))
   echo '{"data":{"repository":{"e0":{"nodes":[{"state":"ACTIVE","commitOid":"'$a'","createdAt":"2026-01-02T00:00:00Z"}]},"e1":{"nodes":[{"state":"ACTIVE","commitOid":"'$b'","createdAt":"2026-01-01T00:00:00Z"}]}}}}' ;;
esac`)
	deps, err := Git{}.Deployments(context.Background(), "r", originRepo(t, "acme/r"))
	if err != nil {
		t.Fatal(err)
	}
	calls := ghCalls(t, log)
	if len(calls) != 2 {
		t.Fatalf("want 2 gh calls, got %q", calls)
	}
	for i, a := range calls[1] {
		// "{a}" survives any escaping, so it catches the name written into the query text in any form.
		if strings.Contains(a, "{a}") && (a != "e0="+hostile || calls[1][i-1] != "-f") {
			t.Fatalf("an environment name may only travel as the -f e0= variable, found in %q", a)
		}
	}
	if !slices.Contains(calls[1], "e0="+hostile) || !slices.Contains(calls[1], "e1=prod") {
		t.Fatalf("names not passed as e0 and e1: %q", calls[1])
	}
	query := calls[1][slices.IndexFunc(calls[1], func(a string) bool { return strings.HasPrefix(a, "query=") })]
	for _, alias := range []string{"e0:deployments(environments:[$e0]", "e1:deployments(environments:[$e1]"} {
		if !strings.Contains(query, alias) { // ParseDeployments reads alias eN as the environment in variable eN
			t.Fatalf("query lacks %q: %s", alias, query)
		}
	}
	byEnv := map[string]string{}
	for _, d := range deps {
		byEnv[d.Environment] = d.Commit[:1]
	}
	if byEnv[hostile] != "a" || byEnv["prod"] != "b" {
		t.Fatalf("aliases read back for the wrong environments: %v", byEnv)
	}
}

func TestIsCommitID(t *testing.T) {
	for in, want := range map[string]bool{
		strings.Repeat("a", 40): true, strings.Repeat("0", 64): true, "abc123": false,
		"--output=" + strings.Repeat("a", 31): false, strings.Repeat("A", 40): false, "": false,
	} {
		if IsCommitID(in) != want {
			t.Errorf("IsCommitID(%q) != %v", in, want)
		}
	}
}

func TestCompareAcceptsCommitIDs(t *testing.T) {
	dir := gitRepo(t)
	g := Git{}
	tags, err := g.Tags(context.Background(), "tmp", dir)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(tags, func(t Tag) bool { return t.Name == "v1" })
	v1 := tags[i].Commit
	commits, err := g.Compare(context.Background(), "tmp", dir, v1, "HEAD")
	if err != nil || len(commits) != 2 {
		t.Fatalf("commits after v1's commit: %v %+v", err, commits)
	}
	if _, err := g.Compare(context.Background(), "tmp", dir, "--output=/dev/null", "HEAD"); err == nil {
		t.Fatal("an option-like revision must be rejected, not run as an option")
	}
}
