package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Presigned direct-upload support for the S3 backend.
//
// The upload primitive is a presigned PUT against a staging key. A SigV4
// presigned PUT DOES bind Content-Type and a NON-ZERO Content-Length, because
// both are in the canonical request — so the store itself rejects a body of the
// wrong size or declared type (see PresignPut below). A zero length is not
// signed at all, so it is refused rather than issued.
//
// What it cannot bind is everything else: the presigned URL stays usable until
// it expires, so the client can re-PUT different bytes of the same length to
// the same key. That is why the size/type binding is not treated as the
// guarantee. The guarantee comes from the copy: the server inspects the staged
// object, then copies it to a final key under an If-Match on the ETag it just
// inspected. A re-PUT that lands in between fails the precondition instead of
// substituting bytes into a published attachment, and the final key is never
// presigned, so no client credential can write it directly.

// presignPutTTL bounds one PUT credential. S3 rejects an expiry beyond seven
// days for SigV4; the API's own limit (900s) is set by the handler, and this
// default only applies when a caller passes a non-positive ttl.
const presignPutTTL = 15 * time.Minute

// presignPutMaxTTL is the SigV4 ceiling for a presigned URL.
const presignPutMaxTTL = 7 * 24 * time.Hour

// PresignPut returns a presigned PUT for one object key. Content-Length and
// Content-Type are folded into the signature so the store itself rejects a
// body of the wrong size or type even before complete-upload inspects it.
//
// Why Content-Length is signed: an unsigned PUT accepts any number of bytes,
// so a client could park a 5 GiB object in the bucket using a credential issued
// for a 1 KiB upload. Signing x-amz-decoded-content-length would not help —
// that header is for chunked framing — so the length is bound by SigV4's
// content-length-in-signature rule, which the SDK applies to the canonical
// request.
//
// The corollary is that length 0 has no binding to offer: SigV4 signs
// content-length only when the value is non-zero, so an empty-file credential
// covers `host` alone and authorizes an unbounded body. Zero is therefore
// refused with ErrUnsupported and the caller routes empty files to the
// multipart path — an empty file is a valid upload, just not one this
// primitive can safely express.
func (s *S3Storage) PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string, contentLength int64) (string, map[string]string, error) {
	if key == "" {
		return "", nil, fmt.Errorf("s3 PresignPut: empty key")
	}
	if contentLength < 0 {
		return "", nil, fmt.Errorf("s3 PresignPut: content length is required")
	}
	if contentLength == 0 {
		// A zero length binds nothing. SigV4 folds content-length into the
		// canonical request only when there is a length to fold — a size-0 PUT
		// is signed over `host` alone, and a real bucket accepted a 120 MiB
		// object through such a URL. The credential would therefore authorize
		// "whatever the client feels like sending", which is precisely the
		// property this function exists to provide.
		//
		// Reported as a capability refusal rather than a bad request: an empty
		// file is a legitimate upload, just not one this path can express, and
		// callers translate ErrUnsupported into the machine-readable 501 that
		// sends the client down the multipart path.
		return "", nil, fmt.Errorf("s3 PresignPut: refusing a zero-length credential that binds no size: %w", ErrUnsupported)
	}
	if ttl <= 0 {
		ttl = presignPutTTL
	}
	if ttl > presignPutMaxTTL {
		ttl = presignPutMaxTTL
	}
	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
	}
	out, err := s.presignClient().PresignPutObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", nil, fmt.Errorf("s3 PresignPutObject: %w", err)
	}
	// Report exactly the headers the signature covers, read back from the
	// presign result rather than assumed: telling a client to send a header the
	// store will not verify (or to omit one it will) produces an opaque
	// SignatureDoesNotMatch at upload time. Host is set by any HTTP client and
	// is not the caller's to supply.
	headers := map[string]string{}
	for name := range out.SignedHeader {
		switch strings.ToLower(name) {
		case "content-type":
			headers["Content-Type"] = contentType
		case "content-length":
			headers["Content-Length"] = fmt.Sprintf("%d", contentLength)
		}
	}
	return out.URL, headers, nil
}

func (s *S3Storage) presignClient() *s3.PresignClient {
	return s3.NewPresignClient(s.client)
}

