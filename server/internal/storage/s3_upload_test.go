package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeObjectStore is a minimal S3-compatible server: enough to prove how the
// SDK calls it, not enough to be S3. It exists to capture the REQUESTS the
// finalizer sends, because those headers are the entire anti-substitution
// guarantee — an implementation that dropped If-Match would still pass a test
// that only checked the resulting bytes.
type fakeObjectStore struct {
	mu       sync.Mutex
	objects  map[string]fakeStoredObject
	requests []fakeCapturedRequest
	// copyStatus overrides the CopyObject reply, so the precondition and
	// not-found translations can be exercised.
	copyStatus int
	// headStatus overrides HEAD, for the missing-object case.
	headStatus int
}

type fakeStoredObject struct {
	body        []byte
	contentType string
	etag        string
}

type fakeCapturedRequest struct {
	method        string
	path          string
	ifMatch       string
	metadataDir   string
	contentType   string
	contentRange  string
	contentLength int64
	// getIfMatch is the If-Match a GetObject carried, which is how the probe
	// read's version condition is observed.
	getIfMatch string
}

func newFakeObjectStore(t *testing.T) (*fakeObjectStore, *S3Storage) {
	t.Helper()
	store := &fakeObjectStore{objects: map[string]fakeStoredObject{}}
	server := httptest.NewServer(http.HandlerFunc(store.handle))
	t.Cleanup(server.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
	return store, &S3Storage{
		client:       client,
		bucket:       "test-bucket",
		region:       "us-east-1",
		endpointURL:  server.URL,
		usePathStyle: true,
	}
}

func (s *fakeObjectStore) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, fakeCapturedRequest{
		method:        r.Method,
		path:          r.URL.Path,
		ifMatch:       r.Header.Get("x-amz-copy-source-if-match"),
		metadataDir:   r.Header.Get("x-amz-metadata-directive"),
		contentType:   r.Header.Get("Content-Type"),
		contentRange:  r.Header.Get("Range"),
		contentLength: r.ContentLength,
		getIfMatch:    r.Header.Get("If-Match"),
	})
	objects := s.objects
	copyStatus := s.copyStatus
	headStatus := s.headStatus
	s.mu.Unlock()

	// Path style: /<bucket>/<key...>
	key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")

	// A CopyObject is a PUT carrying x-amz-copy-source, so it must be
	// distinguished BEFORE the plain-upload branch.
	if r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "" {
		if copyStatus != 0 {
			writeS3Error(w, copyStatus)
			return
		}
		// The SDK sends "<bucket>/<key>" (no leading slash) unless the caller
		// escaped it; accept both so the fake is not the thing under test.
		source := strings.TrimPrefix(r.Header.Get("x-amz-copy-source"), "/")
		source = strings.TrimPrefix(source, "test-bucket/")
		s.mu.Lock()
		sourceObject, ok := objects[source]
		s.mu.Unlock()
		if !ok {
			writeS3Error(w, http.StatusNotFound)
			return
		}
		if required := r.Header.Get("x-amz-copy-source-if-match"); required != "" &&
			strings.Trim(required, `"`) != strings.Trim(sourceObject.etag, `"`) {
			writeS3Error(w, http.StatusPreconditionFailed)
			return
		}
		// MetadataDirective=REPLACE means the reply's Content-Type wins.
		s.mu.Lock()
		objects[key] = fakeStoredObject{body: sourceObject.body, contentType: r.Header.Get("Content-Type"), etag: sourceObject.etag}
		s.mu.Unlock()
		_, _ = io.WriteString(w, `<CopyObjectResult><ETag>`+sourceObject.etag+`</ETag></CopyObjectResult>`)
		return
	}

	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		stored := fakeStoredObject{body: body, contentType: r.Header.Get("Content-Type"), etag: `"` + fmt.Sprintf("%d", len(body)) + `"`}
		s.mu.Lock()
		objects[key] = stored
		s.mu.Unlock()
		w.Header().Set("ETag", stored.etag)
		w.WriteHeader(http.StatusOK)
	case http.MethodHead:
		if headStatus != 0 {
			w.WriteHeader(headStatus)
			return
		}
		s.mu.Lock()
		object, ok := objects[key]
		s.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", object.etag)
		w.Header().Set("Content-Type", object.contentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(object.body)))
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		s.mu.Lock()
		object, ok := objects[key]
		s.mu.Unlock()
		if !ok {
			writeS3Error(w, http.StatusNotFound)
			return
		}
		// Enforce the version condition, so the probe read's If-Match is
		// exercised rather than merely recorded.
		if required := r.Header.Get("If-Match"); required != "" &&
			strings.Trim(required, `"`) != strings.Trim(object.etag, `"`) {
			writeS3Error(w, http.StatusPreconditionFailed)
			return
		}
		w.Header().Set("ETag", object.etag)
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			// A real S3-compatible server answers a range against an EMPTY
			// object with 416 InvalidRange. Reproducing that here is what lets
			// the empty-object path be tested without a live server.
			if len(object.body) == 0 {
				writeS3Error(w, http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(object.body)
	case http.MethodDelete:
		s.mu.Lock()
		delete(objects, key)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func writeS3Error(w http.ResponseWriter, status int) {
	code := "InternalError"
	switch status {
	case http.StatusNotFound:
		code = "NoSuchKey"
	case http.StatusPreconditionFailed:
		code = "PreconditionFailed"
	case http.StatusRequestedRangeNotSatisfiable:
		code = "InvalidRange"
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>`+code+`</Code><Message>`+code+`</Message></Error>`)
}

func TestS3PresignPut_BindsContentTypeAndLength(t *testing.T) {
	_, store := newFakeObjectStore(t)

	rawURL, headers, err := store.PresignPut(context.Background(), "upload-staging/ws/session", 10*time.Minute, "application/pdf", 52428800)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("X-Amz-Signature") == "" {
		t.Fatalf("presigned URL has no signature: %s", rawURL)
	}
	if got := query.Get("X-Amz-Expires"); got != "600" {
		t.Fatalf("X-Amz-Expires = %q, want 600", got)
	}
	// The signed header list is what the client must reproduce byte for byte.
	signed := query.Get("X-Amz-SignedHeaders")
	for _, want := range []string{"host", "content-length", "content-type"} {
		if !strings.Contains(signed, want) {
			t.Fatalf("signed headers %q does not include %q", signed, want)
		}
	}
	// Only headers that were actually signed may be handed back.
	if headers["Content-Type"] != "application/pdf" {
		t.Fatalf("Content-Type header = %q", headers["Content-Type"])
	}
	if headers["Content-Length"] != "52428800" {
		t.Fatalf("Content-Length header = %q", headers["Content-Length"])
	}
	if len(headers) != 2 {
		t.Fatalf("presign returned unsigned headers %v", headers)
	}
}

func TestS3PresignPut_ClampsTTLAndRejectsBadInput(t *testing.T) {
	_, store := newFakeObjectStore(t)

	if _, _, err := store.PresignPut(context.Background(), "", time.Minute, "text/plain", 1); err == nil {
		t.Fatal("PresignPut accepted an empty key")
	}
	if _, _, err := store.PresignPut(context.Background(), "k", time.Minute, "text/plain", -1); err == nil {
		t.Fatal("PresignPut accepted a negative content length")
	}

	// A ttl beyond SigV4's seven-day ceiling is clamped rather than signed into
	// a URL the store would reject.
	rawURL, _, err := store.PresignPut(context.Background(), "k", 30*24*time.Hour, "text/plain", 1)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	parsed, _ := url.Parse(rawURL)
	if got := parsed.Query().Get("X-Amz-Expires"); got != fmt.Sprintf("%d", int64(presignPutMaxTTL/time.Second)) {
		t.Fatalf("X-Amz-Expires = %q, want the seven-day clamp", got)
	}
}

// TestS3PresignPut_RefusesZeroLengthCredential covers the defect a real bucket
// exposed: SigV4 cannot express a zero Content-Length in the canonical request,
// so a size-0 PUT is signed with `host` alone. That URL is a credential for
// "any number of bytes" — a 120 MiB object was stored with a credential issued
// for an empty file. The refusal is a capability error, not a validation error,
// so callers route it to the empty-file fallback instead of reporting a bad
// request.
func TestS3PresignPut_RefusesZeroLengthCredential(t *testing.T) {
	remote, store := newFakeObjectStore(t)

	rawURL, headers, err := store.PresignPut(context.Background(), "upload-staging/ws/session", time.Minute, "text/plain", 0)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("PresignPut(size=0) error = %v, want ErrUnsupported", err)
	}
	if rawURL != "" {
		t.Fatalf("PresignPut(size=0) returned a URL: %s", rawURL)
	}
	if headers != nil {
		t.Fatalf("PresignPut(size=0) returned headers: %v", headers)
	}
	// No request may be made: the point is that no credential exists, not that
	// one exists and is discarded.
	remote.mu.Lock()
	requests := len(remote.requests)
	remote.mu.Unlock()
	if requests != 0 {
		t.Fatalf("PresignPut(size=0) still contacted the store (%d requests)", requests)
	}

	// The very next size is a legitimate, length-bound credential — the rule is
	// "no empty PUT", not "no small PUT".
	rawURL, headers, err = store.PresignPut(context.Background(), "upload-staging/ws/session", time.Minute, "application/octet-stream", 1)
	if err != nil {
		t.Fatalf("PresignPut(size=1): %v", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	signed := parsed.Query().Get("X-Amz-SignedHeaders")
	for _, want := range []string{"host", "content-length", "content-type"} {
		if !strings.Contains(signed, want) {
			t.Fatalf("size=1 signed headers %q do not include %q", signed, want)
		}
	}
	if headers["Content-Length"] != "1" {
		t.Fatalf("size=1 Content-Length header = %q, want 1", headers["Content-Length"])
	}
}

func TestS3FinalizeObject_CopiesUnderIfMatchAndReplacesMetadata(t *testing.T) {
	remote, store := newFakeObjectStore(t)

	body := []byte("staged bytes")
	remote.objects["upload-staging/ws/s"] = fakeStoredObject{
		body: body, contentType: "image/png", etag: `"abc123"`,
	}

	err := store.FinalizeObject(context.Background(),
		"upload-staging/ws/s", "abc123", "workspaces/ws/final.txt",
		int64(len(body)), "text/plain; charset=utf-8", `attachment; filename="final.txt"`, "max-age=432000,public")
	if err != nil {
		t.Fatalf("FinalizeObject: %v", err)
	}

	// The copy must carry the precondition, or a racing PUT could substitute
	// different bytes between inspection and copy.
	var sawCopy bool
	remote.mu.Lock()
	for _, captured := range remote.requests {
		if captured.ifMatch == "" {
			continue
		}
		sawCopy = true
		if strings.Trim(captured.ifMatch, `"`) != "abc123" {
			t.Fatalf("If-Match = %q, want the inspected ETag", captured.ifMatch)
		}
		if captured.metadataDir != "REPLACE" {
			t.Fatalf("x-amz-metadata-directive = %q, want REPLACE", captured.metadataDir)
		}
	}
	remote.mu.Unlock()
	if !sawCopy {
		t.Fatal("no conditional copy request was sent")
	}

	final := remote.objects["workspaces/ws/final.txt"]
	if string(final.body) != string(body) {
		t.Fatalf("final body = %q, want %q", final.body, body)
	}
	// The client's declared type must not survive: REPLACE writes the server's.
	if final.contentType != "text/plain; charset=utf-8" {
		t.Fatalf("final content type = %q, want the server's sniffed type", final.contentType)
	}
}

func TestS3FinalizeObject_RequiresETag(t *testing.T) {
	_, store := newFakeObjectStore(t)
	// An unconditional copy is exactly the substitution this path exists to
	// prevent, so it must be impossible to request by omitting the ETag.
	err := store.FinalizeObject(context.Background(), "src", "", "dst", 1, "text/plain", "", "")
	if err == nil || !strings.Contains(err.Error(), "ETag") {
		t.Fatalf("FinalizeObject error = %v, want a missing-ETag refusal", err)
	}
}

func TestS3FinalizeObject_MapsPreconditionAndNotFound(t *testing.T) {
	remote, store := newFakeObjectStore(t)
	remote.objects["upload-staging/ws/s"] = fakeStoredObject{body: []byte("x"), contentType: "text/plain", etag: `"e1"`}

	remote.copyStatus = http.StatusPreconditionFailed
	if err := store.FinalizeObject(context.Background(), "upload-staging/ws/s", "e1", "dst", 1, "text/plain", "", ""); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("error = %v, want ErrSourceChanged", err)
	}

	remote.copyStatus = http.StatusNotFound
	if err := store.FinalizeObject(context.Background(), "upload-staging/ws/s", "e1", "dst", 1, "text/plain", "", ""); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("error = %v, want ErrObjectNotFound", err)
	}
}

