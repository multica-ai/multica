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

// kimiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var kimiBlockedArgs = map[string]blockedArgMode{
	"--print":         blockedStandalone, // non-interactive mode
	"--output-format": blockedWithValue,  // stream-json protocol for daemon communication
	"--input-format":  blockedWithValue,  // text input format
	"-y":              blockedStandalone, // auto-approve (implied by --print)
	"--yolo":          blockedStandalone, // auto-approve
	"--yes":           blockedStandalone, // auto-approve alias
	"-p":              blockedWithValue,  // prompt is piped via stdin
	"--prompt":        blockedWithValue,  // prompt is piped via stdin
	"-S":              blockedWithValue,  // session ID managed by daemon
	"--session":       blockedWithValue,  // session ID managed by daemon
	"-r":              blockedWithValue,  // resume session managed by daemon
}

// kimiBackend implements Backend by spawning the Kimi Code CLI
// with `--print --output-format stream-json` and parsing its NDJSON event stream.
type kimiBackend struct {
	cfg Config
}

func (b *kimiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "kimi"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("kimi executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}

	args := buildKimiArgs(sessionID, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	b.cfg.Logger.Debug("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	// Pipe prompt via stdin.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[kimi:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start kimi: %w", err)
	}

	// Write prompt to stdin and close.
	go func() {
		defer stdin.Close()
		if _, err := io.WriteString(stdin, prompt); err != nil {
			b.cfg.Logger.Warn("kimi stdin write failed", "error", err)
		}
	}()

	b.cfg.Logger.Info("kimi started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model, "session", sessionID)

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
		scanResult := b.processEvents(stdout, msgCh)

		// Wait for process exit.
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("kimi timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("kimi exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("kimi finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. Kimi doesn't report token usage in stream-json
		// output, so we attribute any accumulated usage to the configured model.
		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
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

// ── Event handlers ──

// kimiEventResult holds accumulated state from processing the event stream.
type kimiEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage
}

// processEvents reads JSON lines from r, dispatches events to ch, and returns
// the accumulated result.
//
// Kimi's stream-json output emits one JSON object per message turn:
//
//   - {"role":"assistant","content":[...],"tool_calls":[...]} — agent response
//   - {"role":"tool","content":[...],"tool_call_id":"..."} — tool result
func (b *kimiBackend) processEvents(r io.Reader, ch chan<- Message) kimiEventResult {
	var output strings.Builder
	sessionID := ""
	finalStatus := "completed"
	var finalError string
	var usage TokenUsage
	emittedStatus := false

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var evt kimiMessage
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		switch evt.Role {
		case "assistant":
			// Emit running status on first assistant message.
			if !emittedStatus {
				trySend(ch, Message{Type: MessageStatus, Status: "running"})
				emittedStatus = true
			}

			// Process content blocks.
			for _, block := range evt.Content {
				switch block.Type {
				case "text":
					if block.Text != "" {
						output.WriteString(block.Text)
						trySend(ch, Message{Type: MessageText, Content: block.Text})
					}
				case "think":
					if block.Think != "" {
						trySend(ch, Message{Type: MessageThinking, Content: block.Think})
					}
				}
			}

			// Process tool calls.
			for _, tc := range evt.ToolCalls {
				var input map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				trySend(ch, Message{
					Type:   MessageToolUse,
					Tool:   tc.Function.Name,
					CallID: tc.ID,
					Input:  input,
				})
			}

		case "tool":
			// Concatenate text content blocks.
			var textParts []string
			for _, block := range evt.Content {
				if block.Type == "text" && block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			}
			toolOutput := strings.Join(textParts, "\n")
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: evt.ToolCallID,
				Output: toolOutput,
			})

		case "system":
			// System messages are informational; skip.

		case "error":
			errMsg := ""
			for _, block := range evt.Content {
				if block.Type == "text" && block.Text != "" {
					errMsg = block.Text
					break
				}
			}
			if errMsg == "" {
				errMsg = "unknown kimi error"
			}
			trySend(ch, Message{Type: MessageError, Content: errMsg})
			finalStatus = "failed"
			finalError = errMsg
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("kimi stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	return kimiEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
	}
}

// ── JSON types for kimi --print --output-format stream-json ──

// kimiMessage represents a single JSON message from Kimi's stream-json output.
type kimiMessage struct {
	Role       string            `json:"role"`
	Content    []kimiContentBlock `json:"content,omitempty"`
	ToolCalls  []kimiToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// kimiContentBlock represents a content block within a Kimi message.
type kimiContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// "think" type
	Think string `json:"think,omitempty"`
}

// kimiToolCall represents a tool call within a Kimi assistant message.
type kimiToolCall struct {
	Type     string           `json:"type"`
	ID       string           `json:"id"`
	Function kimiFunctionCall `json:"function"`
}

// kimiFunctionCall represents the function details of a tool call.
type kimiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string of arguments
}

// ── Arg builder ──

// buildKimiArgs assembles the argv for a one-shot kimi invocation.
//
// Flags:
//
//	--print                     non-interactive mode (implies --yolo)
//	--output-format stream-json streaming NDJSON output for live events
//	--input-format text         text input via stdin
//	-y                          auto-approve all tool use
//	-S <id>                     session ID for resumption
//	-m <model>                  optional model override
//	--skills-dir <dir>          skills directory
func buildKimiArgs(sessionID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "text",
		"-y",
	}
	if sessionID != "" {
		args = append(args, "-S", sessionID)
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, kimiBlockedArgs, logger)...)
	return args
}
