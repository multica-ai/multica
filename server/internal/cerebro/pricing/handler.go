// CEREBRO: pricing endpoint kept in a dedicated cerebro package so upstream
// merges never conflict. Serves cerebro's in-code price table to the
// firtal-data-registry, which pulls it hourly to cost agent-trace events
// (FIR-2471 / FIR-2472). Cerebro is the single source of pricing truth; this
// endpoint is the only read path out of it.
package pricing

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/pkg/pricing"
)

// Handler serves the price table over HTTP. The shared service key gates it:
// the registry presents it as a Bearer token. When no key is configured the
// endpoint only answers direct loopback callers (local dev) and 404s everyone
// else so it is not enumerable — same policy as /health/realtime.
type Handler struct {
	token string
}

// New constructs the pricing Handler. token comes from CEREBRO_PRICING_KEY; an
// empty token switches the handler to loopback-only mode.
func New(token string) *Handler {
	return &Handler{token: strings.TrimSpace(token)}
}

// Get returns the current price table as JSON: {version, fallback_model,
// models{...}}. Cacheable for an hour — the registry pulls on that cadence.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if h.token != "" {
		if !hasBearerToken(r, h.token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="pricing"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	} else if !isDirectLoopbackRequest(r) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(pricing.Snapshot())
}

func hasBearerToken(r *http.Request, want string) bool {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(auth[len(prefix):])
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isDirectLoopbackRequest(r *http.Request) bool {
	if hasForwardingHeader(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func hasForwardingHeader(r *http.Request) bool {
	for _, hdr := range []string{
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Real-Ip",
		"Forwarded",
	} {
		if strings.TrimSpace(r.Header.Get(hdr)) != "" {
			return true
		}
	}
	return false
}
