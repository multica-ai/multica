package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/authority"
)

func TestAuthorityAttestDisabledIsPublicAndNoStore(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(`{"nonce":"`+base64.RawURLEncoding.EncodeToString(make([]byte, 32))+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	testHandler.AttestAuthority(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

type authorityLimiterFunc func(context.Context, string) bool

func (f authorityLimiterFunc) Allow(ctx context.Context, key string) bool {
	return f(ctx, key)
}

func TestAuthorityAttestFailsClosedWhenLimiterUnavailable(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	oldSigner := testHandler.AuthoritySigner
	oldLimiter := testHandler.AuthorityRateLimiter
	testHandler.AuthoritySigner = &authority.Signer{AuthorityID: "local-dev-authority", PrivateKey: priv, PublicKey: priv.Public().(ed25519.PublicKey)}
	testHandler.AuthorityRateLimiter = nil
	t.Cleanup(func() {
		testHandler.AuthoritySigner = oldSigner
		testHandler.AuthorityRateLimiter = oldLimiter
	})

	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(`{"nonce":"`+nonce+`"}`))
	w := httptest.NewRecorder()
	testHandler.AttestAuthority(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestAuthorityAttestRateLimitedBeforeDatabaseWork(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	oldSigner := testHandler.AuthoritySigner
	oldLimiter := testHandler.AuthorityRateLimiter
	testHandler.AuthoritySigner = &authority.Signer{AuthorityID: "local-dev-authority", PrivateKey: priv, PublicKey: priv.Public().(ed25519.PublicKey)}
	testHandler.AuthorityRateLimiter = authorityLimiterFunc(func(context.Context, string) bool { return false })
	t.Cleanup(func() {
		testHandler.AuthoritySigner = oldSigner
		testHandler.AuthorityRateLimiter = oldLimiter
	})

	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(`{"nonce":"`+nonce+`"}`))
	w := httptest.NewRecorder()
	testHandler.AttestAuthority(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s, want 429", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestAuthorityAttestRejectsTrailingJSON(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	oldSigner := testHandler.AuthoritySigner
	testHandler.AuthoritySigner = &authority.Signer{AuthorityID: "local-dev-authority", PrivateKey: priv, PublicKey: priv.Public().(ed25519.PublicKey)}
	t.Cleanup(func() { testHandler.AuthoritySigner = oldSigner })

	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(`{"nonce":"`+nonce+`"} {"nonce":"`+nonce+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	testHandler.AttestAuthority(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestAuthorityAttestRejectsMalformedNonce(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_ = pub
	oldSigner := testHandler.AuthoritySigner
	oldCommit := testHandler.ServerCommit
	testHandler.AuthoritySigner = &authority.Signer{AuthorityID: "local-dev-authority", PrivateKey: priv, PublicKey: priv.Public().(ed25519.PublicKey)}
	testHandler.ServerCommit = "test-commit"
	t.Cleanup(func() {
		testHandler.AuthoritySigner = oldSigner
		testHandler.ServerCommit = oldCommit
	})

	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(`{"nonce":"abc="}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	testHandler.AttestAuthority(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestAuthorityAttestSignsAndRejectsReplay(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	oldSigner := testHandler.AuthoritySigner
	oldCommit := testHandler.ServerCommit
	testHandler.AuthoritySigner = &authority.Signer{AuthorityID: "local-dev-authority", PrivateKey: priv, PublicKey: pub}
	testHandler.ServerCommit = "test-commit"
	t.Cleanup(func() {
		testHandler.AuthoritySigner = oldSigner
		testHandler.ServerCommit = oldCommit
	})

	nonce, err := authority.GenerateNonce(rand.Reader)
	if err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	body := `{"nonce":"` + nonce + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.AttestAuthority(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	var att authority.Attestation
	if err := json.NewDecoder(w.Body).Decode(&att); err != nil {
		t.Fatalf("decode attestation: %v", err)
	}
	if att.Nonce != nonce {
		t.Fatalf("nonce = %q, want %q", att.Nonce, nonce)
	}
	pin := authority.Pin{
		ServerURL:   "http://api.test",
		PublicKey:   authority.EncodePublicKey(pub),
		AuthorityID: "local-dev-authority",
		DBIdentity:  att.DBIdentity,
	}
	if err := authority.Verify(att, pin, "http://api.test", time.Now(), 2*time.Minute, 30*time.Second); err != nil {
		t.Fatalf("verify signed attestation: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	testHandler.AttestAuthority(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("replay status = %d body=%s, want 409", w2.Code, w2.Body.String())
	}
	var conflict struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Code != "authority_nonce_replay" {
		t.Fatalf("conflict code = %q", conflict.Code)
	}
}
