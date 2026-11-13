package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestSelfHostSetupHealthResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		status   int
		redirect bool
		healthy  bool
	}{
		{name: "HTML login page", body: "<html>Sign in</html>"},
		{name: "redirect to HTML login page", body: "<html>Sign in</html>", redirect: true},
		{name: "unrelated JSON", body: `{"status":"healthy"}`},
		{name: "unhealthy status", body: `{"status":"error"}`},
		{name: "missing status", body: `{"pid":123}`},
		{name: "non-string status", body: `{"status":true}`},
		{name: "null response", body: `null`},
		{name: "empty response"},
		{name: "malformed JSON", body: `{"status":"ok"`},
		{name: "trailing content", body: `{"status":"ok"}<html>Sign in</html>`},
		{name: "oversized response", body: `{"status":"ok","extra":"` + strings.Repeat("x", 4096) + `"}`},
		{name: "non-200 response", body: `{"status":"ok"}`, status: http.StatusServiceUnavailable},
		{name: "legacy healthy response", body: `{"status":"ok"}`, healthy: true},
		{name: "healthy response with extra fields", body: `{"status":"ok","pid":123,"commit":"abc123","started_at":"2026-11-10T00:00:00Z","future":{"key":true}}`, healthy: true},
		{name: "healthy response after redirect", body: `{"status":"ok"}`, redirect: true, healthy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			const profile = "self-host-health-test"
			existing := cli.CLIConfig{
				ServerURL:   "https://api.old.example",
				AppURL:      "https://old.example",
				WorkspaceID: "ws-1",
				Token:       "mul_existing_token",
				DeviceName:  "existing-device",
				RuntimeName: "existing-runtime",
			}
			if err := cli.SaveCLIConfigForProfile(existing, profile); err != nil {
				t.Fatal(err)
			}
			path, err := cli.CLIConfigPathForProfile(profile)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.redirect && r.URL.Path == "/health" {
					http.Redirect(w, r, "/redirected", http.StatusFound)
					return
				}
				// A proxy may rewrite Content-Type, so only the payload matters.
				w.Header().Set("Content-Type", "text/plain")
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			result, err := persistSelfHostConfigIfReachable(server.URL, "https://new.example", profile, probeServer)
			if err != nil {
				t.Fatal(err)
			}
			if result.healthy != tc.healthy {
				t.Errorf("healthy = %v, want %v", result.healthy, tc.healthy)
			}
			if !tc.healthy {
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Error("failed health probe changed the existing profile")
				}
				return
			}
			got, err := cli.LoadCLIConfigForProfile(profile)
			if err != nil {
				t.Fatal(err)
			}
			if got.ServerURL != server.URL || got.AppURL != "https://new.example" {
				t.Error("healthy backend URLs were not saved")
			}
		})
	}
}

