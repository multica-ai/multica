package main

import (
	"testing"

	"github.com/spf13/cobra"

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

// TestResolveSelfHostServerURL covers GitHub #3912: `setup self-host` must
// honor MULTICA_SERVER_URL when --server-url is not passed, instead of always
// defaulting to localhost (which left self-hosters stuck on an "unreachable"
// error). The flag still wins over the env var.
func TestResolveSelfHostServerURL(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.Flags().String("server-url", "", "")
		c.Flags().Int("port", 8080, "")
		return c
	}

	t.Run("env var honored when flag absent", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "https://api.internal.co")
		serverURL, userProvided := resolveSelfHostServerURL(newCmd())
		if serverURL != "https://api.internal.co" {
			t.Fatalf("server_url: want env value, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for env-sourced URL")
		}
	})

	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "https://env.example")
		cmd := newCmd()
		if err := cmd.Flags().Set("server-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, userProvided := resolveSelfHostServerURL(cmd)
		if serverURL != "https://flag.example" {
			t.Fatalf("server_url: want flag value, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for flag-sourced URL")
		}
	})

	t.Run("falls back to localhost with --port when neither set", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "")
		cmd := newCmd()
		if err := cmd.Flags().Set("port", "9090"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, userProvided := resolveSelfHostServerURL(cmd)
		if serverURL != "http://localhost:9090" {
			t.Fatalf("server_url: want localhost default, got %q", serverURL)
		}
		if userProvided {
			t.Fatalf("userProvided: want false for localhost fallback")
		}
	})

	// MULTICA_SERVER_URL is documented as a ws:// daemon address; the probe and
	// stored config need an http(s) base, so the ws/wss + /ws form must be
	// normalized just like every other command does.
	t.Run("normalizes the documented ws:// daemon form", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "wss://api.internal.co/ws")
		serverURL, userProvided := resolveSelfHostServerURL(newCmd())
		if serverURL != "https://api.internal.co" {
			t.Fatalf("server_url: want normalized https base, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for env-sourced URL")
		}
	})
}

// TestSelfHostAppURLHonorsEnv pins the app-url half of the GitHub #3912 fix:
// setup self-host resolves --app-url through the same FlagOrEnv path, so
// MULTICA_APP_URL is honored when the flag is absent.
func TestSelfHostAppURLHonorsEnv(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("app-url", "", "")

	t.Run("env honored when flag absent", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "https://app.internal.co")
		if got := cli.FlagOrEnv(cmd, "app-url", "MULTICA_APP_URL", ""); got != "https://app.internal.co" {
			t.Fatalf("app_url: want env value, got %q", got)
		}
	})

	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "https://env.example")
		if err := cmd.Flags().Set("app-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if got := cli.FlagOrEnv(cmd, "app-url", "MULTICA_APP_URL", ""); got != "https://flag.example" {
			t.Fatalf("app_url: want flag value, got %q", got)
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
