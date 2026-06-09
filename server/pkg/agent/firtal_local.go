package agent

// CEREBRO-PATCH(agent-firtal-local): TECH-3226 — dedicated local Ollama backend
// with a full OpenAI-compatible tool loop.
//
// firtal-gateway (firtal_gateway.go) is chat-only: it sends one request to the
// managed cloud proxy and reads plain text. That is fine for chat but dies on
// issues, which need a tool loop (read issue -> reason -> answer). firtal-local
// is the dedicated local-model engine: it talks DIRECTLY to a local Ollama
// /v1/chat/completions (no gateway, no auth) and drives a real tool loop so a
// local model (e.g. gemma on the Sara runtime) can actually solve issues.
//
// Tools are intentionally READ-ONLY (get_issue, list_comments). The model's
// final text answer is returned as Result.Output, which the daemon posts as the
// issue comment — so there is exactly one authored comment and no double-post.
// Tool calls are executed via the authenticated `multica` CLI already present
// on the runtime host (the same CLI every other agent uses), so no token or DB
// plumbing is needed in the backend.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	firtalLocalProvider      = "firtal-local"
	firtalLocalDefaultURL    = "http://localhost:11434"
	firtalLocalDefaultModel  = "gemma4:12b-it-qat"
	firtalLocalMaxToolRounds = 8
	// firtalLocalToolOutputCap bounds how many characters of a single tool
	// result are fed back to the model, mirroring the gateway runtime's
	// issue/comment caps so a huge issue cannot blow up the local context.
	firtalLocalToolOutputCap = 24000
)

type firtalLocalBackend struct {
	cfg Config
	// httpClient is overridable in tests; nil uses a default client.
	httpClient *http.Client
	// runTool executes a multica tool call and returns its textual result.
	// Overridable in tests; nil uses the real `multica` CLI runner.
	runTool firtalLocalToolFunc
}

// firtalLocalToolFunc executes a single tool call (already decoded) and returns
// the textual result that is fed back to the model.
type firtalLocalToolFunc func(ctx context.Context, name string, args map[string]any) (string, error)

type firtalLocalConfig struct {
	BaseURL      string
	Model        string
	MaxTokens    int
	Temperature  *float64
	MaxToolRound int
}

// ---- OpenAI-compatible wire types (Ollama /v1/chat/completions) ----

