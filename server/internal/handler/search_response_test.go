package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchIssuesOmitsExactTotal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	recorder := httptest.NewRecorder()
	testHandler.SearchIssues(recorder, newRequest(http.MethodGet, "/api/issues/search?q=exact-total-contract-test", nil))
	assertSearchResponseOmitsExactTotal(t, recorder, "issues")
}

func TestSearchProjectsOmitsExactTotal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	recorder := httptest.NewRecorder()
	testHandler.SearchProjects(recorder, newRequest(http.MethodGet, "/api/projects/search?q=exact-total-contract-test", nil))
	assertSearchResponseOmitsExactTotal(t, recorder, "projects")
}

func assertSearchResponseOmitsExactTotal(t *testing.T, recorder *httptest.ResponseRecorder, collectionKey string) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Total-Count"); got != "" {
		t.Fatalf("search X-Total-Count = %q, want absent", got)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode search payload: %v", err)
	}
	if _, ok := payload[collectionKey]; !ok {
		t.Fatalf("search response missing %q: %s", collectionKey, recorder.Body.String())
	}
	if _, ok := payload["total"]; ok {
		t.Fatalf("search response unexpectedly contains exact total: %s", recorder.Body.String())
	}
}
