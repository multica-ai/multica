package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestIssuePlatformActionsKeepTaskIssueScope(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		method string
	}{
		{name: "update_issue", method: http.MethodPut},
		{name: "add_comment", method: http.MethodPost},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := chi.NewRouter()
			r.With(AllowTaskScopeForIssue("id")).Method(tt.method, "/api/issues/{id}", pass)

			req := httptest.NewRequest(tt.method, "/api/issues/other-issue", nil)
			req = req.WithContext(withTaskScope(req.Context(), TaskScopeContext{
				TaskID: "task", IssueID: "bound-issue", AgentID: "agent", WorkspaceID: "workspace",
			}))
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s cross-issue request status=%d, want 403", tt.name, rec.Code)
			}
		})
	}
}

func TestIssuePlatformActionGateStillRunsWhenTaskScopeFeatureIsOff(t *testing.T) {
	t.Parallel()

	permissionGate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"platform_action_denied"}`, http.StatusForbidden)
		})
	}
	r := chi.NewRouter()
	r.With(AllowTaskScopeForIssue("id"), permissionGate).Put("/api/issues/{id}", pass.ServeHTTP)

	req := httptest.NewRequest(http.MethodPut, "/api/issues/other-issue", nil)
	ctx := withTaskScope(req.Context(), TaskScopeContext{IssueID: "bound-issue"})
	req = req.WithContext(withTaskScopeEnforcement(ctx, false))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || rec.Body.String() != "{\"error\":\"platform_action_denied\"}\n" {
		t.Fatalf("permission gate result=(%d, %q), want retained 403 platform_action_denied", rec.Code, rec.Body.String())
	}
}
