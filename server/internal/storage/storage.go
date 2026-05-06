package storage

import (
	"context"
	"time"
)

// PresignedUpload describes a pre-signed URL that a client can PUT a file
// to directly, bypassing the server's request-body path. The server
// pre-allocates the storage key so that on successful upload the client
// can call the /confirm endpoint and the attachment record becomes
// usable. Only some backends support this (S3); LocalStorage returns
// ErrPresignUnsupported.
type PresignedUpload struct {
	URL       string    `json:"upload_url"`
	Method    string    `json:"upload_method"`   // always "PUT"
	Headers   map[string]string `json:"required_headers,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Storage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
	Delete(ctx context.Context, key string)
	DeleteKeys(ctx context.Context, keys []string)
	KeyFromURL(rawURL string) string
	CdnDomain() string

	// PresignPut returns a URL the client can PUT to in order to upload
	// the object directly, without buffering through this server. The
	// returned URL encodes the content-type so the client MUST send the
	// same Content-Type header on its PUT. Backends that don't support
	// this (LocalStorage) should return (nil, ErrPresignUnsupported).
	PresignPut(ctx context.Context, key string, contentType string, filename string, expiresIn time.Duration) (*PresignedUpload, error)

	// PresignGet returns a time-limited URL that lets an un-authenticated
	// client GET the object. Used for attachment download when the bucket
	// itself is private (no public-read ACL). Backends that don't support
	// signed reads (LocalStorage) should return ErrPresignUnsupported; the
	// caller should fall back to the stable PublicURL.
	PresignGet(ctx context.Context, key string, expiresIn time.Duration) (string, error)

	// PublicURL returns the stable public URL for an already-uploaded
	// object, without uploading anything. Used after a successful
	// client-side PUT via PresignPut so the attachment record can point
	// at the real object location (CDN-aware).
	PublicURL(key string) string

	// StatObject returns the byte size of an existing object. Used by
	// the /confirm endpoint to verify the client actually completed
	// the PUT before marking the attachment ready.
	StatObject(ctx context.Context, key string) (sizeBytes int64, err error)
}
