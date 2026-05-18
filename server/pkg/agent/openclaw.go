package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// minOpenclawVersion is the lowest openclaw version that emits its
// --json result on stdout. PR #2101 swapped the adapter from reading
// stderr to stdout; older builds wrote JSON to stderr and now appear
// to silently produce no output. The check in Execute fails fast with
// a hardcoded upgrade hint so users see an actionable message instead
// of "openclaw returned no parseable output".
const minOpenclawVersion = "2026.5.5"

// openclawVersionPattern extracts a three-segment dotted version from
// arbitrary `openclaw --version` output (e.g. "openclaw 2026.5.5",
// "openclaw v2026.5.5 c37871e").
var openclawVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// openclawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var openclawBlockedArgs = map[string]blockedArgMode{
	"--local":         blockedStandalone, // local mode for daemon execution
	"--json":          blockedStandalone, // JSON output for daemon communication
	"--session-id":    blockedWithValue,  // managed by daemon for session resumption
	"--message":       blockedWithValue,  // prompt is set by daemon
	"--model":         blockedWithValue,  // openclaw agent does not accept --model; model is bound at registration via `openclaw agents add/update --model`
	"--system-prompt": blockedWithValue,  // openclaw agent does not accept --system-prompt; instructions are injected into --message
}

// openclawBackend implements Backend by spawning `openclaw agent --message <prompt>
// --output-format stream-json --yes` and reading streaming NDJSON events from
// stdout — similar to the opencode backend.
type openclawBackend struct {
	cfg Config
}

