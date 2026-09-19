package main

import (
	"bytes"
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func securityTestCmd(update bool) *cobra.Command {
	cmd := &cobra.Command{Use: "trigger"}
	addWebhookSecurityFlags(cmd, update)
	cmd.Flags().String("kind", "webhook", "")
	cmd.Flags().String("cron", "", "")
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().Bool("enabled", true, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestGitHubSecretInput(t *testing.T) {
	secret := "  exact-secret-bytes-012345  "
	file := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(file, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		input   string
		wantErr bool
	}{
		{"file", []string{"--provider=github", "--signing-secret-file=" + file}, "", false},
		{"stdin", []string{"--provider=github", "--signing-secret-stdin"}, secret, false},
		{"unknown", []string{"--provider=typo"}, "", true},
		{"missing", []string{"--provider=github"}, "", true},
		{"exclusive", []string{"--signing-secret-file=" + file, "--signing-secret-stdin"}, secret, true},
		{"clear-exclusive", []string{"--clear-signing-secret", "--signing-secret-stdin"}, secret, true},
		{"short", []string{"--signing-secret-stdin"}, "short", true},
		{"oversize", []string{"--signing-secret-stdin"}, strings.Repeat("x", 4097), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := securityTestCmd(true)
			cmd.SetIn(strings.NewReader(tc.input))
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"kind": "webhook"}
			err := webhookSecurityBody(cmd, body, true)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatal("secret leaked in error")
			}
			if !tc.wantErr && body["signing_secret"] != secret {
				t.Fatal("secret bytes changed")
			}
		})
	}
}

func TestGitHubCreateSendsAtomicConfigAndRedactsOutput(t *testing.T) {
	t.Chdir(t.TempDir()) // Test-owned API credentials must not inherit the daemon marker.
	const ap = "11111111-1111-1111-1111-111111111111"
	const secret = "test-secret-at-least-16-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/autopilots/"+ap+"/triggers" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["provider"] != "github" || body["enabled"] != false || body["signing_secret"] != secret {
			t.Error("wrong atomic configuration")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "trigger", "autopilot_id": ap, "provider": "github", "kind": "webhook", "enabled": false, "has_signing_secret": true, "webhook_token": "private-token", "webhook_url": "https://example.invalid/private-token", "signing_secret": secret})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	cmd := securityTestCmd(false)
	cmd.SetIn(bytes.NewBufferString(secret))
	if err := cmd.ParseFlags([]string{"--provider=github", "--enabled=false", "--signing-secret-stdin"}); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runAutopilotTriggerAdd(cmd, []string{ap}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, secret) || strings.Contains(out, "private-token") {
		t.Fatal("credential leaked in stdout")
	}
	if !strings.Contains(out, `"has_signing_secret": true`) {
		t.Fatal("missing safe readback")
	}
}

func TestGitHubTriggerCLIContract(t *testing.T) {
	for _, cmd := range []*cobra.Command{autopilotTriggerAddCmd, autopilotTriggerUpdateCmd} {
		for _, flag := range []string{"provider", "signing-secret-file", "signing-secret-stdin", "enabled"} {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("%s lacks --%s", cmd.Name(), flag)
			}
		}
	}
}

func TestGitHubSecretErrorsRedactEchoedBody(t *testing.T) {
	const secret = "sensitive-key-never-in-errors"
	for _, status := range []int{400, 403, 500} {
		err := webhookSecurityWriteError("configure trigger", &cli.HTTPError{StatusCode: status, Body: secret}, map[string]any{"signing_secret": secret})
		if strings.Contains(err.Error(), secret) || strings.Contains(cli.FormatError(err, true), secret) {
			t.Fatal("echoed secret leaked")
		}
	}
}
