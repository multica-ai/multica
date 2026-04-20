package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	client         *s3.Client
	bucket         string
	cdnDomain      string
	endpoint       *url.URL // parsed S3_ENDPOINT for URL construction
	forcePathStyle bool     // if true, use path-style URLs
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

	// Custom endpoint for S3-compatible services (Aliyun OSS, MinIO, etc.)
	endpoint := os.Getenv("S3_ENDPOINT")
	forcePathStyle, _ := strconv.ParseBool(os.Getenv("S3_FORCE_PATH_STYLE"))

	// Parse endpoint URL for URL construction.
	var endpointURL *url.URL
	if endpoint != "" {
		var err error
		endpointURL, err = url.Parse(endpoint)
		if err != nil {
			slog.Error("failed to parse S3_ENDPOINT", "endpoint", endpoint, "error", err)
			return nil
		}
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
		client:         client,
		bucket:         bucket,
		cdnDomain:      cdnDomain,
		endpoint:       endpointURL,
		forcePathStyle: forcePathStyle,
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
	// Try CDN domain prefix.
	if s.cdnDomain != "" {
		if prefix := "https://" + s.cdnDomain + "/"; strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}

	// Try bucket name prefix (AWS S3 standard).
	if prefix := "https://" + s.bucket + "/"; strings.HasPrefix(rawURL, prefix) {
		return strings.TrimPrefix(rawURL, prefix)
	}

	// Try endpoint-based URLs.
	if s.endpoint != nil {
		scheme := s.endpoint.Scheme
		host := s.endpoint.Host
		if s.forcePathStyle {
			// Path-style: scheme://host/bucket/key
			if prefix := scheme + "://" + host + "/" + s.bucket + "/"; strings.HasPrefix(rawURL, prefix) {
				return strings.TrimPrefix(rawURL, prefix)
			}
		} else {
			// Virtual-hosted: scheme://bucket.host/key
			if prefix := scheme + "://" + s.bucket + "." + host + "/"; strings.HasPrefix(rawURL, prefix) {
				return strings.TrimPrefix(rawURL, prefix)
			}
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

	input := &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               bytes.NewReader(data),
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(fmt.Sprintf(`%s; filename="%s"`, disposition, safe)),
		CacheControl:       aws.String("max-age=432000,public"),
<<<<<<< HEAD
		ContentLength:      aws.Int64(int64(len(data))),
	}

	// Only set StorageClass for AWS S3 (no custom endpoint).
	// S3-compatible services may not support IntelligentTiering.
	if s.endpoint == nil {
		input.StorageClass = types.StorageClassIntelligentTiering
	}

	_, err := s.client.PutObject(ctx, input)
=======
		StorageClass:       s.storageClass(),
	})
>>>>>>> main
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}

<<<<<<< HEAD
	return s.objectURL(key), nil
}

// objectURL builds the public URL for an object. Priority:
//  1. cdnDomain       → https://cdnDomain/key
//  2. path-style      → scheme://endpoint/bucket/key (when forcePathStyle)
//  3. virtual-hosted  → scheme://bucket.endpoint/key (default for S3-compatible)
//  4. AWS S3          → https://bucket/key (typically overridden by CLOUDFRONT_DOMAIN)
func (s *S3Storage) objectURL(key string) string {
	if s.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", s.cdnDomain, key)
	}
	if s.endpoint != nil {
		if s.forcePathStyle {
			// Path-style: scheme://host/bucket/key
			return fmt.Sprintf("%s://%s/%s/%s", s.endpoint.Scheme, s.endpoint.Host, s.bucket, key)
		}
		// Virtual-hosted: scheme://bucket.host/key
		return fmt.Sprintf("%s://%s.%s/%s", s.endpoint.Scheme, s.endpoint.Host, s.bucket, key)
	}
	return fmt.Sprintf("https://%s/%s", s.bucket, key)
}