func TestS3HeadObjectAndReadPrefix(t *testing.T) {
	remote, store := newFakeObjectStore(t)
	remote.objects["upload-staging/ws/s"] = fakeStoredObject{
		body: []byte("0123456789"), contentType: "text/plain", etag: `"e1"`,
	}

	info, err := store.HeadObject(context.Background(), "upload-staging/ws/s")
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if info.Size != 10 || info.ETag != "e1" || info.ContentType != "text/plain" {
		t.Fatalf("HeadObject = %+v", info)
	}

	// The prefix read is bounded, and asks for it as a Range so the store does
	// not ship the whole object.
	prefix, err := store.ReadPrefix(context.Background(), "upload-staging/ws/s", 4, "")
	if err != nil {
		t.Fatalf("ReadPrefix: %v", err)
	}
	if len(prefix) > 5 {
		t.Fatalf("ReadPrefix returned %d bytes for a 4-byte bound", len(prefix))
	}
	if !strings.HasPrefix(string(prefix), "0123") {
		t.Fatalf("ReadPrefix = %q", prefix)
	}
	remote.mu.Lock()
	var sawRange bool
	for _, captured := range remote.requests {
		if captured.contentRange != "" {
			sawRange = true
		}
	}
	remote.mu.Unlock()
	if !sawRange {
		t.Fatal("ReadPrefix did not use a Range request")
	}

	if _, err := store.HeadObject(context.Background(), "upload-staging/missing"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("HeadObject error = %v, want ErrObjectNotFound", err)
	}
	if _, err := store.ReadPrefix(context.Background(), "upload-staging/missing", 4, ""); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("ReadPrefix error = %v, want ErrObjectNotFound", err)
	}
}

