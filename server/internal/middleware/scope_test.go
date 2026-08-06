package middleware

// CEREBRO-PATCH(middleware-scope-test): cerebro modification of upstream file

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

type taskScopeFlagReaderStub struct {
	// CEREBRO-PATCH(task-scope-feature-flag): FIR-4076 focused default/on/off coverage.
	rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	err  error
}

func (s taskScopeFlagReaderStub) ListCerebroWorkspaceFeatureFlags(context.Context, pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	return s.rows, s.err
}

// pass is a tiny handler that returns 200 OK so the test asserts on
// "did the middleware reach me or not".
var pass = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// CEREBRO-PATCH(task-scope-hotfix): lock the temporary FIR-4076 pass-through.
func TestRequireUserScope_CompatibilityModeAllowsTaskScope(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/anything", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{TaskID: "t", IssueID: "i", AgentID: "a", WorkspaceID: "w"}))
	rec := httptest.NewRecorder()

	RequireUserScope(pass).ServeHTTP(rec, req)

	// CEREBRO-PATCH(task-scope-hotfix): lock the temporary compatibility verdict.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for task-scoped request during FIR-4076 compatibility mode, got %d", rec.Code)
	}
}

func TestRequireUserScope_AllowsUserScope(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/anything", nil)
	req = req.WithContext(withUserScope(req.Context()))
	rec := httptest.NewRecorder()

	RequireUserScope(pass).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for user-scoped request, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForIssue_BlocksMismatchedIssue(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForIssue("id")).Get("/api/issues/{id}", pass.ServeHTTP)

	// Task is bound to issue "ours", request hits issue "theirs".
	req := httptest.NewRequest("GET", "/api/issues/theirs", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{IssueID: "ours"}))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched issue, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForIssue_FeatureOffAllowsMismatchedIssue(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForIssue("id")).Get("/api/issues/{id}", pass.ServeHTTP)

	req := httptest.NewRequest("GET", "/api/issues/theirs", nil)
	ctx := withTaskScope(req.Context(), TaskScopeContext{IssueID: "ours"})
	req = req.WithContext(withTaskScopeEnforcement(ctx, false))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for mismatched issue while cerebro_task_scope_enforcement is off, got %d", rec.Code)
	}
}

func TestResolveTaskScopeEnforcement_DefaultAndExplicitOnBlock(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		reader taskScopeFlagReaderStub
	}{
		{name: "missing row defaults on"},
		{name: "lookup failure defaults on", reader: taskScopeFlagReaderStub{err: errors.New("unavailable")}},
		{name: "explicit on", reader: taskScopeFlagReaderStub{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{FlagKey: taskScopeEnforcementFlagKey, Enabled: true}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.Use(ResolveTaskScopeEnforcement(tt.reader))
			r.With(AllowTaskScopeForIssue("id")).Get("/api/issues/{id}", pass.ServeHTTP)

			req := httptest.NewRequest("GET", "/api/issues/theirs", nil)
			req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{
				IssueID: "ours", WorkspaceID: "11111111-1111-1111-1111-111111111111",
			}))
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected default/on enforcement to return 403, got %d", rec.Code)
			}
		})
	}
}

func TestResolveTaskScopeEnforcement_ExplicitOffAllows(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.Use(ResolveTaskScopeEnforcement(taskScopeFlagReaderStub{rows: []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{{
		FlagKey: taskScopeEnforcementFlagKey, Enabled: false,
	}}}))
	r.With(AllowTaskScopeForIssue("id")).Get("/api/issues/{id}", pass.ServeHTTP)

	req := httptest.NewRequest("GET", "/api/issues/theirs", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{
		IssueID: "ours", WorkspaceID: "11111111-1111-1111-1111-111111111111",
	}))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected explicit off to return 200, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForIssue_AllowsMatchingIssue(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForIssue("id")).Get("/api/issues/{id}", pass.ServeHTTP)

	req := httptest.NewRequest("GET", "/api/issues/ours", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{IssueID: "ours"}))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for matching issue, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForIssue_PassesUserScopeUntouched(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForIssue("id")).Get("/api/issues/{id}", pass.ServeHTTP)

	// User-scoped requests should pass regardless of URL parameter,
	// because regular auth has already gated workspace membership.
	req := httptest.NewRequest("GET", "/api/issues/something-else", nil)
	req = req.WithContext(withUserScope(req.Context()))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for user-scoped request, got %d", rec.Code)
	}
}

// CEREBRO-PATCH(agent-capabilities-card-task-route): FIR-2243 — verify an agent
// task token reaches the capabilities route only for its OWN agent id (the
// security invariant behind self-capability lookup), and user scope passes
// through untouched.
func TestAllowTaskScopeForAgent_BlocksMismatchedAgent(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForAgent("id")).Get("/api/agents/{id}/capabilities", pass.ServeHTTP)

	// Task is bound to agent "ours", request hits agent "theirs".
	req := httptest.NewRequest("GET", "/api/agents/theirs/capabilities", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{AgentID: "ours"}))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched agent, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForAgent_FeatureOffAllowsMismatchedAgent(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForAgent("id")).Get("/api/agents/{id}/capabilities", pass.ServeHTTP)

	req := httptest.NewRequest("GET", "/api/agents/theirs/capabilities", nil)
	ctx := withTaskScope(req.Context(), TaskScopeContext{AgentID: "ours"})
	req = req.WithContext(withTaskScopeEnforcement(ctx, false))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for mismatched agent while cerebro_task_scope_enforcement is off, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForWorkspace_FeatureOffAllowsMismatchedWorkspace(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForWorkspace("id")).Get("/api/workspaces/{id}/members", pass.ServeHTTP)

	req := httptest.NewRequest("GET", "/api/workspaces/theirs/members", nil)
	ctx := withTaskScope(req.Context(), TaskScopeContext{WorkspaceID: "ours"})
	req = req.WithContext(withTaskScopeEnforcement(ctx, false))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for mismatched workspace while cerebro_task_scope_enforcement is off, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForAgent_AllowsOwnAgent(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForAgent("id")).Get("/api/agents/{id}/capabilities", pass.ServeHTTP)

	req := httptest.NewRequest("GET", "/api/agents/ours/capabilities", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{AgentID: "ours"}))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for own agent, got %d", rec.Code)
	}
}

func TestAllowTaskScopeForAgent_PassesUserScopeUntouched(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	r.With(AllowTaskScopeForAgent("id")).Get("/api/agents/{id}/capabilities", pass.ServeHTTP)

	// User-scoped requests pass regardless of URL id — regular auth has
	// already gated workspace membership.
	req := httptest.NewRequest("GET", "/api/agents/anything/capabilities", nil)
	req = req.WithContext(withUserScope(req.Context()))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for user-scoped request, got %d", rec.Code)
	}
}
