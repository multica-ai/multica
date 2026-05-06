package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/localmode"
)

const localModeRejectErr = "invitations are unavailable in local mode"

// newLocalInvitationHandler returns a shallow copy of the shared testHandler
// with LocalMode toggled. We clone instead of mutating the shared instance so
// parallel/concurrent tests in the package can never observe a transient
// "local mode enabled" state on testHandler itself.
func newLocalInvitationHandler(localEnabled bool) *Handler {
	h := *testHandler
	if localEnabled {
		h.LocalMode = localmode.Config{ProductMode: "local"}
	} else {
		h.LocalMode = localmode.Config{}
	}
	return &h
}

// assertLocalModeRejection asserts that the recorder captured a 403 with the
// canonical "invitations are unavailable in local mode" body.
func assertLocalModeRejection(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != localModeRejectErr {
		t.Fatalf("expected error %q, got %q", localModeRejectErr, resp["error"])
	}
}

// countInvitationsInTestWorkspace reports the number of workspace_invitation
// rows for the fixture workspace — used to verify the local-mode guard runs
// before any DB write.
func countInvitationsInTestWorkspace(t *testing.T) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM workspace_invitation WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID),
	).Scan(&n); err != nil {
		t.Fatalf("count invitations: %v", err)
	}
	return n
}

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

// ---------------------------------------------------------------------------
// Local-mode guardrails — Slice B of Task 6 (Backend Guardrails).
//
// In local product mode, every invitation handler must return 403 with the
// canonical body before performing any DB read or write. The guard runs at
// the top of each handler so we never leak existence info or produce side
// effects.
// ---------------------------------------------------------------------------

func TestLocalGuardInvitation_CreateRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	before := countInvitationsInTestWorkspace(t)

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	h.CreateInvitation(w, req)

	assertLocalModeRejection(t, w)

	if after := countInvitationsInTestWorkspace(t); after != before {
		t.Fatalf("expected no invitation rows to be created, before=%d after=%d", before, after)
	}
}

func TestLocalGuardInvitation_AcceptRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	// Use a syntactically valid UUID that does not exist; the guard must
	// run before the DB lookup so the row's absence is irrelevant here.
	bogusID := "00000000-0000-0000-0000-000000000000"
	req := newRequest("POST", "/api/invitations/"+bogusID+"/accept", nil)
	req = withURLParam(req, "id", bogusID)
	w := httptest.NewRecorder()
	h.AcceptInvitation(w, req)

	assertLocalModeRejection(t, w)
}

func TestLocalGuardInvitation_DeclineRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	bogusID := "00000000-0000-0000-0000-000000000000"
	req := newRequest("POST", "/api/invitations/"+bogusID+"/decline", nil)
	req = withURLParam(req, "id", bogusID)
	w := httptest.NewRecorder()
	h.DeclineInvitation(w, req)

	assertLocalModeRejection(t, w)
}

func TestLocalGuardInvitation_ListWorkspaceRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	req := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/invitations", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	h.ListWorkspaceInvitations(w, req)

	assertLocalModeRejection(t, w)
}

func TestLocalGuardInvitation_ListMyRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	req := newRequest("GET", "/api/invitations", nil)
	w := httptest.NewRecorder()
	h.ListMyInvitations(w, req)

	assertLocalModeRejection(t, w)
}

func TestLocalGuardInvitation_GetMyRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	bogusID := "00000000-0000-0000-0000-000000000000"
	req := newRequest("GET", "/api/invitations/"+bogusID, nil)
	req = withURLParam(req, "id", bogusID)
	w := httptest.NewRecorder()
	h.GetMyInvitation(w, req)

	assertLocalModeRejection(t, w)
}

func TestLocalGuardInvitation_RevokeRejected(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	h := newLocalInvitationHandler(true)

	bogusID := "00000000-0000-0000-0000-000000000000"
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/invitations/"+bogusID, nil)
	req = withURLParam(req, "id", testWorkspaceID)
	req = withURLParam(req, "invitationId", bogusID)
	w := httptest.NewRecorder()
	h.RevokeInvitation(w, req)

	assertLocalModeRejection(t, w)
}

// Outside local mode, CreateInvitation must behave normally — the guard is a
// no-op when LocalMode.Enabled() is false. The shared testHandler is
// configured with default product mode (ProductMode == ""), so we hit it
// directly here.
func TestLocalGuardInvitation_AllowedOutsideLocalMode(t *testing.T) {
	clearInvitationsForTestWorkspace(t)

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: invitationTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)

	if w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), localModeRejectErr) {
		t.Fatalf("CreateInvitation rejected outside local mode: %d %s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 outside local mode, got %d: %s", w.Code, w.Body.String())
	}
}
