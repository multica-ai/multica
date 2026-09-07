package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestClaimTaskByRuntime_PopulatesIssueStatusCatalog verifies the claim
// response carries the workspace's active CUSTOM statuses — and only those —
// so the daemon can render them into the agent brief (MUL-6460). Built-ins
// stay off the wire (the daemon knows them), archived statuses stay off the
// wire (they reject writes), and entries arrive in catalog order (category
// rank first), because the daemon renders them verbatim without re-sorting.
func TestClaimTaskByRuntime_PopulatesIssueStatusCatalog(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	createTestCustomStatus(t, "rework", "todo")
	dbfx.Exec(t, `UPDATE issue_status SET name = 'Rework', description = 'Sent back by review' WHERE workspace_id = $1 AND key = 'rework'`, testWorkspaceID)
	createTestCustomStatus(t, "later", "backlog")
	archived := createTestCustomStatus(t, "old_qa", "in_review")
	dbfx.Exec(t, `UPDATE issue_status SET archived_at = now() WHERE id = $1`, archived.ID)

	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue status catalog claim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Issue status catalog claim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "issue-status-catalog-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID            string `json:"id"`
			IssueStatuses []struct {
				Key         string `json:"key"`
				Name        string `json:"name"`
				Category    string `json:"category"`
				Description string `json:"description"`
			} `json:"issue_statuses"`
			IssueStatusesOmitted int `json:"issue_statuses_omitted"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected dispatched task %s to be claimed, got nil response: %s", taskID, w.Body.String())
	}
	if resp.Task.ID != taskID {
		t.Fatalf("claimed task id = %s, want %s", resp.Task.ID, taskID)
	}
	if got := len(resp.Task.IssueStatuses); got != 2 {
		t.Fatalf("issue_statuses count = %d, want 2 (active customs only): %+v", got, resp.Task.IssueStatuses)
	}
	// Catalog order: backlog (rank 0) precedes todo (rank 1).
	if resp.Task.IssueStatuses[0].Key != "later" || resp.Task.IssueStatuses[0].Category != "backlog" {
		t.Errorf("issue_statuses[0] = %+v, want key=later category=backlog first by category rank", resp.Task.IssueStatuses[0])
	}
	second := resp.Task.IssueStatuses[1]
	if second.Key != "rework" || second.Category != "todo" || second.Name != "Rework" || second.Description != "Sent back by review" {
		t.Errorf("issue_statuses[1] = %+v, want the full rework entry (key/name/category/description)", second)
	}
	if resp.Task.IssueStatusesOmitted != 0 {
		t.Errorf("issue_statuses_omitted = %d, want 0 under the cap", resp.Task.IssueStatusesOmitted)
	}
}

// TestClaimTaskByRuntime_IssueStatusCatalogAbsentWithoutCustoms pins the
// compatibility contract: a workspace with no custom statuses claims with NO
// issue_statuses field at all (omitempty), which is what keeps the daemon's
// brief byte-identical to the pre-MUL-6460 form for existing deployments.
func TestClaimTaskByRuntime_IssueStatusCatalogAbsentWithoutCustoms(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	seedTestCatalog(t)

	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue status catalog empty claim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Issue status catalog empty claim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "issue-status-catalog-empty-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *map[string]any `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected dispatched task %s to be claimed, got nil response: %s", taskID, w.Body.String())
	}
	if _, present := (*resp.Task)["issue_statuses"]; present {
		t.Errorf("issue_statuses must be absent for a workspace with only built-in statuses")
	}
	if _, present := (*resp.Task)["issue_statuses_omitted"]; present {
		t.Errorf("issue_statuses_omitted must be absent when nothing was omitted")
	}
}
