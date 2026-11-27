package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3StorageUploadStreamUsesFixedLengthRequest(t *testing.T) {
	type observedRequest struct {
		contentLength int64
		decodedLength string
		encoding      string
		transfer      []string
		body          string
	}
	observed := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		observed <- observedRequest{
			contentLength: r.ContentLength,
			decodedLength: r.Header.Get("X-Amz-Decoded-Content-Length"),
			encoding:      r.Header.Get("Content-Encoding"),
			transfer:      append([]string(nil), r.TransferEncoding...),
			body:          string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
	store := &S3Storage{
		client:       client,
		bucket:       "test-bucket",
		region:       "us-east-1",
		endpointURL:  server.URL,
		usePathStyle: true,
	}
	payload := "streamed payload"
	nonSeekable := io.LimitReader(strings.NewReader(payload), int64(len(payload)))
	if _, err := store.UploadStream(context.Background(), "uploads/media.bin", nonSeekable, int64(len(payload)), "application/octet-stream", "media.bin"); err != nil {
		t.Fatalf("UploadStream: %v", err)
	}

	got := <-observed
	if got.contentLength != int64(len(payload)) || len(got.transfer) != 0 {
		t.Fatalf("request is not a fixed-length upload: content_length=%d decoded_length=%q encoding=%q transfer=%v",
			got.contentLength, got.decodedLength, got.encoding, got.transfer)
	}
	if got.body != payload {
		t.Fatalf("request body = %q, want %q", got.body, payload)
	}
}

func TestS3StorageUploadStreamRejectsUnknownLength(t *testing.T) {
	store := &S3Storage{}
	if _, err := store.UploadStream(context.Background(), "uploads/media.bin", strings.NewReader("payload"), -1, "application/octet-stream", "media.bin"); err == nil {
		t.Fatal("UploadStream accepted an unknown content length")
	}
}

func TestS3StorageUploadStreamAcceptsKnownEmptyObject(t *testing.T) {
	observed := make(chan int64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("request body length = %d, want 0", len(body))
		}
		observed <- r.ContentLength
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
	store := &S3Storage{
		client:       client,
		bucket:       "test-bucket",
		region:       "us-east-1",
		endpointURL:  server.URL,
		usePathStyle: true,
	}
	if _, err := store.UploadStream(context.Background(), "uploads/empty.bin", strings.NewReader(""), 0, "application/octet-stream", "empty.bin"); err != nil {
		t.Fatalf("UploadStream: %v", err)
	}
	if got := <-observed; got != 0 {
		t.Fatalf("Content-Length = %d, want 0", got)
	}
}

type observedChecksumRequest struct {
	contentSHA256 string
	trailer       string
	checksumCRC32 string
	encoding      string
	body          string
}

// newChecksumRecordingServer records the checksum-relevant wire details of a
// single upload. TLS matters: the SDK only switches to a trailing checksum when
// the request is HTTPS (see the IsHTTPS gate in the checksum middleware), and
// production talks to OSS/S3 over HTTPS. A plain HTTP server would take the
// header-checksum branch instead and never reproduce the trailer.
func newChecksumRecordingServer(t *testing.T, observed chan<- observedChecksumRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		observed <- observedChecksumRequest{
			contentSHA256: r.Header.Get("X-Amz-Content-Sha256"),
			trailer:       r.Header.Get("X-Amz-Trailer"),
			checksumCRC32: r.Header.Get("X-Amz-Checksum-Crc32"),
			encoding:      r.Header.Get("Content-Encoding"),
			body:          string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

// newChecksumTestStore points a store at the recording server. endpointURL is
// the field Upload branches on, so pass "" to exercise the default AWS shape
// and server.URL to exercise an S3-compatible backend.
func newChecksumTestStore(server *httptest.Server, endpointURL string) *S3Storage {
	return &S3Storage{
		client: s3.New(s3.Options{
			Region:       "us-east-1",
			Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
			BaseEndpoint: aws.String(server.URL),
			UsePathStyle: true,
			HTTPClient:   server.Client(),
			// NewS3StorageFromEnv builds its client through
			// config.LoadDefaultConfig, which resolves this to
			// WhenSupported. s3.New leaves it unset, and an unset value
			// emits no checksum at all — so without pinning the production
			// default here the test passes even with the fix removed.
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenSupported,
		}),
		bucket:       "test-bucket",
		region:       "us-east-1",
		endpointURL:  endpointURL,
		usePathStyle: true,
	}
}

func TestS3StorageUploadPathsAvoidChecksumTrailer(t *testing.T) {
	newStore := func(server *httptest.Server) *S3Storage {
		return newChecksumTestStore(server, server.URL)
	}

	// S3-compatible backends such as Aliyun OSS and Tencent COS reject
	// aws-chunked framing outright, so neither upload path may emit a
	// trailing checksum.
	assertNoTrailer := func(t *testing.T, got observedChecksumRequest, payload string) {
		t.Helper()
		if got.contentSHA256 == "STREAMING-UNSIGNED-PAYLOAD-TRAILER" {
			t.Fatalf("x-amz-content-sha256 = %q, want a non-chunked value", got.contentSHA256)
		}
		if got.trailer != "" {
			t.Fatalf("x-amz-trailer = %q, want empty", got.trailer)
		}
		if got.checksumCRC32 != "" {
			t.Fatalf("x-amz-checksum-crc32 = %q, want empty", got.checksumCRC32)
		}
		if strings.Contains(got.encoding, "aws-chunked") {
			t.Fatalf("content-encoding = %q, want no aws-chunked framing", got.encoding)
		}
		if got.body != payload {
			t.Fatalf("request body = %q, want %q", got.body, payload)
		}
	}

	t.Run("buffered upload", func(t *testing.T) {
		observed := make(chan observedChecksumRequest, 1)
		server := newChecksumRecordingServer(t, observed)
		payload := "buffered payload"

		if _, err := newStore(server).Upload(
			context.Background(), "uploads/media.bin", []byte(payload),
			"application/octet-stream", "media.bin",
		); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		assertNoTrailer(t, <-observed, payload)
	})

	t.Run("streaming upload", func(t *testing.T) {
		observed := make(chan observedChecksumRequest, 1)
		server := newChecksumRecordingServer(t, observed)
		payload := "streamed payload"
		nonSeekable := io.LimitReader(strings.NewReader(payload), int64(len(payload)))

		if _, err := newStore(server).UploadStream(
			context.Background(), "uploads/media.bin", nonSeekable, int64(len(payload)),
			"application/octet-stream", "media.bin",
		); err != nil {
			t.Fatalf("UploadStream: %v", err)
		}
		assertNoTrailer(t, <-observed, payload)
	})
}

// usesAWSEndpoint decides whether a request keeps the SDK's checksum trailer,
// so a wrong answer either reintroduces the OSS/COS failure or strips the
// checksum from an AWS bucket that requires one. Both directions have to hold
// on the host label boundary.
func TestS3StorageUsesAWSEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpointURL string
		want        bool
	}{
		{"", true}, // unset: the SDK resolves the default AWS endpoint
		{"https://s3.us-east-1.amazonaws.com", true},
		{"https://S3.US-EAST-1.AMAZONAWS.COM", true},
		{"https://bucket.s3.us-east-1.amazonaws.com:443", true},
		{"https://bucket.s3.amazonaws.com.", true},
		{"https://bucket.vpce-0abc-xyz.s3.us-east-1.vpce.amazonaws.com", true},
		{"https://s3-fips.us-gov-west-1.amazonaws.com", true},
		{"https://s3.dualstack.us-east-1.amazonaws.com", true},
		{"https://s3.cn-north-1.amazonaws.com.cn", true},
		{"s3.us-east-1.amazonaws.com", true}, // scheme-less but still AWS

		{"https://oss-cn-shanghai-internal.aliyuncs.com", false},
		{"https://cos.ap-shanghai.myqcloud.com", false},
		{"https://notamazonaws.com", false},
		{"https://s3.amazonaws.com.example.net", false},
		{"https://minio.internal:9000?probe=amazonaws.com", false},
		{"https://minio.internal:9000/amazonaws.com", false},
		{"oss-cn-shanghai-internal.aliyuncs.com", false},
	} {
		t.Run(tc.endpointURL, func(t *testing.T) {
			s := &S3Storage{endpointURL: tc.endpointURL}
			if got := s.usesAWSEndpoint(); got != tc.want {
				t.Fatalf("usesAWSEndpoint(%q) = %v, want %v", tc.endpointURL, got, tc.want)
			}
		})
	}
}

// The OSS/COS workaround costs the request its client-side checksum, so it
// must not reach AWS: real AWS S3 accepts the aws-chunked trailer, and buckets
// carrying a default Object Lock retention require a checksum to be present.
// The store below is shaped the way NewS3StorageFromEnv leaves it when
// AWS_ENDPOINT_URL is unset; the client is pointed at a local TLS server only
// so the request can be observed.
func TestS3StorageAWSUploadKeepsChecksumTrailer(t *testing.T) {
	for _, endpointURL := range []string{
		"",
		"https://s3.us-east-1.amazonaws.com",
	} {
		t.Run("endpoint="+endpointURL, func(t *testing.T) {
			observed := make(chan observedChecksumRequest, 1)
			server := newChecksumRecordingServer(t, observed)
			payload := "buffered payload"

			if _, err := newChecksumTestStore(server, endpointURL).Upload(
				context.Background(), "uploads/media.bin", []byte(payload),
				"application/octet-stream", "media.bin",
			); err != nil {
				t.Fatalf("Upload: %v", err)
			}

			got := <-observed
			if got.contentSHA256 != "STREAMING-UNSIGNED-PAYLOAD-TRAILER" {
				t.Fatalf("x-amz-content-sha256 = %q, want the SDK default trailer flow", got.contentSHA256)
			}
			if !strings.HasPrefix(got.trailer, "x-amz-checksum-") {
				t.Fatalf("x-amz-trailer = %q, want a checksum trailer", got.trailer)
			}
		})
	}
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

func TestS3StorageKeyFromURL_CustomEndpointVirtualHostedStylePreservesNestedKey(t *testing.T) {
	s := &S3Storage{
		bucket:       "test-bucket",
		endpointURL:  "https://objects.example.com",
		usePathStyle: false,
	}

	rawURL := "https://test-bucket.objects.example.com/uploads/abc/file.png"

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

func TestS3UsePathStyleFromEnv(t *testing.T) {
	t.Run("defaults to false without custom endpoint", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "")
		if got := s3UsePathStyleFromEnv(""); got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want false", got)
		}
	})

	t.Run("defaults to true with custom endpoint", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "")
		if got := s3UsePathStyleFromEnv("https://objects.example.com"); !got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want true", got)
		}
	})

	t.Run("can disable path style for custom endpoint", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "false")
		if got := s3UsePathStyleFromEnv("https://objects.example.com"); got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want false", got)
		}
	})

	t.Run("invalid value keeps default", func(t *testing.T) {
		t.Setenv("S3_USE_PATH_STYLE", "maybe")
		if got := s3UsePathStyleFromEnv("https://objects.example.com"); !got {
			t.Fatalf("s3UsePathStyleFromEnv() = %v, want true", got)
		}
	})
}

