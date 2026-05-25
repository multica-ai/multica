package daemon

// CEREBRO-PATCH(agent-infisical-secrets): tests for spawn-time folder secret env injection.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrepareInfisicalSpawnInjectsFolderSecretsAndSkipsBlockedKeys(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if got := r.URL.Query().Get("workspaceId"); got != "project-1" {
			t.Fatalf("workspaceId = %q", got)
		}
		if got := r.URL.Query().Get("environment"); got != "production" {
			t.Fatalf("environment = %q", got)
		}
		if got := r.URL.Query().Get("secretPath"); got != "/app" {
			t.Fatalf("secretPath = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"secrets": []map[string]string{
				{"secretKey": "DATABASE_URL", "secretValue": "postgres://secret-value"},
				{"secretKey": "API_KEY", "secretValue": "abc123"},
				{"secretKey": "MULTICA_INTERNAL", "secretValue": "should-not-inject"},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("INFISICAL_SITE_URL", srv.URL)
	t.Setenv("INFISICAL_TOKEN", "test-token")
	t.Setenv("INFISICAL_PROJECT_ID", "project-1")

	d := &Daemon{}
	got, err := d.prepareInfisicalSpawn(context.Background(), &AgentData{
		InfisicalFolders: []InfisicalFolderRef{
			{Environment: "production", SecretPath: "/app"},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepareInfisicalSpawn returned error: %v", err)
	}
	if got["DATABASE_URL"] != "postgres://secret-value" {
		t.Fatalf("DATABASE_URL = %q", got["DATABASE_URL"])
	}
	if got["API_KEY"] != "abc123" {
		t.Fatalf("API_KEY = %q", got["API_KEY"])
	}
	if _, ok := got["MULTICA_INTERNAL"]; ok {
		t.Fatalf("blocked MULTICA_INTERNAL key was injected")
	}
	if len(requested) != 1 || requested[0] != "/api/v3/secrets/raw" {
		t.Fatalf("requested paths = %#v", requested)
	}
}
