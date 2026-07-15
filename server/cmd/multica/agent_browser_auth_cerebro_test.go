package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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
