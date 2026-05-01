package storage

import "context"

// Storage abstracts file upload/delete operations so the handler layer
// can work with either S3-compatible storage or local filesystem storage.
type Storage interface {
	// Upload stores data under the given key and returns the public URL.
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	// Delete removes the object identified by key. Errors are logged but not fatal.
	Delete(ctx context.Context, key string)
	// DeleteKeys removes multiple objects. Best-effort, errors are logged.
	DeleteKeys(ctx context.Context, keys []string)
	// KeyFromURL extracts the object key from its public URL.
	KeyFromURL(rawURL string) string
}
