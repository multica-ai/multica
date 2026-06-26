package agentvault

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListVaults_LoginThenList verifies the client logs in for a bearer token
// and decodes the agent-vault GET /v1/vaults response shape
// ({"vaults":[{id,name,...}]}), ignoring fields the picker does not need.
func TestListVaults_LoginThenList(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"adm_tok","expires_at":"2026-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/vaults":
			sawAuth = r.Header.Get("Authorization")
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

	c := NewClient(Config{InternalURL: srv.URL, AdminEmail: "admin@firtal.com", AdminPassword: "pw"})
	vaults, err := c.ListVaults(context.Background())
	if err != nil {
		t.Fatalf("ListVaults: %v", err)
	}
	if sawAuth != "Bearer adm_tok" {
		t.Errorf("Authorization header = %q, want %q", sawAuth, "Bearer adm_tok")
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

// TestListVaults_NonOKStatus surfaces a non-2xx from Agent Vault as an error so
// the read-only endpoint can map it to 502 rather than serving a bogus list.
func TestListVaults_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/login" {
			_, _ = w.Write([]byte(`{"token":"adm_tok"}`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(Config{InternalURL: srv.URL, AdminEmail: "a@b.c", AdminPassword: "pw"})
	if _, err := c.ListVaults(context.Background()); err == nil {
		t.Fatal("ListVaults: expected error on 500, got nil")
	}
}
