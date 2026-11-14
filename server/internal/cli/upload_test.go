package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeDirectUploadServer models the three requests the direct path makes. Each
// handler is a field so a test can change one response without rebuilding the
// routes, and everything the client sent is recorded for assertions on
// credentials and headers.
type fakeDirectUploadServer struct {
	t *testing.T

	mu sync.Mutex
	// presignCalls counts presign requests; presignBody is the last parsed body.
	presignCalls int
	presignBody  map[string]any
	// presignStatus/presignResponse override the presign reply.
	presignStatus   int
	presignResponse string

	// putRequests records what the client sent to the storage host.
	putRequests []recordedPut
	// putStatus overrides the PUT reply.
	putStatus int

	// completeCalls counts complete requests; completeStatus overrides the reply.
	completeCalls  int
	completeStatus int
	// completeAttachment is returned by a successful complete.
	completeAttachment map[string]any

	// fallbackCalls counts legacy multipart uploads that reached the server.
	fallbackCalls int
	fallbackBody  []byte
}

type recordedPut struct {
	body []byte
	// transferEncoding and contentLength together describe how the body was
	// framed, which is what a zero-byte upload gets wrong.
	transferEncoding []string
	contentLength    int64
	contentType      string
	authorization    string
	cookie           string
	clientHeaders    []string
}