func (b *openclawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "openclaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("openclaw executable not found at %q: %w", execPath, err)
	}

	if err := checkOpenclawVersion(ctx, execPath); err != nil {
		return nil, err
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
	args := buildOpenclawArgs(prompt, sessionID, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	// openclaw writes its --json output to stdout. Stderr carries log
	// overflow (security warnings, tool errors, etc.) — capture it via a
	// log writer so it surfaces in daemon logs without being fed into the
	// JSON parser.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openclaw stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[openclaw:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start openclaw: %w", err)
	}

	b.cfg.Logger.Info("openclaw started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
		scanResult := b.processOutput(stdout, msgCh)

		// Wait for process exit.
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("openclaw timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("openclaw exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("openclaw finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. Prefer the model openclaw reported in
		// `meta.agentMeta.model` (the actual LLM, e.g. `deepseek-chat`).
		// Fall back to opts.Model — which for openclaw is the agent name
		// passed via `--agent`, not a real model identifier — only when
		// the runtime didn't surface its own model. Last resort is the
		// daemon's `unknown` placeholder.
		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := scanResult.model
			if model == "" {
				model = opts.Model
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

// buildOpenclawArgs assembles the argv for a one-shot `openclaw agent` invocation.
//
// The CLI only accepts --local, --json, --session-id, --timeout, --message (and
// flags like --agent / --channel that users pass through CustomArgs). Notably
// it does NOT accept --model or --system-prompt — model is bound at agent
// registration time via `openclaw agents add/update --model`, and instructions
// must be injected inline into --message because openclaw loads AGENTS.md from
// its own workspace directory, not from cwd.
func buildOpenclawArgs(prompt, sessionID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"agent", "--local", "--json", "--session-id", sessionID}
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}
	// OpenClaw binds models to pre-registered agents at `openclaw agents
	// add/update --model` time; the daemon selects one at runtime by
	// passing --agent <id>. The model dropdown populates its list from
	// `openclaw agents list`, so opts.Model here is an agent id (see
	// openclawEntriesToModels — the agent's display name lives in the
	// dropdown label, not in opts.Model). Only inject when the user
	// hasn't already set --agent via custom_args — custom_args wins for
	// backward compatibility with existing configs.
	customArgs := filterCustomArgs(opts.CustomArgs, openclawBlockedArgs, logger)
	if opts.Model != "" && !customArgsContains(customArgs, "--agent") {
		args = append(args, "--agent", opts.Model)
	}
	args = append(args, customArgs...)

	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n" + prompt
	}
	args = append(args, "--message", prompt)
	return args
}

// customArgsContains reports whether args contains the given flag
// (either as a standalone token "--flag" or in "--flag=value" form).
func customArgsContains(args []string, flag string) bool {
	prefix := flag + "="
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// checkOpenclawVersion runs `<execPath> --version` and returns a
// user-facing error when the installed openclaw is older than
// minOpenclawVersion. The returned error becomes the task's failure
// comment, so the message intentionally names the detected version
// and the upgrade command.
func checkOpenclawVersion(ctx context.Context, execPath string) error {
	cmd := exec.CommandContext(ctx, execPath, "--version")
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("openclaw --version failed: %w", err)
	}
	detected, ok := parseOpenclawVersion(string(out))
	if !ok {
		return fmt.Errorf("could not parse openclaw version from output: %q", strings.TrimSpace(string(out)))
	}
	if compareOpenclawVersion(detected, minOpenclawVersion) < 0 {
		return fmt.Errorf("openclaw %s is below the minimum supported version %s. Run `openclaw update` to upgrade and try again.", detected, minOpenclawVersion)
	}
	return nil
}

// parseOpenclawVersion extracts the first three-segment dotted version
// from arbitrary `openclaw --version` output. Returns ok=false when no
// match is found.
func parseOpenclawVersion(raw string) (string, bool) {
	m := openclawVersionPattern.FindString(raw)
	if m == "" {
		return "", false
	}
	return m, true
}

// compareOpenclawVersion compares two three-segment dotted versions
// numerically. Returns -1, 0, or +1 like bytes.Compare. Inputs must be
// well-formed (matched by openclawVersionPattern); malformed segments
// compare as zero.
func compareOpenclawVersion(a, b string) int {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		ai, _ := strconv.Atoi(aParts[i])
		bi, _ := strconv.Atoi(bParts[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

// ── Event handlers ──

// openclawEventResult holds accumulated state from processing the event stream.
type openclawEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage
	// model is the LLM identifier reported by openclaw in its result blob
	// (`meta.agentMeta.model`). Empty when the run did not emit it (older
	// openclaw versions, partial outputs). Distinct from `opts.Model`,
	// which for the openclaw backend is the openclaw *agent* name passed
	// via `--agent`, not the underlying model.
	model string
}

// processOutput reads the JSON output from openclaw --json stdout and returns
// the parsed result. OpenClaw writes its JSON output to stdout; stderr carries
// log overflow and is captured separately by the caller. The stream may
// contain:
//
//   - NDJSON streaming events (type: "text", "tool_use", "tool_result", "error",
//     "step_start", "step_finish") — emitted in real time as the agent works
//   - A final result JSON (with payloads + meta) — the legacy single-blob format
//
// We scan line-by-line, emitting messages as events arrive so streaming
// consumers get real-time feedback instead of waiting for the final blob.
func (b *openclawBackend) processOutput(r io.Reader, ch chan<- Message) openclawEventResult {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var output strings.Builder
	var sessionID string
	var model string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string
	gotEvents := false // true if we parsed at least one streaming event or result

	var rawLines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Try parsing as a streaming NDJSON event first.
		if event, ok := tryParseOpenclawEvent(line); ok {
			gotEvents = true
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			switch event.Type {
			case "text":
				if event.Text != "" {
					output.WriteString(event.Text)
					trySend(ch, Message{Type: MessageText, Content: event.Text})
				}
			case "tool_use":
				var input map[string]any
				if event.Input != nil {
					_ = json.Unmarshal(event.Input, &input)
				}
				trySend(ch, Message{
					Type:   MessageToolUse,
					Tool:   event.Tool,
					CallID: event.CallID,
					Input:  input,
				})
			case "tool_result":
				trySend(ch, Message{
					Type:   MessageToolResult,
					Tool:   event.Tool,
					CallID: event.CallID,
					Output: event.Text,
				})
			case "error":
				errMsg := event.errorMessage()
				b.cfg.Logger.Warn("openclaw error event", "error", errMsg)
				trySend(ch, Message{Type: MessageError, Content: errMsg})
				finalStatus = "failed"
				finalError = errMsg
			case "lifecycle":
				phase := event.Phase
				if phase == "error" || phase == "failed" || phase == "cancelled" {
					errMsg := event.errorMessage()
					b.cfg.Logger.Warn("openclaw lifecycle failure", "phase", phase, "error", errMsg)
					trySend(ch, Message{Type: MessageError, Content: errMsg})
					finalStatus = "failed"
					finalError = errMsg
				}
			case "step_start":
				trySend(ch, Message{Type: MessageStatus, Status: "running"})
			case "step_finish":
				if event.Usage != nil {
					u := parseOpenclawUsage(event.Usage)
					usage.InputTokens += u.InputTokens
					usage.OutputTokens += u.OutputTokens
					usage.CacheReadTokens += u.CacheReadTokens
					usage.CacheWriteTokens += u.CacheWriteTokens
				}
			}
			continue
		}

		// Try parsing as a final result blob (legacy format).
		if result, ok := tryParseOpenclawResult(line); ok {
			gotEvents = true
			res := b.buildOpenclawEventResult(result, ch, &output)
			if res.sessionID != "" {
				sessionID = res.sessionID
			}
			if res.model != "" {
				model = res.model
			}
			// Prefer usage from the final result if no streaming events reported it.
			u := res.usage
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
				usage = u
			}
			continue
		}

		// Not JSON — treat as log line.
		b.cfg.Logger.Debug("[openclaw:stdout] " + line)
		rawLines = append(rawLines, line)
	}

	if err := scanner.Err(); err != nil {
		return openclawEventResult{status: "failed", errMsg: fmt.Sprintf("read stdout: %v", err)}
	}

	// If we got no events at all, fall back to raw output.
	if !gotEvents {
		// OpenClaw may output pretty-printed (multi-line) JSON. No single line
		// would parse, so try parsing the accumulated output as a whole.
		// Log lines may precede the JSON, so find the first '{' at line start.
		trimmed := strings.TrimSpace(strings.Join(rawLines, "\n"))
		if trimmed != "" {
			if result, ok := tryParseOpenclawResult(trimmed); ok {
				return b.buildOpenclawEventResult(result, ch, &output)
			}
			// Log lines may precede the JSON blob. Find the first line that
			// starts with '{' and try parsing from there.
			for i, line := range rawLines {
				if len(line) > 0 && line[0] == '{' {
					candidate := strings.TrimSpace(strings.Join(rawLines[i:], "\n"))
					if result, ok := tryParseOpenclawResult(candidate); ok {
						return b.buildOpenclawEventResult(result, ch, &output)
					}
					break
				}
			}
			return openclawEventResult{status: "completed", output: trimmed}
		}
		return openclawEventResult{status: "failed", errMsg: "openclaw returned no parseable output"}
	}

	return openclawEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
		model:     model,
	}
}

// tryParseOpenclawEvent attempts to parse a line as a streaming NDJSON event.
// Returns the event and true if the line is a valid event with a known type.
func tryParseOpenclawEvent(line string) (openclawEvent, bool) {
	if len(line) == 0 || line[0] != '{' {
		return openclawEvent{}, false
	}
	var event openclawEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return openclawEvent{}, false
	}
	if event.Type == "" {
		return openclawEvent{}, false
	}
	return event, true
}

