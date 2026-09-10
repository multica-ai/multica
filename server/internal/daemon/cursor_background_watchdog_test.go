package daemon

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

type backgroundToolBackend struct {
	natural                  time.Duration
	mixed, invalid, noResult bool
	dropTranscript           bool
	interrupts               atomic.Int32
	cleaned                  atomic.Bool
}

func (b *backgroundToolBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message, 8)
	results := make(chan agent.Result, 1)
	stopped := make(chan struct{})
	var nativeCount atomic.Int32
	var nativeActivity atomic.Int64
	nativeActivity.Store(time.Now().UnixNano())
	nativeCount.Store(1)
	if !b.dropTranscript {
		messages <- agent.Message{Type: agent.MessageToolUse, Tool: "shell", CallID: "bg"}
	}
	if b.mixed {
		nativeCount.Add(1)
		messages <- agent.Message{Type: agent.MessageToolUse, Tool: "read", CallID: "fg"}
	}
	go func() {
		defer close(messages)
		defer close(results)
		var natural <-chan time.Time
		if b.natural > 0 {
			natural = time.After(b.natural)
		}
		select {
		case <-natural:
			nativeActivity.Store(time.Now().UnixNano())
			if !b.dropTranscript {
				messages <- agent.Message{Type: agent.MessageToolResult, Tool: "shell", CallID: "bg", Output: "raw launch result"}
			}
			nativeCount.Add(-1)
		case <-stopped:
		case <-ctx.Done():
			results <- agent.Result{Status: "aborted"}
			return
		}
		if b.mixed || b.noResult {
			<-ctx.Done()
			results <- agent.Result{Status: "aborted"}
			return
		}
		finishDelay := 10 * time.Millisecond
		if b.dropTranscript {
			finishDelay = 30 * time.Millisecond
		}
		select {
		case <-time.After(finishDelay):
			results <- agent.Result{Status: "completed", Output: "Cursor terminal result"}
		case <-ctx.Done():
			results <- agent.Result{Status: "aborted"}
		}
	}()
	return &agent.Session{Messages: messages, Result: results, ToolActivity: func() (int32, time.Time) { return nativeCount.Load(), time.Unix(0, nativeActivity.Load()) }, InterruptBackgroundTools: func() bool {
		b.interrupts.Add(1)
		if b.invalid || !b.cleaned.CompareAndSwap(false, true) {
			return false
		}
		nativeActivity.Store(time.Now().UnixNano())
		if !b.dropTranscript {
			messages <- agent.Message{Type: agent.MessageToolResult, Tool: "shell", CallID: "bg", Output: "raw launch result"}
		}
		nativeCount.Add(-1)
		close(stopped)
		return true
	}}, nil
}

func TestExecuteAndDrain_BackgroundToolWatchdog(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		idle, tool, natural      time.Duration
		mixed, invalid, noResult bool
		status                   string
		cleanup                  bool
	}{
		{name: "silent beyond idle under tool budget", idle: 50 * time.Millisecond, tool: time.Second, natural: 250 * time.Millisecond, status: "completed"},
		{name: "budget stops tool and preserves result", idle: 50 * time.Millisecond, tool: 100 * time.Millisecond, status: "completed", cleanup: true},
		{name: "disabled suite", tool: 50 * time.Millisecond, natural: 150 * time.Millisecond, status: "completed"},
		{name: "disabled tool budget", idle: 50 * time.Millisecond, natural: 150 * time.Millisecond, status: "completed"},
		{name: "unverified process falls back", idle: 50 * time.Millisecond, tool: 100 * time.Millisecond, invalid: true, status: "idle_watchdog"},
		{name: "foreground still gets bounded", idle: 50 * time.Millisecond, tool: 100 * time.Millisecond, mixed: true, status: "idle_watchdog", cleanup: true},
		{name: "missing terminal still gets bounded", idle: 50 * time.Millisecond, tool: 100 * time.Millisecond, noResult: true, status: "idle_watchdog", cleanup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := newTestDaemon(t)
			d.cfg.AgentIdleWatchdog, d.cfg.AgentToolWatchdog = tc.idle, tc.tool
			backend := &backgroundToolBackend{natural: tc.natural, mixed: tc.mixed, invalid: tc.invalid, noResult: tc.noResult}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, _, err := d.executeAndDrain(ctx, backend, "test", agent.ExecOptions{}, slog.Default(), "background-watchdog", "", new(atomic.Int32))
			if err != nil || result.Status != tc.status {
				t.Fatalf("result=%+v error=%v, want %s", result, err, tc.status)
			}
			if backend.cleaned.Load() != tc.cleanup {
				t.Fatalf("cleanup=%v want %v", backend.cleaned.Load(), tc.cleanup)
			}
			if tc.natural > 0 && backend.interrupts.Load() != 0 {
				t.Fatalf("legitimate/disabled run interrupted %d times", backend.interrupts.Load())
			}
			if tc.status == "completed" && result.Output != "Cursor terminal result" {
				t.Fatalf("native result lost: %+v", result)
			}
		})
	}
}

