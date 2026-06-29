package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type capturingHandler struct {
	mu    sync.Mutex
	out   bytes.Buffer
	cnt   atomic.Int64
	inner slog.Handler
}

func newCapturingHandler() *capturingHandler {
	h := &capturingHandler{}
	h.inner = slog.NewTextHandler(&h.out, &slog.HandlerOptions{Level: slog.LevelDebug})
	return h
}

func (h *capturingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}
func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(name string) slog.Handler       { return h }
func (h *capturingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cnt.Add(1)
	return h.inner.Handle(ctx, r)
}

func (h *capturingHandler) snapshot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.out.String()
}

func newSafeGoTestDaemon(h *capturingHandler) *Daemon {
	return &Daemon{logger: slog.New(h)}
}

func TestSafeGo_RecoversFromPanic(t *testing.T) {
	t.Parallel()
	h := newCapturingHandler()
	d := newSafeGoTestDaemon(h)

	var ran atomic.Bool
	d.safeGo("boom", func() {
		ran.Store(true)
		panic("simulated runtime SDK explosion")
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ran.Load() && h.cnt.Load() >= 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !ran.Load() {
		t.Fatalf("safeGo closure never executed")
	}
	if h.cnt.Load() == 0 {
		t.Fatalf("safeGo recover never logged the panic")
	}

	logged := h.snapshot()
	if !strings.Contains(logged, "goroutine panicked") {
		t.Errorf("expected panic log line, got: %q", logged)
	}
	if !strings.Contains(logged, "name=boom") {
		t.Errorf("expected name=boom in log, got: %q", logged)
	}
	if !strings.Contains(logged, "simulated runtime SDK explosion") {
		t.Errorf("expected panic payload in log, got: %q", logged)
	}
	if !strings.Contains(logged, "stack=") {
		t.Errorf("expected stack trace in log, got: %q", logged)
	}
}

func TestSafeGo_NormalPathRunsToCompletion(t *testing.T) {
	t.Parallel()
	h := newCapturingHandler()
	d := newSafeGoTestDaemon(h)

	var done atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	d.safeGo("healthy", func() {
		defer wg.Done()
		defer done.Add(1)
		time.Sleep(5 * time.Millisecond)
	})

	wg.Wait()
	if got := done.Load(); got != 1 {
		t.Fatalf("inner defer did not fire: done=%d", got)
	}
	if h.cnt.Load() != 0 {
		t.Errorf("unexpected panic log on healthy path: %q", h.snapshot())
	}
}

func TestSafeGo_InnerDeferFiresOnPanic(t *testing.T) {
	t.Parallel()
	h := newCapturingHandler()
	d := newSafeGoTestDaemon(h)

	// Callers (pollerWG, taskWG, bgSyncs, activeTasks) rely on `defer X.Done()`
	// placed as the first statement of the closure. If safeGo's recover ran
	// BEFORE inner defers, every existing WG would deadlock on the next
	// Wait(). This test pins the LIFO ordering.
	var innerDeferRan atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	d.safeGo("withInnerDefer", func() {
		defer wg.Done()
		defer func() { innerDeferRan.Store(true) }()
		panic("panic after scheduling inner defer")
	})

	wg.Wait()
	if !innerDeferRan.Load() {
		t.Fatalf("inner defer must run before safeGo's recover unwinds")
	}
}
