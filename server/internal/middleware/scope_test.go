package middleware

// CEREBRO-PATCH(middleware-scope-test): cerebro modification of upstream file

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// pass is a tiny handler that returns 200 OK so the test asserts on
// "did the middleware reach me or not".
var pass = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRequireUserScope_RejectsTaskScope(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/anything", nil)
	req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{TaskID: "t", IssueID: "i", AgentID: "a", WorkspaceID: "w"}))
	rec := httptest.NewRecorder()

	RequireUserScope(pass).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for task-scoped request, got %d", rec.Code)
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
