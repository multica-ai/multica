package storage

import (
	"context"
	"time"
)

type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string
	CdnDomain() string
}

// Presigner is implemented by storage backends that can generate short-lived
// signed download URLs. Used as a fallback when CloudFront is not configured.
type Presigner interface {
	PresignDownloadURL(ctx context.Context, rawURL string, expiry time.Duration) (string, error)
}
