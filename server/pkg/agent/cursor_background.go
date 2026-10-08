package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type cursorBackgroundTool struct {
	call    cursorToolCall
	process *cursorBackgroundProcess
}

// The daemon owns the only budget timer. This tracker owns process cleanup and
// the matching tool result, so expiration cannot race a second run-cancel timer.
type cursorBackgroundTools struct {
	ctx              context.Context
	cmd              *exec.Cmd
	messages         chan<- Message
	logger           *slog.Logger
	mu               sync.Mutex
	tools            []cursorBackgroundTool
	closed           bool
	stop             chan struct{}
	done             chan struct{}
	once             sync.Once
	inFlight         atomic.Int32
	lastToolActivity atomic.Int64
	// closeBudget overrides cursorCloseBudget for this tracker. Zero — the value
	// every production construction path leaves it at — means the constant.
	closeBudget time.Duration
	// terminal is set the moment Cursor's authoritative result is read, before
	// Close() takes mu. Cleanup can block on that lock behind an in-progress
	// Interrupt and can legitimately fail, so it cannot be the signal that the
	// outcome is decided; this flag is, and it is deliberately lock-free.
	terminal atomic.Bool
}

// ObserveTerminal records that the backend has read its authoritative terminal
// result. Call it before any cleanup that may block or fail.
func (b *cursorBackgroundTools) ObserveTerminal() { b.terminal.Store(true) }

// TerminalObserved reports whether the run's outcome is already decided.
func (b *cursorBackgroundTools) TerminalObserved() bool { return b.terminal.Load() }

func newCursorBackgroundTools(ctx context.Context, cmd *exec.Cmd, messages chan<- Message, logger *slog.Logger) *cursorBackgroundTools {
	b := &cursorBackgroundTools{ctx: ctx, cmd: cmd, messages: messages, logger: logger, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(b.done)
		// This only observes natural process exit; it never measures inactivity.
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.Reap()
			case <-b.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return b
}

func (b *cursorBackgroundTools) Activity() (int32, time.Time) {
	return b.inFlight.Load(), time.Unix(0, b.lastToolActivity.Load())
}

func (b *cursorBackgroundTools) Send(msg Message) {
	if msg.Type == MessageToolUse || msg.Type == MessageToolResult {
		now := time.Now().UnixNano()
		for {
			previous := b.lastToolActivity.Load()
			if now <= previous || b.lastToolActivity.CompareAndSwap(previous, now) {
				break
			}
		}
	}
	if msg.Type == MessageToolUse {
		b.inFlight.Add(1)
	}
	// Publish completion before switching to the idle budget. Otherwise the
	// watchdog can see zero tools with the old timestamp and an empty queue.
	trySend(b.messages, msg)
	if msg.Type == MessageToolResult {
		for {
			count := b.inFlight.Load()
			if count <= 0 || b.inFlight.CompareAndSwap(count, count-1) {
				break
			}
		}
	}
}

func (b *cursorBackgroundTools) SendResult(call cursorToolCall) {
	b.Send(Message{Type: MessageToolResult, Tool: call.Name, CallID: call.CallID, Output: call.Result})
}

func (b *cursorBackgroundTools) Add(call cursorToolCall) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, tool := range b.tools {
		if call.CallID != "" && tool.call.CallID == call.CallID {
			return
		}
	}
	p, err := captureCursorBackgroundProcess(b.cmd, call.PID)
	if err != nil {
		// No work was claimed. Preserve Cursor's launch completion so the idle
		// watchdog remains available even when the tool watchdog is disabled.
		// This grants no cleanup recovery window and never signals the PID.
		b.logger.Warn("cannot own Cursor background shell; returning launch result for idle watchdog fallback", "error", err)
		b.SendResult(call)
		return
	}
	b.tools = append(b.tools, cursorBackgroundTool{call: call, process: p})
}

