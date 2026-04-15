package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ollamaBackend implements Backend by driving a local Ollama server
// (https://ollama.com) via its `/api/chat` endpoint. The backend runs a
// tool-use loop: it offers a single `shell` function to the model, executes
// any commands the model invokes against the task's working directory, and
// feeds the output back as a tool-result message until the model returns a
// final answer (or MaxTurns is reached).
//
// Models with tool support (gemma3+, qwen2.5+, llama3.1+, etc.) can use
// this to drive the `multica` CLI — read issue context, post comments,
// update status, etc. — exactly the way Claude/Codex/Gemini do via their
// native shells. Models without tool support degrade gracefully: they
// return text-only responses, which still works for chat-mode tasks.
//
// The shell tool runs commands in the daemon's prepared working directory
// (`opts.Cwd`) with the env vars the daemon already merged (`cfg.Env`,
// which includes the multica binary on PATH). This is yolo-equivalent —
// no command filter — matching what Claude's `bypassPermissions` and
// Gemini's `--yolo` already do.
//
// Configuration:
//
//	cfg.ExecutablePath  — Ollama base URL (default "http://localhost:11434").
//	                      The daemon's config probe writes this from
//	                      MULTICA_OLLAMA_HOST.
//	cfg.Env             — env vars merged into each shell tool invocation.
//	opts.Model          — Ollama model name (e.g. "gemma4:latest"). Required.
//	opts.MaxTurns       — max tool-loop iterations (default 25).
//	opts.Timeout        — overall request deadline (default 20 min).
//	opts.SystemPrompt   — sent as a system message before the user prompt.
//	opts.ResumeSessionID — ignored (Ollama is stateless).
type ollamaBackend struct {
	cfg Config
}

// DefaultOllamaHost is the canonical base URL for an unconfigured local
// Ollama server. Exported so the daemon config probe can share it
// without duplicating the literal.
const DefaultOllamaHost = "http://localhost:11434"

const (
	defaultOllamaTimeout  = 20 * time.Minute
	defaultOllamaMaxTurns = 25
	// shellExecTimeoutDefault caps a single shell tool invocation so a
	// hung command can't block the whole task. The agent timeout
	// (opts.Timeout) is the outer ceiling; this is the per-call limit.
	// Override with MULTICA_OLLAMA_SHELL_TIMEOUT (duration, e.g. "10m").
	shellExecTimeoutDefault = 5 * time.Minute
	// maxToolOutputDefault caps the bytes of a single tool-result
	// message fed back to the model. Models can re-run with `| head` or
	// `| tail` if they need more. Override with
	// MULTICA_OLLAMA_MAX_OUTPUT_BYTES (positive int).
	maxToolOutputDefault = 16 * 1024
)

// shellExecTimeout returns the per-command shell timeout, honoring
// MULTICA_OLLAMA_SHELL_TIMEOUT if set and parseable. Read at Execute
// time (not init) so tests can override freely.
func shellExecTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("MULTICA_OLLAMA_SHELL_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return shellExecTimeoutDefault
}

// maxToolOutput returns the byte cap on a single tool-result message,
// honoring MULTICA_OLLAMA_MAX_OUTPUT_BYTES if set and positive.
func maxToolOutput() int {
	if v := strings.TrimSpace(os.Getenv("MULTICA_OLLAMA_MAX_OUTPUT_BYTES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return maxToolOutputDefault
}

// ── Wire types ─────────────────────────────────────────────────────────────

// ollamaChatRequest is the body of POST /api/chat.
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []ollamaTool        `json:"tools,omitempty"`
}

// ollamaChatMessage is one message in a /api/chat conversation. Both
// inbound (response) and outbound (request) messages use this shape.
//
// On the request side: Role is "system", "user", "assistant", or "tool".
// Tool-result messages set Role="tool", Content to the command output,
// and ToolCallID to the id of the call being answered (some models
// strictly correlate; gemma is lenient, qwen2.5/llama3.1 are not).
//
// On the response side: a model returning tool_calls leaves Content empty
// and populates ToolCalls with one or more requested invocations.
type ollamaChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// ollamaTool defines a function the model may invoke. Ollama follows the
// OpenAI function-calling schema (type=function, parameters as JSON Schema).
type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ollamaToolCall is one invocation requested by the model. The Function
// arguments arrive as a JSON object (not a JSON-encoded string like OpenAI),
// so we keep them as RawMessage and decode per-tool.
type ollamaToolCall struct {
	ID       string             `json:"id,omitempty"`
	Function ollamaToolCallFunc `json:"function"`
}

type ollamaToolCallFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ollamaChatResponse is the body of a non-streaming POST /api/chat
// response. (The streaming NDJSON format is one chunk per line; we use
// stream:false here because tool_calls and final content arrive together
// in the last chunk anyway, so reassembly buys us nothing.)
type ollamaChatResponse struct {
	Model           string            `json:"model"`
	Message         ollamaChatMessage `json:"message"`
	Done            bool              `json:"done"`
	DoneReason      string            `json:"done_reason,omitempty"`
	PromptEvalCount int64             `json:"prompt_eval_count,omitempty"`
	EvalCount       int64             `json:"eval_count,omitempty"`
}

// ── Tool catalog ───────────────────────────────────────────────────────────

// shellToolDef is the only tool exposed to the model. The single
// `command` string is passed to `bash -lc` in the task's working
// directory. The model is expected to use this to invoke the `multica`
// CLI (already on PATH) plus standard Unix utilities.
var shellToolDef = ollamaTool{
	Type: "function",
	Function: ollamaToolFunction{
		Name:        "shell",
		Description: "Execute a shell command in the current working directory and return its combined stdout+stderr. Use this to run the `multica` CLI (e.g. `multica issue get <id> --output json`) and any other Unix command needed to complete the task.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute. Will be run via `bash -c` in your current working directory.",
				},
			},
			"required": []string{"command"},
		},
	},
}

