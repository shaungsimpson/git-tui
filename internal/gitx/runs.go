package gitx

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Run is one GitHub Actions workflow run that deploys.
type Run struct {
	Repo       string
	ID         int64
	Workflow   string
	Title      string
	Event      string
	Branch     string
	Commit     string
	Status     string // queued, in_progress, completed…
	Conclusion string // success, failure, cancelled… once completed
	URL        string
	Started    time.Time
	Updated    time.Time
}

// Active reports whether the run has not finished yet.
func (r Run) Active() bool { return r.Status != "completed" }

// DeployRuns lists recent workflow runs that deploy: runs of workflows named "deploy" (except pull request
// checks), and runs matching the workflow, event and branch of a run that recorded a GitHub deployment
// (for example a CI pipeline on main).
func (g Git) DeployRuns(ctx context.Context, repo, dir string) ([]Run, error) {
	slug, err := g.Slug(ctx, dir)
	if err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(slug, "/")
	deployLogs := background(func() (string, error) {
		return command(ctx, dir, ghTimeout, "gh", "api", "graphql", "-f", "owner="+owner, "-f", "name="+name,
			"-f", "query=query($owner:String!,$name:String!){repository(owner:$owner,name:$name){deployments(first:50,orderBy:{field:CREATED_AT,direction:DESC}){nodes{latestStatus{logUrl}}}}}",
			"--jq", ".data.repository.deployments.nodes[].latestStatus.logUrl // empty")
	})
	// shortcut: the newest 100 runs of every workflow; deploy runs older than that are not listed.
	runs, err := command(ctx, dir, ghTimeout, "gh", "run", "list", "--repo", slug, "--limit", "100", "--json",
		"databaseId,workflowName,displayTitle,event,headBranch,headSha,status,conclusion,startedAt,updatedAt,url")
	if err != nil {
		return nil, err
	}
	logs, err := deployLogs()
	if err != nil {
		return nil, err
	}
	return ParseDeployRuns(repo, runs, logs)
}

// ParseDeployRuns keeps the deploy runs from `gh run list` JSON; logURLs are deployment log links, one per line.
func ParseDeployRuns(repo, runsJSON, logURLs string) ([]Run, error) {
	var raw []struct {
		DatabaseID   int64
		WorkflowName string
		DisplayTitle string
		Event        string
		HeadBranch   string
		HeadSha      string
		Status       string
		Conclusion   string
		StartedAt    time.Time
		UpdatedAt    time.Time
		URL          string
	}
	if err := json.Unmarshal([]byte(runsJSON), &raw); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	deployed := map[int64]bool{}
	for _, u := range strings.Split(logURLs, "\n") {
		if _, rest, ok := strings.Cut(u, "/actions/runs/"); ok {
			id, _ := strconv.ParseInt(strings.SplitN(rest, "/", 2)[0], 10, 64)
			deployed[id] = true
		}
	}
	key := func(workflow, event, branch string) string { return workflow + "\x1f" + event + "\x1f" + branch }
	deploys := map[string]bool{}
	for _, r := range raw {
		if deployed[r.DatabaseID] {
			deploys[key(r.WorkflowName, r.Event, r.HeadBranch)] = true
		}
	}
	var out []Run
	for _, r := range raw {
		named := strings.Contains(strings.ToLower(r.WorkflowName), "deploy") && !strings.HasPrefix(r.Event, "pull_request")
		if !named && !deploys[key(r.WorkflowName, r.Event, r.HeadBranch)] {
			continue // e.g. the same "test, build and deploy" workflow running as PR checks
		}
		out = append(out, Run{
			Repo: repo, ID: r.DatabaseID, Workflow: r.WorkflowName, Title: r.DisplayTitle, Event: r.Event,
			Branch: r.HeadBranch, Commit: r.HeadSha, Status: r.Status, Conclusion: r.Conclusion, URL: r.URL,
			Started: r.StartedAt, Updated: r.UpdatedAt,
		})
	}
	slices.SortStableFunc(out, func(a, b Run) int { return b.Started.Compare(a.Started) })
	return out, nil
}
