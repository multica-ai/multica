package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestAutopilotFailureRecoveryConfigRoundTripAndPartialUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createWebhookTestAgent(t, "Failure Recovery Config Agent")
	receiptIssueID := dbfx.Issue(t, "failure receipt target")
	createReq := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                                "Scheduled failure recovery",
		"assignee_id":                          agentID,
		"execution_mode":                       "run_only",
		"resource_failure_retry_enabled":       true,
		"resource_failure_retry_delay_seconds": 2700,
		"failure_receipt_issue_id":             receiptIssueID,
		"failure_receipt_marker":               "validation_officer_daily",
	})
	var created AutopilotResponse
	testutil.Call(t, testHandler.CreateAutopilot, createReq).
		Want(http.StatusCreated).
		JSON(&created)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, created.ID)
	})

	if !created.ResourceFailureRetryEnabled || created.ResourceFailureRetryDelaySeconds != 2700 {
		t.Fatalf("retry config = enabled %v delay %d, want true/2700", created.ResourceFailureRetryEnabled, created.ResourceFailureRetryDelaySeconds)
	}
	if created.FailureReceiptIssueID == nil || *created.FailureReceiptIssueID != receiptIssueID {
		t.Fatalf("failure_receipt_issue_id = %v, want %s", created.FailureReceiptIssueID, receiptIssueID)
	}
	if created.FailureReceiptMarker == nil || *created.FailureReceiptMarker != "validation_officer_daily" {
		t.Fatalf("failure_receipt_marker = %v, want validation_officer_daily", created.FailureReceiptMarker)
	}

	partialReq := newRequest("PATCH", "/api/autopilots/"+created.ID+"?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Scheduled failure recovery renamed",
	})
	partialReq = withURLParam(partialReq, "id", created.ID)
	var partial AutopilotResponse
	testutil.Call(t, testHandler.UpdateAutopilot, partialReq).
		Want(http.StatusOK).
		JSON(&partial)
	if !partial.ResourceFailureRetryEnabled || partial.ResourceFailureRetryDelaySeconds != 2700 ||
		partial.FailureReceiptIssueID == nil || *partial.FailureReceiptIssueID != receiptIssueID ||
		partial.FailureReceiptMarker == nil || *partial.FailureReceiptMarker != "validation_officer_daily" {
		t.Fatalf("partial PATCH cleared failure recovery config: %+v", partial)
	}

	clearReq := newRequest("PATCH", "/api/autopilots/"+created.ID+"?workspace_id="+testWorkspaceID, map[string]any{
		"failure_receipt_issue_id": nil,
		"failure_receipt_marker":   nil,
	})
	clearReq = withURLParam(clearReq, "id", created.ID)
	var cleared AutopilotResponse
	testutil.Call(t, testHandler.UpdateAutopilot, clearReq).
		Want(http.StatusOK).
		JSON(&cleared)
	if cleared.FailureReceiptIssueID != nil || cleared.FailureReceiptMarker != nil {
		t.Fatalf("cleared receipt config = issue %v marker %v, want nil/nil", cleared.FailureReceiptIssueID, cleared.FailureReceiptMarker)
	}
}
