package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// cursorBackend implements Backend by spawning the Cursor Agent CLI
// (cursor-agent) with --output-format stream-json and parsing the JSONL
// event stream. The protocol is similar to Claude Code's stream-json
// format: events are newline-delimited JSON objects with a "type" field.
type cursorBackend struct {
	cfg Config
}

func (b *cursorBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "cursor-agent"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("cursor-agent executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildCursorArgs(prompt, opts, b.cfg.Logger)
	argv0, cmdArgs := chooseCursorInvocation(execName, lookedUp, args, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, argv0, cmdArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", argv0, "args", cmdArgs)
	cmd.WaitDelay = 500 * time.Millisecond
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
		return nil, fmt.Errorf("start cursor-agent: %w", err)
	}

	b.cfg.Logger.Info("cursor-agent started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		// Close stdout when the context is cancelled so scanner.Scan() unblocks.
		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		startTime := time.Now()
		configuredModel := strings.TrimSpace(opts.Model)
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		resultSeen := false
		// stepUsage accumulates per-step token counts from "step_finish" events.
		// resultUsage holds authoritative session totals from "result" events.
		// If the result event includes usage, we use resultUsage exclusively;
		// otherwise we fall back to stepUsage.
		stepUsage := make(map[string]TokenUsage)
		resultUsage := make(map[string]TokenUsage)
		hasResultUsage := false

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			raw := scanner.Text()
			line := normalizeCursorStreamLine(raw)
			if line == "" {
				continue
			}

			var evt cursorStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			if sid := evt.readSessionID(); sid != "" {
				sessionID = sid
			}

			switch evt.Type {
			case "system":
				if evt.Subtype == "init" {
					trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
				}
				if evt.Subtype == "error" {
					errMsg := cursorErrorText(&evt)
					if errMsg != "" {
						trySend(msgCh, Message{Type: MessageError, Content: errMsg})
					}
				}

			case "assistant":
				b.handleCursorAssistant(&evt, msgCh, &output)

			case "tool_use":
				var params map[string]any
				if evt.Parameters != nil {
					_ = json.Unmarshal(evt.Parameters, &params)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolID,
					Input:  params,
				})

			case "tool_result":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolID,
					Output: evt.Output,
				})

			case "result":
				resultSeen = true
				if evt.IsError || evt.Subtype == "error" {
					finalStatus = "failed"
					finalError = cursorErrorText(&evt)
				}
				if evt.ResultText != "" && output.Len() == 0 {
					output.WriteString(evt.ResultText)
				}
				b.accumulateResultUsage(resultUsage, &evt, configuredModel)
				if evt.hasResultUsage() {
					hasResultUsage = true
				}
				// Current Cursor Agent versions can emit the terminal result
				// event but keep a worker process alive. Treat result as the
				// protocol boundary so the daemon can report completion.
				cancel()

			case "error":
				errMsg := cursorErrorText(&evt)
				if errMsg != "" {
					finalError = errMsg
				}
				trySend(msgCh, Message{Type: MessageError, Content: errMsg})

			case "text":
				if evt.Part != nil {
					var part cursorTextPart
					_ = json.Unmarshal(evt.Part, &part)
					if part.Text != "" {
						output.WriteString(part.Text)
						trySend(msgCh, Message{Type: MessageText, Content: part.Text})
					}
				}

			case "step_finish":
				if evt.Part != nil {
					var part cursorStepFinishPart
					_ = json.Unmarshal(evt.Part, &part)
					model := cursorUsageModel(evt.Model, configuredModel)
					u := stepUsage[model]
					u.InputTokens += int64(part.Tokens.Input)
					u.OutputTokens += int64(part.Tokens.Output)
					u.CacheReadTokens += int64(part.Tokens.Cache.Read)
					stepUsage[model] = u
				}
			}
		}

		// Use result usage if available (session totals); otherwise fall back
		// to accumulated step_finish usage.
		if !hasResultUsage {
			resultUsage = stepUsage
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("cursor-agent timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled && !resultSeen {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" && !resultSeen {
			finalStatus = "failed"
			finalError = fmt.Sprintf("cursor-agent exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("cursor-agent finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      resultUsage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *cursorBackend) handleCursorAssistant(evt *cursorStreamEvent, ch chan<- Message, output *strings.Builder) {
	if evt.Message == nil {
		return
	}

	var content cursorAssistantMessage
	if err := json.Unmarshal(evt.Message, &content); err != nil {
		return
	}

	// Note: per-message usage in assistant events is intentionally ignored.
	// Token usage is taken exclusively from "result" events (session totals)
	// to avoid double-counting.

	for _, block := range content.Content {
		switch block.Type {
		case "output_text", "text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   block.Name,
				CallID: block.ID,
				Input:  input,
			})
		}
	}
}

func cursorUsageModel(evtModel, configuredModel string) string {
	if model := strings.TrimSpace(evtModel); model != "" {
		return model
	}
	if model := strings.TrimSpace(configuredModel); model != "" {
		return model
	}
	return "cursor"
}

func (b *cursorBackend) accumulateResultUsage(usage map[string]TokenUsage, evt *cursorStreamEvent, configuredModel string) {
	model := cursorUsageModel(evt.Model, configuredModel)
	u := usage[model]

	// Cursor agent has emitted token usage in multiple shapes: top-level
	// camelCase fields, nested camelCase usage, and nested legacy snake_case
	// usage. Prefer top-level result totals when present, otherwise use the
	// nested usage object.
	if evt.InputTokens != 0 || evt.OutputTokens != 0 || evt.CacheReadTokens != 0 || evt.CacheWriteTokens != 0 {
		u.InputTokens += evt.InputTokens
		u.OutputTokens += evt.OutputTokens
		u.CacheReadTokens += evt.CacheReadTokens
		u.CacheWriteTokens += evt.CacheWriteTokens
	} else if evt.Usage != nil {
		u.InputTokens += evt.Usage.InputTokens
		u.OutputTokens += evt.Usage.OutputTokens
		u.CacheReadTokens += evt.Usage.CacheReadInputTokens
		u.CacheWriteTokens += evt.Usage.CacheWriteInputTokens
	} else {
		return
	}

	usage[model] = u
}

// ── Cursor stream-json types ──

type cursorStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// assistant fields
	Message json.RawMessage `json:"message,omitempty"`

	// tool_use fields
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// tool_result fields
	Output string `json:"output,omitempty"`

	// result fields
	ResultText       string       `json:"result,omitempty"`
	IsError          bool         `json:"is_error,omitempty"`
	InputTokens      int64        `json:"inputTokens,omitempty"`
	OutputTokens     int64        `json:"outputTokens,omitempty"`
	CacheReadTokens  int64        `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64        `json:"cacheWriteTokens,omitempty"`
	Usage            *cursorUsage `json:"usage,omitempty"`
	TotalCost        float64      `json:"total_cost_usd,omitempty"`

	// error fields
	ErrorMsg string `json:"error,omitempty"`
	Detail   string `json:"detail,omitempty"`

	// legacy compat
	Part json.RawMessage `json:"part,omitempty"`
}

func (evt *cursorStreamEvent) readSessionID() string {
	if s := strings.TrimSpace(evt.SessionID); s != "" {
		return s
	}
	return ""
}

func (evt *cursorStreamEvent) hasResultUsage() bool {
	return evt.Usage != nil || evt.InputTokens != 0 || evt.OutputTokens != 0 || evt.CacheReadTokens != 0 || evt.CacheWriteTokens != 0
}

type cursorUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CacheReadInputTokens  int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64
}

func (u *cursorUsage) UnmarshalJSON(data []byte) error {
	var raw struct {
		InputTokensSnake              int64 `json:"input_tokens"`
		InputTokensCamel              int64 `json:"inputTokens"`
		OutputTokensSnake             int64 `json:"output_tokens"`
		OutputTokensCamel             int64 `json:"outputTokens"`
		CachedInputTokensSnake        int64 `json:"cached_input_tokens"`
		CachedInputTokensCamel        int64 `json:"cachedInputTokens"`
		CacheReadTokensCamel          int64 `json:"cacheReadTokens"`
		CacheReadInputTokensSnake     int64 `json:"cache_read_input_tokens"`
		CacheReadInputTokensCamel     int64 `json:"cacheReadInputTokens"`
		CacheWriteTokensCamel         int64 `json:"cacheWriteTokens"`
		CacheCreationInputTokensSnake int64 `json:"cache_creation_input_tokens"`
		CacheCreationInputTokensCamel int64 `json:"cacheCreationInputTokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.InputTokens = firstNonZeroInt64(raw.InputTokensSnake, raw.InputTokensCamel)
	u.OutputTokens = firstNonZeroInt64(raw.OutputTokensSnake, raw.OutputTokensCamel)
	u.CacheReadInputTokens = firstNonZeroInt64(
		raw.CachedInputTokensSnake,
		raw.CachedInputTokensCamel,
		raw.CacheReadTokensCamel,
		raw.CacheReadInputTokensSnake,
		raw.CacheReadInputTokensCamel,
	)
	u.CacheWriteInputTokens = firstNonZeroInt64(
		raw.CacheWriteTokensCamel,
		raw.CacheCreationInputTokensSnake,
		raw.CacheCreationInputTokensCamel,
	)
	return nil
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

type cursorAssistantMessage struct {
	Model   string               `json:"model"`
	Content []cursorContentBlock `json:"content"`
	Usage   *cursorUsage         `json:"usage,omitempty"`
}

type cursorContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type cursorTextPart struct {
	Text string `json:"text"`
}

type cursorStepFinishPart struct {
	Tokens struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Cache  struct {
			Read int `json:"read"`
		} `json:"cache"`
	} `json:"tokens"`
	Cost float64 `json:"cost"`
}

