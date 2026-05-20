package agent

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// geminiBackend implements Backend by spawning Google's AGY CLI
// in print mode and streaming plain stdout back to Multica.
type geminiBackend struct {
	cfg Config
}

func (b *geminiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "agy"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("agy executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildGeminiArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildGeminiEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[agy:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so scanner.Scan() unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
		for scanner.Scan() {
			line := scanner.Text()
			if output.Len() > 0 {
				output.WriteByte('\n')
				trySend(msgCh, Message{Type: MessageText, Content: "\n"})
			}
			output.WriteString(line)
			if line != "" {
				trySend(msgCh, Message{Type: MessageText, Content: line})
			}
		}
		if err := scanner.Err(); err != nil && runCtx.Err() == nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("read agy output: %v", err)
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		} else if authErr := agyAuthError(output.String()); authErr != "" && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = authErr
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func agyAuthError(output string) string {
	normalized := strings.ToLower(output)
	if strings.Contains(normalized, "authentication required") ||
		strings.Contains(normalized, "authentication timed out") {
		return "agy authentication required"
	}
	return ""
}

// ── Arg builder ──

// buildGeminiArgs assembles the argv for a one-shot AGY invocation.
//
// Flags:
//
//	-p / --prompt         non-interactive prompt (the user's task)
//	--dangerously-skip-permissions
//	                      auto-approve all tool executions
//	--conversation <id>   resume a previous session (if provided)
//
// geminiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var geminiBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,  // non-interactive prompt
	"--print":                        blockedWithValue,  // non-interactive prompt
	"--prompt":                       blockedWithValue,  // alias for --print
	"-i":                             blockedWithValue,  // interactive prompt mode
	"--prompt-interactive":           blockedWithValue,  // interactive prompt mode
	"-c":                             blockedStandalone, // continue mode
	"--continue":                     blockedStandalone, // continue mode
	"--conversation":                 blockedWithValue,  // daemon-managed resume
	"--dangerously-skip-permissions": blockedStandalone, // auto-approve tool use
	"--yolo":                         blockedStandalone, // legacy Gemini spelling
	"-m":                             blockedWithValue,  // legacy Gemini model flag; AGY uses settings
	"-r":                             blockedWithValue,  // legacy Gemini resume flag
	"-o":                             blockedWithValue,  // legacy Gemini output format
	"--output-format":                blockedWithValue,  // legacy Gemini output format
}

func buildGeminiArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
	}
	if opts.Model != "" {
		logger.Warn("AGY CLI does not expose a model selection flag; ignoring runtime model override", "model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, geminiBlockedArgs, logger)...)
	return args
}

// buildGeminiEnv wraps buildEnv and defaults GEMINI_CLI_TRUST_WORKSPACE=true so
// legacy Gemini CLI folder-trust gates do not fail every headless daemon
// invocation with exit code 55 (FatalUntrustedWorkspaceError). AGY stores its
// persistent settings under `~/.gemini/antigravity-cli/settings.json` and the
// daemon supplies AGY's headless permission bypass on the command line, but this
// environment default remains harmless and preserves compatibility for older
// Gemini installations and user-pinned MULTICA_GEMINI_PATH values.
//
// If the caller explicitly sets the same key in cfg.Env it wins, preserving the
// ability to opt back into the check.
func buildGeminiEnv(extra map[string]string) []string {
	const trustKey = "GEMINI_CLI_TRUST_WORKSPACE"
	if _, ok := extra[trustKey]; ok {
		return buildEnv(extra)
	}
	merged := make(map[string]string, len(extra)+1)
	for k, v := range extra {
		merged[k] = v
	}
	merged[trustKey] = "true"
	return buildEnv(merged)
}
