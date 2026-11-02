package agent

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// stuckBackgroundProcess never dies and takes a fixed time to signal, which is
// the shape that makes per-process bounds insufficient: each one is bounded,
// but a run may launch any number of them.
type stuckBackgroundProcess struct {
	signal time.Duration
	// entered, when set, is closed the first time this process is signalled.
	// finish() holds mu for the whole pass, so that close is proof the caller
	// owns the lock — which a channel closed just before calling Interrupt is
	// not: it only says the goroutine was about to try.
	entered chan struct{}
	once    sync.Once
}

func (p *stuckBackgroundProcess) alive() (bool, error) { return true, nil }
func (p *stuckBackgroundProcess) terminate() error {
	if p.entered != nil {
		p.once.Do(func() { close(p.entered) })
	}
	time.Sleep(p.signal)
	return nil
}
func (p *stuckBackgroundProcess) close() {}

// TestCursorBackgroundCloseIsBoundedAcrossTools pins the bound that matters
// after a terminal result: Close() runs once the daemon's watchdog has stopped
// supervising the run, so "each process is bounded" is not enough — the whole
// pass has to be. Unconfirmed work must still surface its launch result.
func TestCursorBackgroundCloseIsBoundedAcrossTools(t *testing.T) {
	const (
		tools  = 40
		signal = 100 * time.Millisecond
		budget = 300 * time.Millisecond
	)
	// Without a whole-pass bound this would take tools*signal*2 = 8s.
	unbounded := tools * signal * 2

	messages := make(chan Message, 4*tools)
	b := newCursorBackgroundTools(context.Background(), nil, messages, slog.Default())
	b.closeBudget = budget
	for i := 0; i < tools; i++ {
		b.tools = append(b.tools, cursorBackgroundTool{
			call:    cursorToolCall{Name: "shell", CallID: "bg", Result: `{"isBackground":true}`},
			process: &cursorBackgroundProcess{platform: &stuckBackgroundProcess{signal: signal}},
		})
	}

	start := time.Now()
	b.Close()
	elapsed := time.Since(start)

	// One in-flight signal may still be running when the budget expires.
	if limit := budget + signal + 2*time.Second; elapsed > limit {
		t.Fatalf("Close took %s, want under %s (unbounded would be ~%s)", elapsed, limit, unbounded)
	}
	if elapsed >= unbounded {
		t.Fatalf("Close took %s: the whole-pass budget did not apply", elapsed)
	}

	// Every tool still hands back its original launch result, none of it
	// reported as confirmed cleanup.
	close(messages)
	results := 0
	for msg := range messages {
		if msg.Type == MessageToolResult {
			results++
		}
	}
	if results != tools {
		t.Fatalf("released %d tool results, want %d: unconfirmed work must not be dropped", results, tools)
	}
}

// TestCursorBackgroundCloseIsBoundedBehindAConcurrentPass covers the gap the
// first test cannot see: Close() takes its deadline before the lock, but the
// bound only holds if whoever already holds that lock is bounded too. An
// unbounded Interrupt() scanning an unbounded number of tools is an unbounded
// Close(), one level down — and Interrupt-holds-the-lock-while-the-terminal-
// result-arrives is the exact topology this whole boundary exists for.
func TestCursorBackgroundCloseIsBoundedBehindAConcurrentPass(t *testing.T) {
	const (
		tools  = 20
		signal = 50 * time.Millisecond
		budget = 100 * time.Millisecond
	)
	messages := make(chan Message, 8*tools)
	b := newCursorBackgroundTools(context.Background(), nil, messages, slog.Default())
	b.closeBudget = budget
	// Only the first process carries the gate; it is the one the interrupting
	// pass reaches first, so closing it happens inside that pass's critical
	// section.
	entered := make(chan struct{})
	for i := 0; i < tools; i++ {
		platform := &stuckBackgroundProcess{signal: signal}
		if i == 0 {
			platform.entered = entered
		}
		b.tools = append(b.tools, cursorBackgroundTool{
			call:    cursorToolCall{Name: "shell", CallID: "bg", Result: `{"isBackground":true}`},
			process: &cursorBackgroundProcess{platform: platform},
		})
	}

	// The watchdog's cleanup pass must already hold the lock before Close() asks
	// for it, or Close() can win the lock and finish on its own — and then an
	// unbounded Interrupt() would still pass this test. Waiting for the gate
	// inside the pass is what makes the ordering a fact rather than a scheduling
	// habit. Unbounded, that pass alone would take tools*signal = 1s.
	go b.Interrupt()
	<-entered

	start := time.Now()
	b.Close()
	elapsed := time.Since(start)

	// Worst case is one lock wait plus the closing pass, each bounded, plus a
	// signal that was already in flight when a budget expired.
	if limit := 2*budget + 2*signal + 2*time.Second; elapsed > limit {
		t.Fatalf("Close took %s behind a concurrent pass, want under %s", elapsed, limit)
	}
	if elapsed >= tools*signal {
		t.Fatalf("Close took %s: the concurrent pass was not bounded", elapsed)
	}
}
