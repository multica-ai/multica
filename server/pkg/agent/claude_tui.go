//go:build !windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

func claudeTUIInterruptSignal() os.Signal { return syscall.SIGINT }

// claudeTUIBackend implements Backend by driving the real Claude Code TUI
// over a pseudo-terminal. Unlike claudeBackend (which uses -p stream-json),
// this backend types the prompt into the interactive UI character-by-character
// and accumulates ANSI-stripped output until a silence window elapses.
//
// This is a demo/technical-validation backend. It has hard limitations:
//   - No structured tool_use / tool_result events (Claude Code runs tools
//     locally; daemon only sees final assistant text).
//   - No MCP config injection (--mcp-config is a headless-mode flag).
//   - No session resumption; ResumeSessionID is ignored.
//   - Completion is detected by output silence, not a protocol frame.
//   - Token usage and SessionID are not reported.
//
// Not supported on Windows (creack/pty has no Windows ConPTY implementation
// in this dependency).
type claudeTUIBackend struct {
	cfg Config
}

// claudeTUIDefaultSilence is the default inactivity window after which the
// backend considers the agent done producing output. Demo-grade heuristic:
// long enough that mid-turn pauses don't end the session prematurely, short
// enough that callers don't wait forever after a real completion.
const claudeTUIDefaultSilence = 5 * time.Minute

// ansiEscape matches the common ANSI control sequences that the Claude Code
// TUI emits for redraws, colors, and cursor moves. Not exhaustive — the goal
// is plain-text approximation for the output buffer, not perfect terminal
// emulation.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[=>]|\r`)

func (b *claudeTUIBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	hardTimeout := opts.Timeout
	if hardTimeout == 0 {
		hardTimeout = 60 * time.Minute
	}
	silenceWindow := opts.SemanticInactivityTimeout
	if silenceWindow == 0 {
		silenceWindow = claudeTUIDefaultSilence
	}

	runCtx, cancel := context.WithTimeout(ctx, hardTimeout)

	// Build the command without -p / --output-format so claude renders its
	// real interactive TUI. CustomArgs are still honored for users who want
	// to pass e.g. --model.
	args := append([]string{}, opts.ExtraArgs...)
	args = append(args, opts.CustomArgs...)
	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	b.cfg.Logger.Info("agent command (tui)", "exec", execPath, "args", args, "silence", silenceWindow.String(), "timeout", hardTimeout.String())

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start claude (tui pty): %w", err)
	}

	b.cfg.Logger.Info("claude tui started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// lastByteAt is updated by the reader goroutine every time the TUI writes
	// anything to the PTY. The silence-window watcher reads it to decide when
	// to declare completion. Protected by a mutex because the watcher and the
	// reader run on different goroutines.
	var (
		lastMu     sync.Mutex
		lastByteAt = time.Now()
	)
	bump := func() {
		lastMu.Lock()
		lastByteAt = time.Now()
		lastMu.Unlock()
	}
	since := func() time.Duration {
		lastMu.Lock()
		defer lastMu.Unlock()
		return time.Since(lastByteAt)
	}

	// Typing the prompt: claude's TUI needs a moment to render before it can
	// accept input. A small initial delay plus per-character pacing keeps the
	// input box from dropping characters. End with CR to submit.
	go func() {
		time.Sleep(500 * time.Millisecond)
		for _, r := range prompt {
			if _, err := ptmx.Write([]byte(string(r))); err != nil {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
		// Some TUI builds require Enter rather than CR; \r is the standard
		// terminal submit and is what claude's TUI expects.
		_, _ = ptmx.Write([]byte("\r"))
		// The prompt itself was a burst of writes that bumped lastByteAt via
		// the reader echoing them back. Reset the silence baseline now so the
		// window measures from after-submit, not from process start.
		bump()
	}()

	// Reader: pulls bytes from the PTY, strips ANSI, appends plain text to the
	// output buffer, and bumps the activity timestamp. Streams MessageText
	// chunks on line boundaries so the daemon UI can render live progress.
	var (
		outMu  sync.Mutex
		output strings.Builder
		pend   strings.Builder // partial line buffer
	)
	flushLine := func(line string) {
		if line == "" {
			return
		}
		outMu.Lock()
		output.WriteString(line)
		output.WriteByte('\n')
		outMu.Unlock()
		trySend(msgCh, Message{Type: MessageText, Content: line})
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				bump()
				clean := ansiEscape.ReplaceAllString(string(buf[:n]), "")
				pend.WriteString(clean)
				for {
					s := pend.String()
					idx := strings.IndexByte(s, '\n')
					if idx < 0 {
						break
					}
					flushLine(strings.TrimRight(s[:idx], " \t"))
					pend.Reset()
					pend.WriteString(s[idx+1:])
				}
			}
			if err != nil {
				// io.EOF or read on closed PTY — reader exits, watcher and
				// Wait() handle teardown.
				return
			}
		}
	}()

	// Watcher: declares completion when the silence window elapses, fires the
	// hard timeout, or observes external cancellation. Whoever wins kills the
	// PTY/process; cmd.Wait() then unblocks for final cleanup.
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() { _ = ptmx.Close() }()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		decided := false
		for !decided {
			select {
			case <-runCtx.Done():
				if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("claude tui timed out after %s", hardTimeout)
				} else {
					finalStatus = "aborted"
					finalError = "execution cancelled"
				}
				decided = true
			case <-ticker.C:
				if since() >= silenceWindow {
					finalStatus = "completed"
					decided = true
				}
			}
		}

		// Send SIGINT to the TUI so it has a chance to write any final lines
		// before the PTY closes. If the process doesn't exit within WaitDelay,
		// CommandContext's cancel will kill it.
		if cmd.Process != nil {
			_ = cmd.Process.Signal(claudeTUIInterruptSignal())
		}
		// Give the reader a brief grace period to drain trailing bytes, then
		// force the PTY closed so Read returns and readerDone fires.
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
		}
		_ = ptmx.Close()
		<-readerDone

		// Flush any trailing partial line.
		if tail := strings.TrimRight(pend.String(), " \t\r\n"); tail != "" {
			flushLine(tail)
		}

		// Reap the process. We don't propagate a non-zero exit to finalStatus
		// when we ended on silence — TUI mode exits non-zero on SIGINT, which
		// is the expected teardown path here.
		_ = cmd.Wait()

		duration := time.Since(startTime)
		b.cfg.Logger.Info("claude tui finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		outMu.Lock()
		outText := output.String()
		outMu.Unlock()

		resCh <- Result{
			Status:     finalStatus,
			Output:     outText,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  "", // not exposed by TUI mode
			Usage:      map[string]TokenUsage{},
		}
	}()

	_ = io.Discard // reserved for future stderr piping if needed
	return &Session{Messages: msgCh, Result: resCh}, nil
}
