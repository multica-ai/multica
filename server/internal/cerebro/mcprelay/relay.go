// Package mcprelay brokers a local runtime's MCP connection calls through
// Multica so the runtime never reaches the internal path directly (FIR-1563).
//
// The problem: workspace connections of type mcp_http may point at an
// internal-only host (e.g. customer-service-mcp.internal:3000). A cloud
// runtime that lives inside the Sliplane network can reach it; a LOCAL runtime
// (Claude/Codex on a teammate's laptop) cannot — DNS for *.internal fails, the
// MCP server never starts, and the tools never appear.
//
// The fix mirrors the Agent Vault relay (internal/cerebro/agentvault): instead
// of injecting the internal URL + the connection's real bearer into the local
// runtime, the daemon injects a Multica-hosted relay URL + a short-lived signed
// token. The local runtime calls Multica (publicly reachable, on Sliplane); the
// relay authenticates the token, loads the connection server-side, and forwards
// the MCP request over the internal path with the connection's real credentials.
//
// Net effect: the local machine only ever holds an opaque, connection-scoped
// token that expires; it never sees the internal address and never holds the
// connection secret. It is the only path a local runtime uses — exactly the
// "kald gennem Multica" requirement.
package mcprelay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/util"
)

// tokenTTL bounds how long an injected relay token stays valid. Tokens are
// minted fresh at every task claim, so a short window is fine and limits the
// blast radius of a leaked token to a single run.
const tokenTTL = 12 * time.Hour

// connLoader is the slice of *connections.Store the relay needs. An interface
// keeps the handler unit-testable without a database.
type connLoader interface {
	GetEnabledByName(ctx context.Context, workspaceID pgtype.UUID, name string) (connections.Connection, error)
}

// Signer mints and verifies relay tokens. A token binds a single connection in
// a single workspace to an expiry, authenticated by an HMAC over the server
// secret. The secret never leaves the server; the local runtime only carries
// the opaque token.
type Signer struct {
	secret []byte
	now    func() time.Time // overridable in tests
}

// NewSigner builds a Signer from the shared HMAC secret.
func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret), now: time.Now}
}

type tokenPayload struct {
	WS   string `json:"ws"`
	Conn string `json:"conn"`
	Exp  int64  `json:"exp"`
}

// Mint returns a signed token authorizing relay access to connection `conn` in
// workspace `ws` until tokenTTL from now.
func (s *Signer) Mint(ws, conn string) (string, error) {
	if len(s.secret) == 0 {
		return "", fmt.Errorf("mcprelay: signer not configured")
	}
	body, err := json.Marshal(tokenPayload{WS: ws, Conn: conn, Exp: s.now().Add(tokenTTL).Unix()})
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(body)
	return p + "." + s.sign(p), nil
}

// Verify checks a token's signature and expiry and returns its payload.
func (s *Signer) Verify(token string) (tokenPayload, bool) {
	if len(s.secret) == 0 {
		return tokenPayload{}, false
	}
	p, sig, ok := strings.Cut(token, ".")
	if !ok {
		return tokenPayload{}, false
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(s.sign(p))) != 1 {
		return tokenPayload{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return tokenPayload{}, false
	}
	var payload tokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return tokenPayload{}, false
	}
	if s.now().Unix() >= payload.Exp {
		return tokenPayload{}, false
	}
	return payload, true
}

func (s *Signer) sign(p string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(p))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Relay is the public HTTP handler a local runtime's MCP client talks to. It
// authenticates the injected token, loads the connection server-side, and
// reverse-proxies the request over the internal path with the connection's real
// credentials swapped in.
type Relay struct {
	signer *Signer
	store  connLoader
}

// NewRelay wires the handler.
func NewRelay(signer *Signer, store connLoader) *Relay {
	return &Relay{signer: signer, store: store}
}

// ServeHTTP handles every MCP method (POST for requests, GET for the SSE
// stream) under /mcp-relay/{name}. The reverse proxy streams responses, so SSE
// works transparently.
func (rl *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	token, ok := bearer(r.Header.Get("Authorization"))
	if !ok {
		http.Error(w, "mcp relay: missing bearer token", http.StatusUnauthorized)
		return
	}
	payload, ok := rl.signer.Verify(token)
	if !ok {
		http.Error(w, "mcp relay: invalid or expired token", http.StatusUnauthorized)
		return
	}
	// Defense in depth: the path name must match the token's connection, so a
	// token minted for one connection cannot be replayed against another.
	if payload.Conn != name {
		http.Error(w, "mcp relay: token/connection mismatch", http.StatusForbidden)
		return
	}
	wsID, err := util.ParseUUID(payload.WS)
	if err != nil {
		http.Error(w, "mcp relay: bad workspace", http.StatusBadRequest)
		return
	}
	conn, err := rl.store.GetEnabledByName(r.Context(), wsID, payload.Conn)
	if err != nil {
		http.Error(w, "mcp relay: connection not found or disabled", http.StatusNotFound)
		return
	}
	if conn.Type != connections.TypeMCPHTTP {
		http.Error(w, "mcp relay: not an mcp connection", http.StatusBadRequest)
		return
	}
	target, err := url.Parse(conn.URL)
	if err != nil || target.Host == "" {
		http.Error(w, "mcp relay: connection has no valid url", http.StatusBadGateway)
		return
	}
	real := realHeaders(conn.AuthConfig)
	proxy := &httputil.ReverseProxy{
		// FlushInterval -1 streams each write immediately — required for the
		// MCP SSE response channel to reach the client without buffering.
		FlushInterval: -1,
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = target.Path
			req.URL.RawQuery = target.RawQuery
			req.Host = target.Host
			// Drop anything the local runtime sent that authenticates to the
			// RELAY, then set the connection's real upstream credentials.
			req.Header.Del("Authorization")
			req.Header.Del("CF-Access-Client-Id")
			req.Header.Del("CF-Access-Client-Secret")
			for k, v := range real {
				req.Header.Set(k, v)
			}
		},
	}
	proxy.ServeHTTP(w, r)
}

// realHeaders builds the upstream auth headers from a connection's stored
// credentials — the same mapping connections.BuildMCPConfig uses for the direct
// (cloud) path, kept in sync intentionally.
func realHeaders(a connections.AuthConfig) map[string]string {
	h := make(map[string]string)
	if a.BearerToken != "" {
		h["Authorization"] = "Bearer " + a.BearerToken
	}
	if a.APIKey != "" {
		key := a.APIKeyHeader
		if key == "" {
			key = "X-API-Key"
		}
		h[key] = a.APIKey
	}
	if a.CFAccessID != "" {
		h["CF-Access-Client-Id"] = a.CFAccessID
	}
	if a.CFAccessSecret != "" {
		h["CF-Access-Client-Secret"] = a.CFAccessSecret
	}
	return h
}

func bearer(header string) (string, bool) {
	const p = "Bearer "
	if len(header) <= len(p) || !strings.EqualFold(header[:len(p)], p) {
		return "", false
	}
	return strings.TrimSpace(header[len(p):]), true
}
