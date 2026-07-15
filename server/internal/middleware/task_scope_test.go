package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	taskScopeTaskID         = "00000000-0000-0000-0000-000000000101"
	taskScopeAgentID        = "00000000-0000-0000-0000-000000000102"
	taskScopeWorkspaceID    = "00000000-0000-0000-0000-000000000103"
	taskScopeIssueID        = "00000000-0000-0000-0000-000000000104"
	taskScopeHistoryTaskID  = "00000000-0000-0000-0000-000000000105"
	taskScopeCommentID      = "00000000-0000-0000-0000-000000000106"
	taskScopeForeignIssueID = "00000000-0000-0000-0000-000000000201"
	taskScopeForeignTaskID  = "00000000-0000-0000-0000-000000000202"
	taskScopeForeignComment = "00000000-0000-0000-0000-000000000203"
)

type fakeTaskScopeQueries struct {
	agents     map[string]db.Agent
	tasks      map[string]db.AgentTaskQueue
	comments   map[string]db.Comment
	issues     map[string]db.Issue
	byNumber   map[int32]db.Issue
	workspaces map[string]db.Workspace
}

func (f *fakeTaskScopeQueries) GetAgent(_ context.Context, id pgtype.UUID) (db.Agent, error) {
	if agent, ok := f.agents[util.UUIDToString(id)]; ok {
		return agent, nil
	}
	return db.Agent{}, errors.New("agent not found")
}

func (f *fakeTaskScopeQueries) GetComment(_ context.Context, id pgtype.UUID) (db.Comment, error) {
	if comment, ok := f.comments[util.UUIDToString(id)]; ok {
		return comment, nil
	}
	return db.Comment{}, errors.New("comment not found")
}

func (f *fakeTaskScopeQueries) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	if task, ok := f.tasks[util.UUIDToString(id)]; ok {
		return task, nil
	}
	return db.AgentTaskQueue{}, errors.New("task not found")
}

func (f *fakeTaskScopeQueries) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	if issue, ok := f.issues[util.UUIDToString(id)]; ok {
		return issue, nil
	}
	return db.Issue{}, errors.New("issue not found")
}

func (f *fakeTaskScopeQueries) GetIssueByNumber(_ context.Context, arg db.GetIssueByNumberParams) (db.Issue, error) {
	issue, ok := f.byNumber[arg.Number]
	if !ok || issue.WorkspaceID != arg.WorkspaceID {
		return db.Issue{}, errors.New("issue not found")
	}
	return issue, nil
}

func (f *fakeTaskScopeQueries) GetWorkspace(_ context.Context, id pgtype.UUID) (db.Workspace, error) {
	if workspace, ok := f.workspaces[util.UUIDToString(id)]; ok {
		return workspace, nil
	}
	return db.Workspace{}, errors.New("workspace not found")
}

func newTaskScopeFixture() *fakeTaskScopeQueries {
	workspaceID := util.MustParseUUID(taskScopeWorkspaceID)
	issueID := util.MustParseUUID(taskScopeIssueID)
	foreignIssueID := util.MustParseUUID(taskScopeForeignIssueID)
	issue := db.Issue{ID: issueID, WorkspaceID: workspaceID, Number: 75}
	foreignIssue := db.Issue{ID: foreignIssueID, WorkspaceID: workspaceID, Number: 76}
	return &fakeTaskScopeQueries{
		agents: map[string]db.Agent{
			taskScopeAgentID: {
				ID: util.MustParseUUID(taskScopeAgentID), WorkspaceID: workspaceID,
			},
		},
		tasks: map[string]db.AgentTaskQueue{
			taskScopeTaskID: {
				ID: util.MustParseUUID(taskScopeTaskID), AgentID: util.MustParseUUID(taskScopeAgentID),
				IssueID: issueID, Status: "running",
			},
			taskScopeHistoryTaskID: {
				ID: util.MustParseUUID(taskScopeHistoryTaskID), AgentID: util.MustParseUUID(taskScopeAgentID),
				IssueID: issueID, Status: "completed",
			},
			taskScopeForeignTaskID: {
				ID: util.MustParseUUID(taskScopeForeignTaskID), AgentID: util.MustParseUUID(taskScopeAgentID),
				IssueID: foreignIssueID, Status: "completed",
			},
		},
		comments: map[string]db.Comment{
			taskScopeCommentID: {
				ID: util.MustParseUUID(taskScopeCommentID), IssueID: issueID, WorkspaceID: workspaceID,
			},
			taskScopeForeignComment: {
				ID: util.MustParseUUID(taskScopeForeignComment), IssueID: foreignIssueID, WorkspaceID: workspaceID,
			},
		},
		issues: map[string]db.Issue{
			taskScopeIssueID:        issue,
			taskScopeForeignIssueID: foreignIssue,
		},
		byNumber: map[int32]db.Issue{75: issue, 76: foreignIssue},
		workspaces: map[string]db.Workspace{
			taskScopeWorkspaceID: {ID: workspaceID, IssuePrefix: "ATH"},
		},
	}
}

