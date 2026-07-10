package handler

// FIR-2748: the Skills-page personal review alert ("N changes to review on
// skills you own") only renders when the client can tell which skills the
// current user owns or approves. The client resolves that from the skill LIST
// payload (enrichSkillChanges), so the list MUST carry owner_id + approver_ids.
// Before the fix the cerebro list endpoint dropped both fields, so "mine"
// always resolved to 0 and the alert never appeared. This test seeds a skill
// with a known owner + approver and asserts both fields survive on the wire.

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestListSkills_IncludesOwnershipForReviewAlert(t *testing.T) {
	// The list ownership fields are only surfaced by the cerebro metadata
	// path; the upstream fallback (nil CerebroQueries) intentionally stays
	// narrow. Wire the real cerebro queries for this test.
	orig := testHandler.CerebroQueries
	testHandler.CerebroQueries = cerebrodb.New(testPool)
	t.Cleanup(func() { testHandler.CerebroQueries = orig })

	f := setupOwnershipFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSkills(w, req)
	if w.Code != 200 {
		t.Fatalf("ListSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("ListSkills: decode body: %v", err)
	}

	var found bool
	for _, row := range rows {
		if row["id"] != f.skillID {
			continue
		}
		found = true

		// owner_id must be present and equal to the seeded owner. This is the
		// field whose absence broke the personal review alert.
		owner, ok := row["owner_id"]
		if !ok {
			t.Fatalf("ListSkills: response row missing owner_id (alert cannot resolve 'mine'): %v", row)
		}
		if owner != f.ownerID {
			t.Fatalf("ListSkills: owner_id = %v, want %s", owner, f.ownerID)
		}

		// approver_ids must be present as an array containing the seeded
		// approver, so an approver (not just the owner) also sees the alert.
		rawApprovers, ok := row["approver_ids"]
		if !ok {
			t.Fatalf("ListSkills: response row missing approver_ids: %v", row)
		}
		approvers, ok := rawApprovers.([]any)
		if !ok {
			t.Fatalf("ListSkills: approver_ids is not an array: %T %v", rawApprovers, rawApprovers)
		}
		var hasApprover bool
		for _, a := range approvers {
			if a == f.approverID {
				hasApprover = true
			}
		}
		if !hasApprover {
			t.Fatalf("ListSkills: approver_ids %v does not contain seeded approver %s", approvers, f.approverID)
		}
	}
	if !found {
		t.Fatalf("ListSkills: seeded skill %s not in response", f.skillID)
	}
}
