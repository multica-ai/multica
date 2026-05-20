package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-chi/chi/v5"
)

const sampleManifest = `{"version":"v0.2.32","desktop":{"darwin/arm64":"/api/downloads/multica-desktop-v0.2.32-darwin-arm64.dmg","windows/x64":"/api/downloads/multica-desktop-v0.2.32-windows-x64.exe"}}`

// stubS3 implements the s3GetObjectAPI interface used by DownloadsCache.
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

// newCacheWithStub builds a DownloadsCache wired to an in-process stub,
// bypassing NewDownloadsCache so we don't need real AWS credentials in
// unit tests. Real production wiring goes through NewDownloadsCache; the
// router_test would exercise that.
func newCacheWithStub(t *testing.T, stub *stubS3, ttl time.Duration) *DownloadsCache {
	t.Helper()
	return &DownloadsCache{
		client: stub,
		bucket: "test-bucket",
		prefix: "downloads",
		ttl:    ttl,
		logger: slog.Default(),
	}
}

// jsonObject builds a GetObjectOutput that wraps a JSON body. Mimics what
// the real S3 SDK returns from GetObject on a JSON object.
func jsonObject(body string) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String("application/json"),
	}, nil
}

// ─── DownloadsCache (manifest path) ────────────────────────────────────

func TestDownloadsCache_ServesManifestFromUpstream(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return jsonObject(sampleManifest)
	})
	cache := newCacheWithStub(t, stub, time.Minute)

	got, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != sampleManifest {
		t.Fatalf("body mismatch: got %q want %q", got, sampleManifest)
	}
	if h := stub.Hits(); h != 1 {
		t.Fatalf("upstream hits: got %d want 1", h)
	}
}

func TestDownloadsCache_HitWhileFresh(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return jsonObject(sampleManifest)
	})
	cache := newCacheWithStub(t, stub, time.Minute)

	// Three calls in quick succession should fold into a single upstream
	// fetch — this is the protection against polling fan-out.
	for range 3 {
		if _, err := cache.Get(context.Background()); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if h := stub.Hits(); h != 1 {
		t.Fatalf("upstream hits: got %d want 1", h)
	}
}

func TestDownloadsCache_RefetchAfterTTL(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return jsonObject(sampleManifest)
	})
	// 1 ns TTL → effectively always stale, so we assert each Get
	// triggers a refetch without time.Sleep flakiness.
	cache := newCacheWithStub(t, stub, time.Nanosecond)

	for range 3 {
		if _, err := cache.Get(context.Background()); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if h := stub.Hits(); h != 3 {
		t.Fatalf("upstream hits: got %d want 3", h)
	}
}

func TestDownloadsCache_ServesStaleOnUpstreamError(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return jsonObject(sampleManifest)
	})
	cache := newCacheWithStub(t, stub, time.Nanosecond) // always stale

	// Prime with a good fetch.
	good, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("warmup Get: %v", err)
	}

	// Flip the upstream to error and confirm we still get the old body.
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return nil, errors.New("OSS unreachable")
	})
	stale, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("Get during outage should serve stale, got err: %v", err)
	}
	if string(stale) != string(good) {
		t.Fatalf("expected stale body to equal primed body, got %q", stale)
	}
	if h := stub.Hits(); h < 2 {
		t.Fatalf("expected upstream re-attempt during outage, got %d hits", h)
	}
}

func TestDownloadsCache_BubblesErrorWhenNoCacheYet(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return nil, errors.New("OSS down")
	})
	cache := newCacheWithStub(t, stub, time.Minute)

	if _, err := cache.Get(context.Background()); err == nil {
		t.Fatal("expected error when no cache primed and upstream fails")
	}
}

func TestDownloadsCache_RejectsNonJSON(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		body := "<html>oops not the right object</html>"
		return &s3.GetObjectOutput{
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: aws.Int64(int64(len(body))),
			ContentType:   aws.String("text/html"),
		}, nil
	})
	cache := newCacheWithStub(t, stub, time.Minute)

	if _, err := cache.Get(context.Background()); err == nil {
		t.Fatal("expected error for non-JSON upstream payload")
	}
}

