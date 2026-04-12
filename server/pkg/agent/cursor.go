package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// cursorBackend implements Backend by spawning the Cursor CLI agent
// with -p --force --output-format stream-json.
type cursorBackend struct {
	cfg Config
}

func (b *cursorBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "agent"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("cursor agent executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildCursorArgs(opts, b.cfg.Logger)

	// Cursor CLI doesn't support --append-system-prompt, so prepend it to the prompt.
	fullPrompt := prompt
	if opts.SystemPrompt != "" {
		fullPrompt = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}
	args = append(args, fullPrompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cursor stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[cursor:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start cursor: %w", err)
	}

	b.cfg.Logger.Info("cursor started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		var model string
		var usage *cursorUsage
		finalStatus := "completed"
		var finalError string

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var msg cursorStreamEvent
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			if msg.SessionID != "" {
				sessionID = msg.SessionID
			}

			switch msg.Type {
			case "system":
				if msg.Model != "" {
					model = msg.Model
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
			case "assistant":
				b.handleAssistantEvent(msg, msgCh, &output)
			case "tool_call":
				b.handleToolCallEvent(msg, msgCh)
			case "result":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				if msg.ResultText != "" {
					output.Reset()
					output.WriteString(msg.ResultText)
				}
				if msg.IsError {
					finalStatus = "failed"
					finalError = msg.ResultText
				}
				if msg.Usage != nil {
					usage = msg.Usage
				}
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("cursor timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("cursor exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("cursor finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		var usageMap map[string]TokenUsage
		if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
			modelKey := model
			if modelKey == "" {
				modelKey = opts.Model
			}
			if modelKey == "" {
				modelKey = "unknown"
			}
			usageMap = map[string]TokenUsage{modelKey: {
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
			}}
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *cursorBackend) handleAssistantEvent(msg cursorStreamEvent, ch chan<- Message, output *strings.Builder) {
	if msg.Message == nil {
		return
	}

	var content cursorMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

	for _, block := range content.Content {
		if block.Type == "text" && block.Text != "" {
			output.WriteString(block.Text)
			trySend(ch, Message{Type: MessageText, Content: block.Text})
		}
	}
}

func (b *cursorBackend) handleToolCallEvent(msg cursorStreamEvent, ch chan<- Message) {
	if msg.ToolCall == nil {
		return
	}

	switch msg.Subtype {
	case "started":
		toolName, input := extractCursorToolInfo(msg.ToolCall)
		trySend(ch, Message{
			Type:   MessageToolUse,
			Tool:   toolName,
			CallID: msg.CallID,
			Input:  input,
		})
	case "completed":
		toolName, _ := extractCursorToolInfo(msg.ToolCall)
		output := extractCursorToolResult(msg.ToolCall)
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   toolName,
			CallID: msg.CallID,
			Output: output,
		})
	}
}

// ── Cursor stream-json types ──

type cursorStreamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	ToolCall  json.RawMessage `json:"tool_call,omitempty"`
	Model     string          `json:"model,omitempty"`

	// result fields
	ResultText string       `json:"result,omitempty"`
	IsError    bool         `json:"is_error,omitempty"`
	DurationMs float64      `json:"duration_ms,omitempty"`
	Usage      *cursorUsage `json:"usage,omitempty"`
}

type cursorUsage struct {
	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	CacheReadTokens int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

type cursorMessageContent struct {
	Role    string               `json:"role"`
	Content []cursorContentBlock `json:"content"`
}

type cursorContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ── Tool call parsing ──

// extractCursorToolInfo extracts the tool name and input from a Cursor tool_call payload.
// Cursor uses typed tool calls: readToolCall, writeToolCall, or generic function.
func extractCursorToolInfo(raw json.RawMessage) (string, map[string]any) {
	var tc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tc); err != nil {
		return "unknown", nil
	}

	if data, ok := tc["readToolCall"]; ok {
		var call struct {
			Args map[string]any `json:"args"`
		}
		_ = json.Unmarshal(data, &call)
		return "read_file", call.Args
	}

	if data, ok := tc["writeToolCall"]; ok {
		var call struct {
			Args map[string]any `json:"args"`
		}
		_ = json.Unmarshal(data, &call)
		return "write_file", call.Args
	}

	if data, ok := tc["function"]; ok {
		var call struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		_ = json.Unmarshal(data, &call)
		var args map[string]any
		_ = json.Unmarshal([]byte(call.Arguments), &args)
		return call.Name, args
	}

	return "unknown", nil
}

// extractCursorToolResult extracts a human-readable result string from a completed tool_call.
func extractCursorToolResult(raw json.RawMessage) string {
	var tc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tc); err != nil {
		return ""
	}

	// Try readToolCall result
	if data, ok := tc["readToolCall"]; ok {
		var call struct {
			Result struct {
				Success struct {
					Content    string `json:"content"`
					TotalLines int    `json:"totalLines"`
				} `json:"success"`
			} `json:"result"`
		}
		if json.Unmarshal(data, &call) == nil && call.Result.Success.Content != "" {
			return call.Result.Success.Content
		}
	}

	// Try writeToolCall result
	if data, ok := tc["writeToolCall"]; ok {
		var call struct {
			Result struct {
				Success struct {
					Path         string `json:"path"`
					LinesCreated int    `json:"linesCreated"`
					FileSize     int    `json:"fileSize"`
				} `json:"success"`
			} `json:"result"`
		}
		if json.Unmarshal(data, &call) == nil && call.Result.Success.Path != "" {
			return fmt.Sprintf("wrote %s (%d lines, %d bytes)",
				call.Result.Success.Path, call.Result.Success.LinesCreated, call.Result.Success.FileSize)
		}
	}

	// Fallback: marshal the whole thing
	data, _ := json.Marshal(raw)
	return string(data)
}

// ── Arg builder ──

func buildCursorArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--force",
		"--trust",
		"--output-format", "stream-json",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		logger.Warn("cursor agent does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	return args
}
