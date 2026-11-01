package gitx

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Deployment is what GitHub Deployments records for one environment. It is not verified against the running service.
type Deployment struct {
	Repo        string
	Environment string
	Commit      string // newest successful (ACTIVE) deployment; empty when none is in the window
	Ref         string // branch or tag GitHub recorded for it, if any
	When        time.Time
	Latest      string // state of a newer attempt that did not succeed (FAILURE, IN_PROGRESS…), else empty
	LatestWhen  time.Time
}

type deployNode struct {
	State     string
	CommitOid string
	CreatedAt time.Time
	Ref       struct{ Name string }
}

// Deployments reads, per environment, the newest successful deployment and any newer unsuccessful attempt.
func (g Git) Deployments(ctx context.Context, repo, dir string) ([]Deployment, error) {
	slug, err := g.Slug(ctx, dir)
	if err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(slug, "/")
	// shortcut: first 50 environments per repository; page if a repository has more.
	out, err := command(ctx, dir, ghTimeout, "gh", "api", "graphql", "-f", "owner="+owner, "-f", "name="+name,
		"-f", "query=query($owner:String!,$name:String!){repository(owner:$owner,name:$name){environments(first:50){nodes{name}}}}",
		"--jq", ".data.repository.environments.nodes[].name")
	if err != nil {
		return nil, err
	}
	var envs []string
	for _, e := range strings.Split(out, "\n") {
		if e = strings.TrimSpace(e); e != "" {
			envs = append(envs, e)
		}
	}
	if len(envs) == 0 {
		return nil, nil
	}
	// One aliased query for all environments; names travel as variables, never inside the query text.
	args := []string{"api", "graphql", "-f", "owner=" + owner, "-f", "name=" + name}
	vars, fields := []string{"$owner:String!", "$name:String!"}, ""
	for i, env := range envs {
		args = append(args, "-f", fmt.Sprintf("e%d=%s", i, env))
		vars = append(vars, fmt.Sprintf("$e%d:String!", i))
		// shortcut: newest 20 attempts per environment; an environment with 20 failures in a row shows no deployed commit.
		fields += fmt.Sprintf(" e%d:deployments(environments:[$e%d],first:20,orderBy:{field:CREATED_AT,direction:DESC}){nodes{state commitOid createdAt ref{name}}}", i, i)
	}
	query := fmt.Sprintf("query(%s){repository(owner:$owner,name:$name){%s}}", strings.Join(vars, ","), fields)
	out, err = command(ctx, dir, ghTimeout, "gh", append(args, "-f", "query="+query)...)
	if err != nil {
		return nil, err
	}
	return ParseDeployments(repo, envs, out)
}

// ParseDeployments parses the aliased GraphQL response; envs[i] is the environment queried as alias e<i>.
func ParseDeployments(repo string, envs []string, out string) ([]Deployment, error) {
	var resp struct {
		Data struct {
			Repository map[string]struct{ Nodes []deployNode }
		}
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	var deps []Deployment
	for i, env := range envs {
		nodes := resp.Data.Repository[fmt.Sprintf("e%d", i)].Nodes // newest first
		if len(nodes) == 0 {
			continue
		}
		d := Deployment{Repo: repo, Environment: env}
		for _, n := range nodes {
			if n.State == "ACTIVE" { // these workflows never mark older successes INACTIVE, so take the newest
				d.Commit, d.Ref, d.When = n.CommitOid, n.Ref.Name, n.CreatedAt
				break
			}
		}
		if nodes[0].State != "ACTIVE" {
			d.Latest, d.LatestWhen = nodes[0].State, nodes[0].CreatedAt
		}
		deps = append(deps, d)
	}
	// Most recent activity first (success or a newer attempt), so long-unused environments sink to the bottom.
	last := func(d Deployment) time.Time {
		if d.LatestWhen.After(d.When) {
			return d.LatestWhen
		}
		return d.When
	}
	slices.SortStableFunc(deps, func(a, b Deployment) int { return last(b).Compare(last(a)) })
	return deps, nil
}

// IsCommitID reports whether s is a full hexadecimal object ID, safe to pass to git as a revision.
func IsCommitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}
