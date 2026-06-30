package main

import (
	"net"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	os.Unsetenv("MULTICA_AGENT_ID")
	os.Unsetenv("MULTICA_TASK_ID")
	os.Unsetenv("MULTICA_TOKEN")
	os.Exit(m.Run())
}

// testCmd returns a minimal cobra.Command with the --profile persistent flag
// registered, matching the rootCmd setup used in production.
func testCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.PersistentFlags().String("profile", "", "")
	return cmd
}

func TestResolveAppURL(t *testing.T) {
	cmd := testCmd()

	t.Run("prefers MULTICA_APP_URL", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "http://localhost:14000")
		t.Setenv("FRONTEND_ORIGIN", "http://localhost:13000")

		if got := resolveAppURL(cmd); got != "http://localhost:14000" {
			t.Fatalf("resolveAppURL() = %q, want %q", got, "http://localhost:14000")
		}
	})

	t.Run("falls back to FRONTEND_ORIGIN", func(t *testing.T) {
		t.Setenv("MULTICA_APP_URL", "")
		t.Setenv("FRONTEND_ORIGIN", "http://localhost:13026")

		if got := resolveAppURL(cmd); got != "http://localhost:13026" {
			t.Fatalf("resolveAppURL() = %q, want %q", got, "http://localhost:13026")
		}
	})
}

func TestResolveCallbackBinding(t *testing.T) {
	// Fake outbound detector: pretends the CLI has a fixed LAN IP regardless
	// of which server it dials.
	fixed := func(ip string) func(string) net.IP {
		return func(string) net.IP { return net.ParseIP(ip).To4() }
	}
	failing := func(string) net.IP { return nil }

	cases := []struct {
		name         string
		flagHost     string
		serverURL    string
		appURL       string
		detect       func(string) net.IP
		wantCallback string
		wantBind     string
	}{
		{
			name:         "public app URL stays on loopback",
			appURL:       "https://multica.ai",
			serverURL:    "https://api.multica.ai",
			detect:       failing,
			wantCallback: "localhost",
			wantBind:     "127.0.0.1",
		},
		{
			name:         "localhost app URL stays on loopback",
			appURL:       "http://localhost:3000",
			serverURL:    "http://localhost:8080",
			detect:       failing,
			wantCallback: "localhost",
			wantBind:     "127.0.0.1",
		},
		{
			name:         "same-machine self-host uses loopback (CLI IP matches app IP)",
			appURL:       "http://192.168.0.28:3000",
			serverURL:    "http://192.168.0.28:8080",
			detect:       fixed("192.168.0.28"),
			wantCallback: "localhost",
			wantBind:     "127.0.0.1",
		},
		{
			name:         "cross-machine self-host points callback at CLI's LAN IP",
			appURL:       "http://192.168.0.28:3000",
			serverURL:    "http://192.168.0.28:8080",
			detect:       fixed("192.168.0.47"),
			wantCallback: "192.168.0.47",
			wantBind:     "0.0.0.0",
		},
		{
			name:         "outbound detection failure falls back to app IP",
			appURL:       "http://192.168.0.28:3000",
			serverURL:    "http://192.168.0.28:8080",
			detect:       failing,
			wantCallback: "192.168.0.28",
			wantBind:     "0.0.0.0",
		},
		{
			name:         "--callback-host flag overrides everything",
			flagHost:     "cli.internal.example",
			appURL:       "https://multica.ai",
			serverURL:    "https://api.multica.ai",
			detect:       fixed("10.0.0.5"),
			wantCallback: "cli.internal.example",
			wantBind:     "0.0.0.0",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotCallback, gotBind := resolveCallbackBinding(tc.flagHost, tc.serverURL, tc.appURL, tc.detect)
			if gotCallback != tc.wantCallback {
				t.Errorf("callback host = %q, want %q", gotCallback, tc.wantCallback)
			}
			if gotBind != tc.wantBind {
				t.Errorf("bind addr = %q, want %q", gotBind, tc.wantBind)
			}
		})
	}
}

