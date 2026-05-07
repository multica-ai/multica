package main

import (
	"net"
	"testing"

	"github.com/spf13/cobra"
)

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
