package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type runningSquadLeaderTaskFixture struct {
	IssueID          string
	LeaderID         string
	TaskID           string
	TriggerCommentID string
}

func newRunningSquadLeaderTaskFixture(t *testing.T) runningSquadLeaderTaskFixture {
	t.Helper()
	ctx := context.Background()

	fx := newSquadCommentTriggerFixture(t)
	issueID := uuidToString(fx.Issue.ID)

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id FROM agent WHERE id = $1
	`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'LGTM', 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("create trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, trigger_comment_id,
			status, priority, started_at
		)
		VALUES ($1, $2, $3, $4, 'running', 0, now())
		RETURNING id
	`, fx.LeaderID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("create running squad leader task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	return runningSquadLeaderTaskFixture{
		IssueID:          issueID,
		LeaderID:         fx.LeaderID,
		TaskID:           taskID,
		TriggerCommentID: triggerCommentID,
	}
}

func recordSquadLeaderEvaluationForTask(t *testing.T, fx runningSquadLeaderTaskFixture, outcome string) {
	t.Helper()
	recordSquadLeaderEvaluationForTaskWithHeader(t, fx, outcome, fx.TaskID)
}

func recordSquadLeaderEvaluationForTaskWithHeader(t *testing.T, fx runningSquadLeaderTaskFixture, outcome, taskIDHeader string) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/squad-evaluated", map[string]any{
		"outcome": outcome,
		"reason":  "test reason",
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", taskIDHeader)

	testHandler.RecordSquadLeaderEvaluation(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("RecordSquadLeaderEvaluation: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func completeRunningTask(t *testing.T, fx runningSquadLeaderTaskFixture, output string) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+fx.TaskID+"/complete",
		map[string]any{"output": output},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", fx.TaskID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func countAgentCommentsForIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
	`, issueID, agentID).Scan(&count); err != nil {
		t.Fatalf("count agent comments: %v", err)
	}
	return count
}

func TestCompleteTask_SquadLeaderNoActionDoesNotSynthesizeComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)
	recordSquadLeaderEvaluationForTask(t, fx, "no_action")

	completeRunningTask(t, fx, "No action needed. Exiting silently.")

	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 0 {
		t.Fatalf("expected no squad leader comment after no_action completion, got %d", got)
	}
}

func TestCompleteTask_SquadLeaderNoActionCanonicalizesTaskID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)
	recordSquadLeaderEvaluationForTaskWithHeader(t, fx, "no_action", strings.ToUpper(fx.TaskID))

	completeRunningTask(t, fx, "No action needed. Exiting silently.")

	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 0 {
		t.Fatalf("expected no comment when no_action was recorded with uppercase task id header, got %d", got)
	}
}

func TestCompleteTask_SquadLeaderActionStillSynthesizesComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)
	recordSquadLeaderEvaluationForTask(t, fx, "action")

	completeRunningTask(t, fx, "Delegated the review.")

	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 1 {
		t.Fatalf("expected action completion to synthesize one comment, got %d", got)
	}
}

func TestCreateComment_SquadLeaderNoActionRejectsComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderTaskFixture(t)
	recordSquadLeaderEvaluationForTask(t, fx, "no_action")

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+fx.IssueID+"/comments", map[string]any{
		"content":   "No action needed.",
		"parent_id": fx.TriggerCommentID,
	})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateComment: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := countAgentCommentsForIssue(t, fx.IssueID, fx.LeaderID); got != 0 {
		t.Fatalf("expected rejected no_action comment not to be stored, got %d", got)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["error"] == "" {
		t.Fatalf("expected error message in response, got %v", body)
	}
}
