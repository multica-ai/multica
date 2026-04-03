package storage

// MULTICA-LOCAL: S3 storage replaced with local filesystem storage.
// This file defines the Storage interface and the local implementation.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Storage is the interface for file storage operations.
type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string
	FilePath(key string) string
}

// LocalStorage stores files on the local filesystem.
type LocalStorage struct {
	baseDir string // e.g. ~/.multica-local/storage
	baseURL string // e.g. /api/files
}

// NewLocalStorage creates a local filesystem storage adapter.
func NewLocalStorage() *LocalStorage {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Error("unable to get home directory for storage", "error", err)
		homeDir = "."
	}

	baseDir := filepath.Join(homeDir, ".multica-local", "storage")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		slog.Error("unable to create storage directory", "error", err)
	}

	slog.Info("local storage initialized", "path", baseDir)
	return &LocalStorage{
		baseDir: baseDir,
		baseURL: "/api/files",
	}
}

func (s *LocalStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	filePath := filepath.Join(s.baseDir, key)
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create storage dir: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	link := fmt.Sprintf("%s/%s", s.baseURL, key)
	return link, nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	filePath := filepath.Join(s.baseDir, key)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		slog.Error("failed to delete file", "key", key, "error", err)
	}
}

func (s *LocalStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *LocalStorage) KeyFromURL(rawURL string) string {
	prefix := s.baseURL + "/"
	if strings.HasPrefix(rawURL, prefix) {
		return strings.TrimPrefix(rawURL, prefix)
	}
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

func (s *LocalStorage) FilePath(key string) string {
	return filepath.Join(s.baseDir, key)
}
