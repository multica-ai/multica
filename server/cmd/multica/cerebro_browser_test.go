package main

// FIR-2037 — tests for the `multica cerebro-browser open` subcommand.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
)

// writeTestSidecar points the CLI at a stub control server by writing the
// rendezvous file the desktop side normally writes. HOME is overridden to a
// temp dir so this never touches a real ~/.multica.
func writeTestSidecar(t *testing.T, home string, port int, token string) {
	t.Helper()
	dir := filepath.Join(home, ".multica")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	body, _ := json.Marshal(cerebroBrowserSidecar{Port: port, Token: token, PID: os.Getpid()})
	if err := os.WriteFile(filepath.Join(dir, "cerebro-browser-control.json"), body, 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

func portIntOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	p := portOf(t, srv)
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("parse port %q: %v", p, err)
	}
	return n
}

func TestCerebroBrowserOpen_SidecarPresent_PostsOpenTab(t *testing.T) {
	const token = "test-token-abcdef"
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows home resolution
	t.Setenv(personalBrowserGrantEnvCLI, "1")
	t.Setenv("MULTICA_TOKEN", "agent-token-xyz")
	writeTestSidecar(t, home, portIntOf(t, srv), token)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := runCerebroBrowserOpen(cmd, "https://example.com/dash"); err != nil {
		t.Fatalf("runCerebroBrowserOpen: %v", err)
	}

	if gotPath != "/agent/open-tab" {
		t.Errorf("posted to %q, want /agent/open-tab", gotPath)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("auth header = %q, want Bearer %s", gotAuth, token)
	}
	if gotBody["url"] != "https://example.com/dash" {
		t.Errorf("body url = %v, want https://example.com/dash", gotBody["url"])
	}
	// The CLI forwards the agent token so the desktop can authorize as the agent.
	if gotBody["agentToken"] != "agent-token-xyz" {
		t.Errorf("body agentToken = %v, want agent-token-xyz", gotBody["agentToken"])
	}
}

func TestCerebroBrowserOpen_NoGrantEnv_Refuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(personalBrowserGrantEnvCLI, "")

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := runCerebroBrowserOpen(cmd, ""); err == nil {
		t.Fatal("expected an error when the grant env is unset, got nil")
	}
}

func TestCerebroBrowserSecureFill_SendsReferencesOnly(t *testing.T) {
	const secret = "registry-password-must-not-leak"
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/secure-fill" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"ok":true,"audit":{"vault":"Shared/browser-login/registry","key":"PASSWORD"}}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(personalBrowserGrantEnvCLI, "1")
	t.Setenv("MULTICA_TOKEN", "agent-token")
	writeTestSidecar(t, home, portIntOf(t, srv), "sidecar-token")
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := callCerebroBrowser(cmd, "secure-fill", map[string]any{"ref": "@e7", "vault": "Shared/browser-login/registry", "key": "PASSWORD"}); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(body)
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(out.Bytes(), []byte(secret)) {
		t.Fatal("secret leaked into the CLI request or output")
	}
	if _, exists := body["value"]; exists {
		t.Fatal("secure-fill request must never accept a plaintext value")
	}
}
