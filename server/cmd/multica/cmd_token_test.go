package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func tokenCreateTestCommand(outputPath string) *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().Bool("external-relay", false, "")
	cmd.Flags().Int("expires-in-days", 0, "")
	cmd.Flags().String("output-file", outputPath, "")
	return cmd
}

func TestRunTokenCreateWritesSecretPrivatelyAndPrintsOnlyMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-auth-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	const secret = "mul_raw_secret_token"
	path := filepath.Join(t.TempDir(), "pat")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/tokens" {
			t.Fatalf("request = %s %s, want POST /api/tokens", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["name"] != "relay" || body["is_external_relay"] != true || body["expires_in_days"] != float64(30) {
			t.Fatalf("request body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "12345678-aaaa-bbbb-cccc-dddddddddddd", "name": "relay", "token": secret,
			"expires_at": "2026-12-07T03:04:05Z",
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	cmd := tokenCreateTestCommand(path)
	_ = cmd.Flags().Set("name", "relay")
	_ = cmd.Flags().Set("external-relay", "true")
	_ = cmd.Flags().Set("expires-in-days", "30")
	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error { return runTokenCreate(cmd, nil) })
	errout := stderr.read()
	if err != nil {
		t.Fatalf("runTokenCreate: %v", err)
	}
	if strings.Contains(out, secret) || strings.Contains(errout, secret) || !strings.Contains(out, "12345678") || !strings.Contains(out, "Name: relay") {
		t.Fatalf("unexpected output; stdout=%q stderr=%q", out, errout)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("token file = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestRunTokenCreateRefusesExistingOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := tokenCreateTestCommand(path)
	_ = cmd.Flags().Set("name", "relay")
	err := runTokenCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "create token output file") {
		t.Fatalf("runTokenCreate error = %v, want existing-file error", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "keep" {
		t.Fatalf("existing file changed: %q, %v", got, readErr)
	}
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
