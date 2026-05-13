package storage

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type OSSStorage struct {
	client        *s3.Client
	bucket        string
	region        string
	endpoint      string
	publicBaseURL string
}

// NewOSSStorageFromEnv creates an OSSStorage from environment variables.
// Returns nil if OSS_BUCKET is not set.
//
// Environment variables:
//   - OSS_BUCKET (required)
//   - OSS_REGION (default: cn-hangzhou)
//   - OSS_ENDPOINT (default: https://oss-<region>.aliyuncs.com)
//   - OSS_ACCESS_KEY_ID / OSS_ACCESS_KEY_SECRET / OSS_SECURITY_TOKEN (optional)
//   - OSS_PUBLIC_BASE_URL (optional; CDN or custom domain used for returned URLs)
func NewOSSStorageFromEnv() *OSSStorage {
	bucket := strings.TrimSpace(os.Getenv("OSS_BUCKET"))
	if bucket == "" {
		return nil
	}

	region := strings.TrimSpace(os.Getenv("OSS_REGION"))
	if region == "" {
		region = "cn-hangzhou"
	}

	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("OSS_ENDPOINT")), "/")
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://oss-%s.aliyuncs.com", region)
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}
	accessKey := os.Getenv("OSS_ACCESS_KEY_ID")
	secretKey := os.Getenv("OSS_ACCESS_KEY_SECRET")
	securityToken := os.Getenv("OSS_SECURITY_TOKEN")
	if accessKey != "" && secretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, securityToken),
		))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		slog.Error("failed to load OSS config", "error", err)
		return nil
	}

	publicBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OSS_PUBLIC_BASE_URL")), "/")
	slog.Info("OSS storage initialized", "bucket", bucket, "region", region, "endpoint", endpoint, "public_base_url", publicBaseURL)
	return &OSSStorage{
		client: s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = false
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}),
		bucket:        bucket,
		region:        region,
		endpoint:      endpoint,
		publicBaseURL: publicBaseURL,
	}
}

func (s *OSSStorage) CdnDomain() string {
	u, err := url.Parse(s.uploadedURL(""))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *OSSStorage) KeyFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return fallbackKeyFromURL(rawURL)
	}

	key := strings.TrimPrefix(u.EscapedPath(), "/")
	if decoded, err := url.PathUnescape(key); err == nil {
		key = decoded
	}

	if s.publicBaseURL != "" && strings.HasPrefix(rawURL, s.publicBaseURL+"/") {
		return key
	}

	if endpointURL, err := url.Parse(s.endpoint); err == nil && endpointURL.Hostname() != "" {
		host := u.Hostname()
		endpointHost := endpointURL.Hostname()
		if host == endpointHost {
			return strings.TrimPrefix(key, s.bucket+"/")
		}
		if host == s.bucket+"."+endpointHost {
			return key
		}
	}

	defaultHost := fmt.Sprintf("%s.oss-%s.aliyuncs.com", s.bucket, s.region)
	if u.Hostname() == defaultHost {
		return key
	}
	if u.Hostname() == fmt.Sprintf("oss-%s.aliyuncs.com", s.region) {
		return strings.TrimPrefix(key, s.bucket+"/")
	}

	return fallbackKeyFromURL(rawURL)
}

func (s *OSSStorage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		slog.Error("oss DeleteObject failed", "key", key, "error", err)
	}
}

func (s *OSSStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *OSSStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
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
		ContentLength:      aws.Int64(int64(len(data))),
	})
	if err != nil {
		return "", fmt.Errorf("oss PutObject: %w", err)
	}
	return s.uploadedURL(key), nil
}

func (s *OSSStorage) uploadedURL(key string) string {
	if s.publicBaseURL != "" {
		return strings.TrimRight(s.publicBaseURL, "/") + "/" + key
	}
	return fmt.Sprintf("https://%s.oss-%s.aliyuncs.com/%s", s.bucket, s.region, key)
}
