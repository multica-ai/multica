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
// Tools are executed via the authenticated `multica` CLI already present on the
// runtime host (the same CLI every other agent uses). The tool list is fetched
// from GET /api/agents/{id}/tools at task start and filtered to the locally-
// implementable subset — so the same grant configuration that governs cloud
// agents governs firtal-local as well. When no grants are configured, all
// locally-implementable tools are exposed.
//
// If the model calls add_comment during the loop its final text output is
// suppressed so the daemon does not post a second comment on top.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
	// firtalLocalGrantFetchTimeout caps the server round-trip for the grant list.
	firtalLocalGrantFetchTimeout = 5 * time.Second
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
	tools := b.buildToolList(ctx)

	history := []firtalLocalMessage{
		{Role: "system", Content: firtalLocalSystemPrompt(opts.SystemPrompt, tools)},
		{Role: "user", Content: prompt},
	}

	var usage TokenUsage
	accumulate := func(r firtalLocalResponse) {
		if r.Usage != nil {
			usage.InputTokens += r.Usage.PromptTokens
			usage.OutputTokens += r.Usage.CompletionTokens
		}
	}

	addCommentUsed := false

	for round := 0; round < cfg.MaxToolRound; round++ {
		resp, err := b.complete(ctx, cfg, history, tools)
		if err != nil {
			return "", usage, err
		}
		accumulate(resp)
		content, calls := firtalLocalExtract(resp)

		if len(calls) == 0 {
			output := strings.TrimSpace(content)
			// If the model already posted via add_comment, suppress the final
			// text so the daemon does not create a duplicate comment.
			if addCommentUsed {
				return "", usage, nil
			}
			return output, usage, nil
		}

		history = append(history, firtalLocalMessage{Role: "assistant", Content: content, ToolCalls: calls})
		for _, call := range calls {
			if call.Function.Name == "add_comment" {
				addCommentUsed = true
			}
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
	if addCommentUsed {
		return "", usage, nil
	}
	return strings.TrimSpace(content), usage, nil
}

// buildToolList determines which tools to expose to the model for this task.
// When HTTP env vars are set (server URL + task token + agent ID), it fetches
// the full tool definitions (with JSON schemas) from the server — this gives
// access to ALL granted tools including connections (web_fetch, firtal_registry,
// Google Sheets, etc.). Falls back to the locally-implementable CLI tool set
// filtered by grants, or to all CLI tools when no grants are configured.
func (b *firtalLocalBackend) buildToolList(ctx context.Context) []firtalLocalToolDef {
	if b.hasHTTPEnv() {
		if tools, ok := b.fetchGrantedToolDefs(ctx); ok {
			return tools
		}
	}
	// CLI fallback: filter the 9 locally-implementable tools by grant list.
	all := firtalLocalAllToolDefs()
	granted, hasGrants := b.fetchGrantedTools(ctx)
	if !hasGrants {
		return all
	}
	var filtered []firtalLocalToolDef
	for _, t := range all {
		if granted[t.Function.Name] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return all
	}
	return filtered
}

// hasHTTPEnv returns true when the three env vars required for server-side
// tool invocation are present in the task config.
func (b *firtalLocalBackend) hasHTTPEnv() bool {
	return firstEnv(b.cfg.Env, "MULTICA_SERVER_URL") != "" &&
		firstEnv(b.cfg.Env, "MULTICA_TOKEN") != "" &&
		firstEnv(b.cfg.Env, "MULTICA_AGENT_ID") != ""
}

// fetchGrantedToolDefs queries GET /api/agents/{id}/tools and builds a full
// firtalLocalToolDef list from the response (using InputSchema from the server).
// Tools without a schema in the response are skipped — they cannot be described
// to the model. Returns (nil, false) when the fetch fails or yields no tools.
func (b *firtalLocalBackend) fetchGrantedToolDefs(ctx context.Context) ([]firtalLocalToolDef, bool) {
	serverURL := strings.TrimRight(firstEnv(b.cfg.Env, "MULTICA_SERVER_URL"), "/")
	token := firstEnv(b.cfg.Env, "MULTICA_TOKEN")
	agentID := firstEnv(b.cfg.Env, "MULTICA_AGENT_ID")

	fetchCtx, cancel := context.WithTimeout(ctx, firtalLocalGrantFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, serverURL+"/api/agents/"+agentID+"/tools", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false
	}

	var items []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Enabled     bool            `json:"enabled"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, false
	}

	var tools []firtalLocalToolDef
	for _, item := range items {
		if !item.Enabled || len(item.InputSchema) == 0 {
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(item.InputSchema, &schema); err != nil {
			continue
		}
		tools = append(tools, firtalLocalToolDef{
			Type: "function",
			Function: firtalLocalToolFunction{
				Name:        item.Name,
				Description: item.Description,
				Parameters:  schema,
			},
		})
	}
	if len(tools) == 0 {
		return nil, false
	}
	return tools, true
}

// fetchGrantedTools queries GET /api/agents/{id}/tools to find which tools
// are enabled for the current agent. Returns (map, true) when at least one
// tool is enabled. Returns (nil, false) on any error or empty grant list,
// signalling the caller to fall back to the full local tool set.
func (b *firtalLocalBackend) fetchGrantedTools(ctx context.Context) (map[string]bool, bool) {
	serverURL := strings.TrimRight(firstEnv(b.cfg.Env, "MULTICA_SERVER_URL"), "/")
	token := firstEnv(b.cfg.Env, "MULTICA_TOKEN")
	agentID := firstEnv(b.cfg.Env, "MULTICA_AGENT_ID")

	if serverURL == "" || token == "" || agentID == "" {
		return nil, false
	}

	fetchCtx, cancel := context.WithTimeout(ctx, firtalLocalGrantFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, serverURL+"/api/agents/"+agentID+"/tools", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false
	}

	var items []struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, false
	}

	enabled := make(map[string]bool)
	for _, it := range items {
		if it.Enabled {
			enabled[it.Name] = true
		}
	}
	if len(enabled) == 0 {
		return nil, false
	}
	return enabled, true
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

// dispatch routes a tool call to the configured runner and converts any error
// into a textual result the model can read and recover from rather than
// aborting the whole task.
// Priority: injected runTool (tests) → runToolHTTP (when env vars present) → runToolCLI.
func (b *firtalLocalBackend) dispatch(ctx context.Context, name string, args map[string]any) string {
	runner := b.runTool
	if runner == nil {
		if b.hasHTTPEnv() {
			runner = b.runToolHTTP
		} else {
			runner = b.runToolCLI
		}
	}
	out, err := runner(ctx, name, args)
	if err != nil {
		return fmt.Sprintf("tool error: %s", err.Error())
	}
	return out
}

// runToolHTTP executes a tool via POST /api/agents/{id}/tools/{name}/invoke on
// the Multica server. This is the primary execution path when the task-scoped
// env vars (MULTICA_SERVER_URL, MULTICA_TOKEN, MULTICA_AGENT_ID) are present.
// It gives firtal-local the same tool surface as firtal-gateway including
// connections that require server-side credentials.
func (b *firtalLocalBackend) runToolHTTP(ctx context.Context, name string, args map[string]any) (string, error) {
	serverURL := strings.TrimRight(firstEnv(b.cfg.Env, "MULTICA_SERVER_URL"), "/")
	token := firstEnv(b.cfg.Env, "MULTICA_TOKEN")
	agentID := firstEnv(b.cfg.Env, "MULTICA_AGENT_ID")

	reqBody, err := json.Marshal(map[string]any{"args": args})
	if err != nil {
		return "", fmt.Errorf("marshal tool args: %w", err)
	}

	endpoint := serverURL + "/api/agents/" + agentID + "/tools/" + url.PathEscape(name) + "/invoke"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build invoke request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("invoke request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.Unmarshal(respBody, &errResp) //nolint:errcheck
		if errResp.Error != "" {
			return "", fmt.Errorf("%s", errResp.Error)
		}
		return "", fmt.Errorf("invoke returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse invoke response: %w", err)
	}
	return result.Result, nil
}

// runToolCLI executes a multica tool via the authenticated `multica` CLI
// present on the runtime host. Each tool maps to one or more multica subcommands.
func (b *firtalLocalBackend) runToolCLI(ctx context.Context, name string, args map[string]any) (string, error) {
	switch name {
	case "get_issue":
		issueID := strings.TrimSpace(toolArgString(args, "issue_id"))
		if issueID == "" {
			return "", fmt.Errorf("get_issue requires issue_id")
		}
		return b.execMultica(ctx, "issue", "get", issueID, "--output", "json")

	case "list_comments":
		issueID := strings.TrimSpace(toolArgString(args, "issue_id"))
		if issueID == "" {
			return "", fmt.Errorf("list_comments requires issue_id")
		}
		return b.execMultica(ctx, "issue", "comment", "list", issueID, "--output", "json")

	case "add_comment":
		issueID := strings.TrimSpace(toolArgString(args, "issue_id"))
		content := strings.TrimSpace(toolArgString(args, "content"))
		if issueID == "" {
			return "", fmt.Errorf("add_comment requires issue_id")
		}
		if content == "" {
			return "", fmt.Errorf("add_comment requires content")
		}
		// Use --content-stdin to safely handle multi-line content and quotes.
		return b.execMulticaStdin(ctx, content, "issue", "comment", "add", issueID, "--content-stdin")

	case "list_issues":
		cliArgs := []string{"issue", "list", "--output", "json"}
		if s := toolArgString(args, "status"); s != "" {
			cliArgs = append(cliArgs, "--status", s)
		}
		if a := toolArgString(args, "assignee"); a != "" {
			cliArgs = append(cliArgs, "--assignee", a)
		}
		if l := toolArgString(args, "limit"); l != "" {
			cliArgs = append(cliArgs, "--limit", l)
		}
		return b.execMultica(ctx, cliArgs...)

	case "create_issue":
		title := strings.TrimSpace(toolArgString(args, "title"))
		if title == "" {
			return "", fmt.Errorf("create_issue requires title")
		}
		cliArgs := []string{"issue", "create", "--title", title, "--output", "json"}
		if p := toolArgString(args, "priority"); p != "" {
			cliArgs = append(cliArgs, "--priority", p)
		}
		if s := toolArgString(args, "status"); s != "" {
			cliArgs = append(cliArgs, "--status", s)
		}
		if parent := toolArgString(args, "parent_id"); parent != "" {
			cliArgs = append(cliArgs, "--parent", parent)
		}
		desc := strings.TrimSpace(toolArgString(args, "description"))
		if desc != "" {
			// Use --description-stdin so multi-line markdown is passed safely.
			return b.execMulticaStdin(ctx, desc, append(cliArgs, "--description-stdin")...)
		}
		return b.execMultica(ctx, cliArgs...)

	case "update_issue":
		issueID := strings.TrimSpace(toolArgString(args, "issue_id"))
		if issueID == "" {
			return "", fmt.Errorf("update_issue requires issue_id")
		}
		cliArgs := []string{"issue", "update", issueID}
		if t := toolArgString(args, "title"); t != "" {
			cliArgs = append(cliArgs, "--title", t)
		}
		if s := toolArgString(args, "status"); s != "" {
			cliArgs = append(cliArgs, "--status", s)
		}
		if p := toolArgString(args, "priority"); p != "" {
			cliArgs = append(cliArgs, "--priority", p)
		}
		return b.execMultica(ctx, cliArgs...)

	case "assign_issue":
		issueID := strings.TrimSpace(toolArgString(args, "issue_id"))
		assignee := strings.TrimSpace(toolArgString(args, "assignee"))
		if issueID == "" {
			return "", fmt.Errorf("assign_issue requires issue_id")
		}
		if assignee == "" {
			return "", fmt.Errorf("assign_issue requires assignee")
		}
		return b.execMultica(ctx, "issue", "assign", issueID, "--to", assignee)

	case "list_projects":
		return b.execMultica(ctx, "project", "list", "--output", "json")

	case "get_me":
		result := map[string]any{
			"agent_id":     firstEnv(b.cfg.Env, "MULTICA_AGENT_ID"),
			"name":         firstEnv(b.cfg.Env, "MULTICA_AGENT_NAME"),
			"workspace_id": firstEnv(b.cfg.Env, "MULTICA_WORKSPACE_ID"),
		}
		raw, _ := json.Marshal(result)
		return string(raw), nil

	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// execMultica runs a multica CLI subcommand, merging the task-scoped env
// (MULTICA_TOKEN, MULTICA_SERVER_URL, etc.) on top of the OS environment
// so the right credentials are used even when the daemon has a different token.
func (b *firtalLocalBackend) execMultica(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "multica", args...)
	cmd.Env = b.buildCLIEnv()
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

// execMulticaStdin runs a multica CLI subcommand with a string piped to stdin.
// Used for commands that accept --content-stdin / --description-stdin to safely
// pass multi-line or special-character content without shell quoting issues.
func (b *firtalLocalBackend) execMulticaStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "multica", args...)
	cmd.Env = b.buildCLIEnv()
	cmd.Stdin = strings.NewReader(stdin)
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

// buildCLIEnv merges the task-scoped env vars (MULTICA_TOKEN, MULTICA_SERVER_URL,
// etc.) on top of the OS environment. Task-scoped vars take precedence so the
// multica CLI uses the right credentials for this task rather than any ambient
// daemon-level credentials that happen to be in the process environment.
func (b *firtalLocalBackend) buildCLIEnv() []string {
	if len(b.cfg.Env) == 0 {
		return nil // inherit OS env unchanged
	}
	base := os.Environ()
	// Build a map so we can override keys without duplicates.
	merged := make(map[string]string, len(base)+len(b.cfg.Env))
	for _, kv := range base {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			merged[kv[:idx]] = kv[idx+1:]
		}
	}
	for k, v := range b.cfg.Env {
		merged[k] = v
	}
	result := make([]string, 0, len(merged))
	for k, v := range merged {
		result = append(result, k+"="+v)
	}
	return result
}

// firtalLocalAllToolDefs returns the complete set of tools that firtal-local
// can implement via the multica CLI. This list is filtered at runtime by the
// agent's grant configuration from the server.
func firtalLocalAllToolDefs() []firtalLocalToolDef {
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
			Description: "Get a Multica issue's title, description, status, and metadata. issue_id accepts a UUID or an identifier like TECH-123.",
			Parameters:  issueIDParam,
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "list_comments",
			Description: "List all comments on a Multica issue in chronological order.",
			Parameters:  issueIDParam,
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "add_comment",
			Description: "Post a new comment on a Multica issue authored by you (the calling agent).",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"issue_id", "content"},
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "Issue UUID or identifier (e.g. TECH-123).",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Markdown body of the comment.",
					},
				},
			},
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "list_issues",
			Description: "List issues in the workspace. Optional filters: status, assignee, limit.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{},
				"properties": map[string]any{
					"status": map[string]any{
						"type":        "string",
						"description": "Filter by status: todo, in_progress, in_review, done, blocked, backlog, cancelled.",
					},
					"assignee": map[string]any{
						"type":        "string",
						"description": "Filter by assignee name or agent name.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max issues to return (default 50).",
					},
				},
			},
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "create_issue",
			Description: "Create a new Multica issue.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"title"},
				"properties": map[string]any{
					"title":       map[string]any{"type": "string", "description": "Issue title."},
					"description": map[string]any{"type": "string", "description": "Issue description in markdown."},
					"priority":    map[string]any{"type": "string", "description": "none, low, medium, high, urgent."},
					"status":      map[string]any{"type": "string", "description": "todo, in_progress, in_review, done, blocked, backlog, cancelled."},
					"parent_id":   map[string]any{"type": "string", "description": "Parent issue UUID or identifier for sub-issues."},
				},
			},
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "update_issue",
			Description: "Update fields on an existing Multica issue.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"issue_id"},
				"properties": map[string]any{
					"issue_id": map[string]any{"type": "string", "description": "Issue UUID or identifier (e.g. TECH-123)."},
					"title":    map[string]any{"type": "string", "description": "New title."},
					"status":   map[string]any{"type": "string", "description": "New status."},
					"priority": map[string]any{"type": "string", "description": "New priority."},
				},
			},
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "assign_issue",
			Description: "Assign a Multica issue to a member or agent by name.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"issue_id", "assignee"},
				"properties": map[string]any{
					"issue_id": map[string]any{"type": "string", "description": "Issue UUID or identifier."},
					"assignee": map[string]any{"type": "string", "description": "Name of the member or agent to assign to."},
				},
			},
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "list_projects",
			Description: "List all projects in the workspace.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{},
				"properties": map[string]any{},
			},
		}},
		{Type: "function", Function: firtalLocalToolFunction{
			Name:        "get_me",
			Description: "Return your own agent ID, name, and workspace context.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{},
				"properties": map[string]any{},
			},
		}},
	}
}

// firtalLocalSystemPrompt builds the system prompt for the model.
// The tool list is injected so the prompt accurately describes what is available.
func firtalLocalSystemPrompt(daemonPrompt string, tools []firtalLocalToolDef) string {
	hasAddComment := false
	for _, t := range tools {
		if t.Function.Name == "add_comment" {
			hasAddComment = true
			break
		}
	}

	var replyInstruction string
	if hasAddComment {
		replyInstruction = "To reply on the issue: use add_comment with the issue_id and your full answer. After calling add_comment, do not also write a plain-text final answer — that would create a duplicate comment."
	} else {
		replyInstruction = "To reply on the issue: write your complete answer as your final plain-text message. That message is posted as your comment, so make it self-contained."
	}

	base := strings.Join([]string{
		"You are a Multica agent running on a local model. Use the provided FUNCTION TOOLS for all Multica operations — reading issues, posting comments, managing tasks. Do not try to run shell commands or the `multica` CLI directly; the tools handle all platform access.",
		"To handle an issue: first call get_issue (and list_comments for the discussion thread) to read the real content. Then act on what you find.",
		replyInstruction,
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
