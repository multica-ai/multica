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
