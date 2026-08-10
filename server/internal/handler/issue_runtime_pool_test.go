package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestQuickCreateRuntimePreflightByBinding(t *testing.T) {
	tests := []struct {
		name         string
		bindingMode  string
		runtimeValid bool
		want         bool
	}{
		{name: "Pool unbound uses scheduler gate", bindingMode: "pool", want: false},
		{name: "Pool bound still uses scheduler gate", bindingMode: "pool", runtimeValid: true, want: false},
		{name: "fixed bound keeps legacy preflight", bindingMode: "fixed", runtimeValid: true, want: true},
		{name: "fixed unbound keeps legacy 422", bindingMode: "fixed", want: true},
		{name: "unknown mode fails closed", bindingMode: "future", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := db.Agent{
				RuntimeBindingMode: tt.bindingMode,
				RuntimeID:          pgtype.UUID{Valid: tt.runtimeValid},
			}
			if got := quickCreateRequiresFixedRuntimePreflight(agent); got != tt.want {
				t.Fatalf("quickCreateRequiresFixedRuntimePreflight() = %v, want %v", got, tt.want)
			}
		})
	}
}

func createRuntimePoolRerunIssue(t *testing.T, ctx context.Context, agentID, title string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			number, position, assignee_type, assignee_id
		)
		SELECT $1, $2, 'in_progress', 'none', 'member', $3,
		       COALESCE(MAX(number), 0) + 1, 0, 'agent', $4
		FROM issue
		WHERE workspace_id = $1
		RETURNING id
	`, testWorkspaceID, title, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create rerun Issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func TestRerunFixedFreshSessionReturns400(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET owner_id = $1 WHERE id = $2
	`, testUserID, agentID); err != nil {
		t.Fatalf("make fixed Agent invokable: %v", err)
	}
	issueID := createRuntimePoolRerunIssue(t, ctx, agentID, "Fixed fresh rerun is rejected")

	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create fixed source Task: %v", err)
	}

	var beforeCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&beforeCount); err != nil {
		t.Fatalf("count fixed Tasks before rerun: %v", err)
	}
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/rerun", map[string]any{
		"task_id":       sourceTaskID,
		"fresh_session": true,
	}), "id", issueID)
	testHandler.RerunIssue(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("RerunIssue fixed fresh: status=%d body=%s, want 400", w.Code, w.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode fixed fresh error: %v", err)
	}
	if body.Code != "FRESH_SESSION_REQUIRES_POOL" {
		t.Fatalf("error code = %q, want FRESH_SESSION_REQUIRES_POOL", body.Code)
	}
	var sourceStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, sourceTaskID).Scan(&sourceStatus); err != nil {
		t.Fatalf("read fixed source Task: %v", err)
	}
	if sourceStatus != "queued" {
		t.Fatalf("source status = %q, want queued (validation must precede cancellation)", sourceStatus)
	}
	var afterCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&afterCount); err != nil {
		t.Fatalf("count fixed Tasks after rerun: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("fixed fresh changed Task count: before=%d after=%d", beforeCount, afterCount)
	}
}

func TestClaimTaskExplicitFreshSessionWinsExactRerun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime
		SET status = 'online', last_seen_at = now(), visibility = 'public', owner_id = $1
		WHERE id = $2
	`, testUserID, runtimeID); err != nil {
		t.Fatalf("make claim Runtime eligible: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent
		SET runtime_id = NULL,
		    runtime_mode = 'pool',
		    runtime_binding_mode = 'pool',
		    runtime_requirements = '{}'::jsonb
		WHERE id = $1
	`, agentID); err != nil {
		t.Fatalf("convert claim Agent to Pool: %v", err)
	}
	issueID := createRuntimePoolRerunIssue(t, ctx, agentID, "Explicit fresh beats exact rerun")

	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, completed_at,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, session_affinity_state,
			session_affinity_runtime_id, session_id, work_dir
		) VALUES (
			$1, $2, $3, 'completed', now(), 'pool', '{}'::jsonb, $4, $5,
			'pinned', $3, 'source-session', '/tmp/source-workdir'
		)
		RETURNING id
	`, agentID, issueID, runtimeID, testWorkspaceID, testUserID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create Pool rerun source: %v", err)
	}
	var freshTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, priority,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, session_affinity_state,
			rerun_of_task_id, force_fresh_session, explicit_fresh_session
		) VALUES (
			$1, $2, $3, 'queued', 0, 'pool', '{}'::jsonb, $4, $5, 'none',
			$6, TRUE, TRUE
		)
		RETURNING id
	`, agentID, issueID, runtimeID, testWorkspaceID, testUserID, sourceTaskID).Scan(&freshTaskID); err != nil {
		t.Fatalf("create explicit-fresh Pool rerun: %v", err)
	}

	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(freshTaskID))
	if err != nil {
		t.Fatalf("load explicit-fresh Task: %v", err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("load claim Runtime: %v", err)
	}
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
	claimed, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &task, runtime, runtimeID, testWorkspaceID)
	if failure != nil {
		t.Fatalf("build explicit-fresh claim response: %+v", failure)
	}
	if claimed.PriorSessionID != "" || claimed.PriorWorkDir != "" {
		t.Fatalf("explicit fresh claim returned PriorSessionID/WorkDir=%q/%q, want both empty",
			claimed.PriorSessionID, claimed.PriorWorkDir)
	}
}
