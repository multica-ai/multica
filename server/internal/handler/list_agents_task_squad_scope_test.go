package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// leaderTaskScopeFixture seeds the world for the `scope=task_squad` filter:
//
//   - a squad with leader, worker (squad_member), and an outsider agent
//     that is intentionally NOT in the squad
//   - a squad-assigned issue
//   - a running leader-task whose UUID we can send back as X-Task-ID
//
// Reuses crossSquadMentionFixture for the agent layout and adds the running
// is_leader_task row on top. Lives next to the cross-squad mention tests
// (squad_cross_squad_mention_test.go) because the two tests share the same
// invariant — leader stays inside its roster — at different layers.
type leaderTaskScopeFixture struct {
	crossSquadMentionFixture
	TaskID string
}

func newLeaderTaskScopeFixture(t *testing.T, isLeaderTask bool) leaderTaskScopeFixture {
	t.Helper()
	ctx := context.Background()
	base := newCrossSquadMentionFixture(t)

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, base.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, is_leader_task
		)
		VALUES ($1, $2, $3, 'running', 0, now(), $4)
		RETURNING id
	`, base.LeaderID, runtimeID, base.IssueID, isLeaderTask).Scan(&taskID); err != nil {
		t.Fatalf("create leader task (is_leader_task=%v): %v", isLeaderTask, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	return leaderTaskScopeFixture{
		crossSquadMentionFixture: base,
		TaskID:                   taskID,
	}
}

// agentIDSetFromListAgentsResponse extracts the set of agent IDs present in a
// ListAgents JSON response body.
func agentIDSetFromListAgentsResponse(t *testing.T, body []byte) map[string]struct{} {
	t.Helper()
	var resp []AgentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode ListAgents response: %v", err)
	}
	set := make(map[string]struct{}, len(resp))
	for _, a := range resp {
		set[a.ID] = struct{}{}
	}
	return set
}

func newAgentTaskListAgentsRequest(agentID, taskID string, scope string) *http.Request {
	q := ""
	if scope != "" {
		q = "?scope=" + scope
	}
	req := newRequest("GET", "/api/agents"+q, nil)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

// TestListAgents_TaskSquadScope_FiltersToSquadMembers locks in the squad-leader
// scoping path: when an agent actor on a leader task asks for `?scope=task_squad`,
// the response includes the squad leader and squad_member agents, but NOT
// workspace-peers that are outside the squad.
func TestListAgents_TaskSquadScope_FiltersToSquadMembers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newLeaderTaskScopeFixture(t, true)

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newAgentTaskListAgentsRequest(fx.LeaderID, fx.TaskID, "task_squad"))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := agentIDSetFromListAgentsResponse(t, w.Body.Bytes())
	if _, ok := got[fx.LeaderID]; !ok {
		t.Errorf("scoped list missing leader %s — leader fallback expected to keep them visible", fx.LeaderID)
	}
	if _, ok := got[fx.WorkerID]; !ok {
		t.Errorf("scoped list missing squad worker %s", fx.WorkerID)
	}
	if _, ok := got[fx.OutsiderID]; ok {
		t.Errorf("scoped list unexpectedly contains outsider %s — out-of-roster agent leaked through", fx.OutsiderID)
	}
}

// TestListAgents_TaskSquadScope_NoOpForNonLeaderTask proves the scope param is
// safely ignored when the requesting task is not a leader task — workers
// keep their full A2A view of the workspace.
func TestListAgents_TaskSquadScope_NoOpForNonLeaderTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newLeaderTaskScopeFixture(t, false)

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newAgentTaskListAgentsRequest(fx.LeaderID, fx.TaskID, "task_squad"))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := agentIDSetFromListAgentsResponse(t, w.Body.Bytes())
	if _, ok := got[fx.OutsiderID]; !ok {
		t.Errorf("non-leader task with scope=task_squad should keep outsider %s visible (no-op)", fx.OutsiderID)
	}
}

// TestListAgents_TaskSquadScope_FullListWithoutScopeParam confirms the legacy
// behavior — no scope param means the existing A2A-visible list is returned
// unchanged, even on a leader task. This is the path `--all` on the CLI
// takes.
func TestListAgents_TaskSquadScope_FullListWithoutScopeParam(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newLeaderTaskScopeFixture(t, true)

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newAgentTaskListAgentsRequest(fx.LeaderID, fx.TaskID, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := agentIDSetFromListAgentsResponse(t, w.Body.Bytes())
	if _, ok := got[fx.OutsiderID]; !ok {
		t.Errorf("no scope param: outsider %s must remain visible (existing behavior)", fx.OutsiderID)
	}
}
