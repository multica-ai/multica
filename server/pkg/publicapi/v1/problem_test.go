package publicapiv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblemKeepsLegacyErrorAndStableFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/issues/MUL-1", nil)
	req.Header.Set("X-Request-ID", "request-123")
	response := httptest.NewRecorder()

	WriteProblem(response, req, http.StatusForbidden, "missing_scope", "issues:read is required")

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != ProblemContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header().Get("X-Request-Id"); got != "request-123" {
		t.Fatalf("X-Request-Id = %q", got)
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "missing_scope" || problem.Detail != "issues:read is required" || problem.Error != problem.Detail {
		t.Fatalf("unexpected problem: %+v", problem)
	}
	if problem.RequestID != "request-123" || problem.Type != "urn:multica:problem:missing_scope" {
		t.Fatalf("unexpected identity fields: %+v", problem)
	}
}
