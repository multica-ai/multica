package storage

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// PrivateStorage is the local object store for data that must never be
// addressable through the public /uploads handler. It intentionally does not
// expose an ObjectURL: callers must stream through an authenticated endpoint.
// The on-disk implementation is shared with LocalStorage so atomic writes,
// traversal checks, and sidecar cleanup keep one security boundary.
type PrivateStorage struct {
	backend Storage
}

// NewPrivateStorageFromEnv creates the dedicated knowledge object store. It
// supports the same local/S3 backends as attachments, but deliberately hides
// ObjectURL and CdnDomain so knowledge objects are only reachable through an
// authenticated application endpoint.
func NewPrivateStorageFromEnv() *PrivateStorage {
	if os.Getenv("KNOWLEDGE_STORAGE_BACKEND") == "s3" {
		if backend := newS3StorageFromEnv("KNOWLEDGE_"); backend != nil {
			return &PrivateStorage{backend: backend}
		}
		slog.Warn("knowledge S3 storage requested but S3 is not configured")
		return nil
	}
	dir := os.Getenv("KNOWLEDGE_LOCAL_STORAGE_DIR")
	if dir == "" {
		dir = os.Getenv("KNOWLEDGE_STORAGE_DIR")
	}
	if dir == "" {
		dir = "./data/knowledge"
	}
	return NewPrivateLocalStorageAt(dir)
}

// NewPrivateLocalStorageFromEnv is retained for callers that explicitly want
// a local store; the server uses NewPrivateStorageFromEnv to honor the
// documented backend switch.
func NewPrivateLocalStorageFromEnv() *PrivateStorage {
	dir := os.Getenv("KNOWLEDGE_LOCAL_STORAGE_DIR")
	if dir == "" {
		dir = os.Getenv("KNOWLEDGE_STORAGE_DIR")
	}
	if dir == "" {
		dir = "./data/knowledge"
	}
	return NewPrivateLocalStorageAt(dir)
}

func NewPrivateLocalStorageAt(dir string) *PrivateStorage {
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("failed to create private knowledge storage directory", "dir", dir, "error", err)
		return nil
	}
	return &PrivateStorage{backend: &LocalStorage{uploadDir: filepath.Clean(dir)}}
}

func (s *PrivateStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	if s == nil || s.backend == nil {
		return "", os.ErrInvalid
	}
	if _, err := s.backend.Upload(ctx, key, data, contentType, filename); err != nil {
		return "", err
	}
	return key, nil
}

func (s *PrivateStorage) Delete(ctx context.Context, key string) {
	if s != nil && s.backend != nil {
		s.backend.Delete(ctx, key)
	}
}

func (s *PrivateStorage) DeleteObject(ctx context.Context, key string) error {
	if s == nil || s.backend == nil {
		return nil
	}
	return s.backend.DeleteObject(ctx, key)
}

func (s *PrivateStorage) DeleteKeys(ctx context.Context, keys []string) {
	if s != nil && s.backend != nil {
		s.backend.DeleteKeys(ctx, keys)
	}
}

func (s *PrivateStorage) KeyFromURL(rawURL string) string {
	if s == nil || s.backend == nil {
		return rawURL
	}
	return s.backend.KeyFromURL(rawURL)
}

func (s *PrivateStorage) ObjectURL(string) string { return "" }

func (s *PrivateStorage) CdnDomain() string { return "" }

func (s *PrivateStorage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	if s == nil || s.backend == nil {
		return nil, os.ErrInvalid
	}
	return s.backend.GetReader(ctx, key)
}

var _ Storage = (*PrivateStorage)(nil)
