package webhooks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// mockSource is a Source the tests inject to drive router behaviour
// without depending on the stub adapters' behaviour (which can change
// in PR3).
type mockSource struct {
	name      string
	sigHeader string
	current   string
	previous  string
	normalize func(*http.Request) (*TriggerEvent, error)
}

func (m *mockSource) Name() string                { return m.name }
func (m *mockSource) SignatureHeader() string     { return m.sigHeader }
func (m *mockSource) Secrets() (string, string)   { return m.current, m.previous }
func (m *mockSource) Normalize(r *http.Request) (*TriggerEvent, error) {
	if m.normalize == nil {
		return nil, ErrUnsupportedEvent
	}
	return m.normalize(r)
}

func newTestRouter(t *testing.T, src Source) (*chi.Mux, *Router) {
	t.Helper()
	mux := chi.NewRouter()
	r := NewRouter(nil)
	if src != nil {
		r.Register(src)
	}
	r.Mount(mux)
	return mux, r
}

func TestServe_UnknownSource_Returns404(t *testing.T) {
	mux, _ := newTestRouter(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/unregistered", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServe_HMACBadSignature_Returns401(t *testing.T) {
	src := &mockSource{
		name:      "github",
		sigHeader: "X-Hub-Signature-256",
		current:   "real-secret",
	}
	mux, _ := newTestRouter(t, src)

	body := `{"event":"workflow_run"}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", computeSig("wrong-secret", body))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestServe_HMACMissing_Returns401(t *testing.T) {
	src := &mockSource{
		name:      "github",
		sigHeader: "X-Hub-Signature-256",
		current:   "real-secret",
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	// no signature header at all
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (missing sig)", rec.Code)
	}
}

func TestServe_HMACMalformed_Returns400(t *testing.T) {
	src := &mockSource{
		name:      "github",
		sigHeader: "X-Hub-Signature-256",
		current:   "real-secret",
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	// Garbage in the signature header — wrong algorithm tag.
	req.Header.Set("X-Hub-Signature-256", "md5=00000000")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (malformed)", rec.Code)
	}
}

func TestServe_ValidDispatch_Returns202(t *testing.T) {
	// Mock adapter returns a populated TriggerEvent; verify router
	// passes the body through, computes the right HMAC path, and
	// returns 202 with no body. Also asserts the adapter receives the
	// raw body unmodified (HMAC buffer-replay correctness).
	const secret = "rot13-pretend"
	body := `{"action":"completed","conclusion":"failure"}`

	var seenBody string
	src := &mockSource{
		name:      "github",
		sigHeader: "X-Hub-Signature-256",
		current:   secret,
		normalize: func(r *http.Request) (*TriggerEvent, error) {
			b, _ := io.ReadAll(r.Body)
			seenBody = string(b)
			return &TriggerEvent{
				EventID:   uuid.New(),
				EventType: EventTypeCIFailure,
				PRURL:     "https://github.com/owner/repo/pull/42",
				PRNumber:  42,
				PRTitle:   "[PUL-99] feat(x): y",
				HeadSHA:   "abc123",
				Branch:    "agent-1/pul-99-x",
			}, nil
		},
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", computeSig(secret, body))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if seenBody != body {
		t.Fatalf("adapter saw mangled body:\n  want: %s\n  got:  %s", body, seenBody)
	}
}

func TestServe_NoHMACSource_DispatchesWithoutAuth(t *testing.T) {
	// Sources that opt out of HMAC (e.g. gitlab-style plain token,
	// future jwt-based providers) should still dispatch normally.
	src := &mockSource{
		name: "gitlab",
		// sigHeader empty → opt out of generic HMAC
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return &TriggerEvent{
				EventID:   uuid.New(),
				EventType: EventTypePRMerged,
				PRURL:     "https://gitlab/example/-/merge_requests/1",
				PRNumber:  1,
				PRTitle:   "test",
				HeadSHA:   "deadbeef",
				Branch:    "feat/x",
			}, nil
		},
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
}

func TestServe_SchemaMismatch_Returns400(t *testing.T) {
	src := &mockSource{
		name: "github",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return nil, ErrSchemaMismatch
		},
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestServe_UnsupportedEvent_Returns204(t *testing.T) {
	src := &mockSource{
		name: "github",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return nil, ErrUnsupportedEvent
		},
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestServe_AdapterContractViolation_Returns500(t *testing.T) {
	// nil event + nil error — adapter bug. We deliberately surface
	// this as 500 so the bug gets paged rather than silently
	// dropping events.
	src := &mockSource{
		name: "buggy",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return nil, nil
		},
	}
	mux, _ := newTestRouter(t, src)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/buggy", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestServe_PayloadTooLarge_Returns413(t *testing.T) {
	src := &mockSource{name: "github"}
	mux, _ := newTestRouter(t, src)

	huge := strings.Repeat("A", maxPayloadSize+1)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(huge))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestServe_StampsReceivedAt(t *testing.T) {
	// When the adapter doesn't set ReceivedAt, the router stamps it
	// from the injected clock. Verifies the clock injection works
	// (needed for deterministic logs in PR4 worker tests downstream).
	// The adapter returns a pointer to an event we keep a reference
	// to, then assert the router mutated ReceivedAt after Normalize
	// returned.
	fixed := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	captured := &TriggerEvent{
		EventID:   uuid.New(),
		EventType: EventTypePRMerged,
		PRURL:     "u",
		PRNumber:  1,
		PRTitle:   "[PUL-1] x",
		HeadSHA:   "s",
		Branch:    "b",
	}
	src := &mockSource{
		name: "github",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return captured, nil
		},
	}
	router := NewRouter(nil)
	router.now = func() time.Time { return fixed }
	router.Register(src)

	mux := chi.NewRouter()
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if !captured.ReceivedAt.Equal(fixed) {
		t.Fatalf("router did not stamp ReceivedAt: got %v, want %v", captured.ReceivedAt, fixed)
	}
	if captured.Source != "github" {
		t.Fatalf("router did not stamp Source: got %q, want %q", captured.Source, "github")
	}
}

// fakeStore satisfies EventStore for tests of the persistence path.
type fakeStore struct {
	called   int
	lastEvt  PersistableEvent
	returnErr error
}

func (f *fakeStore) Insert(_ context.Context, e PersistableEvent) error {
	f.called++
	f.lastEvt = e
	return f.returnErr
}

func TestServe_PersistanceSuccess_Returns200(t *testing.T) {
	store := &fakeStore{}
	src := &mockSource{
		name: "github",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return &TriggerEvent{
				EventID:   uuid.New(),
				EventType: EventTypePRMerged,
				PRURL:     "https://github.com/owner/repo/pull/77",
				PRNumber:  77,
				PRTitle:   "[PUL-1] x",
				HeadSHA:   "store-sha",
				Branch:    "agent-1/pul-1",
			}, nil
		},
	}
	mux := chi.NewRouter()
	r := NewRouter(nil).WithStore(store)
	r.Register(src)
	r.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if store.called != 1 {
		t.Fatalf("store.Insert called %d times, want 1", store.called)
	}
	if store.lastEvt.PRNumber != 77 || store.lastEvt.HeadSHA != "store-sha" {
		t.Fatalf("lastEvt mismatch: %+v", store.lastEvt)
	}
}

func TestServe_PersistanceDuplicate_Returns200(t *testing.T) {
	store := &fakeStore{returnErr: ErrAlreadyExists}
	src := &mockSource{
		name: "github",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return &TriggerEvent{
				EventID:   uuid.New(),
				EventType: EventTypePRMerged,
				PRURL:     "u", PRNumber: 1, PRTitle: "t", HeadSHA: "s", Branch: "b",
			}, nil
		},
	}
	mux := chi.NewRouter()
	r := NewRouter(nil).WithStore(store)
	r.Register(src)
	r.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (idempotent re-delivery)", rec.Code)
	}
}

func TestServe_PersistanceFailure_Returns500(t *testing.T) {
	store := &fakeStore{returnErr: errors.New("db down")}
	src := &mockSource{
		name: "github",
		normalize: func(*http.Request) (*TriggerEvent, error) {
			return &TriggerEvent{
				EventID:   uuid.New(),
				EventType: EventTypePRMerged,
				PRURL:     "u", PRNumber: 1, PRTitle: "t", HeadSHA: "s", Branch: "b",
			}, nil
		},
	}
	mux := chi.NewRouter()
	r := NewRouter(nil).WithStore(store)
	r.Register(src)
	r.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (db down)", rec.Code)
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	r := NewRouter(nil)
	r.Register(GitHubStub())

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	r.Register(GitHubStub())
}

func TestRegister_EmptyNamePanics(t *testing.T) {
	r := NewRouter(nil)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty Name")
		}
	}()
	r.Register(&mockSource{name: ""})
}

func TestRegister_HMACSourceWithoutSecretPanics(t *testing.T) {
	r := NewRouter(nil)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when HMAC source has no current secret")
		}
	}()
	r.Register(&mockSource{
		name:      "github",
		sigHeader: "X-Hub-Signature-256",
		// current empty — should panic
	})
}
