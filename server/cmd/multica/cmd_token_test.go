package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func tokenListTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func TestRunTokenListOnlyPrintsAllowlistedMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-auth-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	const sensitivePrefix = "mul_sensitive_prefix"
	const sensitiveHash = "sha256-sensitive-hash"
	const sensitiveToken = "mul_raw_secret_token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/tokens" {
			t.Fatalf("request = %s %s, want GET /api/tokens", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"12345678-aaaa-bbbb-cccc-dddddddddddd","name":"relay","created_at":"2026-01-02T03:04:05Z","last_used_at":null,"expires_at":"2026-02-01T03:04:05Z","is_external_relay":true,"token_prefix":"` + sensitivePrefix + `","token":"` + sensitiveToken + `","token_hash":"` + sensitiveHash + `"}]`))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	for _, output := range []string{"table", "json"} {
		t.Run(output, func(t *testing.T) {
			cmd := tokenListTestCommand()
			_ = cmd.Flags().Set("output", output)
			got, err := captureStdout(t, func() error { return runTokenList(cmd, nil) })
			if err != nil {
				t.Fatalf("runTokenList: %v", err)
			}
			for _, forbidden := range []string{sensitivePrefix, sensitiveHash, sensitiveToken, "is_external_relay"} {
				if strings.Contains(got, forbidden) {
					t.Errorf("output leaked forbidden field/value %q: %s", forbidden, got)
				}
			}
			if output == "json" {
				var rows []map[string]any
				if err := json.Unmarshal([]byte(got), &rows); err != nil {
					t.Fatalf("decode JSON output: %v (%q)", err, got)
				}
				if len(rows) != 1 || len(rows[0]) != 5 {
					t.Fatalf("JSON output fields = %#v, want exactly 5 allowlisted fields", rows)
				}
				if rows[0]["id"] != "12345678" || rows[0]["name"] != "relay" {
					t.Fatalf("JSON metadata = %#v", rows[0])
				}
			}
		})
	}
}
