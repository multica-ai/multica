package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3StorageKeyFromURL_CustomEndpointPreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket:      "test-bucket",
		endpointURL: "http://localhost:9000",
	}

	rawURL := "http://localhost:9000/test-bucket/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_CustomEndpointWithTrailingSlash(t *testing.T) {
	s := &S3Storage{
		bucket:      "test-bucket",
		endpointURL: "http://localhost:9000/",
	}

	rawURL := "http://localhost:9000/test-bucket/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_VirtualHostedStylePreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket: "test-bucket",
		region: "us-east-1",
	}

	rawURL := "https://test-bucket.s3.us-east-1.amazonaws.com/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_PathStylePreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket: "bucket.with.dots",
		region: "us-east-1",
	}

	rawURL := "https://s3.us-east-1.amazonaws.com/bucket.with.dots/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestS3StorageKeyFromURL_LegacyBucketOnlyHostStillRoundTrips(t *testing.T) {
	// Old records written before the suffix bug was fixed look like
	// "https://<bucket>/<key>". They were broken at fetch time but were still
	// stored, so KeyFromURL must continue to recognise that prefix when we
	// migrate or delete those records.
	s := &S3Storage{
		bucket: "test-bucket",
		region: "us-east-1",
	}

	rawURL := "https://test-bucket/uploads/abc/file.png"

	if got := s.KeyFromURL(rawURL); got != "uploads/abc/file.png" {
		t.Fatalf("KeyFromURL(%q) = %q, want %q", rawURL, got, "uploads/abc/file.png")
	}
}

func TestLooksLikeS3Hostname(t *testing.T) {
	cases := []struct {
		bucket string
		want   bool
	}{
		{"my-bucket", false},
		{"bucket.with.dots", false},
		{"my-bucket.s3.us-east-1.amazonaws.com", true},
		{"my-bucket.s3.amazonaws.com", true},
		{"s3.us-east-1.amazonaws.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.bucket, func(t *testing.T) {
			if got := looksLikeS3Hostname(tc.bucket); got != tc.want {
				t.Fatalf("looksLikeS3Hostname(%q) = %v, want %v", tc.bucket, got, tc.want)
			}
		})
	}
}

