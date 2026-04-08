package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorageUploadAndDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	storage := &LocalStorage{dir: dir}
	key := "screenshots/test.png"
	data := []byte("hello")

	if err := storage.Upload(context.Background(), key, data, "image/png", "test.png"); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	filePath := filepath.Join(dir, "screenshots", "test.png")
	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("stored data = %q, want %q", string(got), string(data))
	}

	storage.Delete(context.Background(), key)
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, stat err = %v", err)
	}
}

func TestLocalStoragePublicURLUsesForwardedHeaders(t *testing.T) {
	t.Parallel()

	storage := &LocalStorage{dir: t.TempDir()}
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/upload-file", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "multica.example.com")

	got := storage.PublicURL(req, "images/demo.png")
	want := "https://multica.example.com/files/images/demo.png"
	if got != want {
		t.Fatalf("PublicURL() = %q, want %q", got, want)
	}
}

func TestLocalStorageRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	storage := &LocalStorage{dir: t.TempDir()}
	if _, err := storage.pathForKey("../secrets.txt"); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestLocalStorageKeyFromURL(t *testing.T) {
	t.Parallel()

	storage := &LocalStorage{dir: t.TempDir()}
	got := storage.KeyFromURL("https://multica.example.com/files/images/demo.png")
	if got != "images/demo.png" {
		t.Fatalf("KeyFromURL() = %q, want %q", got, "images/demo.png")
	}
}

func TestLocalStorageFileHandlerServesFilesWithoutDirectoryListing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	storage := &LocalStorage{dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "demo.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	handler := storage.FileHandler()

	fileReq := httptest.NewRequest(http.MethodGet, "/demo.txt", nil)
	fileRes := httptest.NewRecorder()
	handler.ServeHTTP(fileRes, fileReq)
	if fileRes.Code != http.StatusOK {
		t.Fatalf("file response status = %d, want %d", fileRes.Code, http.StatusOK)
	}
	if fileRes.Body.String() != "hello" {
		t.Fatalf("file response body = %q, want %q", fileRes.Body.String(), "hello")
	}

	dirReq := httptest.NewRequest(http.MethodGet, "/", nil)
	dirRes := httptest.NewRecorder()
	handler.ServeHTTP(dirRes, dirReq)
	if dirRes.Code != http.StatusNotFound {
		t.Fatalf("directory response status = %d, want %d", dirRes.Code, http.StatusNotFound)
	}
}
