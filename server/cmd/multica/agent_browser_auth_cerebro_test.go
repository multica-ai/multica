package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
	"github.com/multica-ai/multica/server/internal/cli"
)

type recordingAgentBrowserAuthSaver struct {
	profile  string
	loginURL string
	username string
	password string
}

func (r *recordingAgentBrowserAuthSaver) Save(_ context.Context, profile, loginURL, username, password string) error {
	r.profile = profile
	r.loginURL = loginURL
	r.username = username
	r.password = password
	return nil
}

func TestProvisionAgentBrowserAuth_SecretOnlyReachesSaver(t *testing.T) {
	const secret = "vault-password-must-never-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/cerebro/agent-browser/provision-auth" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		var body agentBrowserProvisionRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Vault != "Shared/browser-login/registry" || body.UsernameKey != "EMAIL" || body.PasswordKey != "PASSWORD" {
			t.Fatalf("unexpected request: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(agentBrowserProvisionWireResponse{
			Username: "qa@example.com",
			Password: secret,
			Audit:    agentBrowserProvisionAudit{Vault: body.Vault, UsernameKey: body.UsernameKey, PasswordKey: body.PasswordKey},
		})
	}))
	defer server.Close()

	client := cli.NewAPIClient(server.URL, "workspace-id", "task-token")
	saver := &recordingAgentBrowserAuthSaver{}
	var out bytes.Buffer
	err := provisionAgentBrowserAuth(context.Background(), client, saver, &out, agentBrowserProvisionOptions{
		Profile: "registry-qa", LoginURL: "https://registry.firtal.com/auth/login",
		Vault: "Shared/browser-login/registry", UsernameKey: "EMAIL", PasswordKey: "PASSWORD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saver.password != secret || saver.username != "qa@example.com" {
		t.Fatal("credential pair did not reach the auth saver")
	}
	if bytes.Contains(out.Bytes(), []byte(secret)) || bytes.Contains(out.Bytes(), []byte("qa@example.com")) {
		t.Fatalf("credential leaked to output: %s", out.String())
	}
	if saver.profile != "registry-qa" || saver.loginURL != "https://registry.firtal.com/auth/login" {
		t.Fatalf("wrong profile metadata: %#v", saver)
	}
}

func TestAgentBrowserAuthSaveArgsExcludePassword(t *testing.T) {
	args := agentBrowserAuthSaveArgs("registry", "https://registry.firtal.com/login", "agent-testing@firtal.com")
	want := []string{"auth", "save", "registry", "--url", "https://registry.firtal.com/login", "--username", "agent-testing@firtal.com", "--password-stdin"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("argv = %#v, want %#v", args, want)
	}
}

func TestVerifyInternalAgentBrowserPrintsOnlySanitizedResult(t *testing.T) {
	screenshot := []byte("\x89PNG\r\n\x1a\nverified-registry-dashboard")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/cerebro/agent-browser/internal-verify" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		var body internalBrowserVerifyRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.App != "registry" {
			t.Fatalf("app = %q", body.App)
		}
		if !body.Async {
			t.Fatal("new CLI must opt into asynchronous verification")
		}
		_ = json.NewEncoder(w).Encode(internalBrowserVerifyResponse{
			App: "registry", InternalHost: "firtal-data-registry-private.internal:3000",
			FinalURL: "http://firtal-data-registry-private.internal:3000/", Markers: []string{"Dashboard"},
			Errors: []string{}, ScreenshotPNG: screenshot, VersionCommit: "abcdef0123456789",
		})
	}))
	defer server.Close()

	client := cli.NewAPIClient(server.URL, "workspace-id", "task-token")
	var out bytes.Buffer
	screenshotPath := filepath.Join(t.TempDir(), "registry.png")
	if err := verifyInternalAgentBrowser(context.Background(), client, &out, "registry", screenshotPath); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"app":"registry"`)) {
		t.Fatalf("missing sanitized result: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"version_commit":"abcdef0123456789"`)) {
		t.Fatalf("missing safe version commit: %s", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("password")) || bytes.Contains(out.Bytes(), []byte("username")) {
		t.Fatalf("credential field leaked to output: %s", out.String())
	}
	if bytes.Contains(out.Bytes(), screenshot) || bytes.Contains(out.Bytes(), []byte("ScreenshotPNG")) {
		t.Fatalf("screenshot bytes leaked to output: %s", out.String())
	}
	written, err := os.ReadFile(screenshotPath)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	if !bytes.Equal(written, screenshot) {
		t.Fatalf("screenshot bytes = %q, want %q", written, screenshot)
	}
	info, err := os.Stat(screenshotPath)
	if err != nil {
		t.Fatalf("stat screenshot: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("screenshot mode = %o, want 600", info.Mode().Perm())
	}
	if !bytes.Contains(out.Bytes(), []byte(screenshotPath)) {
		t.Fatalf("output does not name screenshot path: %s", out.String())
	}
}

func TestVerifyInternalAgentBrowserPollsAsyncJob(t *testing.T) {
	screenshot := []byte("\x89PNG\r\n\x1a\nasync-result")
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(internalBrowserVerifyResponse{JobID: "job-123", State: "pending"})
		case req.Method == http.MethodGet && req.URL.Path == "/api/cerebro/agent-browser/internal-verify/job-123":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(internalBrowserVerifyResponse{State: "pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(internalBrowserVerifyResponse{App: "registry", ScreenshotPNG: screenshot})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	client := cli.NewAPIClient(server.URL, "workspace-id", "task-token")
	path := filepath.Join(t.TempDir(), "registry.png")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := verifyInternalAgentBrowser(ctx, client, io.Discard, "registry", path); err != nil {
		t.Fatal(err)
	}
	if polls != 2 {
		t.Fatalf("polls = %d, want 2", polls)
	}
	if written, err := os.ReadFile(path); err != nil || !bytes.Equal(written, screenshot) {
		t.Fatalf("written screenshot = %q, err = %v", written, err)
	}
}

func TestVerifyInternalAgentBrowserRejectsInvalidScreenshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(internalBrowserVerifyResponse{ScreenshotPNG: []byte("not-a-png")})
	}))
	defer server.Close()

	client := cli.NewAPIClient(server.URL, "workspace-id", "task-token")
	screenshotPath := filepath.Join(t.TempDir(), "registry.png")
	err := verifyInternalAgentBrowser(context.Background(), client, io.Discard, "registry", screenshotPath)
	if err == nil || err.Error() != "internal browser verification returned an invalid screenshot" {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(screenshotPath); !os.IsNotExist(err) {
		t.Fatalf("invalid screenshot was written: %v", err)
	}
}

func TestConfigureInternalBrowserVerifyClientAllowsBrowserStartup(t *testing.T) {
	client := cli.NewAPIClient("https://example.invalid", "workspace-id", "task-token")
	configureInternalBrowserVerifyClient(client)
	if client.HTTPClient.Timeout != internalBrowserVerifyTimeout {
		t.Fatalf("timeout = %s, want %s", client.HTTPClient.Timeout, internalBrowserVerifyTimeout)
	}
}

// The CLI deadline must outlast the verifier's own worst case. When it did not,
// the cold-start retry was unreachable: the CLI hung up 40s into the second open
// attempt and reported a transport deadline instead of the app's real verdict.
func TestInternalBrowserVerifyTimeoutOutlastsVerifier(t *testing.T) {
	if internalBrowserVerifyTimeout <= internalbrowserqa.MaxVerificationDuration {
		t.Fatalf("CLI timeout %s must exceed verifier worst case %s",
			internalBrowserVerifyTimeout, internalbrowserqa.MaxVerificationDuration)
	}
}
