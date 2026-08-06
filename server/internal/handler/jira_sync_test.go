package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/jira"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func jiraSyncReq(wsID, connID string) *http.Request {
	req := httptest.NewRequest("POST", "/api/workspaces/"+wsID+"/jira/connections/"+connID+"/sync", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", wsID)
	rctx.URLParams.Add("connectionId", connID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestJiraSync_PullCreatesAndUpdates exercises the pull-based sync end to
// end against the test DB: a first sync creates mirrored issues, a repeat
// sync with a changed summary updates them through the same shared
// create-or-sync path the webhook uses.
func TestJiraSync_PullCreatesAndUpdates(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	mock := &mockJiraClient{searchResults: []jira.Issue{
		{ID: "10001", Key: "PULL-1", Summary: "First pulled issue", Description: "details one"},
		{ID: "10002", Key: "PULL-2", Summary: "Second pulled issue"},
	}}
	testHandler.JiraClient = mock
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	fire := func() JiraSyncResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.SyncJiraConnection(w, jiraSyncReq(testWorkspaceID, uuidToString(conn.ID)))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var resp JiraSyncResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	resp := fire()
	if resp.Created != 2 || resp.Updated != 0 || resp.Total != 2 {
		t.Errorf("first sync = %+v, want {2 0 2}", resp)
	}
	// Connection has no stored JQL → default is applied.
	if mock.lastJQL != defaultJiraSyncJQL {
		t.Errorf("jql = %q, want default %q", mock.lastJQL, defaultJiraSyncJQL)
	}

	link, err := testHandler.Queries.GetJiraIssueLink(ctx, db.GetJiraIssueLinkParams{
		ConnectionID: conn.ID,
		JiraIssueKey: "PULL-1",
	})
	if err != nil {
		t.Fatalf("GetJiraIssueLink: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, link.MulticaIssueID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Title != "First pulled issue" || issue.Description.String != "details one" {
		t.Errorf("unexpected issue: title=%q description=%q", issue.Title, issue.Description.String)
	}
	if issue.OriginType.String != "jira" {
		t.Errorf("origin_type = %q, want jira", issue.OriginType.String)
	}

	// Second run with a renamed summary syncs in place instead of duplicating.
	mock.searchResults[0].Summary = "Renamed pulled issue"
	resp = fire()
	if resp.Created != 0 || resp.Updated != 2 || resp.Total != 2 {
		t.Errorf("second sync = %+v, want {0 2 2}", resp)
	}
	issue, err = testHandler.Queries.GetIssue(ctx, link.MulticaIssueID)
	if err != nil {
		t.Fatalf("GetIssue after resync: %v", err)
	}
	if issue.Title != "Renamed pulled issue" {
		t.Errorf("title not synced, got %q", issue.Title)
	}
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM jira_issue_link WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 links, got %d", count)
	}
}

// TestJiraSync_UsesStoredJQL verifies a connection's stored JQL overrides
// the default filter.
func TestJiraSync_UsesStoredJQL(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	mock := &mockJiraClient{}
	testHandler.JiraClient = mock
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })
	if _, err := testPool.Exec(ctx, `UPDATE jira_connection SET jql = 'project = OPS ORDER BY updated DESC' WHERE id = $1`, uuidToString(conn.ID)); err != nil {
		t.Fatalf("set jql: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.SyncJiraConnection(w, jiraSyncReq(testWorkspaceID, uuidToString(conn.ID)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if mock.lastJQL != "project = OPS ORDER BY updated DESC" {
		t.Errorf("jql = %q, want the stored one", mock.lastJQL)
	}
}

func TestJiraSync_SearchErrors(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unauthorized", jira.ErrUnauthorized, http.StatusBadRequest},
		{"bad jql", jira.ErrBadRequest, http.StatusBadRequest},
		{"unreachable", context.DeadlineExceeded, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testHandler.JiraClient = &mockJiraClient{err: tc.err}
			w := httptest.NewRecorder()
			testHandler.SyncJiraConnection(w, jiraSyncReq(testWorkspaceID, uuidToString(conn.ID)))
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestJiraSync_WrongWorkspaceIs404(t *testing.T) {
	ctx := context.Background()
	box := withJiraBox(t)
	testHandler.JiraClient = &mockJiraClient{}
	conn := seedJiraConnection(t, ctx, box, "https://acme.atlassian.net")
	t.Cleanup(func() { cleanupJira(ctx) })

	w := httptest.NewRecorder()
	testHandler.SyncJiraConnection(w, jiraSyncReq("00000000-0000-0000-0000-000000000001", uuidToString(conn.ID)))
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-workspace sync: status = %d, want 404", w.Code)
	}
}
