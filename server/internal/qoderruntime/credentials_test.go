package qoderruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryCredentialRequestOnly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{cfg: Config{GitHubTokenFiles: map[string]string{"https://github.com/Owner/Repo.git": file}}}
	r := &run{Resources: []map[string]any{{"type": "github_repository", "url": "https://github.com/owner/repo", "checkout": map[string]string{"type": "branch", "name": "main"}}}}
	request, err := b.sessionResources(r.Resources)
	if err != nil {
		t.Fatal(err)
	}
	if request[0]["authorization_token"] != "test-secret" {
		t.Fatal("missing request credential")
	}
	state := filepath.Join(dir, "state.json")
	if err := save(state, &journal{Run: r}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "test-secret") || strings.Contains(string(data), "authorization_token") {
		t.Fatal("credential persisted")
	}
	if err := os.WriteFile(file, []byte("rotated-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	request, err = b.sessionResources(r.Resources)
	if err != nil || request[0]["authorization_token"] != "rotated-secret" {
		t.Fatal("credential rotation not picked up")
	}
	r.Resources[0]["url"] = "https://github.com/owner/another"
	if _, err := b.sessionResources(r.Resources); err == nil {
		t.Fatal("credential crossed repository boundary")
	}
}

func TestRepositoryCredentialValidation(t *testing.T) {
	for _, raw := range []string{"https://github.com/owner", "https://github.com:443/a/b", "https://github.com/a/b?token=x", "https://user@github.com/a/b", "https://github.com/a/b/extra", "https://github.com/a/%62", "https://github.com/a/.."} {
		if _, err := repositoryKey(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	file := filepath.Join(t.TempDir(), "token")
	for _, secret := range []string{"", "first\nsecond", strings.Repeat("x", 16385)} {
		if err := os.WriteFile(file, []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRepositoryToken(file); err == nil {
			t.Fatal("accepted invalid credential")
		}
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRepositoryToken(file); err == nil {
		t.Fatal("accepted public file")
	}
}
