package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A chat bound to a private project (MUL-8472) hands the project's title,
// description and resources to every run. A member removed from the project
// keeps the chat, so the claim must stop carrying the project then.
func TestPrivateProjectChatClaimDropsContextAfterRemoval(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		name    string
		removed bool
	}{
		{"project member", false},
		{"removed from the project", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			marker := fmt.Sprintf("ppchat%d", time.Now().UnixNano())
			member := createPlainMember(t, marker+"@multica.test")
			projectID := dbfx.Project(t, marker+" secret project", testutil.Cols{
				"visibility":  projectaccess.VisibilityPrivate,
				"description": marker + " secret description",
			})
			addProjectMemberRow(t, projectID, member)

			// The agent's owner is the workspace owner, who sees every
			// project: only the chat's human can stop the context.
			agentID := createHandlerTestAgent(t, "PrivateChatClaimAgent", []byte("[]"))
			runtimeID := handlerTestRuntimeID(t)
			sessionID := dbfx.ChatSession(t, agentID, testutil.Cols{"creator_id": member, "project_id": projectID})
			dbfx.Exec(t, `INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'hello')`, sessionID)
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "priority": 1000, "chat_session_id": sessionID,
				"originator_user_id": member, "accountable_user_id": member,
			})
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
			})
			if tc.removed {
				if _, err := testPool.Exec(ctx, `DELETE FROM project_member WHERE project_id = $1 AND user_id = $2`, projectID, member); err != nil {
					t.Fatal(err)
				}
			}

			w := httptest.NewRecorder()
			req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil), "runtimeId", runtimeID)
			testHandler.ClaimTaskByRuntime(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("ClaimTaskByRuntime = %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Task *struct {
					ID                 string `json:"id"`
					ProjectID          string `json:"project_id"`
					ProjectTitle       string `json:"project_title"`
					ProjectDescription string `json:"project_description"`
				} `json:"task"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if resp.Task == nil || resp.Task.ID != taskID {
				t.Fatalf("claimed task = %+v, want %s", resp.Task, taskID)
			}
			if tc.removed {
				if resp.Task.ProjectID != "" || resp.Task.ProjectTitle != "" || resp.Task.ProjectDescription != "" {
					t.Fatalf("claim after removal carries the private project: %+v", resp.Task)
				}
				// The session keeps its binding; access coming back restores it.
				if n := dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id = $1 AND project_id = $2`, sessionID, projectID); n != 1 {
					t.Fatal("the claim cleared the session's project")
				}
				return
			}
			if resp.Task.ProjectID != projectID || resp.Task.ProjectTitle != marker+" secret project" {
				t.Fatalf("project member's claim lacks the project: %+v", resp.Task)
			}
		})
	}
}
