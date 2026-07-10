package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// defaultSpaceIDForTestWorkspace returns the workspace's default (earliest
// active) space id — the same one AcceptInvitation falls back to.
func defaultSpaceIDForTestWorkspace(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM workspace_space
		WHERE workspace_id = $1 AND archived_at IS NULL
		ORDER BY created_at ASC LIMIT 1
	`, parseUUID(testWorkspaceID)).Scan(&id); err != nil {
		t.Fatalf("resolve default space: %v", err)
	}
	return id
}

// A targeted space id must be persisted on the invitation row and echoed back
// in the response.
func TestCreateInvitation_PersistsSpaceIDs(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	spaceID := defaultSpaceIDForTestWorkspace(t)

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email:    "invite-space-persist@multica.ai",
		Role:     "member",
		SpaceIDs: []string{spaceID, spaceID}, // duplicate is deduped
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp InvitationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.SpaceIDs) != 1 || resp.SpaceIDs[0] != spaceID {
		t.Fatalf("response space_ids = %v, want [%s]", resp.SpaceIDs, spaceID)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workspace_invitation
		WHERE id = $1 AND $2 = ANY(space_ids)
	`, parseUUID(resp.ID), parseUUID(spaceID)).Scan(&count); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected space_id persisted on invitation row")
	}
}

// A space that is not an active space in this workspace must be rejected up
// front rather than silently dropped.
func TestCreateInvitation_RejectsUnknownSpace(t *testing.T) {
	clearInvitationsForTestWorkspace(t)

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email:    "invite-space-bad@multica.ai",
		Role:     "member",
		SpaceIDs: []string{"00000000-0000-0000-0000-000000000000"},
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateInvitation(w, req)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("expected a 4xx for an unknown space, got %d: %s", w.Code, w.Body.String())
	}
}

// The invitee joins exactly the spaces the invitation targets.
func TestAcceptInvitation_JoinsTargetedSpaces(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	ctx := context.Background()

	var spaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, issue_counter, created_by)
		VALUES ($1, 'Invite Target', 'INVTGT', 0, $2)
		RETURNING id
	`, parseUUID(testWorkspaceID), parseUUID(testUserID)).Scan(&spaceID); err != nil {
		t.Fatalf("create target space: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, parseUUID(spaceID))
	})

	inviteeID := createInviteeUser(t, "invite-accept-target@multica.ai")

	invID := seedPendingInvitation(t, "invite-accept-target@multica.ai", inviteeID, []string{spaceID})

	w := acceptAs(t, invID, inviteeID)
	if w.Code != http.StatusOK {
		t.Fatalf("accept: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !isSpaceMember(t, spaceID, inviteeID) {
		t.Fatalf("expected invitee to join the targeted space")
	}
	if isSpaceMember(t, defaultSpaceIDForTestWorkspace(t), inviteeID) {
		t.Fatalf("invitee should not have joined the default space when a different space was targeted")
	}
}

// When every targeted space has been archived since the invite was sent, the
// invitee falls back to the default space rather than landing space-less.
func TestAcceptInvitation_FallsBackToDefaultWhenTargetsArchived(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	ctx := context.Background()

	var spaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, issue_counter, created_by, archived_at)
		VALUES ($1, 'Archived Target', 'ARCTGT', 0, $2, now())
		RETURNING id
	`, parseUUID(testWorkspaceID), parseUUID(testUserID)).Scan(&spaceID); err != nil {
		t.Fatalf("create archived space: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, parseUUID(spaceID))
	})

	inviteeID := createInviteeUser(t, "invite-accept-archived@multica.ai")
	invID := seedPendingInvitation(t, "invite-accept-archived@multica.ai", inviteeID, []string{spaceID})

	w := acceptAs(t, invID, inviteeID)
	if w.Code != http.StatusOK {
		t.Fatalf("accept: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if isSpaceMember(t, spaceID, inviteeID) {
		t.Fatalf("invitee should not have joined an archived space")
	}
	if !isSpaceMember(t, defaultSpaceIDForTestWorkspace(t), inviteeID) {
		t.Fatalf("expected invitee to fall back to the default space")
	}
}

// --- helpers -------------------------------------------------------------

func createInviteeUser(t *testing.T, email string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Invitee', $1)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, email).Scan(&id); err != nil {
		t.Fatalf("create invitee user: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM workspace_space_member WHERE user_id = $1`, parseUUID(id))
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1`, parseUUID(id))
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, parseUUID(id))
	})
	return id
}

func seedPendingInvitation(t *testing.T, email, inviteeID string, spaceIDs []string) string {
	t.Helper()
	ids := make([]pgtype.UUID, len(spaceIDs))
	for i, s := range spaceIDs {
		ids[i] = parseUUID(s)
	}
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, invitee_user_id, role, space_ids)
		VALUES ($1, $2, $3, $4, 'member', $5)
		RETURNING id
	`, parseUUID(testWorkspaceID), parseUUID(testUserID), email, parseUUID(inviteeID), ids).Scan(&id); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}
	return id
}

func acceptAs(t *testing.T, invitationID, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest("POST", "/api/invitations/"+invitationID+"/accept", nil)
	req.Header.Set("X-User-ID", userID)
	req = withURLParam(req, "id", invitationID)
	w := httptest.NewRecorder()
	testHandler.AcceptInvitation(w, req)
	return w
}

func isSpaceMember(t *testing.T, spaceID, userID string) bool {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workspace_space_member WHERE space_id = $1 AND user_id = $2
	`, parseUUID(spaceID), parseUUID(userID)).Scan(&count); err != nil {
		t.Fatalf("read space membership: %v", err)
	}
	return count > 0
}