func newFakeDirectUploadServer(t *testing.T) (*fakeDirectUploadServer, *httptest.Server) {
	t.Helper()
	fake := &fakeDirectUploadServer{
		t:                  t,
		presignStatus:      http.StatusOK,
		putStatus:          http.StatusOK,
		completeStatus:     http.StatusOK,
		completeAttachment: map[string]any{"id": "att-1", "markdown_url": "/api/attachments/att-1/download", "size_bytes": float64(0)},
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeDirectUploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/attachments/presign-upload":
		f.handlePresign(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/storage/"):
		f.handlePut(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete-upload"):
		f.handleComplete(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
		f.handleFallback(w, r)
	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeDirectUploadServer) handlePresign(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.presignCalls++
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.mu.Unlock()
		f.t.Errorf("decode presign body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.presignBody = body
	status := f.presignStatus
	response := f.presignResponse
	f.mu.Unlock()

	if response != "" {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"attachment_id": "att-1",
		"upload_url":    "http://" + r.Host + "/storage/staging-key?X-Amz-Signature=secret-signature",
		"method":        http.MethodPut,
		"headers":       map[string]string{"Content-Type": "text/plain", "Content-Length": "0"},
		"expires_in":    900,
	})
}

func (f *fakeDirectUploadServer) handlePut(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read PUT body: %v", err)
	}
	f.mu.Lock()
	f.putRequests = append(f.putRequests, recordedPut{
		body:             body,
		transferEncoding: append([]string(nil), r.TransferEncoding...),
		contentLength:    r.ContentLength,
		contentType:      r.Header.Get("Content-Type"),
		authorization:    r.Header.Get("Authorization"),
		cookie:           r.Header.Get("Cookie"),
	})
	status := f.putStatus
	f.mu.Unlock()
	w.WriteHeader(status)
}

func (f *fakeDirectUploadServer) handleComplete(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.completeCalls++
	status := f.completeStatus
	attachment := f.completeAttachment
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(attachment)
}

func (f *fakeDirectUploadServer) handleFallback(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.fallbackCalls++
	f.fallbackBody = body
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":           "legacy-1",
		"url":          "https://cdn.example.com/legacy",
		"markdown_url": "/api/attachments/legacy-1/download",
	})
}

// writeTestFile creates a file of size bytes inside the test's temp dir.
func writeTestFile(t *testing.T, name string, size int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

func sha256Of(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestUploadAttachmentFile_StreamsThroughDirectPath(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	client := NewAPIClient(server.URL, "ws-1", "test-token")
	client.AgentID = "agent-1"

	path := writeTestFile(t, "report.txt", 3<<20) // 3 MiB
	att, err := client.UploadAttachmentFile(context.Background(), path, "issue-1")
	if err != nil {
		t.Fatalf("UploadAttachmentFile: %v", err)
	}
	if att.ID != "att-1" {
		t.Fatalf("attachment id = %q", att.ID)
	}

	if fake.presignCalls != 1 || fake.completeCalls != 1 {
		t.Fatalf("presign/complete calls = %d/%d, want 1/1", fake.presignCalls, fake.completeCalls)
	}
	if fake.fallbackCalls != 0 {
		t.Fatalf("the direct path fell back to multipart on a healthy server")
	}
	// The presign request carries the metadata the server needs and nothing it
	// could use to choose a key.
	if fake.presignBody["filename"] != "report.txt" {
		t.Fatalf("filename = %v", fake.presignBody["filename"])
	}
	if fake.presignBody["size_bytes"] != float64(3<<20) {
		t.Fatalf("size_bytes = %v", fake.presignBody["size_bytes"])
	}
	if fake.presignBody["issue_id"] != "issue-1" {
		t.Fatalf("issue_id = %v", fake.presignBody["issue_id"])
	}
	for _, forbidden := range []string{"key", "object_key", "url", "path"} {
		if _, ok := fake.presignBody[forbidden]; ok {
			t.Fatalf("presign body carried %q; the server generates the key", forbidden)
		}
	}

	// The PUT streamed the exact file with an explicit length, so the signed
	// Content-Length is honored rather than chunked.
	if len(fake.putRequests) != 1 {
		t.Fatalf("PUT count = %d, want 1", len(fake.putRequests))
	}
	put := fake.putRequests[0]
	if put.contentLength != int64(3<<20) {
		t.Fatalf("PUT Content-Length = %d, want %d", put.contentLength, 3<<20)
	}
	if !strings.HasPrefix(put.contentType, "text/plain") {
		t.Fatalf("PUT Content-Type = %q", put.contentType)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read source: %v", err)
	}
	if sha256Of(t, put.body) != sha256Of(t, want) {
		t.Fatalf("PUT body differs from the source file (sent %d bytes, want %d)", len(put.body), len(want))
	}
	// The object-storage request must carry no Multica credential: the storage
	// host is a different origin and must never receive the API token.
	if put.authorization != "" {
		t.Fatalf("PUT leaked an Authorization header: %q", put.authorization)
	}
	if put.cookie != "" {
		t.Fatalf("PUT leaked a Cookie header: %q", put.cookie)
	}
}

func TestUploadAttachmentFile_DoesNotBufferTheFile(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	client := NewAPIClient(server.URL, "ws-1", "test-token")

	// A source that only ever produces bytes when read: if the uploader read the
	// whole thing into memory first, the reader would be drained before the PUT
	// started and this counter would not advance during the request.
	var reads int
	source := UploadSource{
		Path: "generated.bin",
		Size: 8 << 20,
		Open: func() (io.ReadCloser, error) {
			return &countingReader{remaining: 8 << 20, reads: &reads}, nil
		},
	}
	ctx := context.Background()
	if _, err := client.uploadAttachment(ctx, source, DirectUploadTarget{IssueID: "issue-1"}); err != nil {
		t.Fatalf("uploadAttachment: %v", err)
	}
	if reads == 0 {
		t.Fatal("the source was never read")
	}
	if len(fake.putRequests) != 1 || int64(len(fake.putRequests[0].body)) != 8<<20 {
		t.Fatalf("PUT body length = %d, want %d", len(fake.putRequests[0].body), 8<<20)
	}
}

// countingReader yields exactly remaining zero bytes across reads of at most
// 32 KiB, mimicking a 100 MiB file without holding it.
type countingReader struct {
	remaining int
	reads     *int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > 32<<10 {
		n = 32 << 10
	}
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 0
	}
	r.remaining -= n
	*r.reads++
	return n, nil
}

func (r *countingReader) Close() error { return nil }

func TestUploadAttachmentFile_FallsBackOnlyOnCapabilityAbsence(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		wantFallback bool
	}{
		{name: "404 route absent", status: http.StatusNotFound, body: `{"error":"not found"}`, wantFallback: true},
		{name: "405 method absent", status: http.StatusMethodNotAllowed, body: `{"error":"method not allowed"}`, wantFallback: true},
		{
			name:         "501 capability declined",
			status:       http.StatusNotImplemented,
			body:         `{"error":"unsupported","code":"attachment_presign_unsupported"}`,
			wantFallback: true,
		},
		{
			// A bare 501 is not a capability declaration; treating it as one
			// would hide a broken direct path behind a slower upload.
			name:         "501 without the capability code",
			status:       http.StatusNotImplemented,
			body:         `{"error":"something else broke"}`,
			wantFallback: false,
		},
		{name: "401 unauthorized", status: http.StatusUnauthorized, body: `{"error":"unauthenticated"}`, wantFallback: false},
		{name: "403 forbidden", status: http.StatusForbidden, body: `{"error":"forbidden"}`, wantFallback: false},
		{name: "500 server error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantFallback: false},
		{name: "400 validation", status: http.StatusBadRequest, body: `{"error":"bad size"}`, wantFallback: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, server := newFakeDirectUploadServer(t)
			fake.presignStatus = tc.status
			fake.presignResponse = tc.body
			client := NewAPIClient(server.URL, "ws-1", "test-token")
			path := writeTestFile(t, "a.txt", 16)

			_, err := client.UploadAttachmentFile(context.Background(), path, "issue-1")
			if tc.wantFallback {
				if err != nil {
					t.Fatalf("expected a successful fallback, got %v", err)
				}
				if fake.fallbackCalls != 1 {
					t.Fatalf("fallback calls = %d, want 1", fake.fallbackCalls)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error, got a fallback upload")
			}
			if fake.fallbackCalls != 0 {
				t.Fatalf("a %d fell back to the legacy upload path", tc.status)
			}
			// The failure keeps its status for exit-code classification.
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status {
				t.Fatalf("error = %v, want an HTTPError with status %d", err, tc.status)
			}
		})
	}
}

func TestUploadAttachmentFile_RejectedPutDoesNotFallBack(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	fake.putStatus = http.StatusForbidden
	client := NewAPIClient(server.URL, "ws-1", "test-token")
	path := writeTestFile(t, "a.txt", 16)

	_, err := client.UploadAttachmentFile(context.Background(), path, "issue-1")
	if err == nil {
		t.Fatal("expected the PUT rejection to surface")
	}
	if fake.fallbackCalls != 0 {
		t.Fatal("a rejected PUT fell back to the legacy path, which would re-upload the bytes")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "to storage failed") {
		t.Fatalf("error = %v, want it to name the file and the storage step", err)
	}
	// The signed URL must not leak into the error text.
	if strings.Contains(err.Error(), "X-Amz-Signature") || strings.Contains(err.Error(), "secret-signature") {
		t.Fatalf("error leaked the signed URL: %v", err)
	}
}

func TestUploadAttachmentFile_CompleteRetriesAndIsIdempotent(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	// Two server errors, then success: a transient failure must not discard an
	// upload whose bytes are already stored.
	var attempts int
	var mu sync.Mutex
	inner := http.HandlerFunc(fake.ServeHTTP)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete-upload") {
			mu.Lock()
			attempts++
			current := attempts
			mu.Unlock()
			if current < 3 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"error":"transient"}`)
				return
			}
		}
		inner.ServeHTTP(w, r)
	})

	client := NewAPIClient(server.URL, "ws-1", "test-token")
	path := writeTestFile(t, "a.txt", 16)
	att, err := client.UploadAttachmentFile(context.Background(), path, "issue-1")
	if err != nil {
		t.Fatalf("UploadAttachmentFile: %v", err)
	}
	if att.ID != "att-1" {
		t.Fatalf("attachment id = %q", att.ID)
	}
	if attempts != 3 {
		t.Fatalf("complete-upload attempts = %d, want 3", attempts)
	}
	// One PUT: the retry is on complete, not on the transfer.
	if len(fake.putRequests) != 1 {
		t.Fatalf("PUT count = %d, want 1", len(fake.putRequests))
	}
	if fake.fallbackCalls != 0 {
		t.Fatal("a retried completion fell back to the legacy path")
	}
}

func TestUploadAttachmentFile_CompleteClientErrorIsNotRetried(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	fake.completeStatus = http.StatusConflict
	fake.completeAttachment = map[string]any{"error": "object changed", "code": "attachment_upload_object_changed"}
	client := NewAPIClient(server.URL, "ws-1", "test-token")
	path := writeTestFile(t, "a.txt", 16)

	if _, err := client.UploadAttachmentFile(context.Background(), path, "issue-1"); err == nil {
		t.Fatal("expected the 409 to surface")
	}
	if fake.completeCalls != 1 {
		t.Fatalf("complete-upload calls = %d, want 1 (a 4xx is not retryable)", fake.completeCalls)
	}
	if fake.fallbackCalls != 0 {
		t.Fatal("a failed completion fell back to the legacy path")
	}
}

func TestUploadAttachmentFile_CancellationStopsUpload(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	// Hold the PUT open so the cancellation lands mid-transfer.
	release := make(chan struct{})
	inner := http.HandlerFunc(fake.ServeHTTP)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		inner.ServeHTTP(w, r)
	})
	t.Cleanup(func() { close(release) })

	client := NewAPIClient(server.URL, "ws-1", "test-token")
	path := writeTestFile(t, "a.txt", 1<<20)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel once the PUT is in flight.
		for {
			fake.mu.Lock()
			started := len(fake.putRequests) > 0
			fake.mu.Unlock()
			if started {
				cancel()
				return
			}
		}
	}()

	_, err := client.UploadAttachmentFile(ctx, path, "issue-1")
	if err == nil {
		t.Fatal("a cancelled upload reported success")
	}
	if fake.fallbackCalls != 0 {
		t.Fatal("a cancelled upload fell back to the legacy path")
	}
}

func TestUploadAttachmentFile_MissingFileFailsBeforeAnyRequest(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	client := NewAPIClient(server.URL, "ws-1", "test-token")

	if _, err := client.UploadAttachmentFile(context.Background(), filepath.Join(t.TempDir(), "nope.txt"), "issue-1"); err == nil {
		t.Fatal("expected a missing-file error")
	}
	if fake.presignCalls != 0 || fake.fallbackCalls != 0 {
		t.Fatalf("a missing file still made requests: presign=%d fallback=%d", fake.presignCalls, fake.fallbackCalls)
	}
}

func TestNewLocalUploadSource_ReportsSizeAndDetectsReplacement(t *testing.T) {
	path := writeTestFile(t, "a.bin", 1234)
	source, err := NewLocalUploadSource(path)
	if err != nil {
		t.Fatalf("NewLocalUploadSource: %v", err)
	}
	if source.Size != 1234 {
		t.Fatalf("size = %d, want 1234", source.Size)
	}

	if _, err := NewLocalUploadSource(t.TempDir()); err == nil {
		t.Fatal("NewLocalUploadSource accepted a directory")
	}

	// A file that changes size between the preflight and the upload must fail
	// loudly rather than sign a different length than it sends.
	if err := os.WriteFile(path, make([]byte, 99), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	reader, err := source.Open()
	if err == nil {
		reader.Close()
		t.Fatal("Open accepted a file whose size changed")
	}
	if !strings.Contains(err.Error(), "changed size") {
		t.Fatalf("error = %v, want a size-change refusal", err)
	}
}

func TestUploadTransferCeiling_ScalesWithFileSize(t *testing.T) {
	// A fixed 60s budget is what made large uploads fail deterministically; the
	// ceiling has to grow with the file.
	small := uploadTransferCeiling(1 << 10)
	large := uploadTransferCeiling(100 << 20)
	if large <= small {
		t.Fatalf("ceiling did not grow: small=%s large=%s", small, large)
	}
	// 100 MiB at the pessimistic floor is ~27 minutes; well past the old 60s.
	if large < 20*60*1e9 {
		t.Fatalf("100 MiB ceiling = %s, want at least 20 minutes", large)
	}
	if small < TransferCeiling() {
		t.Fatalf("small ceiling %s is below the shared transfer ceiling %s", small, TransferCeiling())
	}
}

// TestNewUploadHTTPClient_LeavesSharedTransportAlone guards the boundary the
// rework moved: the wide ceiling must belong to THIS upload client only. If it
// ever leaked into the shared constructor, every other command's timeout
// behaviour would change silently.
func TestNewUploadHTTPClient_LeavesSharedTransportAlone(t *testing.T) {
	shared := NewStallAwareHTTPClient()
	if shared.Timeout != TransferCeiling() {
		t.Fatalf("shared client timeout = %s, want the shared ceiling %s", shared.Timeout, TransferCeiling())
	}

	upload := newUploadHTTPClient(100 << 20)
	if upload.Timeout <= shared.Timeout {
		t.Fatalf("100 MiB upload client timeout = %s, want wider than the shared %s", upload.Timeout, shared.Timeout)
	}
	// The small-file case must be indistinguishable from the shared client.
	small := newUploadHTTPClient(1 << 10)
	if small.Timeout != shared.Timeout {
		t.Fatalf("small upload client timeout = %s, want the shared %s", small.Timeout, shared.Timeout)
	}
	// The progress guard is what actually detects a dead transfer, and it is
	// unchanged: only the backstop matters.
	if upload.Transport == nil || shared.Transport == nil {
		t.Fatal("clients lost their transport")
	}
}

func TestDetectUploadContentType(t *testing.T) {
	cases := map[string]string{
		"chart.PNG":   "image/png",
		"icon.svg":    "image/svg+xml",
		"data.json":   "application/json",
		"unknown.bin": "application/octet-stream",
		"README.md":   "text/plain; charset=utf-8",
	}
	for name, want := range cases {
		if got := DetectUploadContentType(name); got != want {
			t.Errorf("DetectUploadContentType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSanitizeUploadTransportError_StripsSignedURL(t *testing.T) {
	signed := "https://bucket.s3.amazonaws.com/key?X-Amz-Signature=deadbeef&X-Amz-Credential=AKID"
	// Build the error the way net/http does: a *url.Error wrapping the cause.
	err := &url.Error{Op: "Put", URL: signed, Err: fmt.Errorf("dial tcp: connection refused")}

	sanitized := sanitizeUploadTransportError(err)
	if strings.Contains(sanitized.Error(), "X-Amz-Signature") || strings.Contains(sanitized.Error(), "deadbeef") {
		t.Fatalf("sanitized error still carries the signature: %v", sanitized)
	}
	if !strings.Contains(sanitized.Error(), "connection refused") {
		t.Fatalf("sanitized error lost the cause: %v", sanitized)
	}
	if !strings.Contains(sanitized.Error(), "Put") {
		t.Fatalf("sanitized error lost the operation: %v", sanitized)
	}
}

// TestUploadAttachmentFile_EmptyFileIsNotChunked guards the framing of a
// zero-byte PUT. Handing net/http a reader with ContentLength 0 makes it send
// the body chunked, and a real S3-compatible server rejects that for a
// presigned PUT with "UnexpectedContent: This request does not support
// content" — so an empty file could never be uploaded directly. A fake storage
// server cannot catch this, which is why the assertion is on the wire framing.
func TestUploadAttachmentFile_EmptyFileIsNotChunked(t *testing.T) {
	fake, server := newFakeDirectUploadServer(t)
	client := NewAPIClient(server.URL, "ws-1", "test-token")

	path := writeTestFile(t, "empty.bin", 0)
	if _, err := client.UploadAttachmentFile(context.Background(), path, "issue-1"); err != nil {
		t.Fatalf("UploadAttachmentFile on an empty file: %v", err)
	}
	if len(fake.putRequests) != 1 {
		t.Fatalf("PUT count = %d, want 1", len(fake.putRequests))
	}
	put := fake.putRequests[0]
	if len(put.transferEncoding) != 0 {
		t.Fatalf("empty upload was sent with Transfer-Encoding %v; a presigned PUT must carry an explicit Content-Length",
			put.transferEncoding)
	}
	if put.contentLength != 0 {
		t.Fatalf("Content-Length = %d, want 0", put.contentLength)
	}
	if len(put.body) != 0 {
		t.Fatalf("body carried %d bytes for an empty file", len(put.body))
	}
}
