package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// squadSelfLoopFixture builds a squad-assigned backlog issue whose leader
// holds a running task on that very issue, and returns (issueID, leaderID,
// taskID). The squad is inserted directly and the assignee set by SQL — the
// CreateIssue write path has its own squad-assign permission gate that is not
// under test here.
func squadSelfLoopFixture(t *testing.T) (string, string, string) {
	t.Helper()
	leaderID := seededReadyAgentID(t)

	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'selfloop-squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	issue := createIssueForTest(t, map[string]any{
		"title": "selfloop squad", "status": "backlog",
	})
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue SET assignee_type = 'squad', assignee_id = $1 WHERE id = $2
	`, squadID, issue.ID); err != nil {
		t.Fatalf("assign squad: %v", err)
	}

	// The leader's in-flight run on this very issue: the leader promoting its
	// own squad's issue out of backlog is the genuine self-loop shape.
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT runtime_id FROM agent WHERE id = $1
	`, leaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	taskID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (id, agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, $3, 'running', $4)
	`, taskID, leaderID, issue.ID, runtimeID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return issue.ID, leaderID, taskID
}

// TestStatusWriteBySquadLeaderRunningOnIssueStaysGuarded pins the squad half
// of the wake-target guard: for a squad-assigned issue the wake target is the
// squad LEADER, not the squad id sitting in issue.AssigneeID. Comparing the
// actor against the squad id can never match, so the guard would be skipped
// entirely and a leader promoting its own squad's issue from inside its own
// running task would enqueue a second leader run (hasPendingRun does not see
// running tasks).
func TestStatusWriteBySquadLeaderRunningOnIssueStaysGuarded(t *testing.T) {
	issueID, leaderID, taskID := squadSelfLoopFixture(t)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "todo"}), "id", issueID)
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Agent-ID", leaderID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var leaderNew int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
	`, issueID, leaderID).Scan(&leaderNew); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderNew != 0 {
		t.Fatalf("leader has %d new runs for its own squad's issue, want 0 — the guard must block a leader self-wake", leaderNew)
	}
}

// TestPreviewTriggerSquadLeaderRunningOnIssueSeesNoRun mirrors the write-path
// squad guard on the preview surface: the preview must not offer the leader a
// run the write path would refuse (MUL-3375 — the two probes must not drift).
func TestPreviewTriggerSquadLeaderRunningOnIssueSeesNoRun(t *testing.T) {
	issueID, leaderID, taskID := squadSelfLoopFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/preview-trigger?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{issueID},
		"status":    "todo",
	})
	req.Header.Set("X-Agent-ID", leaderID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.PreviewIssueTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewIssueTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if resp.TotalCount != 0 || len(resp.Triggers) != 0 {
		t.Fatalf("preview must not offer the leader a run on its own squad's issue while its task is running on it, got %+v", resp)
	}
}

// TestStatusWriteBySquadMemberHandsSquadIssueToLeader locks the other half of
// the wake-target semantics: an actor that is NOT the wake target (here a
// plain squad member, while the wake target is the leader) must not be
// guarded — their write hands the issue to the squad and the leader's run
// must start.
func TestStatusWriteBySquadMemberHandsSquadIssueToLeader(t *testing.T) {
	issueID, leaderID, _ := squadSelfLoopFixture(t)

	// A second agent as the acting member.
	memberID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent (id, workspace_id, name, runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode)
		SELECT $1, workspace_id, 'selfloop-squad-member', runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode
		FROM agent WHERE id = $2
	`, memberID, leaderID); err != nil {
		t.Fatalf("insert member agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, memberID)
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "todo"}), "id", issueID)
	req.Header.Set("X-Agent-ID", memberID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Count only fresh enqueues — the fixture's running task row is expected
	// to still be there.
	var leaderNew int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
	`, issueID, leaderID).Scan(&leaderNew); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderNew != 1 {
		t.Fatalf("leader has %d new runs for the issue, want 1 — a non-target actor's write must still wake the squad leader", leaderNew)
	}
}
