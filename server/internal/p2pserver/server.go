package p2pserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	cryptopb "github.com/libp2p/go-libp2p/core/crypto/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	// DefaultDeviceTTL is how long a registration stays valid without being
	// refreshed. The API server never tells this service about sign-outs or
	// deleted accounts, so stale devices age out instead.
	DefaultDeviceTTL = 24 * time.Hour
	// DefaultSessionTTL is how long a session token is valid. The Multica
	// token travels to this service only when a session is issued.
	DefaultSessionTTL = 24 * time.Hour

	maxFieldLen = 200
	maxBodySize = 16 << 10
	maxAddrs    = 32
	maxAddrLen  = 256
	// sessionsPerIPPerMinute rate-limits session issuance, the only
	// endpoint that reaches the API server.
	sessionsPerIPPerMinute = 30
)

// Server serves the device registry API:
//
//	POST   /v1/sessions                        exchange a Multica token for a session token
//	GET    /v1/config                          relay address
//	POST   /v1/devices                         register or refresh this device
//	GET    /v1/devices                         list the account's live devices
//	GET    /v1/events                          watch account device invalidations (SSE)
//	DELETE /v1/devices?daemon_id=..&profile=.. remove a device
//	GET    /healthz
//
// Every /v1 endpoint except /v1/sessions takes the session token.
type Server struct {
	Auth       Authenticator
	Store      *Store
	Relay      *Relay
	DeviceTTL  time.Duration
	SessionTTL time.Duration
	Logger     *slog.Logger
	Now        func() time.Time

	limiterOnce sync.Once
	limiter     *ipLimiter
	watchMu     sync.Mutex
	watchers    map[string]map[chan struct{}]struct{}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /v1/sessions", s.createSession)
	mux.HandleFunc("GET /v1/config", s.authed(s.config))
	mux.HandleFunc("POST /v1/devices", s.authed(s.register))
	mux.HandleFunc("GET /v1/devices", s.authed(s.list))
	mux.HandleFunc("GET /v1/events", s.authed(s.watchDevices))
	mux.HandleFunc("DELETE /v1/devices", s.authed(s.remove))
	return mux
}

// RunPurge deletes expired devices every interval until ctx ends.
func (s *Server) RunPurge(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := s.now()
			if n, err := s.Store.PurgeBefore(ctx, now.Add(-s.ttl()), now); err != nil {
				s.logger().Warn("p2pserver: purge failed", "error", err)
			} else if n > 0 {
				s.logger().Info("p2pserver: purged expired devices", "count", n)
			}
		}
	}
}

func (s *Server) ttl() time.Duration {
	if s.DeviceTTL > 0 {
		return s.DeviceTTL
	}
	return DefaultDeviceTTL
}

func (s *Server) sessionTTL() time.Duration {
	if s.SessionTTL > 0 {
		return s.SessionTTL
	}
	return DefaultSessionTTL
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

type authedHandler func(w http.ResponseWriter, r *http.Request, userID string)

func bearer(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	return token, ok && token != ""
}

func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		userID, ok, err := s.Store.SessionUser(r.Context(), token, s.now())
		if err != nil {
			s.logger().Warn("p2pserver: session lookup failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to check session")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		h(w, r, userID)
	}
}

