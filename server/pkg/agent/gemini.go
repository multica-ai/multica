package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// geminiBackend implements Backend by spawning the Google Gemini CLI
// (`gemini -p <prompt> --yolo -o text`) and collecting its stdout.
//
// This is a minimal v1 implementation — it captures the final text
// response but does not stream tool calls in real time. Follow-ups
// can move to `-o stream-json` and parse Gemini's event schema once
// we have a reliable reproduction of its output format.
type geminiBackend struct {
	cfg Config
}

func (b *geminiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "gemini"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("gemini executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildGeminiArgs(prompt, opts)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gemini stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[gemini:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start gemini: %w", err)
	}

	b.cfg.Logger.Info("gemini started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 16)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		output, readErr := io.ReadAll(bufio.NewReader(stdout))

		// Forward the full response as a single text message so the daemon
		// can persist it verbatim. Tool streaming is intentionally omitted
		// in v1; see the file-level comment.
		text := strings.TrimRight(string(output), "\n")
		if text != "" {
			trySend(msgCh, Message{Type: MessageText, Content: text})
		}

		waitErr := cmd.Wait()
		durationMs := time.Since(startTime).Milliseconds()

		result := Result{
			Status:     "completed",
			Output:     text,
			DurationMs: durationMs,
		}

		if readErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("read stdout: %s", readErr.Error())
		} else if waitErr != nil {
			// Distinguish context cancellation (timeout) from exit errors.
			if runCtx.Err() == context.DeadlineExceeded {
				result.Status = "timeout"
				result.Error = fmt.Sprintf("gemini timed out after %s", timeout)
			} else {
				result.Status = "failed"
				result.Error = waitErr.Error()
			}
		}

		resCh <- result
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// buildGeminiArgs assembles the argv for a one-shot gemini invocation.
//
// Flags:
//
//	-p / --prompt         non-interactive prompt (the user's task)
//	--yolo                auto-approve all tool executions (equivalent to
//	                      claude's --permission-mode bypassPermissions)
//	-o text               plain text output (stream-json is a follow-up)
//	-m <model>            optional model override (from MULTICA_GEMINI_MODEL)
//	-r <session>          resume a previous session (if provided)
//
// Note: gemini reads stdin and appends it to -p when both are present.
// The daemon does not pipe stdin, so the prompt comes exclusively from -p.
func buildGeminiArgs(prompt string, opts ExecOptions) []string {
	args := []string{
		"-p", prompt,
		"--yolo",
		"-o", "text",
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-r", opts.ResumeSessionID)
	}
	return args
}
