package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestRouterCORSContract(t *testing.T) {
	const origin = "https://cors-client.example"
	t.Setenv("CORS_ALLOWED_ORIGINS", origin)
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	t.Run("preflight accepts browser request headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/config", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "X-Client-Capabilities, Idempotency-Key")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusOK)
		}
		for _, want := range []string{"X-Client-Capabilities", "Idempotency-Key"} {
			if !headerListContains(rec.Header().Get("Access-Control-Allow-Headers"), want) {
				t.Errorf("Access-Control-Allow-Headers = %q, missing %q", rec.Header().Get("Access-Control-Allow-Headers"), want)
			}
		}
	})

	t.Run("browser can read truncation signals", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		for _, want := range []string{
			handler.HeaderCommentsTruncated,
			handler.HeaderTimelineTruncated,
			handler.HeaderActiveRunsTruncated,
		} {
			if !headerListContains(rec.Header().Get("Access-Control-Expose-Headers"), want) {
				t.Errorf("Access-Control-Expose-Headers = %q, missing %q", rec.Header().Get("Access-Control-Expose-Headers"), want)
			}
		}
	})
}

func headerListContains(header, want string) bool {
	for _, value := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

// Every header the browser clients send has to be preflight-allowed, and the
// CSRF header is the one where getting this wrong is invisible in same-origin
// development and fatal in a split app/API deployment: the preflight fails, so
// the request never reaches a handler and the failure looks nothing like a
// rejected CSRF token.
//
// It is also the header whose name must not change casually. A rolled-back
// server allowlists only the names it shipped with, so a client that starts
// sending a NEW header can be blocked by a version of the server that predates
// it — which is why MUL-7436 carries two CSRF cookie values through this one
// header name rather than adding a second.
func TestRouterCORSAllowsTheCSRFHeader(t *testing.T) {
	const origin = "https://cors-client.example"
	t.Setenv("CORS_ALLOWED_ORIGINS", origin)
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	req := httptest.NewRequest(http.MethodOptions, "/api/config", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", auth.CSRFHeaderName)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusOK)
	}
	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	if !headerListContains(allowed, auth.CSRFHeaderName) {
		t.Errorf("Access-Control-Allow-Headers = %q, missing %q", allowed, auth.CSRFHeaderName)
	}
}

// Pins the invariant rather than one name: whatever header the auth package
// tells clients to send, the router must allow. If someone introduces a second
// CSRF header without touching corsAllowedHeaders, this fails here instead of
// in a production split-origin deployment.
func TestCSRFHeaderIsInTheCORSAllowlist(t *testing.T) {
	for _, h := range corsAllowedHeaders {
		if strings.EqualFold(h, auth.CSRFHeaderName) {
			return
		}
	}
	t.Fatalf("auth.CSRFHeaderName (%q) is not in corsAllowedHeaders; browsers would fail the preflight", auth.CSRFHeaderName)
}
