package traceupload

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gunzipLines decompresses a gzipped body into its JSONL lines.
func gunzipLines(t *testing.T, body []byte) []string {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	var lines []string
	sc := bufio.NewScanner(gr)
	sc.Buffer(make([]byte, 0, 1024), 1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines = append(lines, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return lines
}

// CP1/CP2: the wire body is gzip(meta-line + raw transcript), meta first.
func TestBuildBodyWireFormat(t *testing.T) {
	s := Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s", AgentName: "Mia"}
	transcript := []byte(`{"type":"user","text":"hi"}` + "\n" + `{"type":"assistant","text":"yo"}`)
	body, err := BuildBody(s, transcript)
	if err != nil {
		t.Fatal(err)
	}
	lines := gunzipLines(t, body)
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (meta + 2 events), got %d: %v", len(lines), lines)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &meta); err != nil {
		t.Fatalf("meta line not json: %v", err)
	}
	if meta["type"] != "trace_meta" || meta["runtime"] != "claude" || meta["task_id"] != "t" {
		t.Errorf("bad meta line: %v", meta)
	}
	if !strings.Contains(lines[1], `"user"`) || !strings.Contains(lines[2], `"assistant"`) {
		t.Errorf("transcript lines not preserved: %v", lines[1:])
	}
}

func TestHTTPUploaderSuccess(t *testing.T) {
	var gotKey, gotEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotEncoding = r.Header.Get("Content-Encoding")
		// Body must be valid gzip with a trace_meta first line. The server
		// reads it as raw bytes (manual gunzip), so the client must NOT have
		// declared Content-Encoding (would risk transparent decompression).
		raw, _ := io.ReadAll(r.Body)
		lines := gunzipLines(t, raw)
		if len(lines) == 0 || !strings.Contains(lines[0], "trace_meta") {
			t.Errorf("server got malformed body: %v", lines)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ingested","event_count":2}`))
	}))
	defer srv.Close()

	u := &HTTPUploader{Endpoint: srv.URL, APIKey: "secret-key", Client: srv.Client()}
	res, err := u.Upload(context.Background(), Sidecar{Runtime: "codex", TaskID: "t", SessionID: "s"}, []byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if res.Status != "ingested" || res.EventCount != 2 {
		t.Errorf("unexpected result: %+v", res)
	}
	if gotKey != "secret-key" {
		t.Errorf("x-api-key not sent, got %q", gotKey)
	}
	if gotEncoding != "" {
		t.Errorf("Content-Encoding=%q want empty (server gunzips manually)", gotEncoding)
	}
}

func TestHTTPUploaderRetryableVsTerminal(t *testing.T) {
	cases := []struct {
		code      int
		retryable bool
	}{
		{401, false}, // auth — terminal at the HTTP layer
		{400, false}, // malformed
		{413, false}, // too large
		{429, true},  // rate limited
		{500, true},  // server error
		{503, true},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.code)
		}))
		u := &HTTPUploader{Endpoint: srv.URL, APIKey: "k", Client: srv.Client()}
		_, err := u.Upload(context.Background(), Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s"}, []byte(`{}`))
		srv.Close()
		if err == nil {
			t.Errorf("code %d: expected error", c.code)
			continue
		}
		if IsRetryable(err) != c.retryable {
			t.Errorf("code %d: IsRetryable=%v want %v", c.code, IsRetryable(err), c.retryable)
		}
	}
}

// A network failure (server down) is retryable.
func TestHTTPUploaderNetworkErrorRetryable(t *testing.T) {
	u := &HTTPUploader{Endpoint: "http://127.0.0.1:0/nope", APIKey: "k", Client: &http.Client{}}
	_, err := u.Upload(context.Background(), Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s"}, []byte(`{}`))
	if err == nil || !IsRetryable(err) {
		t.Fatalf("expected retryable network error, got %v (retryable=%v)", err, IsRetryable(err))
	}
}
