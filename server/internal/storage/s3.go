package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	client              *s3.Client
	bucket              string
	publicBaseURL       string // base URL for generated file links, e.g. https://cdn.example.com
	usingCustomEndpoint bool   // true for R2/MinIO — limits unsupported S3 features
}

	// NewS3StorageFromEnv creates an S3Storage from environment variables.
// Returns nil if S3_BUCKET is not set.
//
// Environment variables:
//   - S3_BUCKET (required)
//   - S3_REGION (default: us-west-2)
//   - AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (optional; falls back to default credential chain)
//   - AWS_ENDPOINT_URL (optional; for S3-compatible stores like Cloudflare R2)
//   - CLOUDFRONT_DOMAIN (optional; CDN domain used as the base URL for public file links)
//     When using R2 without a custom domain, set this to your R2 public bucket URL.
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

	cdnDomain := os.Getenv("CLOUDFRONT_DOMAIN")

	// S3-compatible custom endpoint (e.g. Cloudflare R2, MinIO).
	// AWS_ENDPOINT_URL follows the same convention as the AWS CLI.
	var s3ClientOpts []func(*s3.Options)
	usingCustomEndpoint := false
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if endpointURL != "" {
		usingCustomEndpoint = true
		s3ClientOpts = append(s3ClientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
			// Cloudflare R2 and other S3-compatible stores use path-style addressing.
			o.UsePathStyle = true
		})
	}

	// Determine the base URL used to construct public file links:
	//   1. CLOUDFRONT_DOMAIN if set (CDN or R2 custom domain)
	//   2. Standard AWS virtual-hosted URL: https://{bucket}.s3.{region}.amazonaws.com
	// When using a custom endpoint without a CDN domain, CLOUDFRONT_DOMAIN must be set
	// to the bucket's public access URL (e.g. R2 public dev URL or custom domain).
	var publicBaseURL string
	if cdnDomain != "" {
		publicBaseURL = "https://" + strings.TrimRight(cdnDomain, "/")
	} else if !usingCustomEndpoint {
		publicBaseURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", bucket, region)
	} else {
		// Custom endpoint without CDN domain — warn and leave empty; Upload will error.
		slog.Warn("AWS_ENDPOINT_URL is set but CLOUDFRONT_DOMAIN is not; file URLs will be invalid. Set CLOUDFRONT_DOMAIN to your bucket's public URL.")
	}

	slog.Info("S3 storage initialized", "bucket", bucket, "region", region, "public_base_url", publicBaseURL)
	return &S3Storage{
		client:              s3.NewFromConfig(cfg, s3ClientOpts...),
		bucket:              bucket,
		publicBaseURL:       publicBaseURL,
		usingCustomEndpoint: usingCustomEndpoint,
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
// e.g. "https://cdn.example.com/abc123.png" → "abc123.png"
func (s *S3Storage) KeyFromURL(rawURL string) string {
	// Strip the publicBaseURL prefix if it matches.
	if s.publicBaseURL != "" {
		prefix := s.publicBaseURL + "/"
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

	// Use inline disposition for media types that browsers can preview natively;
	// everything else should be downloaded as an attachment.
	disposition := fmt.Sprintf(`attachment; filename="%s"`, safe)
	if isPreviewable(contentType) {
		disposition = fmt.Sprintf(`inline; filename="%s"`, safe)
	}

	input := &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               bytes.NewReader(data),
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(disposition),
		CacheControl:       aws.String("max-age=432000,public"),
	}
	// IntelligentTiering is AWS S3-specific; R2 and other compatible stores reject it.
	if !s.usingCustomEndpoint {
		input.StorageClass = types.StorageClassIntelligentTiering
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}

	if s.publicBaseURL == "" {
		return "", fmt.Errorf("s3 storage: publicBaseURL is empty; set CLOUDFRONT_DOMAIN to your bucket's public URL")
	}
	link := s.publicBaseURL + "/" + key
	return link, nil
}

// isPreviewable reports whether a content type should be displayed inline in the browser.
func isPreviewable(contentType string) bool {
	for _, prefix := range []string{"image/", "video/", "audio/"} {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	switch contentType {
	case "application/pdf", "text/plain":
		return true
	}
	return false
}
