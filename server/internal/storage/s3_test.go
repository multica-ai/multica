package storage

import (
	"context"
	"net/url"
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

func TestS3StoragePresignGet(t *testing.T) {
	store := &S3Storage{
		client: s3.New(s3.Options{
			Region:      "us-east-1",
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		}),
		bucket: "test-bucket",
	}

	got, err := store.PresignGet(context.Background(), "uploads/abc/file.txt", 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	for _, want := range []string{
		"https://test-bucket.s3.us-east-1.amazonaws.com/uploads/abc/file.txt",
		"X-Amz-Signature=",
		"X-Amz-Expires=300",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("presigned URL %q does not contain %q", got, want)
		}
	}
}

func TestS3StoragePresignGetWithContentDisposition(t *testing.T) {
	store := &S3Storage{
		client: s3.New(s3.Options{
			Region:      "us-east-1",
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		}),
		bucket: "test-bucket",
	}

	got, err := store.PresignGetWithContentDisposition(
		context.Background(),
		"uploads/abc/file.txt",
		5*time.Minute,
		`attachment; filename="report.txt"`,
	)
	if err != nil {
		t.Fatalf("PresignGetWithContentDisposition: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if got := u.Query().Get("response-content-disposition"); got != `attachment; filename="report.txt"` {
		t.Fatalf("response-content-disposition = %q", got)
	}
	if sig := u.Query().Get("X-Amz-Signature"); sig == "" {
		t.Fatalf("missing X-Amz-Signature in %q", got)
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

func TestS3StorageUploadedURL(t *testing.T) {
	const key = "uploads/abc/file.png"

	cases := []struct {
		name        string
		bucket      string
		region      string
		cdnDomain   string
		endpointURL string
		want        string
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
			name:        "endpoint only",
			bucket:      "test-bucket",
			region:      "us-east-1",
			endpointURL: "http://localhost:9000",
			want:        "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:        "endpoint with trailing slash",
			bucket:      "test-bucket",
			region:      "us-east-1",
			endpointURL: "http://localhost:9000/",
			want:        "http://localhost:9000/test-bucket/uploads/abc/file.png",
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
				bucket:      tc.bucket,
				region:      tc.region,
				cdnDomain:   tc.cdnDomain,
				endpointURL: tc.endpointURL,
			}
			if got := s.uploadedURL(key); got != tc.want {
				t.Fatalf("uploadedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
