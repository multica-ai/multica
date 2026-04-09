package storage

import (
	"context"
	"net/http"
)

// Storage provides a backend-agnostic interface for file uploads.
type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) error
	PublicURL(r *http.Request, key string) string
	KeyFromURL(rawURL string) string
	Delete(ctx context.Context, key string)
	DeleteKeys(ctx context.Context, keys []string)
}
