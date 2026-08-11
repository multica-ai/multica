package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPoolSquadWaitingTaskReportsWorking(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{status: "waiting_runtime", want: true},
		{status: "dispatched", want: true},
		{status: "running", want: true},
		{status: "waiting_local_directory", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			if got := isSquadWorkingTaskStatus(tc.status); got != tc.want {
				t.Fatalf("isSquadWorkingTaskStatus(%q) = %t, want %t", tc.status, got, tc.want)
			}
		})
	}
}

// TestCreateIssueAssignedToPoolSquadEnqueuesWaitingLeader catches the native
// Squad admission path falling back to fixed-only AgentReadiness. A Pool
// leader is routable without agent.runtime_id and must reach the shared Pool
// factory as a waiting_runtime Task.
func TestCreateIssueAssignedToPoolSquadEnqueuesWaitingLeader(t *testing.T) {
	ctx := context.Background()

	var leaderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_binding_mode,
			runtime_requirements, visibility, status, owner_id
		)
		VALUES (
			$1, 'Pool Squad Leader', 'pool', 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]}'::jsonb,
			'workspace', 'offline', $2
		)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&leaderID); err != nil {
		t.Fatalf("create Pool leader: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, leaderID)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Pool Trigger Test Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create Pool squad: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Pool squad assigned at creation",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	}()

	var status, bindingMode, affinityState, waitReason string
	var runtimeID *string
	if err := testPool.QueryRow(ctx, `
		SELECT status, runtime_binding_mode, runtime_id::text,
		       session_affinity_state, wait_reason
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, created.ID, leaderID).Scan(&status, &bindingMode, &runtimeID, &affinityState, &waitReason); err != nil {
		t.Fatalf("load Pool leader task: %v", err)
	}
	if status != "waiting_runtime" || bindingMode != "pool" || runtimeID != nil ||
		affinityState != "none" || waitReason != "no_eligible_runtime" {
		t.Fatalf("Pool leader routing = status=%s binding=%s runtime=%v affinity=%s reason=%s",
			status, bindingMode, runtimeID, affinityState, waitReason)
	}
}