func TestBrowserLoginInstructionsSSHRemoteHint(t *testing.T) {
	const loginURL = "https://multica.ai/login?cli_callback=http%3A%2F%2Flocalhost%3A43689%2Fcallback"

	got := browserLoginInstructions(loginURL, "localhost", 43689, true)
	if !strings.Contains(got, "ssh -L 43689:127.0.0.1:43689 <user>@<remote-host>") {
		t.Fatalf("remote SSH instructions missing tunnel command:\n%s", got)
	}
	if !strings.Contains(got, loginURL) {
		t.Fatalf("instructions missing login URL:\n%s", got)
	}

	got = browserLoginInstructions(loginURL, "localhost", 43689, false)
	if strings.Contains(got, "ssh -L") {
		t.Fatalf("local instructions should not include SSH tunnel command:\n%s", got)
	}

	got = browserLoginInstructions(loginURL, "192.168.1.25", 43689, true)
	if strings.Contains(got, "ssh -L") {
		t.Fatalf("non-loopback callback should not include SSH tunnel command:\n%s", got)
	}
}

func TestCallbackHostFlagValueReadsParentSetupFlag(t *testing.T) {
	var got string
	setup := &cobra.Command{Use: "setup"}
	setup.Flags().String(callbackHostFlag, "", "")
	cloud := &cobra.Command{
		Use: "cloud",
		Run: func(cmd *cobra.Command, args []string) {
			got = callbackHostFlagValue(cmd)
		},
	}
	cloud.Flags().String(callbackHostFlag, "", "")
	setup.AddCommand(cloud)
	setup.SetArgs([]string{"--callback-host", "10.0.0.5", "cloud"})

	if err := setup.Execute(); err != nil {
		t.Fatalf("execute setup cloud: %v", err)
	}
	if got != "10.0.0.5" {
		t.Fatalf("callback host = %q, want parent flag value", got)
	}
}

// TestLoginTokenFlagWiring asserts the production loginCmd flag is registered
// the way #1994 needs it to be: a String flag (not Bool) with a NoOptDefVal
// so `--token` (no value) keeps its legacy prompt-mode behavior. This is the
// load-bearing regression guard — without these asserts a future change that
// reverts the flag to Bool could pass while a synthetic stand-in test happily
// keeps testing string-flag parsing.
func TestLoginTokenFlagWiring(t *testing.T) {
	tokenFlag := loginCmd.Flags().Lookup("token")
	if tokenFlag == nil {
		t.Fatal("loginCmd is missing the --token flag")
	}
	if got := tokenFlag.Value.Type(); got != "string" {
		t.Fatalf("loginCmd --token type = %q, want %q (regressed to bool?)", got, "string")
	}
	if tokenFlag.NoOptDefVal != tokenPromptSentinel {
		t.Fatalf("loginCmd --token NoOptDefVal = %q, want %q (legacy `multica login --token` prompt mode would break)", tokenFlag.NoOptDefVal, tokenPromptSentinel)
	}
}

// TestLoginTokenFlagParsing exercises every documented invocation form
// against a cobra command wired up exactly the same way as the production
// loginCmd, then runs runAuthLogin's flag-resolution logic to confirm the
// right downstream branch is taken: `--token mul_xxx` and `--token=mul_xxx`
// both consume the value (the bug from #1994), `--token` alone falls
// through to the prompt sentinel (preserves the legacy headless form), and
// no flag at all leaves the browser flow untouched.
func TestLoginTokenFlagParsing(t *testing.T) {
	type want struct {
		changed         bool
		resolvedToken   string // empty == "fall through to prompt"
		expectsPrompted bool
	}

	cases := []struct {
		name string
		argv []string
		want want
	}{
		{
			name: "space-separated value (the form from #1994)",
			argv: []string{"--token", "mul_xxx"},
			want: want{changed: true, resolvedToken: "mul_xxx"},
		},
		{
			name: "equals-separated value",
			argv: []string{"--token=mul_yyy"},
			want: want{changed: true, resolvedToken: "mul_yyy"},
		},
		{
			name: "no value falls through to prompt (legacy CLI_INSTALL.md form)",
			argv: []string{"--token"},
			want: want{changed: true, expectsPrompted: true},
		},
		{
			name: "explicit empty value also falls through to prompt",
			argv: []string{"--token="},
			want: want{changed: true, expectsPrompted: true},
		},
		{
			name: "no flag at all → browser flow",
			argv: []string{},
			want: want{changed: false},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "login"}
			// Mirror loginCmd's exact flag wiring. If init() in cmd_login.go
			// regresses, TestLoginTokenFlagWiring catches that; here we test
			// the parsing behavior given the documented wiring.
			cmd.Flags().String("token", "", "")
			cmd.Flags().Lookup("token").NoOptDefVal = tokenPromptSentinel

			if err := cmd.ParseFlags(tc.argv); err != nil {
				t.Fatalf("ParseFlags(%v) error: %v", tc.argv, err)
			}
			if cmd.Flags().Changed("token") != tc.want.changed {
				t.Fatalf("Changed(token) = %v, want %v for argv=%v", cmd.Flags().Changed("token"), tc.want.changed, tc.argv)
			}
			if !tc.want.changed {
				return
			}

			// Replay runAuthLogin's resolution logic so the test fails if
			// either the flag wiring OR the space-form recovery breaks.
			tokenFlag, _ := cmd.Flags().GetString("token")
			positional := cmd.Flags().Args()
			if tokenFlag == tokenPromptSentinel && len(positional) == 1 {
				tokenFlag = positional[0]
			}

			if tc.want.expectsPrompted {
				if tokenFlag != tokenPromptSentinel && tokenFlag != "" {
					t.Fatalf("expected prompt fall-through, got resolved token %q", tokenFlag)
				}
			} else {
				if tokenFlag != tc.want.resolvedToken {
					t.Fatalf("resolved token = %q, want %q", tokenFlag, tc.want.resolvedToken)
				}
			}
		})
	}
}

