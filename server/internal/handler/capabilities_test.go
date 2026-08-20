package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCapabilitiesAdvertisesCommentBranchV1(t *testing.T) {
	w := httptest.NewRecorder()
	(&Handler{}).GetCapabilities(w, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("GetCapabilities status = %d, want 200", w.Code)
	}
	var response map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if !response["comment_branch_v1"] {
		t.Fatalf("comment_branch_v1 = %v, want true", response["comment_branch_v1"])
	}
}