// createSession exchanges a Multica login token for a session token.
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !s.ipLimiter().allow(clientIP(r), s.now()) {
		writeError(w, http.StatusTooManyRequests, "too many session requests")
		return
	}
	token, ok := bearer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	userID, err := s.Auth.UserID(r.Context(), token)
	switch {
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	case err != nil:
		// Never log the token; the error carries only status details.
		s.logger().Warn("p2pserver: account check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "account service unavailable")
		return
	}
	expires := s.now().Add(s.sessionTTL()).UTC().Truncate(time.Second)
	session, err := s.Store.CreateSession(r.Context(), userID, expires)
	if err != nil {
		s.logger().Warn("p2pserver: create session failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	s.logger().Info("p2pserver: session issued", "user_id", userID)
	writeJSON(w, http.StatusOK, map[string]any{"session_token": session, "expires_at": expires})
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request, _ string) {
	resp := map[string]any{"relay": nil}
	if s.Relay != nil {
		resp["relay"] = s.Relay.Info()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		s.logger().Warn("p2pserver: health check: database", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if s.Relay != nil && !s.Relay.Healthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type registerRequest struct {
	DaemonID string `json:"daemon_id"`
	Profile  string `json:"profile"`
	// PublicKey is a libp2p protobuf-encoded ed25519 public key, base64
	// (standard) encoded. The peer ID is derived from it here.
	PublicKey  string `json:"public_key"`
	DeviceName string `json:"device_name"`
	// Addrs are multiaddrs the device can be dialed on. Invalid or
	// oversized entries are dropped.
	Addrs []string `json:"addrs"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request, userID string) {
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.DaemonID = strings.TrimSpace(req.DaemonID)
	if req.DaemonID == "" || len(req.DaemonID) > maxFieldLen {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	publicKey, peerID, ok := parsePublicKey(req.PublicKey)
	if !ok {
		writeError(w, http.StatusBadRequest, "public_key must be a base64 libp2p ed25519 public key")
		return
	}
	now := s.now()
	d, changed, err := s.Store.Upsert(r.Context(), Device{
		UserID:     userID,
		DaemonID:   req.DaemonID,
		Profile:    truncate(strings.TrimSpace(req.Profile)),
		PeerID:     peerID,
		PublicKey:  publicKey,
		DeviceName: truncate(strings.TrimSpace(req.DeviceName)),
		Addrs:      cleanAddrs(req.Addrs),
	}, now, now.Add(-s.ttl()))
	if errors.Is(err, ErrPeerTaken) {
		writeError(w, http.StatusConflict, "public_key is registered to another account")
		return
	}
	if err != nil {
		s.logger().Warn("p2pserver: register failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to register device")
		return
	}
	if changed {
		s.devicesChanged(userID)
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, userID string) {
	devices, err := s.Store.List(r.Context(), userID, s.now().Add(-s.ttl()))
	if err != nil {
		s.logger().Warn("p2pserver: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request, userID string) {
	daemonID := strings.TrimSpace(r.URL.Query().Get("daemon_id"))
	if daemonID == "" {
		writeError(w, http.StatusBadRequest, "daemon_id is required")
		return
	}
	peerID, err := s.Store.Delete(r.Context(), userID, daemonID, strings.TrimSpace(r.URL.Query().Get("profile")))
	if err != nil {
		s.logger().Warn("p2pserver: delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete device")
		return
	}
	if peerID == "" {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	if s.Relay != nil {
		s.Relay.Forget(peerID)
	}
	s.logger().Info("p2pserver: device removed", "user_id", userID, "peer_id", peerID)
	s.devicesChanged(userID)
	w.WriteHeader(http.StatusNoContent)
}

// parsePublicKey accepts only ed25519 keys, the type daemons generate, and
// returns the canonical encoding with its peer ID.
func parsePublicKey(encoded string) (canonical, peerID string, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	pub, err := crypto.UnmarshalPublicKey(raw)
	if err != nil || pub.Type() != cryptopb.KeyType_Ed25519 {
		return "", "", false
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return "", "", false
	}
	out, err := crypto.MarshalPublicKey(pub)
	if err != nil {
		return "", "", false
	}
	return base64.StdEncoding.EncodeToString(out), id.String(), true
}

func cleanAddrs(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		if len(out) == maxAddrs {
			break
		}
		if len(s) > maxAddrLen {
			continue
		}
		a, err := ma.NewMultiaddr(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		if c := a.String(); !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func truncate(s string) string {
	if r := []rune(s); len(r) > maxFieldLen {
		return string(r[:maxFieldLen])
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) ipLimiter() *ipLimiter {
	s.limiterOnce.Do(func() { s.limiter = &ipLimiter{limit: sessionsPerIPPerMinute, counts: map[string]int{}} })
	return s.limiter
}

// clientIP is the TCP peer address. The service is reached directly, not
// through a proxy, so forwarding headers are not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipLimiter allows limit requests per IP per clock minute.
type ipLimiter struct {
	limit int

	mu     sync.Mutex
	window time.Time
	counts map[string]int
}

func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if w := now.Truncate(time.Minute); !w.Equal(l.window) {
		l.window = w
		clear(l.counts)
	}
	if l.counts[ip] >= l.limit {
		return false
	}
	l.counts[ip]++
	return true
}
