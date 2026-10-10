package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createRoutingTestSquad seeds a pge-core-squad led by the workspace's first
// agent and returns its id. The convention name is what the default-assignee
// resolver falls back to when no settings.default_core_squad_id is set.
func createRoutingTestSquad(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })
	return squadID
}

// createIssueViaHandler posts an issue to /api/issues with the given body and
// returns the decoded create response + HTTP status.
func createIssueViaHandler(t *testing.T, body map[string]any) (*IssueResponse, int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		return nil, w.Code
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})
	return &created, w.Code
}

// TestCreateIssueDefaultsCoreTaskToCoreSquad verifies the RIC-1024 default
// assignee routing: a core-development task created without an assignee is
// assigned to the workspace's pge-core-squad.
func TestCreateIssueDefaultsCoreTaskToCoreSquad(t *testing.T) {
	squadID := createRoutingTestSquad(t, "pge-core-squad")

	created, code := createIssueViaHandler(t, map[string]any{
		"title": "Implement the new auth flow",
	})
	if code != http.StatusCreated {
		t.Fatalf("create core issue: expected 201, got %d", code)
	}
	if created.AssigneeType == nil || *created.AssigneeType != "squad" {
		t.Fatalf("core task assignee_type = %v, want squad", created.AssigneeType)
	}
	if created.AssigneeID == nil || *created.AssigneeID != squadID {
		t.Fatalf("core task assignee_id = %v, want %s", created.AssigneeID, squadID)
	}
}

// TestCreateIssueKeepsOpsTaskFastTrack verifies an ops/inspection issue created
// without an assignee stays unassigned (single-agent fast track).
func TestCreateIssueKeepsOpsTaskFastTrack(t *testing.T) {
	createRoutingTestSquad(t, "pge-core-squad")

	created, code := createIssueViaHandler(t, map[string]any{
		"title": "Daily monitoring report for the API health",
	})
	if code != http.StatusCreated {
		t.Fatalf("create ops issue: expected 201, got %d", code)
	}
	if created.AssigneeType != nil || created.AssigneeID != nil {
		t.Fatalf("ops task got default assignee (type=%v id=%v), want unassigned", created.AssigneeType, created.AssigneeID)
	}
}

// TestCreateIssueExplicitAssigneeWinsOverDefault verifies an explicitly named
// assignee always beats the squad default.
func TestCreateIssueExplicitAssigneeWinsOverDefault(t *testing.T) {
	squadID := createRoutingTestSquad(t, "pge-core-squad")

	// Build the body with an explicit (validated) agent assignee — reuse the
	// seeded leader so the pair resolves.
	var leaderID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	created, code := createIssueViaHandler(t, map[string]any{
		"title":         "Implement the new auth flow",
		"assignee_type": "agent",
		"assignee_id":   leaderID,
	})
	if code != http.StatusCreated {
		t.Fatalf("create explicit-issue: expected 201, got %d", code)
	}
	if created.AssigneeType == nil || *created.AssigneeType != "agent" {
		t.Fatalf("explicit assignee_type = %v, want agent", created.AssigneeType)
	}
	if created.AssigneeID == nil || *created.AssigneeID != leaderID {
		t.Fatalf("explicit assignee_id = %v, want %s", created.AssigneeID, leaderID)
	}
	_ = squadID
}