func TestLocalStorageIsNotUploadCapable(t *testing.T) {
	// The API decides between 501 and a working endpoint from this assertion, so
	// it is worth pinning: local storage has no presigned PUT, and pretending
	// otherwise would hand clients URLs that cannot exist.
	var localStorage *LocalStorage
	var asStorage Storage = localStorage
	if _, ok := AsUploadCapable(asStorage); ok {
		t.Fatal("LocalStorage reported presigned-upload capability")
	}
}

func TestCopySourceEscapesSegments(t *testing.T) {
	// S3 reads the header's value as a URL, so an unescaped space or plus in a
	// key silently addresses a different object (or none).
	got := copySource("bucket", "a b/c+d/e.txt")
	if got != "bucket/a%20b/c%2Bd/e.txt" {
		t.Fatalf("copySource = %q", got)
	}
}

// TestS3ReadPrefix_SendsVersionConditionAndMapsPrecondition covers the storage
// half of the "probe and copy describe the same object" guarantee: the read
// must actually carry If-Match, and a failed precondition must surface as
// ErrSourceChanged rather than as bytes of whatever is there now.
func TestS3ReadPrefix_SendsVersionConditionAndMapsPrecondition(t *testing.T) {
	remote, store := newFakeObjectStore(t)
	remote.objects["upload-staging/ws/s"] = fakeStoredObject{
		body: []byte("0123456789"), contentType: "text/plain", etag: `"e1"`,
	}

	// Matching ETag: the request carries the condition and the bytes come back.
	prefix, err := store.ReadPrefix(context.Background(), "upload-staging/ws/s", 4, "e1")
	if err != nil {
		t.Fatalf("ReadPrefix with a matching ETag: %v", err)
	}
	if !strings.HasPrefix(string(prefix), "0123") {
		t.Fatalf("ReadPrefix = %q", prefix)
	}
	remote.mu.Lock()
	var sawIfMatch string
	for _, captured := range remote.requests {
		if captured.method == http.MethodGet && captured.getIfMatch != "" {
			sawIfMatch = captured.getIfMatch
		}
	}
	remote.mu.Unlock()
	if strings.Trim(sawIfMatch, `"`) != "e1" {
		t.Fatalf("probe read sent If-Match %q, want the measured ETag", sawIfMatch)
	}

	// Stale ETag: the store must refuse and the client must see ErrSourceChanged,
	// never the current object's bytes.
	if _, err := store.ReadPrefix(context.Background(), "upload-staging/ws/s", 4, "stale"); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("ReadPrefix with a stale ETag = %v, want ErrSourceChanged", err)
	}
}

