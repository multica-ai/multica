package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fullDelegationFixture struct {
	home, target, agent, task, chat, runtimeOwner, targetRuntime, targetChat, foreignAgent, foreignChat, token string
}

func seedFullDelegation(t *testing.T) fullDelegationFixture {
	t.Helper()
	home := dbfx.Workspace(t, "full delegation home", "full-home-"+uuid.NewString()[:8])
	runtimeOwner := dbfx.User(t, "full runtime owner", "full-runtime-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Member(t, home, runtimeOwner, "member")
	dbfx.Member(t, home, testUserID, "member")
	target := dbfx.Workspace(t, "full delegation target", "full-target-"+uuid.NewString()[:8])
	dbfx.Member(t, target, testUserID, "member")
	sourceRuntime := dbfx.Runtime(t, "full source runtime", testutil.Cols{"workspace_id": home, "owner_id": runtimeOwner})
	agent := dbfx.Agent(t, "full source agent", sourceRuntime, testutil.Cols{"workspace_id": home, "owner_id": testUserID})
	chat := dbfx.ChatSession(t, agent, testutil.Cols{"workspace_id": home, "creator_id": testUserID, "explicitly_created_at": testutil.Raw("now()")})
	task := dbfx.Task(t, agent, testutil.Cols{
		"status": "running", "runtime_id": sourceRuntime, "chat_session_id": chat,
		"originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human",
	})
	targetRuntime := dbfx.Runtime(t, "full target runtime", testutil.Cols{"workspace_id": target, "owner_id": testUserID})
	targetAgent := dbfx.Agent(t, "full target agent", targetRuntime, testutil.Cols{"workspace_id": target, "owner_id": testUserID})
	targetChat := dbfx.ChatSession(t, targetAgent, testutil.Cols{"workspace_id": target, "creator_id": testUserID, "title": "Other private chat", "explicitly_created_at": testutil.Raw("now()")})
	dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": targetChat, "role": "user", "content": "private transcript"})
	foreign := dbfx.User(t, "foreign private owner", "foreign-private-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Member(t, target, foreign, "member")
	foreignAgent := dbfx.Agent(t, "foreign private agent", targetRuntime, testutil.Cols{"workspace_id": target, "owner_id": foreign, "permission_mode": "private"})
	foreignChat := dbfx.ChatSession(t, foreignAgent, testutil.Cols{"workspace_id": target, "creator_id": testUserID, "title": "Inaccessible private chat", "explicitly_created_at": testutil.Raw("now()")})
	dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": foreignChat, "role": "user", "content": "must not leak"})
	token := "mat_full_" + uuid.NewString()
	dbfx.Insert(t, "task_token", testutil.Cols{
		"token_hash": auth.HashToken(token), "task_id": task, "agent_id": agent,
		"workspace_id": home, "user_id": runtimeOwner, "expires_at": testutil.Raw("now() + interval '1 hour'"),
	})
	return fullDelegationFixture{home, target, agent, task, chat, runtimeOwner, targetRuntime, targetChat, foreignAgent, foreignChat, token}
}

func TestFullDelegationRequiresOwnerChatAndLiveMembership(t *testing.T) {
	f := seedFullDelegation(t)
	params := db.AuthorizeFullAgentDelegationParams{
		TaskID: util.MustParseUUID(f.task), AgentID: util.MustParseUUID(f.agent),
		HomeWorkspaceID: util.MustParseUUID(f.home), TargetWorkspaceID: util.MustParseUUID(f.target),
	}
	check := func(want bool) {
		t.Helper()
		got, err := testHandler.Queries.AuthorizeFullAgentDelegation(context.Background(), params)
		if want && (err != nil || uuidToString(got) != testUserID) {
			t.Fatalf("delegation denied: %v %v", got, err)
		}
		if !want && err != pgx.ErrNoRows {
			t.Fatalf("delegation unexpectedly active: %v %v", got, err)
		}
	}
	check(false)
	dbfx.InsertNoID(t, "agent_full_delegation", testutil.Cols{"agent_id": f.agent, "grantor_user_id": testUserID}, "agent_id=$1", f.agent)
	check(true)
	foreign := dbfx.User(t, "foreign caller", "foreign-call-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2", foreign, f.task)
	check(false)
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1, originator_source='delegation' WHERE id=$2", testUserID, f.task)
	check(false)
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_source='direct_human', retry_of_task_id=$1 WHERE id=$2", uuid.NewString(), f.task)
	check(false)
	dbfx.Exec(t, "UPDATE agent_task_queue SET retry_of_task_id=NULL, chat_session_id=NULL WHERE id=$1", f.task)
	check(false)
	dbfx.Exec(t, "UPDATE agent_task_queue SET chat_session_id=$1 WHERE id=$2", f.chat, f.task)
	check(true)
	dbfx.Exec(t, "UPDATE member SET role='guest' WHERE user_id=$1 AND workspace_id=$2", testUserID, f.home)
	check(false)
	dbfx.Exec(t, "UPDATE member SET role='member' WHERE user_id=$1 AND workspace_id=$2", testUserID, f.home)
	dbfx.Exec(t, "DELETE FROM member WHERE user_id=$1 AND workspace_id=$2", testUserID, f.target)
	check(false)
	dbfx.Member(t, f.target, testUserID, "member")
	check(true)
	otherOwner := dbfx.User(t, "new agent owner", "full-new-owner-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Exec(t, "UPDATE agent SET owner_id=$1 WHERE id=$2", otherOwner, f.agent)
	check(false)
	dbfx.Exec(t, "DELETE FROM agent_full_delegation WHERE agent_id=$1", f.agent)
	check(false)
}

func TestFullDelegationTaskUsesGrantorPermissionsAndAgentAttribution(t *testing.T) {
	f := seedFullDelegation(t)
	dbfx.InsertNoID(t, "agent_full_delegation", testutil.Cols{"agent_id": f.agent, "grantor_user_id": testUserID}, "agent_id=$1", f.agent)
	router := chi.NewRouter()
	router.Use(middleware.Auth(testHandler.Queries, nil, nil, nil))
	router.Use(middleware.RequireWorkspaceMember(testHandler.Queries))
	router.Get("/api/issues", testHandler.ListIssues)
	router.Post("/api/issues", testHandler.CreateIssue)
	router.Post("/api/docs", testHandler.CreateDoc)
	router.Get("/api/docs/{id}", testHandler.GetDoc)
	router.Get("/api/chat/sessions", testHandler.ListChatSessions)
	router.Get("/api/chat/sessions/{sessionId}/messages", testHandler.ListChatMessages)
	router.Get("/api/agents", testHandler.ListAgents)
	router.Get("/api/agents/{id}", testHandler.GetAgent)
	router.Get("/api/runtimes", testHandler.ListAgentRuntimes)
	router.Post("/api/agents", testHandler.CreateAgent)
	router.With(RequireHumanActor).Get("/api/nondelegable", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.Header.Set("X-Workspace-ID", f.target)
		req.Header.Set("X-User-ID", f.runtimeOwner)
		req.Header.Set("X-Full-Delegation-User-ID", f.runtimeOwner)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if got := call(http.MethodGet, "/api/nondelegable", ""); got.Code != http.StatusForbidden {
		t.Fatalf("nondelegable route allowed: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, "/api/issues", ""); got.Code != http.StatusOK {
		t.Fatalf("issues: %d %s", got.Code, got.Body.String())
	}
	privateProject := dbfx.Project(t, "delegated private project", testutil.Cols{"workspace_id": f.target, "visibility": "private"})
	privateIssue := dbfx.Issue(t, "delegated private issue", testutil.Cols{"workspace_id": f.target, "project_id": privateProject})
	if got := call(http.MethodGet, "/api/issues", ""); bytes.Contains(got.Body.Bytes(), []byte(privateIssue)) {
		t.Fatalf("private issue visible without grantor access: %d %s", got.Code, got.Body.String())
	}
	dbfx.InsertNoID(t, "project_member", testutil.Cols{"workspace_id": f.target, "project_id": privateProject, "user_id": testUserID}, "project_id=$1 AND user_id=$2", privateProject, testUserID)
	if got := call(http.MethodGet, "/api/issues", ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(privateIssue)) {
		t.Fatalf("new grantor project access ignored: %d %s", got.Code, got.Body.String())
	}
	issue := call(http.MethodPost, "/api/issues", `{"title":"Full delegated issue"}`)
	if issue.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", issue.Code, issue.Body.String())
	}
	var issueBody map[string]any
	if err := json.Unmarshal(issue.Body.Bytes(), &issueBody); err != nil {
		t.Fatal(err)
	}
	if issueBody["creator_type"] != "agent" || issueBody["creator_id"] != f.agent {
		t.Fatalf("lost agent attribution: %+v", issueBody)
	}
	doc := call(http.MethodPost, "/api/docs", `{"title":"Delegated target doc","content":"target content"}`)
	if doc.Code != http.StatusCreated {
		t.Fatalf("create target doc: %d %s", doc.Code, doc.Body.String())
	}
	var docBody map[string]any
	if err := json.Unmarshal(doc.Body.Bytes(), &docBody); err != nil {
		t.Fatal(err)
	}
	if docBody["created_by_type"] != "agent" || docBody["created_by_id"] != f.agent {
		t.Fatalf("lost doc attribution: %+v", docBody)
	}
	if got := call(http.MethodGet, "/api/docs/"+docBody["id"].(string), ""); got.Code != http.StatusOK {
		t.Fatalf("read target doc: %d %s", got.Code, got.Body.String())
	}
	chats := call(http.MethodGet, "/api/chat/sessions?status=all", "")
	if chats.Code != http.StatusOK || !bytes.Contains(chats.Body.Bytes(), []byte(f.targetChat)) || bytes.Contains(chats.Body.Bytes(), []byte(f.foreignChat)) {
		t.Fatalf("chat list visibility: %d %s", chats.Code, chats.Body.String())
	}
	messages := call(http.MethodGet, "/api/chat/sessions/"+f.targetChat+"/messages", "")
	if messages.Code != http.StatusOK || !bytes.Contains(messages.Body.Bytes(), []byte("private transcript")) {
		t.Fatalf("chat transcript: %d %s", messages.Code, messages.Body.String())
	}
	if got := call(http.MethodGet, "/api/chat/sessions/"+f.foreignChat+"/messages", ""); got.Code != http.StatusForbidden {
		t.Fatalf("foreign private transcript visible: %d %s", got.Code, got.Body.String())
	}
	agents := call(http.MethodGet, "/api/agents", "")
	if agents.Code != http.StatusOK || bytes.Contains(agents.Body.Bytes(), []byte("foreign private agent")) {
		t.Fatalf("foreign agent visible: %d %s", agents.Code, agents.Body.String())
	}
	if got := call(http.MethodGet, "/api/agents/"+f.foreignAgent, ""); got.Code != http.StatusForbidden {
		t.Fatalf("foreign private agent detail visible: %d %s", got.Code, got.Body.String())
	}
	runtimes := call(http.MethodGet, "/api/runtimes", "")
	if runtimes.Code != http.StatusOK || !bytes.Contains(runtimes.Body.Bytes(), []byte(f.targetRuntime)) {
		t.Fatalf("target runtimes: %d %s", runtimes.Code, runtimes.Body.String())
	}
	created := call(http.MethodPost, "/api/agents", `{"name":"full-created-`+uuid.NewString()[:8]+`","runtime_id":"`+f.targetRuntime+`"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", created.Code, created.Body.String())
	}
	var agentBody map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &agentBody); err != nil {
		t.Fatal(err)
	}
	if agentBody["owner_id"] != testUserID || agentBody["workspace_id"] != f.target {
		t.Fatalf("wrong target ownership: %+v", agentBody)
	}
	dbfx.Exec(t, "DELETE FROM agent_full_delegation WHERE agent_id=$1", f.agent)
	if got := call(http.MethodGet, "/api/chat/sessions?status=all", ""); got.Code != http.StatusForbidden {
		t.Fatalf("revoked grant still read chats: %d %s", got.Code, got.Body.String())
	}
}

func TestFullDelegationOwnerControlsAndAccountRevocation(t *testing.T) {
	f := seedFullDelegation(t)
	router := chi.NewRouter()
	router.Route("/api/agents/{id}/full-delegation", func(r chi.Router) {
		r.Use(RequireHumanActor)
		r.Get("/", testHandler.GetAgentFullDelegation)
		r.Put("/", testHandler.SetAgentFullDelegation)
	})
	router.With(RequireHumanActor).Get("/api/me/agent-delegations", testHandler.ListMyAgentFullDelegations)
	router.With(RequireHumanActor).Delete("/api/me/agent-delegations/{agentId}", testHandler.RevokeMyAgentFullDelegation)
	call := func(method, path, user, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("X-User-ID", user)
		req.Header.Set("X-Workspace-ID", f.home)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	path := "/api/agents/" + f.agent + "/full-delegation/"
	if got := call(http.MethodGet, path, testUserID, ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatalf("default grant: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodPut, path, f.runtimeOwner, `{"enabled":true}`); got.Code != http.StatusForbidden {
		t.Fatalf("non-owner enabled grant: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodPut, path, testUserID, `{"enabled":true}`); got.Code != http.StatusOK {
		t.Fatalf("owner enabled grant: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, "/api/me/agent-delegations", testUserID, ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(f.agent)) {
		t.Fatalf("account list: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodDelete, "/api/me/agent-delegations/"+f.agent, testUserID, ""); got.Code != http.StatusNoContent {
		t.Fatalf("account revoke: %d %s", got.Code, got.Body.String())
	}
	if got := call(http.MethodGet, path, testUserID, ""); got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatalf("grant after revoke: %d %s", got.Code, got.Body.String())
	}
}
