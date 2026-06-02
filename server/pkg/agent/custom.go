package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const CustomPromptPlaceholder = "{{prompt}}"

// CustomInvocation describes a generic local CLI invocation supplied by the
// daemon rather than compiled into Multica. V1 is intentionally small: it is
// an environment-variable bridge for commands like `king -p "{{prompt}}"`,
// not a full provider plugin system. Keeping this runtime-neutral shape here
// gives a future server/Web-backed custom-runtime editor a stable contract to
// persist and hand back to the daemon without reworking the execution layer.
type CustomInvocation struct {
	Args []string
}

type customBackend struct {
	cfg Config
}

func (b *customBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if b.cfg.Custom == nil {
		return nil, fmt.Errorf("custom invocation is required")
	}
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		return nil, fmt.Errorf("custom executable path is required")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("custom executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args, promptInArgs := buildCustomArgs(b.cfg.Custom.Args, prompt)
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	cmd.WaitDelay = 10 * time.Second

	logger := b.cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("agent command", "exec", execPath, "args", args, "custom", true)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("custom stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(logger, "[custom:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	var stdin io.WriteCloser
	if !promptInArgs {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("custom stdin pipe: %w", err)
		}
	}

	if err := cmd.Start(); err != nil {
		if stdin != nil {
			_ = stdin.Close()
		}
		cancel()
		return nil, fmt.Errorf("start custom agent: %w", err)
	}
	logger.Info("custom agent started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 1)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		stdoutDone := make(chan readAllResult, 1)
		go func() {
			data, err := io.ReadAll(stdout)
			stdoutDone <- readAllResult{data: data, err: err}
		}()

		var stdinErr error
		if stdin != nil {
			if _, err := io.WriteString(stdin, prompt); err != nil {
				stdinErr = err
			}
			if err := stdin.Close(); err != nil && stdinErr == nil {
				stdinErr = err
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)
		stdoutResult := <-stdoutDone
		output := string(stdoutResult.data)
		if output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: output})
		}

		status := "completed"
		errMsg := ""
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			status = "timeout"
			errMsg = fmt.Sprintf("custom agent timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			status = "aborted"
			errMsg = "execution cancelled"
		case stdinErr != nil:
			status = "failed"
			errMsg = fmt.Sprintf("write custom prompt: %v", stdinErr)
		case stdoutResult.err != nil:
			status = "failed"
			errMsg = fmt.Sprintf("read custom stdout: %v", stdoutResult.err)
		case exitErr != nil:
			status = "failed"
			errMsg = fmt.Sprintf("custom agent exited with error: %v", exitErr)
		}
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "custom", stderrBuf.Tail())
		}

		logger.Info("custom agent finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status:     status,
			Output:     output,
			Error:      errMsg,
			DurationMs: duration.Milliseconds(),
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type readAllResult struct {
	data []byte
	err  error
}

func buildCustomArgs(args []string, prompt string) ([]string, bool) {
	out := make([]string, 0, len(args))
	promptInArgs := false
	for _, arg := range args {
		if strings.Contains(arg, CustomPromptPlaceholder) {
			promptInArgs = true
			arg = strings.ReplaceAll(arg, CustomPromptPlaceholder, prompt)
		}
		out = append(out, arg)
	}
	return out, promptInArgs
}