func TestDownloadsCache_EnforcesSizeCap(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		var body bytes.Buffer
		body.WriteString(`{"version":"v0.2.32","junk":"`)
		body.WriteString(strings.Repeat("A", MaxManifestBytes+10))
		body.WriteString(`"}`)
		return &s3.GetObjectOutput{
			Body:          io.NopCloser(&body),
			ContentLength: aws.Int64(int64(body.Len())),
			ContentType:   aws.String("application/json"),
		}, nil
	})
	cache := newCacheWithStub(t, stub, time.Minute)

	if _, err := cache.Get(context.Background()); err == nil {
		t.Fatal("expected error when upstream exceeds size cap")
	}
}

// ─── GetDownloads handler (manifest endpoint) ──────────────────────────

func TestGetDownloads_UnconfiguredReturns503(t *testing.T) {
	h := &Handler{} // Downloads explicitly nil
	req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
	w := httptest.NewRecorder()
	h.GetDownloads(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when DownloadsCache unconfigured, got %d", w.Code)
	}
}

func TestGetDownloads_ServesManifestBody(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return jsonObject(sampleManifest)
	})
	h := &Handler{Downloads: newCacheWithStub(t, stub, time.Minute)}

	req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
	w := httptest.NewRecorder()
	h.GetDownloads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != sampleManifest {
		t.Fatalf("body mismatch: got %q want %q", got, sampleManifest)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
}

func TestGetDownloads_502OnUpstreamFailureWithNoCache(t *testing.T) {
	stub := newStubS3()
	stub.Set("downloads/version.json", func() (*s3.GetObjectOutput, error) {
		return nil, errors.New("OSS down")
	})
	h := &Handler{Downloads: newCacheWithStub(t, stub, time.Minute)}

	req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
	w := httptest.NewRecorder()
	h.GetDownloads(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

// ─── GetDownloadFile handler (binary streaming) ────────────────────────

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
	stub.Set("downloads/multica-desktop-v0.2.32-darwin-arm64.dmg", func() (*s3.GetObjectOutput, error) {
		return &s3.GetObjectOutput{
			Body:               io.NopCloser(strings.NewReader(binaryBody)),
			ContentLength:      aws.Int64(int64(len(binaryBody))),
			ContentType:        aws.String("application/x-apple-diskimage"),
			ContentDisposition: aws.String(`attachment; filename="multica-desktop-v0.2.32-darwin-arm64.dmg"`),
		}, nil
	})
	h := &Handler{Downloads: newCacheWithStub(t, stub, time.Minute)}

	req := chiRequest(http.MethodGet,
		"/api/downloads/multica-desktop-v0.2.32-darwin-arm64.dmg",
		"multica-desktop-v0.2.32-darwin-arm64.dmg")
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
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "multica-desktop-v0.2.32-darwin-arm64.dmg") {
		t.Fatalf("expected Content-Disposition passthrough, got %q", cd)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("expected long-cache header on binary response, got %q", cc)
	}
}

func TestGetDownloadFile_NoSuchKeyIs404(t *testing.T) {
	stub := newStubS3() // no handlers → default NoSuchKey
	h := &Handler{Downloads: newCacheWithStub(t, stub, time.Minute)}

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
	h := &Handler{Downloads: newCacheWithStub(t, stub, time.Minute)}

	req := chiRequest(http.MethodGet, "/api/downloads/whatever.dmg", "whatever.dmg")
	w := httptest.NewRecorder()
	h.GetDownloadFile(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestGetDownloadFile_RejectsPathTraversal(t *testing.T) {
	stub := newStubS3()
	h := &Handler{Downloads: newCacheWithStub(t, stub, time.Minute)}

	for _, bad := range []string{
		"../version.json",
		"foo/bar.dmg",
		"foo\\bar.dmg",
		"",
		".hidden.dmg",
		"file\x00.dmg",
		"中文.dmg",
	} {
		req := chiRequest(http.MethodGet, "/api/downloads/"+bad, bad)
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
		// Real filenames produced by the packaging script.
		{"multica-desktop-v0.2.32-darwin-arm64.dmg", true},
		{"multica-desktop-v0.2.32-windows-x64.exe", true},
		{"multica-desktop-v0.2.32-linux-x64.AppImage", true},
		{"multica-desktop-v0.2.32-mac-arm64.dmg.blockmap", true},
		// Path traversal attempts.
		{"../version.json", false},
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

// ─── NewDownloadsCache constructor validation ──────────────────────────

func TestNewDownloadsCache_RequiresBucket(t *testing.T) {
	_, err := NewDownloadsCache(DownloadsCacheConfig{})
	if err == nil {
		t.Fatal("expected error when Bucket is empty")
	}
}
