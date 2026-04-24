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

	// MUL-1342 review: when the server is behind a reverse proxy on
	// localhost (Caddy / Nginx -> 127.0.0.1:8080), public callers reach
	// the handler with RemoteAddr=127.0.0.1. The presence of forwarding
	// headers must disqualify the loopback shortcut, otherwise the
	// metrics surface is fully exposed in self-hosted deployments.
	proxyHeaders := []struct {
		name   string
		header string
		value  string
	}{
		{"x-forwarded-for", "X-Forwarded-For", "203.0.113.10"},
		{"x-forwarded-for chain", "X-Forwarded-For", "203.0.113.10, 10.0.0.1"},
		{"x-real-ip", "X-Real-Ip", "203.0.113.10"},
		{"x-forwarded-host", "X-Forwarded-Host", "metrics.example.com"},
		{"x-forwarded-proto", "X-Forwarded-Proto", "https"},
		{"forwarded rfc7239", "Forwarded", "for=203.0.113.10;proto=https"},
	}
	for _, tc := range proxyHeaders {
		tc := tc
		t.Run("proxied "+tc.name+" via loopback returns 404", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set(tc.header, tc.value)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for proxied loopback request (%s), got %d", tc.header, rec.Code)
			}
		})
	}

	t.Run("proxied loopback ipv6 returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "[::1]:9999"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for proxied ::1 request, got %d", rec.Code)
		}
	})

	t.Run("empty forwarding header still allows loopback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health/realtime", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-For", "   ")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 when forwarding header is blank, got %d", rec.Code)
		}
	})
}
