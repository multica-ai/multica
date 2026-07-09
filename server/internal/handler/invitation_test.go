package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const invitationTestEmail = "invitation-test@multica.ai"

func clearInvitationsForTestWorkspace(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`DELETE FROM workspace_invitation WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID),
	); err != nil {
		t.Fatalf("clear invitations: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_invitation WHERE workspace_id = $1`,
			parseUUID(testWorkspaceID),
		)
	})
}

// Sanity check: a fresh, live pending invitation must block re-invitation.
func TestCreateInvitation_BlocksWhilePending(t *testing.T) {
	clearInvitationsForTestWorkspace(t)

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first invite: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateInvitation(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second invite: expected 409 while still pending, got %d: %s", w2.Code, w2.Body.String())
	}
}

// Regression for issue #2055: an expired pending invitation must NOT block a
// new invitation to the same email. The stale row should be flipped to
// 'expired' and a fresh pending row should be created.
func TestCreateInvitation_AllowsAfterExpiry(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	ctx := context.Background()

	var staleID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_invitation (
			workspace_id, inviter_id, invitee_email, role, status, created_at, updated_at, expires_at
		)
		VALUES ($1, $2, $3, 'member', 'pending', now() - interval '10 days', now() - interval '10 days', now() - interval '3 days')
		RETURNING id
	`, parseUUID(testWorkspaceID), parseUUID(testUserID), invitationTestEmail).Scan(&staleID); err != nil {
		t.Fatalf("seed expired invitation: %v", err)
	}

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-invite after expiry: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp InvitationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" || resp.ID == staleID {
		t.Fatalf("expected a new invitation row, got id=%q (stale=%q)", resp.ID, staleID)
	}

	var staleStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM workspace_invitation WHERE id = $1`, staleID,
	).Scan(&staleStatus); err != nil {
		t.Fatalf("read stale row: %v", err)
	}
	if staleStatus != "expired" {
		t.Fatalf("expected stale row to be 'expired', got %q", staleStatus)
	}

	var pendingCount int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspace_invitation
		WHERE workspace_id = $1 AND invitee_email = $2 AND status = 'pending'
	`, parseUUID(testWorkspaceID), invitationTestEmail).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected exactly 1 pending invitation after re-invite, got %d", pendingCount)
	}
}
