package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestListWorkspaceAgentTaskSnapshot covers the agent presence snapshot endpoint:
// every active task (queued/dispatched/running) PLUS each agent's most recent
// OUTCOME task (completed/failed only). Cancelled tasks are excluded by design
// from the outcome half — they're a procedural signal, not an outcome, and
// must NOT mask a prior failure.
//
// The fixtures cover every branch the SQL must classify:
//   - actives are always returned, no dedup
//   - outcomes are deduped to "latest per agent" by completed_at
//   - the OLD 2-minute window must be irrelevant (a 5-minute-old failure is
//     still returned if it's the latest outcome)
//   - cancelled rows are NEVER returned, even when they are temporally newer
//     than a failure — this is what keeps the failed signal sticky after the
//     user cancels their queued retry
func TestListWorkspaceAgentTaskSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	// Three agents so we can verify per-agent semantics independently.
	agentA := createHandlerTestAgent(t, "snapshot-agent-a", []byte(`{}`))
	agentB := createHandlerTestAgent(t, "snapshot-agent-b", []byte(`{}`))
	agentC := createHandlerTestAgent(t, "snapshot-agent-c", []byte(`{}`))

	type taskFixture struct {
		agentID     string
		status      string
		completedAt string // SQL expression; "" for NULL
		label       string
	}
	fixtures := []taskFixture{
		// Agent A — actives + a newer completed supersedes an older failed.
		{agentA, "queued", "", "A.queued"},
		{agentA, "dispatched", "", "A.dispatched"},
		{agentA, "running", "", "A.running"},
		{agentA, "failed", "now() - interval '10 minutes'", "A.old_failed"},
		{agentA, "completed", "now() - interval '30 seconds'", "A.latest_completed"},

		// Agent B — old failure with no later outcome stays visible (no
		// time window).
		{agentB, "failed", "now() - interval '5 minutes'", "B.stale_failed_kept"},

		// Agent C — failure followed by a NEWER cancelled. The cancelled
		// must be skipped by the SQL filter so the failure remains visible.
		// This is the scenario where a user fails, then cancels their
		// queued retry to debug.
		{agentC, "failed", "now() - interval '5 minutes'", "C.failure"},
		{agentC, "cancelled", "now() - interval '30 seconds'", "C.newer_cancelled_must_be_ignored"},
	}

	insertedIDs := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		var id string
		var query string
		if f.completedAt == "" {
			query = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
			         VALUES ($1, $2, $3, 0) RETURNING id`
		} else {
			query = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, completed_at)
			         VALUES ($1, $2, $3, 0, ` + f.completedAt + `) RETURNING id`
		}
		if err := testPool.QueryRow(ctx, query, f.agentID, testRuntimeID, f.status).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", f.label, err)
		}
		insertedIDs = append(insertedIDs, id)
	}
	t.Cleanup(func() {
		for _, id := range insertedIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id)
		}
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/agent-task-snapshot", nil)
	testHandler.ListWorkspaceAgentTaskSnapshot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceAgentTaskSnapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tasks []AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Per-agent breakdown so leftover tasks from other tests in this package
	// don't pollute the assertions.
	type key struct{ agent, status string }
	counts := map[key]int{}
	for _, task := range tasks {
		if task.AgentID != agentA && task.AgentID != agentB && task.AgentID != agentC {
			continue
		}
		counts[key{task.AgentID, task.Status}]++
	}

	wantCounts := map[key]int{
		// Agent A: 3 actives + the latest outcome (completed). The older
		// failed must be excluded by DISTINCT ON.
		{agentA, "queued"}:     1,
		{agentA, "dispatched"}: 1,
		{agentA, "running"}:    1,
		{agentA, "completed"}:  1,
		// Agent B: just the failed outcome.
		{agentB, "failed"}: 1,
		// Agent C: the failed outcome must survive the temporally newer
		// cancellation — that's the whole point of excluding cancelled
		// from the outcome half.
		{agentC, "failed"}: 1,
	}
	for k, expected := range wantCounts {
		if got := counts[k]; got != expected {
			t.Errorf("agent=%s status=%s: expected %d, got %d", k.agent, k.status, expected, got)
		}
	}

	// The OLD failed terminal on agent A must be excluded.
	if counts[key{agentA, "failed"}] != 0 {
		t.Errorf("agent A old failed must be superseded by newer completed; got %d", counts[key{agentA, "failed"}])
	}

	// No cancelled row may ever appear in the snapshot — they're filtered at
	// SQL level so the front-end's "cancel doesn't mask failure" rule lands
	// without any front-end logic.
	for _, agentID := range []string{agentA, agentB, agentC} {
		if counts[key{agentID, "cancelled"}] != 0 {
			t.Errorf("agent %s: cancelled rows must be excluded from snapshot; got %d",
				agentID, counts[key{agentID, "cancelled"}])
		}
	}
}

