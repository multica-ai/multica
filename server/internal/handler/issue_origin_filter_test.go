package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// origin_type narrows the list to one provenance label, so the Secretary desk
// can read directive issues without pulling the whole workspace.
func TestListIssuesOriginTypeFilter(t *testing.T) {
	directiveID := createMetadataTestIssue(t, "Directive issue")
	plainID := createMetadataTestIssue(t, "Plain issue")

	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET origin_type = 'directive' WHERE id = $1`, directiveID); err != nil {
		t.Fatalf("seed origin_type: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", `/api/issues?origin_type=directive`, nil)
	testHandler.ListIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list by origin_type: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	foundDirective := false
	for _, iss := range listResp.Issues {
		if iss.ID == plainID {
			t.Errorf("origin filter leaked: plain issue %s appeared in directive result", plainID)
		}
		if iss.ID == directiveID {
			foundDirective = true
		}
	}
	if !foundDirective {
		t.Errorf("directive issue %s missing from origin_type=directive result", directiveID)
	}
}
