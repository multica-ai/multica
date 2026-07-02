package mcprelay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestSignerRoundTrip(t *testing.T) {
	s := NewSigner("super-secret")
	tok, err := s.Mint("ws-1", "customer-service-mcp")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	p, ok := s.Verify(tok)
	if !ok {
		t.Fatal("verify rejected a freshly minted token")
	}
	if p.WS != "ws-1" || p.Conn != "customer-service-mcp" {
		t.Fatalf("payload mismatch: %+v", p)
	}
}

func TestSignerRejectsTamperedAndForeign(t *testing.T) {
	s := NewSigner("secret-a")
	tok, _ := s.Mint("ws-1", "conn")

	if _, ok := s.Verify(tok + "x"); ok {
		t.Fatal("tampered signature accepted")
	}
	if _, ok := s.Verify("not-a-token"); ok {
		t.Fatal("garbage token accepted")
	}
	other := NewSigner("secret-b")
	if _, ok := other.Verify(tok); ok {
		t.Fatal("token signed by a different secret accepted")
	}
}

func TestSignerRejectsExpired(t *testing.T) {
	base := time.Now()
	s := NewSigner("secret")
	s.now = func() time.Time { return base }
	tok, _ := s.Mint("ws", "conn")
	s.now = func() time.Time { return base.Add(tokenTTL + time.Minute) }
	if _, ok := s.Verify(tok); ok {
		t.Fatal("expired token accepted")
	}
}

// stubLoader returns a fixed connection regardless of lookup.
type stubLoader struct {
	conn connections.Connection
	err  error
}

func (s stubLoader) GetEnabledByName(_ context.Context, _ pgtype.UUID, _ string) (connections.Connection, error) {
	return s.conn, s.err
}

func TestRelayForwardsWithRealCredentials(t *testing.T) {
	// Upstream stands in for the internal MCP server. It records what the relay
	// forwarded so we can assert the local-runtime token was swapped out for the
	// connection's real secret.
	var gotAuth, gotCFID, gotPath, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCFID = r.Header.Get("CF-Access-Client-Id")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	ws := util.UUIDToString(newUUID(t))
	signer := NewSigner("secret")
	conn := connections.Connection{
		WorkspaceID: ws,
		Name:        "customer-service-mcp",
		Type:        connections.TypeMCPHTTP,
		Internal:    true,
		URL:         upstream.URL + "/mcp",
		AuthConfig: connections.AuthConfig{
			BearerToken: "fc_mcp_real_secret",
			CFAccessID:  "cf-id-123",
		},
	}
	relay := NewRelay(signer, stubLoader{conn: conn}, nil)

	tok, _ := signer.Mint(ws, "customer-service-mcp")

	r := chi.NewRouter()
	r.Handle("/mcp-relay/{name}", relay)
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp-relay/customer-service-mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("relay request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relay status = %d, want 200", resp.StatusCode)
	}
	if gotAuth != "Bearer fc_mcp_real_secret" {
		t.Fatalf("upstream Authorization = %q, want the connection's real bearer", gotAuth)
	}
	if gotCFID != "cf-id-123" {
		t.Fatalf("upstream CF-Access-Client-Id = %q, want cf-id-123", gotCFID)
	}
	if gotPath != "/mcp" {
		t.Fatalf("upstream path = %q, want /mcp (connection URL path)", gotPath)
	}
	if !strings.Contains(gotBody, "tools/list") {
		t.Fatalf("upstream body = %q, want the forwarded JSON-RPC", gotBody)
	}
}

func TestRelayRejectsBadAuth(t *testing.T) {
	signer := NewSigner("secret")
	relay := NewRelay(signer, stubLoader{}, nil)
	r := chi.NewRouter()
	r.Handle("/mcp-relay/{name}", relay)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// No token.
	resp, _ := http.Get(srv.URL + "/mcp-relay/customer-service-mcp")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token: status = %d, want 401", resp.StatusCode)
	}

	// Valid token, but for a different connection than the path → mismatch.
	ws := util.UUIDToString(newUUID(t))
	tok, _ := signer.Mint(ws, "some-other-conn")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp-relay/customer-service-mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp2, _ := http.DefaultClient.Do(req)
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("connection mismatch: status = %d, want 403", resp2.StatusCode)
	}
}

