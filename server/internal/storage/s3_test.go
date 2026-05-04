package storage

import "testing"

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
