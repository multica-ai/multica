package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	client      *s3.Client
	getClient   *s3.Client // same config as client, but always virtual-hosted style; used for PresignGet
	bucket      string
	cdnDomain   string // if set, returned URLs use this instead of bucket name
	endpointURL string // if set, use path-style URLs (e.g. MinIO)
}

// NewS3StorageFromEnv creates an S3Storage from environment variables.
// Returns nil if S3_BUCKET is not set.
//
// Environment variables:
//   - S3_BUCKET (required)
//   - S3_REGION (default: us-west-2)
//   - AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (optional; falls back to default credential chain)
func NewS3StorageFromEnv() *S3Storage {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		slog.Info("S3_BUCKET not set, cloud upload disabled")
		return nil
	}

	region := os.Getenv("S3_REGION")
	if region == "" {
		region = "us-west-2"
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey != "" && secretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		return nil
	}

	cdnDomain := os.Getenv("CLOUDFRONT_DOMAIN")

	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	s3Opts := []func(*s3.Options){}
	if endpointURL != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
			o.UsePathStyle = true
		})
	}

	slog.Info("S3 storage initialized",
		"bucket", bucket,
		"region", region,
		"cdn_domain", cdnDomain,
		"endpoint_url", endpointURL,
	)
	// Separate client for PresignGet — always virtual-hosted style.
	// Some non-AWS S3-compatible backends accept presigned PUT against
	// path-style URLs but reject presigned GET against path-style with
	// 403, even when the signature is correct. Virtual-hosted style
	// works on those backends and is identical to path-style on real
	// AWS, so this is safe everywhere.
	getS3Opts := []func(*s3.Options){}
	if endpointURL != "" {
		getS3Opts = append(getS3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
			o.UsePathStyle = false
		})
	}
	return &S3Storage{
		client:      s3.NewFromConfig(cfg, s3Opts...),
		getClient:   s3.NewFromConfig(cfg, getS3Opts...),
		bucket:      bucket,
		cdnDomain:   cdnDomain,
		endpointURL: endpointURL,
	}
}

func (s *S3Storage) CdnDomain() string {
	return s.cdnDomain
}

// storageClass returns the appropriate S3 storage class.
// Custom endpoints (e.g. MinIO) only support STANDARD; real AWS defaults to INTELLIGENT_TIERING.
func (s *S3Storage) storageClass() types.StorageClass {
	if s.endpointURL != "" {
		return types.StorageClassStandard
	}
	return types.StorageClassIntelligentTiering
}

// KeyFromURL extracts the S3 object key from a CDN or bucket URL.
// e.g. "https://multica-static.copilothub.ai/abc123.png" → "abc123.png"
func (s *S3Storage) KeyFromURL(rawURL string) string {
	if s.endpointURL != "" {
		prefix := strings.TrimRight(s.endpointURL, "/") + "/" + s.bucket + "/"
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}

	// Strip the "https://domain/" prefix.
	for _, prefix := range []string{
		"https://" + s.cdnDomain + "/",
		"https://" + s.bucket + "/",
	} {
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}
	// Fallback: take everything after the last "/".
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

// Delete removes an object from S3. Errors are logged but not fatal.
func (s *S3Storage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		slog.Error("s3 DeleteObject failed", "key", key, "error", err)
	}
}

// DeleteKeys removes multiple objects from S3. Best-effort, errors are logged.
func (s *S3Storage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *S3Storage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	safe := sanitizeFilename(filename)
	disposition := "attachment"
	if isInlineContentType(contentType) {
		disposition = "inline"
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               bytes.NewReader(data),
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(fmt.Sprintf(`%s; filename="%s"`, disposition, safe)),
		CacheControl:       aws.String("max-age=432000,public"),
		StorageClass:       s.storageClass(),
	})
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}
	return s.uploadedURL(key), nil
}

