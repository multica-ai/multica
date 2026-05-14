package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// TestLocalStorage_ServeFile_ContentDispositionFromSidecar verifies the fix
// for issue #2442: downloads served from /uploads/* must carry the original
// uploaded filename in Content-Disposition, mirroring the S3 Upload path's
// existing ContentDisposition behavior. Without this, browsers fall back to
// the storage-key basename (UUID + ext) for the download filename.
func TestLocalStorage_ServeFile_ContentDispositionFromSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	cases := []struct {
		name        string
		key         string
		contentType string
		filename    string
		wantHeader  string
	}{
		{
			name:        "attachment for non-inline type",
			key:         "workspaces/ws-1/abc.xlsx",
			contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			filename:    "Bwave JE_V1.xlsx",
			wantHeader:  `attachment; filename="Bwave JE_V1.xlsx"`,
		},
		{
			name:        "inline for image",
			key:         "workspaces/ws-1/def.png",
			contentType: "image/png",
			filename:    "screenshot 2026-05-11.png",
			wantHeader:  `inline; filename="screenshot 2026-05-11.png"`,
		},
		{
			name:        "filename with header-injection characters is sanitized",
			key:         "workspaces/ws-1/ghi.txt",
			contentType: "text/plain",
			filename:    "weird\";name.txt",
			wantHeader:  `attachment; filename="weird__name.txt"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if _, err := store.Upload(ctx, tc.key, []byte("body"), tc.contentType, tc.filename); err != nil {
				t.Fatalf("Upload failed: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/uploads/"+tc.key, nil)
			rec := httptest.NewRecorder()
			store.ServeFile(rec, req, tc.key)

			got := rec.Header().Get("Content-Disposition")
			if got != tc.wantHeader {
				t.Errorf("Content-Disposition = %q, want %q", got, tc.wantHeader)
			}
		})
	}
}

// TestLocalStorage_ServeFile_NoSidecarFallback documents the graceful
// degradation path: files uploaded before the sidecar landed (or written
// out-of-band) have no .meta.json on disk and ServeFile must not set
// Content-Disposition — leaving the existing pre-fix behavior intact.
func TestLocalStorage_ServeFile_NoSidecarFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	key := "legacy-no-sidecar.txt"
	if err := os.WriteFile(filepath.Join(tmpDir, key), []byte("body"), 0644); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil)
	rec := httptest.NewRecorder()
	store.ServeFile(rec, req, key)

	if got := rec.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("Content-Disposition = %q, want empty (no sidecar fallback)", got)
	}
}

// TestLocalStorage_ServeFile_RejectsSidecarSuffix verifies that the sidecar
// JSON itself is not addressable via /uploads/*. The sidecar is an
// implementation detail; exposing it would turn the filename + content-type
// pair into a stable read API and make any future ACL change leakier than
// the data file it sits next to.
func TestLocalStorage_ServeFile_RejectsSidecarSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	ctx := context.Background()
	if _, err := store.Upload(ctx, "abc.xlsx", []byte("body"), "text/plain", "real.xlsx"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	sidecarKey := "abc.xlsx" + metaSuffix
	req := httptest.NewRequest(http.MethodGet, "/uploads/"+sidecarKey, nil)
	rec := httptest.NewRecorder()
	store.ServeFile(rec, req, sidecarKey)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("Content-Disposition = %q, want empty", got)
	}
}

// TestLocalStorage_ServeFile_RejectsPathTraversal documents that a key
// pointing outside uploadDir is rejected before any sidecar read. Without
// this guard, readLocalMeta would attempt a disk read at <some-path>.meta.json
// before http.ServeFile's own ".." check fires on r.URL.Path.
func TestLocalStorage_ServeFile_RejectsPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)

	store := NewLocalStorageFromEnv()
	if store == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	// Seed a sidecar OUTSIDE uploadDir so we'd notice if it were read: the
	// header would carry "leaked.xlsx". Locating the sibling inside the
	// per-test TempDir keeps the test self-contained — no real /etc reads.
	parentDir := filepath.Dir(tmpDir)
	leakedBase := filepath.Join(parentDir, "leaked-target")
	if err := os.WriteFile(leakedBase+metaSuffix, []byte(`{"filename":"leaked.xlsx","content_type":"text/plain"}`), 0644); err != nil {
		t.Fatalf("seed leaked sidecar failed: %v", err)
	}
	t.Cleanup(func() {
		os.Remove(leakedBase + metaSuffix)
	})

	traversal := "../" + filepath.Base(leakedBase)
	req := httptest.NewRequest(http.MethodGet, "/uploads/"+traversal, nil)
	rec := httptest.NewRecorder()
	store.ServeFile(rec, req, traversal)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("Content-Disposition = %q, want empty (sidecar must not leak)", got)
	}
}

// TestLocalStorage_Upload_SkipsSidecarWhenFilenameEmpty verifies the tighter
// Upload gate: a write with no filename has nothing useful to preserve, so
// we shouldn't litter the upload directory with content-type-only sidecars
// that ServeFile would ignore anyway.
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
