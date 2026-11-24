package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A task token's user id is its runtime owner, whoever started the run. Private
// quick actions belong to their creator, so a run another member started on
// the owner's runtime must see, preview, and run only public actions.
func TestQuickActionTaskTokenPrivateAccess(t *testing.T) {
	other := createPlainMember(t, "qa-originator-"+uuid.NewString()+"@example.test")
	const secret = "runtime owner's private prompt"
	privateID := dbfx.Insert(t, "quick_action", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "Owner private", "description": "",
		"assignee_type": "", "prompt": secret, "visibility": "private",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	publicID := dbfx.Insert(t, "quick_action", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "Shared public", "description": "",
		"assignee_type": "", "prompt": "shared prompt", "visibility": "public",
		"created_by_type": "member", "created_by_id": testUserID,
	})
	issueID := dbfx.Issue(t, "Quick action task access", nil)
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)

	serve := func(handler func(*Handler, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return middleware.Auth(testHandler.Queries, nil, nil, nil)(
			middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler(testHandler, w, r)
			})),
		).ServeHTTP
	}
	request := func(token, method, path string, params ...string) *http.Request {
		req := testutil.WithHeaders(testutil.JSONRequest(method, path, nil),
			"Authorization", "Bearer "+token, "X-Workspace-ID", testWorkspaceID)
		if len(params) > 0 {
			req = testutil.WithURLParams(req, params...)
		}
		return req
	}
	listIDs := func(t *testing.T, token string) map[string]bool {
		t.Helper()
		var resp ListQuickActionsResponse
		testutil.Call(t, serve((*Handler).ListQuickActions), request(token, http.MethodGet, "/api/quick-actions")).
			Want(http.StatusOK).JSON(&resp)
		ids := map[string]bool{}
		for _, qa := range resp.QuickActions {
			ids[qa.ID] = true
		}
		return ids
	}
	actionPath := func(id, suffix string) string {
		return "/api/issues/" + issueID + "/quick-actions/" + id + suffix
	}
	render := func(t *testing.T, token, id string) *testutil.Response {
		t.Helper()
		return testutil.Call(t, serve((*Handler).RenderQuickAction),
			request(token, http.MethodPost, actionPath(id, "/render"), "id", issueID, "quickActionId", id))
	}
	run := func(t *testing.T, token, id string) *testutil.Response {
		t.Helper()
		return testutil.Call(t, serve((*Handler).RunQuickAction),
			request(token, http.MethodPost, actionPath(id, "/run"), "id", issueID, "quickActionId", id))
	}
	secretComments := func(t *testing.T) int {
		t.Helper()
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND quick_action_id = $2`, issueID, privateID).Scan(&n)
		return n
	}

	for _, tc := range []struct {
		name       string
		originator any
		status     string
		private    bool
	}{
		{"runtime owner started the run", testUserID, "running", true},
		{"another member started the run", other, "running", false},
		{"no human started the run", nil, "running", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Quick action runtime")
			agentID := dbfx.Agent(t, "Quick action agent", runtimeID)
			accountable := tc.originator
			if accountable == nil {
				accountable = testUserID
			}
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": tc.status,
				"originator_user_id": tc.originator, "accountable_user_id": accountable,
			})
			token := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)

			ids := listIDs(t, token)
			if !ids[publicID] {
				t.Fatal("public action missing from the list")
			}
			if ids[privateID] != tc.private {
				t.Fatalf("private action listed = %v, want %v", ids[privateID], tc.private)
			}

			render(t, token, publicID).Want(http.StatusOK)
			before := secretComments(t)
			if !tc.private {
				render(t, token, privateID).Want(http.StatusNotFound)
				run(t, token, privateID).Want(http.StatusNotFound)
				if got := secretComments(t); got != before {
					t.Fatalf("refused run still posted the private prompt: %d comments, want %d", got, before)
				}
				return
			}
			var preview struct {
				Content string `json:"content"`
			}
			render(t, token, privateID).Want(http.StatusOK).JSON(&preview)
			if preview.Content != secret {
				t.Fatalf("preview = %q, want the owner's prompt", preview.Content)
			}
			run(t, token, privateID).Want(http.StatusCreated)
			if got := secretComments(t); got != before+1 {
				t.Fatalf("run posted %d comments, want %d", got, before+1)
			}
		})
	}

	// A finished run's token is refused at auth (GitHub #11378), so it reaches
	// neither public nor private actions and posts nothing.
	t.Run("finished run with a lingering token", func(t *testing.T) {
		runtimeID := dbfx.Runtime(t, "Quick action runtime")
		agentID := dbfx.Agent(t, "Quick action agent", runtimeID)
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "issue_id": issueID, "status": "completed",
			"originator_user_id": testUserID, "accountable_user_id": testUserID,
		})
		token := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)

		testutil.Call(t, serve((*Handler).ListQuickActions), request(token, http.MethodGet, "/api/quick-actions")).
			Want(http.StatusUnauthorized)
		render(t, token, publicID).Want(http.StatusUnauthorized)
		before := secretComments(t)
		run(t, token, privateID).Want(http.StatusUnauthorized)
		if got := secretComments(t); got != before {
			t.Fatalf("refused run still posted the private prompt: %d comments, want %d", got, before)
		}
	})

	// A human credential still reaches the creator's own private action.
	t.Run("human session", func(t *testing.T) {
		var resp ListQuickActionsResponse
		testutil.Call(t, testHandler.ListQuickActions, newRequest(http.MethodGet, "/api/quick-actions", nil)).
			Want(http.StatusOK).JSON(&resp)
		found := false
		for _, qa := range resp.QuickActions {
			found = found || qa.ID == privateID
		}
		if !found {
			t.Fatal("creator lost access to their own private action")
		}
	})
}