// CEREBRO-PATCH(list-agents-visibility-split): JEH-1066 — ListAgents must
// return every agent in the workspace to a plain member, even agents whose
// trigger gate denies them. The cerebro group allowlist is surfaced via the
// new `can_trigger` flag rather than filtering rows out — that's the whole
// point of the visibility/trigger split (replaces the older filter-skip
// behavior the JEH-1009 PR 4 tests would have asserted).
//
// This test also pins the inverse: for an allowed agent, `can_trigger` is
// true. Together those two cases describe the contract the picker UI relies
// on (lock icon iff !can_trigger).
func TestListAgents_VisibilitySplit_MemberSeesAllWithCanTrigger(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Agent A is "denied"; agent B is "allowed" — the stub flips the answer
	// based on which UUID the gate is queried for.
	agentA := createHandlerTestAgent(t, "visibility-split-denied", []byte(`{}`))
	agentB := createHandlerTestAgent(t, "visibility-split-allowed", []byte(`{}`))

	// Demote the test user to "member" so the cerebro seam is consulted
	// (admins bypass the gate inside cerebroVisibleAgentIDSet).
	if _, err := testPool.Exec(ctx,
		`UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, testUserID,
	); err != nil {
		t.Fatalf("demote to member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, testUserID,
		)
	})

	// Wire a stub group-permissions invoker: VisibleAgentIDs grants only
	// agent B; CanCreateAgent returns false so the owner-exemption does NOT
	// fire and we can isolate the visibility behavior of the new code path.
	prev := testHandler.GroupPermissions
	stub := &stubGroupPermissions{
		resolve: func(context.Context, pgtype.UUID, pgtype.UUID) ([]pgtype.UUID, error) {
			return nil, nil
		},
		canAG: func(context.Context, GroupPermissionsViewer, pgtype.UUID) (bool, error) {
			return false, nil
		},
		visAgents: func(context.Context, GroupPermissionsViewer, pgtype.UUID) ([]pgtype.UUID, error) {
			return []pgtype.UUID{parseUUID(agentB)}, nil
		},
	}
	testHandler.GroupPermissions = stub
	t.Cleanup(func() { testHandler.GroupPermissions = prev })

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agents []AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byID := map[string]AgentResponse{}
	for _, a := range agents {
		byID[a.ID] = a
	}

	gotA, okA := byID[agentA]
	if !okA {
		t.Fatalf("agent A (denied) must still appear in the list — the visibility/trigger split means we never hide rows")
	}
	if gotA.CanTrigger {
		t.Errorf("agent A: expected can_trigger=false (not in allowlist, owner-exemption disabled), got true")
	}

	gotB, okB := byID[agentB]
	if !okB {
		t.Fatalf("agent B (allowed) missing from list")
	}
	if !gotB.CanTrigger {
		t.Errorf("agent B: expected can_trigger=true (in allowlist), got false")
	}
}

func TestCreateAgent_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Clean up any agents created by this test.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "duplicate-name-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "duplicate-name-test-agent",
		"description":          "first description",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	// First call — creates the agent.
	w1 := httptest.NewRecorder()
	testHandler.CreateAgent(w1, newRequest(http.MethodPost, "/api/agents", body))
	if w1.Code != http.StatusCreated {
		t.Fatalf("first CreateAgent: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	agentID1, _ := resp1["id"].(string)
	if agentID1 == "" {
		t.Fatalf("first CreateAgent: no id in response: %v", resp1)
	}

	// Second call — same name must be rejected with 409 Conflict.
	// The unique constraint prevents silent duplicates; the UI shows a clear error.
	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusConflict {
		t.Fatalf("second CreateAgent with duplicate name: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}
