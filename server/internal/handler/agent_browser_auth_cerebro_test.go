package handler

import (
	"errors"
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

func TestVerifyInternalAgentBrowserRejectsNonTaskToken(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/cerebro/agent-browser/internal-verify", strings.NewReader(`{"app":"registry"}`))
	rec := httptest.NewRecorder()

	h.VerifyInternalAgentBrowser(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-task-token status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "registry-test") {
		t.Fatal("response leaked credential material")
	}
}

func TestWriteInternalBrowserVerificationErrorPreservesSafeStage(t *testing.T) {
	rec := httptest.NewRecorder()

	writeInternalBrowserVerificationError(rec, errors.New("internal browser stage auth failed"))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if body := rec.Body.String(); !strings.Contains(body, "internal browser stage auth failed") {
		t.Fatalf("body = %q, want safe stage", body)
	}
}
