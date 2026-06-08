package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type OSSStorage struct {
	client          *oss.Client
	bucket          string
	region          string
	cdnDomain       string
	cdnAuthKey      string
	endpointURL     string
	staticDomain    string
	presignDisabled bool
}

// NewOSSStorageFromEnv creates an OSSStorage from environment variables.
// Returns nil if OSS_BUCKET is not set.
//
// Environment variables:
//   - OSS_BUCKET (required)
//   - OSS_REGION (required, e.g. cn-hangzhou)
//   - ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET (optional; falls back to ECS RAM role)
//   - OSS_CDN_DOMAIN (optional)
//   - OSS_CDN_AUTH_KEY (optional; Alibaba Cloud CDN URL Auth Type A private key — enables CDN signed URLs)
//   - OSS_ENDPOINT (optional, custom endpoint for internal/VPC access)
//   - STATIC_DOMAIN (optional; hostname for the auth-redirect proxy route)
//   - OSS_PRESIGN_DISABLED (optional; set to "true" or "1" to disable presigned URL generation)
func NewOSSStorageFromEnv() *OSSStorage {
	bucket := os.Getenv("OSS_BUCKET")
	if bucket == "" {
		slog.Info("OSS_BUCKET not set, OSS upload disabled")
		return nil
	}

	region := os.Getenv("OSS_REGION")
	if region == "" {
		slog.Warn("OSS_REGION not set, OSS upload disabled")
		return nil
	}

	cfg := oss.LoadDefaultConfig().WithRegion(region)

	accessKey := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	secretKey := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	if accessKey != "" && secretKey != "" {
		cfg = cfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey))
	} else {
		cfg = cfg.WithCredentialsProvider(credentials.NewEcsRoleCredentialsProvider())
	}

	endpointURL := os.Getenv("OSS_ENDPOINT")
	if endpointURL != "" {
		cfg = cfg.WithEndpoint(endpointURL)
	}

	cdnDomain := os.Getenv("OSS_CDN_DOMAIN")
	cdnAuthKey := os.Getenv("OSS_CDN_AUTH_KEY")
	staticDomain := strings.TrimSpace(os.Getenv("STATIC_DOMAIN"))

	presignDisabledVal := strings.TrimSpace(os.Getenv("OSS_PRESIGN_DISABLED"))
	presignDisabled := presignDisabledVal == "true" || presignDisabledVal == "1"

	slog.Info("OSS storage initialized",
		"bucket", bucket,
		"region", region,
		"cdn_domain", cdnDomain,
		"cdn_auth", cdnAuthKey != "",
		"endpoint", endpointURL,
		"static_domain", staticDomain,
		"presign_disabled", presignDisabled,
	)
	return &OSSStorage{
		client:          oss.NewClient(cfg),
		bucket:          bucket,
		region:          region,
		cdnDomain:       cdnDomain,
		cdnAuthKey:      cdnAuthKey,
		endpointURL:     endpointURL,
		staticDomain:    staticDomain,
		presignDisabled: presignDisabled,
	}
}

func (o *OSSStorage) CdnDomain() string {
	return o.cdnDomain
}

// KeyFromURL extracts the OSS object key from a CDN or bucket URL.
func (o *OSSStorage) KeyFromURL(rawURL string) string {
	prefixes := make([]string, 0, 3)
	if o.cdnDomain != "" {
		prefixes = append(prefixes, "https://"+o.cdnDomain+"/")
	}
	if o.endpointURL != "" {
		prefixes = append(prefixes, strings.TrimRight(o.endpointURL, "/")+"/"+o.bucket+"/")
	}
	// virtual-hosted-style: https://<bucket>.oss-<region>.aliyuncs.com/<key>
	prefixes = append(prefixes, "https://"+o.bucket+".oss-"+o.region+".aliyuncs.com/")

	for _, prefix := range prefixes {
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}
	// Generic fallback: extract full path from any URL so directory structure is preserved.
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		return strings.TrimPrefix(u.Path, "/")
	}
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

// PresignGetURL generates a time-limited signed URL for direct object download.
// Returns an empty string without error when OSS_PRESIGN_DISABLED is set.
//
// When OSS_CDN_DOMAIN and OSS_CDN_AUTH_KEY are both set, it generates an
// Alibaba Cloud CDN URL Authentication (Type A) signed URL. Otherwise it falls
// back to an OSS presigned URL that points directly to the bucket endpoint.
func (o *OSSStorage) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if o.presignDisabled {
		return "", nil
	}
	if key == "" {
		return "", fmt.Errorf("oss PresignGetURL: empty key")
	}
	if o.cdnDomain != "" && o.cdnAuthKey != "" {
		return o.signCDNURL(key, expiry), nil
	}
	result, err := o.client.Presign(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(o.bucket),
		Key:    oss.Ptr(key),
	}, oss.PresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("oss presign: %w", err)
	}
	return result.URL, nil
}

// signCDNURL generates an Alibaba Cloud CDN URL Authentication Type A URL.
//
// Type A format: https://<cdn>/<key>?auth_key=<timestamp>-<rand>-<uid>-<md5>
// MD5 input:     /<key>-<timestamp>-<rand>-<uid>-<privateKey>
func (o *OSSStorage) signCDNURL(key string, expiry time.Duration) string {
	uri := "/" + key
	timestamp := time.Now().Add(expiry).Unix()
	rand := "0"
	uid := "0"
	plain := fmt.Sprintf("%s-%d-%s-%s-%s", uri, timestamp, rand, uid, o.cdnAuthKey)
	hash := fmt.Sprintf("%x", md5.Sum([]byte(plain)))
	authKey := fmt.Sprintf("%d-%s-%s-%s", timestamp, rand, uid, hash)
	return fmt.Sprintf("https://%s%s?auth_key=%s", o.cdnDomain, uri, authKey)
}

func (o *OSSStorage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, fmt.Errorf("oss GetReader: empty key")
	}
	out, err := o.client.GetObject(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(o.bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return nil, fmt.Errorf("oss GetObject: %w", err)
	}
	return out.Body, nil
}

func (o *OSSStorage) Delete(ctx context.Context, key string) {
	if key == "" {
		return
	}
	_, err := o.client.DeleteObject(ctx, &oss.DeleteObjectRequest{
		Bucket: oss.Ptr(o.bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		slog.Error("oss DeleteObject failed", "key", key, "error", err)
	}
}

func (o *OSSStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		o.Delete(ctx, key)
	}
}

func (o *OSSStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	safe := sanitizeFilename(filename)
	disposition := "attachment"
	if isInlineContentType(contentType) {
		disposition = "inline"
	}
	_, err := o.client.PutObject(ctx, &oss.PutObjectRequest{
		Bucket:             oss.Ptr(o.bucket),
		Key:                oss.Ptr(key),
		Body:               bytes.NewReader(data),
		ContentType:        oss.Ptr(contentType),
		ContentDisposition: oss.Ptr(fmt.Sprintf(`%s; filename="%s"`, disposition, safe)),
		CacheControl:       oss.Ptr("max-age=432000,public"),
	})
	if err != nil {
		return "", fmt.Errorf("oss PutObject: %w", err)
	}
	return o.uploadedURL(key), nil
}

func (o *OSSStorage) uploadedURL(key string) string {
	if o.staticDomain != "" {
		return fmt.Sprintf("https://%s/%s", o.staticDomain, key)
	}
	if o.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", o.cdnDomain, key)
	}
	if o.endpointURL != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(o.endpointURL, "/"), o.bucket, key)
	}
	return fmt.Sprintf("https://%s.oss-%s.aliyuncs.com/%s", o.bucket, o.region, key)
}
