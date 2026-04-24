package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealtimeMetricsHandler_TokenRequired(t *testing.T) {
	const token = "secret-test-token"
	h := realtimeMetricsHandler(token)

	t.Run("missing auth rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "203.0.113.10:54321"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got == "" {
			t.Fatalf("expected WWW-Authenticate header, got empty")
		}
	})

	t.Run("loopback without token rejected when token configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 even from loopback when token required, got %d", rec.Code)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.Header.Set("Authorization", "Bearer not-the-token")
		req.RemoteAddr = "203.0.113.10:54321"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non-bearer scheme rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.Header.Set("Authorization", "Basic "+token)
		req.RemoteAddr = "203.0.113.10:54321"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("correct bearer token accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.RemoteAddr = "203.0.113.10:54321"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected JSON content-type, got %q", ct)
		}
	})
}

func TestRealtimeMetricsHandler_NoToken_LoopbackOnly(t *testing.T) {
	h := realtimeMetricsHandler("")

	t.Run("loopback ipv4 allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 from loopback, got %d", rec.Code)
		}
	})

	t.Run("loopback ipv6 allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "[::1]:9999"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 from ipv6 loopback, got %d", rec.Code)
		}
	})

	t.Run("non-loopback returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "10.0.0.5:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 to hide endpoint, got %d", rec.Code)
		}
	})

	t.Run("public ipv6 returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "[2001:db8::1]:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}
