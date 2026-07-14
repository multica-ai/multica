package agentvault

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
)

// fakeConns is a static connectionResolver for tests.
type fakeConns struct {
	list []connections.Connection
	err  error
}

func (f fakeConns) ListEnabled(context.Context, pgtype.UUID) ([]connections.Connection, error) {
	return f.list, f.err
}

// agentVaultConn builds an enabled "Agent Vault" REST API connection pointing at
// url with the given bearer token.
func agentVaultConn(url, bearer string) connections.Connection {
	return connections.Connection{
		Name:       "agent-vault",
		Type:       connections.TypeAPI,
		URL:        url,
		Enabled:    true,
		AuthConfig: connections.AuthConfig{BearerToken: bearer},
	}
}

// TestListVaults_ViaConnection verifies the client resolves the Agent Vault
// connection, calls GET /v1/vaults with the connection's own bearer token (never
// an admin login), and decodes the agent-vault response shape
// ({"vaults":[{id,name,...}]}), ignoring fields the picker does not need.
func TestListVaults_ViaConnection(t *testing.T) {
	var sawAuth, sawPath string
	loginCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/login":
			loginCalled = true
			http.Error(w, "login must not be called", http.StatusForbidden)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/vaults":
			sawAuth = r.Header.Get("Authorization")
			sawPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"vaults":[
				{"id":"v1","name":"bigquery","role":"admin","membership":"explicit","created_at":"2026-01-01T00:00:00Z","pending_proposals":0},
				{"id":"v2","name":"cloudflare","membership":"implicit","created_at":"2026-01-01T00:00:00Z","pending_proposals":2,"credential_store":{"kind":"infisical"}}
			]}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	conns := fakeConns{list: []connections.Connection{agentVaultConn(srv.URL, "av_agt_tok")}}
	c := NewClient(Config{InternalURL: srv.URL}, conns)

	vaults, err := c.ListVaults(context.Background(), pgtype.UUID{})
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	if loginCalled {
		t.Error("admin login was called; it must be gone")
	}
	if sawPath != "/v1/vaults" {
		t.Errorf("path = %q, want /v1/vaults", sawPath)
	}
	if sawAuth != "Bearer av_agt_tok" {
		t.Errorf("Authorization = %q, want %q", sawAuth, "Bearer av_agt_tok")
	}
	if len(vaults) != 2 {
		t.Fatalf("len(vaults) = %d, want 2", len(vaults))
	}
	if vaults[0].ID != "v1" || vaults[0].Name != "bigquery" {
		t.Errorf("vaults[0] = %+v, want {v1 bigquery}", vaults[0])
	}
	if vaults[1].ID != "v2" || vaults[1].Name != "cloudflare" {
		t.Errorf("vaults[1] = %+v, want {v2 cloudflare}", vaults[1])
	}
}

// TestListVaults_NoConnection degrades to an empty list (not an error) when the
// workspace has no Agent Vault connection, so the picker shows "no vaults
// available" rather than breaking the Permissions screen.
func TestListVaults_NoConnection(t *testing.T) {
	c := NewClient(Config{InternalURL: "http://agent-vault.internal:14321"}, fakeConns{})
	vaults, err := c.ListVaults(context.Background(), pgtype.UUID{})
	if err != nil {
		t.Fatalf("ListVaults: unexpected error %v", err)
	}
	if len(vaults) != 0 {
		t.Fatalf("len(vaults) = %d, want 0", len(vaults))
	}
}

// TestListVaults_NilResolver degrades to an empty list when no resolver is wired.
func TestListVaults_NilResolver(t *testing.T) {
	c := NewClient(Config{InternalURL: "http://agent-vault.internal:14321"}, nil)
	vaults, err := c.ListVaults(context.Background(), pgtype.UUID{})
	if err != nil || len(vaults) != 0 {
		t.Fatalf("ListVaults = (%v, %v), want (empty, nil)", vaults, err)
	}
}

// TestListVaults_MissingBearer surfaces a misconfigured connection (matched by
// host but carrying no bearer token) as an error rather than a silent empty list.
func TestListVaults_MissingBearer(t *testing.T) {
	conns := fakeConns{list: []connections.Connection{agentVaultConn("http://agent-vault.internal:14321", "")}}
	c := NewClient(Config{InternalURL: "http://agent-vault.internal:14321"}, conns)
	if _, err := c.ListVaults(context.Background(), pgtype.UUID{}); err == nil {
		t.Fatal("ListVaults: expected error for connection without bearer token")
	}
}

// TestHostOf checks host:port extraction, including the scheme-less fallback so
// a mis-set AGENT_VAULT_INTERNAL_URL override still matches a scheme-ful
// connection URL on the same host.
func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"http://agent-vault.internal:14321":      "agent-vault.internal:14321",
		"http://agent-vault.internal:14321/":     "agent-vault.internal:14321",
		"http://agent-vault.internal:14321/v1/x": "agent-vault.internal:14321",
		"agent-vault.internal:14321":             "agent-vault.internal:14321",
		"  http://agent-vault.internal:14321  ":  "agent-vault.internal:14321",
		"":                                       "",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestListVaults_NonOKStatus surfaces a non-2xx from Agent Vault as an error so
// the read-only endpoint can map it to 502 rather than serving a bogus list.
func TestListVaults_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	conns := fakeConns{list: []connections.Connection{agentVaultConn(srv.URL, "av_agt_tok")}}
	c := NewClient(Config{InternalURL: srv.URL}, conns)
	if _, err := c.ListVaults(context.Background(), pgtype.UUID{}); err == nil {
		t.Fatal("ListVaults: expected error on 500, got nil")
	}
}

func TestRevealCredential_ViaConnectionFetchesOnlyRequestedKey(t *testing.T) {
	const secret = "registry-password-must-not-leak"
	var sawAuth, sawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":["PASSWORD"],"credentials":[{"key":"PASSWORD","type":"static","value":"` + secret + `"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{InternalURL: srv.URL}, fakeConns{list: []connections.Connection{agentVaultConn(srv.URL, "owner-token")}})
	value, err := c.RevealCredential(context.Background(), pgtype.UUID{}, "Shared/browser-login/registry", "PASSWORD")
	if err != nil {
		t.Fatalf("RevealCredential: %v", err)
	}
	if value != secret {
		t.Fatal("RevealCredential returned the wrong value")
	}
	if sawAuth != "Bearer owner-token" {
		t.Errorf("Authorization = %q", sawAuth)
	}
	query, _ := url.ParseQuery(sawQuery)
	if query.Get("vault") != "Shared/browser-login/registry" || query.Get("reveal") != "true" || query.Get("key") != "PASSWORD" {
		t.Errorf("query = %q", sawQuery)
	}
}

func TestRevealCredential_RejectsNonBrowserLoginVault(t *testing.T) {
	c := NewClient(Config{}, nil)
	if _, err := c.RevealCredential(context.Background(), pgtype.UUID{}, "shared-vault", "PASSWORD"); err == nil {
		t.Fatal("expected a non browser-login vault to be rejected")
	}
}

func TestRevealCredential_ErrorNeverIncludesResponseBody(t *testing.T) {
	const secret = "registry-password-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, secret, http.StatusBadGateway)
	}))
	defer srv.Close()
	c := NewClient(Config{InternalURL: srv.URL}, fakeConns{list: []connections.Connection{agentVaultConn(srv.URL, "owner-token")}})
	_, err := c.RevealCredential(context.Background(), pgtype.UUID{}, "Shared/browser-login/registry", "PASSWORD")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("sensitive response body leaked through error: %v", err)
	}
}