// ── Backend implementation ─────────────────────────────────────────────────

func (b *ollamaBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	baseURL := strings.TrimRight(b.cfg.ExecutablePath, "/")
	if baseURL == "" {
		baseURL = DefaultOllamaHost
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("ollama: model is required (set MULTICA_OLLAMA_MODEL on the daemon)")
	}
	if opts.ResumeSessionID != "" {
		b.cfg.Logger.Debug("ollama: ignoring ResumeSessionID (provider is stateless)", "session_id", opts.ResumeSessionID)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultOllamaTimeout
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultOllamaMaxTurns
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)

	// Capacity matches claude.go and gemini.go (256). A 25-turn loop with
	// 2 events per tool call plus text fits comfortably; oversize is fine
	// because trySend drops on full anyway.
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		client := &http.Client{}

		messages := buildInitialMessages(prompt, opts)

		var (
			finalText      strings.Builder
			usage          TokenUsage
			gotFinalAnswer bool // model produced a turn with no tool_calls
		)

		b.cfg.Logger.Info("ollama started", "url", baseURL+"/api/chat", "model", opts.Model, "cwd", opts.Cwd, "max_turns", maxTurns)

		for turn := 0; turn < maxTurns; turn++ {
			resp, err := postOllamaChat(runCtx, client, baseURL, opts.Model, messages)
			if err != nil {
				resCh <- failedResult(err, finalText.String(), startTime, runCtx, timeout, opts.Model, usage)
				return
			}

			// Track token counts across turns.
			usage.InputTokens += resp.PromptEvalCount
			usage.OutputTokens += resp.EvalCount

			assistantMsg := resp.Message

			// Guard against an empty response from Ollama — e.g. the
			// server returns {"done":true} with no message payload, or
			// the model OOMed mid-generation and emitted
			// {role:"assistant", content:"", tool_calls:[]} (common for
			// small quantized models under memory pressure). Without
			// this check, the loop would exit with gotFinalAnswer=true
			// and the task would be reported "completed" with empty
			// output. Role is always "assistant" on responses and tells
			// us nothing useful — only content/tool_calls matter.
			if assistantMsg.Content == "" && len(assistantMsg.ToolCalls) == 0 {
				resCh <- failedResult(
					fmt.Errorf("ollama: response carried no message content and no tool_calls (turn %d, done=%v)", turn+1, resp.Done),
					finalText.String(), startTime, runCtx, timeout, opts.Model, usage,
				)
				return
			}

			// If the model returned text content, accumulate and stream it.
			if assistantMsg.Content != "" {
				finalText.WriteString(assistantMsg.Content)
				trySend(msgCh, Message{Type: MessageText, Content: assistantMsg.Content})
			}

			// No tool calls → final answer, exit the loop.
			if len(assistantMsg.ToolCalls) == 0 {
				gotFinalAnswer = true
				break
			}

			// Append the assistant turn (with tool_calls) to history so the
			// model sees its own request when we return tool results.
			messages = append(messages, assistantMsg)

			// Execute each tool call and append a tool-result message.
			for _, call := range assistantMsg.ToolCalls {
				toolResult := b.dispatchTool(runCtx, call, opts.Cwd)
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   call.Function.Name,
					CallID: call.ID,
					Input:  decodeToolArgs(call.Function.Arguments),
				})
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					Tool:   call.Function.Name,
					CallID: call.ID,
					Output: toolResult,
				})
				messages = append(messages, ollamaChatMessage{
					Role:       "tool",
					Content:    toolResult,
					ToolCallID: call.ID,
				})
			}
		}

		durationMs := time.Since(startTime).Milliseconds()
		text := finalText.String()
		result := Result{
			Status:     "completed",
			Output:     text,
			DurationMs: durationMs,
		}
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			result.Usage = map[string]TokenUsage{opts.Model: usage}
		}
		// Context status takes precedence over a clean loop exit. If the
		// parent timed out or cancelled between the last iteration and now,
		// the loop may have broken because postOllamaChat returned a
		// transport error and we exited via the failedResult path — but we
		// also defend the success path so a race window doesn't report a
		// half-finished task as "completed". Same hardening claude.go has.
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			result.Status = "timeout"
			result.Error = fmt.Sprintf("ollama timed out after %s", timeout)
		case errors.Is(runCtx.Err(), context.Canceled):
			result.Status = "aborted"
			result.Error = "execution cancelled"
		case !gotFinalAnswer:
			// Loop exhausted MaxTurns without the model producing a
			// non-tool-call response — treat as a failure so the user sees
			// it rather than silently truncating mid-task.
			result.Status = "failed"
			result.Error = fmt.Sprintf("ollama: hit MaxTurns=%d without a final response", maxTurns)
		}

		b.cfg.Logger.Info("ollama finished",
			"model", opts.Model,
			"status", result.Status,
			"duration", time.Since(startTime).Round(time.Millisecond).String(),
		)
		resCh <- result
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// postOllamaChat sends one /api/chat request and decodes the (non-streaming)
// response. Tool-related streaming is intentionally avoided — Ollama emits
// tool_calls only on the final chunk anyway, so single-shot is simpler and
// equally responsive at task granularity.
func postOllamaChat(ctx context.Context, client *http.Client, baseURL, model string, messages []ollamaChatMessage) (*ollamaChatResponse, error) {
	body, err := json.Marshal(ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
		Tools:    []ollamaTool{shellToolDef},
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: unreachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var out ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}
	return &out, nil
}

