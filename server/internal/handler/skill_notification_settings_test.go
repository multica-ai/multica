package handler

// CEREBRO-PATCH(skill-notification-settings-test): FIR-1587 coverage for the
// owner-controlled notify toggles, fork notification, and agent-assignment
// notification added on top of the FIR-2627 change-request inbox flow.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// countSkillInbox returns how many inbox_item rows of the given type point at
// the given skill (via details->>'skill_id' or 'parent_skill_id').
func countSkillInbox(t *testing.T, itemType, skillID, recipientID string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND type = $2 AND recipient_id = $3
		  AND ((details->>'skill_id') = $4 OR (details->>'parent_skill_id') = $4)
	`, testWorkspaceID, itemType, recipientID, skillID).Scan(&n); err != nil {
		t.Fatalf("count inbox %s: %v", itemType, err)
	}
	return n
}

// TestChangeRequestNotifyToggleOff verifies that turning notify_change_requests
// off via the ownership endpoint suppresses the owner/approver inbox fan-out.
func TestChangeRequestNotifyToggleOff(t *testing.T) {
	ctx := context.Background()
	f := setupOwnershipFixture(t)

	// Owner turns change-request notifications off for this skill.
	w := httptest.NewRecorder()
	req := newRequestAs(f.ownerID, "PUT", "/api/skills/"+f.skillID+"/ownership", map[string]any{
		"notify_change_requests": false,
	})
	req = withURLParam(req, "id", f.skillID)
	testHandler.UpdateSkillOwnership(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSkillOwnership: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sk SkillResponse
	if err := json.NewDecoder(w.Body).Decode(&sk); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	if sk.NotifyChangeRequests {
		t.Fatal("notify_change_requests should be false after toggle")
	}
	// Owner/approvers unchanged by the partial update.
	if sk.OwnerID == nil || *sk.OwnerID != f.ownerID {
		t.Fatalf("owner must be untouched by toggle update, got %v", sk.OwnerID)
	}

	// Proposer opens a change request — no inbox items should be created.
	w = httptest.NewRecorder()
	req = newRequestAs(f.proposerID, "POST", "/api/skills/"+f.skillID+"/change-requests", map[string]any{
		"title":            "noop",
		"proposed_version": "1.1.0",
		"proposed_content": "line one changed\nline two\nline three\n",
	})
	req = withURLParam(req, "id", f.skillID)
	testHandler.CreateSkillChangeRequest(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSkillChangeRequest: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var cr SkillChangeRequestResponse
	_ = json.NewDecoder(w.Body).Decode(&cr)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM skill_change_request WHERE id = $1`, cr.ID) })

	if got := countSkillInbox(t, "skill_change_request_created", f.skillID, f.ownerID); got != 0 {
		t.Fatalf("expected 0 change-request inbox items with notify off, got %d", got)
	}
}

// TestForkNotifiesParentOwner verifies a fork by a non-owner notifies the
// parent owner, and that notify_forks=false suppresses it.
func TestForkNotifiesParentOwner(t *testing.T) {
	ctx := context.Background()
	f := setupOwnershipFixture(t)

	fork := func(name string) string {
		w := httptest.NewRecorder()
		req := newRequestAs(f.proposerID, "POST", "/api/skills/"+f.skillID+"/forks", map[string]any{
			"name": name,
		})
		req = withURLParam(req, "id", f.skillID)
		testHandler.CreateSkillFork(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateSkillFork: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp SkillWithFilesResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, resp.ID) })
		return resp.ID
	}

	fork("fork-on-" + uuid.NewString())
	if got := countSkillInbox(t, "skill_forked", f.skillID, f.ownerID); got != 1 {
		t.Fatalf("expected 1 fork inbox item for owner, got %d", got)
	}

	// Toggle forks off, fork again — count must stay at 1.
	w := httptest.NewRecorder()
	req := newRequestAs(f.ownerID, "PUT", "/api/skills/"+f.skillID+"/ownership", map[string]any{
		"notify_forks": false,
	})
	req = withURLParam(req, "id", f.skillID)
	testHandler.UpdateSkillOwnership(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle forks off: %d: %s", w.Code, w.Body.String())
	}
	fork("fork-off-" + uuid.NewString())
	if got := countSkillInbox(t, "skill_forked", f.skillID, f.ownerID); got != 1 {
		t.Fatalf("expected fork inbox to stay at 1 after notify_forks=false, got %d", got)
	}
}

// TestAgentAssignmentNotifiesSkillOwner verifies assigning a skill to an agent
// notifies the skill owner when the assigner is not the owner.
func TestAgentAssignmentNotifiesSkillOwner(t *testing.T) {
	ctx := context.Background()
	f := setupOwnershipFixture(t)

	// Create an agent in the workspace to assign the skill to.
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode)
		VALUES ($1, $2, 'cloud')
		RETURNING id
	`, testWorkspaceID, "notify-agent-"+uuid.NewString()).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	// Assign as the workspace owner (has agent-manage rights and is not the
	// skill owner, so the skill owner is a legitimate notification recipient).
	w := httptest.NewRecorder()
	req := newRequestAs(testUserID, "POST", "/api/agents/"+agentID+"/skills/add", map[string]any{
		"skill_ids": []string{f.skillID},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentSkills(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AddAgentSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := countSkillInbox(t, "skill_agent_assigned", f.skillID, f.ownerID); got != 1 {
		t.Fatalf("expected 1 agent-assigned inbox item for owner, got %d", got)
	}
}