func TestRelayEntryOnlyRewritesInternal(t *testing.T) {
	m := &Module{Signer: NewSigner("secret"), baseURL: "https://multica.firtal.com/mcp-relay"}

	// Public (non-internal) connection: left direct.
	if _, _, ok := m.RelayEntry("ws", "public-conn", "https://api.example.com/mcp", false); ok {
		t.Fatal("RelayEntry rewrote a non-internal connection")
	}
	// Internal connection (by flag): rewritten to a relay URL + token.
	gotURL, bearer, ok := m.RelayEntry("ws", "customer-service-mcp", "https://api.example.com/mcp", true)
	if !ok {
		t.Fatal("RelayEntry did not rewrite an internal connection")
	}
	// Internal connection (by .internal host, flag unset): also rewritten.
	if _, _, ok := m.RelayEntry("ws", "cs", "http://customer-service-mcp.internal:3000/mcp", false); !ok {
		t.Fatal("RelayEntry did not rewrite a .internal host with flag unset")
	}
	if gotURL != "https://multica.firtal.com/mcp-relay/customer-service-mcp" {
		t.Fatalf("relay url = %q", gotURL)
	}
	if p, ok := m.Signer.Verify(bearer); !ok || p.Conn != "customer-service-mcp" {
		t.Fatalf("minted bearer did not verify for the connection: ok=%v p=%+v", ok, p)
	}
}

// fakePolicy denies exactly the (connection, tool) pairs it is given, and
// records the last resolve so a test can assert the gate consulted policy — and
// with which actor, so a test can prove the token's actor reached the resolver.
type fakePolicy struct {
	deny      map[string]bool // "conn/tool" → denied
	err       error
	askedFor  string
	gotActor  ConnActor
	denyActor func(actor ConnActor) bool // optional actor-aware override
}

func (p *fakePolicy) ToolDenied(_ context.Context, _ pgtype.UUID, actor ConnActor, conn, tool string) (bool, error) {
	p.askedFor = conn + "/" + tool
	p.gotActor = actor
	if p.err != nil {
		return false, p.err
	}
	if p.denyActor != nil {
		return p.denyActor(actor), nil
	}
	return p.deny[conn+"/"+tool], nil
}

// relayWithPolicy spins up an upstream + relay wired with the given policy and
// returns the relay test server plus a pointer to whether upstream was hit.
func relayWithPolicy(t *testing.T, policy toolPolicy) (*httptest.Server, *bool, string) {
	t.Helper()
	hit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		b, _ := io.ReadAll(r.Body)
		// Echo the body back so a test can confirm it survived the policy peek.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	t.Cleanup(upstream.Close)

	ws := util.UUIDToString(newUUID(t))
	signer := NewSigner("secret")
	conn := connections.Connection{
		WorkspaceID: ws, Name: "cs", Type: connections.TypeMCPHTTP, Internal: true,
		URL: upstream.URL + "/mcp", AuthConfig: connections.AuthConfig{BearerToken: "real"},
	}
	relay := NewRelay(signer, stubLoader{conn: conn}, policy)
	r := chi.NewRouter()
	r.Handle("/mcp-relay/{name}", relay)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	tok, _ := signer.Mint(ws, "cs")
	return srv, &hit, tok
}

func postToolCall(t *testing.T, srv *httptest.Server, tok, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp-relay/cs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("relay request: %v", err)
	}
	return resp
}

// FIR-2441 fix-list #2: a tools/call for a Deny-verdict tool is blocked at the
// relay with 403 and never reaches upstream — regardless of the agent CLI.
func TestRelayDeniesToolByPolicy(t *testing.T) {
	pol := &fakePolicy{deny: map[string]bool{"cs/dangerous": true}}
	srv, hit, tok := relayWithPolicy(t, pol)

	resp := postToolCall(t, srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"dangerous","arguments":{}}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied tool: status = %d, want 403", resp.StatusCode)
	}
	if *hit {
		t.Fatal("denied tool must NOT reach upstream")
	}
	if pol.askedFor != "cs/dangerous" {
		t.Fatalf("policy consulted for %q, want cs/dangerous", pol.askedFor)
	}
}

