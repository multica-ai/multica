package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const CustomPromptPlaceholder = "{{prompt}}"
const CustomSessionIDPlaceholder = "{{session_id}}"

// CustomInvocation describes a generic local CLI invocation supplied by the
// daemon rather than compiled into Multica. Args drive the first turn. When
// Multica has a prior provider session id, ResumeArgs can use
// {{session_id}} plus {{prompt}} to continue that provider-native session.
// SessionIDRegex lets the daemon extract the provider's real session id from
// stdout, keeping the contract runtime-neutral instead of baking in one
// custom CLI's stream format.
type CustomInvocation struct {
	Args           []string
	ResumeArgs     []string
	SessionIDRegex string
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
	sessionIDPattern, err := compileCustomSessionIDRegex(b.cfg.Custom.SessionIDRegex)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	templateArgs := b.cfg.Custom.Args
	if opts.ResumeSessionID != "" && len(b.cfg.Custom.ResumeArgs) > 0 {
		templateArgs = b.cfg.Custom.ResumeArgs
	}
	args, promptInArgs := buildCustomArgs(templateArgs, prompt, opts.ResumeSessionID)
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

		stdoutResult := <-stdoutDone
		exitErr := cmd.Wait()
		duration := time.Since(startTime)
		output := string(stdoutResult.data)
		sessionID := extractCustomSessionID(output, sessionIDPattern)
		if sessionID == "" {
			sessionID = opts.ResumeSessionID
		}
		if output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: output, SessionID: sessionID})
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
			SessionID:  sessionID,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type readAllResult struct {
	data []byte
	err  error
}

func buildCustomArgs(args []string, prompt, sessionID string) ([]string, bool) {
	out := make([]string, 0, len(args))
	promptInArgs := false
	for _, arg := range args {
		if strings.Contains(arg, CustomPromptPlaceholder) {
			promptInArgs = true
			arg = strings.ReplaceAll(arg, CustomPromptPlaceholder, prompt)
		}
		if strings.Contains(arg, CustomSessionIDPlaceholder) {
			arg = strings.ReplaceAll(arg, CustomSessionIDPlaceholder, sessionID)
		}
		out = append(out, arg)
	}
	return out, promptInArgs
}

func compileCustomSessionIDRegex(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid custom session id regex: %w", err)
	}
	return re, nil
}

func extractCustomSessionID(output string, re *regexp.Regexp) string {
	if re == nil || output == "" {
		return ""
	}
	match := re.FindStringSubmatch(output)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if len(match) == 1 {
		return strings.TrimSpace(match[0])
	}
	return ""
}