// dispatchTool routes a tool call to its handler and returns the output
// the model should see. Unknown tool names get an explanatory error
// string (returned to the model rather than failing the task — the model
// can recover by trying a different approach).
func (b *ollamaBackend) dispatchTool(ctx context.Context, call ollamaToolCall, cwd string) string {
	switch call.Function.Name {
	case "shell":
		return b.execShell(ctx, call.Function.Arguments, cwd)
	default:
		b.cfg.Logger.Warn("ollama: model called unknown tool", "name", call.Function.Name)
		return fmt.Sprintf("error: unknown tool %q (only 'shell' is available)", call.Function.Name)
	}
}

// execShell runs a single shell command via `bash -lc` in cwd, with the
// daemon-prepared environment, and returns the combined stdout+stderr
// truncated to a sane size. Any error is folded into the returned string
// so the model can react to it on the next turn.
func (b *ollamaBackend) execShell(ctx context.Context, rawArgs json.RawMessage, cwd string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return fmt.Sprintf("error: invalid arguments for shell tool: %s", err.Error())
	}
	cmdStr := strings.TrimSpace(args.Command)
	if cmdStr == "" {
		return "error: shell tool requires a non-empty 'command' argument"
	}

	timeout := shellExecTimeout()
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use `bash -c` (NOT `bash -lc`): the daemon prepends the multica
	// binary's directory to PATH via cfg.Env, but a login shell would
	// re-source /etc/profile and overwrite that injection. Avoid the
	// reset by skipping login mode — the agent doesn't need shell init
	// files anyway.
	cmd := exec.CommandContext(cmdCtx, "bash", "-c", cmdStr)
	cmd.Dir = cwd
	cmd.Env = buildEnv(b.cfg.Env)

	b.cfg.Logger.Info("ollama: shell exec", "cmd", truncate(cmdStr, 200), "cwd", cwd)

	output, err := cmd.CombinedOutput()
	out := string(output)
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			out += fmt.Sprintf("\n[exit code %d]", exitErr.ExitCode())
		case errors.Is(cmdCtx.Err(), context.DeadlineExceeded):
			out += fmt.Sprintf("\n[command timed out after %s]", timeout)
		case errors.Is(cmdCtx.Err(), context.Canceled):
			// Parent was cancelled (user aborted task or daemon
			// shutting down). The outer loop will detect this on its
			// next postOllamaChat call and bail with status="aborted".
			out += "\n[command cancelled]"
		default:
			out += "\n[error: " + err.Error() + "]"
		}
	}
	// Cap to keep token usage bounded; the model can re-run with `| head`
	// or `| tail` if it needs more context.
	cap := maxToolOutput()
	if len(out) > cap {
		out = out[:cap] + fmt.Sprintf("\n[truncated: %d more bytes]", len(out)-cap)
	}
	return out
}

