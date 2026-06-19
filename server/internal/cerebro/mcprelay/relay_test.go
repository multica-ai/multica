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
	relay := NewRelay(signer, stubLoader{conn: conn})

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
	relay := NewRelay(signer, stubLoader{})
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
