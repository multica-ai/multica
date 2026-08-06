package daemon

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// newDrainTestDaemon builds a Daemon stripped to just the fields graceful
// drain touches.
func newDrainTestDaemon(enabled bool, window time.Duration) *Daemon {
	return &Daemon{
		cfg: Config{
			GracefulDrain: GracefulDrainConfig{Enabled: enabled, Window: window},
		},
		logger: slog.Default(),
	}
}

// TestTaskParentCtx_DecouplesFromRootWhenEnabled is the crux of FIR-3758: with
// the feature ON, cancelling the root ctx (SIGTERM) must NOT cancel the
// context handed to in-flight tasks. With the feature OFF the task ctx is the
// root ctx, so behaviour is unchanged.
func TestTaskParentCtx_DecouplesFromRootWhenEnabled(t *testing.T) {
	t.Run("enabled: survives root cancel", func(t *testing.T) {
		d := newDrainTestDaemon(true, time.Second)
		root, cancelRoot := context.WithCancel(context.Background())
		taskCtx := d.taskParentCtx(root)

		cancelRoot() // simulate SIGTERM

		select {
		case <-taskCtx.Done():
			t.Fatal("task ctx was cancelled by root ctx cancel — restart would kill in-flight agent")
		case <-time.After(50 * time.Millisecond):
			// good: task ctx is independent of the root ctx
		}
	})

	t.Run("disabled: tied to root cancel", func(t *testing.T) {
		d := newDrainTestDaemon(false, time.Second)
		root, cancelRoot := context.WithCancel(context.Background())
		taskCtx := d.taskParentCtx(root)

		if taskCtx != root {
			t.Fatal("feature off must return the root ctx unchanged")
		}
		cancelRoot()
		select {
		case <-taskCtx.Done():
			// good: legacy behaviour, task ctx dies with root
		case <-time.After(time.Second):
			t.Fatal("task ctx should be cancelled with root when feature is off")
		}
	})
}

// TestTaskParentCtx_SharedAcrossPollers verifies every poller gets the SAME
// task ctx so one drain cancel tears down all in-flight tasks together.
func TestTaskParentCtx_SharedAcrossPollers(t *testing.T) {
	d := newDrainTestDaemon(true, time.Second)
	root := context.Background()
	a := d.taskParentCtx(root)
	b := d.taskParentCtx(root)
	if a != b {
		t.Fatal("expected a single shared task ctx across pollers")
	}
}

// TestDrainInFlightTasks_LetsRunningTaskFinishWithinWindow is the deliverable:
// an in-flight run is NOT interrupted by a restart that happens within the
// window. The task runs for less than the window, verifying its own context
// is never cancelled, and drain returns as soon as it finishes.
func TestDrainInFlightTasks_LetsRunningTaskFinishWithinWindow(t *testing.T) {
	d := newDrainTestDaemon(true, 5*time.Second)

	root, cancelRoot := context.WithCancel(context.Background())
	taskCtx := d.taskParentCtx(root)

	var taskWG sync.WaitGroup
	var sawCancel bool
	var finished bool

	taskWG.Add(1)
	taskStarted := make(chan struct{})
	go func() {
		defer taskWG.Done()
		close(taskStarted)
		// Simulate real work well under the window; poll for cancellation.
		deadline := time.After(300 * time.Millisecond)
		for {
			select {
			case <-taskCtx.Done():
				sawCancel = true
				return
			case <-deadline:
				finished = true
				return
			}
		}
	}()
	<-taskStarted

	// Simulate SIGTERM: root ctx cancelled, then pollLoop drains.
	cancelRoot()

	start := time.Now()
	d.drainInFlightTasks(&taskWG)
	elapsed := time.Since(start)

	if sawCancel {
		t.Fatal("in-flight task was cancelled during drain window — restart interrupted the run")
	}
	if !finished {
		t.Fatal("task did not finish normally")
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("drain waited the full window instead of returning when the task finished (%s)", elapsed)
	}
}

// TestDrainInFlightTasks_CancelsStragglerAfterWindow verifies the fallback: a
// task that outlives the window IS cancelled so the process can exit (those
// runs fall back to Spor B recovery).
func TestDrainInFlightTasks_CancelsStragglerAfterWindow(t *testing.T) {
	d := newDrainTestDaemon(true, 150*time.Millisecond)

	root, cancelRoot := context.WithCancel(context.Background())
	taskCtx := d.taskParentCtx(root)

	var taskWG sync.WaitGroup
	var cancelledByWindow bool

	taskWG.Add(1)
	taskStarted := make(chan struct{})
	go func() {
		defer taskWG.Done()
		close(taskStarted)
		<-taskCtx.Done() // straggler: only stops when drain cancels it
		cancelledByWindow = true
	}()
	<-taskStarted

	cancelRoot()

	start := time.Now()
	d.drainInFlightTasks(&taskWG)
	elapsed := time.Since(start)

	if !cancelledByWindow {
		t.Fatal("straggler task was not cancelled after the window elapsed")
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("drain returned before the window elapsed (%s)", elapsed)
	}
}

// TestDrainInFlightTasks_LegacyWaitsForCancelledTasks verifies the feature-OFF
// path: tasks run under the root ctx (already cancelled at shutdown), and
// drain simply waits for them to unwind and returns.
func TestDrainInFlightTasks_LegacyWaitsForCancelledTasks(t *testing.T) {
	d := newDrainTestDaemon(false, time.Second)

	root, cancelRoot := context.WithCancel(context.Background())
	taskCtx := d.taskParentCtx(root) // == root when disabled

	var taskWG sync.WaitGroup
	taskWG.Add(1)
	go func() {
		defer taskWG.Done()
		<-taskCtx.Done() // dies with the root ctx, as before
	}()

	cancelRoot()

	done := make(chan struct{})
	go func() {
		d.drainInFlightTasks(&taskWG)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy drain did not return promptly for cancelled tasks")
	}
}

func TestLoadGracefulDrainConfig(t *testing.T) {
	t.Run("default off", func(t *testing.T) {
		t.Setenv("MULTICA_DAEMON_GRACEFUL_DRAIN", "")
		t.Setenv("MULTICA_DAEMON_GRACEFUL_DRAIN_WINDOW", "")
		cfg := loadGracefulDrainConfig()
		if cfg.Enabled {
			t.Fatal("graceful drain must default to OFF")
		}
		if cfg.Window != DefaultGracefulDrainWindow {
			t.Fatalf("unexpected default window: %s", cfg.Window)
		}
	})

	t.Run("enabled with custom window", func(t *testing.T) {
		t.Setenv("MULTICA_DAEMON_GRACEFUL_DRAIN", "true")
		t.Setenv("MULTICA_DAEMON_GRACEFUL_DRAIN_WINDOW", "90s")
		cfg := loadGracefulDrainConfig()
		if !cfg.Enabled {
			t.Fatal("expected enabled")
		}
		if cfg.Window != 90*time.Second {
			t.Fatalf("expected 90s window, got %s", cfg.Window)
		}
	})

	t.Run("invalid window falls back to default", func(t *testing.T) {
		t.Setenv("MULTICA_DAEMON_GRACEFUL_DRAIN", "1")
		t.Setenv("MULTICA_DAEMON_GRACEFUL_DRAIN_WINDOW", "not-a-duration")
		cfg := loadGracefulDrainConfig()
		if !cfg.Enabled {
			t.Fatal("expected enabled")
		}
		if cfg.Window != DefaultGracefulDrainWindow {
			t.Fatalf("expected default window on invalid input, got %s", cfg.Window)
		}
	})
}
