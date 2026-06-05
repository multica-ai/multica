package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// qoderclicnBackend implements Backend by spawning Qoder CLI CN
// (`qoderclicn`) in non-interactive stream-json mode.
type qoderclicnBackend struct {
	cfg Config
}

func (b *qoderclicnBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "qoderclicn"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("qoderclicn executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	var mcpConfigPath string
	var mcpFileCleanup func()
	if len(opts.McpConfig) > 0 {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			cancel()
			return nil, err
		}
		mcpConfigPath = path
		mcpFileCleanup = func() { os.Remove(mcpConfigPath) }
	}
	defer func() {
		if mcpFileCleanup != nil {
			mcpFileCleanup()
		}
	}()

	args := buildQoderclicnArgs(prompt, opts, mcpConfigPath, b.cfg.Logger)

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
		return nil, fmt.Errorf("qoderclicn stdout pipe: %w", err)
	}
	stderrWriter := io.Writer(newLogWriter(b.cfg.Logger, "[qoderclicn:stderr] "))
	if opts.TraceCallback != nil {
		stderrWriter = io.MultiWriter(stderrWriter, newTraceWriter("raw_stderr", opts.TraceCallback))
	}
	stderrBuf := newStderrTail(stderrWriter, agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qoderclicn: %w", err)
	}

	b.cfg.Logger.Info("qoderclicn started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)
	mcpFileCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer os.Remove(mcpConfigPath)
		}

		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			rawLine := scanner.Text()
			if opts.TraceCallback != nil {
				opts.TraceCallback("raw_stdout", rawLine, "")
			}
			line := strings.TrimSpace(rawLine)
			if line == "" {
				continue
			}
			if opts.TraceCallback != nil {
				opts.TraceCallback("provider_event", "", line)
			}

			var evt qoderclicnStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}
			if evt.SessionID != "" {
				sessionID = evt.SessionID
			}

			switch evt.Type {
			case "system":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
				emitDisplayEvent(opts.TraceCallback, "status", "qoderclicn", "running", nil)
			case "assistant":
				handleQoderclicnAssistant(evt, msgCh, &output, usage, opts.TraceCallback)
			case "user":
				handleQoderclicnUser(evt, msgCh, opts.TraceCallback)
			case "result":
				if evt.SessionID != "" {
					sessionID = evt.SessionID
				}
				if evt.ResultText != "" {
					output.Reset()
					output.WriteString(evt.ResultText)
				}
				if resultUsage := qoderclicnResultUsage(evt, opts.Model); len(resultUsage) > 0 {
					usage = resultUsage
				}
				if evt.IsError || evt.Subtype == "error" {
					finalStatus = "failed"
					finalError = evt.ResultText
					if finalError == "" {
						finalError = evt.ErrorText
					}
				}
				emitDisplayEvent(opts.TraceCallback, "status", "qoderclicn", finalStatus, map[string]any{"error": finalError})
			case "log":
				if evt.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   evt.Log.Level,
						Content: evt.Log.Message,
					})
				}
			case "error":
				if evt.ErrorText != "" {
					finalStatus = "failed"
					finalError = evt.ErrorText
					trySend(msgCh, Message{Type: MessageError, Content: evt.ErrorText})
				}
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("qoderclicn stdout scanner error", "err", err)
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("qoderclicn timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("qoderclicn exited with error: %v", exitErr)
		}
		if finalError != "" {
			finalError = withAgentStderr(finalError, "qoderclicn", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("qoderclicn finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed"),
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

var qoderclicnBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedStandalone,
	"--print":                        blockedStandalone,
	"--output-format":                blockedWithValue,
	"-o":                             blockedWithValue,
	"--input-format":                 blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone,
	"--permission-mode":              blockedWithValue,
	"-c":                             blockedStandalone,
	"--continue":                     blockedStandalone,
	"-r":                             blockedWithValue,
	"--resume":                       blockedWithValue,
	"--session-id":                   blockedWithValue,
	"-w":                             blockedWithValue,
	"--cwd":                          blockedWithValue,
	"--mcp-config":                   blockedWithValue,
	"--strict-mcp-config":            blockedStandalone,
	"--":                             blockedWithValue,
}

func buildQoderclicnArgs(prompt string, opts ExecOptions, mcpConfigPath string, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--permission-mode", "bypass_permissions",
		"--dangerously-skip-permissions",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Cwd != "" {
		args = append(args, "--cwd", filepath.Clean(opts.Cwd))
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if opts.MaxTurns > 0 {
		// qoderclicn has no --max-turns equivalent; the daemon timeout remains
		// the execution guard.
	}
	if opts.ThinkingLevel != "" {
		// qoderclicn exposes --reasoning-effort, but its accepted catalog is
		// not yet surfaced through model discovery. Leave runtime_config
		// thinking_level ignored until the UI can advertise valid values.
	}
	if mcpConfigPath != "" {
		args = append(args, "--mcp-config", mcpConfigPath, "--strict-mcp-config")
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, qoderclicnBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, qoderclicnBlockedArgs, logger)...)
	args = append(args, "--", prompt)
	return args
}

func handleQoderclicnAssistant(
	evt qoderclicnStreamEvent,
	ch chan<- Message,
	output *strings.Builder,
	usage map[string]TokenUsage,
	trace TraceCallback,
) {
	var content qoderclicnAssistantMessage
	if err := json.Unmarshal(evt.Message, &content); err != nil {
		return
	}

	if content.Usage != nil && content.Model != "" {
		u := usage[content.Model]
		u.InputTokens += content.Usage.inputTokens()
		u.OutputTokens += content.Usage.outputTokens()
		u.CacheReadTokens += content.Usage.cacheReadTokens()
		u.CacheWriteTokens += content.Usage.cacheWriteTokens()
		usage[content.Model] = u
	}

	for _, block := range content.Content {
		switch block.Type {
		case "text", "output_text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
				if trace != nil {
					trace("normalized", block.Text, "")
				}
				emitDisplayEvent(trace, "assistant_text", "qoderclicn", block.Text, nil)
			}
		case "thinking":
			text := block.Thinking
			if text == "" {
				text = block.Text
			}
			if text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: text})
				emitDisplayEvent(trace, "thinking", "Thinking", text, nil)
			}
		case "tool_use":
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			emitClaudeToolUse(ch, trace, block.Name, block.ID, input, map[string]any{"provider": "qoderclicn"})
		}
	}
}

