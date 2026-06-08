package exchangerates

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// queries is nil in these cases on purpose: every path under test returns
// (401/404/400) before Store — which is the only code that touches the DB — is
// ever reached, so no test database is required.

func TestIngestPost_TokenMode_Unauthorized(t *testing.T) {
	h := NewIngestHandler(nil, "secret-key")

	for _, auth := range []string{"", "Bearer wrong", "secret-key" /* missing prefix */} {
		req := httptest.NewRequest(http.MethodPost, "/api/cerebro/exchange-rates", strings.NewReader(`{}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.Post(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("auth=%q: want 401, got %d", auth, w.Code)
		}
	}
}

func TestIngestPost_TokenMode_BadBody(t *testing.T) {
	h := NewIngestHandler(nil, "secret-key")

	cases := map[string]string{
		"malformed json": `{not-json`,
		"empty rates":    `{"base":"USD","date":"2026-06-04","rates":{}}`,
		"missing date":   `{"base":"USD","rates":{"DKK":6.8}}`,
		"missing base":   `{"date":"2026-06-04","rates":{"DKK":6.8}}`,
	}
	for name, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/cerebro/exchange-rates", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret-key")
		w := httptest.NewRecorder()
		h.Post(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", name, w.Code)
		}
	}
}

func TestIngestPost_LoopbackMode(t *testing.T) {
	h := NewIngestHandler(nil, "") // no token → loopback-only

	// Proxied request (forwarding header present) → 404, not enumerable.
	proxied := httptest.NewRequest(http.MethodPost, "/api/cerebro/exchange-rates", strings.NewReader(`{}`))
	proxied.RemoteAddr = "127.0.0.1:5000"
	proxied.Header.Set("X-Forwarded-For", "203.0.113.7")
	w := httptest.NewRecorder()
	h.Post(w, proxied)
	if w.Code != http.StatusNotFound {
		t.Fatalf("proxied loopback: want 404, got %d", w.Code)
	}

	// Non-loopback remote → 404.
	remote := httptest.NewRequest(http.MethodPost, "/api/cerebro/exchange-rates", strings.NewReader(`{}`))
	remote.RemoteAddr = "203.0.113.7:5000"
	w = httptest.NewRecorder()
	h.Post(w, remote)
	if w.Code != http.StatusNotFound {
		t.Fatalf("remote caller: want 404, got %d", w.Code)
	}

	// Direct loopback caller passes the gate; a malformed body then yields 400
	// (reached before Store, so still no DB needed).
	direct := httptest.NewRequest(http.MethodPost, "/api/cerebro/exchange-rates", strings.NewReader(`{bad`))
	direct.RemoteAddr = "127.0.0.1:5000"
	w = httptest.NewRecorder()
	h.Post(w, direct)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("direct loopback bad body: want 400, got %d", w.Code)
	}
}
