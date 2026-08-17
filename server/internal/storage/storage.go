package storage

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)
	// DeleteObject is Delete with the error surfaced — the channel-media
	// reconciler schedules retries on failure instead of assuming success.
	DeleteObject(ctx context.Context, key string) error
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string
	// ObjectURL is the URL a successful Upload of key would return — a pure
	// function of configuration, so the media intent ledger can persist it
	// BEFORE the upload.
	ObjectURL(key string) string
	CdnDomain() string
	// GetReader streams an object back to the caller. Used by the attachment
	// preview proxy (GET /api/attachments/{id}/content) to bypass CloudFront
	// CORS and the inline/attachment Content-Disposition decision. Caller
	// must Close the returned reader.
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

type Presigner interface {
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type DownloadPresigner interface {
	PresignGetWithContentDisposition(ctx context.Context, key string, ttl time.Duration, contentDisposition string) (string, error)
}
