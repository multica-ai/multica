package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecureFillPersonalBrowserRejectsDirectAgentCall(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/cerebro/personal-browser/secure-fill", strings.NewReader(`{"action":"secure-fill"}`))
	req.Header.Set("X-Actor-Source", "task_token")
	rec := httptest.NewRecorder()
	h.SecureFillPersonalBrowser(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("direct task-token call status = %d, want 403", rec.Code)
	}
}
