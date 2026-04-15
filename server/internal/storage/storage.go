package storage

import (
	"context"
)

type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string
	CdnDomain() string
}
