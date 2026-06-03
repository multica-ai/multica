// CEREBRO-PATCH(cerebro-github-pr-link): FIR-2568 — tests for the PR-link endpoint's auth gate.
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPRLinkHasBearerToken(t *testing.T) {
	cases := []struct {
		name string
		auth string
		want bool
	}{
		{"valid", "Bearer s3cret", true},
		{"wrong", "Bearer nope", false},
		{"no_prefix", "s3cret", false},
		{"empty_token", "Bearer ", false},
		{"empty_header", "", false},
		{"case_insensitive_prefix", "bearer s3cret", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/cerebro/github/pull-requests", nil)
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			if got := hasBearerToken(r, "s3cret"); got != tc.want {
				t.Fatalf("hasBearerToken(%q) = %v, want %v", tc.auth, got, tc.want)
			}
		})
	}
}

func TestPRLinkLoopbackGate(t *testing.T) {
	// A forwarded request (proxied from outside) must never count as loopback.
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if isDirectLoopbackRequest(r) {
		t.Fatal("forwarded request must not be treated as direct loopback")
	}

	// A direct loopback caller with no forwarding headers is allowed.
	r2 := httptest.NewRequest(http.MethodPost, "/", nil)
	r2.RemoteAddr = "127.0.0.1:5000"
	if !isDirectLoopbackRequest(r2) {
		t.Fatal("direct loopback caller should be allowed")
	}

	// A non-loopback direct caller is rejected.
	r3 := httptest.NewRequest(http.MethodPost, "/", nil)
	r3.RemoteAddr = "10.0.0.5:5000"
	if isDirectLoopbackRequest(r3) {
		t.Fatal("non-loopback direct caller must be rejected")
	}
}
