package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createLeaderAssignRequest builds a CreateIssue request made by agentID while
// its running task taskID is the actor, the way a squad leader calls the CLI.
// The task carries a human originator, so the invoke gate sees a real chain.
func createLeaderAssignRequest(t *testing.T, agentID, taskID, assigneeID string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Sub-issue from " + agentID + " to " + assigneeID,
		"assignee_type": "agent",
		"assignee_id":   assigneeID,
	})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, req)
	if w.Code == http.StatusCreated {
		var created IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		t.Cleanup(func() {
			cleanup := withURLParam(newRequest("DELETE", "/api/issues/"+created.ID, nil), "id", created.ID)
			testHandler.DeleteIssue(httptest.NewRecorder(), cleanup)
		})
	}
	return w
}

// TestSquadLeaderAssignsOnlyWithinItsSquad pins the boundary: a leader agent
// may assign an issue to a member of its squad, to itself, but not to an
// agent outside the squads it leads. An agent that leads no squad is not
// limited by this gate.
func TestSquadLeaderAssignsOnlyWithinItsSquad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Leader assign runtime")
	leaderID := seedAgentOnRuntime(t, runtimeID, "Leader assign lead", false)
	memberID := seedAgentOnRuntime(t, runtimeID, "Leader assign member", false)
	outsiderID := seedAgentOnRuntime(t, runtimeID, "Leader assign outsider", false)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Leader assign squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, 'agent', $2, 'member')
	`, squadID, memberID); err != nil {
		t.Fatalf("add squad member: %v", err)
	}

	// Each actor runs a task whose originator is the workspace's test user, so
	// the existing invoke gate admits the assignment and only the squad check
	// decides the outcome.
	leaderTaskID := seedLeaderRunningTask(t, ctx, leaderID, runtimeID)
	outsiderTaskID := seedLeaderRunningTask(t, ctx, outsiderID, runtimeID)

	if w := createLeaderAssignRequest(t, leaderID, leaderTaskID, outsiderID); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "only to agents in its squad") {
		t.Fatalf("leader assigning to an agent outside its squad: want 403 from the squad gate, got %d: %s", w.Code, w.Body.String())
	}
	if w := createLeaderAssignRequest(t, leaderID, leaderTaskID, memberID); w.Code != http.StatusCreated {
		t.Fatalf("leader assigning to a squad member: want 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := createLeaderAssignRequest(t, leaderID, leaderTaskID, leaderID); w.Code != http.StatusCreated {
		t.Fatalf("leader assigning to itself: want 201, got %d: %s", w.Code, w.Body.String())
	}
	// The outsider leads no squad, so the squad gate does not limit it.
	if w := createLeaderAssignRequest(t, outsiderID, outsiderTaskID, leaderID); w.Code != http.StatusCreated {
		t.Fatalf("agent that leads no squad: want 201, got %d: %s", w.Code, w.Body.String())
	}
}

// seedLeaderRunningTask inserts a running task for agentID with the test user
// as originator, and returns its id.
func seedLeaderRunningTask(t *testing.T, ctx context.Context, agentID, runtimeID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority,
			originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, $2, 'running', 0, $3, $3, 'trigger_owner') RETURNING id
	`, agentID, runtimeID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}