// TestS3ReadPrefix_EmptyObjectUsesConditionalWholeRead covers item 3 on the
// storage side. A real S3-compatible server answers a Range request against a
// zero-length object with 416, so a ranged read alone makes every empty upload
// fail completion. The fallback must still carry the version condition.
func TestS3ReadPrefix_EmptyObjectUsesConditionalWholeRead(t *testing.T) {
	remote, store := newFakeObjectStore(t)
	remote.objects["upload-staging/ws/empty"] = fakeStoredObject{
		body: nil, contentType: "text/plain", etag: `"empty1"`,
	}

	prefix, err := store.ReadPrefix(context.Background(), "upload-staging/ws/empty", 1024, "empty1")
	if err != nil {
		t.Fatalf("ReadPrefix on an empty object: %v", err)
	}
	if len(prefix) != 0 {
		t.Fatalf("ReadPrefix on an empty object returned %d bytes", len(prefix))
	}

	// The fallback read must be conditional too, or a racing PUT could be
	// described as the empty version.
	if _, err := store.ReadPrefix(context.Background(), "upload-staging/ws/empty", 1024, "stale"); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unconditional fallback read accepted a stale ETag: %v", err)
	}

	// And the first attempt really did hit the 416 path, so this is exercising
	// the fallback rather than a fake that silently returns empty.
	remote.mu.Lock()
	var gotRanged, gotUnranged bool
	for _, captured := range remote.requests {
		if captured.method != http.MethodGet {
			continue
		}
		if captured.contentRange != "" {
			gotRanged = true
		} else {
			gotUnranged = true
		}
	}
	remote.mu.Unlock()
	if !gotRanged || !gotUnranged {
		t.Fatalf("expected a ranged attempt then an unranged fallback (ranged=%v unranged=%v)", gotRanged, gotUnranged)
	}
}
