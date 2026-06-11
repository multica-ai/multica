package agent

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// antigravityBackend implements Backend by spawning Google's Antigravity CLI
// (`agy -p <prompt>`) in non-interactive print mode. Unlike Claude / Codex /
// Cursor / Gemini, the Antigravity CLI does not expose a structured event
// stream — stdout is plain assistant text (intermediate "I will run X" lines
// and the final reply, all interleaved). The backend therefore streams stdout
// line-by-line as `MessageText` events and accumulates the same text as the
// final `Result.Output`.
//
// Session resumption uses `--conversation <id>`. The conversation id is not
// emitted on stdout; we capture it by routing `--log-file` to a temp file and
// scanning its glog-formatted lines for the `conversation=<uuid>` token that
// printmode.go logs at message-send time.
type antigravityBackend struct {
	cfg Config
}

func (b *antigravityBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "agy"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("agy executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	logFile, err := os.CreateTemp("", "multica-agy-log-*.log")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create agy log file: %w", err)
	}
	logPath := logFile.Name()
	_ = logFile.Close()

	args := buildAntigravityArgs(prompt, logPath, timeout, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[agy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer os.Remove(logPath)

		startTime := time.Now()
		var output strings.Builder
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		for scanner.Scan() {
			line := scanner.Text()
			if output.Len() > 0 {
				output.WriteByte('\n')
			}
			output.WriteString(line)
			if strings.TrimSpace(line) != "" {
				trySend(msgCh, Message{Type: MessageText, Content: line})
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("agy stdout scanner error", "err", err)
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		sessionID := readAntigravityConversationID(logPath)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		}
		if finalError != "" {
			finalError = withAgentStderr(finalError, "agy", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			// The Antigravity CLI doesn't surface per-turn token usage today;
			// leave Usage empty rather than report misleading zeros under a
			// guessed model name.
			Usage: map[string]TokenUsage{},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// antigravityConversationIDRe matches the glog line printmode.go writes when
// the CLI dispatches the user's message — the only place in the log that
// reliably surfaces the conversation UUID for both fresh and resumed turns.
//
// Example: `I0528 13:36:23.318877 73304 printmode.go:130] Print mode:
// conversation=b8b263a4-4b2f-4339-acc9-78b248e2b606, sending message`
var antigravityConversationIDRe = regexp.MustCompile(
	`conversation=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`,
)

// readAntigravityConversationID scans the per-run log file for the
// conversation UUID. Best-effort: returns "" if the log file is missing, the
// CLI exited before dispatching, or the format changes upstream.
func readAntigravityConversationID(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := antigravityConversationIDRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	// The CLI logs the conversation id repeatedly during a turn (one entry
	// per dispatched message, plus stream-update lines). Any non-empty UUID
	// in the file resolves to the same conversation, so the last match wins
	// — that's what `--conversation` should be pinned to next turn.
	return string(matches[len(matches)-1][1])
}

// antigravityBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// non-interactive operation or the daemon's session-resume bookkeeping.
var antigravityBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,
	"--print":                        blockedWithValue,
	"--prompt":                       blockedWithValue,
	"-i":                             blockedStandalone, // interactive mode would block the daemon
	"--prompt-interactive":           blockedStandalone,
	"-c":                             blockedStandalone, // resume via --conversation, not --continue
	"--continue":                     blockedStandalone,
	"--conversation":                 blockedWithValue, // managed via ExecOptions.ResumeSessionID
	"--print-timeout":                blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone, // always-on in daemon mode
	"--log-file":                     blockedWithValue,  // daemon needs it for session capture
}

// buildAntigravityArgs assembles the argv for a one-shot agy invocation.
//
//	agy -p <prompt> --dangerously-skip-permissions --print-timeout <duration>
//	    --log-file <tmp> [--conversation <id>] [--add-dir <cwd>]
//
// The Antigravity CLI exposes neither --model nor --system-prompt today;
// model selection lives in the user's Antigravity settings, and runtime
// instructions are delivered via AGENTS.md in the task workdir.
func buildAntigravityArgs(prompt, logPath string, timeout time.Duration, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
	}
	// Only pass --print-timeout when a positive wall-clock cap is configured.
	// timeout <= 0 means "no cap" (MUL-3064): agy then runs without its own
	// print-timeout guillotine, matching every other backend's runContext
	// semantics. Passing antigravityFormatTimeout(0) would clamp to 1s and kill
	// the run almost immediately — the opposite of "no cap".
	if timeout > 0 {
		args = append(args, "--print-timeout", antigravityFormatTimeout(timeout))
	}
	args = append(args, "--log-file", logPath)
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--add-dir", filepath.Clean(opts.Cwd))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, antigravityBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, antigravityBlockedArgs, logger)...)
	return args
}

// antigravityFormatTimeout renders a Go duration in the `<n>m<n>s` shape the
// agy CLI accepts (e.g. 20m0s). Sub-second timeouts round up to 1s so the CLI
// doesn't reject the flag.
func antigravityFormatTimeout(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	// time.Duration.String() already produces shapes like "20m0s" / "1h30m0s"
	// that agy parses via Go's stdlib flag.Duration on the receiving side.
	return d.String()
}
