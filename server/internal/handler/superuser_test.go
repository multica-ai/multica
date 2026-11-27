package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDelegatedSuperuserUsesLiveGrantWithoutLendingItToOtherCallers(t *testing.T) {
	f := seedFullDelegation(t)
	foreignCreator := dbfx.User(t, "superuser foreign creator", "superuser-foreign-creator@example.test")
	dbfx.Member(t, f.target, foreignCreator, "member")
	foreignChat := dbfx.ChatSession(t, f.foreignAgent, testutil.Cols{
		"workspace_id": f.target, "creator_id": foreignCreator,
		"title": "foreign incident", "explicitly_created_at": testutil.Raw("now()"),
	})
	dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": foreignChat, "role": "user", "content": "foreign transcript"})
	privateProject := dbfx.Project(t, "superuser private project", testutil.Cols{"workspace_id": f.target, "visibility": "private"})
	privateIssue := dbfx.Issue(t, "superuser private issue", testutil.Cols{"workspace_id": f.target, "project_id": privateProject})
	dbfx.InsertNoID(t, "agent_full_delegation", testutil.Cols{"agent_id": f.agent, "grantor_user_id": testUserID}, "agent_id=$1", f.agent)
	dbfx.Cleanup(t, `DELETE FROM workspace_superuser_audit WHERE workspace_id=$1`, f.target)

	router := chi.NewRouter()
	router.Use(middleware.Auth(testHandler.Queries, nil, nil, nil))
	router.Use(middleware.RequireWorkspaceMember(testHandler.Queries))
	router.Get("/api/chat/superuser/sessions", testHandler.ListSuperuserChats)
	router.Get("/api/chat/sessions/{sessionId}", testHandler.GetChatSession)
	router.Get("/api/chat/sessions/{sessionId}/messages", testHandler.ListChatMessages)
	router.Patch("/api/chat/sessions/{sessionId}", testHandler.UpdateChatSession)
	router.Get("/api/agents/{id}", testHandler.GetAgent)
	router.Get("/api/issues", testHandler.ListIssues)
	router.With(middleware.RequireWorkspaceRole(testHandler.Queries, "owner")).Get("/owner-only", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.Header.Set("X-Workspace-ID", f.target)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	chatPath := "/api/chat/sessions/" + foreignChat
	if got := call(http.MethodGet, chatPath, ""); got.Code != http.StatusForbidden {
		t.Fatalf("ungranted cross-member chat: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, "/owner-only", ""); got.Code != http.StatusForbidden {
		t.Fatalf("ungranted owner action: %d %s", got.Code, got.Body.String())
	}
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(testUserID), WorkspaceID: parseUUID(f.target),
	})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.InsertNoID(t, "workspace_superuser_policy", testutil.Cols{"workspace_id": f.target, "enabled_by": testUserID}, "workspace_id=$1", f.target)
	dbfx.InsertNoID(t, "workspace_superuser_grant", testutil.Cols{
		"member_id": uuidToString(member.ID), "workspace_id": f.target,
		"user_id": testUserID, "granted_by": testUserID,
	}, "member_id=$1", uuidToString(member.ID))
	humanRouter := chi.NewRouter()
	humanRouter.Use(middleware.RequireWorkspaceMember(testHandler.Queries))
	humanRouter.With(middleware.RequireWorkspaceRole(testHandler.Queries, "owner")).Get("/owner-only", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	humanRouter.Get("/api/agents/{id}", testHandler.GetAgent)
	humanRequest := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", f.target)
		w := httptest.NewRecorder()
		humanRouter.ServeHTTP(w, req)
		return w
	}
	if got := humanRequest("/owner-only"); got.Code != http.StatusOK {
		t.Fatalf("human grant did not elevate role: %d %s", got.Code, got.Body.String())
	}
	if got := humanRequest("/api/agents/" + f.foreignAgent); got.Code != http.StatusOK {
		t.Fatalf("human grant did not expose private agent: %d %s", got.Code, got.Body.String())
	}
	if persisted, err := testHandler.Queries.GetMember(context.Background(), member.ID); err != nil || persisted.Role != "member" {
		t.Fatalf("grant changed actual membership role: %+v, %v", persisted, err)
	}
	if got := call(http.MethodGet, "/owner-only", ""); got.Code != http.StatusOK {
		t.Fatalf("live grant did not elevate role: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, "/api/issues", ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(privateIssue)) {
		t.Fatalf("private project hidden after superuser grant: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, "/api/agents/"+f.foreignAgent, ""); got.Code != http.StatusOK {
		t.Fatalf("private agent hidden after superuser grant: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, chatPath, ""); got.Code != http.StatusOK {
		t.Fatalf("cross-member chat denied: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, chatPath+"/messages", ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte("foreign transcript")) {
		t.Fatalf("cross-member transcript denied: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, "/api/chat/superuser/sessions?q=foreign", ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(foreignChat)) {
		t.Fatalf("superuser search denied: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodPatch, chatPath, `{"title":"hijacked"}`); got.Code != http.StatusForbidden {
		t.Fatalf("cross-member mutation allowed: %d %s", got.Code, got.Body.String())
	}
	var audited int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_superuser_audit
		WHERE actor_user_id=$1 AND actor_agent_id=$2 AND actor_task_id=$3 AND target_user_id=$4 AND chat_session_id=$5 AND action='read_chat'`,
		testUserID, f.agent, f.task, foreignCreator, foreignChat).Scan(&audited); err != nil || audited != 3 {
		t.Fatalf("delegated audit count=%d err=%v", audited, err)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, f.runtimeOwner, f.task)
	if got := call(http.MethodGet, chatPath, ""); got.Code != http.StatusForbidden {
		t.Fatalf("foreign caller inherited grant: %d %s", got.Code, got.Body.String())
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, testUserID, f.task)
	dbfx.Exec(t, `DELETE FROM workspace_superuser_grant WHERE member_id=$1`, member.ID)
	if got := call(http.MethodGet, chatPath, ""); got.Code != http.StatusForbidden {
		t.Fatalf("revoked human grant active: %d %s", got.Code, got.Body.String())
	}
	dbfx.Exec(t, `INSERT INTO workspace_superuser_grant (member_id, workspace_id, user_id, granted_by) VALUES ($1,$2,$3,$3)`, member.ID, f.target, testUserID)
	dbfx.Exec(t, `DELETE FROM agent_full_delegation WHERE agent_id=$1`, f.agent)
	if got := call(http.MethodGet, chatPath, ""); got.Code != http.StatusForbidden {
		t.Fatalf("revoked delegation active: %d %s", got.Code, got.Body.String())
	}
	dbfx.Exec(t, `INSERT INTO agent_full_delegation (agent_id, grantor_user_id) VALUES ($1,$2)`, f.agent, testUserID)
	dbfx.Exec(t, `DELETE FROM member WHERE id=$1`, member.ID)
	if got := call(http.MethodGet, chatPath, ""); got.Code != http.StatusForbidden && got.Code != http.StatusNotFound {
		t.Fatalf("lost membership retained access: %d %s", got.Code, got.Body.String())
	}
}

func TestScopedTaskCannotUseRuntimeOwnerSuperuserGrant(t *testing.T) {
	f := seedFullDelegation(t)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(f.runtimeOwner), WorkspaceID: parseUUID(f.home),
	})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.InsertNoID(t, "workspace_superuser_policy", testutil.Cols{"workspace_id": f.home, "enabled_by": testUserID}, "workspace_id=$1", f.home)
	dbfx.InsertNoID(t, "workspace_superuser_grant", testutil.Cols{
		"member_id": uuidToString(member.ID), "workspace_id": f.home,
		"user_id": f.runtimeOwner, "granted_by": testUserID,
	}, "member_id=$1", uuidToString(member.ID))
	router := chi.NewRouter()
	router.Use(middleware.Auth(testHandler.Queries, nil, nil, nil))
	router.Use(middleware.RequireWorkspaceMember(testHandler.Queries))
	router.Get("/api/chat/superuser/sessions", testHandler.ListSuperuserChats)
	router.With(middleware.RequireWorkspaceRole(testHandler.Queries, "owner")).Get("/owner-only", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, path := range []string{"/api/chat/superuser/sessions", "/owner-only"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.Header.Set("X-Workspace-ID", f.home)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("scoped task used runtime owner's superuser grant on %s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

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
	testutil.Call(t, testHandler.ListSuperuserChats, request(http.MethodGet, "/api/chat/superuser/sessions?offset=-1", nil, "")).Want(http.StatusBadRequest)
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
