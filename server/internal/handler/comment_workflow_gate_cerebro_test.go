package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type commentWorkflowGateFunc func(context.Context, CommentWorkflowGateInput) (CommentWorkflowGateResult, error)

func (f commentWorkflowGateFunc) EvaluateComment(ctx context.Context, input CommentWorkflowGateInput) (CommentWorkflowGateResult, error) {
	return f(ctx, input)
}

// TestCreateComment_WorkflowCorrectsResumedSessionParentDrift locks the
// production regression behind FIR-3692: a resumed agent session may submit
// the prior turn's parent, but before.message.send must move the reply onto the
// exact comment that triggered the current task.
func TestCreateComment_WorkflowCorrectsResumedSessionParentDrift(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fixture := newRunningSquadLeaderTaskFixture(t)
	var staleParentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'older thread', 'comment')
		RETURNING id
	`, fixture.IssueID, testWorkspaceID, testUserID).Scan(&staleParentID); err != nil {
		t.Fatalf("create stale parent: %v", err)
	}

	originalGate := testHandler.CommentTargetGuard
	t.Cleanup(func() { testHandler.CommentTargetGuard = originalGate })
	var gotInput CommentWorkflowGateInput
	testHandler.CommentTargetGuard = commentWorkflowGateFunc(func(_ context.Context, input CommentWorkflowGateInput) (CommentWorkflowGateResult, error) {
		gotInput = input
		return CommentWorkflowGateResult{Allowed: true, ParentID: fixture.TriggerCommentID}, nil
	})

	request := newRequest("POST", "/api/issues/"+fixture.IssueID+"/comments", map[string]any{
		"content":   "[@all](mention://all/all) Reply from the resumed session.",
		"parent_id": staleParentID,
	})
	request = withURLParam(request, "id", fixture.IssueID)
	request.Header.Set("X-Agent-ID", fixture.LeaderID)
	request.Header.Set("X-Task-ID", fixture.TaskID)
	response := httptest.NewRecorder()

	testHandler.CreateComment(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", response.Code, response.Body.String())
	}
	if !gotInput.ThreadRequired || gotInput.ParentID != staleParentID || gotInput.RequiredParentID != fixture.TriggerCommentID {
		t.Fatalf("Workflow input = %#v", gotInput)
	}

	var storedParentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT parent_id
		FROM comment
		WHERE issue_id=$1 AND author_type='agent' AND author_id=$2
		ORDER BY created_at DESC
		LIMIT 1
	`, fixture.IssueID, fixture.LeaderID).Scan(&storedParentID); err != nil {
		t.Fatalf("load stored comment: %v", err)
	}
	if storedParentID != fixture.TriggerCommentID {
		t.Fatalf("stored parent_id = %s, want trigger comment %s", storedParentID, fixture.TriggerCommentID)
	}
}