// uploadedURL returns the URL stored for client consumption after an upload.
// Priority: CDN domain > custom endpoint > bucket. The CDN domain wins even when
// a custom endpoint is set so S3-compatible backends (MinIO, R2, B2, Wasabi, etc.)
// can be paired with a separate public-read domain — writes still go through the
// SDK with the custom endpoint; only the reader-facing URL changes.
func (s *S3Storage) uploadedURL(key string) string {
	if s.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", s.cdnDomain, key)
	}
	if s.endpointURL != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(s.endpointURL, "/"), s.bucket, key)
	}
	return fmt.Sprintf("https://%s/%s", s.bucket, key)
}

// PublicURL is the Storage-interface version of uploadedURL; it returns
// the stable read-side URL for an object that has already been uploaded
// (e.g. via a pre-signed PUT).
func (s *S3Storage) PublicURL(key string) string {
	return s.uploadedURL(key)
}

// PresignPut returns a pre-signed PUT URL for key. The returned URL
// encodes content-type + content-disposition into the signature, so
// the client MUST send the same Content-Type header on its PUT call.
// This lets the client stream multi-GB uploads directly to S3 without
// the server buffering the request body (which would OOM the daemon
// box for kernel-dump-sized files).
func (s *S3Storage) PresignPut(ctx context.Context, key string, contentType string, filename string, expiresIn time.Duration) (*PresignedUpload, error) {
	if expiresIn <= 0 {
		expiresIn = 15 * time.Minute
	}
	ps := s3.NewPresignClient(s.client)
	// Keep the signed input minimal — only headers the client can easily
	// reproduce on its PUT (Content-Type). Do NOT sign Content-Disposition
	// here: the client would have to echo it byte-for-byte and 99% of
	// clients (curl --upload-file, boto3, plain fetch) don't set it by
	// default, so the PUT would fail with SignatureDoesNotMatch. If the
	// caller needs a friendly download filename later, use
	// response-content-disposition query params on the GET URL instead.
	req, err := ps.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		ContentType:  aws.String(contentType),
		StorageClass: s.storageClass(),
	}, func(o *s3.PresignOptions) {
		o.Expires = expiresIn
	})
	if err != nil {
		return nil, fmt.Errorf("presign PutObject: %w", err)
	}
	// Client MUST send the same Content-Type on the PUT — SigV4 signs
	// it. filename parameter is kept for API symmetry with Upload()
	// but is intentionally unused on this path; it's already part of
	// the attachment record.
	_ = filename
	headers := map[string]string{
		"Content-Type": contentType,
	}
	return &PresignedUpload{
		URL:       req.URL,
		Method:    req.Method,
		Headers:   headers,
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
}

// StatObject returns the byte size of an existing object. The client's
// /confirm endpoint uses this to verify the pre-signed PUT actually
// completed before the attachment record flips to "ready".
func (s *S3Storage) StatObject(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("s3 HeadObject: %w", err)
	}
	if out.ContentLength == nil {
		return 0, fmt.Errorf("s3 HeadObject: no ContentLength for %s", key)
	}
	return *out.ContentLength, nil
}

// PresignGet returns a short-lived signed GET URL for the object. Used
// by the attachment download path when the bucket is private — without
// a signed URL the client gets 403 AccessDenied. Only the bucket+key+
// expiry are signed; the client does not need to send any custom
// header on the GET.
//
// Important EOS compatibility detail: the upload path uses path-style
// addressing (https://endpoint/bucket/key) because that's what the PUT
// code path + STREAMING-UNSIGNED-PAYLOAD combo requires. But 移动云 EOS
// rejects pre-signed GET requests issued against the path-style URL
// with 403, while the same pre-signed GET against virtual-hosted style
// (https://bucket.endpoint/key) works. Signing a distinct client for
// GET lets both halves of the flow work independently.
//
// For real AWS S3 (no custom endpoint) both styles are accepted, so
// the distinction is harmless.
func (s *S3Storage) PresignGet(ctx context.Context, key string, expiresIn time.Duration) (string, error) {
	if expiresIn <= 0 {
		expiresIn = 15 * time.Minute
	}
	ps := s3.NewPresignClient(s.getClient)
	req, err := ps.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, func(o *s3.PresignOptions) {
		o.Expires = expiresIn
	})
	if err != nil {
		return "", fmt.Errorf("presign GetObject: %w", err)
	}
	return req.URL, nil
}
