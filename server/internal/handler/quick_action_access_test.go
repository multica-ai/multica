package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Cross-member access regression for private quick actions (MUL-5465, review
// finding #2).
//
// The ownership rule used to be enforced inline in RunQuickAction ONLY. Update
// and Render reached rows by (id, workspace) alone, so any member who learned a
// UUID — an action-created comment carries one in `quick_action_id` — could
// rewrite or read back another member's private prompt. The gate now lives in
// loadReachableQuickAction, and these tests hold every entry point to it.
//
// A non-owner gets 404 rather than 403 on purpose: whether a given UUID exists
// is not their business either.

// seedPrivateQuickActionOwnedByOther inserts a private action attributed to a
// second member, and returns its id. The row is removed on cleanup.
func seedPrivateQuickActionOwnedByOther(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (email, name)
		VALUES ('qa-other@example.test', 'QA Other')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`).Scan(&otherUserID); err != nil {
		t.Fatalf("seed other user: %v", err)
	}

	agentID := createHandlerTestAgent(t, "QA Access Agent", []byte("null"))

	var actionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO quick_action (
			workspace_id, name, description, assignee_type, assignee_id, prompt,
			visibility, created_by_type, created_by_id
		) VALUES ($1, 'Other Private', '', 'agent', $2, 'secret prompt', 'private', 'member', $3)
		RETURNING id
	`, testWorkspaceID, agentID, otherUserID).Scan(&actionID); err != nil {
		t.Fatalf("seed private quick action: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM quick_action WHERE id = $1`, actionID)
	})
	return actionID
}

func TestUpdateQuickActionRejectsNonOwnerOfPrivateAction(t *testing.T) {
	actionID := seedPrivateQuickActionOwnedByOther(t)

	req := withURLParam(
		newRequest(http.MethodPatch, "/api/quick-actions/"+actionID, map[string]any{
			"name": "Hijacked",
		}),
		"id", actionID,
	)
	rr := httptest.NewRecorder()
	testHandler.UpdateQuickAction(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("a non-owner must not reach another member's private action: got %d, want 404", rr.Code)
	}

	// The prompt must be untouched, not merely un-returned.
	var name string
	if err := testPool.QueryRow(context.Background(),
		`SELECT name FROM quick_action WHERE id = $1`, actionID).Scan(&name); err != nil {
		t.Fatalf("re-read action: %v", err)
	}
	if name != "Other Private" {
		t.Fatalf("the refused update still mutated the row: name = %q", name)
	}
}

func TestDeleteQuickActionRejectsNonOwnerOfPrivateAction(t *testing.T) {
	actionID := seedPrivateQuickActionOwnedByOther(t)

	req := withURLParam(
		newRequest(http.MethodDelete, "/api/quick-actions/"+actionID, nil),
		"id", actionID,
	)
	rr := httptest.NewRecorder()
	testHandler.DeleteQuickAction(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("a non-owner must not delete another member's private action: got %d, want 404", rr.Code)
	}

	var alive int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM quick_action WHERE id = $1`, actionID).Scan(&alive); err != nil {
		t.Fatalf("re-read action: %v", err)
	}
	if alive != 1 {
		t.Fatal("the refused delete still removed the row")
	}
}