func TestSetupSelfHostProbeFailure(t *testing.T) {
	for _, tc := range []struct {
		name            string
		existing        bool
		unreachable     bool
		status          int
		wantMessage     string
		wantGatewayHint bool
	}{
		{name: "invalid health preserves profile", existing: true, wantMessage: "did not return a healthy Multica backend response", wantGatewayHint: true},
		{name: "invalid health creates no profile", wantMessage: "did not return a healthy Multica backend response", wantGatewayHint: true},
		{name: "connection failure preserves profile", existing: true, unreachable: true, wantMessage: "is not reachable"},
		{name: "unauthorized response", existing: true, status: http.StatusUnauthorized, wantMessage: "returned HTTP 401 (Unauthorized) from /health"},
		{name: "bad gateway response", existing: true, status: http.StatusBadGateway, wantMessage: "returned HTTP 502 (Bad Gateway) from /health"},
		{name: "unavailable response", existing: true, status: http.StatusServiceUnavailable, wantMessage: "returned HTTP 503 (Service Unavailable) from /health"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			if tc.existing {
				if err := cli.SaveCLIConfig(cli.CLIConfig{
					ServerURL: "https://api.old.example", Token: "mul_existing_token", DeviceName: "existing-device",
				}); err != nil {
					t.Fatal(err)
				}
			}
			path, err := cli.CLIConfigPathForProfile("")
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				fmt.Fprint(w, "<html>Sign in</html>")
			}))
			defer server.Close()
			if tc.unreachable {
				server.Close()
			}
			cmd := testCmd()
			cmd.Flags().String("server-url", "", "")
			cmd.Flags().String("app-url", "", "")
			cmd.Flags().Int("frontend-port", 3000, "")
			if err := cmd.Flags().Set("server-url", server.URL); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Flags().Set("app-url", "https://new.example"); err != nil {
				t.Fatal(err)
			}

			stderr := captureStderr(t)
			defer stderr.restore()
			pipeStdin(t, "y\n", func() {
				if err := runSetupSelfHost(cmd, nil); err != nil {
					t.Fatalf("setup: %v", err)
				}
			})
			output := stderr.read()
			if !strings.Contains(output, tc.wantMessage) || !strings.Contains(output, "configuration was left unchanged") {
				t.Errorf("missing probe failure message: %s", output)
			}
			if strings.Contains(output, "login gateway") != tc.wantGatewayHint {
				t.Errorf("gateway hint should appear only for invalid HTTP 200 health responses: %s", output)
			}
			after, err := os.ReadFile(path)
			if !tc.existing {
				if !os.IsNotExist(err) {
					t.Errorf("failed setup created a profile: read error = %v", err)
				}
			} else if err != nil || !bytes.Equal(before, after) {
				t.Errorf("failed setup changed the existing profile: read error = %v", err)
			}
		})
	}
}

