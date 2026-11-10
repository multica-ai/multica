package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorage_Upload(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	os.Unsetenv("LOCAL_UPLOAD_BASE_URL")
	// No LOCAL_UPLOAD_BASE_URL set - should return relative path

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	data := []byte("hello world")
	contentType := "text/plain"
	filename := "test.txt"

	link, err := store.Upload(ctx, "test-key.txt", data, contentType, filename)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	expectedLink := "/uploads/test-key.txt"
	if link != expectedLink {
		t.Errorf("link = %q, want %q", link, expectedLink)
	}

	filePath := filepath.Join(tmpDir, "test-key.txt")
	stored, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read uploaded file: %v", err)
	}
	if string(stored) != string(data) {
		t.Errorf("stored data = %q, want %q", stored, data)
	}
}

func TestLocalStorage_Upload_WithBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "http://localhost:8080")

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	data := []byte("hello world")
	contentType := "text/plain"
	filename := "test.txt"

	link, err := store.Upload(ctx, "test-key.txt", data, contentType, filename)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// When LOCAL_UPLOAD_BASE_URL is set, should return full URL
	expectedLink := "http://localhost:8080/uploads/test-key.txt"
	if link != expectedLink {
		t.Errorf("link = %q, want %q", link, expectedLink)
	}

	filePath := filepath.Join(tmpDir, "test-key.txt")
	stored, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read uploaded file: %v", err)
	}
	if string(stored) != string(data) {
		t.Errorf("stored data = %q, want %q", stored, data)
	}
}

func TestLocalStorage_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	data := []byte("hello world")

	_, err := store.Upload(ctx, "delete-me.txt", data, "text/plain", "delete-me.txt")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	filePath := filepath.Join(tmpDir, "delete-me.txt")
	if _, err := os.ReadFile(filePath); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	store.Delete(ctx, "delete-me.txt")

	if _, err := os.ReadFile(filePath); !os.IsNotExist(err) {
		t.Errorf("file should be deleted")
	}
}

func TestLocalStorage_KeyFromURL(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	// No baseURL set

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	tests := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{"local URL format", "/uploads/abc123.png", "abc123.png"},
		{"local URL with subdir", "/uploads/2024/01/image.jpg", "2024/01/image.jpg"},
		{"local URL with workspace prefix", "/uploads/workspaces/ws-123/abc.png", "workspaces/ws-123/abc.png"},
		{"just filename", "abc123.png", "abc123.png"},
		{"full path", "/some/path/to/file.pdf", "file.pdf"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := store.KeyFromURL(tc.rawURL)
			if got != tc.expected {
				t.Errorf("KeyFromURL(%q) = %q, want %q", tc.rawURL, got, tc.expected)
			}
		})
	}
}

func TestLocalStorage_KeyFromURL_WithBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "http://localhost:8080")

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	tests := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{"full URL format", "http://localhost:8080/uploads/abc123.png", "abc123.png"},
		{"full URL with subdir", "http://localhost:8080/uploads/2024/01/image.jpg", "2024/01/image.jpg"},
		{"local URL format still works", "/uploads/abc123.png", "abc123.png"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := store.KeyFromURL(tc.rawURL)
			if got != tc.expected {
				t.Errorf("KeyFromURL(%q) = %q, want %q", tc.rawURL, got, tc.expected)
			}
		})
	}
}

func TestLocalStorage_DeleteKeys(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	data := []byte("hello world")

	keys := []string{"file1.txt", "file2.txt", "file3.txt"}
	for _, key := range keys {
		_, err := store.Upload(ctx, key, data, "text/plain", key)
		if err != nil {
			t.Fatalf("Upload %s failed: %v", key, err)
		}
	}

	store.DeleteKeys(ctx, keys)

	for _, key := range keys {
		filePath := filepath.Join(tmpDir, key)
		if _, err := os.ReadFile(filePath); !os.IsNotExist(err) {
			t.Errorf("file %s should be deleted", key)
		}
	}
}

func TestLocalStorage_KeyFromURL_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	if got := store.KeyFromURL(""); got != "" {
		t.Errorf("KeyFromURL(\"\") = %q, want empty string", got)
	}
}

// TestLocalStorage_Upload_SkipsSidecarWhenFilenameEmpty verifies that uploads
// without a filename do not create content-type-only sidecars.
func TestLocalStorage_Upload_SkipsSidecarWhenFilenameEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	key := "no-filename.bin"
	if _, err := store.Upload(ctx, key, []byte("body"), "application/octet-stream", ""); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, key+metaSuffix)); !os.IsNotExist(err) {
		t.Errorf("sidecar should not exist when filename is empty, got err=%v", err)
	}
}

// TestLocalStorage_Delete_RemovesSidecar verifies the cleanup half of the
// fix: when the upload is deleted, its sidecar metadata file disappears too.
// Otherwise the upload directory grows orphan .meta.json files forever.
func TestLocalStorage_Delete_RemovesSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	key := "deleteme.txt"
	if _, err := store.Upload(ctx, key, []byte("body"), "text/plain", "original.txt"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	sidecar := filepath.Join(tmpDir, key+metaSuffix)
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar should exist after Upload: %v", err)
	}

	store.Delete(ctx, key)

	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("sidecar should be removed after Delete, got err=%v", err)
	}
}

// GetReader returns the uploaded bytes verbatim — used by the preview proxy.
func TestLocalStorage_GetReader_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	key := "preview.md"
	body := []byte("# hello\nworld\n")
	if _, err := store.Upload(ctx, key, body, "text/markdown", "preview.md"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	rc, err := store.GetReader(ctx, key)
	if err != nil {
		t.Fatalf("GetReader: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

// Refuses path traversal at storage layer so callers don't need to defend it.
func TestLocalStorage_GetReader_RejectsTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	if rc, err := store.GetReader(context.Background(), "../../../etc/passwd"); err == nil {
		rc.Close()
		t.Fatal("GetReader should refuse traversal keys")
	}
}

// The sidecar JSON is an internal detail. Allowing /content to read it via a
// crafted key would expose the original filename + content-type stored next
// to every upload.
func TestLocalStorage_GetReader_RejectsSidecarSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	if rc, err := store.GetReader(context.Background(), "some-key.txt"+metaSuffix); err == nil {
		rc.Close()
		t.Fatal("GetReader should refuse sidecar keys")
	}
}

// Missing key surfaces as a plain error — the handler maps it to 404.
func TestLocalStorage_GetReader_MissingKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	if rc, err := store.GetReader(context.Background(), "nonexistent.txt"); err == nil {
		rc.Close()
		t.Fatal("GetReader should error on missing key")
	}
}