// tryParseOpenclawResult attempts to parse a line as a final result blob
// (the legacy format with payloads + meta). Lines must start with '{' to be
// considered — we no longer scan for braces at arbitrary positions, which
// avoids false matches on log lines containing JSON fragments.
func tryParseOpenclawResult(raw string) (openclawResult, bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return openclawResult{}, false
	}
	var result openclawResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return openclawResult{}, false
	}
	if result.Payloads == nil && result.Meta.DurationMs == 0 {
		return openclawResult{}, false
	}
	return result, true
}

// buildOpenclawEventResult extracts text and metadata from a final result blob.
// Text payloads are appended to the shared output builder and emitted to ch.
func (b *openclawBackend) buildOpenclawEventResult(result openclawResult, ch chan<- Message, output *strings.Builder) openclawEventResult {
	for _, p := range result.Payloads {
		if p.Text != "" {
			output.WriteString(p.Text)
			trySend(ch, Message{Type: MessageText, Content: p.Text})
		}
	}

	var sessionID string
	var model string
	var usage TokenUsage
	if result.Meta.AgentMeta != nil {
		if sid, ok := result.Meta.AgentMeta["sessionId"].(string); ok {
			sessionID = sid
		}
		// `meta.agentMeta.model` is openclaw's true LLM identifier
		// (e.g. "deepseek-chat", "claude-sonnet-4"). Take it as-is — the
		// dashboard expects whatever string the runtime reports, mirroring
		// claude/pi/codex which read model directly off their stream.
		if m, ok := result.Meta.AgentMeta["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
		if u, ok := result.Meta.AgentMeta["usage"].(map[string]any); ok {
			usage = parseOpenclawUsage(u)
		}
	}

	return openclawEventResult{
		status:    "completed",
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
		model:     model,
	}
}

// parseOpenclawUsage extracts token usage from a map, supporting multiple
// field name conventions used by different OpenClaw versions and PaperClip:
//
//	input / inputTokens / input_tokens
//	output / outputTokens / output_tokens
//	cacheRead / cachedInputTokens / cached_input_tokens / cache_read
//	cacheWrite / cacheCreationInputTokens / cache_creation_input_tokens / cache_write
func parseOpenclawUsage(data map[string]any) TokenUsage {
	return TokenUsage{
		InputTokens:      openclawInt64FirstOf(data, "input", "inputTokens", "input_tokens"),
		OutputTokens:     openclawInt64FirstOf(data, "output", "outputTokens", "output_tokens"),
		CacheReadTokens:  openclawInt64FirstOf(data, "cacheRead", "cachedInputTokens", "cached_input_tokens", "cache_read", "cache_read_input_tokens"),
		CacheWriteTokens: openclawInt64FirstOf(data, "cacheWrite", "cacheCreationInputTokens", "cache_creation_input_tokens", "cache_write"),
	}
}

// openclawInt64FirstOf returns the first non-zero int64 value found under any
// of the given keys. This supports field name variants across protocol versions.
func openclawInt64FirstOf(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v := openclawInt64(data, key); v != 0 {
			return v
		}
	}
	return 0
}

