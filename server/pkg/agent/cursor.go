package agent

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// cursorBackend implements Backend by spawning `cursor-agent` as a one-shot
// subprocess and streaming its stdout lines back to the caller.
type cursorBackend struct {
	cfg Config
}

func (b *cursorBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "cursor-agent"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("cursor-agent executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	taskPrompt := prompt
	if opts.SystemPrompt != "" {
		taskPrompt = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}

	args := []string{"--print", "--force", "--trust", "--sandbox", "disabled", taskPrompt}

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cursor-agent stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[cursor:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start cursor-agent: %w", err)
	}

	b.cfg.Logger.Info("cursor-agent started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()

		var output strings.Builder
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			trySend(msgCh, Message{Type: MessageText, Content: line})
			if output.Len() > 0 {
				output.WriteByte('\n')
			}
			output.WriteString(line)
		}

		readErr := scanner.Err()
		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		result := Result{
			Status:     "completed",
			Output:     output.String(),
			DurationMs: duration.Milliseconds(),
		}

		if runCtx.Err() == context.DeadlineExceeded {
			result.Status = "timeout"
			result.Error = fmt.Sprintf("cursor-agent timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			result.Status = "aborted"
			result.Error = "execution cancelled"
		} else if readErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("read stdout: %v", readErr)
		} else if waitErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("cursor-agent exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("cursor-agent finished", "pid", cmd.Process.Pid, "status", result.Status, "duration", duration.Round(time.Millisecond).String())

		resCh <- result
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
