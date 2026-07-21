package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateAutopilot_SquadPrivateLeader_PlainMemberBlocked verifies that a
// plain member cannot create an autopilot assigned to a squad whose leader
// is a private agent.
func TestCreateAutopilot_SquadPrivateLeader_PlainMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP Private Leader Create', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "should be blocked",
		"assignee_type":  "squad",
		"assignee_id":    squadID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAutopilot_SquadPrivateLeader_PlainMemberBlocked verifies that a
// plain member cannot update an autopilot to point at a private-leader squad.
func TestUpdateAutopilot_SquadPrivateLeader_PlainMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	// Create a non-private agent for the initial autopilot.
	publicAgentID := createHandlerTestAgent(t, "ap-private-leader-public", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP Private Leader Update', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create autopilot as workspace owner assigned to the public agent.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "update target ap",
		"assignee_id":    publicAgentID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ap AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&ap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	// Plain member tries to update to the private-leader squad.
	squadType := "squad"
	w = httptest.NewRecorder()
	r = newRequestAs(memberID, "PATCH", "/api/autopilots/"+ap.ID+"?workspace_id="+testWorkspaceID, map[string]any{
		"assignee_type": squadType,
		"assignee_id":   squadID,
	})
	r = withURLParam(r, "id", ap.ID)
	testHandler.UpdateAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAutopilot_SquadPrivateLeader_OwnerAllowed verifies that a
// workspace owner CAN create an autopilot assigned to a private-leader squad.
func TestCreateAutopilot_SquadPrivateLeader_OwnerAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP Private Leader Owner', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// The AGENT OWNER creates the autopilot — allowed under MUL-3963 (workspace
	// owner/admin no longer bypasses a private leader's invocation gate).
	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "owner creates private-leader squad ap",
		"assignee_type":  "squad",
		"assignee_id":    squadID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ap AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&ap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})
}

// TestTriggerAutopilot_SquadPrivateLeader_OwnerCanDispatch verifies that a
// squad autopilot with private leader configured by an owner triggers
// correctly at dispatch time.
func TestTriggerAutopilot_SquadPrivateLeader_OwnerCanDispatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP Private Leader Dispatch', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create autopilot as the AGENT OWNER (MUL-3963: only owner/allow-listed
	// may invoke the private leader; workspace admin no longer bypasses).
	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "dispatch test private leader squad",
		"assignee_type":  "squad",
		"assignee_id":    squadID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ap AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&ap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch test private leader squad%')`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'dispatch test private leader squad%'`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	// Trigger AS THE OWNER — manual "run now" admits on the current clicker's
	// invoke permission (MUL-4525), so the owner (who can invoke the private
	// leader) must be the one clicking. A non-owner clicker is covered by
	// TestTriggerAutopilot_SquadPrivateLeader_NonOwnerClicker_Blocked below.
	w = httptest.NewRecorder()
	r = newRequestAs(ownerID, "POST", "/api/autopilots/"+ap.ID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", ap.ID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("TriggerAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var run AutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Status != "issue_created" {
		t.Fatalf("run status = %q, want issue_created", run.Status)
	}
}

// TestTriggerAutopilot_SquadPrivateLeader_NonOwnerClicker_Blocked pins the
// MUL-4525 fork fix: manual "run now" admits on the CURRENT clicker, not the
// autopilot creator. Even for an autopilot the OWNER created (so the creator
// could invoke), a different member clicking Run now who cannot invoke the
// private leader is blocked — surfaced as a 200 + status=skipped run carrying a
// stable, enumeration-safe reason_code (not a silent success). This is the exact
// case where the old creator-based admission and clicker-based attribution
// forked.
func TestTriggerAutopilot_SquadPrivateLeader_NonOwnerClicker_Blocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP Private Leader Clicker Fork', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Owner creates a legitimate autopilot (creator CAN invoke the leader).
	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "clicker fork private leader squad",
		"assignee_type":  "squad",
		"assignee_id":    squadID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ap AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&ap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'clicker fork private leader squad%'`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	// The workspace owner (testUserID) — NOT the private agent's owner — clicks
	// Run now. requireAutopilotWrite passes (workspace owner can manage), but the
	// invoke gate keys on the clicker and denies them.
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/autopilots/"+ap.ID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", ap.ID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("TriggerAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var run AutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Status != "skipped" {
		t.Fatalf("run status = %q, want skipped (non-owner clicker blocked)", run.Status)
	}
	if run.ReasonCode == nil || *run.ReasonCode != string(ReasonInvocationNotAllowed) {
		got := "<nil>"
		if run.ReasonCode != nil {
			got = *run.ReasonCode
		}
		t.Fatalf("reason_code = %s, want %s", got, ReasonInvocationNotAllowed)
	}
}

// TestTriggerAutopilot_SquadPrivateLeader_PlainMemberCreator_Blocked verifies
// that if an autopilot pointing to a private-leader squad was somehow saved
// by a plain member (legacy data), dispatch is blocked at runtime.
func TestTriggerAutopilot_SquadPrivateLeader_PlainMemberCreator_Blocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP Private Leader Blocked Dispatch', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Directly insert an autopilot with the plain member as creator
	// (simulating legacy data before the save-time gate).
	var apID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id,
		                       execution_mode, created_by_type, created_by_id, status)
		VALUES ($1, 'legacy illegal ap', 'squad', $2, 'create_issue', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, squadID, memberID).Scan(&apID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, apID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, apID)
	})

	// Trigger as workspace owner — the dispatch should fail because the
	// autopilot's creator (plain member) cannot access the private leader.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	// Dispatch returns 200 with status=skipped (or failed) — the run is created
	// but the dispatch is blocked by the private-leader gate.
	if w.Code != http.StatusOK {
		t.Fatalf("TriggerAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var run AutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	// The dispatch-time gate should cause a skipped or failed run.
	if run.Status == "issue_created" || run.Status == "running" {
		t.Fatalf("run status = %q; want skipped/failed since creator is plain member", run.Status)
	}
}

// TestTriggerAutopilot_RunOnly_SquadPrivateLeader_PlainMemberCreator_Blocked
// mirrors the create_issue dispatch test above but exercises the run_only
// dispatch path (dispatchRunOnly), ensuring both dispatch branches gate
// private-leader access.
func TestTriggerAutopilot_RunOnly_SquadPrivateLeader_PlainMemberCreator_Blocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'AP RunOnly Private Leader Blocked', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Legacy autopilot: run_only mode, plain member creator, private-leader squad.
	var apID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id,
		                       execution_mode, created_by_type, created_by_id, status)
		VALUES ($1, 'legacy run_only illegal ap', 'squad', $2, 'run_only', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, squadID, memberID).Scan(&apID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, apID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, apID)
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("TriggerAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var run AutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Status == "running" {
		t.Fatalf("run status = %q; want skipped/failed since creator is plain member and leader is private", run.Status)
	}
}
