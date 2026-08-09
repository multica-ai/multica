package mcprelay

// FIR-4779: the relay forwards the calling agent to an mcp_http connection that
// has on_behalf_of enabled, so the downstream MCP server can attribute a write
// to that agent instead of the shared connection key.
//
// The security property under test is that the value is server-owned: it comes
// from the HMAC-signed relay token minted at claim, and any X-On-Behalf-Of the
// runtime sent is dropped first — so an agent cannot name itself.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/util"
)

// onBehalfOfRelay stands up a relay in front of a recording upstream. It returns
// the relay server, the workspace id, the signer, and a getter for the last
// X-On-Behalf-Of the upstream saw (with a bool for "header present at all", so a
// stripped header is distinguishable from an empty one).
func onBehalfOfRelay(t *testing.T, enabled bool) (*httptest.Server, string, *Signer, func() (string, bool)) {
	t.Helper()
	var got string
	var present bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(onBehalfOfHeader)
		_, present = r.Header[onBehalfOfHeader]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	ws := util.UUIDToString(newUUID(t))
	signer := NewSigner("secret")
	conn := connections.Connection{
		WorkspaceID: ws, Name: "finance-mcp", Type: connections.TypeMCPHTTP, Internal: true,
		URL:        upstream.URL + "/api/mcp",
		AuthConfig: connections.AuthConfig{BearerToken: "real"},
	}
	if enabled {
		conn.AuthConfig.OnBehalfOf = &connections.OnBehalfOfConfig{Enabled: true}
	}
	r := chi.NewRouter()
	r.Handle("/mcp-relay/{name}", NewRelay(signer, stubLoader{conn: conn}, nil))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return srv, ws, signer, func() (string, bool) { return got, present }
}

// call posts through the relay, optionally with a forged X-On-Behalf-Of set by
// the "runtime". forged == "" sends no header at all.
func call(t *testing.T, srv *httptest.Server, tok, forged string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp-relay/finance-mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	if forged != "" {
		req.Header.Set(onBehalfOfHeader, forged)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("relay request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relay status = %d, want 200", resp.StatusCode)
	}
}

func TestRelayStampsCallingAgentWhenOnBehalfOfEnabled(t *testing.T) {
	srv, ws, signer, seen := onBehalfOfRelay(t, true)
	agent := mustUUID(t, "33333333-3333-3333-3333-333333333333")

	tok, err := signer.MintFor(ws, "finance-mcp", ConnActor{AgentID: agent})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	call(t, srv, tok, "")

	got, _ := seen()
	want := "agent:" + util.UUIDToString(agent)
	if got != want {
		t.Fatalf("upstream %s = %q, want %q", onBehalfOfHeader, got, want)
	}
}

func TestRelayOverridesRuntimeSuppliedIdentity(t *testing.T) {
	srv, ws, signer, seen := onBehalfOfRelay(t, true)
	realAgent := mustUUID(t, "33333333-3333-3333-3333-333333333333")

	tok, err := signer.MintFor(ws, "finance-mcp", ConnActor{AgentID: realAgent})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// The runtime claims to be somebody else; the token says otherwise.
	call(t, srv, tok, "agent:99999999-9999-9999-9999-999999999999")

	got, _ := seen()
	want := "agent:" + util.UUIDToString(realAgent)
	if got != want {
		t.Fatalf("upstream %s = %q, want the token's agent %q", onBehalfOfHeader, got, want)
	}
}

func TestRelayDropsRuntimeIdentityWithoutActor(t *testing.T) {
	// A workspace-scoped token (Mint) carries no actor — the agent-less runtime
	// tool scan uses it. No header may reach upstream, and a forged one must not
	// survive as a substitute.
	srv, ws, signer, seen := onBehalfOfRelay(t, true)

	tok, err := signer.Mint(ws, "finance-mcp")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	call(t, srv, tok, "agent:99999999-9999-9999-9999-999999999999")

	if got, present := seen(); present {
		t.Fatalf("upstream saw %s = %q, want the header absent", onBehalfOfHeader, got)
	}
}

func TestRelaySendsNoIdentityWhenOnBehalfOfDisabled(t *testing.T) {
	// on_behalf_of is opt-in per connection and off by default: turning it off is
	// the kill switch, so a disabled connection must send nothing even when the
	// token carries a real actor.
	srv, ws, signer, seen := onBehalfOfRelay(t, false)
	agent := mustUUID(t, "33333333-3333-3333-3333-333333333333")

	tok, err := signer.MintFor(ws, "finance-mcp", ConnActor{AgentID: agent})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	call(t, srv, tok, "agent:99999999-9999-9999-9999-999999999999")

	if got, present := seen(); present {
		t.Fatalf("upstream saw %s = %q, want the header absent", onBehalfOfHeader, got)
	}
}
