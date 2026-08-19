package tagaccess

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type assertionClock struct{ now time.Time }

func (c assertionClock) Now() time.Time { return c.now }

func setSignedHTTPAssertion(t *testing.T, request *http.Request, assertion HTTPAssertion, key []byte) {
	t.Helper()
	payload, err := json.Marshal(assertion)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalHTTPAssertion(assertion)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	request.Header.Set(HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(payload))
	request.Header.Set(HTTPAssertionSignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set(HTTPAssertionKeyIDHeader, assertion.KeyID)
}

func fixtureHTTPAssertion(now time.Time) HTTPAssertion {
	return HTTPAssertion{
		SchemaVersion:              HTTPAssertionSchemaVersion,
		Issuer:                     HTTPAssertionIssuer,
		Audience:                   HTTPAssertionAudience,
		KeyID:                      "gateway-primary",
		Method:                     http.MethodPost,
		Path:                       "/api/issues",
		Query:                      "b=2&a=1&a=3",
		BodySHA256:                 hex.EncodeToString(sha256.New().Sum(nil)),
		UserID:                     "vibes-user-1",
		WorkspaceID:                "vibes-workspace-1",
		SessionID:                  "vibes-session-1",
		AccountEpoch:               7,
		SessionWorkspaceGeneration: 5,
		AuthorityVersion:           11,
		MembershipGeneration:       3,
		IssuedAt:                   now.Add(-time.Second).UnixMilli(),
		ExpiresAt:                  now.Add(4 * time.Second).UnixMilli(),
		RequestID:                  "request-1",
		Nonce:                      "nonce-1",
	}
}

func TestHTTPAssertionVerifierBindsIdentityMethodTargetAndBody(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("dedicated-gateway-assertion-key-32-bytes")
	verifier, err := NewHTTPAssertionVerifier(map[string][]byte{"gateway-primary": key}, assertionClock{now})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"title":"hello"}`)
	assertion := fixtureHTTPAssertion(now)
	digest := sha256.Sum256(body)
	assertion.BodySHA256 = hex.EncodeToString(digest[:])
	request := httptest.NewRequest(http.MethodPost, "/api/issues?b=2&a=1&a=3", bytes.NewReader(body))
	setSignedHTTPAssertion(t, request, assertion, key)

	verified, err := verifier.VerifyRequest(request)
	if err != nil {
		t.Fatalf("VerifyRequest() error = %v", err)
	}
	if verified.UserID != assertion.UserID || verified.WorkspaceID != assertion.WorkspaceID || verified.SessionID != assertion.SessionID || verified.SessionWorkspaceGeneration != 5 {
		t.Fatalf("verified identity = %#v", verified)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil || !bytes.Equal(restored, body) {
		t.Fatalf("request body was not restored: %q, err=%v", restored, err)
	}

	for name, mutate := range map[string]func(*http.Request){
		"method": func(r *http.Request) { r.Method = http.MethodPatch },
		"path":   func(r *http.Request) { r.URL.Path = "/api/agents" },
		"query":  func(r *http.Request) { r.URL.RawQuery = "b=2&a=3&a=1" },
		"body":   func(r *http.Request) { r.Body = io.NopCloser(bytes.NewReader([]byte(`{"title":"tampered"}`))) },
	} {
		t.Run(name, func(t *testing.T) {
			tampered := httptest.NewRequest(http.MethodPost, "/api/issues?b=2&a=1&a=3", bytes.NewReader(body))
			setSignedHTTPAssertion(t, tampered, assertion, key)
			mutate(tampered)
			if _, err := verifier.VerifyRequest(tampered); err == nil {
				t.Fatal("tampered request was authorized")
			}
		})
	}
}

func TestHTTPAssertionVerifierFailsClosedOnTimeSignatureTransportAndConfiguration(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	key := []byte("dedicated-gateway-assertion-key-32-bytes")
	verifier, err := NewHTTPAssertionVerifier(map[string][]byte{"gateway-primary": key}, assertionClock{now})
	if err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*HTTPAssertion){
		"expired at boundary": func(a *HTTPAssertion) { a.ExpiresAt = now.UnixMilli() },
		"future":              func(a *HTTPAssertion) { a.IssuedAt = now.Add(time.Millisecond).UnixMilli() },
		"long": func(a *HTTPAssertion) {
			a.ExpiresAt = time.UnixMilli(a.IssuedAt).Add(HTTPAssertionMaxLifetime + time.Millisecond).UnixMilli()
		},
		"issuer":   func(a *HTTPAssertion) { a.Issuer = "private-multica" },
		"audience": func(a *HTTPAssertion) { a.Audience = "vibes-tag-browser-ws-v1" },
		"key":      func(a *HTTPAssertion) { a.KeyID = "unknown" },
		"schema":   func(a *HTTPAssertion) { a.SchemaVersion = 2 },
		"session workspace generation": func(a *HTTPAssertion) {
			a.SessionWorkspaceGeneration = 0
		},
		"nonce": func(a *HTTPAssertion) { a.Nonce = "" },
	} {
		t.Run(name, func(t *testing.T) {
			assertion := fixtureHTTPAssertion(now)
			assertion.Method, assertion.Path, assertion.Query, assertion.BodySHA256 = http.MethodGet, "/api/me", "", ""
			mutate(&assertion)
			request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			payload, _ := json.Marshal(assertion)
			request.Header.Set(HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(payload))
			request.Header.Set(HTTPAssertionSignatureHeader, base64.RawURLEncoding.EncodeToString(make([]byte, sha256.Size)))
			request.Header.Set(HTTPAssertionKeyIDHeader, assertion.KeyID)
			if _, err := verifier.VerifyRequest(request); err == nil {
				t.Fatal("invalid assertion was authorized")
			}
		})
	}

	assertion := fixtureHTTPAssertion(now)
	assertion.Method, assertion.Path, assertion.Query, assertion.BodySHA256 = http.MethodGet, "/api/me", "", ""
	forged := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	setSignedHTTPAssertion(t, forged, assertion, []byte("different-dedicated-key-32-bytes!!"))
	if _, err := verifier.VerifyRequest(forged); err == nil {
		t.Fatal("forged signature was authorized")
	}

	for name, mutate := range map[string]func(*http.Request){
		"missing signature": func(r *http.Request) { r.Header.Del(HTTPAssertionSignatureHeader) },
		"missing key id":    func(r *http.Request) { r.Header.Del(HTTPAssertionKeyIDHeader) },
		"duplicate payload": func(r *http.Request) { r.Header.Add(HTTPAssertionHeader, r.Header.Get(HTTPAssertionHeader)) },
		"duplicate signature": func(r *http.Request) {
			r.Header.Add(HTTPAssertionSignatureHeader, r.Header.Get(HTTPAssertionSignatureHeader))
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			setSignedHTTPAssertion(t, request, assertion, key)
			mutate(request)
			if _, err := verifier.VerifyRequest(request); err == nil {
				t.Fatal("invalid assertion transport was authorized")
			}
		})
	}

	getWithBody := httptest.NewRequest(http.MethodGet, "/api/me", strings.NewReader("not-bound"))
	setSignedHTTPAssertion(t, getWithBody, assertion, key)
	if _, err := verifier.VerifyRequest(getWithBody); err == nil {
		t.Fatal("safe request with an unsigned body was authorized")
	}

	trace := fixtureHTTPAssertion(now)
	trace.Method, trace.Path, trace.Query, trace.BodySHA256 = http.MethodTrace, "/api/me", "", ""
	traceRequest := httptest.NewRequest(http.MethodTrace, "/api/me", nil)
	setSignedHTTPAssertion(t, traceRequest, trace, key)
	if _, err := verifier.VerifyRequest(traceRequest); err != nil {
		t.Fatalf("TRACE should use the provider safe-method contract: %v", err)
	}

	if _, err := NewHTTPAssertionVerifier(nil, assertionClock{now}); err == nil {
		t.Fatal("missing key configuration was accepted")
	}
	if _, err := NewHTTPAssertionVerifier(map[string][]byte{"gateway-primary": []byte("short")}, assertionClock{now}); err == nil {
		t.Fatal("weak key configuration was accepted")
	}
	overflowLifetime := assertion
	overflowLifetime.IssuedAt = 1
	overflowLifetime.ExpiresAt = int64(^uint64(0) >> 1)
	if _, err := CanonicalHTTPAssertion(overflowLifetime); err == nil {
		t.Fatal("overflowing assertion lifetime was accepted")
	}
}

func TestCanonicalHTTPRequestTargetMatchesVIBESOrderAndPercentEncoding(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/items?value=a+b&path=%2f&a=first&a=second", nil)
	path, query, err := CanonicalHTTPRequestTarget(request)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/items" || query != "value=a%20b&path=%2F&a=first&a=second" {
		t.Fatalf("target = %q ? %q", path, query)
	}

	request.URL.RawQuery = "bad=%zz"
	if _, _, err := CanonicalHTTPRequestTarget(request); err == nil {
		t.Fatal("malformed query was canonicalized")
	}
}

func TestCanonicalHTTPRequestTargetMatchesVIBESWHATWGEdgeCases(t *testing.T) {
	for _, testCase := range []struct {
		target string
		path   string
		query  string
	}{
		{target: "/a/../b?&&a=1&&b=2&", path: "/b", query: "a=1&b=2"},
		{target: "/a/%2e%2e/b", path: "/b"},
		{target: "/a/..//b", path: "//b"},
		{target: "/a/.", path: "/a/"},
		{target: "/a/..", path: "/"},
		{target: "/a/%2E./b", path: "/b"},
		{target: "/a/%2f/b", path: "/a/%2F/b"},
	} {
		t.Run(testCase.target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.target, nil)
			path, query, err := CanonicalHTTPRequestTarget(request)
			if err != nil {
				t.Fatal(err)
			}
			if path != testCase.path || query != testCase.query {
				t.Fatalf("target = %q ? %q, want %q ? %q", path, query, testCase.path, testCase.query)
			}
		})
	}
}
