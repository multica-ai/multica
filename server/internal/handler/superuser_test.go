package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWorkspaceSuperuserGrantAndChatRead(t *testing.T) {
	ctx := context.Background()
	viewer := dbfx.User(t, "Superuser viewer", "superuser-viewer@example.test")
	viewerMember := dbfx.Member(t, testWorkspaceID, viewer, "member")
	creator := dbfx.User(t, "Chat creator", "superuser-creator@example.test")
	dbfx.Member(t, testWorkspaceID, creator, "member")
	agent := createHandlerTestAgent(t, "SuperuserChatAgent", []byte("[]"))
	session := dbfx.ChatSession(t, agent, testutil.Cols{"creator_id": creator, "explicitly_created_at": testutil.Raw("now()")})
	_, err := testPool.Exec(ctx, `INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'historical secret')`, session)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type IN ('workspace_superuser_policy', 'workspace_superuser_grant')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace_superuser_audit WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace_superuser_grant WHERE member_id = $1`, viewerMember)
		testPool.Exec(ctx, `DELETE FROM workspace_superuser_policy WHERE workspace_id = $1`, testWorkspaceID)
	})

	request := func(method, path string, body any, target string) *http.Request {
		req := newRequest(method, path, body)
		req.Header.Set("X-User-ID", viewer)
		member, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(viewer), WorkspaceID: parseUUID(testWorkspaceID)})
		if err != nil {
			t.Fatal(err)
		}
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
		if target != "" {
			req = withURLParam(req, "sessionId", target)
		}
		return req
	}
	chatPath := "/api/chat/superuser/sessions/" + session
	testutil.Call(t, testHandler.GetSuperuserChat, request(http.MethodGet, chatPath, nil, session)).Want(http.StatusForbidden)
	ownerReq := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, chatPath, nil)), "sessionId", session)
	testutil.Call(t, testHandler.GetSuperuserChat, ownerReq).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ListSuperuserChats, request(http.MethodGet, "/api/chat/superuser/sessions", nil, "")).Want(http.StatusForbidden)

	grantPath := "/api/workspaces/" + testWorkspaceID + "/superusers/" + viewerMember
	grant := withURLParams(newRequest(http.MethodPut, grantPath, nil), "id", testWorkspaceID, "memberId", viewerMember)
	testutil.Call(t, testHandler.ChangeWorkspaceSuperuser, grant).Want(http.StatusOK)
	link := createTestShareLink(t, testWorkspaceID, "member", 0, 0)
	var preview ShareLinkInfoResponse
	testutil.Call(t, testHandler.GetShareLinkInfo, withURLParam(newRequest(http.MethodGet, "/api/share-links/"+link.Code, nil), "code", link.Code)).Want(http.StatusOK).JSON(&preview)
	if !preview.SuperuserPolicyEnabled {
		t.Fatal("join preview omitted active superuser policy")
	}
	invitation := dbfx.Insert(t, "workspace_invitation", testutil.Cols{
		"workspace_id": testWorkspaceID, "inviter_id": testUserID, "invitee_user_id": testUserID,
		"invitee_email": handlerTestEmail, "role": "member",
	})
	var invitationPreview InvitationResponse
	testutil.Call(t, testHandler.GetMyInvitation, withURLParam(newRequest(http.MethodGet, "/api/invitations/"+invitation, nil), "id", invitation)).Want(http.StatusOK).JSON(&invitationPreview)
	if !invitationPreview.SuperuserPolicyEnabled {
		t.Fatal("invitation preview omitted active superuser policy")
	}
	var invitationList []InvitationResponse
	testutil.Call(t, testHandler.ListMyInvitations, newRequest(http.MethodGet, "/api/invitations", nil)).Want(http.StatusOK).JSON(&invitationList)
	if len(invitationList) == 0 || !invitationList[0].SuperuserPolicyEnabled {
		t.Fatalf("invitation list omitted policy: %+v", invitationList)
	}
	newSession := dbfx.ChatSession(t, agent, testutil.Cols{"creator_id": creator, "explicitly_created_at": testutil.Raw("now()")})
	testutil.Call(t, testHandler.GetSuperuserChat, request(http.MethodGet, "/api/chat/superuser/sessions/"+newSession, nil, newSession)).Want(http.StatusOK)
	var notices int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2 AND type IN ('workspace_superuser_policy', 'workspace_superuser_grant')`, testWorkspaceID, creator).Scan(&notices); err != nil || notices != 2 {
		t.Fatalf("member notices = %d, %v", notices, err)
	}
	var listing struct {
		Enabled bool                      `json:"enabled"`
		Grants  []workspaceSuperuserGrant `json:"grants"`
	}
	testutil.Call(t, testHandler.ListWorkspaceSuperusers, withURLParam(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/superusers", nil), "id", testWorkspaceID)).Want(http.StatusOK).JSON(&listing)
	if !listing.Enabled || len(listing.Grants) != 1 || listing.Grants[0].UserID != viewer {
		t.Fatalf("grant listing: %+v", listing)
	}
	testutil.Call(t, testHandler.GetSuperuserChat, request(http.MethodGet, chatPath, nil, session)).Want(http.StatusOK)
	var found []superuserChatSummary
	testutil.Call(t, testHandler.ListSuperuserChats, request(http.MethodGet, "/api/chat/superuser/sessions?q=historical%20secret", nil, "")).Want(http.StatusOK).JSON(&found)
	if len(found) != 1 || found[0].ID != session || found[0].CreatorID != creator {
		t.Fatalf("search result = %+v", found)
	}
	var messages []ChatMessageResponse
	testutil.Call(t, testHandler.ListSuperuserChatMessages, request(http.MethodGet, chatPath+"/messages", nil, session)).Want(http.StatusOK).JSON(&messages)
	if len(messages) != 1 || messages[0].Content != "historical secret" {
		t.Fatalf("messages = %+v", messages)
	}
	var auditCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace_superuser_audit WHERE actor_user_id = $1 AND target_user_id = $2 AND chat_session_id = $3 AND action = 'read_chat'`, viewer, creator, session).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("audit = %d, %v", auditCount, err)
	}

	// A superuser may inspect another member's chat but cannot mutate it.
	testutil.Call(t, testHandler.UpdateChatSession, request(http.MethodPatch, "/api/chat/sessions/"+session, map[string]any{"title": "hijack"}, session)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.GetSuperuserChat, func() *http.Request {
		req := request(http.MethodGet, chatPath, nil, session)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agent)
		return req
	}()).Want(http.StatusForbidden)
	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'guest' WHERE id = $1`, viewerMember); err != nil {
		t.Fatal(err)
	}
	testutil.Call(t, testHandler.GetSuperuserChat, request(http.MethodGet, chatPath, nil, session)).Want(http.StatusForbidden)
	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'member' WHERE id = $1`, viewerMember); err != nil {
		t.Fatal(err)
	}

	revoke := withURLParams(newRequest(http.MethodDelete, grantPath, nil), "id", testWorkspaceID, "memberId", viewerMember)
	testutil.Call(t, testHandler.ChangeWorkspaceSuperuser, revoke).Want(http.StatusOK)
	testutil.Call(t, testHandler.GetSuperuserChat, request(http.MethodGet, chatPath, nil, session)).Want(http.StatusForbidden)
}
