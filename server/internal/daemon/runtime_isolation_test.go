package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRuntimeSetWatcherFanOut pins the multi-subscriber contract: every
// subscribed channel must receive a nudge on each notify, and unsubscribed
// channels must not.
func TestRuntimeSetWatcherFanOut(t *testing.T) {
	t.Parallel()

	w := newRuntimeSetWatcher()
	chA, unsubA := w.Subscribe()
	chB, unsubB := w.Subscribe()
	defer unsubA()
	defer unsubB()

	w.notify()
	for _, ch := range []<-chan struct{}{chA, chB} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("expected each subscriber to receive a nudge")
		}
	}

	// Coalescing: a second notify before the subscriber drains must not
	// block, and the subscriber should still see exactly one pending nudge.
	w.notify()
	w.notify()
	select {
	case <-chA:
	default:
		t.Fatal("expected coalesced nudge to be pending")
	}
	select {
	case <-chA:
		t.Fatal("expected only one coalesced nudge to be queued")
	default:
	}

	// Unsubscribed channels must not get nudges. Drain any in-flight nudge
	// on chB first so we observe only post-unsubscribe behaviour.
	select {
	case <-chB:
	default:
	}
	unsubB()
	w.notify()
	select {
	case <-chB:
		t.Fatal("unsubscribed channel must not receive a nudge")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestRunRuntimePollerIsolatesSlowRuntime is the regression test for
// MUL-1744: a slow ClaimTask for one runtime must not delay claims on any
// other runtime. The serial pre-isolation pollLoop would have blocked the
// fast runtime behind the slow one's HTTP request.
//
// MaxConcurrentTasks is deliberately set to 1 — the strict case GPT-Boy
// flagged on review. Holding the only slot during a slow claim would make
// the fast runtime starve, so this guards both the per-goroutine refactor
// AND the slot-acquired-after-claim invariant.
func TestRunRuntimePollerIsolatesSlowRuntime(t *testing.T) {
	t.Parallel()

	var fastClaims atomic.Int64
	slowEntered := make(chan struct{}, 1)
	releaseSlow := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/runtimes/runtime-slow/tasks/claim"):
			select {
			case slowEntered <- struct{}{}:
			default:
			}
			select {
			case <-releaseSlow:
			case <-r.Context().Done():
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":null}`))
		case strings.HasSuffix(path, "/runtimes/runtime-fast/tasks/claim"):
			fastClaims.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":null}`))
		default:
			http.Error(w, "unexpected path: "+path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	defer close(releaseSlow)

	d := New(Config{
		ServerBaseURL:      srv.URL,
		HeartbeatInterval:  time.Hour, // disable WS-suppression effects
		PollInterval:       50 * time.Millisecond,
		MaxConcurrentTasks: 1,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sem := newTaskSlotSemaphore(d.cfg.MaxConcurrentTasks)
	var taskWG sync.WaitGroup

	slowCtx, slowCancel := context.WithCancel(ctx)
	defer slowCancel()
	go d.runRuntimePoller(slowCtx, ctx, "runtime-slow", sem, make(chan struct{}, 1), &taskWG)

	fastCtx, fastCancel := context.WithCancel(ctx)
	defer fastCancel()
	go d.runRuntimePoller(fastCtx, ctx, "runtime-fast", sem, make(chan struct{}, 1), &taskWG)

	// Wait for the slow handler to actually enter (so we know its claim is
	// in flight) before checking fast-runtime progress.
	select {
	case <-slowEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow runtime claim never entered server handler")
	}

	// Within a short window, the fast runtime should issue several claims.
	// Pre-isolation, it would be stuck behind the still-blocked slow claim.
	deadline := time.After(2 * time.Second)
	for fastClaims.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("fast runtime made only %d claims while slow runtime blocked; expected ≥3", fastClaims.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestPollLoopShutdownWaitsForPollersBeforeTaskWG is a race-detector
// regression for the WaitGroup misuse GPT-Boy flagged: pollLoop must not
// call taskWG.Wait while a poller goroutine could still execute
// taskWG.Add(1). The supervisor uses a separate pollerWG that this test
// implicitly exercises by running shutdown concurrently with a task being
// dispatched.
func TestPollLoopShutdownWaitsForPollersBeforeTaskWG(t *testing.T) {
	t.Parallel()

	taskID := "00000000-0000-0000-0000-000000000001"
	releaseClaim := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(path, "/tasks/claim"):
			// Block until the test releases. When released, return a real task
			// so the poller proceeds into the slot/dispatch path — exactly the
			// window where taskWG.Add(1) races with shutdown's taskWG.Wait.
			select {
			case <-releaseClaim:
			case <-r.Context().Done():
				w.Write([]byte(`{"task":null}`))
				return
			}
			w.Write([]byte(`{"task":{"id":"` + taskID + `","runtime_id":"runtime-1","issue_id":"issue-1","agent":{"name":"test"}}}`))
		case strings.HasSuffix(path, "/start"):
			w.Write([]byte(`{}`))
		case strings.HasSuffix(path, "/fail"):
			w.Write([]byte(`{}`))
		case strings.HasSuffix(path, "/complete"):
			w.Write([]byte(`{}`))
		case strings.HasSuffix(path, "/progress"):
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	d := New(Config{
		ServerBaseURL:      srv.URL,
		HeartbeatInterval:  time.Hour,
		PollInterval:       50 * time.Millisecond,
		MaxConcurrentTasks: 1,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	d.workspaces["ws-1"] = &workspaceState{
		workspaceID: "ws-1",
		runtimeIDs:  []string{"runtime-1"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	pollDone := make(chan error, 1)
	go func() {
		pollDone <- d.pollLoop(ctx, nil)
	}()

	// Let the poller enter ClaimTask, then trigger shutdown right as the
	// claim is about to return a task. The race is the window between
	// ClaimTask returning and taskWG.Add(1) executing.
	time.Sleep(100 * time.Millisecond)
	close(releaseClaim)
	cancel()

	select {
	case <-pollDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pollLoop did not return within shutdown deadline")
	}
}

// TestRunRuntimeHeartbeatIsolatesSlowRuntime is the heartbeat-side mirror of
// the poll-isolation test: a slow SendHeartbeat for one runtime must not
// block other runtimes' heartbeats.
func TestRunRuntimeHeartbeatIsolatesSlowRuntime(t *testing.T) {
	t.Parallel()

	var fastBeats atomic.Int64
	slowEntered := make(chan struct{}, 1)
	releaseSlow := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 1024)
		n, _ := r.Body.Read(body)
		payload := string(body[:n])
		switch {
		case strings.Contains(payload, `"runtime-slow"`):
			select {
			case slowEntered <- struct{}{}:
			default:
			}
			select {
			case <-releaseSlow:
			case <-r.Context().Done():
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		case strings.Contains(payload, `"runtime-fast"`):
			fastBeats.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected payload", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	defer close(releaseSlow)

	d := New(Config{
		ServerBaseURL:     srv.URL,
		HeartbeatInterval: 50 * time.Millisecond,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.runRuntimeHeartbeat(ctx, "runtime-slow")
	go d.runRuntimeHeartbeat(ctx, "runtime-fast")

	select {
	case <-slowEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow heartbeat never entered server handler")
	}

	deadline := time.After(2 * time.Second)
	for fastBeats.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("fast runtime sent only %d heartbeats while slow runtime blocked; expected ≥3", fastBeats.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// noopWriter discards log output so the test runner doesn't get noisy.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
