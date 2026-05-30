package pricing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corepricing "github.com/multica-ai/multica/server/pkg/pricing"
)

func decodeBody(t *testing.T, body []byte) corepricing.TableSnapshot {
	t.Helper()
	var snap corepricing.TableSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return snap
}

func TestGet_TokenMode(t *testing.T) {
	h := New("secret-key")

	// Wrong / missing key → 401.
	for _, auth := range []string{"", "Bearer wrong", "secret-key"} {
		req := httptest.NewRequest(http.MethodGet, "/api/cerebro/pricing", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.Get(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("auth=%q: want 401, got %d", auth, w.Code)
		}
	}

	// Correct key → 200 with the full table.
	req := httptest.NewRequest(http.MethodGet, "/api/cerebro/pricing", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	snap := decodeBody(t, w.Body.Bytes())
	if snap.Version != corepricing.Version {
		t.Fatalf("version: want %q, got %q", corepricing.Version, snap.Version)
	}
	if snap.FallbackModel == "" {
		t.Fatal("fallback_model empty")
	}
	// The current runtime model must be priced explicitly, not via fallback.
	rate, ok := snap.Models["claude-opus-4-8"]
	if !ok {
		t.Fatal("claude-opus-4-8 missing from served table")
	}
	if rate.InputCentsPerMtok != 500 || rate.OutputCentsPerMtok != 2500 {
		t.Fatalf("opus-4-8 rate drift: %+v", rate)
	}
}

func TestGet_LoopbackMode(t *testing.T) {
	h := New("") // no token → loopback-only

	// Proxied request (forwarding header present) → 404, not enumerable.
	proxied := httptest.NewRequest(http.MethodGet, "/api/cerebro/pricing", nil)
	proxied.RemoteAddr = "127.0.0.1:5000"
	proxied.Header.Set("X-Forwarded-For", "203.0.113.7")
	w := httptest.NewRecorder()
	h.Get(w, proxied)
	if w.Code != http.StatusNotFound {
		t.Fatalf("proxied loopback: want 404, got %d", w.Code)
	}

	// Direct loopback caller → 200.
	direct := httptest.NewRequest(http.MethodGet, "/api/cerebro/pricing", nil)
	direct.RemoteAddr = "127.0.0.1:5000"
	w = httptest.NewRecorder()
	h.Get(w, direct)
	if w.Code != http.StatusOK {
		t.Fatalf("direct loopback: want 200, got %d", w.Code)
	}
}
