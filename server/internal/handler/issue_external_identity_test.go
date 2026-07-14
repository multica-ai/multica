package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpsertIssueExternalIdentityRejectsInvalidAliasBeforeDB(t *testing.T) {
	req := newRequest(http.MethodPost, "/api/issues/upsert-external", map[string]any{
		"aliases": []map[string]any{{"namespace": "GitHub", "external_id": "123"}},
		"create":  map[string]any{"title": "Imported"},
	})
	w := httptest.NewRecorder()

	testHandler.UpsertIssueExternalIdentity(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestUpsertIssueExternalIdentityRejectsTrailingJSONBeforeDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", strings.NewReader(
		`{"aliases":[{"namespace":"github","external_id":"123"}],"create":{"title":"Imported"}} {}`,
	))
	w := httptest.NewRecorder()

	testHandler.UpsertIssueExternalIdentity(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid request body") {
		t.Fatalf("status = %d body=%s, want 400 invalid request body", w.Code, w.Body.String())
	}
}
