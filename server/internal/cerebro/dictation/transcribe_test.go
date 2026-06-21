package dictation

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newAudioRequest builds a multipart `file` POST the Transcribe handler accepts.
func newAudioRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "dictation.webm")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("fake-audio-bytes"))
	_ = mw.WriteField("language", "da")
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/transcribe", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{})
	return req.WithContext(ctx)
}

// TestTranscribeRetriesColdStart proves the cold-start fix (Jesper, FIR-1637):
// the hviske engine 5xxs on its first inference after scaling to zero, then
// serves the next request. The handler must retry and return the warm result
// rather than surfacing "failed".
func TestTranscribeRetriesColdStart(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First two attempts mimic a cold container; the third is warm.
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "Internal Server Error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"dette er en test","language":"da"}`)
	}))
	defer upstream.Close()

	handler := New(Options{
		TranscribeURL: upstream.URL,
		Queries:       newMemberQueries(testUserID, testWorkspaceID),
	})

	rec := httptest.NewRecorder()
	handler.Transcribe(rec, newAudioRequest(t, tokenString))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (cold start should be retried, not surfaced)", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("dette er en test")) {
		t.Fatalf("body = %s, want the warm transcript", rec.Body.String())
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("upstream called %d times, want 3 (two cold failures + one warm success)", got)
	}
}

// TestTranscribeDoesNotRetry4xx keeps the retry narrow: a deterministic 4xx
// (bad audio) will never change on retry, so the handler must fail fast and
// hit the upstream exactly once.
func TestTranscribeDoesNotRetry4xx(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer upstream.Close()

	handler := New(Options{
		TranscribeURL: upstream.URL,
		Queries:       newMemberQueries(testUserID, testWorkspaceID),
	})

	rec := httptest.NewRecorder()
	handler.Transcribe(rec, newAudioRequest(t, tokenString))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream called %d times, want 1 (4xx must not retry)", got)
	}
}

// TestWarmupAcceptsAndPingsEngine proves the pre-boot path: warmup answers the
// browser immediately (202) and fires a real clip at the engine so it wakes up
// while the user is still speaking.
func TestWarmupAcceptsAndPingsEngine(t *testing.T) {
	tokenString := signedTestToken(t, testUserID)

	pinged := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case pinged <- body:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"","language":"da"}`)
	}))
	defer upstream.Close()

	handler := New(Options{
		TranscribeURL: upstream.URL,
		Queries:       newMemberQueries(testUserID, testWorkspaceID),
	})

	req := httptest.NewRequest(http.MethodPost, "/warmup", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))

	rec := httptest.NewRecorder()
	handler.Warmup(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (warmup is fire-and-forget)", rec.Code)
	}

	select {
	case body := <-pinged:
		// The warmup clip is a multipart `file` field, never empty.
		if len(body) == 0 {
			t.Fatal("warmup ping had an empty body")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the warmup ping to reach the engine")
	}
}

// TestWarmupRejectsNonMember keeps warmup behind the same membership gate as
// transcribe — it must not let an outsider spin up the GPU.
func TestWarmupRejectsNonMember(t *testing.T) {
	tokenString := signedTestToken(t, otherTestUserID)

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer upstream.Close()

	handler := New(Options{
		TranscribeURL: upstream.URL,
		Queries:       newMemberQueries(testUserID, testWorkspaceID),
	})

	req := httptest.NewRequest(http.MethodPost, "/warmup", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))

	rec := httptest.NewRecorder()
	handler.Warmup(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a non-member", rec.Code)
	}
	// Give any (erroneously spawned) goroutine a moment to hit the upstream.
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("upstream pinged %d times for a non-member, want 0", got)
	}
}