// HeadObject reports the stored object's size, ETag and recorded content type.
// A missing key returns ErrObjectNotFound so callers can distinguish "the
// client never finished its PUT" from "the store is unavailable".
func (s *S3Storage) HeadObject(ctx context.Context, key string) (ObjectInfo, error) {
	if key == "" {
		return ObjectInfo{}, fmt.Errorf("s3 HeadObject: empty key")
	}
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s.uploadChecksumOptions()...)
	if err != nil {
		if isS3NotFound(err) {
			return ObjectInfo{}, ErrObjectNotFound
		}
		return ObjectInfo{}, fmt.Errorf("s3 HeadObject: %w", err)
	}
	info := ObjectInfo{
		ETag:        strings.Trim(aws.ToString(out.ETag), `"`),
		ContentType: aws.ToString(out.ContentType),
	}
	if out.ContentLength != nil {
		info.Size = *out.ContentLength
	}
	return info, nil
}

// ReadPrefix reads at most n leading bytes of an object, for MIME sniffing and
// image dimension extraction. The body is closed on every path.
func (s *S3Storage) ReadPrefix(ctx context.Context, key string, n int, etag string) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("s3 ReadPrefix: empty key")
	}
	if n <= 0 {
		return nil, fmt.Errorf("s3 ReadPrefix: %d is not a positive byte count", n)
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	// Range only for a non-empty object: S3 rejects a range request against a
	// zero-length object with 416 InvalidRange, which would make every empty
	// upload fail at completion even though the object is perfectly valid.
	// Emptiness is not known here, so a zero upper bound is expressed by
	// omitting Range and reading the (necessarily tiny) body instead: any
	// non-empty object yields at least one byte, which the LimitReader below
	// bounds.
	//
	// A non-empty etag makes the read conditional on that exact version, so the
	// bytes cannot belong to a PUT that landed after the caller's HEAD.
	if etag != "" {
		input.IfMatch = aws.String(`"` + strings.Trim(etag, `"`) + `"`)
	}
	// n is a byte count, so the inclusive range end is n-1.
	input.Range = aws.String(fmt.Sprintf("bytes=0-%d", int64(n)-1))
	out, err := s.client.GetObject(ctx, input, s.uploadChecksumOptions()...)
	if err != nil {
		switch {
		case isS3NotFound(err):
			return nil, ErrObjectNotFound
		case isS3PreconditionFailed(err):
			return nil, ErrSourceChanged
		case isS3InvalidRange(err):
			// The object is empty (or the store refuses the range for an
			// equivalent reason): re-read it without a Range. This is the only
			// case where the unconditional form is correct, because the object
			// has no bytes to select from and its size (0) was verified by the
			// caller's HEAD.
			return s.readWholeSmallObject(ctx, key, n, etag)
		default:
			return nil, fmt.Errorf("s3 GetObject: %w", err)
		}
	}
	defer out.Body.Close()
	// n+1 so a caller can distinguish "exactly n bytes" from "more than n".
	buf, err := io.ReadAll(io.LimitReader(out.Body, int64(n)+1))
	if err != nil {
		return nil, fmt.Errorf("s3 GetObject body: %w", err)
	}
	return buf, nil
}

// readWholeSmallObject reads an object the store refused to serve as a range —
// in practice an empty one — still under the caller's version condition. The
// read is bounded by n+1 bytes, so a store that answers this for a large object
// cannot make the caller buffer it.
func (s *S3Storage) readWholeSmallObject(ctx context.Context, key string, n int, etag string) ([]byte, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if etag != "" {
		input.IfMatch = aws.String(`"` + strings.Trim(etag, `"`) + `"`)
	}
	out, err := s.client.GetObject(ctx, input, s.uploadChecksumOptions()...)
	if err != nil {
		switch {
		case isS3NotFound(err):
			return nil, ErrObjectNotFound
		case isS3PreconditionFailed(err):
			return nil, ErrSourceChanged
		default:
			return nil, fmt.Errorf("s3 GetObject: %w", err)
		}
	}
	defer out.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(out.Body, int64(n)+1))
	if err != nil {
		return nil, fmt.Errorf("s3 GetObject body: %w", err)
	}
	return buf, nil
}