// TestPersistSelfHostConfigIfReachable verifies the fix for the
// setup-wipes-token bug: a failed reachability probe must leave the existing
// config (and its auth token) untouched, instead of overwriting it before the
// probe and bailing — which left the user logged out with no recovery.
func TestPersistSelfHostConfigIfReachable(t *testing.T) {
	t.Run("unreachable server preserves existing config and token", func(t *testing.T) {
		redirectTestHome(t, t.TempDir())
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
			func(string) serverProbeResult { return serverProbeResult{} },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if proceed.statusCode != 0 || proceed.healthy {
			t.Fatalf("probe result = %v, want unreachable", proceed)
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
		redirectTestHome(t, t.TempDir())

		proceed, err := persistSelfHostConfigIfReachable(
			"https://api.new.example", "https://new.example", "",
			func(string) serverProbeResult { return serverProbeResult{statusCode: http.StatusOK, healthy: true} },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !proceed.healthy {
			t.Fatalf("probe result = %v, want healthy", proceed)
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

func TestDispatchDaemonAfterSetup(t *testing.T) {
	newRunner := func(calls *[]string, name string) func(*cobra.Command, []string) error {
		return func(*cobra.Command, []string) error {
			*calls = append(*calls, name)
			return nil
		}
	}

	t.Run("restarts an existing daemon so new config takes effect", func(t *testing.T) {
		var calls []string
		err := dispatchDaemonAfterSetup(
			&cobra.Command{},
			nil,
			map[string]any{"status": "running"},
			newRunner(&calls, "start"),
			newRunner(&calls, "restart"),
		)
		if err != nil {
			t.Fatalf("dispatchDaemonAfterSetup: %v", err)
		}
		if got := strings.Join(calls, ","); got != "restart" {
			t.Fatalf("calls = %q, want restart", got)
		}
	})

	t.Run("starts when no daemon is running", func(t *testing.T) {
		var calls []string
		err := dispatchDaemonAfterSetup(
			&cobra.Command{},
			nil,
			map[string]any{"status": "stopped"},
			newRunner(&calls, "start"),
			newRunner(&calls, "restart"),
		)
		if err != nil {
			t.Fatalf("dispatchDaemonAfterSetup: %v", err)
		}
		if got := strings.Join(calls, ","); got != "start" {
			t.Fatalf("calls = %q, want start", got)
		}
	})

	t.Run("does not restart while tasks are active", func(t *testing.T) {
		var calls []string
		err := dispatchDaemonAfterSetup(
			&cobra.Command{},
			nil,
			map[string]any{
				"status":            "running",
				"active_task_count": float64(2),
			},
			newRunner(&calls, "start"),
			newRunner(&calls, "restart"),
		)
		if err == nil {
			t.Fatal("dispatchDaemonAfterSetup: want active-task error")
		}
		if !strings.Contains(err.Error(), "2 active tasks") ||
			!strings.Contains(err.Error(), "multica daemon restart") {
			t.Fatalf("error = %q, want active count and restart guidance", err)
		}
		if len(calls) != 0 {
			t.Fatalf("calls = %v, want neither start nor restart", calls)
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
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), cli.CLIConfig{})
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
		serverURL, userProvided := resolveSelfHostServerURL(cmd, cli.CLIConfig{})
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
		serverURL, userProvided := resolveSelfHostServerURL(cmd, cli.CLIConfig{})
		if serverURL != "http://localhost:9090" {
			t.Fatalf("server_url: want localhost default, got %q", serverURL)
		}
		if userProvided {
			t.Fatalf("userProvided: want false for localhost fallback")
		}
	})

	// Re-running `setup self-host` after `config set` (or an earlier setup)
	// must keep the configured remote server instead of probing localhost.
	t.Run("falls back to existing config server_url when no flag, env, or --port", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "https://api.example.com"}
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), existing)
		if serverURL != "https://api.example.com" {
			t.Fatalf("server_url: want existing config value, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for config-sourced URL")
		}
	})

	t.Run("explicit --port overrides existing config server_url", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "https://api.internal.co"}
		cmd := newCmd()
		if err := cmd.Flags().Set("port", "9090"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, userProvided := resolveSelfHostServerURL(cmd, existing)
		if serverURL != "http://localhost:9090" {
			t.Fatalf("server_url: want localhost from --port, got %q", serverURL)
		}
		if userProvided {
			t.Fatalf("userProvided: want false for localhost fallback")
		}
	})

	t.Run("flag wins over existing config", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "https://config.example"}
		cmd := newCmd()
		if err := cmd.Flags().Set("server-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		serverURL, _ := resolveSelfHostServerURL(cmd, existing)
		if serverURL != "https://flag.example" {
			t.Fatalf("server_url: want flag value, got %q", serverURL)
		}
	})

	// `config set server_url` stores the value as-is, so the config may hold
	// the documented ws:// daemon form. The fallback must normalize it to the
	// http(s) base — otherwise the probe hits the raw wss:// value and reports
	// the server as unreachable.
	t.Run("normalizes ws:// daemon form from existing config", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "")
		existing := cli.CLIConfig{ServerURL: "wss://api.internal.co/ws"}
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), existing)
		if serverURL != "https://api.internal.co" {
			t.Fatalf("server_url: want normalized https base, got %q", serverURL)
		}
		if !userProvided {
			t.Fatalf("userProvided: want true for config-sourced URL")
		}
	})

	// MULTICA_SERVER_URL is documented as a ws:// daemon address; the probe and
	// stored config need an http(s) base, so the ws/wss + /ws form must be
	// normalized just like every other command does.
	t.Run("normalizes the documented ws:// daemon form", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "wss://api.internal.co/ws")
		serverURL, userProvided := resolveSelfHostServerURL(newCmd(), cli.CLIConfig{})
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

