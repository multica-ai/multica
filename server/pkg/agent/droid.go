package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type droidBackend struct {
	cfg Config
}

func (b *droidBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "droid"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("droid executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildDroidArgs(prompt, opts, b.cfg.Logger)
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
		return nil, fmt.Errorf("droid stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[droid:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start droid: %w", err)
	}

	b.cfg.Logger.Info("droid started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("droid timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("droid exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("droid finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      scanResult.usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type droidEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     map[string]TokenUsage
}

func (b *droidBackend) processEvents(r io.Reader, ch chan<- Message) droidEventResult {
	result := droidEventResult{
		status: "completed",
		usage:  map[string]TokenUsage{},
	}
	var output strings.Builder

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt droidStreamEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if evt.SessionID != "" {
			result.sessionID = evt.SessionID
		}

		switch evt.Type {
		case "system":
			if evt.Subtype == "init" {
				trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: result.sessionID})
			}
		case "message":
			if evt.Role == "assistant" && evt.Text != "" {
				output.WriteString(evt.Text)
				trySend(ch, Message{Type: MessageText, Content: evt.Text})
			}
		case "completion":
			if evt.FinalText != "" {
				result.output = evt.FinalText
			}
			if evt.Usage != nil {
				result.usage["droid"] = TokenUsage{
					InputTokens:      evt.Usage.InputTokens,
					OutputTokens:     evt.Usage.OutputTokens,
					CacheReadTokens:  evt.Usage.CacheReadInputTokens,
					CacheWriteTokens: evt.Usage.CacheCreationInputTokens,
				}
			}
		case "error":
			result.status = "failed"
			result.errMsg = evt.Message
			trySend(ch, Message{Type: MessageError, Content: evt.Message})
		}
	}

	if result.output == "" {
		result.output = output.String()
	}
	return result
}

type droidStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	Role      string `json:"role,omitempty"`
	Text      string `json:"text,omitempty"`
	Message   string `json:"message,omitempty"`
	FinalText string `json:"finalText,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Usage     *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage,omitempty"`
}

var droidBlockedArgs = map[string]blockedArgMode{
	"-o":                       blockedWithValue,
	"--output-format":          blockedWithValue,
	"--auto":                   blockedWithValue,
	"--skip-permissions-unsafe": blockedStandalone,
	"--cwd":                    blockedWithValue,
	"-w":                       blockedWithValue,
	"--worktree":               blockedWithValue,
	"--worktree-dir":           blockedWithValue,
	"-m":                       blockedWithValue,
	"--model":                  blockedWithValue,
	"-r":                       blockedWithValue,
	"--reasoning-effort":       blockedWithValue,
	"-s":                       blockedWithValue,
	"--session-id":             blockedWithValue,
	"--fork":                   blockedWithValue,
	"--append-system-prompt":   blockedWithValue,
	"--use-spec":               blockedStandalone,
	"--mission":                blockedStandalone,
}

func buildDroidArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"exec",
		"-o", "stream-json",
		"--auto", "medium",
	}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "-r", opts.ThinkingLevel)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session-id", opts.ResumeSessionID)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.MaxTurns > 0 {
		logger.Warn("droid does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if len(opts.McpConfig) > 0 {
		logger.Warn("droid does not support per-run MCP config injection; ignoring")
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, droidBlockedArgs, logger)...)
	args = append(args, prompt)
	return args
}
