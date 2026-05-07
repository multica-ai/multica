package main

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// realtimeMetricsHandler returns the HTTP handler for /health/realtime.
//
// The endpoint exposes operational counters (per-event / per-scope sends,
// Redis relay state, etc.) that should not be reachable by anonymous public
// clients. See MUL-1342.
//
// Access policy:
//   - If token != "": require Authorization: Bearer <token>; reject other
//     callers with 401.
//   - If token == "": only allow direct loopback callers (127.0.0.1 / ::1)
//     with no forwarding headers; reject anything else with 404 so the
//     endpoint is not enumerable. This keeps local development workflows
//     working without configuration while ensuring the metrics surface is
//     not exposed on a public listener — including when the server sits
//     behind a reverse proxy (Caddy / Nginx) that terminates TLS on
//     localhost, in which case all requests would otherwise look like
//     loopback (see MUL-1342 review).
func realtimeMetricsHandler(token string) http.HandlerFunc {
	token = strings.TrimSpace(token)
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			if !hasBearerToken(r, token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if !isDirectLoopbackRequest(r) {
			// Hide the endpoint from non-loopback callers (and from
			// proxied requests we cannot attribute) when no token is
			// configured. Returning 404 avoids advertising its
			// existence to remote scanners.
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		snapshot := realtime.M.Snapshot()
		snapshot["daemonws"] = daemonws.M.Snapshot()
		_ = json.NewEncoder(w).Encode(snapshot)
	}
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
	// Any indication that the request was relayed by a proxy disqualifies
	// the loopback shortcut: behind Caddy/Nginx -> localhost:8080, public
	// callers would otherwise appear as 127.0.0.1 here. We deliberately do
	// NOT trust these headers to identify the real client — we only use
	// their presence as a "this is proxied, fail closed" signal.
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
	for _, h := range []string{
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Real-Ip",
		"Forwarded",
	} {
		if strings.TrimSpace(r.Header.Get(h)) != "" {
			return true
		}
	}
	return false
}