// openclawInt64 safely extracts an int64 from a JSON-decoded map value (which
// may be float64 due to Go's JSON number handling).
func openclawInt64(data map[string]any, key string) int64 {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// ── JSON types for `openclaw agent --json` output ──

// openclawEvent represents a single streaming NDJSON event from openclaw --json.
//
// Event types:
//   - "text"        — text output (text field)
//   - "tool_use"    — tool invocation (tool, callId, input)
//   - "tool_result" — tool output (tool, callId, text)
//   - "error"       — error (text, or structured error object)
//   - "lifecycle"   — phase changes (phase: "error"/"failed"/"cancelled")
//   - "step_start"  — agent step begins
//   - "step_finish" — agent step ends (usage)
type openclawEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callId,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Usage     map[string]any  `json:"usage,omitempty"`
	Phase     string          `json:"phase,omitempty"`   // lifecycle event phase
	Error     *openclawError  `json:"error,omitempty"`   // structured error object
	Message   string          `json:"message,omitempty"` // alternative error message field
}

// errorMessage extracts a human-readable error message from the event,
// checking multiple fields: structured error object, text, message, or fallback.
func (e openclawEvent) errorMessage() string {
	if e.Error != nil {
		if msg := e.Error.message(); msg != "" {
			return msg
		}
	}
	if e.Text != "" {
		return e.Text
	}
	if e.Message != "" {
		return e.Message
	}
	return "unknown openclaw error"
}

// openclawError represents a structured error in an openclaw event,
// compatible with PaperClip's error format (name + data.message).
type openclawError struct {
	Name    string             `json:"name,omitempty"`
	Data    *openclawErrorData `json:"data,omitempty"`
	Message string             `json:"message,omitempty"`
}

func (e *openclawError) message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type openclawErrorData struct {
	Message string `json:"message,omitempty"`
}

// openclawResult represents the final JSON output from `openclaw agent --json`
// (the legacy single-blob format with payloads + meta).
type openclawResult struct {
	Payloads []openclawPayload `json:"payloads"`
	Meta     openclawMeta      `json:"meta"`
}

type openclawPayload struct {
	Text string `json:"text"`
}

type openclawMeta struct {
	DurationMs int64          `json:"durationMs"`
	AgentMeta  map[string]any `json:"agentMeta"`
}