// FinalizeObject copies the staged object to its permanent key under an
// If-Match on the ETag the caller inspected, then — because S3's copy is a
// single atomic operation that applies the destination metadata — removes the
// staging object only after the copy succeeded.
//
// MetadataDirective REPLACE is what makes the destination's Content-Type and
// Content-Disposition the server's decision rather than the uploader's: the
// presigned PUT stored whatever header the client sent, and carrying it over
// (COPY) would republish a client-controlled type. This same call therefore
// implements both "finalize" and "set verified metadata".
func (s *S3Storage) FinalizeObject(ctx context.Context, srcKey, srcETag, dstKey string, expectedSize int64, contentType, contentDisposition, cacheControl string) error {
	if srcKey == "" || dstKey == "" {
		return fmt.Errorf("s3 FinalizeObject: source and destination keys are required")
	}
	if srcKey == dstKey {
		return fmt.Errorf("s3 FinalizeObject: source and destination are the same key")
	}
	if strings.TrimSpace(srcETag) == "" {
		// Without a precondition the copy is an unconditional read of whatever
		// the staging key holds right now, which is exactly the substitution
		// this whole path exists to prevent.
		return fmt.Errorf("s3 FinalizeObject: source ETag is required for the conditional copy")
	}
	input := &s3.CopyObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(dstKey),
		CopySource:         aws.String(copySource(s.bucket, srcKey)),
		CopySourceIfMatch:  aws.String(`"` + strings.Trim(srcETag, `"`) + `"`),
		MetadataDirective:  types.MetadataDirectiveReplace,
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(contentDisposition),
		CacheControl:       aws.String(cacheControl),
		StorageClass:       s.storageClass(),
	}
	if _, err := s.client.CopyObject(ctx, input, s.uploadChecksumOptions()...); err != nil {
		switch {
		case isS3NotFound(err):
			return ErrObjectNotFound
		case isS3PreconditionFailed(err):
			return ErrSourceChanged
		default:
			return fmt.Errorf("s3 CopyObject: %w", err)
		}
	}
	// Re-read the destination rather than trusting the copy response: the
	// published size is what a download will actually serve, and a copy that
	// silently truncated would otherwise be recorded as the declared size.
	info, err := s.HeadObject(ctx, dstKey)
	if err != nil {
		return fmt.Errorf("s3 FinalizeObject: verify destination: %w", err)
	}
	if info.Size != expectedSize {
		// Leave both objects in place: the caller's recovery path decides
		// whether to retry or fail, and deleting here would race the retry.
		return fmt.Errorf("s3 FinalizeObject: destination is %d bytes, expected %d", info.Size, expectedSize)
	}
	return nil
}

// copySource builds the x-amz-copy-source value. Each path segment is escaped
// individually so a key containing a space or a slash survives the round trip;
// S3 requires the header's value to be URL-encoded.
func copySource(bucket, key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = escapeCopySourceSegment(segment)
	}
	return bucket + "/" + strings.Join(segments, "/")
}

func escapeCopySourceSegment(segment string) string {
	// url.PathEscape escapes the characters that break the header (space, "?",
	// "#") but leaves "+" alone, and S3 would read a literal "+" as a space.
	return strings.ReplaceAll(url.PathEscape(segment), "+", "%2B")
}

// isS3NotFound distinguishes "this object does not exist" from a transport or
// authorization failure. Both a typed NoSuchKey and the generic NotFound (which
// is what this SDK returns for a missing key on several S3-compatible stores)
// mean the same thing to a caller.
func isS3NotFound(err error) bool {
	var noKey *types.NoSuchKey
	if errors.As(err, &noKey) {
		return true
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	// HEAD responses carry no body, so the SDK cannot decode an error code and
	// reports the bare status instead.
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	return false
}

// isS3InvalidRange reports a 416, which S3 returns for a Range request against
// an empty object (and for a range that starts past EOF).
func isS3InvalidRange(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidRange" {
		return true
	}
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusRequestedRangeNotSatisfiable {
		return true
	}
	return false
}

// isS3PreconditionFailed reports a failed If-Match on the copy source — the
// staged object changed between inspection and finalization.
func isS3PreconditionFailed(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "PreconditionFailed" {
			return true
		}
	}
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusPreconditionFailed {
		return true
	}
	return false
}