// ── Helpers ──

// normalizeCursorStreamLine handles the stdout:/stderr: prefix that Cursor
// CLI may emit in stream-json mode. Returns the trimmed JSON line.
func normalizeCursorStreamLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// Cursor CLI may prefix lines with "stdout:" or "stderr:" — strip it.
	if idx := cursorStreamPrefixRe.FindStringIndex(trimmed); idx != nil {
		return strings.TrimSpace(trimmed[idx[1]:])
	}
	return trimmed
}

var cursorStreamPrefixRe = regexp.MustCompile(`^(?i)(stdout|stderr)\s*[:=]?\s*`)

func cursorErrorText(evt *cursorStreamEvent) string {
	if evt.ErrorMsg != "" {
		return evt.ErrorMsg
	}
	if evt.Detail != "" {
		return evt.Detail
	}
	if evt.ResultText != "" {
		return evt.ResultText
	}
	return ""
}

// cursorBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// the daemon↔cursor-agent communication protocol.
var cursorBlockedArgs = map[string]blockedArgMode{
	"-p":              blockedStandalone, // non-interactive print mode
	"--output-format": blockedWithValue,  // stream-json protocol
	"--yolo":          blockedStandalone, // auto-approval for autonomous operation
}

// buildCursorArgs assembles the argv for a one-shot cursor-agent invocation.
//
// Usage: cursor-agent -p <prompt> --output-format stream-json
//
//	--workspace <cwd> --yolo [--model <m>] [--resume <id>]
func buildCursorArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--yolo",
	}
	if opts.Cwd != "" {
		args = append(args, "--workspace", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	// NOTE: cursor-agent CLI does not support --system-prompt or --max-turns.
	// Instructions are injected via AGENTS.md and .cursor/skills/ files instead.
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, cursorBlockedArgs, logger)...)
	return args
}
