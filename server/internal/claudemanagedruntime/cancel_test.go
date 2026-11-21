package claudemanagedruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCancelSession serves one Claude session and records every request so
// tests can assert that cancellation never archives or deletes it.
type fakeCancelSession struct {
	mu         sync.Mutex
	statuses   []string // returned by successive GETs; the last one repeats
	interrupt  int      // HTTP status for the interrupt POST; 0 means 200
	bodies     []string
	requests   []string
	interrupts int
}

func (f *fakeCancelSession) serve(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/sess_1/events":
			b, _ := io.ReadAll(r.Body)
			f.bodies = append(f.bodies, string(b))
			f.interrupts++
			if f.interrupt != 0 {
				w.WriteHeader(f.interrupt)
				return
			}
			json.NewEncoder(w).Encode(fixture(t, "events_interrupt.json"))
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/sess_1":
			status := f.statuses[0]
			if len(f.statuses) > 1 {
				f.statuses = f.statuses[1:]
			}
			session := fixture(t, "session_get.json")
			session["status"] = status
			json.NewEncoder(w).Encode(session)
		case strings.HasPrefix(r.URL.Path, "/sessions/sess_down"):
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (f *fakeCancelSession) assertNeverArchivedOrDeleted(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.requests {
		if strings.HasPrefix(req, http.MethodDelete) || strings.HasSuffix(req, "/archive") {
			t.Fatalf("cancellation must not archive or delete the session: %s", req)
		}
	}
}

func fastCancelPolling(t *testing.T, wait time.Duration) {
	t.Helper()
	oldWait, oldPoll := cancelConfirmWait, cancelConfirmPoll
	cancelConfirmWait, cancelConfirmPoll = wait, time.Millisecond
	t.Cleanup(func() { cancelConfirmWait, cancelConfirmPoll = oldWait, oldPoll })
}

func TestCancelSendsInterruptAloneAndWaitsForIdle(t *testing.T) {
	fastCancelPolling(t, time.Second)
	f := &fakeCancelSession{statuses: []string{"running", "running", "idle"}}
	s := f.serve(t)
	if err := CancelRemoteSession(context.Background(), s.URL, "anthropic-secret", "sess_1"); err != nil {
		t.Fatal(err)
	}
	if len(f.bodies) != 1 {
		t.Fatalf("interrupts sent: %d", len(f.bodies))
	}
	var body struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || len(body.Events[0]) != 1 || body.Events[0]["type"] != "user.interrupt" {
		t.Fatalf("interrupt must be sent alone: %s", f.bodies[0])
	}
	if len(f.statuses) != 1 {
		t.Fatal("cancel returned before the session became idle")
	}
	f.assertNeverArchivedOrDeleted(t)
}

func TestCancelAcceptsTerminatedSession(t *testing.T) {
	fastCancelPolling(t, time.Second)
	f := &fakeCancelSession{statuses: []string{"terminated"}}
	s := f.serve(t)
	if err := CancelRemoteSession(context.Background(), s.URL, "anthropic-secret", "sess_1"); err != nil {
		t.Fatal(err)
	}
}

func TestCancelGivesUpWhileSessionKeepsRunning(t *testing.T) {
	fastCancelPolling(t, 20*time.Millisecond)
	f := &fakeCancelSession{statuses: []string{"running"}}
	s := f.serve(t)
	if err := CancelRemoteSession(context.Background(), s.URL, "anthropic-secret", "sess_1"); err == nil {
		t.Fatal("unconfirmed interrupt reported as cancelled")
	}
	f.assertNeverArchivedOrDeleted(t)
}

func TestCancelTreatsMissingSessionAsDone(t *testing.T) {
	fastCancelPolling(t, time.Second)
	f := &fakeCancelSession{statuses: []string{"running"}, interrupt: http.StatusNotFound}
	s := f.serve(t)
	if err := CancelRemoteSession(context.Background(), s.URL, "anthropic-secret", "sess_1"); err != nil {
		t.Fatal(err)
	}
	f.assertNeverArchivedOrDeleted(t)
}

func TestFailedInterruptStaysQueuedForRetry(t *testing.T) {
	fastCancelPolling(t, time.Second)
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := &fakeCancelSession{statuses: []string{"running", "running", "idle"}, interrupt: http.StatusServiceUnavailable}
	h.b.anthropic.base = f.serve(t).URL
	h.status = "cancelled"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.b.state.Cleanup) != 1 || h.b.state.Cleanup[0] != "sess_1" {
		t.Fatal("failed interrupt was not kept for retry")
	}
	f.mu.Lock()
	f.interrupt = 0
	f.mu.Unlock()
	h.b.cleanup(context.Background())
	if len(h.b.state.Cleanup) != 0 || f.interrupts != 2 {
		t.Fatalf("retry did not interrupt and confirm: cleanup=%v interrupts=%d", h.b.state.Cleanup, f.interrupts)
	}
	f.assertNeverArchivedOrDeleted(t)
}

func TestCancelSkipsInterruptForStoppedSession(t *testing.T) {
	for _, status := range []string{"idle", "terminated"} {
		t.Run(status, func(t *testing.T) {
			fastCancelPolling(t, time.Second)
			f := &fakeCancelSession{statuses: []string{status}, interrupt: http.StatusBadRequest}
			s := f.serve(t)
			if err := CancelRemoteSession(context.Background(), s.URL, "anthropic-secret", "sess_1"); err != nil {
				t.Fatal(err)
			}
			if f.interrupts != 0 {
				t.Fatalf("interrupted a stopped session %d times", f.interrupts)
			}
		})
	}
}

func TestCancelRejectedInterruptChecksStatus(t *testing.T) {
	for _, tc := range []struct {
		after string
		ok    bool
	}{{"idle", true}, {"terminated", true}, {"running", false}} {
		t.Run(tc.after, func(t *testing.T) {
			fastCancelPolling(t, 20*time.Millisecond)
			f := &fakeCancelSession{statuses: []string{"running", tc.after}, interrupt: http.StatusConflict}
			s := f.serve(t)
			err := CancelRemoteSession(context.Background(), s.URL, "anthropic-secret", "sess_1")
			if (err == nil) != tc.ok {
				t.Fatalf("session %s after a rejected interrupt: err = %v", tc.after, err)
			}
			if f.interrupts != 1 {
				t.Fatalf("interrupts sent: %d", f.interrupts)
			}
		})
	}
}

func TestCleanupContinuesPastFailingEntry(t *testing.T) {
	fastCancelPolling(t, time.Second)
	h := setup(t)
	f := &fakeCancelSession{statuses: []string{"running", "idle"}}
	h.b.anthropic.base = f.serve(t).URL
	h.b.state.Cleanup = []string{"sess_down_1", "sess_1", "sess_down_2"}
	h.b.cleanup(context.Background())
	if got := strings.Join(h.b.state.Cleanup, ","); got != "sess_down_1,sess_down_2" || f.interrupts != 1 {
		t.Fatalf("cleanup = %s, interrupts = %d", got, f.interrupts)
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(j.Cleanup, ","); got != "sess_down_1,sess_down_2" {
		t.Fatalf("persisted cleanup = %s", got)
	}
}
