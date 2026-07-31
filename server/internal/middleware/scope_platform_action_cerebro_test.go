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