func TestBackgroundToolWatchdogShortOverride(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var last, threshold atomic.Int64
		var tools atomic.Int32
		var fired atomic.Bool
		last.Store(time.Now().UnixNano())
		tools.Store(1)
		calls := 0
		go new(Daemon).runIdleWatchdog(ctx, 10*time.Minute, time.Minute, &last, tools.Load, &fired, &threshold, cancel, make(chan agent.Message), func() bool { calls++; cancel(); return true }, nil, slog.Default())
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if calls != 1 || fired.Load() {
			t.Fatalf("short tool budget: calls=%d fired=%v", calls, fired.Load())
		}
	})
}

func TestBackgroundToolWatchdogNaturalExitRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var last, threshold atomic.Int64
		var tools atomic.Int32
		var fired atomic.Bool
		last.Store(time.Now().UnixNano())
		tools.Store(1)
		go new(Daemon).runIdleWatchdog(ctx, time.Minute, time.Minute, &last, tools.Load, &fired, &threshold, cancel, make(chan agent.Message), func() bool {
			tools.Store(0)
			last.Store(time.Now().UnixNano())
			return false
		}, nil, slog.Default())
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if fired.Load() || ctx.Err() != nil {
			t.Fatal("stale tool threshold cancelled after natural completion")
		}
	})
}

func TestBackgroundToolWatchdogFreshActivityDuringRevalidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var last, threshold atomic.Int64
		var fired atomic.Bool
		last.Store(time.Now().UnixNano())
		refreshAtBoundary, refreshed := false, false
		// Match Session.ToolActivity's production callback: refreshing native
		// activity publishes a timestamp even when the tool count stays positive.
		toolState := func() int32 {
			if refreshAtBoundary && !refreshed {
				last.Store(time.Now().UnixNano())
				refreshed = true
			}
			return 1
		}
		go new(Daemon).runIdleWatchdog(ctx, time.Minute, time.Minute, &last, toolState, &fired, &threshold, cancel, make(chan agent.Message), func() bool {
			refreshAtBoundary = true
			return false
		}, nil, slog.Default())
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if !refreshed || fired.Load() || ctx.Err() != nil {
			t.Fatalf("fresh native activity lost: refreshed=%v fired=%v err=%v", refreshed, fired.Load(), ctx.Err())
		}
		time.Sleep(59 * time.Second)
		synctest.Wait()
		if fired.Load() || ctx.Err() != nil {
			t.Fatal("watchdog did not grant the renewed tool budget")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if !fired.Load() || ctx.Err() == nil {
			t.Fatal("genuine inactivity after the renewed budget did not fire")
		}
	})
}

func TestExecuteAndDrain_BackgroundNativeCountSurvivesDroppedTranscript(t *testing.T) {
	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond
	d.cfg.AgentToolWatchdog = time.Second
	backend := &backgroundToolBackend{natural: 200 * time.Millisecond, dropTranscript: true}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, _, err := d.executeAndDrain(ctx, backend, "test", agent.ExecOptions{}, slog.Default(), "dropped-transcript", "", new(atomic.Int32))
	if err != nil || result.Status != "completed" || backend.interrupts.Load() != 0 {
		t.Fatalf("native tool state lost: %+v err=%v", result, err)
	}
}

// TestCursorBackgroundCapturedDisabledToolWatchdog is the deliberate mirror of
// TestCursorBackgroundUncapturedDisabledToolWatchdog. When ownership WAS
// captured the launched process is genuinely in flight, so a zero tool budget
// means what it says and nothing force-stops the run — the same outcome a
// foreground tool that never returns already has. The uncaptured case releases
// its tool precisely because nothing was claimed there; these two must not be
// "fixed" into agreeing without changing what MULTICA_AGENT_TOOL_WATCHDOG=0
// promises. See the knob's documentation in config.go.
func TestCursorBackgroundCapturedDisabledToolWatchdog(t *testing.T) {
	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond
	d.cfg.AgentToolWatchdog = 0

	// noResult: the launched process never exits on its own (GH #7833's dev
	// server). invalid stays false, so ownership is held and the tool is real.
	backend := &backgroundToolBackend{noResult: true}

	// Stands in for MULTICA_AGENT_TIMEOUT, which is 0 (unbounded) by default.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result, _, err := d.executeAndDrain(ctx, backend, "test", agent.ExecOptions{}, slog.Default(), "captured-background", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if result.Status == "idle_watchdog" {
		t.Fatalf("a held background tool was force-stopped despite a zero tool budget: %+v", result)
	}
	if backend.interrupts.Load() != 0 {
		t.Fatalf("cleanup ran %d times without a tool budget", backend.interrupts.Load())
	}
}