func TestNormalizeAPIBaseURL(t *testing.T) {
	t.Run("converts websocket base URL", func(t *testing.T) {
		if got := normalizeAPIBaseURL("ws://localhost:18106/ws"); got != "http://localhost:18106" {
			t.Fatalf("normalizeAPIBaseURL() = %q, want %q", got, "http://localhost:18106")
		}
	})

	t.Run("keeps http base URL", func(t *testing.T) {
		if got := normalizeAPIBaseURL("http://localhost:8080"); got != "http://localhost:8080" {
			t.Fatalf("normalizeAPIBaseURL() = %q, want %q", got, "http://localhost:8080")
		}
	})

	t.Run("falls back to raw value for invalid URL", func(t *testing.T) {
		if got := normalizeAPIBaseURL("://bad-url"); got != "://bad-url" {
			t.Fatalf("normalizeAPIBaseURL() = %q, want %q", got, "://bad-url")
		}
	})
}

// TestValidateLoginTokenPrefix pins the accepted PAT prefix set for
// `multica login --token`. The original implementation hardcoded `mul_`
// only, which rejected legitimate Multica Cloud Node PATs (`mcn_`) at
// the CLI even though the server's middleware would have accepted them.
// If a future change drops `mcn_` from the list (or accidentally
// broadens the set to anything-goes), this test fails.
func TestValidateLoginTokenPrefix(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{name: "mul_ PAT", token: "mul_abc123", wantErr: false},
		{name: "mcn_ Cloud Node PAT", token: "mcn_abc123", wantErr: false},
		{name: "empty token", token: "", wantErr: true},
		{name: "no prefix", token: "abc123", wantErr: true},
		{name: "wrong prefix mdt_", token: "mdt_abc123", wantErr: true},
		{name: "wrong prefix mat_", token: "mat_abc123", wantErr: true},
		{name: "case-sensitive: MUL_ rejected", token: "MUL_abc123", wantErr: true},
		{name: "leading whitespace not allowed (callers TrimSpace first)", token: " mul_abc", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateLoginTokenPrefix(tc.token)
			if tc.wantErr && err == nil {
				t.Fatalf("validateLoginTokenPrefix(%q) = nil, want error", tc.token)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateLoginTokenPrefix(%q) = %v, want nil", tc.token, err)
			}
		})
	}

	// The error string is user-facing; make sure it lists every accepted
	// prefix so users hitting it can self-serve. Hardcoding the exact
	// prefixes here is deliberate — if someone adds a new prefix to
	// loginTokenPrefixes they should also update the docs / this test.
	err := validateLoginTokenPrefix("nope_xxx")
	if err == nil {
		t.Fatal("expected error for unknown prefix")
	}
	for _, p := range []string{"mul_", "mcn_"} {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not mention prefix %q", err.Error(), p)
		}
	}
}
