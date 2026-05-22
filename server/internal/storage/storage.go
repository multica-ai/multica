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

type KeyPrefixStorage interface {
	KeyPrefix() string
}

type ObjectInfo struct {
	SizeBytes   int64
	ContentType string
}

type DirectUploadStorage interface {
	CreatePresignedPutURL(ctx context.Context, key string, contentType string, filename string, expires time.Duration) (string, map[string]string, error)
	HeadObject(ctx context.Context, key string) (ObjectInfo, error)
	PublicURL(key string) string
}

type MultipartUploadPart struct {
	PartNumber int32
	ETag       string
}

type MultipartUploadStorage interface {
	DirectUploadStorage
	CreateMultipartUpload(ctx context.Context, key string, contentType string, filename string, expires time.Duration) (uploadID string, headers map[string]string, err error)
	CreatePresignedUploadPartURL(ctx context.Context, key string, uploadID string, partNumber int32, expires time.Duration) (url string, headers map[string]string, err error)
	CompleteMultipartUpload(ctx context.Context, key string, uploadID string, parts []MultipartUploadPart) error
	AbortMultipartUpload(ctx context.Context, key string, uploadID string) error
}
