package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func dispatchCleanupReceipt(t *testing.T, key string) (*TaskService, string, string, string) {
	t.Helper()
	svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	req := reworkRequest(agentID, key, "cleanup receipt")
	req.SourceTaskID = reworkSourceTask(t, pool, agentID, runtimeID, issueID)
	req.ActorUserID = util.MustParseUUID(creatorID)
	workspaceID := reworkWorkspaceID(t, svc, issueID)
	if _, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, req, nil); err != nil {
		t.Fatalf("dispatch receipt %s: %v", key, err)
	}
	return svc, issueID, util.UUIDToString(workspaceID), key
}

func hasReceipt(t *testing.T, svc *TaskService, issueID, workspaceID, key string) bool {
	t.Helper()
	_, err := svc.Queries.GetIssueReworkDispatchReceipt(context.Background(), db.GetIssueReworkDispatchReceiptParams{IssueID: util.MustParseUUID(issueID), WorkspaceID: util.MustParseUUID(workspaceID), OperationKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func TestDeleteIssueCleansOnlyItsReworkReceipts(t *testing.T) {
	svc, issueID, workspaceID, key := dispatchCleanupReceipt(t, "delete-issue-target")
	otherSvc, otherIssueID, otherWorkspaceID, otherKey := dispatchCleanupReceipt(t, "delete-issue-other")
	if err := svc.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{ID: util.MustParseUUID(issueID), WorkspaceID: util.MustParseUUID(workspaceID)}); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if hasReceipt(t, svc, issueID, workspaceID, key) {
		t.Fatal("deleted issue receipt remains")
	}
	if !hasReceipt(t, otherSvc, otherIssueID, otherWorkspaceID, otherKey) {
		t.Fatal("other issue receipt was removed")
	}
}

func TestDeleteWorkspaceIssueRootsCleansOnlyWorkspaceReworkReceipts(t *testing.T) {
	svc, issueID, workspaceID, key := dispatchCleanupReceipt(t, "delete-workspace-target")
	otherSvc, otherIssueID, otherWorkspaceID, otherKey := dispatchCleanupReceipt(t, "delete-workspace-other")
	if err := svc.Queries.DeleteWorkspaceIssueRoots(context.Background(), util.MustParseUUID(workspaceID)); err != nil {
		t.Fatalf("DeleteWorkspaceIssueRoots: %v", err)
	}
	if hasReceipt(t, svc, issueID, workspaceID, key) {
		t.Fatal("deleted workspace receipt remains")
	}
	if !hasReceipt(t, otherSvc, otherIssueID, otherWorkspaceID, otherKey) {
		t.Fatal("other workspace receipt was removed")
	}
}
