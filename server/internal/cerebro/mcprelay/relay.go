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
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

// ConnActor is the actor a relay token was minted for, parsed back out of the
// token at call time so the relay can resolve the FULL policy chain — not just
// the workspace scope. Every field is optional: a workspace-scoped token (the
// legacy direct mint, or the agent-less runtime tool-scan) carries none, so all
// fields are the zero UUID and the resolve collapses to workspace scope exactly
// as the first slice did. A claim mints the fields it knows (agent + owner +
// on_behalf_of initiator), so the relay honors an agent-level or member-level
// Deny too (FIR-2441 fix-list #2 follow-up).
type ConnActor struct {
	RuntimeID    pgtype.UUID
	AgentID      pgtype.UUID
	OwnerID      pgtype.UUID // agent owner → tool-policy User layer
	OnBehalfOfID pgtype.UUID // task initiator → tighten-only on_behalf_of layer
}

// toolPolicy decides whether a single mcp_http tool call is forbidden by policy
// for the actor a relay token carries (FIR-2441 fix-list #2). It exists because
// the relay previously proxied EVERY tools/call for a valid connection token
// untouched — so a `Deny` on one tool was only honored by CLIs that respect
// --disallowedTools (Claude/Cursor/Gemini). Codex/Hermes/Kimi/Kiro/OpenCode
// ignore that list, so the Deny leaked. Enforcing it here — server-side, on the
// path EVERY local runtime's mcp_http call already flows through — closes the
// hole independently of which agent program is running and independently of the
// cerebro_api_connection_tools flag. The first slice enforced only a
// workspace-scope Deny; the resolver now folds the whole actor chain (Workspace ›
// Runtime › Agent › Group › User › OnBehalfOf) from the token's actor, so an
// agent- or member-level Deny is honored too — an actor with no fields set (a
// workspace-scoped token) resolves exactly the workspace scope as before. A nil
// resolver means "no server-side policy" (the pre-FIR-2441 pass-through
// behavior), and the resolver only ever returns deny=true for an EXPLICIT Deny
// verdict — any error or unknown verdict is fail-open, so a tool that works today
// can never be broken by this gate.
type toolPolicy interface {
	ToolDenied(ctx context.Context, workspaceID pgtype.UUID, actor ConnActor, connName, toolName string) (denied bool, err error)
}

// maxRelayPolicyPeek bounds how large a request body the relay will buffer to
// inspect for a tools/call. MCP JSON-RPC requests are tiny; anything larger (or
// with an unknown length, e.g. chunked/SSE) is proxied untouched rather than
// buffered, so the policy peek can never truncate or OOM a legitimate request.
const maxRelayPolicyPeek = 1 << 20 // 1 MiB

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
	// Actor fields (FIR-2441 fix-list #2 follow-up). All omitempty so a
	// workspace-scoped token stays byte-for-byte the old shape and a token minted
	// off the direct Mint carries no actor at all. Stored as strings (UUID text)
	// so the token is self-describing and the relay can rebuild a ConnActor.
	RT  string `json:"rt,omitempty"`  // runtime id
	Agt string `json:"agt,omitempty"` // agent id
	Own string `json:"own,omitempty"` // agent owner (tool-policy User layer)
	OBO string `json:"obo,omitempty"` // on_behalf_of initiator (tighten-only layer)
}

// Mint returns a signed token authorizing relay access to connection `conn` in
// workspace `ws` until tokenTTL from now. It carries no actor, so the relay
// enforces tool policy at the workspace scope only — the pre-follow-up behavior,
// kept for the agent-less runtime tool-scan and any caller with no actor.
func (s *Signer) Mint(ws, conn string) (string, error) {
	return s.MintFor(ws, conn, ConnActor{})
}

