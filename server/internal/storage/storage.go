package storage

import (
	"context"
	"io"
	"time"
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

// WorkspaceObjectKey builds the canonical object key for a workspace-scoped
// attachment: "workspaces/<workspaceID>/<filename>". It is the single source of
// truth for the layout so the web upload handler and the inbound-media
// ingesters cannot drift apart (a prefix-based listing/cleanup relies on both
// producing the same shape).
func WorkspaceObjectKey(workspaceID, filename string) string {
	return "workspaces/" + workspaceID + "/" + filename
}

type Presigner interface {
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type DownloadPresigner interface {
	PresignGetWithContentDisposition(ctx context.Context, key string, ttl time.Duration, contentDisposition string) (string, error)
}