// cursorCloseBudget bounds the WHOLE of Close(), not each process in it.
// Per-process termination is already bounded, but the number of background
// shells one run may launch is not, so "bounded per process" is not a bound on
// finalization. It has to be one, because Close() runs after the terminal
// result has been observed — the point at which the daemon's watchdog has
// deliberately stopped supervising this run, and MULTICA_AGENT_TIMEOUT is 0 by
// default. Work still unconfirmed when the budget runs out takes the existing
// unconfirmed-at-close path: its result is preserved and its cleanup is logged
// as unconfirmed, never reported as successful.
const cursorCloseBudget = 10 * time.Second

func (b *cursorBackgroundTools) Reap() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.finish(false, time.Now().Add(b.budget()))
	}
}

// budget resolves the per-pass cleanup bound. Every pass that holds mu is
// bounded by it, not just the closing one: Close() has to wait for this lock,
// so an unbounded pass held by anyone else is an unbounded Close(), which is
// the same defect one level down.
func (b *cursorBackgroundTools) budget() time.Duration {
	if b.closeBudget > 0 {
		return b.closeBudget
	}
	return cursorCloseBudget
}

// Interrupt is called synchronously by the daemon's tool watchdog. Each tool
// can grant a recovery window only once: it is removed with its tool result.
func (b *cursorBackgroundTools) Interrupt() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil {
		return false
	}
	return b.finish(true, time.Now().Add(b.budget()))
}

// finish is called with mu held. A failed ownership check or cleanup never
// decrements the tool count; only confirmed process exit releases its result.
//
// A non-zero deadline caps the whole pass rather than each process: once it has
// passed, the remaining tools are left untouched and unconfirmed instead of
// each adding its own termination wait.
func (b *cursorBackgroundTools) finish(interrupt bool, deadline time.Time) bool {
	completed := false
	remaining := b.tools[:0]
	for _, tool := range b.tools {
		if tool.process == nil {
			remaining = append(remaining, tool)
			continue
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			remaining = append(remaining, tool)
			continue
		}
		alive, err := tool.process.Alive()
		if err == nil && alive && interrupt {
			err = tool.process.Terminate()
			if err == nil {
				alive, err = tool.process.Alive()
			}
		}
		if err != nil || alive {
			if err != nil && interrupt {
				b.logger.Warn("could not stop Cursor background shell", "error", err)
			}
			remaining = append(remaining, tool)
			continue
		}
		b.SendResult(tool.call)
		tool.process.Close()
		completed = true
	}
	b.tools = remaining
	return completed
}

func (b *cursorBackgroundTools) Close() {
	b.once.Do(func() {
		// Taken before the lock on purpose: waiting for a concurrent pass is
		// part of what Close() has to bound, not something outside its budget.
		// Every other pass is bounded by the same value, so the wait is too.
		deadline := time.Now().Add(b.budget())
		b.mu.Lock()
		b.closed = true
		// If waiting for the lock already consumed the budget, the closing pass
		// still gets a full one of its own: the bound this guards is "Close()
		// returns", and giving it zero time would just push every tool onto the
		// unconfirmed path without trying. Two budgets is the honest ceiling,
		// and terminalResultHandoffBudget is derived from exactly that.
		if !time.Now().Before(deadline) {
			deadline = time.Now().Add(b.budget())
		}
		b.finish(true, deadline)
		if len(b.tools) > 0 {
			// Retry a transient lookup/termination failure before releasing the
			// final claim. Persistent errors remain explicitly unconfirmed.
			b.finish(true, deadline)
		}
		for _, tool := range b.tools {
			// The stream is closing, so preserve even unverifiable launch
			// payloads without pretending cleanup succeeded in native accounting.
			b.logger.Warn("Cursor background cleanup unconfirmed at close", "call_id", tool.call.CallID)
			trySend(b.messages, Message{Type: MessageToolResult, Tool: tool.call.Name, CallID: tool.call.CallID, Output: tool.call.Result})
			tool.process.Close()
		}
		b.tools = nil
		b.mu.Unlock()
		close(b.stop)
		<-b.done
	})
}
