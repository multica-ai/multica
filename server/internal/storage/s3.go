package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	client       *s3.Client
	bucket       string
	cdnDomain    string // if set, returned URLs use this instead of bucket name
	endpointHost string // hostname portion of S3_ENDPOINT for URL construction
}

// NewS3StorageFromEnv creates an S3Storage from environment variables.
// Returns nil if S3_BUCKET is not set.
//
// Environment variables:
//   - S3_BUCKET (required)
//   - S3_REGION (default: us-west-2)
//   - S3_ENDPOINT (optional; for S3-compatible services like Aliyun OSS, MinIO)
//   - S3_FORCE_PATH_STYLE (optional; default false)
//   - AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (optional; falls back to default credential chain)
func NewS3StorageFromEnv() *S3Storage {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		slog.Info("S3_BUCKET not set, file upload disabled")
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

	// Custom endpoint for S3-compatible services (Aliyun OSS, MinIO, etc.)
	endpoint := os.Getenv("S3_ENDPOINT")
	forcePathStyle, _ := strconv.ParseBool(os.Getenv("S3_FORCE_PATH_STYLE"))

	// Extract hostname from endpoint for URL construction.
	var endpointHost string
	if endpoint != "" {
		endpointHost = strings.TrimPrefix(endpoint, "https://")
		endpointHost = strings.TrimPrefix(endpointHost, "http://")
		endpointHost = strings.TrimRight(endpointHost, "/")
	}

	var client *s3.Client
	if endpoint != "" {
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = forcePathStyle
			// S3-compatible services (Aliyun OSS, MinIO) don't support aws-chunked
			// content encoding. Only calculate checksums when the operation requires it.
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		})
	} else {
		client = s3.NewFromConfig(cfg)
	}

	cdnDomain := os.Getenv("CLOUDFRONT_DOMAIN")

	slog.Info("S3 storage initialized", "bucket", bucket, "region", region, "endpoint", endpoint, "cdn_domain", cdnDomain)
	return &S3Storage{
		client:       client,
		bucket:       bucket,
		cdnDomain:    cdnDomain,
		endpointHost: endpointHost,
	}
}

// sanitizeFilename removes characters that could cause header injection in Content-Disposition.
func sanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		// Strip control chars, newlines, null bytes, quotes, semicolons, backslashes
		if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '\\' || r == '\x00' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// KeyFromURL extracts the S3 object key from a CDN or bucket URL.
// e.g. "https://multica-static.copilothub.ai/abc123.png" → "abc123.png"
func (s *S3Storage) KeyFromURL(rawURL string) string {
	// Strip the "https://domain/" prefix.
	for _, prefix := range []string{
		"https://" + s.cdnDomain + "/",
		"https://" + s.bucket + "/",
	} {
		if prefix != "https:///" && strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}
	// Also try endpoint-based URL: https://bucket.endpoint-host/key
	if s.endpointHost != "" {
		if prefix := "https://" + s.bucket + "." + s.endpointHost + "/"; strings.HasPrefix(rawURL, prefix) {
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

// isInlineContentType returns true for media types that browsers should
// display inline (images, video, audio, PDF). Everything else triggers a
// download via Content-Disposition: attachment.
func isInlineContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/") ||
		strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "audio/") ||
		ct == "application/pdf"
}

func (s *S3Storage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	safe := sanitizeFilename(filename)
	disposition := "attachment"
	if isInlineContentType(contentType) {
		disposition = "inline"
	}

	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               reader,
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(fmt.Sprintf(`%s; filename="%s"`, disposition, safe)),
		CacheControl:       aws.String("max-age=432000,public"),
		ContentLength:      aws.Int64(int64(len(data))),
	})
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}

	return s.objectURL(key), nil
}

// objectURL builds the public URL for an object. Priority:
//  1. cdnDomain  → https://cdnDomain/key
//  2. endpoint   → https://bucket.endpointHost/key  (virtual-hosted style)
//  3. AWS S3     → https://bucket/key (typically overridden by CLOUDFRONT_DOMAIN)
func (s *S3Storage) objectURL(key string) string {
	if s.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", s.cdnDomain, key)
	}
	if s.endpointHost != "" {
		return fmt.Sprintf("https://%s.%s/%s", s.bucket, s.endpointHost, key)
	}
	return fmt.Sprintf("https://%s/%s", s.bucket, key)
}