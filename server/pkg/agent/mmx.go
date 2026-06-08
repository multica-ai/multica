package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// mmxBackend implements Backend by spawning MiniMax CLI (`mmx`) in
// non-interactive JSON mode. Unlike stream-json backends (claude,
// qoderclicn), mmx returns a single aggregated JSON response per call, so
// the daemon must drive the tool-use loop itself.
type mmxBackend struct {
	cfg Config
}

// mmxResponse mirrors the Anthropic Messages API compatible output that
// mmx --output json produces.
type mmxResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []mmxContentBlock  `json:"content"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      mmxUsage           `json:"usage"`
}

type mmxContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type mmxUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// mmxError mirrors the structured JSON error that mmx emits on failure.
type mmxError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint,omitempty"`
	} `json:"error"`
}

func (b *mmxBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "mmx"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("mmx executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		// Build initial messages array with system prompt and user message.
		messages := b.buildInitialMessages(prompt, opts)
		toolDefs := opts.McpConfig // not used in v1; reserved for future tool defs

		maxTurns := opts.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 50 // safety default
		}

		for turn := 0; turn < maxTurns; turn++ {
			resp, err := b.callMmx(runCtx, execPath, opts, messages, toolDefs)
			if err != nil {
				// Check if it's a rate-limit error that we should surface.
				if isMmxRateLimit(err) {
					finalStatus = "failed"
					finalError = fmt.Sprintf("mmx rate limited: %v", err)
				} else if isMmxAuthError(err) {
					finalStatus = "failed"
					finalError = fmt.Sprintf("mmx authentication error: %v", err)
				} else {
					finalStatus = "failed"
					finalError = fmt.Sprintf("mmx execution error: %v", err)
				}
				trySend(msgCh, Message{Type: MessageError, Content: finalError})
				break
			}

			// Accumulate usage.
			model := resp.Model
			if model == "" {
				model = opts.Model
			}
			if model != "" {
				u := usage[model]
				u.InputTokens += resp.Usage.InputTokens
				u.OutputTokens += resp.Usage.OutputTokens
				usage[model] = u
			}

			// Process content blocks.
			hasToolUse := false
			for _, block := range resp.Content {
				switch block.Type {
				case "text", "output_text":
					if block.Text != "" {
						output.WriteString(block.Text)
						trySend(msgCh, Message{Type: MessageText, Content: block.Text})
						if opts.TraceCallback != nil {
							opts.TraceCallback("normalized", block.Text, "")
						}
						emitDisplayEvent(opts.TraceCallback, "assistant_text", "mmx", block.Text, nil)
					}
				case "thinking":
					text := block.Thinking
					if text == "" {
						text = block.Text
					}
					if text != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: text})
						emitDisplayEvent(opts.TraceCallback, "thinking", "Thinking", text, nil)
					}
				case "tool_use":
					hasToolUse = true
					var input map[string]any
					if block.Input != nil {
						_ = json.Unmarshal(block.Input, &input)
					}
					emitClaudeToolUse(msgCh, opts.TraceCallback, block.Name, block.ID, input, map[string]any{"provider": "mmx"})

					// Execute the tool and append the result.
					toolResult := b.executeTool(block)
					messages = append(messages,
						map[string]any{"role": "assistant", "content": []map[string]any{
							{"type": "tool_use", "id": block.ID, "name": block.Name, "input": input},
						}},
						map[string]any{"role": "user", "content": []map[string]any{
							toolResult,
						}},
					)
					trySend(msgCh, Message{
						Type:   MessageToolResult,
						CallID: block.ID,
						Output: toolResult["content"].(string),
					})
				}
			}

			// Append assistant message to conversation history.
			if !hasToolUse {
				// No more tool calls — we're done.
				break
			}
		}

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("mmx timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("mmx finished", "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// callMmx invokes the mmx CLI with the given messages and returns the parsed response.
func (b *mmxBackend) callMmx(ctx context.Context, execPath string, opts ExecOptions, messages []map[string]any, toolDefs json.RawMessage) (*mmxResponse, error) {
	// Write messages to a temp file.
	msgJSON, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("marshal messages: %w", err)
	}
	tmpFile, err := os.CreateTemp("", "mmx-messages-*.json")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(msgJSON); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	args := buildMmxArgs(opts, tmpPath)

	cmd := exec.CommandContext(ctx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("mmx command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	// Capture both stdout and stderr.
	var stdout strings.Builder
	cmd.Stdout = &stdout
	stderrWriter := io.Writer(newLogWriter(b.cfg.Logger, "[mmx:stderr] "))
	if opts.TraceCallback != nil {
		stderrWriter = io.MultiWriter(stderrWriter, newTraceWriter("raw_stderr", opts.TraceCallback))
	}
	stderrBuf := newStderrTail(stderrWriter, agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Run(); err != nil {
		// Try to parse structured error from stdout first.
		raw := strings.TrimSpace(stdout.String())
		if raw != "" {
			var mmxErr mmxError
			if jsonErr := json.Unmarshal([]byte(raw), &mmxErr); jsonErr == nil && mmxErr.Error.Message != "" {
				return nil, &mmxExecError{
					Code:    mmxErr.Error.Code,
					Message: mmxErr.Error.Message,
					Hint:    mmxErr.Error.Hint,
					ExitErr: err,
				}
			}
		}
		// Fallback: include stderr tail.
		stderrTail := stderrBuf.Tail()
		return nil, fmt.Errorf("mmx exited with error: %w%s", err, formatStderrTail(stderrTail))
	}

	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		return nil, fmt.Errorf("mmx returned empty output")
	}

	if opts.TraceCallback != nil {
		opts.TraceCallback("raw_stdout", raw, "")
		opts.TraceCallback("provider_event", "", raw)
	}

	var resp mmxResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse mmx response: %w (raw: %s)", err, truncate(raw, 500))
	}

	return &resp, nil
}

// buildInitialMessages constructs the initial messages array for mmx.
func (b *mmxBackend) buildInitialMessages(prompt string, opts ExecOptions) []map[string]any {
	return []map[string]any{
		{"role": "user", "content": prompt},
	}
}

// buildMmxArgs constructs CLI arguments for the mmx command.
func buildMmxArgs(opts ExecOptions, messagesFile string) []string {
	args := []string{
		"text", "chat",
		"--messages-file", messagesFile,
		"--output", "json",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--system", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, mmxBlockedArgs, slog.Default())...)
	args = append(args, filterCustomArgs(opts.CustomArgs, mmxBlockedArgs, slog.Default())...)
	return args
}

// executeTool executes a tool call and returns a tool_result content block.
// In v1 this is a stub that returns a descriptive message; real tool execution
// is delegated to the daemon's built-in tools.
func (b *mmxBackend) executeTool(block mmxContentBlock) map[string]any {
	return map[string]any{
		"type":       "tool_result",
		"tool_use_id": block.ID,
		"content":    fmt.Sprintf("Tool %q executed (v1 stub, input: %s)", block.Name, truncate(string(block.Input), 200)),
	}
}

// mmxExecError wraps structured mmx errors with their error code.
type mmxExecError struct {
	Code    int
	Message string
	Hint    string
	ExitErr error
}

func (e *mmxExecError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s (code %d). %s", e.Message, e.Code, e.Hint)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

func (e *mmxExecError) Unwrap() error { return e.ExitErr }

func isMmxRateLimit(err error) bool {
	var mmxErr *mmxExecError
	if ok := matchMmxExecError(err, &mmxErr); ok {
		// HTTP 429 maps to mmx error code 4.
		return mmxErr.Code == 4 || strings.Contains(mmxErr.Message, "rate") || strings.Contains(mmxErr.Message, "429")
	}
	return false
}

func isMmxAuthError(err error) bool {
	var mmxErr *mmxExecError
	if ok := matchMmxExecError(err, &mmxErr); ok {
		return mmxErr.Code == 3 || strings.Contains(mmxErr.Message, "401") || strings.Contains(mmxErr.Message, "auth")
	}
	return false
}

func matchMmxExecError(err error, target **mmxExecError) bool {
	if e, ok := err.(*mmxExecError); ok {
		*target = e
		return true
	}
	return false
}

var mmxBlockedArgs = map[string]blockedArgMode{
	"--output":       blockedWithValue,
	"--messages-file": blockedWithValue,
	"--system":       blockedWithValue,
	"--model":        blockedWithValue,
	"--api-key":      blockedWithValue,
	"--region":       blockedWithValue,
	"--base-url":     blockedWithValue,
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func formatStderrTail(tail string) string {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return ""
	}
	return fmt.Sprintf("\nstderr: %s", tail)
}