func TestNewS3StorageFromEnv_ConfiguresEndpointPathStyle(t *testing.T) {
	t.Run("defaults custom endpoints to path style", func(t *testing.T) {
		t.Setenv("S3_BUCKET", "test-bucket")
		t.Setenv("S3_REGION", "us-east-1")
		t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")
		t.Setenv("AWS_ENDPOINT_URL", "https://objects.example.com")
		t.Setenv("S3_USE_PATH_STYLE", "")

		store := NewS3StorageFromEnv()
		if store == nil {
			t.Fatal("NewS3StorageFromEnv() = nil")
		}
		if !store.usePathStyle {
			t.Fatalf("usePathStyle = false, want true")
		}
		if !store.client.Options().UsePathStyle {
			t.Fatalf("client UsePathStyle = false, want true")
		}
	})

	t.Run("can disable path style for custom endpoints", func(t *testing.T) {
		t.Setenv("S3_BUCKET", "test-bucket")
		t.Setenv("S3_REGION", "us-east-1")
		t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")
		t.Setenv("AWS_ENDPOINT_URL", "https://objects.example.com")
		t.Setenv("S3_USE_PATH_STYLE", "false")

		store := NewS3StorageFromEnv()
		if store == nil {
			t.Fatal("NewS3StorageFromEnv() = nil")
		}
		if store.usePathStyle {
			t.Fatalf("usePathStyle = true, want false")
		}
		if store.client.Options().UsePathStyle {
			t.Fatalf("client UsePathStyle = true, want false")
		}
		if got, want := store.uploadedURL("uploads/file.txt"), "https://test-bucket.objects.example.com/uploads/file.txt"; got != want {
			t.Fatalf("uploadedURL() = %q, want %q", got, want)
		}
	})
}

