package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvisionAgentBrowserAuthRejectsNonTaskToken(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/cerebro/agent-browser/provision-auth", strings.NewReader(`{
		"vault":"Shared/browser-login/registry",
		"username_key":"EMAIL",
		"password_key":"PASSWORD",
		"host":"https://registry.firtal.com/login"
	}`))
	rec := httptest.NewRecorder()

	h.ProvisionAgentBrowserAuth(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-task-token status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "PASSWORD") {
		t.Fatal("response leaked a credential key")
	}
}
