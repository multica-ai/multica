package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
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

// FIR-4447: Agent Vault's own vault list — and Cerebro's Permissions table with
// it — shows the flattened box name `shared-browser-login-<app>`, so that is the
// value a caller naturally reaches for. It used to fail late inside
// RevealCredential as a 502 that Cloudflare replaced with its own error page,
// which is why the real cause stayed hidden for days. It must fail at the
// boundary with a 400 that names the accepted format.
func TestProvisionAgentBrowserAuthRejectsFlatVaultNameWith400(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/cerebro/agent-browser/provision-auth", strings.NewReader(`{
		"vault":"shared-browser-login-registry",
		"username_key":"USERNAME",
		"password_key":"PASSWORD",
		"host":"https://registry.firtal.com/auth/login"
	}`))
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", "4d8b4a77-e0df-4d5d-b279-a13587e8ff74")
	req.Header.Set("X-Workspace-ID", "ecb5a399-e7ab-49cc-ac18-2b5f7bad3fd0")
	rec := httptest.NewRecorder()

	h.ProvisionAgentBrowserAuth(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("flat vault name status = %d, want 400", rec.Code)
	}
	// writeJSON HTML-escapes the <app> placeholder, so match on the prefix that
	// survives escaping — the part that actually tells the caller what to type.
	if !strings.Contains(rec.Body.String(), "vault must be "+agentvault.BrowserLoginVaultPrefix) {
		t.Fatalf("body = %q, want it to name the accepted vault format", rec.Body.String())
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
