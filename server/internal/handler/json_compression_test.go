package handler

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteMaybeCompressedJSONHonorsGzip(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/rt/tasks/claim", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	w := httptest.NewRecorder()

	writeMaybeCompressedJSON(w, req, http.StatusOK, map[string]string{
		"payload": strings.Repeat("claim-response-", 128),
	})

	resp := w.Result()
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := resp.Header.Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("new gzip reader: %v", err)
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode compressed json: %v", err)
	}
	if got["payload"] != strings.Repeat("claim-response-", 128) {
		t.Fatalf("payload did not round-trip")
	}
}