func serveTaskScopeRequest(t *testing.T, queries taskScopeQuerier, method, path, body string, mutate func(*http.Request)) (int, string, bool) {
	t.Helper()
	called := false
	handler := TaskTokenScopeGuard(queries)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", taskScopeTaskID)
	req.Header.Set("X-Agent-ID", taskScopeAgentID)
	req.Header.Set("X-Workspace-ID", taskScopeWorkspaceID)
	if mutate != nil {
		mutate(req)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String(), called
}

func TestTaskTokenScopeGuard_HumanCredentialsKeepExistingBehavior(t *testing.T) {
	called := false
	handler := TaskTokenScopeGuard(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPatch, "/api/workspaces/anything", nil)
	req.Header.Set("X-Actor-Source", "member")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("human request should pass unchanged, code=%d called=%v", recorder.Code, called)
	}
}

func TestTaskTokenScopeGuard_DeniesInactiveOrForeignAgentIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeTaskScopeQueries)
	}{
		{
			name: "archived agent",
			mutate: func(queries *fakeTaskScopeQueries) {
				agent := queries.agents[taskScopeAgentID]
				agent.ArchivedAt = pgtype.Timestamptz{Valid: true}
				queries.agents[taskScopeAgentID] = agent
			},
		},
		{
			name: "agent moved to another workspace",
			mutate: func(queries *fakeTaskScopeQueries) {
				agent := queries.agents[taskScopeAgentID]
				agent.WorkspaceID = util.MustParseUUID("00000000-0000-0000-0000-000000000999")
				queries.agents[taskScopeAgentID] = agent
			},
		},
		{
			name: "agent no longer exists",
			mutate: func(queries *fakeTaskScopeQueries) {
				delete(queries.agents, taskScopeAgentID)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			queries := newTaskScopeFixture()
			tc.mutate(queries)
			code, response, called := serveTaskScopeRequest(t, queries, http.MethodGet, "/api/issues/"+taskScopeIssueID, "", nil)
			if code != http.StatusForbidden || called || response != `{"error":"task_scope_denied"}` {
				t.Fatalf("expected task scope denial, code=%d called=%v body=%q", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_AllowsOnlyBoundIssueOperations(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct {
		name, method, path, body string
	}{
		{"get issue by uuid", http.MethodGet, "/api/issues/" + taskScopeIssueID, ""},
		{"get issue by identifier", http.MethodGet, "/api/issues/ATH-75", ""},
		{"list comments", http.MethodGet, "/api/issues/" + taskScopeIssueID + "/comments", ""},
		{"add comment", http.MethodPost, "/api/issues/" + taskScopeIssueID + "/comments", `{"content":"done","parent_id":null}`},
		{"reply to bound comment", http.MethodPost, "/api/issues/" + taskScopeIssueID + "/comments", `{"content":"done","parent_id":"` + taskScopeCommentID + `"}`},
		{"change status", http.MethodPut, "/api/issues/" + taskScopeIssueID, `{"status":"in_review"}`},
		{"read bound task messages", http.MethodGet, "/api/tasks/" + taskScopeTaskID + "/messages", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, response, called := serveTaskScopeRequest(t, queries, tc.method, tc.path, tc.body, nil)
			if code != http.StatusNoContent || !called {
				t.Fatalf("expected pass, code=%d called=%v body=%s", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_DeniesSameIssueTaskOrchestrationAndForeignTaskReads(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct {
		name, method, path, body string
	}{
		{"read same issue foreign task messages", http.MethodGet, "/api/tasks/" + taskScopeHistoryTaskID + "/messages", ""},
		{"rerun current assignee", http.MethodPost, "/api/issues/" + taskScopeIssueID + "/rerun", `{}`},
		{"rerun bound task", http.MethodPost, "/api/issues/" + taskScopeIssueID + "/rerun", `{"task_id":"` + taskScopeTaskID + `"}`},
		{"rerun same issue foreign task", http.MethodPost, "/api/issues/" + taskScopeIssueID + "/rerun", `{"task_id":"` + taskScopeHistoryTaskID + `"}`},
		{"enumerate same issue task runs", http.MethodGet, "/api/issues/" + taskScopeIssueID + "/task-runs", ""},
		{"cancel all same issue tasks", http.MethodPut, "/api/issues/" + taskScopeIssueID, `{"status":"cancelled"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, response, called := serveTaskScopeRequest(t, queries, tc.method, tc.path, tc.body, nil)
			if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
				t.Fatalf("expected task isolation denial, code=%d called=%v body=%s", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_DeniesWorkspaceAndManagementAPIs(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct {
		resource string
		method   string
		path     string
	}{
		{"workspace read", http.MethodGet, "/api/workspaces"},
		{"workspace write", http.MethodPatch, "/api/workspaces/" + taskScopeWorkspaceID},
		{"project read", http.MethodGet, "/api/projects"},
		{"project write", http.MethodPost, "/api/projects"},
		{"repo read", http.MethodGet, "/api/repos"},
		{"repo write", http.MethodPatch, "/api/workspaces/" + taskScopeWorkspaceID},
		{"agent read", http.MethodGet, "/api/agents"},
		{"agent write", http.MethodPatch, "/api/agents/" + taskScopeAgentID},
		{"runtime read", http.MethodGet, "/api/runtimes"},
		{"runtime write", http.MethodPost, "/api/runtimes"},
		{"autopilot read", http.MethodGet, "/api/autopilots"},
		{"autopilot write", http.MethodPost, "/api/autopilots"},
		{"skill read", http.MethodGet, "/api/skills"},
		{"skill write", http.MethodPost, "/api/skills"},
		{"squad read", http.MethodGet, "/api/squads"},
		{"squad write", http.MethodPost, "/api/squads"},
		{"account read", http.MethodGet, "/api/me"},
		{"token write", http.MethodPost, "/api/tokens"},
		{"upload write", http.MethodPost, "/api/upload-file"},
	}
	for _, tc := range tests {
		t.Run(tc.resource, func(t *testing.T) {
			code, response, called := serveTaskScopeRequest(t, queries, tc.method, tc.path, "", nil)
			if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
				t.Fatalf("expected task scope denial, code=%d called=%v body=%s", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_DeniesForeignIssueAndTaskAccess(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/issues/" + taskScopeForeignIssueID, ""},
		{http.MethodGet, "/api/issues/ATH-76/comments", ""},
		{http.MethodPost, "/api/issues/" + taskScopeForeignIssueID + "/comments", `{"content":"cross issue"}`},
		{http.MethodPut, "/api/issues/" + taskScopeForeignIssueID, `{"status":"done"}`},
		{http.MethodPost, "/api/issues/" + taskScopeForeignIssueID + "/rerun", `{}`},
		{http.MethodGet, "/api/issues/" + taskScopeForeignIssueID + "/task-runs", ""},
		{http.MethodGet, "/api/tasks/" + taskScopeForeignTaskID + "/messages", ""},
		{http.MethodPost, "/api/issues/" + taskScopeIssueID + "/rerun", `{"task_id":"` + taskScopeForeignTaskID + `"}`},
	}
	for _, tc := range tests {
		code, response, called := serveTaskScopeRequest(t, queries, tc.method, tc.path, tc.body, nil)
		if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
			t.Fatalf("expected task scope denial for %s %s, code=%d called=%v body=%s", tc.method, tc.path, code, called, response)
		}
	}
}

func TestTaskTokenScopeGuard_DeniesBodyWidening(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct{ method, path, body string }{
		{http.MethodPut, "/api/issues/" + taskScopeIssueID, `{"status":"done","project_id":"00000000-0000-0000-0000-000000000999"}`},
		{http.MethodPut, "/api/issues/" + taskScopeIssueID, `{"title":"changed"}`},
		{http.MethodPost, "/api/issues/" + taskScopeIssueID + "/comments", `{"content":"x","suppress_agent_ids":["` + taskScopeAgentID + `"]}`},
		{http.MethodPost, "/api/issues/" + taskScopeIssueID + "/rerun", `{"force":true}`},
	}
	for _, tc := range tests {
		code, response, called := serveTaskScopeRequest(t, queries, tc.method, tc.path, tc.body, nil)
		if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
			t.Fatalf("expected widened body denial, code=%d called=%v body=%s", code, called, response)
		}
	}
}

func TestTaskTokenScopeGuard_DeniesAllAssigneeMutations(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct {
		name string
		body string
	}{
		{"member", `{"assignee_type":"member","assignee_id":"` + taskScopeAgentID + `"}`},
		{"agent", `{"assignee_type":"agent","assignee_id":"` + taskScopeAgentID + `"}`},
		{"squad", `{"assignee_type":"squad","assignee_id":"` + taskScopeAgentID + `"}`},
		{"unassign", `{"assignee_type":null,"assignee_id":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, response, called := serveTaskScopeRequest(t, queries, http.MethodPut, "/api/issues/"+taskScopeIssueID, tc.body, nil)
			if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
				t.Fatalf("expected assignee mutation denial, code=%d called=%v body=%s", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_DeniesInvalidIssueMutationValues(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct{ name, body string }{
		{"status null", `{"status":null}`},
		{"status empty", `{"status":""}`},
		{"status number", `{"status":7}`},
		{"status unknown", `{"status":"active"}`},
		{"assignee empty object", `{}`},
		{"assignee missing id", `{"assignee_type":"agent"}`},
		{"assignee missing type", `{"assignee_id":"` + taskScopeAgentID + `"}`},
		{"assignee mixed null", `{"assignee_type":null,"assignee_id":"` + taskScopeAgentID + `"}`},
		{"assignee unknown type", `{"assignee_type":"runtime","assignee_id":"` + taskScopeAgentID + `"}`},
		{"assignee invalid uuid", `{"assignee_type":"agent","assignee_id":"agent-1"}`},
		{"assignee non canonical uuid", `{"assignee_type":"agent","assignee_id":"{` + taskScopeAgentID + `}"}`},
		{"duplicate key", `{"status":"done","status":"todo"}`},
		{"trailing json", `{"status":"done"}{"status":"todo"}`},
		{"array", `[{"status":"done"}]`},
		{"null body", `null`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, response, called := serveTaskScopeRequest(t, queries, http.MethodPut, "/api/issues/"+taskScopeIssueID, tc.body, nil)
			if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
				t.Fatalf("expected denial, code=%d called=%v body=%s", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_DeniesInvalidCommentValues(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct{ name, body string }{
		{"missing content", `{}`},
		{"empty content", `{"content":""}`},
		{"null content", `{"content":null}`},
		{"numeric content", `{"content":3}`},
		{"invalid parent uuid", `{"content":"x","parent_id":"comment-1"}`},
		{"foreign parent", `{"content":"x","parent_id":"` + taskScopeForeignComment + `"}`},
		{"parent object", `{"content":"x","parent_id":{}}`},
		{"extra key", `{"content":"x","type":"system"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, response, called := serveTaskScopeRequest(t, queries, http.MethodPost, "/api/issues/"+taskScopeIssueID+"/comments", tc.body, nil)
			if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
				t.Fatalf("expected denial, code=%d called=%v body=%s", code, called, response)
			}
		})
	}
}

func TestTaskTokenScopeGuard_DeniesInvalidRerunValues(t *testing.T) {
	queries := newTaskScopeFixture()
	for _, body := range []string{
		`{"task_id":null}`,
		`{"task_id":"task-1"}`,
		`{"task_id":7}`,
		`{"task_id":"{` + taskScopeHistoryTaskID + `}"}`,
	} {
		code, response, called := serveTaskScopeRequest(t, queries, http.MethodPost, "/api/issues/"+taskScopeIssueID+"/rerun", body, nil)
		if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
			t.Fatalf("expected rerun denial for %s, code=%d called=%v body=%s", body, code, called, response)
		}
	}
}

func TestTaskTokenScopeGuard_EnforcesQueryAllowlist(t *testing.T) {
	queries := newTaskScopeFixture()
	allowed := []string{
		"/api/issues/" + taskScopeIssueID + "/comments?since=1&thread=" + taskScopeCommentID + "&recent=1&tail=1&roots_only=false&summary=true&fold=true&before=cursor&before_id=" + taskScopeCommentID,
		"/api/tasks/" + taskScopeTaskID + "/messages?since=1",
	}
	for _, path := range allowed {
		code, response, called := serveTaskScopeRequest(t, queries, http.MethodGet, path, "", nil)
		if code != http.StatusNoContent || !called {
			t.Fatalf("expected allowed query %s, code=%d called=%v body=%s", path, code, called, response)
		}
	}

	denied := []string{
		"/api/issues/" + taskScopeIssueID + "?expand=workspace",
		"/api/issues/" + taskScopeIssueID + "/task-runs?include=agent",
		"/api/issues/" + taskScopeIssueID + "/comments?workspace_id=" + taskScopeWorkspaceID,
		"/api/issues/" + taskScopeIssueID + "/comments?unknown=1",
		"/api/issues/" + taskScopeIssueID + "/comments?since=1&since=2",
		"/api/tasks/" + taskScopeTaskID + "/messages?unknown=1",
		"/api/tasks/" + taskScopeTaskID + "/messages?since=1&since=2",
		"/api/tasks/" + taskScopeTaskID + "/messages?since=%zz",
		"/api/issues/" + taskScopeIssueID + "/comments?since=1;workspace_id=" + taskScopeWorkspaceID,
	}
	for _, path := range denied {
		code, response, called := serveTaskScopeRequest(t, queries, http.MethodGet, path, "", nil)
		if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
			t.Fatalf("expected query denial %s, code=%d called=%v body=%s", path, code, called, response)
		}
	}
}

func TestTaskTokenScopeGuard_DeniesAmbiguousPaths(t *testing.T) {
	queries := newTaskScopeFixture()
	paths := []string{
		"/api/issues/" + taskScopeIssueID + "/",
		"/api//issues/" + taskScopeIssueID,
		"/api/issues/./" + taskScopeIssueID,
		"/api/issues/../issues/" + taskScopeIssueID,
		"/api/issues/ATH-075",
		"/api/issues/ATH_75",
		"/api/issues/%7B" + taskScopeIssueID + "%7D",
		"/api/issues/ATH%2F75",
	}
	for _, path := range paths {
		code, response, called := serveTaskScopeRequest(t, queries, http.MethodGet, path, "", nil)
		if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
			t.Fatalf("expected path denial %s, code=%d called=%v body=%s", path, code, called, response)
		}
	}
}

func TestTaskTokenScopeGuard_DeniesOversizedBody(t *testing.T) {
	queries := newTaskScopeFixture()
	body := `{"content":"` + strings.Repeat("x", taskScopeMaxBodyBytes) + `"}`
	code, response, called := serveTaskScopeRequest(t, queries, http.MethodPost, "/api/issues/"+taskScopeIssueID+"/comments", body, nil)
	if code != http.StatusForbidden || called || !strings.Contains(response, "task_scope_denied") {
		t.Fatalf("expected oversized body denial, code=%d called=%v body=%s", code, called, response)
	}
}

func TestTaskTokenScopeGuard_DeniesForgedBindingHeadersAndInactiveTask(t *testing.T) {
	queries := newTaskScopeFixture()
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"agent", func(r *http.Request) { r.Header.Set("X-Agent-ID", "00000000-0000-0000-0000-000000000999") }},
		{"workspace", func(r *http.Request) { r.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000999") }},
		{"task", func(r *http.Request) { r.Header.Set("X-Task-ID", taskScopeForeignTaskID) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, called := serveTaskScopeRequest(t, queries, http.MethodGet, "/api/issues/"+taskScopeIssueID, "", tc.mutate)
			if code != http.StatusForbidden || called {
				t.Fatalf("expected forged %s binding to be denied, code=%d called=%v", tc.name, code, called)
			}
		})
	}

	task := queries.tasks[taskScopeTaskID]
	task.Status = "completed"
	queries.tasks[taskScopeTaskID] = task
	code, _, called := serveTaskScopeRequest(t, queries, http.MethodGet, "/api/issues/"+taskScopeIssueID, "", nil)
	if code != http.StatusForbidden || called {
		t.Fatalf("completed token-bound task must be denied, code=%d called=%v", code, called)
	}
}