func handleQoderclicnUser(evt qoderclicnStreamEvent, ch chan<- Message, trace TraceCallback) {
	var content qoderclicnAssistantMessage
	if err := json.Unmarshal(evt.Message, &content); err != nil {
		return
	}
	for _, block := range content.Content {
		if block.Type != "tool_result" {
			continue
		}
		resultStr := ""
		if block.Content != nil {
			resultStr = string(block.Content)
		}
		trySend(ch, Message{
			Type:   MessageToolResult,
			CallID: block.ToolUseID,
			Output: resultStr,
		})
		if trace != nil {
			trace("normalized", "[tool_result: "+block.ToolUseID+"]", resultStr)
		}
		emitDisplayEvent(trace, "tool_result", "Tool result", resultStr, map[string]any{"call_id": block.ToolUseID, "provider": "qoderclicn"})
	}
}

type qoderclicnStreamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`

	ResultText string `json:"result,omitempty"`
	ErrorText  string `json:"error,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`

	Usage      *qoderclicnUsage                      `json:"usage,omitempty"`
	ModelUsage map[string]qoderclicnResultModelUsage `json:"modelUsage,omitempty"`
	Log        *claudeLogEntry                       `json:"log,omitempty"`
}

type qoderclicnAssistantMessage struct {
	Role    string                   `json:"role"`
	Model   string                   `json:"model"`
	Content []qoderclicnContentBlock `json:"content"`
	Usage   *qoderclicnUsage         `json:"usage,omitempty"`
}

type qoderclicnContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type qoderclicnUsage struct {
	InputTokensSnake              int64 `json:"input_tokens"`
	OutputTokensSnake             int64 `json:"output_tokens"`
	CacheReadInputTokensSnake     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokensSnake int64 `json:"cache_creation_input_tokens"`

	InputTokensCamel              int64 `json:"inputTokens"`
	OutputTokensCamel             int64 `json:"outputTokens"`
	CacheReadInputTokensCamel     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokensCamel int64 `json:"cacheCreationInputTokens"`
}

func (u qoderclicnUsage) inputTokens() int64 {
	if u.InputTokensCamel != 0 {
		return u.InputTokensCamel
	}
	return u.InputTokensSnake
}

func (u qoderclicnUsage) outputTokens() int64 {
	if u.OutputTokensCamel != 0 {
		return u.OutputTokensCamel
	}
	return u.OutputTokensSnake
}

func (u qoderclicnUsage) cacheReadTokens() int64 {
	if u.CacheReadInputTokensCamel != 0 {
		return u.CacheReadInputTokensCamel
	}
	return u.CacheReadInputTokensSnake
}

func (u qoderclicnUsage) cacheWriteTokens() int64 {
	if u.CacheCreationInputTokensCamel != 0 {
		return u.CacheCreationInputTokensCamel
	}
	return u.CacheCreationInputTokensSnake
}

type qoderclicnResultModelUsage = qoderclicnUsage

func qoderclicnResultUsage(evt qoderclicnStreamEvent, fallbackModel string) map[string]TokenUsage {
	if len(evt.ModelUsage) > 0 {
		usage := make(map[string]TokenUsage, len(evt.ModelUsage))
		for model, u := range evt.ModelUsage {
			if model == "" || !claudeUsageHasTokens(u.inputTokens(), u.outputTokens(), u.cacheReadTokens(), u.cacheWriteTokens()) {
				continue
			}
			usage[model] = TokenUsage{
				InputTokens:      u.inputTokens(),
				OutputTokens:     u.outputTokens(),
				CacheReadTokens:  u.cacheReadTokens(),
				CacheWriteTokens: u.cacheWriteTokens(),
			}
		}
		if len(usage) > 0 {
			return usage
		}
	}

	model := evt.Model
	if model == "" {
		model = fallbackModel
	}
	if evt.Usage == nil || model == "" || !claudeUsageHasTokens(
		evt.Usage.inputTokens(),
		evt.Usage.outputTokens(),
		evt.Usage.cacheReadTokens(),
		evt.Usage.cacheWriteTokens(),
	) {
		return nil
	}
	return map[string]TokenUsage{
		model: {
			InputTokens:      evt.Usage.inputTokens(),
			OutputTokens:     evt.Usage.outputTokens(),
			CacheReadTokens:  evt.Usage.cacheReadTokens(),
			CacheWriteTokens: evt.Usage.cacheWriteTokens(),
		},
	}
}
