package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func removeTestMember(t *testing.T, memberID string, body any) {
	t.Helper()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/members/"+memberID, body)
	req = withURLParams(req, "id", testWorkspaceID, "memberId", memberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)
}

func TestDeleteMember_BlockRejoinRefusesEveryWayBackIn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	email := "blocked-rejoin-" + uuid.NewString() + "@multica.ai"
	userID := dbfx.User(t, "Blocked Rejoin", email)
	memberID := dbfx.Member(t, testWorkspaceID, userID, "member")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_member_block WHERE email = $1`, email)
		testPool.Exec(context.Background(), `DELETE FROM workspace_invitation WHERE invitee_email = $1`, email)
	})
	invitationID := dbfx.Insert(t, "workspace_invitation", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"inviter_id":      testUserID,
		"invitee_email":   email,
		"invitee_user_id": userID,
		"role":            "member",
		"status":          "pending",
		"expires_at":      testutil.Raw("now() + interval '1 day'"),
	})

	removeTestMember(t, memberID, map[string]any{"block_rejoin": true})

	// A new invitation is refused.
	invite := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{Email: email, Role: "member"})
	invite = withURLParam(invite, "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, invite).Want(http.StatusConflict)

	// An invitation sent before the removal cannot be accepted.
	accept := func() *http.Request {
		req := newRequest("POST", "/api/invitations/"+invitationID+"/accept", nil)
		req.Header.Set("X-User-ID", userID)
		return withURLParam(req, "id", invitationID)
	}
	testutil.Call(t, testHandler.AcceptInvitation, accept()).Want(http.StatusForbidden)

	// Nor does a share link get them back in.
	clearShareLinksForTestWorkspace(t)
	link := createTestShareLink(t, testWorkspaceID, "member", 0, 0)
	join := newRequest("POST", "/api/share-links/join", JoinByShareLinkRequest{Code: link.Code})
	join.Header.Set("X-User-ID", userID)
	testutil.Call(t, testHandler.JoinByShareLink, join).Want(http.StatusForbidden)

	// The block is listed, and lifting it lets the old invitation through.
	list := withURLParam(newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/member-blocks", nil), "id", testWorkspaceID)
	var blocks []MemberBlockResponse
	testutil.Call(t, testHandler.ListMemberBlocks, list).Want(http.StatusOK).JSON(&blocks)
	var blockID string
	for _, b := range blocks {
		if b.Email == email {
			blockID = b.ID
		}
	}
	if blockID == "" {
		t.Fatalf("blocks = %+v, want %s listed", blocks, email)
	}
	unblock := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/member-blocks/"+blockID, nil)
	unblock = withURLParams(unblock, "id", testWorkspaceID, "blockId", blockID)
	testutil.Call(t, testHandler.DeleteMemberBlock, unblock).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.AcceptInvitation, accept()).Want(http.StatusOK)
}

func TestDeleteMember_WithoutBodyDoesNotBlock(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	email := "plain-remove-" + uuid.NewString() + "@multica.ai"
	userID := dbfx.User(t, "Plain Remove", email)
	memberID := dbfx.Member(t, testWorkspaceID, userID, "member")

	removeTestMember(t, memberID, nil)

	var blocked bool
	dbfx.QueryRow(t, `SELECT EXISTS (SELECT 1 FROM workspace_member_block WHERE email = $1)`, email).Scan(&blocked)
	if blocked {
		t.Fatal("a plain removal blocked the member")
	}
}
