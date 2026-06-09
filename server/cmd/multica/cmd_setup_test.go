package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

// TestPersistSelfHostConfigIfReachable verifies the fix for the
// setup-wipes-token bug: a failed reachability probe must leave the existing
// config (and its auth token) untouched, instead of overwriting it before the
// probe and bailing — which left the user logged out with no recovery.
func TestPersistSelfHostConfigIfReachable(t *testing.T) {
	t.Run("unreachable server preserves existing config and token", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		existing := cli.CLIConfig{
			ServerURL:   "https://api.old.example",
			AppURL:      "https://old.example",
			WorkspaceID: "ws-1",
			Token:       "mul_existing_token",
		}
		if err := cli.SaveCLIConfig(existing); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		proceed, err := persistSelfHostConfigIfReachable(
			"https://api.new.example", "https://new.example", "",
			func(string) bool { return false },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if proceed {
			t.Fatalf("proceed: want false for unreachable server")
		}

		got, err := cli.LoadCLIConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if got.Token != "mul_existing_token" {
			t.Fatalf("token: want preserved, got %q", got.Token)
		}
		if got.ServerURL != "https://api.old.example" {
			t.Fatalf("server_url: want unchanged, got %q", got.ServerURL)
		}
	})

	t.Run("reachable server writes new self-host config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		proceed, err := persistSelfHostConfigIfReachable(
			"https://api.new.example", "https://new.example", "",
			func(string) bool { return true },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !proceed {
			t.Fatalf("proceed: want true for reachable server")
		}

		got, err := cli.LoadCLIConfig()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if got.ServerURL != "https://api.new.example" || got.AppURL != "https://new.example" {
			t.Fatalf("config not written: %+v", got)
		}
	})
}

func TestServerHostIsLocal(t *testing.T) {
	cases := []struct {
		name   string
		server string
		want   bool
	}{
		{"localhost", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:8080", true},
		{"IPv6 loopback", "http://[::1]:8080", true},
		{"LAN IP", "http://192.168.0.28:8080", false},
		{"public FQDN", "https://api.internal.co", false},
		{"unparseable", "://bad", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := serverHostIsLocal(tc.server); got != tc.want {
				t.Errorf("serverHostIsLocal(%q) = %v, want %v", tc.server, got, tc.want)
			}
		})
	}
}
