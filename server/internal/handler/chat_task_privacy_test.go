package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type chatTaskPrivacyFixture struct {
	ownerID     string
	agentID     string
	chatTaskID  string
	issueTaskID string
}

// seedChatTaskPrivacy builds a plain member's agent with a finished chat task
// the member started and a running issue task, both carrying a result and an
// error. The bystander in these tests is testUserID, the workspace owner, so an
// admin role cannot be what decides access.
func seedChatTaskPrivacy(t *testing.T) chatTaskPrivacyFixture {
	t.Helper()
	ownerID := dbfx.User(t, "Chat task owner", "chat-task-privacy-owner@example.test")
	dbfx.Member(t, testWorkspaceID, ownerID, "member")
	runtimeID := handlerTestRuntimeID(t)
	agentID := dbfx.Agent(t, "chat task privacy agent", runtimeID, testutil.Cols{"owner_id": ownerID})
	chatSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{"creator_id": ownerID})
	chatTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":      runtimeID,
		"chat_session_id": chatSessionID,
		"status":          "completed",
		"completed_at":    testutil.Raw("now()"),
		"result":          testutil.Raw(`'{"output":"private chat reply"}'::jsonb`),
		"error":           "private chat error",
	})
	dbfx.Insert(t, "task_message", testutil.Cols{
		"task_id": chatTaskID,
		"seq":     1,
		"type":    "text",
		"content": "private chat transcript",
	})
	issueID := dbfx.Issue(t, "chat task privacy issue", testutil.Cols{"status": "in_progress"})
	issueTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
		"result":     testutil.Raw(`'{"output":"issue output"}'::jsonb`),
		"error":      "issue error",
	})
	return chatTaskPrivacyFixture{ownerID: ownerID, agentID: agentID, chatTaskID: chatTaskID, issueTaskID: issueTaskID}
}

func TestListTaskMessagesByUserHidesChatTranscriptFromNonOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedChatTaskPrivacy(t)
	read := func(userID, taskID string) *testutil.Response {
		req := withURLParam(newRequestAs(userID, http.MethodGet, "/api/tasks/"+taskID+"/messages", nil), "taskId", taskID)
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
		return testutil.Call(t, testHandler.ListTaskMessagesByUser, req)
	}

	var history []protocol.TaskMessagePayload
	read(f.ownerID, f.chatTaskID).Want(http.StatusOK).JSON(&history)
	if len(history) != 1 || history[0].Content != "private chat transcript" {
		t.Fatalf("owner transcript = %+v, want the one private message", history)
	}
	read(testUserID, f.chatTaskID).Want(http.StatusNotFound)
	read(testUserID, f.issueTaskID).Want(http.StatusOK)
}

func TestAgentTaskListsRedactForeignChatTaskContent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedChatTaskPrivacy(t)

	lists := map[string]func(t *testing.T, userID string) []AgentTaskResponse{
		"agent tasks": func(t *testing.T, userID string) []AgentTaskResponse {
			req := withURLParam(newRequestAs(userID, http.MethodGet, "/api/agents/"+f.agentID+"/tasks", nil), "id", f.agentID)
			var tasks []AgentTaskResponse
			testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
			return tasks
		},
		"workspace snapshot": func(t *testing.T, userID string) []AgentTaskResponse {
			req := newRequestAs(userID, http.MethodGet, "/api/agent-task-snapshot", nil)
			var tasks []AgentTaskResponse
			testutil.Call(t, testHandler.ListWorkspaceAgentTaskSnapshot, req).Want(http.StatusOK).JSON(&tasks)
			return tasks
		},
	}
	for name, list := range lists {
		t.Run(name, func(t *testing.T) {
			byViewer := map[string]map[string]AgentTaskResponse{}
			for _, viewer := range []string{f.ownerID, testUserID} {
				byViewer[viewer] = map[string]AgentTaskResponse{}
				for _, task := range list(t, viewer) {
					byViewer[viewer][task.ID] = task
				}
				for _, id := range []string{f.chatTaskID, f.issueTaskID} {
					if _, ok := byViewer[viewer][id]; !ok {
						t.Fatalf("viewer %s: task %s missing; rows must stay listed for presence", viewer, id)
					}
				}
			}

			own := byViewer[f.ownerID][f.chatTaskID]
			if own.Result == nil || own.Error == nil || *own.Error != "private chat error" {
				t.Fatalf("owner sees chat task result=%v error=%v, want full content", own.Result, own.Error)
			}
			foreign := byViewer[testUserID][f.chatTaskID]
			if foreign.Result != nil || foreign.Error != nil {
				t.Fatalf("non-owner sees chat task result=%v error=%v, want both redacted", foreign.Result, foreign.Error)
			}
			if foreign.ChatSessionID == "" || foreign.Status != "completed" {
				t.Fatalf("non-owner chat row = %+v, want its metadata kept", foreign)
			}
			issue := byViewer[testUserID][f.issueTaskID]
			if issue.Result == nil || issue.Error == nil || *issue.Error != "issue error" {
				t.Fatalf("issue task result=%v error=%v, want it untouched", issue.Result, issue.Error)
			}
		})
	}
}