type firtalLocalMessage struct {
	Role       string                `json:"role"`
	Content    string                `json:"content,omitempty"`
	ToolCalls  []firtalLocalToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type firtalLocalToolDef struct {
	Type     string                  `json:"type"`
	Function firtalLocalToolFunction `json:"function"`
}

type firtalLocalToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type firtalLocalToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type firtalLocalResponse struct {
	Choices []struct {
		Message struct {
			Content   string                `json:"content"`
			ToolCalls []firtalLocalToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (b *firtalLocalBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	msgCh := make(chan Message, 16)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		start := time.Now()
		cfg := b.config(opts)
		output, usage, err := b.runLoop(runCtx, cfg, prompt, opts, msgCh)
		if err != nil {
			resCh <- Result{Status: "failed", Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
			return
		}
		if output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: output})
		}
		resCh <- Result{
			Status:     "completed",
			Output:     output,
			DurationMs: time.Since(start).Milliseconds(),
			Usage:      map[string]TokenUsage{cfg.Model: usage},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *firtalLocalBackend) config(opts ExecOptions) firtalLocalConfig {
	baseURL := strings.TrimRight(firstEnv(b.cfg.Env, "FIRTAL_LOCAL_OLLAMA_URL", "OLLAMA_HOST"), "/")
	if baseURL == "" {
		baseURL = firtalLocalDefaultURL
	}

	model := strings.TrimSpace(opts.Model)
	if model == "" || model == "auto" {
		model = firstEnv(b.cfg.Env, "FIRTAL_LOCAL_MODEL")
	}
	if model == "" {
		model = firtalLocalDefaultModel
	}

	maxTokens := 4096
	if raw := firstEnv(b.cfg.Env, "FIRTAL_LOCAL_MAX_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	var temperature *float64
	if raw := firstEnv(b.cfg.Env, "FIRTAL_LOCAL_TEMPERATURE"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			temperature = &parsed
		}
	}

	maxRounds := firtalLocalMaxToolRounds
	if raw := firstEnv(b.cfg.Env, "FIRTAL_LOCAL_MAX_TOOL_ROUNDS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxRounds = parsed
		}
	}

	return firtalLocalConfig{
		BaseURL:      baseURL,
		Model:        model,
		MaxTokens:    maxTokens,
		Temperature:  temperature,
		MaxToolRound: maxRounds,
	}
}

// runLoop drives the OpenAI-compatible tool loop against the local model. It
// mirrors the proven server-runtime loop (firtal_gateway_executor.go): call
// with tools until the model stops emitting tool_calls, then make one final
// tool-free call to force a text answer if the round budget is exhausted.
func (b *firtalLocalBackend) runLoop(ctx context.Context, cfg firtalLocalConfig, prompt string, opts ExecOptions, msgCh chan<- Message) (string, TokenUsage, error) {
	tools := firtalLocalToolDefs()

	history := []firtalLocalMessage{
		{Role: "system", Content: firtalLocalSystemPrompt(opts.SystemPrompt)},
		{Role: "user", Content: prompt},
	}

	var usage TokenUsage
	accumulate := func(r firtalLocalResponse) {
		if r.Usage != nil {
			usage.InputTokens += r.Usage.PromptTokens
			usage.OutputTokens += r.Usage.CompletionTokens
		}
	}

	for round := 0; round < cfg.MaxToolRound; round++ {
		resp, err := b.complete(ctx, cfg, history, tools)
		if err != nil {
			return "", usage, err
		}
		accumulate(resp)
		content, calls := firtalLocalExtract(resp)

		if len(calls) == 0 {
			return strings.TrimSpace(content), usage, nil
		}

		history = append(history, firtalLocalMessage{Role: "assistant", Content: content, ToolCalls: calls})
		for _, call := range calls {
			args := decodeLocalToolArgs(call.Function.Arguments)
			trySend(msgCh, Message{Type: MessageToolUse, Tool: call.Function.Name, CallID: call.ID, Input: args})
			result := b.dispatch(ctx, call.Function.Name, args)
			if len(result) > firtalLocalToolOutputCap {
				result = result[:firtalLocalToolOutputCap] + "\n…[truncated]"
			}
			trySend(msgCh, Message{Type: MessageToolResult, Tool: call.Function.Name, CallID: call.ID, Output: result})
			history = append(history, firtalLocalMessage{Role: "tool", ToolCallID: call.ID, Content: result})
		}
	}

	// Round budget exhausted: one final tool-free call to force a text answer.
	history = append(history, firtalLocalMessage{
		Role:    "user",
		Content: "Stop using tools now. Write your final answer to the user as plain text.",
	})
	resp, err := b.complete(ctx, cfg, history, nil)
	if err != nil {
		return "", usage, err
	}
	accumulate(resp)
	content, _ := firtalLocalExtract(resp)
	return strings.TrimSpace(content), usage, nil
}

func (b *firtalLocalBackend) complete(ctx context.Context, cfg firtalLocalConfig, messages []firtalLocalMessage, tools []firtalLocalToolDef) (firtalLocalResponse, error) {
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": cfg.MaxTokens,
		"stream":     false,
		"messages":   messages,
	}
	if cfg.Temperature != nil {
		body["temperature"] = *cfg.Temperature
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return firtalLocalResponse{}, fmt.Errorf("marshal local request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return firtalLocalResponse{}, fmt.Errorf("build local request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return firtalLocalResponse{}, fmt.Errorf("call local model: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return firtalLocalResponse{}, fmt.Errorf("read local response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return firtalLocalResponse{}, fmt.Errorf("local model returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 2048))
	}

	var parsed firtalLocalResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return firtalLocalResponse{}, fmt.Errorf("parse local response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return firtalLocalResponse{}, fmt.Errorf("local model error: %s", parsed.Error.Message)
	}
	return parsed, nil
}

func firtalLocalExtract(resp firtalLocalResponse) (string, []firtalLocalToolCall) {
	if len(resp.Choices) == 0 {
		return "", nil
	}
	msg := resp.Choices[0].Message
	calls := make([]firtalLocalToolCall, 0, len(msg.ToolCalls))
	for _, c := range msg.ToolCalls {
		if strings.TrimSpace(c.Function.Name) == "" {
			continue
		}
		if c.Type == "" {
			c.Type = "function"
		}
		calls = append(calls, c)
	}
	return msg.Content, calls
}

// dispatch routes a tool call to the configured runner (or the real CLI runner)
// and converts any error into a textual result the model can read and recover
// from, rather than aborting the whole task.
func (b *firtalLocalBackend) dispatch(ctx context.Context, name string, args map[string]any) string {
	runner := b.runTool
	if runner == nil {
		runner = b.runToolCLI
	}
	out, err := runner(ctx, name, args)
	if err != nil {
		return fmt.Sprintf("tool error: %s", err.Error())
	}
	return out
}

// runToolCLI executes a read-only multica tool via the authenticated `multica`
// CLI present on the runtime host. The set is deliberately small and read-only;
// the model's final text answer is what gets posted as the issue comment.
func (b *firtalLocalBackend) runToolCLI(ctx context.Context, name string, args map[string]any) (string, error) {
	issueID := strings.TrimSpace(toolArgString(args, "issue_id"))
	switch name {
	case "get_issue":
		if issueID == "" {
			return "", fmt.Errorf("get_issue requires issue_id")
		}
		return b.execMultica(ctx, "issue", "get", issueID, "--output", "json")
	case "list_comments":
		if issueID == "" {
			return "", fmt.Errorf("list_comments requires issue_id")
		}
		return b.execMultica(ctx, "issue", "comment", "list", issueID, "--output", "json")
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (b *firtalLocalBackend) execMultica(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "multica", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// firtalLocalToolDefs returns the read-only tool surface offered to the local
// model. Kept in lock-step with runToolCLI.
func firtalLocalToolDefs() []firtalLocalToolDef {
	issueIDParam := map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Issue UUID or identifier (e.g. TECH-123).",
			},
		},
	}
	return []firtalLocalToolDef{
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "get_issue",
			Description: "Get a Multica issue's title, description, status, and comments. issue_id accepts a UUID or an identifier like TECH-123.",
			Parameters:  issueIDParam,
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "list_comments",
			Description: "List all comments on a Multica issue in chronological order.",
			Parameters:  issueIDParam,
		}},
	}
}

func firtalLocalSystemPrompt(daemonPrompt string) string {
	base := strings.Join([]string{
		"You are a Multica agent running on a local model. You drive Multica through the provided FUNCTION TOOLS (get_issue, list_comments) — you cannot run shell commands or a `multica` CLI yourself, so ignore any instruction telling you to run CLI commands and call the matching tool instead.",
		"To handle an issue: first call get_issue (and list_comments if you need the discussion) to read the real content, then write your complete answer to the user as your final plain-text message. That final message is what gets posted as your comment on the issue, so make it self-contained — do not say you will do something later.",
		"Always pass the issue id exactly as it appears in the task (a UUID or an identifier like TECH-123).",
	}, "\n\n")
	if p := strings.TrimSpace(daemonPrompt); p != "" {
		return base + "\n\n---\n\n" + p
	}
	return base
}

func decodeLocalToolArgs(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func toolArgString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch v := args[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// firtalLocalStaticModels lists the local Ollama models exposed for selection.
// The local runtime is the source of truth for what is actually pulled; this
// list is the convenience default surfaced in the UI.
func firtalLocalStaticModels() []Model {
	return []Model{
		{ID: "gemma4:12b-it-qat", Label: "Gemma 4 12B (local)", Provider: "ollama", Default: true},
		{ID: "gemma4:26b-a4b-it-qat", Label: "Gemma 4 26B (local)", Provider: "ollama"},
		{ID: "qwen2.5:14b", Label: "Qwen2.5 14B (local)", Provider: "ollama"},
	}
}
