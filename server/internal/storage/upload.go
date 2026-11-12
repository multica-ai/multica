package storage

import (
	"context"
	"errors"
	"time"
)

// ErrObjectNotFound reports that the object a caller named does not exist. It
// is separate from a transport failure because the two mean opposite things to
// a caller: a missing object can never become present, so a complete-upload
// that sees one must refuse the request rather than retry it.
var ErrObjectNotFound = errors.New("object not found")

// ErrSourceChanged reports that an object changed between the read that
// described it and the write that was meant to be conditional on that
// description. It is the signal a certificate-of-copy is missing its
// precondition: the bytes copied would not be the bytes that were validated.
var ErrSourceChanged = errors.New("object changed during copy")

// ErrUnsupported reports that the configured backend cannot perform an
// operation. Callers translate it into a stable, machine-readable refusal (a
// 501) rather than pretending the capability exists, because the fallback path
// is the client's decision: a client may upload through the API instead, but
// only if it is told plainly that the direct path is unavailable here.
var ErrUnsupported = errors.New("storage backend does not support this operation")

// S3 object metadata limits the presigned PUT path needs to enforce. The
// handler has its own HTTP-facing constants; these bound what a storage
// backend will accept in a HEAD result.
type ObjectInfo struct {
	Size int64
	// ETag is the object's entity tag as the store reported it. It is used as
	// the copy precondition — NOT as a content hash: S3 defines it as the MD5
	// of the object only for single-part uploads, so treating it as one would
	// be wrong for anything uploaded in parts. Comparing it to itself is valid
	// regardless: it identifies one stored version of the bytes.
	ETag string
	// ContentType is what the store recorded from the uploader's header. It is
	// client-controlled and must never be the published content type; callers
	// sniff the bytes instead.
	ContentType string
}

// UploadPresigner issues a single-object PUT credential and reports the
// headers that credential requires. headers must be sent verbatim or the
// signature will not validate.
type UploadPresigner interface {
	PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string, contentLength int64) (url string, headers map[string]string, err error)
}

// ObjectInspector reports what an object currently is, and reads a bounded
// prefix of it for content sniffing without pulling the whole body.
type ObjectInspector interface {
	HeadObject(ctx context.Context, key string) (ObjectInfo, error)
	// ReadPrefix returns at most n leading bytes of the object.
	//
	// A NON-EMPTY etag makes the read conditional: the store must return the
	// bytes of that exact object version or report a precondition failure. Call
	// it with the ETag HeadObject just returned whenever the bytes will be used
	// to describe an object that a later conditional copy will publish.
	//
	// Why the condition is load-bearing rather than defensive: the presigned URL
	// stays usable, so a client can PUT version B between the HEAD that
	// measured version A and this read. Without the condition the probe would
	// describe B while the copy (conditioned on A) publishes A — the published
	// bytes and the published metadata would describe different objects. Callers
	// pass a small n: this feeds a MIME/image sniffer, never a file load.
	ReadPrefix(ctx context.Context, key string, n int, etag string) ([]byte, error)
}

// ObjectFinalizer moves a staging object to its permanent key without routing
// the bytes through the application. The copy is conditional on the staged
// object still being the one the caller inspected, so a client that PUTs to
// its (still valid) staging URL after that inspection cannot substitute
// different bytes into the published attachment.
type ObjectFinalizer interface {
	// FinalizeObject copies srcKey to dstKey only when the source object still
	// carries srcETag and is exactly expectedSize bytes, and applies metadata to
	// the destination. A source that moved or changed returns ErrSourceChanged;
	// a missing source returns ErrObjectNotFound.
	FinalizeObject(ctx context.Context, srcKey, srcETag, dstKey string, expectedSize int64, contentType, contentDisposition, cacheControl string) error
}

// uploadCapable is the full capability set the presigned-upload endpoints
// require. A backend missing any part of it is reported as unsupported at the
// API boundary instead of failing partway through a client's upload.
type UploadCapable interface {
	UploadPresigner
	ObjectInspector
	ObjectFinalizer
}

// AsUploadCapable narrows a Storage to the presigned-upload capability.
func AsUploadCapable(s Storage) (UploadCapable, bool) {
	capable, ok := s.(UploadCapable)
	return capable, ok
}