// TestResolveSelfHostAppURL mirrors TestResolveSelfHostServerURL for the
// frontend URL: flag wins, then env, then an already-configured app_url (so
// re-running setup self-host keeps a remote frontend), then "" so the caller
// infers it. An explicit --frontend-port opts out of the config fallback.
func TestResolveSelfHostAppURL(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.Flags().String("app-url", "", "")
		c.Flags().Int("frontend-port", 3000, "")
		return c
	}

	t.Run("flag wins over env and config", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "https://env.example")
		existing := cli.CLIConfig{AppURL: "https://config.example"}
		cmd := newCmd()
		if err := cmd.Flags().Set("app-url", "https://flag.example"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if got := resolveSelfHostAppURL(cmd, existing); got != "https://flag.example" {
			t.Fatalf("app_url: want flag value, got %q", got)
		}
	})

	t.Run("env wins over config when flag absent", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "https://env.example")
		existing := cli.CLIConfig{AppURL: "https://config.example"}
		if got := resolveSelfHostAppURL(newCmd(), existing); got != "https://env.example" {
			t.Fatalf("app_url: want env value, got %q", got)
		}
	})

	t.Run("falls back to existing config when no flag, env, or --frontend-port", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "")
		existing := cli.CLIConfig{AppURL: "https://app.example.com"}
		if got := resolveSelfHostAppURL(newCmd(), existing); got != "https://app.example.com" {
			t.Fatalf("app_url: want existing config value, got %q", got)
		}
	})

	t.Run("explicit --frontend-port skips config fallback", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "")
		existing := cli.CLIConfig{AppURL: "https://config.example"}
		cmd := newCmd()
		if err := cmd.Flags().Set("frontend-port", "4000"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if got := resolveSelfHostAppURL(cmd, existing); got != "" {
			t.Fatalf("app_url: want empty so caller infers localhost, got %q", got)
		}
	})

	t.Run("empty when nothing set", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "")
		if got := resolveSelfHostAppURL(newCmd(), cli.CLIConfig{}); got != "" {
			t.Fatalf("app_url: want empty, got %q", got)
		}
	})
}

// TestFormatURLChange covers the setup overwrite prompt: a changed value is
// shown as "old -> new" so the passed --server-url/--app-url is visibly
// received, while an unchanged value renders plain.
func TestFormatURLChange(t *testing.T) {
	cases := []struct {
		name   string
		oldVal string
		newVal string
		want   string
	}{
		{"changed", "http://localhost:8080", "https://api.internal.co", "http://localhost:8080  ->  https://api.internal.co"},
		{"unchanged", "https://api.internal.co", "https://api.internal.co", "https://api.internal.co"},
		{"empty new keeps old", "http://localhost:8080", "", "http://localhost:8080"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := formatURLChange(tc.oldVal, tc.newVal); got != tc.want {
				t.Errorf("formatURLChange(%q, %q) = %q, want %q", tc.oldVal, tc.newVal, got, tc.want)
			}
		})
	}
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

func TestSetupCommandsFailClosedInTaskContext(t *testing.T) {
	ownerHome := t.TempDir()
	redirectTestHome(t, ownerHome)
	t.Setenv("MULTICA_AGENT_ID", "agent-test")
	t.Setenv("MULTICA_TASK_ID", "task-test")
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", filepath.Join(t.TempDir(), "task-multica"))

	ownerPath := filepath.Join(ownerHome, ".multica", "config.json")
	if err := os.MkdirAll(filepath.Dir(ownerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	ownerBytes := []byte("{\n  \"server_url\": \"https://owner.invalid\",\n  \"token\": \"mul_owner_sentinel\"\n}\n")
	if err := os.WriteFile(ownerPath, ownerBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, run := range map[string]func() error{
		"cloud":     func() error { return runSetupCloud(setupCmd, nil) },
		"self-host": func() error { return runSetupSelfHost(setupSelfHostCmd, nil) },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "not available inside a daemon-managed task") {
			t.Fatalf("setup %s error = %v, want task-context guard", name, err)
		}
	}
	after, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(ownerBytes) {
		t.Fatalf("owner config content changed: got %q", after)
	}
}