// MintFor is Mint with an actor bound into the token, so the relay resolves the
// full Workspace › Runtime › Agent › Group › User › OnBehalfOf policy chain for
// that actor and honors an agent- or member-level Deny server-side — even for a
// CLI that ignores --disallowedTools (FIR-2441 fix-list #2 follow-up). A zero
// ConnActor is identical to Mint.
func (s *Signer) MintFor(ws, conn string, actor ConnActor) (string, error) {
	if len(s.secret) == 0 {
		return "", fmt.Errorf("mcprelay: signer not configured")
	}
	body, err := json.Marshal(tokenPayload{
		WS:   ws,
		Conn: conn,
		Exp:  s.now().Add(tokenTTL).Unix(),
		RT:   uuidText(actor.RuntimeID),
		Agt:  uuidText(actor.AgentID),
		Own:  uuidText(actor.OwnerID),
		OBO:  uuidText(actor.OnBehalfOfID),
	})
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(body)
	return p + "." + s.sign(p), nil
}

// uuidText renders a UUID as its text form, or "" when unset, so an absent actor
// field is simply omitted from the token (omitempty).
func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return util.UUIDToString(id)
}

// actor rebuilds the ConnActor from a token payload. A field that does not parse
// as a UUID is left zero (Valid=false), which the tool-policy chain treats as
// "layer empty" — so a malformed actor field degrades to a narrower scope, never
// a wider one.
func (p tokenPayload) actor() ConnActor {
	return ConnActor{
		RuntimeID:    parseUUIDOrZero(p.RT),
		AgentID:      parseUUIDOrZero(p.Agt),
		OwnerID:      parseUUIDOrZero(p.Own),
		OnBehalfOfID: parseUUIDOrZero(p.OBO),
	}
}

func parseUUIDOrZero(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
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
	policy toolPolicy // nil ⇒ no server-side tool-policy enforcement
}

// NewRelay wires the handler. policy may be nil, in which case the relay proxies
// every call for a valid token exactly as before (pre-FIR-2441). When non-nil it
// gates each tools/call against the workspace tool policy (fix-list #2).
func NewRelay(signer *Signer, store connLoader, policy toolPolicy) *Relay {
	return &Relay{signer: signer, store: store, policy: policy}
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
	// FIR-2441 fix-list #2: gate a tools/call against the tool policy for the
	// token's actor server-side, so a Deny is honored regardless of which agent
	// CLI is running. The actor (agent + owner + on_behalf_of) rides in the token,
	// so an agent- or member-level Deny is enforced here too; a workspace-scoped
	// token carries no actor and resolves the workspace scope exactly as before.
	// Only an explicit Deny blocks; everything else (including a body we cannot
	// safely buffer, an unparseable body, or a resolver error) is proxied
	// untouched, so this never breaks a call that works today.
	if rl.policy != nil && r.Method == http.MethodPost && r.ContentLength > 0 && r.ContentLength <= maxRelayPolicyPeek {
		buf, readErr := io.ReadAll(r.Body)
		_ = r.Body.Close()
		// Restore the body for the proxy no matter what we found.
		r.Body = io.NopCloser(bytes.NewReader(buf))
		r.ContentLength = int64(len(buf))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf)), nil }
		if readErr == nil {
			if tool, ok := toolCallName(buf); ok {
				denied, derr := rl.policy.ToolDenied(r.Context(), wsID, payload.actor(), payload.Conn, tool)
				switch {
				case derr != nil:
					// Fail open (don't break a working tool) but record it — a
					// silently-skipped policy check is a security regression.
					slog.Warn("mcp relay: tool policy resolve failed; proxying without enforcement",
						"workspace_id", payload.WS, "connection", payload.Conn, "tool", tool, "error", derr)
				case denied:
					http.Error(w, "mcp relay: tool denied by policy", http.StatusForbidden)
					return
				}
			}
		}
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

// toolCallName extracts the tool name from an MCP JSON-RPC request body when it
// is a tools/call. It returns ok=false for any other method, a batch (array)
// body, or a malformed body — the caller then proxies without enforcement, so a
// shape the relay does not understand never blocks a call. It intentionally only
// reads the two fields it needs and ignores everything else.
func toolCallName(body []byte) (string, bool) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false
	}
	if req.Method != "tools/call" {
		return "", false
	}
	name := strings.TrimSpace(req.Params.Name)
	if name == "" {
		return "", false
	}
	return name, true
}

func bearer(header string) (string, bool) {
	const p = "Bearer "
	if len(header) <= len(p) || !strings.EqualFold(header[:len(p)], p) {
		return "", false
	}
	return strings.TrimSpace(header[len(p):]), true
}
