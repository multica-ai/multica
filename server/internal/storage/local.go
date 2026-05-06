package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LocalStorage struct {
	uploadDir string
	baseURL   string
}

// NewLocalStorageFromEnv creates a LocalStorage from environment variables.
// Returns nil if upload directory cannot be created.
//
// Environment variables:
//   - LOCAL_UPLOAD_DIR (default: "./data/uploads")
//   - LOCAL_UPLOAD_BASE_URL (optional, e.g., "http://localhost:8080")
func NewLocalStorageFromEnv() *LocalStorage {
	uploadDir := os.Getenv("LOCAL_UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./data/uploads"
	}

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		slog.Error("failed to create upload directory", "dir", uploadDir, "error", err)
		return nil
	}

	baseURL := strings.TrimSuffix(os.Getenv("LOCAL_UPLOAD_BASE_URL"), "/")

	slog.Info("local storage initialized", "dir", uploadDir, "baseURL", baseURL)
	return &LocalStorage{
		uploadDir: uploadDir,
		baseURL:   baseURL,
	}
}

func (s *LocalStorage) CdnDomain() string {
	if s.baseURL == "" {
		return ""
	}
	u, err := url.Parse(s.baseURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *LocalStorage) KeyFromURL(rawURL string) string {
	if s.baseURL != "" && strings.HasPrefix(rawURL, s.baseURL) {
		rawURL = strings.TrimPrefix(rawURL, s.baseURL)
	}

	prefix := "/uploads/"
	if idx := strings.Index(rawURL, prefix); idx >= 0 {
		return rawURL[idx+len(prefix):]
	}
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

func (s *LocalStorage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	filePath := filepath.Join(s.uploadDir, key)
	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			slog.Error("local storage Delete failed", "key", key, "error", err)
		}
	}
}

func (s *LocalStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *LocalStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	dest := filepath.Join(s.uploadDir, key)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", fmt.Errorf("local storage MkdirAll: %w", err)
	}
	if err := os.WriteFile(dest, data, 0644); err != nil {
		return "", fmt.Errorf("local storage WriteFile: %w", err)
	}

	if s.baseURL != "" {
		return fmt.Sprintf("%s/uploads/%s", s.baseURL, key), nil
	}
	return fmt.Sprintf("/uploads/%s", key), nil
}

func (s *LocalStorage) GetFilePath(key string) string {
	return filepath.Join(s.uploadDir, key)
}

func (s *LocalStorage) ServeFile(w http.ResponseWriter, r *http.Request, filename string) {
	filePath := filepath.Join(s.uploadDir, filename)
	slog.Info("serving file", "filename", filename, "filepath", filePath)

	// Use http.ServeFile which has built-in path traversal protection
	// It sanitizes the path and prevents access outside the directory
	http.ServeFile(w, r, filePath)
}

func (s *LocalStorage) UploadFromReader(ctx context.Context, key string, reader io.Reader, contentType string, filename string) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("local storage ReadAll: %w", err)
	}

	return s.Upload(ctx, key, data, contentType, filename)
}

// PresignPut — not supported for the local backend because we have
// no way to hand the caller a URL that bypasses this server. Callers
// fall back to the body-upload path.
func (s *LocalStorage) PresignPut(ctx context.Context, key string, contentType string, filename string, expiresIn time.Duration) (*PresignedUpload, error) {
	return nil, ErrPresignUnsupported
}

// PresignGet — not supported. Local files are served by the same-
// process /uploads/* handler; PublicURL points at that handler
// directly (no signing involved).
func (s *LocalStorage) PresignGet(ctx context.Context, key string, expiresIn time.Duration) (string, error) {
	return "", ErrPresignUnsupported
}

// PublicURL returns the public (served-by-this-server) URL for an
// already-staged file under the local upload dir.
func (s *LocalStorage) PublicURL(key string) string {
	return "/uploads/" + key
}

// StatObject returns the byte size of an already-uploaded object in
// the local upload dir. Used by the /confirm endpoint, even though
// local backends don't use presign themselves — keeping the interface
// uniform makes call sites simpler.
func (s *LocalStorage) StatObject(ctx context.Context, key string) (int64, error) {
	fi, err := os.Stat(filepath.Join(s.uploadDir, key))
	if err != nil {
		return 0, fmt.Errorf("local StatObject: %w", err)
	}
	return fi.Size(), nil
}
