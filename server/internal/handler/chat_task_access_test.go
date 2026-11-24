package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A task token's user id is its runtime owner, whoever started the run. A chat
// session belongs to the member who opened it, so a run another member started
// on the owner's runtime must not list, read, write or open the owner's chats.
func TestChatSessionTaskTokenOwnerAccess(t *testing.T) {
	other := createPlainMember(t, "chat-originator-"+uuid.NewString()+"@example.test")
	const secret = "runtime owner's private chat"
	chatRuntimeID := dbfx.Runtime(t, "Chat access chat runtime")
	chatAgentID := dbfx.Agent(t, "Chat access agent", chatRuntimeID)
	sessionID := dbfx.Insert(t, "chat_session", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": chatAgentID, "creator_id": testUserID,
		"title": "Owner private chat", "status": "active",
	})
	dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID, "role": "user", "content": secret,
	})
	dbfx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, sessionID)
	dbfx.Cleanup(t, `DELETE FROM chat_session WHERE workspace_id = $1 AND agent_id = $2`, testWorkspaceID, chatAgentID)
	chatTaskID := dbfx.Task(t, chatAgentID, testutil.Cols{
		"runtime_id": chatRuntimeID, "chat_session_id": sessionID, "status": "queued",
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	issueID := dbfx.Issue(t, "Chat task access", nil)

	serve := func(handler func(*Handler, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return middleware.Auth(testHandler.Queries, nil, nil, nil)(
			middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler(testHandler, w, r)
			})),
		).ServeHTTP
	}
	request := func(token, method, path string, body any) *http.Request {
		req := testutil.WithHeaders(testutil.JSONRequest(method, path, body),
			"Authorization", "Bearer "+token, "X-Workspace-ID", testWorkspaceID)
		return testutil.WithURLParams(req, "sessionId", sessionID)
	}
	sessionPath := "/api/chat/sessions/" + sessionID

	for _, tc := range []struct {
		name       string
		originator any
		allowed    bool
	}{
		{"runtime owner started the run", testUserID, true},
		{"another member started the run", other, false},
		{"no human started the run", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Chat access runtime")
			agentID := dbfx.Agent(t, "Chat access task agent", runtimeID)
			accountable := tc.originator
			if accountable == nil {
				accountable = testUserID
			}
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
				"originator_user_id": tc.originator, "accountable_user_id": accountable,
			})
			token := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)

			list := testutil.Call(t, serve((*Handler).ListChatSessions),
				request(token, http.MethodGet, "/api/chat/sessions?status=all", nil))
			messages := testutil.Call(t, serve((*Handler).ListChatMessages),
				request(token, http.MethodGet, sessionPath+"/messages", nil))
			wakeups := testutil.Call(t, serve((*Handler).ListMyChatWakeups),
				request(token, http.MethodGet, "/api/chat/my-wakeups", nil))
			if !tc.allowed {
				// An empty list is also a refusal; what matters is that
				// neither the session nor its transcript comes back.
				if list.Code == http.StatusOK && strings.Contains(list.Body.String(), sessionID) {
					t.Fatalf("list returned the owner's chat session: %s", list.Body.String())
				}
				if strings.Contains(messages.Body.String(), secret) {
					t.Fatalf("messages returned the owner's transcript: %d %s", messages.Code, messages.Body.String())
				}
				wakeups.Want(http.StatusForbidden)
				testutil.Call(t, serve((*Handler).GetChatSession),
					request(token, http.MethodGet, sessionPath, nil)).Want(http.StatusForbidden)
				testutil.Call(t, serve((*Handler).SendChatMessage),
					request(token, http.MethodPost, sessionPath+"/messages", map[string]any{"content": "impersonated"})).
					Want(http.StatusForbidden)
				testutil.Call(t, serve((*Handler).CreateChatSession),
					request(token, http.MethodPost, "/api/chat/sessions", map[string]any{"agent_id": chatAgentID, "title": "impersonated"})).
					Want(http.StatusForbidden)
				testutil.Call(t, serve((*Handler).ListAgentBuilderSessions),
					request(token, http.MethodGet, "/api/agent-builder/sessions", nil)).Want(http.StatusForbidden)
				cancel := testutil.WithURLParams(request(token, http.MethodPost, "/api/tasks/"+chatTaskID+"/cancel", nil), "taskId", chatTaskID)
				testutil.Call(t, serve((*Handler).CancelTaskByUser), cancel).Want(http.StatusForbidden)
				var status string
				dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, chatTaskID).Scan(&status)
				if status != "queued" {
					t.Fatalf("refused cancel still moved the owner's chat task to %q", status)
				}
				return
			}
			list.Want(http.StatusOK)
			if !strings.Contains(list.Body.String(), sessionID) {
				t.Fatalf("owner-started run lost the owner's chat session: %s", list.Body.String())
			}
			messages.Want(http.StatusOK)
			wakeups.Want(http.StatusOK)
			if !strings.Contains(messages.Body.String(), secret) {
				t.Fatalf("owner-started run lost the owner's transcript: %s", messages.Body.String())
			}
			testutil.Call(t, serve((*Handler).ListAgentBuilderSessions),
				request(token, http.MethodGet, "/api/agent-builder/sessions", nil)).Want(http.StatusOK)
		})
	}

	t.Run("human session", func(t *testing.T) {
		req := testutil.WithURLParams(newRequest(http.MethodGet, sessionPath+"/messages", nil), "sessionId", sessionID)
		resp := testutil.Call(t, testHandler.ListChatMessages, chatPendingCtxAs(t, req, testUserID)).Want(http.StatusOK)
		if !strings.Contains(resp.Body.String(), secret) {
			t.Fatal("creator lost access to their own chat transcript")
		}
	})
}
