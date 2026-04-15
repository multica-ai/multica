package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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

	args := buildCursorArgs(prompt, opts)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	cmd.WaitDelay = 20 * time.Second
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
				b.handleCursorAssistant(&evt, msgCh, &output, usage)

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
				if evt.IsError || evt.Subtype == "error" {
					finalStatus = "failed"
					finalError = cursorErrorText(&evt)
				}
				if evt.ResultText != "" && output.Len() == 0 {
					output.WriteString(evt.ResultText)
				}
				b.accumulateResultUsage(usage, &evt)

			case "error":
				errMsg := cursorErrorText(&evt)
				if errMsg != "" {
					finalError = errMsg
				}
				trySend(msgCh, Message{Type: MessageError, Content: errMsg})

			case "text":
				// Compatibility with older stream-json shapes.
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
					model := evt.Model
					if model == "" {
						model = "cursor"
					}
					u := usage[model]
					u.InputTokens += int64(part.Tokens.Input)
					u.OutputTokens += int64(part.Tokens.Output)
					u.CacheReadTokens += int64(part.Tokens.Cache.Read)
					usage[model] = u
				}
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("cursor-agent timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
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
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *cursorBackend) handleCursorAssistant(evt *cursorStreamEvent, ch chan<- Message, output *strings.Builder, usage map[string]TokenUsage) {
	if evt.Message == nil {
		return
	}

	var content cursorAssistantMessage
	if err := json.Unmarshal(evt.Message, &content); err != nil {
		return
	}

	if content.Usage != nil && content.Model != "" {
		u := usage[content.Model]
		u.InputTokens += content.Usage.InputTokens
		u.OutputTokens += content.Usage.OutputTokens
		u.CacheReadTokens += content.Usage.CacheReadInputTokens
		usage[content.Model] = u
	}

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

func (b *cursorBackend) accumulateResultUsage(usage map[string]TokenUsage, evt *cursorStreamEvent) {
	if evt.Usage == nil {
		return
	}
	model := evt.Model
	if model == "" {
		model = "cursor"
	}
	u := usage[model]
	u.InputTokens += evt.Usage.InputTokens
	u.OutputTokens += evt.Usage.OutputTokens
	u.CacheReadTokens += evt.Usage.CacheReadInputTokens
	usage[model] = u
}

// ── Cursor stream-json types ──

type cursorStreamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`

	// assistant fields
	Message json.RawMessage `json:"message,omitempty"`

	// tool_use fields
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// tool_result fields
	Output string `json:"output,omitempty"`

	// result fields
	ResultText string          `json:"result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Usage      *cursorUsage    `json:"usage,omitempty"`
	TotalCost  float64         `json:"total_cost_usd,omitempty"`

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

type cursorUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cached_input_tokens"`
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

// isCursorUnknownSessionError detects session-not-found errors so the
// caller can retry without --resume.
func isCursorUnknownSessionError(stdout, stderr string) bool {
	haystack := stdout + "\n" + stderr
	return cursorSessionErrorRe.MatchString(haystack)
}

var cursorSessionErrorRe = regexp.MustCompile(
	`(?i)(unknown\s+(session|chat)|session\s+.*\s+not\s+found|chat\s+.*\s+not\s+found|resume\s+.*\s+not\s+found|could\s+not\s+resume)`,
)

// buildCursorArgs assembles the argv for a one-shot cursor-agent invocation.
//
// Usage: cursor-agent chat -p <prompt> --output-format stream-json
//
//	--workspace <cwd> --yolo [--model <m>] [--resume <id>]
func buildCursorArgs(prompt string, opts ExecOptions) []string {
	args := []string{
		"chat",
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
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	return args
}
