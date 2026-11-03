package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type claimParentIssueResp struct {
	Task *struct {
		ID          string            `json:"id"`
		ParentIssue *ClaimParentIssue `json:"parent_issue"`
	} `json:"task"`
}

func claimParentIssueTask(t *testing.T, runtimeID string) claimParentIssueResp {
	t.Helper()
	req := newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	var resp claimParentIssueResp
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimed task, got nil: %s", w.Body.String())
	}
	return resp
}

// A run on a sub-issue is handed its parent so the daemon can point the agent
// at the discussion that produced the sub-issue (MUL-8441).
func TestClaimTaskByRuntime_SubIssueCarriesParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Parent issue runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Parent issue agent")

	parentID := dbfx.Issue(t, "Merge PRs from Multica", testutil.Cols{"status": "in_review"})
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, parentID, issueID)
	var prefix string
	var number int32
	dbfx.QueryRow(t, `SELECT w.issue_prefix, i.number FROM issue i JOIN workspace w ON w.id = i.workspace_id WHERE i.id = $1`, parentID).
		Scan(&prefix, &number)

	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)
	got := claimParentIssueTask(t, runtimeID).Task.ParentIssue
	if got == nil {
		t.Fatalf("sub-issue claim must carry parent_issue")
	}
	want := ClaimParentIssue{
		ID:         parentID,
		Identifier: service.IssueIdentifier(prefix, number),
		Title:      "Merge PRs from Multica",
		Status:     "in_review",
	}
	if *got != want {
		t.Errorf("parent_issue = %+v, want %+v", *got, want)
	}
}

func TestClaimTaskByRuntime_TopLevelIssueHasNoParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "No parent runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "No parent agent")

	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)
	if got := claimParentIssueTask(t, runtimeID).Task.ParentIssue; got != nil {
		t.Errorf("top-level issue must not carry parent_issue, got %+v", *got)
	}
}

// A parent that does not resolve inside the sub-issue's workspace is dropped,
// and the claim still succeeds: the parent is context, not a precondition.
func TestClaimTaskByRuntime_ParentOutsideWorkspaceIsDropped(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Foreign parent runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Foreign parent agent")

	otherWorkspaceID := dbfx.Workspace(t, "Foreign parent workspace", "foreign-parent-ws")
	foreignParentID := dbfx.Issue(t, "Another tenant's issue", testutil.Cols{"workspace_id": otherWorkspaceID})
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, foreignParentID, issueID)

	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)
	if got := claimParentIssueTask(t, runtimeID).Task.ParentIssue; got != nil {
		t.Errorf("parent outside the workspace must be dropped, got %+v", *got)
	}
}
