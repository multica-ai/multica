package attachmentserve

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeRangeable_FullRequestReturns200(t *testing.T) {
	body := []byte("hello image bytes")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/x/download", nil)

	ServeRangeable(rec, req, "image/png", "inline", int64(len(body)), time.Time{}, bytes.NewReader(body))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if got := res.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("accept-ranges = %q, want bytes (range support advertised)", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", got)
	}
	got, _ := io.ReadAll(res.Body)
	if !bytes.Equal(got, body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestServeRangeable_RangeRequestReturns206(t *testing.T) {
	body := []byte("0123456789abcdef")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/x/download", nil)
	req.Header.Set("Range", "bytes=4-7")

	ServeRangeable(rec, req, "image/png", "inline", int64(len(body)), time.Time{}, bytes.NewReader(body))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 Partial Content", res.StatusCode)
	}
	got, _ := io.ReadAll(res.Body)
	if string(got) != "4567" {
		t.Fatalf("ranged body = %q, want %q", got, "4567")
	}
}

func TestServeRangeable_LargeObjectStreamsWithoutRange(t *testing.T) {
	// Pretend the object is larger than the buffer cap: it must stream (200,
	// no 206) and never be read fully into memory.
	size := int64(maxBufferBytes) + 1
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/x/download", nil)
	req.Header.Set("Range", "bytes=0-3")

	ServeRangeable(rec, req, "video/mp4", "inline", size, time.Time{}, strings.NewReader("abcd"))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (large object streams, ignores Range)", res.StatusCode)
	}
	got, _ := io.ReadAll(res.Body)
	if string(got) != "abcd" {
		t.Fatalf("streamed body = %q, want %q", got, "abcd")
	}
}