// An Allow (not-denied) tool proxies through with its body intact.
func TestRelayAllowsToolAndPreservesBody(t *testing.T) {
	pol := &fakePolicy{deny: map[string]bool{"cs/dangerous": true}}
	srv, hit, tok := relayWithPolicy(t, pol)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"safe","arguments":{"x":1}}}`
	resp := postToolCall(t, srv, tok, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allowed tool: status = %d, want 200", resp.StatusCode)
	}
	if !*hit {
		t.Fatal("allowed tool must reach upstream")
	}
	echoed, _ := io.ReadAll(resp.Body)
	if string(echoed) != body {
		t.Fatalf("upstream received %q, want the untouched body %q", echoed, body)
	}
}

// A non-tools/call method (tools/list) is never gated, even if a same-named
// tool would be denied — the method check comes first.
func TestRelayDoesNotGateNonToolCall(t *testing.T) {
	pol := &fakePolicy{deny: map[string]bool{"cs/anything": true}}
	srv, hit, tok := relayWithPolicy(t, pol)

	resp := postToolCall(t, srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !*hit {
		t.Fatalf("tools/list must proxy through: status=%d hit=%v", resp.StatusCode, *hit)
	}
	if pol.askedFor != "" {
		t.Fatalf("policy must not be consulted for a non-tools/call; got %q", pol.askedFor)
	}
}

// A resolver error fails OPEN — the call proxies through rather than breaking a
// tool that works today (the error is logged, not surfaced as a block).
func TestRelayFailsOpenOnPolicyError(t *testing.T) {
	pol := &fakePolicy{err: errFakePolicy}
	srv, hit, tok := relayWithPolicy(t, pol)

	resp := postToolCall(t, srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"safe"}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !*hit {
		t.Fatalf("policy error must fail open: status=%d hit=%v", resp.StatusCode, *hit)
	}
}

var errFakePolicy = fmtErr("boom")

type fmtErr string

func (e fmtErr) Error() string { return string(e) }

func TestToolCallName(t *testing.T) {
	if name, ok := toolCallName([]byte(`{"method":"tools/call","params":{"name":"foo"}}`)); !ok || name != "foo" {
		t.Fatalf("tools/call parse: name=%q ok=%v", name, ok)
	}
	if _, ok := toolCallName([]byte(`{"method":"tools/list"}`)); ok {
		t.Fatal("tools/list must not be treated as a tool call")
	}
	if _, ok := toolCallName([]byte(`{"method":"tools/call","params":{"name":""}}`)); ok {
		t.Fatal("empty tool name must be ignored")
	}
	if _, ok := toolCallName([]byte(`not json`)); ok {
		t.Fatal("malformed body must not parse as a tool call")
	}
	if _, ok := toolCallName([]byte(`[{"method":"tools/call"}]`)); ok {
		t.Fatal("batch/array body must not parse as a single tool call")
	}
}

// FIR-2441 fix-list #2 follow-up: an actor-scoped token round-trips its actor
// through Verify, so the relay can resolve the full chain at call time.
func TestSignerMintForCarriesActor(t *testing.T) {
	s := NewSigner("secret")
	agent := mustUUID(t, "22222222-2222-2222-2222-222222222222")
	owner := mustUUID(t, "33333333-3333-3333-3333-333333333333")
	obo := mustUUID(t, "44444444-4444-4444-4444-444444444444")
	tok, err := s.MintFor("ws", "cs", ConnActor{AgentID: agent, OwnerID: owner, OnBehalfOfID: obo})
	if err != nil {
		t.Fatalf("mintfor: %v", err)
	}
	p, ok := s.Verify(tok)
	if !ok {
		t.Fatal("verify rejected an actor-scoped token")
	}
	got := p.actor()
	if got.AgentID != agent || got.OwnerID != owner || got.OnBehalfOfID != obo {
		t.Fatalf("actor round-trip mismatch: %+v", got)
	}
	// A plain Mint carries no actor — every field zero.
	plain, _ := s.Mint("ws", "cs")
	pp, _ := s.Verify(plain)
	if a := pp.actor(); a.AgentID.Valid || a.OwnerID.Valid || a.OnBehalfOfID.Valid || a.RuntimeID.Valid {
		t.Fatalf("plain Mint must carry no actor; got %+v", a)
	}
}

// The relay enforces an AGENT-level (not just workspace) Deny when the token
// carries the actor: the resolver denies only for the token's agent, and the
// call is blocked before it reaches upstream — regardless of the CLI.
func TestRelayEnforcesActorLevelDeny(t *testing.T) {
	deniedAgent := mustUUID(t, "22222222-2222-2222-2222-222222222222")
	pol := &fakePolicy{denyActor: func(a ConnActor) bool { return a.AgentID == deniedAgent }}

	hit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	ws := util.UUIDToString(newUUID(t))
	signer := NewSigner("secret")
	conn := connections.Connection{
		WorkspaceID: ws, Name: "cs", Type: connections.TypeMCPHTTP, Internal: true,
		URL: upstream.URL + "/mcp", AuthConfig: connections.AuthConfig{BearerToken: "real"},
	}
	relay := NewRelay(signer, stubLoader{conn: conn}, pol)
	r := chi.NewRouter()
	r.Handle("/mcp-relay/{name}", relay)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"whatever"}}`

	// Token for the denied agent → blocked at the relay.
	tokDenied, _ := signer.MintFor(ws, "cs", ConnActor{AgentID: deniedAgent})
	resp := postToolCall(t, srv, tokDenied, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied agent: status = %d, want 403", resp.StatusCode)
	}
	if hit {
		t.Fatal("denied agent must NOT reach upstream")
	}
	if pol.gotActor.AgentID != deniedAgent {
		t.Fatalf("resolver saw actor %+v, want agent %v", pol.gotActor, deniedAgent)
	}

	// Token for a different agent → same tool proxies through.
	hit = false
	otherAgent := mustUUID(t, "55555555-5555-5555-5555-555555555555")
	tokOK, _ := signer.MintFor(ws, "cs", ConnActor{AgentID: otherAgent})
	resp2 := postToolCall(t, srv, tokOK, body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || !hit {
		t.Fatalf("non-denied agent must proxy through: status=%d hit=%v", resp2.StatusCode, hit)
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

// newUUID returns a valid pgtype.UUID for tests by round-tripping a fixed
// string through the util parser (keeps the workspace id format realistic).
func newUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	return id
}
