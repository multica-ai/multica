package handler

import (
	"context"
	"fmt"
	"net/http"
	"slices"
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
	createTestCustomStatus(t, "later", "unstarted")
	createTestCustomStatus(t, "awaiting_response", "started")
	createTestCustomStatus(t, "accepted", "done")
	createTestCustomStatus(t, "withdrawn", "closed")
	// Contracted storage must retain the installed daemon wire vocabulary.
	dbfx.Exec(t, `UPDATE issue_status SET description = 'Custom guidance' WHERE workspace_id = $1 AND key IN ('awaiting_response', 'accepted', 'withdrawn')`, testWorkspaceID)
	// A different workspace's catalog must never enter this claim.
	otherWorkspace := dbfx.Workspace(t, "Other catalog", "other-claim-catalog")
	dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": otherWorkspace, "key": "private_status", "name": "Private status",
		"category": "started", "description": "Other workspace only", "color": "#123456",
	})
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
	if got := len(resp.Task.IssueStatuses); got != 5 {
		t.Fatalf("issue_statuses count = %d, want 5 (active customs in this workspace only): %+v", got, resp.Task.IssueStatuses)
	}
	// Both statuses share unstarted; insertion position determines their order.
	if resp.Task.IssueStatuses[1].Key != "later" || resp.Task.IssueStatuses[1].Category != "todo" {
		t.Errorf("issue_statuses[1] = %+v, want key=later category=todo", resp.Task.IssueStatuses[1])
	}
	first := resp.Task.IssueStatuses[0]
	if first.Key != "rework" || first.Category != "todo" || first.Name != "Rework" || first.Description != "Sent back by review" {
		t.Errorf("issue_statuses[0] = %+v, want the full rework entry (key/name/category/description)", first)
	}
	for i, want := range []struct{ key, category string }{
		{"awaiting_response", "in_progress"}, {"accepted", "done"}, {"withdrawn", "cancelled"},
	} {
		got := resp.Task.IssueStatuses[i+2]
		if got.Key != want.key || got.Name != want.key || got.Category != want.category || got.Description != "Custom guidance" {
			t.Errorf("issue_statuses[%d] = %+v, want key/name=%s category=%s", i+2, got, want.key, want.category)
		}
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

// The claim cap counts active customs only, preserves catalog order, and
// discloses server truncation independently of daemon rendering omissions.
func TestClaimTaskByRuntime_IssueStatusCatalogCap(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	for i := 0; i < 34; i++ {
		createTestCustomStatus(t, fmt.Sprintf("custom_%02d", i), "started")
	}
	archived := createTestCustomStatus(t, "archived_custom", "started")
	dbfx.Exec(t, `UPDATE issue_status SET archived_at = now() WHERE id = $1`, archived.ID)
	runtimeID := createClaimReclaimRuntime(t, ctx, "Capped catalog runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Capped catalog agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "capped-catalog-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	var resp struct {
		Task *struct {
			ID                   string                `json:"id"`
			IssueStatuses        []TaskIssueStatusData `json:"issue_statuses"`
			IssueStatusesOmitted int                   `json:"issue_statuses_omitted"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
	if resp.Task == nil || resp.Task.ID != taskID {
		t.Fatalf("expected task %s, got %+v", taskID, resp.Task)
	}
	if len(resp.Task.IssueStatuses) != 30 || resp.Task.IssueStatusesOmitted != 4 {
		t.Fatalf("want 30 entries and 4 omitted, got %+v", resp.Task)
	}
	keys := make([]string, 0, 30)
	for _, entry := range resp.Task.IssueStatuses {
		if entry.Category != "in_progress" {
			t.Errorf("category = %q, want in_progress", entry.Category)
		}
		keys = append(keys, entry.Key)
	}
	want := make([]string, 30)
	for i := range want {
		want[i] = fmt.Sprintf("custom_%02d", i)
	}
	if !slices.Equal(keys, want) {
		t.Errorf("catalog keys = %v, want %v", keys, want)
	}
}
