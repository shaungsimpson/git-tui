package gitx

import "testing"

func TestParseDeployRuns(t *testing.T) {
	runs := `[
	 {"databaseId":1,"workflowName":"Main CI/CD Pipeline","event":"push","headBranch":"main","status":"completed","conclusion":"success","startedAt":"2026-10-30T03:12:00Z"},
	 {"databaseId":2,"workflowName":"Main CI/CD Pipeline","event":"push","headBranch":"main","status":"in_progress","conclusion":"","startedAt":"2026-10-30T04:00:00Z"},
	 {"databaseId":3,"workflowName":"Main CI/CD Pipeline","event":"pull_request","headBranch":"feature/x","status":"completed","conclusion":"success","startedAt":"2026-10-30T05:00:00Z"},
	 {"databaseId":4,"workflowName":"Direct Deploy","event":"workflow_dispatch","headBranch":"v6.3.6","status":"completed","conclusion":"success","startedAt":"2026-10-29T05:20:00Z"},
	 {"databaseId":5,"workflowName":"Frontend test, build and deploy","event":"pull_request","headBranch":"fix/y","status":"completed","conclusion":"success","startedAt":"2026-10-30T06:00:00Z"},
	 {"databaseId":6,"workflowName":"Renovate","event":"workflow_dispatch","headBranch":"main","status":"completed","conclusion":"success","startedAt":"2026-10-30T07:00:00Z"}
	]`
	// Run 1 recorded a deployment, so later main pushes of the same pipeline count as deploys.
	logs := "https://github.com/o/r/actions/runs/1/job/99\nhttps://example.com/not-a-run\n"
	got, err := ParseDeployRuns("backend", runs, logs)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	// Newest first: 2 (pipeline, running), 1 (pipeline), 4 (named deploy). Not 3 (PR CI), 5 (PR checks), 6 (Renovate).
	if len(ids) != 3 || ids[0] != 2 || ids[1] != 1 || ids[2] != 4 {
		t.Fatalf("deploy runs %v", ids)
	}
	if !got[0].Active() || got[1].Active() || got[0].Repo != "backend" {
		t.Fatalf("fields: %+v", got[0])
	}
	if _, err := ParseDeployRuns("x", "not json", ""); err == nil {
		t.Fatal("bad JSON must be an error")
	}
}

func TestParseRunReasons(t *testing.T) {
	got, err := ParseRunReasons(`["Canceling since a higher priority waiting request for CI exists","Canceling since a higher priority waiting request for CI exists","Failed to restore\nRequestId: 1","  "]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Canceling since a higher priority waiting request for CI exists" || got[1] != "Failed to restore" {
		t.Fatalf("reasons %q", got)
	}
	if _, err := ParseRunReasons("not json"); err == nil {
		t.Fatal("bad JSON must be an error")
	}
	if !(Run{Status: "completed", Conclusion: "cancelled"}).Stopped() || (Run{Status: "completed", Conclusion: "success"}).Stopped() || (Run{Status: "in_progress"}).Stopped() {
		t.Fatal("Stopped: only finished runs that did not succeed")
	}
}