func TestS3StorageUploadedURL(t *testing.T) {
	const key = "uploads/abc/file.png"

	cases := []struct {
		name         string
		bucket       string
		region       string
		cdnDomain    string
		endpointURL  string
		usePathStyle bool
		want         string
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
			name:         "endpoint path style",
			bucket:       "test-bucket",
			region:       "us-east-1",
			endpointURL:  "http://localhost:9000",
			usePathStyle: true,
			want:         "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:         "endpoint path style with trailing slash",
			bucket:       "test-bucket",
			region:       "us-east-1",
			endpointURL:  "http://localhost:9000/",
			usePathStyle: true,
			want:         "http://localhost:9000/test-bucket/uploads/abc/file.png",
		},
		{
			name:         "endpoint virtual hosted style",
			bucket:       "test-bucket",
			region:       "us-east-1",
			endpointURL:  "https://objects.example.com",
			usePathStyle: false,
			want:         "https://test-bucket.objects.example.com/uploads/abc/file.png",
		},
		{
			name:         "endpoint and cdn both set prefers cdn",
			bucket:       "test-bucket",
			region:       "us-east-1",
			cdnDomain:    "cdn.example.com",
			endpointURL:  "http://localhost:9000",
			usePathStyle: false,
			want:         "https://cdn.example.com/uploads/abc/file.png",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &S3Storage{
				bucket:       tc.bucket,
				region:       tc.region,
				cdnDomain:    tc.cdnDomain,
				endpointURL:  tc.endpointURL,
				usePathStyle: tc.usePathStyle,
			}
			if got := s.uploadedURL(key); got != tc.want {
				t.Fatalf("uploadedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsGCSEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		want     bool
	}{
		{"https://storage.googleapis.com", true},
		{"https://bucket.storage.googleapis.com", true},
		{"https://STORAGE.GOOGLEAPIS.COM:443", true},
		{"https://storage.googleapis.com.", true},
		{"", false},
		{"https://s3.us-east-1.amazonaws.com", false},
		{"https://minio.example.com", false},
		{"https://storage.googleapis.com.attacker.example", false},
		{"https://notstorage.googleapis.com", false},
		{"https://example.com/storage.googleapis.com", false},
		{"https://example.com?endpoint=storage.googleapis.com", false},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			if got := isGCSEndpoint(tc.endpoint); got != tc.want {
				t.Fatalf("isGCSEndpoint(%q) = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

// Keep the constructor's API options, but route requests to a local TLS server.
// Replacing HTTPClient also verifies that signing does not depend on a client wrapper.
func newS3SigningTestStore(t *testing.T, server *httptest.Server, endpoint string) *S3Storage {
	t.Helper()
	t.Setenv("S3_BUCKET", "test-bucket")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
	t.Setenv("S3_USE_PATH_STYLE", "true")
	store := NewS3StorageFromEnv()
	if store == nil {
		t.Fatal("NewS3StorageFromEnv() = nil")
	}
	opts := store.client.Options()
	opts.BaseEndpoint = aws.String(server.URL)
	opts.HTTPClient = server.Client()
	opts.Retryer = retry.NewStandard(func(o *retry.StandardOptions) {
		o.MaxAttempts = 2
		o.MaxBackoff = time.Millisecond
	})
	store.client = s3.New(opts)
	return store
}

func TestS3StorageGCSRequestSigning(t *testing.T) {
	payload := []byte("attachment contents")
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		endpoint string
		unsigned bool
	}{
		{"GCS", "https://storage.googleapis.com", true},
		{"AWS", "", false},
		{"MinIO", "https://minio.example.com", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type observedRequest struct {
				header http.Header
				body   []byte
			}
			observed := make(chan observedRequest, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				observed <- observedRequest{header: r.Header.Clone(), body: body}
				if r.Method == http.MethodGet {
					w.Header().Set("Content-Encoding", "gzip")
					_, _ = w.Write(compressed.Bytes())
				} else if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			t.Cleanup(server.Close)
			store := newS3SigningTestStore(t, server, tc.endpoint)

			for _, operation := range []struct {
				name string
				run  func(*testing.T)
			}{
				{"upload", func(t *testing.T) {
					if _, err := store.Upload(context.Background(), "file", payload, "application/octet-stream", "file"); err != nil {
						t.Fatal(err)
					}
				}},
				{"streaming upload", func(t *testing.T) {
					stream := io.LimitReader(bytes.NewReader(payload), int64(len(payload)))
					if _, err := store.UploadStream(context.Background(), "file", stream, int64(len(payload)), "application/octet-stream", "file"); err != nil {
						t.Fatal(err)
					}
				}},
				{"download", func(t *testing.T) {
					reader, err := store.GetReader(context.Background(), "file")
					if err != nil {
						t.Fatal(err)
					}
					defer reader.Close()
					got, err := io.ReadAll(reader)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, compressed.Bytes()) {
						t.Fatal("download changed gzip-encoded object bytes")
					}
				}},
				{"delete", func(t *testing.T) {
					if err := store.DeleteObject(context.Background(), "file"); err != nil {
						t.Fatal(err)
					}
				}},
			} {
				if !t.Run(operation.name, func(t *testing.T) {
					operation.run(t)
					got := <-observed
					auth := got.header.Get("Authorization")
					if auth == "" || !strings.Contains(auth, "x-amz-date") || !strings.Contains(auth, "x-amz-content-sha256") {
						t.Fatalf("required headers are not signed: %s", auth)
					}
					if signed := strings.Contains(auth, "accept-encoding"); signed == tc.unsigned {
						t.Fatalf("accept-encoding signed = %v, want %v", signed, !tc.unsigned)
					}
					if encoding := got.header.Get("Accept-Encoding"); encoding != "identity" {
						t.Fatalf("Accept-Encoding = %q, want identity", encoding)
					}
					if operation.name == "upload" || operation.name == "streaming upload" {
						if !strings.Contains(auth, "content-type") {
							t.Fatalf("content-type is not signed: %s", auth)
						}
						if tc.unsigned && !bytes.Equal(got.body, payload) {
							t.Fatal("GCS upload changed object bytes")
						}
					}
				}) {
					break
				}
			}
		})
	}
}

func TestS3StorageGCSOnlyExcludesAcceptEncoding(t *testing.T) {
	t.Setenv("S3_UNSIGNED_HEADERS", "content-type,x-amz-content-sha256")
	observed := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		observed <- r.Header.Get("Authorization")
	}))
	t.Cleanup(server.Close)
	store := newS3SigningTestStore(t, server, "https://storage.googleapis.com")
	if _, err := store.Upload(context.Background(), "file", []byte("payload"), "text/plain", "file.txt"); err != nil {
		t.Fatal(err)
	}
	auth := <-observed
	for _, header := range []string{"content-type", "x-amz-content-sha256"} {
		if !strings.Contains(auth, header) {
			t.Errorf("environment variable removed %s from signature: %s", header, auth)
		}
	}
}

func TestS3StorageGCSPresign(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("presigning must not send an HTTP request")
	}))
	t.Cleanup(server.Close)
	store := newS3SigningTestStore(t, server, "https://storage.googleapis.com")
	signedURL, err := store.PresignGet(context.Background(), "file", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signedURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("X-Amz-SignedHeaders"); got != "host" {
		t.Fatalf("X-Amz-SignedHeaders = %q, want host", got)
	}
	if u.Query().Get("X-Amz-Signature") == "" {
		t.Fatal("missing presigned signature")
	}
}

