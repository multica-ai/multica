package execenv

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/piagent"
)

func TestPreparePiHomePreservesStateAndDerivesModels(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(source, "packages"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "auth.json"), []byte(`{"token":"host"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "models.json"), []byte(`{
		"providers":{"existing":{"baseUrl":"https://existing.example","models":[{"id":"old"}]}},
		"extra":true
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	home, err := PreparePiHome(root, source, piagent.Config{
		Provider: "openai",
		API:      "openai-responses",
		BaseURL:  "https://api.example.com/v1",
		Model:    "gpt-5",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("PreparePiHome: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil {
		t.Fatalf("auth.json was not mirrored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "packages")); err != nil {
		t.Fatalf("packages was not mirrored: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || containsSecret(string(data), "host") {
		t.Fatalf("derived models unexpectedly contains auth state: %s", data)
	}
	var models map[string]any
	if err := json.Unmarshal(data, &models); err != nil {
		t.Fatal(err)
	}
	providers := models["providers"].(map[string]any)
	openai := providers["openai"].(map[string]any)
	if openai["apiKey"] != "$"+piagent.APIKeyEnv {
		t.Fatalf("apiKey must reference env, got %#v", openai["apiKey"])
	}
	if _, ok := providers["existing"]; !ok {
		t.Fatal("existing provider was not preserved")
	}
	if _, err := os.Stat(filepath.Join(source, "models.json")); err != nil {
		t.Fatalf("source config was modified: %v", err)
	}
	if mode := fileMode(t, filepath.Join(home, "models.json")); mode.Perm() != 0o600 {
		t.Fatalf("models.json permissions = %o, want 600", mode.Perm())
	}
}

func TestPreparePiHomeRejectsMalformedSourceModels(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "models.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := PreparePiHome(t.TempDir(), source, piagent.Config{
		Provider: "openai", API: "openai-responses", BaseURL: "https://example.com", Model: "x",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("expected malformed source models.json to fail closed")
	}
}

func containsSecret(value, secret string) bool {
	return len(secret) > 0 && len(value) >= len(secret) && stringContains(value, secret)
}

func stringContains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
