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

// droidBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var droidBlockedArgs = map[string]blockedArgMode{
	"--output-format":           blockedWithValue,  // stream-json protocol
	"--input-format":            blockedWithValue,  // exec prompt mode
	"--auto":                    blockedWithValue,  // daemon sets high autonomy
	"--skip-permissions-unsafe": blockedStandalone, // mutually exclusive with --auto
	"--cwd":                     blockedWithValue,  // task workdir anchor
	"-m":                        blockedWithValue,  // owned by ExecOptions.Model
	"--model":                   blockedWithValue,
	"-r":                        blockedWithValue, // owned by ExecOptions.ThinkingLevel
	"--reasoning-effort":        blockedWithValue,
	"-s":                        blockedWithValue, // owned by ExecOptions.ResumeSessionID
	"--session-id":              blockedWithValue,
	"--append-system-prompt":    blockedWithValue, // owned by ExecOptions.SystemPrompt
}

// droidBackend implements Backend by spawning `droid exec --output-format
// stream-json` and parsing Factory's newline-delimited JSON event stream.
type droidBackend struct {
	cfg Config
}

func buildDroidArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"exec",
		"--output-format", "stream-json",
		"--auto", "high",
	}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--reasoning-effort", opts.ThinkingLevel)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session-id", opts.ResumeSessionID)
	}
	if opts.MaxTurns > 0 {
		logger.Warn("droid does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, droidBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, droidBlockedArgs, logger)...)
	args = append(args, prompt)
	return args
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
	runCtx, cancel := runContext(ctx, timeout)

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
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[droid:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

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
		} else if runCtx.Err() == context.Canceled && scanResult.status == "completed" {
			// stream-json emits completion before the process exits; context
			// cancellation after completion is expected.
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("droid exited with error: %v", exitErr)
		}
		if scanResult.errMsg != "" {
			scanResult.errMsg = withAgentStderr(scanResult.errMsg, "droid", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("droid finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = scanResult.model
			}
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type droidScanResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	model     string
	usage     TokenUsage
}

type droidStreamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	Role      string          `json:"role"`
	Text      string          `json:"text"`
	FinalText string          `json:"finalText"`
	SessionID string          `json:"session_id"`
	Model     string          `json:"model"`
	IsError   bool            `json:"is_error"`
	Error     string          `json:"error"`
	Tool      string          `json:"tool"`
	ToolName  string          `json:"tool_name"`
	CallID    string          `json:"call_id"`
	Input     json.RawMessage `json:"input"`
	Output    string          `json:"output"`
	Usage     *droidUsage     `json:"usage"`
}

type droidUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func (b *droidBackend) processEvents(r io.Reader, msgCh chan<- Message) droidScanResult {
	result := droidScanResult{status: "completed"}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var output strings.Builder

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
		if evt.Model != "" {
			result.model = evt.Model
		}

		switch evt.Type {
		case "system":
			if evt.Subtype == "init" {
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: evt.SessionID})
			}
			if evt.Subtype == "error" || evt.IsError {
				errMsg := droidErrorText(&evt)
				if errMsg != "" {
					trySend(msgCh, Message{Type: MessageError, Content: errMsg})
				}
			}

		case "message":
			if evt.Role == "assistant" && evt.Text != "" {
				output.WriteString(evt.Text)
				trySend(msgCh, Message{Type: MessageText, Content: evt.Text, SessionID: evt.SessionID})
			}

		case "tool_use", "tool-use":
			tool := evt.Tool
			if tool == "" {
				tool = evt.ToolName
			}
			var params map[string]any
			if len(evt.Input) > 0 {
				_ = json.Unmarshal(evt.Input, &params)
			}
			trySend(msgCh, Message{
				Type:   MessageToolUse,
				Tool:   tool,
				CallID: evt.CallID,
				Input:  params,
			})

		case "tool_result", "tool-result":
			trySend(msgCh, Message{
				Type:   MessageToolResult,
				CallID: evt.CallID,
				Output: evt.Output,
			})

		case "completion":
			if evt.FinalText != "" {
				if output.Len() == 0 {
					output.WriteString(evt.FinalText)
				}
			}
			if evt.IsError {
				result.status = "failed"
				result.errMsg = droidErrorText(&evt)
			}
			if evt.Usage != nil {
				result.usage = droidTokenUsage(evt.Usage)
			}
			if evt.SessionID != "" {
				result.sessionID = evt.SessionID
			}

		case "error":
			result.status = "failed"
			errMsg := droidErrorText(&evt)
			if errMsg != "" {
				result.errMsg = errMsg
				trySend(msgCh, Message{Type: MessageError, Content: errMsg})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		b.cfg.Logger.Warn("droid stdout scanner error", "err", err)
	}

	result.output = output.String()
	if result.status == "completed" && strings.TrimSpace(result.output) == "" && result.errMsg == "" {
		result.status = "failed"
		result.errMsg = "droid produced no output"
	}

	return result
}

func droidErrorText(evt *droidStreamEvent) string {
	if evt.Error != "" {
		return evt.Error
	}
	if evt.Text != "" {
		return evt.Text
	}
	if evt.FinalText != "" {
		return evt.FinalText
	}
	return ""
}

func droidTokenUsage(u *droidUsage) TokenUsage {
	return TokenUsage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}
