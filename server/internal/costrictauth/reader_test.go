package costrictauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCredentials_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", oldHome)

	cred, err := LoadCredentials()
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cred != nil {
		t.Fatalf("expected nil credentials for missing file, got %+v", cred)
	}
}

func TestLoadCredentials_Valid(t *testing.T) {
	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", oldHome)

	dir := filepath.Join(tmp, ".costrict", "share")
	os.MkdirAll(dir, 0o755)
	data := `{"access_token":"test_token_123","base_url":"https://example.com"}`
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte(data), 0o600)

	cred, err := LoadCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credentials, got nil")
	}
	if cred.AccessToken != "test_token_123" {
		t.Errorf("access_token = %q, want %q", cred.AccessToken, "test_token_123")
	}
	if cred.BaseURL != "https://example.com" {
		t.Errorf("base_url = %q, want %q", cred.BaseURL, "https://example.com")
	}
}

func TestLoadCredentials_EmptyToken(t *testing.T) {
	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", oldHome)

	dir := filepath.Join(tmp, ".costrict", "share")
	os.MkdirAll(dir, 0o755)
	data := `{"access_token":"","base_url":"https://example.com"}`
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte(data), 0o600)

	cred, err := LoadCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred != nil {
		t.Fatalf("expected nil credentials when token is empty, got %+v", cred)
	}
}