// ── Helpers ────────────────────────────────────────────────────────────────

// buildInitialMessages assembles the [system?, user] starting conversation.
func buildInitialMessages(prompt string, opts ExecOptions) []ollamaChatMessage {
	msgs := make([]ollamaChatMessage, 0, 2)
	if opts.SystemPrompt != "" {
		msgs = append(msgs, ollamaChatMessage{Role: "system", Content: opts.SystemPrompt})
	}
	msgs = append(msgs, ollamaChatMessage{Role: "user", Content: prompt})
	return msgs
}

// decodeToolArgs converts a tool-call argument object into a generic map
// for forwarding as a Message.Input. Returns nil if decode fails — the
// streamed message is informational and shouldn't fail the task.
func decodeToolArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// failedResult produces a Result for an error that aborted the loop.
//
// Status is determined by the CONTEXT, not the side-effect error: a
// timeout or user cancellation kills the in-flight HTTP request, which
// surfaces as a generic transport error from the client. Reporting that
// transport error as "failed" hides the real cause. This mirrors the
// pattern claude.go and gemini.go use after upstream PR #920 fixed the
// equivalent bug there.
func failedResult(err error, accumulated string, start time.Time, ctx context.Context, timeout time.Duration, model string, usage TokenUsage) Result {
	r := Result{
		Output:     accumulated,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		r.Usage = map[string]TokenUsage{model: usage}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		r.Status = "timeout"
		r.Error = fmt.Sprintf("ollama timed out after %s", timeout)
	case errors.Is(ctx.Err(), context.Canceled):
		r.Status = "aborted"
		r.Error = "execution cancelled"
	default:
		r.Status = "failed"
		r.Error = err.Error()
	}
	return r
}

// OllamaModelCapabilities fetches the capabilities list Ollama
// advertises for a given model via POST /api/show. Used by the daemon
// config probe to warn operators when they've configured a model that
// does NOT declare "tools" capability — those models will fall back to
// text-only responses and appear broken for issue-assignment tasks.
// Returns (caps, nil) on success; (nil, err) if the request fails.
func OllamaModelCapabilities(ctx context.Context, baseURL, model string) ([]string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = DefaultOllamaHost
	}
	body, err := json.Marshal(map[string]string{"name": model})
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal /api/show: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build /api/show request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: /api/show unreachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return nil, fmt.Errorf("ollama: /api/show returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama: decode /api/show: %w", err)
	}
	return out.Capabilities, nil
}

// OllamaModelSupportsTools returns true if the given model declares
// "tools" in its /api/show capabilities list. False means the model
// can still chat but won't follow tool_calls requests — in that case
// Multica issue-assignment tasks will degrade to text-only and the
// daemon should warn the operator.
func OllamaModelSupportsTools(ctx context.Context, baseURL, model string) (bool, error) {
	caps, err := OllamaModelCapabilities(ctx, baseURL, model)
	if err != nil {
		return false, err
	}
	for _, c := range caps {
		if c == "tools" {
			return true, nil
		}
	}
	return false, nil
}

// detectOllamaVersion fetches the running Ollama server's version via
// GET {baseURL}/api/version. Used by agent.DetectVersion when the
// provider is "ollama" — the daemon stores the base URL in entry.Path.
func detectOllamaVersion(ctx context.Context, baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = DefaultOllamaHost
	}
	baseURL = strings.TrimRight(baseURL, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/version", nil)
	if err != nil {
		return "", fmt.Errorf("ollama: build version request: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: unreachable at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama: /api/version returned %d", resp.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("ollama: decode version: %w", err)
	}
	if body.Version == "" {
		return "", fmt.Errorf("ollama: empty version in response")
	}
	return body.Version, nil
}
