package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testProviderDaemon builds the bare minimum for the provider-pool helpers:
// a logger and a cancel poll interval long enough that the cancellation
// watcher and every other ticker in the wait path never fire during a test.
func testProviderDaemon(t *testing.T) *Daemon {
	t.Helper()
	return &Daemon{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancelPollInterval: time.Hour,
	}
}

func TestProviderLimitEnvVar(t *testing.T) {
	cases := map[string]string{
		"claude": "MULTICA_CLAUDE_MAX_CONCURRENT_TASKS",
		"codex ": "MULTICA_CODEX_MAX_CONCURRENT_TASKS",
		" opencode ": "MULTICA_OPENCODE_MAX_CONCURRENT_TASKS",
	}
	for provider, want := range cases {
		if got := providerLimitEnvVar(provider); got != want {
			t.Errorf("providerLimitEnvVar(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestProviderPoolUnlimited(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_MAX_CONCURRENT_TASKS", "")
	d := testProviderDaemon(t)
	if pool := d.providerPool("claude"); pool != nil {
		t.Fatalf("pool = %v, want nil for an unset variable", pool)
	}
}

func TestProviderPoolInvalid(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-3"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("MULTICA_CODEX_MAX_CONCURRENT_TASKS", bad)
			d := testProviderDaemon(t)
			if pool := d.providerPool("codex"); pool != nil {
				t.Fatalf("pool has capacity %d, want nil for invalid value %q", cap(pool), bad)
			}
		})
	}
}

func TestProviderPoolLimited(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_MAX_CONCURRENT_TASKS", "2")
	d := testProviderDaemon(t)
	pool := d.providerPool("claude")
	if cap(pool) != 2 {
		t.Fatalf("cap = %d, want 2", cap(pool))
	}
	// The pool is memoized per provider: a second lookup must return the same
	// channel, not a fresh one.
	if again := d.providerPool("claude"); cap(again) != 2 || again != pool {
		t.Fatalf("second lookup returned a different pool (cap %d)", cap(again))
	}
}

func TestWaitForProviderCapacityUnlimited(t *testing.T) {
	d := testProviderDaemon(t)
	release, ok := d.waitForProviderCapacity(context.Background(), Task{ID: "t1"}, "claude", d.logger)
	if !ok {
		t.Fatal("ok = false for an unlimited provider")
	}
	release()
}

func TestWaitForProviderCapacityAvailable(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_MAX_CONCURRENT_TASKS", "1")
	d := testProviderDaemon(t)

	release, ok := d.waitForProviderCapacity(context.Background(), Task{ID: "t1"}, "claude", d.logger)
	if !ok {
		t.Fatal("ok = false, want true while a token is free")
	}
	release()

	// After the release the token is free again and a second waiter acquires
	// it immediately.
	release2, ok := d.waitForProviderCapacity(context.Background(), Task{ID: "t2"}, "claude", d.logger)
	if !ok {
		t.Fatal("ok = false after the first waiter released its token")
	}
	release2()
}

func TestWaitForProviderCapacityBlocksUntilTokenFreed(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_MAX_CONCURRENT_TASKS", "1")
	d := testProviderDaemon(t)

	pool := d.providerPool("claude")
	pool <- struct{}{} // saturate the single slot

	type result struct {
		release func()
		ok      bool
	}
	res := make(chan result, 1)
	go func() {
		release, ok := d.waitForProviderCapacity(context.Background(), Task{ID: "t1"}, "claude", d.logger)
		res <- result{release: release, ok: ok}
	}()

	select {
	case r := <-res:
		t.Fatalf("waiter completed before the token was freed (ok=%v)", r.ok)
	case <-time.After(200 * time.Millisecond):
	}

	<-pool // free the slot; the parked waiter takes it
	select {
	case r := <-res:
		if !r.ok {
			t.Fatal("ok = false after the token was freed, want true")
		}
		r.release()
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not acquire the token after it was freed")
	}
}

func TestWaitForProviderCapacityAbortedByContext(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_MAX_CONCURRENT_TASKS", "1")
	d := testProviderDaemon(t)

	pool := d.providerPool("claude")
	pool <- struct{}{} // saturate the single slot

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := d.waitForProviderCapacity(ctx, Task{ID: "t1"}, "claude", d.logger)
		done <- ok
	}()

	time.Sleep(100 * time.Millisecond) // let the waiter park
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("ok = true after the parent context was cancelled, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not wake when the parent context was cancelled")
	}
}

// TestHandleTaskEnforcesProviderConcurrency pins the end-to-end behavior: two
// tasks for the same capped provider, claimed into two global slots, may not
// run at the same time. The second task parks in waitForProviderCapacity and
// only runs once the first one finishes.
func TestHandleTaskEnforcesProviderConcurrency(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_MAX_CONCURRENT_TASKS", "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := New(Config{
		ServerBaseURL:      srv.URL,
		HeartbeatInterval:  time.Hour,
		PollInterval:       time.Hour,
		MaxConcurrentTasks: 4,
	}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1", Provider: "claude"}
	d.cancelPollInterval = time.Hour

	var (
		mu         sync.Mutex
		running    atomic.Int64
		maxRunning atomic.Int64
		t1Entered  = make(chan struct{})
		t1Exited   bool
		t2Entered  bool
	)
	releaseFirst := make(chan struct{})
	d.runner = taskRunnerFunc(func(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error) {
		running.Add(1)
		defer running.Add(-1)
		n := running.Load()
		for {
			m := maxRunning.Load()
			if n <= m || maxRunning.CompareAndSwap(m, n) {
				break
			}
		}
		mu.Lock()
		if task.ID == "t1" {
			t1Exited = false
			mu.Unlock()
			close(t1Entered)
			<-releaseFirst
			mu.Lock()
			t1Exited = true
			mu.Unlock()
			return TaskResult{Status: "completed"}, nil
		}
		t2Entered = true
		mu.Unlock()
		return TaskResult{Status: "completed"}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.handleTask(ctx, Task{ID: "t1", RuntimeID: "rt-1", Agent: &AgentData{Name: "a1"}}, 0)
	}()
	<-t1Entered

	go func() {
		defer wg.Done()
		d.handleTask(ctx, Task{ID: "t2", RuntimeID: "rt-1", Agent: &AgentData{Name: "a2"}}, 1)
	}()

	// t2 must stay parked on the provider token while t1 holds it: poll for
	// 300ms and fail the moment t2 enters the runner.
	parkDeadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(parkDeadline) {
		mu.Lock()
		entered := t2Entered
		mu.Unlock()
		if entered {
			t.Fatal("t2 started running while t1 still holds the only claude token")
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(releaseFirst)
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(4 * time.Second):
		t.Fatal("handleTask goroutines did not finish after the token was released")
	}

	mu.Lock()
	bothDone := t1Exited && t2Entered
	mu.Unlock()
	if !bothDone {
		t.Fatalf("t1Exited=%v t2Entered=%v, want both tasks to run", t1Exited, t2Entered)
	}
	if got := maxRunning.Load(); got != 1 {
		t.Fatalf("max concurrent claude runs = %d, want 1", got)
	}
}
