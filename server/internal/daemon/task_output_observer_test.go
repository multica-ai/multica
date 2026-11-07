package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestShouldRetireSilentResume(t *testing.T) {
	t.Parallel()

	long := silentResumeRetireThreshold + time.Second
	short := silentResumeRetireThreshold - time.Second
	cases := []struct {
		name      string
		status    string
		prior     string
		silent    time.Duration
		wasSilent bool
		want      bool
	}{
		// MUL-8188: resumed, cancelled by hand after minutes of nothing.
		{"cancelled silent resume", "cancelled", "ses_broken", long, true, true},
		{"aborted silent resume", "aborted", "ses_broken", long, true, true},
		// An ordinary early cancel keeps the conversation.
		{"early cancel", "cancelled", "ses_ok", short, true, false},
		// Any output proves the session loads and runs.
		{"cancel after output", "cancelled", "ses_ok", long, false, false},
		// A cold run has no resumed session to blame.
		{"cold run", "cancelled", "", long, true, false},
		// Failures/timeouts are classified by their own guards.
		{"failed", "failed", "ses_x", long, true, false},
		{"timeout", "timeout", "ses_x", long, true, false},
		{"idle watchdog", "idle_watchdog", "ses_x", long, true, false},
		{"completed", "completed", "ses_x", long, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRetireSilentResume(tc.status, tc.prior, tc.silent, tc.wasSilent); got != tc.want {
				t.Fatalf("shouldRetireSilentResume(%q, %q, %s, %v) = %v, want %v", tc.status, tc.prior, tc.silent, tc.wasSilent, got, tc.want)
			}
		})
	}
}

// fakeAwakeClock is an awake-time source the test advances by hand. A host
// suspend is simply wall time passing while this clock stands still.
type fakeAwakeClock struct {
	mu  sync.Mutex
	now time.Duration
}

func (c *fakeAwakeClock) read() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeAwakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	c.mu.Unlock()
}

func TestTaskOutputObserver(t *testing.T) {
	t.Parallel()

	var nilObs *taskOutputObserver
	nilObs.begin()
	nilObs.markOutput()
	if _, ok := nilObs.silentFor(); ok {
		t.Fatal("nil observer must report not-silent")
	}

	clock := &fakeAwakeClock{now: time.Hour}
	o := &taskOutputObserver{clock: clock.read}
	if _, ok := o.silentFor(); ok {
		t.Fatal("never-started observer must report not-silent")
	}
	o.begin()
	clock.advance(7 * time.Minute)
	if d, ok := o.silentFor(); !ok || d != 7*time.Minute {
		t.Fatalf("silentFor = %s, %v; want 7m, true", d, ok)
	}
	o.markOutput()
	clock.advance(time.Minute)
	if _, ok := o.silentFor(); ok {
		t.Fatal("observer with output must report not-silent")
	}
	// A new execution (fresh-session retry) resets the observation.
	o.begin()
	clock.advance(time.Minute)
	if _, ok := o.silentFor(); !ok {
		t.Fatal("begin must reset the output flag")
	}

	ctx := withTaskOutputObserver(context.Background(), o)
	if taskOutputObserverFromContext(ctx) != o {
		t.Fatal("observer not recoverable from context")
	}
	if taskOutputObserverFromContext(context.Background()) != nil {
		t.Fatal("empty context must yield a nil observer")
	}
}

// TestTaskOutputObserverIgnoresHostSleep is the suspend regression the review
// asked for: a resume starts, the host sleeps for an hour (wall time passes,
// awake time does not), the user wakes it and cancels within a minute. That
// run was silent for one awake minute, not 61, and must not retire a healthy
// session. Mirrors the idle-watchdog fix (MUL-7851 / #9201).
func TestTaskOutputObserverIgnoresHostSleep(t *testing.T) {
	t.Parallel()

	clock := &fakeAwakeClock{}
	o := &taskOutputObserver{clock: clock.read}
	o.begin()
	clock.advance(30 * time.Second)
	// Host suspended for an hour here: wall time passes, awakeclock does not
	// move, so there is nothing to advance. The user cancels 30s after waking.
	clock.advance(30 * time.Second)

	silent, wasSilent := o.silentFor()
	if !wasSilent || silent != time.Minute {
		t.Fatalf("silentFor = %s, %v; want 1m of awake silence", silent, wasSilent)
	}
	if shouldRetireSilentResume("cancelled", "ses_ok", silent, wasSilent) {
		t.Fatal("a run cancelled one awake minute after resuming must not retire its session, however long the host slept")
	}
}

func TestTaskOutputObserverDefaultsToAwakeClock(t *testing.T) {
	t.Parallel()

	o := &taskOutputObserver{}
	o.begin()
	if d, ok := o.silentFor(); !ok || d < 0 || d > time.Minute {
		t.Fatalf("silentFor on the real awake clock = %s, %v; want a small non-negative duration", d, ok)
	}
}

// TestHandleTask_CancelAckCarriesRetiredSession pins the delivery half of
// MUL-8188: the session runTask retired must reach the server on the
// poll-cancelled path, the only terminal channel a cancelled task has.
func TestHandleTask_CancelAckCarriesRetiredSession(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var ackBody map[string]any
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(body, &ackBody)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			mu.Lock()
			statusCalls++
			first := statusCalls == 1
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if first {
				_, _ = w.Write([]byte(`{"status":"cancelled"}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "kiro"}},
		cancelPollInterval: 10 * time.Millisecond,
	}
	d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		<-runCtx.Done()
		return TaskResult{Status: "aborted", RetiredSessionID: "ses_broken"}, nil
	})

	d.handleTask(context.Background(), Task{
		ID:        "task-retire-ack",
		RuntimeID: "rt-1",
		IssueID:   "issue-retire-ack",
		Agent:     &AgentData{Name: "test-agent"},
	}, 0)

	mu.Lock()
	defer mu.Unlock()
	if ackBody == nil {
		t.Fatal("cancel-ack was never posted")
	}
	if got := ackBody["retired_session_id"]; got != "ses_broken" {
		t.Fatalf("cancel-ack retired_session_id = %v, want ses_broken (body=%v)", got, ackBody)
	}
}
