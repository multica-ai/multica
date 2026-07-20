package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
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

	writeInternalBrowserVerificationError(rec, internalbrowserqa.Result{
		App: "registry", InternalHost: "firtal-data-registry-private.internal:3000",
		FailureStage: "auth", FailureCause: "not-found",
	}, errors.New("internal browser stage auth failed"))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "internal browser stage auth failed") {
		t.Fatalf("body = %q, want safe stage", body)
	}
	// The diagnostics are the point of the 422: without the attempted address and
	// the cause, a caller is back to guessing which of six apps is misconfigured.
	for _, want := range []string{"firtal-data-registry-private.internal:3000", `"failure_stage":"auth"`, `"failure_cause":"not-found"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want it to contain %q", body, want)
		}
	}
}

func TestWriteInternalBrowserVerificationErrorRejectsUnsafeDetail(t *testing.T) {
	rec := httptest.NewRecorder()

	writeInternalBrowserVerificationError(rec, internalbrowserqa.Result{},
		errors.New("password=must-not-escape"))

	if body := rec.Body.String(); strings.Contains(body, "must-not-escape") {
		t.Fatalf("body leaked an unsafe error detail: %q", body)
	}
}
