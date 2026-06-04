package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// mockS3Client implements s3Client for testing.
type mockS3Client struct {
	getObjectFn   func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	deleteObjectFn func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	putObjectFn    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

func (m *mockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(ctx, params, optFns...)
	}
	return nil, errors.New("mockS3Client.GetObject not implemented")
}

func (m *mockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, params, optFns...)
	}
	return nil, errors.New("mockS3Client.DeleteObject not implemented")
}

func (m *mockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putObjectFn != nil {
		return m.putObjectFn(ctx, params, optFns...)
	}
	return nil, errors.New("mockS3Client.PutObject not implemented")
}

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

func TestS3StorageUploadedURL(t *testing.T) {
	const key = "abc/file.png"

	cases := []struct {
		name      string
		bucket    string
		region    string
		cdnDomain string
		want      string
	}{
		{
			name:   "no cdn returns relative path",
			bucket: "test-bucket",
			region: "us-east-1",
			want:   "/uploads/abc/file.png",
		},
		{
			name:      "cdn returns absolute url",
			bucket:    "test-bucket",
			region:    "us-east-1",
			cdnDomain: "cdn.example.com",
			want:      "https://cdn.example.com/abc/file.png",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &S3Storage{
				bucket:    tc.bucket,
				region:    tc.region,
				cdnDomain: tc.cdnDomain,
			}
			if got := s.uploadedURL(key); got != tc.want {
				t.Fatalf("uploadedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// mockGetObjectOutput helper to build GetObjectOutput for tests.
func mockGetObjectOutput(body []byte, opts ...func(*s3.GetObjectOutput)) *s3.GetObjectOutput {
	out := &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: aws.Int64(int64(len(body))),
	}
	for _, opt := range opts {
		opt(out)
	}
	return out
}

func withContentType(t string) func(*s3.GetObjectOutput) { return func(o *s3.GetObjectOutput) { o.ContentType = aws.String(t) } }
func withContentDisposition(d string) func(*s3.GetObjectOutput) {
	return func(o *s3.GetObjectOutput) { o.ContentDisposition = aws.String(d) }
}
func withETag(e string) func(*s3.GetObjectOutput)          { return func(o *s3.GetObjectOutput) { o.ETag = aws.String(e) } }
func withLastModified(l string) func(*s3.GetObjectOutput) {
	return func(o *s3.GetObjectOutput) {
		t, _ := time.Parse(time.RFC1123, l)
		o.LastModified = aws.Time(t)
	}
}

func TestS3Storage_ServeFile_EmptyKey(t *testing.T) {
	s := &S3Storage{bucket: "test-bucket", client: &mockS3Client{}}
	req := httptest.NewRequest(http.MethodGet, "/uploads/", nil)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestS3Storage_ServeFile_KeyWithDotDot(t *testing.T) {
	// S3 keys are flat strings; ".." is a legitimate filename component, not a
	// traversal mechanism. S3 will return NoSuchKey for a key literally named
	// "../etc/passwd", which should surface as 404.
	s := &S3Storage{
		bucket: "test-bucket",
		client: &mockS3Client{
			getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return nil, &types.NoSuchKey{}
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/uploads/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "../etc/passwd")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestS3Storage_ServeFile_HappyPath(t *testing.T) {
	body := []byte("hello world")
	s := &S3Storage{
		bucket: "test-bucket",
		client: &mockS3Client{
			getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return mockGetObjectOutput(body,
					withContentType("text/plain"),
					withContentDisposition(`attachment; filename="test.txt"`),
				), nil
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/uploads/test.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "test.txt")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain")
	}
	if cl := rec.Header().Get("Content-Length"); cl != "11" {
		t.Errorf("Content-Length = %q, want %q", cl, "11")
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="test.txt"` {
		t.Errorf("Content-Disposition = %q, want %q", cd, `attachment; filename="test.txt"`)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want %q", cc, "private, max-age=31536000")
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestS3Storage_ServeFile_GetObjectError(t *testing.T) {
	s := &S3Storage{
		bucket: "test-bucket",
		client: &mockS3Client{
			getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return nil, errors.New("s3 error")
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/uploads/test.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "test.txt")
	// Non-NoSuchKey errors return 502
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestS3Storage_ServeFile_NoSuchKey(t *testing.T) {
	s := &S3Storage{
		bucket: "test-bucket",
		client: &mockS3Client{
			getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return nil, &types.NoSuchKey{}
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/uploads/nonexistent.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "nonexistent.txt")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestS3Storage_ServeFile_NilHeaderFields(t *testing.T) {
	body := []byte("test")
	s := &S3Storage{
		bucket: "test-bucket",
		client: &mockS3Client{
			getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return &s3.GetObjectOutput{
					Body: io.NopCloser(bytes.NewReader(body)),
					// Content-Type, Content-Length, Content-Disposition all nil
				}, nil
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/uploads/test.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "test.txt")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// Nil header fields should not write empty strings
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q, want empty", ct)
	}
}

func TestS3Storage_ServeFile_ConditionalGET(t *testing.T) {
	body := []byte("hello")
	s := &S3Storage{
		bucket: "test-bucket",
		client: &mockS3Client{
			getObjectFn: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return mockGetObjectOutput(body,
					withETag(`"abc123"`),
					withLastModified("Wed, 21 Oct 2025 07:28:00 GMT"),
				), nil
			},
		},
	}

	// Request with If-None-Match matching ETag → 304
	req := httptest.NewRequest(http.MethodGet, "/uploads/test.txt", nil)
	req.Header.Set("If-None-Match", `"abc123"`)
	rec := httptest.NewRecorder()
	s.ServeFile(rec, req, "test.txt")

	if rec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", rec.Code)
	}
	if etag := rec.Header().Get("ETag"); etag != `"abc123"` {
		t.Errorf("ETag = %q, want %q", etag, `"abc123"`)
	}

	// Request without If-None-Match → 200
	req2 := httptest.NewRequest(http.MethodGet, "/uploads/test.txt", nil)
	rec2 := httptest.NewRecorder()
	s.ServeFile(rec2, req2, "test.txt")

	if rec2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec2.Code)
	}
}