func TestS3StorageGCSRetries(t *testing.T) {
	observed := make(chan http.Header, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		observed <- r.Header.Clone()
		if len(observed) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	store := newS3SigningTestStore(t, server, "https://storage.googleapis.com")
	if _, err := store.Upload(context.Background(), "file", []byte("payload"), "text/plain", "file.txt"); err != nil {
		t.Fatal(err)
	}
	if got := len(observed); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
	for range 2 {
		header := <-observed
		if auth := header.Get("Authorization"); auth == "" || strings.Contains(auth, "accept-encoding") {
			t.Fatalf("invalid retry signature: %s", auth)
		}
		if got := header.Get("Accept-Encoding"); got != "identity" {
			t.Fatalf("Accept-Encoding = %q, want identity", got)
		}
	}
}

func TestS3StorageGCSConcurrentRequests(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		if auth := r.Header.Get("Authorization"); auth == "" || strings.Contains(auth, "accept-encoding") {
			t.Errorf("invalid concurrent request signature: %s", auth)
		}
	}))
	t.Cleanup(server.Close)
	store := newS3SigningTestStore(t, server, "https://storage.googleapis.com")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			reader, err := store.GetReader(context.Background(), "file")
			if err != nil {
				t.Error(err)
				return
			}
			_ = reader.Close()
		})
	}
	wg.Wait()
}
