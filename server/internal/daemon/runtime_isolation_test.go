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

// TestRunBatchPollerClaimsAcrossRuntimes pins the machine-level cutover
// (MUL-4257): a single batch poller issues one claim across ALL of the daemon's
// runtimes (HTTP fallback here, since no WS is attached) and dispatches each
// returned task to its runtime.
func TestRunBatchPollerClaimsAcrossRuntimes(t *testing.T) {
	t.Parallel()

	var claimCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim") {
			if claimCalls.Add(1) == 1 {
				w.Write([]byte(`{"tasks":[
					{"id":"t1","runtime_id":"rt-1","issue_id":"i1","agent":{"name":"a"}},
					{"id":"t2","runtime_id":"rt-2","issue_id":"i2","agent":{"name":"b"}}
				]}`))
				return
			}
			w.Write([]byte(`{"tasks":[]}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := New(Config{
		ServerBaseURL:      srv.URL,
		HeartbeatInterval:  time.Hour,
		PollInterval:       20 * time.Millisecond,
		MaxConcurrentTasks: 4,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1", "rt-2"}}
	d.cancelPollInterval = time.Hour // no server-side cancellation polling in this test

	var mu sync.Mutex
	dispatched := map[string]int{}
	d.runner = taskRunnerFunc(func(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error) {
		mu.Lock()
		dispatched[task.RuntimeID]++
		mu.Unlock()
		return TaskResult{Status: "completed"}, nil
	})

	sem := newTaskSlotSemaphore(d.cfg.MaxConcurrentTasks)
	var taskWG sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.runBatchPoller(ctx, ctx, sem, make(chan struct{}, 1), &taskWG)

	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		got1, got2 := dispatched["rt-1"], dispatched["rt-2"]
		mu.Unlock()
		if got1 >= 1 && got2 >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("batch poller did not dispatch both runtimes; got rt-1=%d rt-2=%d", got1, got2)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	taskWG.Wait()
}

// TestRunBatchPollerSkipsClaimWhenAtCapacity pins slot-before-claim for the
// batch poller: with no free execution slots it must NOT claim, so tasks never
// pile up server-side `dispatched` and race the dispatch-timeout sweeper.
func TestRunBatchPollerSkipsClaimWhenAtCapacity(t *testing.T) {
	t.Parallel()

	var claimAttempts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			claimAttempts.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[]}`))
	}))
	defer srv.Close()

	d := New(Config{
		ServerBaseURL:      srv.URL,
		HeartbeatInterval:  time.Hour,
		PollInterval:       20 * time.Millisecond,
		MaxConcurrentTasks: 1,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1"}}

	sem := newTaskSlotSemaphore(d.cfg.MaxConcurrentTasks)
	<-sem // hold the only slot for the whole test

	var taskWG sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	go d.runBatchPoller(ctx, ctx, sem, make(chan struct{}, 1), &taskWG)

	time.Sleep(200 * time.Millisecond)
	if got := claimAttempts.Load(); got != 0 {
		t.Fatalf("batch poller claimed %d times while at capacity; want 0", got)
	}
	cancel()
}

// TestPollLoopBatchShutdown pins that pollLoop stops its single batch poller and
// returns promptly on ctx cancel even with a task in flight.
func TestPollLoopBatchShutdown(t *testing.T) {
	t.Parallel()

	releaseRun := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim") {
			w.Write([]byte(`{"tasks":[{"id":"t1","runtime_id":"rt-1","issue_id":"i1","agent":{"name":"a"}}]}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	defer close(releaseRun)

	d := New(Config{
		ServerBaseURL:      srv.URL,
		HeartbeatInterval:  time.Hour,
		PollInterval:       20 * time.Millisecond,
		MaxConcurrentTasks: 1,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1"}}
	d.cancelPollInterval = time.Hour
	d.runner = taskRunnerFunc(func(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error) {
		// Block until the test releases or the run ctx is cancelled by shutdown.
		select {
		case <-releaseRun:
		case <-ctx.Done():
		}
		return TaskResult{Status: "completed"}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	pollDone := make(chan error, 1)
	go func() { pollDone <- d.pollLoop(ctx, nil) }()

	time.Sleep(150 * time.Millisecond) // let it claim + enter the in-flight run
	cancel()
	select {
	case <-pollDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pollLoop did not return within shutdown deadline")
	}
}
