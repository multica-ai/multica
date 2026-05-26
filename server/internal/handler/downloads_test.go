package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-chi/chi/v5"
)

// stubS3 implements the s3GetObjectAPI interface used by DownloadsProxy.
// Tests register per-key handlers via Set(); a missing key returns the
// SDK's typed NoSuchKey error (matches how Aliyun OSS S3-compat responds
// to GetObject for an absent key).
type stubS3 struct {
	mu       sync.Mutex
	handlers map[string]func() (*s3.GetObjectOutput, error)
	hits     atomic.Int64
}

func newStubS3() *stubS3 {
	return &stubS3{handlers: map[string]func() (*s3.GetObjectOutput, error){}}
}

func (s *stubS3) Set(key string, h func() (*s3.GetObjectOutput, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[key] = h
}

func (s *stubS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	s.hits.Add(1)
	s.mu.Lock()
	h, ok := s.handlers[aws.ToString(params.Key)]
	s.mu.Unlock()
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	return h()
}

func (s *stubS3) Hits() int64 { return s.hits.Load() }

// newProxyWithStub builds a DownloadsProxy wired to an in-process stub,
// bypassing NewDownloadsProxy so we don't need real AWS credentials in
// unit tests. Real production wiring goes through NewDownloadsProxy; the
// router_test would exercise that.
func newProxyWithStub(t *testing.T, stub *stubS3) *DownloadsProxy {
	t.Helper()
	return &DownloadsProxy{
		client: stub,
		bucket: "test-bucket",
		prefix: "downloads",
		logger: slog.Default(),
	}
}

// ─── GetDownloadFile handler (binary + metadata streaming) ─────────────

// chiRequest wraps a request with the chi route context so URLParam
// works in unit tests without booting a full router.
func chiRequest(method, path, filenameParam string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", filenameParam)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestGetDownloadFile_StreamsBinary(t *testing.T) {
	binaryBody := strings.Repeat("X", 8*1024) // 8 KB stand-in for a dmg
	stub := newStubS3()
	stub.Set("downloads/multica-desktop-v0.2.32-mac-arm64.dmg", func() (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{
			Body:               io.NopCloser(strings.NewReader(binaryBody)),
			ContentLength:      aws.Int64(int64(len(binaryBody))),
			ContentType:        aws.String("application/x-apple-diskimage"),
			ContentDisposition: aws.String(`attachment; filename="multica-desktop-v0.2.32-mac-arm64.dmg"`),
		}, nil
	})
	h := &Handler{Downloads: newProxyWithStub(t, stub)}

	req := chiRequest(http.MethodGet,
		"/api/downloads/multica-desktop-v0.2.32-mac-arm64.dmg",
		"multica-desktop-v0.2.32-mac-arm64.dmg")
	w := httptest.NewRecorder()
	h.GetDownloadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != binaryBody {
		t.Fatalf("body length mismatch: got %d want %d", len(got), len(binaryBody))
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-apple-diskimage" {
		t.Fatalf("expected upstream Content-Type passthrough, got %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "multica-desktop-v0.2.32-mac-arm64.dmg") {
		t.Fatalf("expected Content-Disposition passthrough, got %q", cd)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("expected long-cache header on binary response, got %q", cc)
	}
}

func TestGetDownloadFile_LatestYmlGetsShortCache(t *testing.T) {
	// electron-updater poll target. A short cache is load-bearing — a
	// long one would mean a republished version is invisible to clients
	// for up to a year. Encode that contract.
	ymlBody := "version: 0.2.39\nfiles: []\n"
	stub := newStubS3()
	stub.Set("downloads/latest-mac.yml", func() (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{
			Body:          io.NopCloser(strings.NewReader(ymlBody)),
			ContentLength: aws.Int64(int64(len(ymlBody))),
			ContentType:   aws.String("application/x-yaml"),
		}, nil
	})
	h := &Handler{Downloads: newProxyWithStub(t, stub)}

	req := chiRequest(http.MethodGet, "/api/downloads/latest-mac.yml", "latest-mac.yml")
	w := httptest.NewRecorder()
	h.GetDownloadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	cc := w.Header().Get("Cache-Control")
	if !strings.Contains(cc, "max-age=60") || strings.Contains(cc, "immutable") {
		t.Fatalf("expected short-cache header for latest-*.yml, got %q", cc)
	}
}

func TestGetDownloadFile_NoSuchKeyIs404(t *testing.T) {
	stub := newStubS3() // no handlers → default NoSuchKey
	h := &Handler{Downloads: newProxyWithStub(t, stub)}

	req := chiRequest(http.MethodGet, "/api/downloads/does-not-exist.dmg", "does-not-exist.dmg")
	w := httptest.NewRecorder()
	h.GetDownloadFile(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetDownloadFile_TransportErrorIs502(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/whatever.dmg", func() (*s3.GetObjectOutput, error) {
		// Generic non-typed error stands in for a real network blip
		// (DNS, TCP reset, signature mismatch). Should funnel to 502.
		return nil, fmt.Errorf("connection reset")
	})
	h := &Handler{Downloads: newProxyWithStub(t, stub)}

	req := chiRequest(http.MethodGet, "/api/downloads/whatever.dmg", "whatever.dmg")
	w := httptest.NewRecorder()
	h.GetDownloadFile(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestGetDownloadFile_RejectsPathTraversal(t *testing.T) {
	stub := newStubS3()
	h := &Handler{Downloads: newProxyWithStub(t, stub)}

	for _, bad := range []string{
		"../latest-mac.yml",
		"foo/bar.dmg",
		"foo\\bar.dmg",
		"",
		".hidden.dmg",
		"file\x00.dmg",
		"中文.dmg",
	} {
		// The URL path is irrelevant — GetDownloadFile reads the
		// filename via chi.URLParam, not r.URL — so use a safe
		// placeholder here. Embedding `bad` in the path would panic
		// inside httptest.NewRequest's URL parser for control chars
		// (\x00) and reject non-ASCII before our IsSafeDownloadFilename
		// guard ever runs.
		req := chiRequest(http.MethodGet, "/api/downloads/_", bad)
		w := httptest.NewRecorder()
		h.GetDownloadFile(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %q, got %d", bad, w.Code)
		}
	}
	// Crucial: not a single upstream call fired for any of the
	// rejected names. A bypass here would mean a malicious request
	// could probe arbitrary OSS objects.
	if h := stub.Hits(); h != 0 {
		t.Fatalf("expected 0 upstream hits for rejected names, got %d", h)
	}
}

func TestGetDownloadFile_UnconfiguredReturns503(t *testing.T) {
	h := &Handler{} // Downloads explicitly nil
	req := chiRequest(http.MethodGet, "/api/downloads/whatever.dmg", "whatever.dmg")
	w := httptest.NewRecorder()
	h.GetDownloadFile(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// ─── IsSafeDownloadFilename pure-function unit tests ───────────────────

func TestIsSafeDownloadFilename(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// Real filenames produced by the packaging script + electron-builder.
		{"multica-desktop-v0.2.32-mac-arm64.dmg", true},
		{"multica-desktop-v0.2.32-windows-x64.exe", true},
		{"multica-desktop-v0.2.32-linux-x64.AppImage", true},
		{"latest-mac.yml", true},
		{"latest.yml", true},
		{"latest-arm64.yml", true},
		{"latest-linux.yml", true},
		// Path traversal attempts.
		{"../latest-mac.yml", false},
		{"..", false},
		{"/etc/passwd", false},
		{"foo/bar.dmg", false},
		{"foo\\bar.dmg", false},
		// Hidden-file marker.
		{".env", false},
		// Empty / overlong.
		{"", false},
		{strings.Repeat("a", 256) + ".dmg", false},
		// Control / non-ASCII.
		{"file\n.dmg", false},
		{"中文.dmg", false},
	}
	for _, tc := range cases {
		if got := IsSafeDownloadFilename(tc.name); got != tc.want {
			t.Errorf("IsSafeDownloadFilename(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ─── NewDownloadsProxy constructor validation ──────────────────────────

func TestNewDownloadsProxy_RequiresBucket(t *testing.T) {
	_, err := NewDownloadsProxy(DownloadsProxyConfig{})
	if err == nil {
		t.Fatal("expected error when Bucket is empty")
	}
}
