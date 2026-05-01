package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage stores uploaded files on the local filesystem.
// It is used as a development fallback when S3 is not configured.
// Upload returns a relative path (/uploads/{key}); the handler layer resolves
// it to a full URL using the incoming request's host.
type LocalStorage struct {
	dir string // filesystem directory for stored files
}

// NewLocalStorage creates a LocalStorage instance.
// Reads UPLOAD_DIR (default: "uploads") for the filesystem path.
func NewLocalStorage() *LocalStorage {
	dir := os.Getenv("UPLOAD_DIR")
	if dir == "" {
		dir = "uploads"
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("local storage: failed to create upload dir", "dir", dir, "error", err)
	}

	slog.Info("local storage initialized (development mode)", "dir", dir)
	return &LocalStorage{dir: dir}
}

// Dir returns the filesystem directory where files are stored.
// Used by the router to mount a static file server.
func (l *LocalStorage) Dir() string { return l.dir }

// Upload writes data to disk and returns a relative path (/uploads/{key}).
// The caller (handler) is responsible for resolving this to an absolute URL
// using the incoming request's host.
func (l *LocalStorage) Upload(_ context.Context, key string, data []byte, _ string, _ string) (string, error) {
	dst := filepath.Join(l.dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return "", fmt.Errorf("local storage mkdir: %w", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return "", fmt.Errorf("local storage write: %w", err)
	}
	return "/uploads/" + key, nil
}

// Delete removes a file from disk. Missing files are silently ignored.
func (l *LocalStorage) Delete(_ context.Context, key string) {
	if key == "" {
		return
	}
	dst := filepath.Join(l.dir, filepath.FromSlash(key))
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		slog.Error("local storage delete failed", "key", key, "error", err)
	}
}

// DeleteKeys removes multiple files. Best-effort.
func (l *LocalStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, k := range keys {
		l.Delete(ctx, k)
	}
}

// KeyFromURL extracts the object key from a local upload URL or relative path.
// e.g. "/uploads/abc123.png" → "abc123.png"
// e.g. "http://example.com/uploads/abc123.png" → "abc123.png"
func (l *LocalStorage) KeyFromURL(rawURL string) string {
	const relPrefix = "/uploads/"
	if strings.HasPrefix(rawURL, relPrefix) {
		return strings.TrimPrefix(rawURL, relPrefix)
	}
	if i := strings.LastIndex(rawURL, "/uploads/"); i >= 0 {
		return rawURL[i+len("/uploads/"):]
	}
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}
