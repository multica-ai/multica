package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// opencodeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var opencodeBlockedArgs = map[string]blockedArgMode{
	"--format": blockedWithValue, // json output format for daemon communication
}

// opencodeBackend implements Backend by spawning `opencode run --format json`
// and reading streaming JSON events from stdout — the same pattern as Claude.
type opencodeBackend struct {
	cfg Config
}

func (b *opencodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "opencode"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("opencode executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{"run", "--format", "json"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--prompt", opts.SystemPrompt)
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, opencodeBlockedArgs, b.cfg.Logger)...)
	args = append(args, prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	b.cfg.Logger.Debug("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	// Auto-approve all tool use in daemon mode.
	env = append(env, `OPENCODE_PERMISSION={"*":"allow"}`)
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[opencode:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	b.cfg.Logger.Info("opencode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so the scanner unblocks.
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
			scanResult.errMsg = fmt.Sprintf("opencode timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("opencode exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("opencode finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. OpenCode doesn't report model per-step, so we
		// attribute all usage to the configured model (or "unknown").
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

// eventResult holds the accumulated state from processing the event stream.
type eventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage // accumulated token usage across all steps
}

// processEvents reads JSON lines from r, dispatches events to ch, and returns
// the accumulated result. This is the core scanner loop, extracted for testability.
func (b *opencodeBackend) processEvents(r io.Reader, ch chan<- Message) eventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event opencodeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.SessionID != "" {
			sessionID = event.SessionID
		}

		switch event.Type {
		case "text":
			b.handleTextEvent(event, ch, &output)
		case "tool_use":
			b.handleToolUseEvent(event, ch)
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			// Accumulate token usage from step_finish events.
			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
		}
	}

	// Check for scanner errors (e.g. broken pipe, read errors).
	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("opencode stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	return eventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
	}
}

func (b *opencodeBackend) handleTextEvent(event opencodeEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

// handleToolUseEvent processes "tool_use" events from opencode. A single
// tool_use event contains both the call and result in part.state when the
// tool has completed (state.status == "completed").
func (b *opencodeBackend) handleToolUseEvent(event opencodeEvent, ch chan<- Message) {
	// Extract input from state.input (the tool invocation parameters).
	var input map[string]any
	if event.Part.State != nil && event.Part.State.Input != nil {
		_ = json.Unmarshal(event.Part.State.Input, &input)
	}

	// Emit the tool-use message.
	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   event.Part.Tool,
		CallID: event.Part.CallID,
		Input:  input,
	})

	// If the tool has completed, also emit a tool-result message.
	if event.Part.State != nil && event.Part.State.Status == "completed" {
		outputStr := extractToolOutput(event.Part.State.Output)
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   event.Part.Tool,
			CallID: event.Part.CallID,
			Output: outputStr,
		})
	}
}

// handleErrorEvent processes "error" events from opencode. OpenCode can exit
// with RC=0 even on errors (e.g. invalid model), so error events are the
// reliable signal for failures.
func (b *opencodeBackend) handleErrorEvent(event opencodeEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown opencode error"
	}

	b.cfg.Logger.Warn("opencode error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

// extractToolOutput converts the tool state output (which may be a string or
// structured object) into a string.
func extractToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

// ── JSON types for `opencode run --format json` stdout events ──

// opencodeEvent represents a single JSON line from `opencode run --format json`.
//
// Event types observed in real output:
//
//	"step_start"  — agent step begins
//	"text"        — text output from agent (part.text)
//	"tool_use"    — tool invocation with call and result (part.tool, part.callID, part.state)
//	"error"       — error from opencode (error.name, error.data.message)
//	"step_finish" — agent step completes (includes token usage)
type opencodeEvent struct {
	Type      string            `json:"type"`
	Timestamp int64             `json:"timestamp,omitempty"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      opencodeEventPart `json:"part"`
	Error     *opencodeError    `json:"error,omitempty"`
}

// opencodeEventPart represents the part field in an opencode event.
type opencodeEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	// Text events
	Text string `json:"text,omitempty"`

	// Tool use events
	Tool   string             `json:"tool,omitempty"`
	CallID string             `json:"callID,omitempty"`
	State  *opencodeToolState `json:"state,omitempty"`

	// step_finish token usage
	Tokens *opencodeTokens `json:"tokens,omitempty"`
}

// opencodeTokens represents token usage in a step_finish event.
type opencodeTokens struct {
	Input  int64              `json:"input"`
	Output int64              `json:"output"`
	Cache  *opencodeCacheTokens `json:"cache,omitempty"`
}

type opencodeCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// opencodeToolState represents the state of a tool invocation.
type opencodeToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
}

// opencodeError represents an error event from opencode.
type opencodeError struct {
	Name string           `json:"name,omitempty"`
	Data *opencodeErrData `json:"data,omitempty"`
}

// Message returns the human-readable error message.
func (e *opencodeError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type opencodeErrData struct {
	Message string `json:"message,omitempty"`
}
