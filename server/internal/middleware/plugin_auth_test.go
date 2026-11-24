package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

func TestPluginBearerOnlyRejectsNonPluginCredentials(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing"},
		{name: "session jwt", authorization: "Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature"},
		{name: "personal access token", authorization: "Bearer mul_personal"},
		{name: "wrong scheme", authorization: "Basic mpi_installation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/context", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			response := httptest.NewRecorder()

			PluginBearerOnly(next).ServeHTTP(response, req)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
			if called {
				t.Fatal("non-plugin credential reached the Action API handler")
			}
			if got := response.Header().Get("Content-Type"); got != publicapiv1.ProblemContentType {
				t.Fatalf("Content-Type = %q", got)
			}
			var problem publicapiv1.Problem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Code != "plugin_bearer_required" || problem.Error == "" || problem.RequestID == "" {
				t.Fatalf("unexpected problem: %+v", problem)
			}
		})
	}
}

func TestPluginBearerOnlyPassesPluginTokenKindsToTheHandler(t *testing.T) {
	for _, token := range []string{"mpi_installation", "mpc_callback"} {
		t.Run(token[:3], func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/context", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()

			PluginBearerOnly(next).ServeHTTP(response, req)

			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
			if !called {
				t.Fatal("plugin credential did not reach the Action API handler")
			}
		})
	}
}
