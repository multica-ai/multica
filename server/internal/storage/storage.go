package storage

import (
	"context"
	"io"
)

type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string
	CdnDomain() string
	// GetReader streams an object back to the caller. Used by the attachment
	// preview proxy (GET /api/attachments/{id}/content) to bypass CloudFront
	// CORS and the inline/attachment Content-Disposition decision. Caller
	// must Close the returned reader.
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}