func TestNormalizeS3KeyPrefix(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"/":                    "",
		"attachments":          "attachments",
		"/attachments/dev/":    "attachments/dev",
		"  attachments/dev  ":  "attachments/dev",
		"//attachments//dev//": "attachments//dev",
		".":                    "",
	}
	for in, want := range cases {
		if got := normalizeS3KeyPrefix(in); got != want {
			t.Fatalf("normalizeS3KeyPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseS3ForcePathStyle(t *testing.T) {
	cases := map[string]bool{
		"":        true,
		"true":    true,
		"1":       true,
		"yes":     true,
		"on":      true,
		"false":   false,
		"0":       false,
		"no":      false,
		"off":     false,
		" FALSE ": false,
		"invalid": true,
	}
	for in, want := range cases {
		if got := parseS3ForcePathStyle(in); got != want {
			t.Fatalf("parseS3ForcePathStyle(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestS3StorageUploadedURL(t *testing.T) {
	const key = "uploads/abc/file.png"

	cases := []struct {
		name           string
		bucket         string
		region         string
		cdnDomain      string
		endpointURL    string
		forcePathStyle bool
		want           string
	}{
		{
			name:   "default aws virtual hosted style",
			bucket: "test-bucket",
			region: "us-east-1",
			want:   "https://test-bucket.s3.us-east-1.amazonaws.com/uploads/abc/file.png",
		},
		{
			name:   "default aws path style when bucket contains dots",
			bucket: "bucket.with.dots",
			region: "us-east-1",
			want:   "https://s3.us-east-1.amazonaws.com/bucket.with.dots/uploads/abc/file.png",
		},
		{
			name:      "cdn only",
			bucket:    "test-bucket",
			region:    "us-east-1",
			cdnDomain: "cdn.example.com",
			want:      "https://cdn.example.com/uploads/abc/file.png",
		},
		{
			name:           "endpoint only defaults to path style",
			bucket:         "test-bucket",
			region:         "us-east-1",
			endpointURL:    "http://localhost:9000",
			forcePathStyle: true,
			want:           "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:           "endpoint with trailing slash",
			bucket:         "test-bucket",
			region:         "us-east-1",
			endpointURL:    "http://localhost:9000/",
			forcePathStyle: true,
			want:           "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:           "endpoint virtual hosted style",
			bucket:         "test-bucket",
			region:         "cn-east-3",
			endpointURL:    "https://obs.cn-east-3.myhuaweicloud.com",
			forcePathStyle: false,
			want:           "https://test-bucket.obs.cn-east-3.myhuaweicloud.com/uploads/abc/file.png",
		},
		{
			name:        "endpoint and cdn both set prefers cdn",
			bucket:      "test-bucket",
			region:      "us-east-1",
			cdnDomain:   "cdn.example.com",
			endpointURL: "http://localhost:9000",
			want:        "https://cdn.example.com/uploads/abc/file.png",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &S3Storage{
				bucket:         tc.bucket,
				region:         tc.region,
				cdnDomain:      tc.cdnDomain,
				endpointURL:    tc.endpointURL,
				forcePathStyle: tc.forcePathStyle,
			}
			if got := s.uploadedURL(key); got != tc.want {
				t.Fatalf("uploadedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestS3StoragePublicURL(t *testing.T) {
	s := &S3Storage{
		bucket:    "test-bucket",
		region:    "us-east-1",
		cdnDomain: "cdn.example.com",
	}
	if got := s.PublicURL("workspaces/ws/file.txt"); got != "https://cdn.example.com/workspaces/ws/file.txt" {
		t.Fatalf("PublicURL() = %q", got)
	}
}

func TestS3StoragePublicURLWithKeyPrefix(t *testing.T) {
	s := &S3Storage{
		bucket:    "test-bucket",
		region:    "us-east-1",
		cdnDomain: "cdn.example.com",
		keyPrefix: "multica/dev",
	}
	key := s.KeyPrefix() + "/workspaces/ws/file.txt"
	if got := s.PublicURL(key); got != "https://cdn.example.com/multica/dev/workspaces/ws/file.txt" {
		t.Fatalf("PublicURL() = %q", got)
	}
}

func TestS3StorageOBSURLRoundTripWithBucketDomain(t *testing.T) {
	s := &S3Storage{
		bucket:      "test-bucket",
		region:      "cn-east-3",
		cdnDomain:   "test-bucket.obs.cn-east-3.myhuaweicloud.com",
		endpointURL: "https://obs.cn-east-3.myhuaweicloud.com",
	}
	key := "workspaces/ws-1/file.zip"
	rawURL := s.uploadedURL(key)
	if got := s.KeyFromURL(rawURL); got != key {
		t.Fatalf("KeyFromURL(uploadedURL(%q)) = %q, want %q", key, got, key)
	}
}

func TestS3StorageOBSURLRoundTripWithVirtualHostedEndpoint(t *testing.T) {
	s := &S3Storage{
		bucket:         "test-bucket",
		region:         "cn-east-3",
		endpointURL:    "https://obs.cn-east-3.myhuaweicloud.com",
		forcePathStyle: false,
	}
	key := "workspaces/ws-1/file.zip"
	rawURL := s.uploadedURL(key)
	if rawURL != "https://test-bucket.obs.cn-east-3.myhuaweicloud.com/workspaces/ws-1/file.zip" {
		t.Fatalf("uploadedURL(%q) = %q", key, rawURL)
	}
	if got := s.KeyFromURL(rawURL); got != key {
		t.Fatalf("KeyFromURL(uploadedURL(%q)) = %q, want %q", key, got, key)
	}
}

func TestS3StoragePresignedPutURLWithVirtualHostedEndpoint(t *testing.T) {
	cfg := aws.Config{
		Region:      "cn-east-3",
		Credentials: credentials.NewStaticCredentialsProvider("test-ak", "test-sk", ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://obs.cn-east-3.myhuaweicloud.com")
		o.UsePathStyle = false
	})
	s := &S3Storage{
		client:         client,
		bucket:         "multica",
		region:         "cn-east-3",
		endpointURL:    "https://obs.cn-east-3.myhuaweicloud.com",
		forcePathStyle: false,
	}

	rawURL, headers, err := s.CreatePresignedPutURL(context.Background(), "storage/workspaces/ws-1/file.md", "text/markdown", "脉率.md", 15*time.Minute)
	if err != nil {
		t.Fatalf("CreatePresignedPutURL() error = %v", err)
	}
	if !strings.HasPrefix(rawURL, "https://multica.obs.cn-east-3.myhuaweicloud.com/storage/workspaces/ws-1/file.md?") {
		t.Fatalf("CreatePresignedPutURL() = %q", rawURL)
	}
	for name, value := range headers {
		for _, r := range value {
			if r > 0x7f {
				t.Fatalf("signed header %q contains non-ASCII value %q", name, value)
			}
		}
	}
	if got := headers["Content-Disposition"]; got != `attachment; filename="__.md"; filename*=UTF-8''%E8%84%89%E7%8E%87.md` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestS3StoragePresignedInlineGetURLOverridesDisposition(t *testing.T) {
	cfg := aws.Config{
		Region:      "cn-east-3",
		Credentials: credentials.NewStaticCredentialsProvider("test-ak", "test-sk", ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://obs.cn-east-3.myhuaweicloud.com")
		o.UsePathStyle = false
	})
	s := &S3Storage{
		client:         client,
		bucket:         "multica",
		region:         "cn-east-3",
		endpointURL:    "https://obs.cn-east-3.myhuaweicloud.com",
		forcePathStyle: false,
	}

	rawURL, err := s.PresignedInlineGetURL(context.Background(), "storage/workspaces/ws-1/manual.pdf", "application/pdf", "manual.pdf", 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignedInlineGetURL() error = %v", err)
	}
	if !strings.HasPrefix(rawURL, "https://multica.obs.cn-east-3.myhuaweicloud.com/storage/workspaces/ws-1/manual.pdf?") {
		t.Fatalf("PresignedInlineGetURL() = %q", rawURL)
	}
	if !strings.Contains(rawURL, "response-content-disposition=inline") {
		t.Fatalf("PresignedInlineGetURL() missing inline disposition override: %q", rawURL)
	}
	if !strings.Contains(rawURL, "response-content-type=application%2Fpdf") {
		t.Fatalf("PresignedInlineGetURL() missing content-type override: %q", rawURL)
	}
}
