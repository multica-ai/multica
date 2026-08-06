package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func markIssueAsExecutionSuppressedParent(t *testing.T, issueID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET metadata = jsonb_build_object(
			'workflow_role', 'parent_orchestrator',
			'execution_expected', false
		)
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue as execution-suppressed parent: %v", err)
	}
}

func updateIssueStatusForParentSuppressionTest(t *testing.T, issueID, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"status": status,
	}), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// An execution-suppressed parent is an aggregation node, not runnable work.
// Legal parking/active transitions must never create a direct execution, while
// a normal Stage child beneath the same parent keeps the ordinary enqueue rule.
func TestIntegrationParentOrchestratorSuppressesDirectExecutionButStageChildRuns(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	parent := createIssueForTest(t, map[string]any{
		"title":         "execution-suppressed parent",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	markIssueAsExecutionSuppressedParent(t, parent.ID)

	for _, status := range []string{"todo", "in_progress", "blocked", "backlog", "todo"} {
		preview := previewIssueTrigger(t, map[string]any{
			"issue_ids": []string{parent.ID},
			"status":    status,
		})
		if preview.TotalCount != 0 {
			t.Fatalf("parent status=%q previewed executions: %+v", status, preview)
		}
		updateIssueStatusForParentSuppressionTest(t, parent.ID, status)
	}
	if got := taskCountFor(t, parent.ID, agentID); got != 0 {
		t.Fatalf("parent transitions created %d direct tasks, want 0", got)
	}

	stage := createIssueForTest(t, map[string]any{
		"title":           "normal stage child",
		"status":          "backlog",
		"parent_issue_id": parent.ID,
		"stage":           1,
		"assignee_type":   "agent",
		"assignee_id":     agentID,
	})
	stagePreview := previewIssueTrigger(t, map[string]any{
		"issue_ids": []string{stage.ID},
		"status":    "todo",
	})
	if stagePreview.TotalCount != 1 {
		t.Fatalf("normal Stage child preview = %+v, want one execution", stagePreview)
	}
	updateIssueStatusForParentSuppressionTest(t, stage.ID, "todo")
	if got := taskCountFor(t, stage.ID, agentID); got != 1 {
		t.Fatalf("normal Stage child created %d tasks, want 1", got)
	}
	if got := taskCountFor(t, parent.ID, agentID); got != 0 {
		t.Fatalf("Stage child promotion created %d parent tasks, want 0", got)
	}
}

// Comment routing is also execution: neither implicit assignee fallback nor an
// explicit mention may turn an aggregation-only parent into runnable work.
// Explicit targets still receive a structured blocked outcome so the request is
// not a silent no-op; implicit routing remains absent from the response.
func TestIntegrationParentOrchestratorSuppressesCommentExecutions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	assigneeID := createHandlerTestAgent(t, "Suppressed Parent Assignee", nil)
	mentionedID := createHandlerTestAgent(t, "Suppressed Parent Mention", nil)
	squadLeaderID := createHandlerTestAgent(t, "Suppressed Parent Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Suppressed Parent Squad", squadLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "execution-suppressed parent comments", "agent", assigneeID)
	markIssueAsExecutionSuppressedParent(t, issueID)

	plain := "aggregate this note without running the parent"
	plainPreview := previewCommentTriggersForTest(t, issueID, CommentTriggerPreviewRequest{Content: plain})
	requirePreviewAgents(t, plainPreview)
	postCommentForTriggerPreviewTest(t, issueID, map[string]any{"content": plain})
	if got := taskCountFor(t, issueID, assigneeID); got != 0 {
		t.Fatalf("plain parent comment created %d assignee tasks, want 0", got)
	}

	explicit := fmt.Sprintf(
		"[@Mentioned](mention://agent/%s) [@Squad](mention://squad/%s) inspect this parent",
		mentionedID,
		squadID,
	)
	explicitPreview := previewCommentTriggersForTest(t, issueID, CommentTriggerPreviewRequest{Content: explicit})
	requirePreviewAgents(t, explicitPreview)
	if len(explicitPreview.Blocked) != 2 {
		t.Fatalf("explicit parent preview blocked = %+v, want agent and squad outcomes", explicitPreview.Blocked)
	}
	previewByID := make(map[string]CommentTriggerOutcome, len(explicitPreview.Blocked))
	for _, blocked := range explicitPreview.Blocked {
		previewByID[blocked.TargetID] = blocked
	}
	for _, want := range []struct {
		targetType string
		targetID   string
	}{
		{targetType: "agent", targetID: mentionedID},
		{targetType: "squad", targetID: squadID},
	} {
		blocked, ok := previewByID[want.targetID]
		if !ok || blocked.TargetType != want.targetType ||
			blocked.Status != DispatchBlocked || blocked.ReasonCode != ReasonExecutionSuppressed {
			t.Fatalf("explicit parent preview outcome for %s = %+v, want %s blocked/execution_suppressed", want.targetID, blocked, want.targetType)
		}
	}

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": explicit}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var commentResp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&commentResp); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}
	if len(commentResp.TriggerOutcomes) != 2 {
		t.Fatalf("explicit parent trigger_outcomes = %+v, want agent and squad outcomes", commentResp.TriggerOutcomes)
	}
	createdByID := make(map[string]CommentTriggerOutcome, len(commentResp.TriggerOutcomes))
	for _, outcome := range commentResp.TriggerOutcomes {
		createdByID[outcome.TargetID] = outcome
	}
	for _, want := range []struct {
		targetType string
		targetID   string
	}{
		{targetType: "agent", targetID: mentionedID},
		{targetType: "squad", targetID: squadID},
	} {
		blocked, ok := createdByID[want.targetID]
		if !ok || blocked.TargetType != want.targetType ||
			blocked.Status != DispatchBlocked || blocked.ReasonCode != ReasonExecutionSuppressed {
			t.Fatalf("explicit parent create outcome for %s = %+v, want %s blocked/execution_suppressed", want.targetID, blocked, want.targetType)
		}
	}
	if got := taskCountFor(t, issueID, mentionedID); got != 0 {
		t.Fatalf("explicit parent agent mention created %d tasks, want 0", got)
	}
	if got := taskCountFor(t, issueID, squadLeaderID); got != 0 {
		t.Fatalf("explicit parent squad mention created %d leader tasks, want 0", got)
	}
}

// Manual rerun is an explicit execution request too. It must receive the same
// structured suppression reason without creating or cancelling any tasks.
func TestIntegrationParentOrchestratorRerunReportsExecutionSuppressed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	parent := createIssueForTest(t, map[string]any{
		"title":         "execution-suppressed parent rerun",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	markIssueAsExecutionSuppressedParent(t, parent.ID)

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+parent.ID+"/rerun", map[string]any{}), "id", parent.ID)
	testHandler.RerunIssue(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("RerunIssue: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if code := readReasonCode(t, w.Body.Bytes()); code != string(ReasonExecutionSuppressed) {
		t.Fatalf("RerunIssue reason_code = %q, want execution_suppressed", code)
	}
	if got := taskCountFor(t, parent.ID, agentID); got != 0 {
		t.Fatalf("suppressed rerun created %d tasks, want 0", got)
	}
}

// Stage completion remains visible on the parent timeline, but its system
// comment is aggregation evidence only and must not wake the parent assignee.
func TestIntegrationParentOrchestratorChildDoneCommentDoesNotExecuteParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newChildDoneFixture(t, "in_progress")
	agentID := seededReadyAgentID(t)
	setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
	markIssueAsExecutionSuppressedParent(t, fx.parent.ID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.parent.ID)
	})

	updateChildStatus(t, fx.child.ID, "done")

	content := parentSystemCommentContent(t, fx.parent.ID)
	if content == "" {
		t.Fatal("expected Stage completion evidence on parent timeline")
	}
	if got := countPendingTasksForAgent(t, fx.parent.ID, agentID); got != 0 {
		t.Fatalf("child-done system comment created %d parent tasks, want 0", got)
	}
}
