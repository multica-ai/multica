package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/tasktoken"
)

// jwksHandler builds a Handler carrying just the issuer. The endpoint reads no
// database and no session, so it needs nothing else — and this keeps the test
// runnable without a database.
func jwksHandler(t *testing.T, catalog string) *Handler {
	t.Helper()
	if catalog == "" {
		return &Handler{}
	}
	iss, err := tasktoken.NewIssuer(catalog, testTaskTokenKeyPEM(t), "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	return &Handler{TaskTokenIssuer: iss}
}

func TestJWKSEndpointIs404WhenTaskTokensAreNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	jwksHandler(t, "").GetTaskTokenJWKS(rec, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — an unconfigured deployment should not advertise the endpoint", rec.Code)
	}
}

func TestJWKSEndpointServesThePublicKeys(t *testing.T) {
	catalog := `[{"id":"erp","label":"ERP","env":"BOT_TOKEN_ERP","key_id":"erp-2026","claims":{"sub":"{{identity.email_local}}"}}]`
	rec := httptest.NewRecorder()
	jwksHandler(t, catalog).GetTaskTokenJWKS(rec, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Error("Cache-Control is empty; verifiers refetch the set per request without it")
	}

	var set tasktoken.JWKSet
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	if len(set.Keys) != 1 {
		t.Fatalf("served %d keys, want 1", len(set.Keys))
	}
	if set.Keys[0].Kid != "erp-2026" {
		t.Errorf("kid = %q, want erp-2026", set.Keys[0].Kid)
	}
	if set.Keys[0].Kty != "EC" {
		t.Errorf("kty = %q, want EC", set.Keys[0].Kty)
	}
}
