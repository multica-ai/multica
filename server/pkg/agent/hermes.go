package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// hermesBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport; overriding it
// would break the daemon↔Hermes communication contract.
var hermesBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

// hermesBackend implements Backend by spawning `hermes acp` and communicating
// via the ACP (Agent Communication Protocol) JSON-RPC 2.0 over stdin/stdout.
// This is the same pattern as Codex but with the ACP protocol instead of
// the Codex-specific JSON-RPC methods.
type hermesBackend struct {
	cfg Config
}

func (b *hermesBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "hermes"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("hermes executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	hermesArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, hermesBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, hermesArgs...)
	b.cfg.Logger.Debug("agent command", "exec", execPath, "args", hermesArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	// Enable yolo mode so Hermes auto-approves all tool executions.
	env = append(env, "HERMES_YOLO_MODE=1")
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stdin pipe: %w", err)
	}
	// Forward stderr to the daemon log *and* sniff provider-level
	// errors out of it so we can surface them in the task result.
	// Hermes' session/prompt still reports stopReason=end_turn when
	// the underlying HTTP call to the LLM returns 4xx/5xx, so
	// without this we'd report a misleading "empty output" and hide
	// the real cause (wrong model for the current provider, bad
	// credentials, rate limit, …) in the daemon log.
	providerErr := newACPProviderErrorSniffer("hermes")
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[hermes:stderr] "), providerErr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start hermes: %w", err)
	}

	b.cfg.Logger.Info("hermes acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	promptDone := make(chan hermesPromptResult, 1)

	c := &hermesClient{
		cfg:     b.cfg,
		stdin:   stdin,
		pending: make(map[int]*pendingRPC),
		onMessage: func(msg Message) {
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
		},
		onPromptDone: func(result hermesPromptResult) {
			select {
			case promptDone <- result:
			default:
			}
		},
	}

	// Start reading stdout in background.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("hermes process exited"))
	}()

	// Drive the ACP session lifecycle in a goroutine.
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string

		// 1. Initialize handshake.
		_, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("hermes initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":       cwd,
				"sessionId": opts.ResumeSessionID,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = opts.ResumeSessionID
			_ = result
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "hermes session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("hermes session created", "session_id", sessionID)

		// 3. If the caller picked a model (via agent.model from the
		// UI dropdown), ask hermes to switch the session to it
		// before we send any prompt. Hermes' _build_model_state
		// exposes modelId as `provider:model` — we pass that
		// through verbatim. This MUST fail the task on error:
		// if we silently fell back to hermes' default model the
		// user would think their pick was honoured while the
		// task actually ran on something else.
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("hermes set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes could not switch to model %q: %v", opts.Model, err)
				resCh <- Result{
					Status:     finalStatus,
					Error:      finalError,
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  sessionID,
				}
				return
			}
			b.cfg.Logger.Info("hermes session model set", "model", opts.Model)
		}

		// 4. Build the prompt content. If we have a system prompt, prepend it.
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// 5. Send the prompt and wait for PromptResponse.
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
		})
		if err != nil {
			// If the request itself failed (not just context cancelled),
			// check if the context was cancelled/timed out.
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("hermes timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/prompt failed: %v", err)
			}
		} else {
			// The prompt completed. Check if we got a promptDone result
			// from the response parsing.
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "hermes cancelled the prompt"
				}
				// Merge usage from the PromptResponse.
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("hermes finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Close stdin and cancel context to signal hermes acp to exit.
		stdin.Close()
		cancel()

		// Wait for the reader goroutine to finish so all output is accumulated.
		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// If hermes produced no visible output but we sniffed a
		// provider-level error on stderr (typically HTTP 4xx from
		// the configured LLM endpoint), promote the status to
		// failed and surface the real reason. Without this the
		// daemon reports a cryptic "hermes returned empty output"
		// and the actionable error (e.g. "model X not supported
		// with your ChatGPT account") stays buried in daemon logs.
		if finalStatus == "completed" && finalOutput == "" {
			if msg := providerErr.message(); msg != "" {
				finalStatus = "failed"
				finalError = msg
			}
		}

		// Build usage map.
		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// ── hermesClient: ACP JSON-RPC 2.0 transport ──

type hermesPromptResult struct {
	stopReason string
	usage      TokenUsage
}

type hermesClient struct {
	cfg          Config
	stdin        interface{ Write([]byte) (int, error) }
	writeMu      sync.Mutex // serialises stdin.Write calls across goroutines
	mu           sync.Mutex
	nextID       int
	pending      map[int]*pendingRPC
	sessionID    string
	onMessage    func(Message)
	onPromptDone func(hermesPromptResult)

	usageMu sync.Mutex
	usage   TokenUsage
}

// writeLine serialises concurrent JSON-RPC writes so request() (main
// goroutine) and handleAgentRequest() (reader goroutine) don't
// interleave frames. The pipe itself is atomic for small writes, but
// we also want deterministic ordering under contention.
func (c *hermesClient) writeLine(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(data)
	return err
}

func (c *hermesClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *hermesClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *hermesClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// Agent → client request: has id + method (no result / error yet).
	// Kimi uses this for session/request_permission; if we don't answer,
	// the agent blocks for 300s and the task hangs. Hermes doesn't send
	// these when launched with HERMES_YOLO_MODE=1, but we still handle
	// the case generically for any future ACP backend we bolt on.
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleAgentRequest(raw)
			return
		}
	}

	// Notification (no id, has method) — session updates from Hermes.
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

// handleAgentRequest replies to JSON-RPC requests the agent sends
// us (agent → client direction). The only one we care about today is
// `session/request_permission`: the daemon is headless and cannot
// actually prompt a user, so we auto-approve every action. Using
// `approve_for_session` rather than `approve` means subsequent
// identical actions (every Shell invocation, every file write) don't
// round-trip through us — the agent remembers them locally.
func (c *hermesClient) handleAgentRequest(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	rawID, ok := raw["id"]
	if !ok {
		return
	}

	var resp map[string]any
	switch method {
	case "session/request_permission":
		resp = map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(rawID),
			"result": map[string]any{
				"outcome": map[string]any{
					"outcome":  "selected",
					"optionId": "approve_for_session",
				},
			},
		}
		c.cfg.Logger.Debug("auto-approved agent permission request", "method", method)
	default:
		// Unknown agent→client method — reply with standard "method
		// not found" so the agent doesn't block waiting for us. Better
		// than silence: the agent can decide how to proceed.
		resp = map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(rawID),
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found: " + method,
			},
		}
		c.cfg.Logger.Debug("unhandled agent→client request", "method", method)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		c.cfg.Logger.Warn("marshal agent-request response", "method", method, "error", err)
		return
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.cfg.Logger.Warn("write agent-request response", "method", method, "error", err)
	}
}

func (c *hermesClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		// Try float (JSON numbers are floats by default).
		var fid float64
		if err := json.Unmarshal(raw["id"], &fid); err != nil {
			return
		}
		id = int(fid)
	}

	c.mu.Lock()
	pr, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if !ok {
		return
	}

	if errData, hasErr := raw["error"]; hasErr {
		var rpcErr struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		pr.ch <- rpcResult{err: fmt.Errorf("%s: %s (code=%d)", pr.method, rpcErr.Message, rpcErr.Code)}
	} else {
		// If this is a prompt response, extract usage and stop reason.
		if pr.method == "session/prompt" {
			c.extractPromptResult(raw["result"])
		}
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

func (c *hermesClient) extractPromptResult(data json.RawMessage) {
	var resp struct {
		StopReason string `json:"stopReason"`
		Usage      *struct {
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			TotalTokens      int64 `json:"totalTokens"`
			ThoughtTokens    int64 `json:"thoughtTokens"`
			CachedReadTokens int64 `json:"cachedReadTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	pr := hermesPromptResult{
		stopReason: resp.StopReason,
	}
	if resp.Usage != nil {
		pr.usage = TokenUsage{
			InputTokens:     resp.Usage.InputTokens,
			OutputTokens:    resp.Usage.OutputTokens,
			CacheReadTokens: resp.Usage.CachedReadTokens,
		}
	}

	if c.onPromptDone != nil {
		c.onPromptDone(pr)
	}
}

func (c *hermesClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	if method != "session/update" {
		return
	}

	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	if len(params.Update) == 0 {
		return
	}

	// Parse the update discriminator.
	var updateType struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	_ = json.Unmarshal(params.Update, &updateType)

	switch updateType.SessionUpdate {
	case "agent_message_chunk":
		c.handleAgentMessage(params.Update)
	case "agent_thought_chunk":
		c.handleAgentThought(params.Update)
	case "tool_call":
		c.handleToolCallStart(params.Update)
	case "tool_call_update":
		c.handleToolCallUpdate(params.Update)
	case "usage_update":
		c.handleUsageUpdate(params.Update)
	}
}

func (c *hermesClient) handleAgentMessage(data json.RawMessage) {
	var msg struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Content.Text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageText, Content: msg.Content.Text})
	}
}

func (c *hermesClient) handleAgentThought(data json.RawMessage) {
	var msg struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Content.Text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageThinking, Content: msg.Content.Text})
	}
}

func (c *hermesClient) handleToolCallStart(data json.RawMessage) {
	var msg struct {
		ToolCallID string            `json:"toolCallId"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	toolName := hermesToolNameFromTitle(msg.Title, msg.Kind)
	input := msg.RawInput
	// kimi-cli emits the tool input as ACP `content` blocks (text type),
	// not `rawInput`. Fall back so the UI shows "echo hello" instead of
	// a blank input bubble. See kimi_cli/acp/session.py::_send_tool_call.
	if input == nil {
		if text := extractACPToolCallText(msg.Content); text != "" {
			input = map[string]any{"text": text}
		}
	}
	if c.onMessage != nil {
		c.onMessage(Message{
			Type:   MessageToolUse,
			Tool:   toolName,
			CallID: msg.ToolCallID,
			Input:  input,
		})
	}
}

func (c *hermesClient) handleToolCallUpdate(data json.RawMessage) {
	var msg struct {
		ToolCallID string            `json:"toolCallId"`
		Status     string            `json:"status"`
		Kind       string            `json:"kind"`
		RawOutput  string            `json:"rawOutput"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Only emit tool result when the call is completed.
	if msg.Status != "completed" && msg.Status != "failed" {
		return
	}

	output := msg.RawOutput
	// kimi-cli returns Shell / file-tool output via `content` blocks on
	// the completion update, not `rawOutput`. Fall back so the UI shows
	// the command's stdout instead of an empty terminal panel. See
	// kimi_cli/acp/session.py::_send_tool_result.
	if output == "" {
		output = extractACPToolCallText(msg.Content)
	}
	if c.onMessage != nil {
		c.onMessage(Message{
			Type:   MessageToolResult,
			CallID: msg.ToolCallID,
			Output: output,
		})
	}
}

// extractACPToolCallText concatenates the text of every ACP
// `ContentToolCallContent` block (shape: {"type":"content",
// "content":{"type":"text","text":"..."}}) from a tool_call /
// tool_call_update's `content` array. Non-text blocks (terminal,
// diff, image) are skipped — the client UI doesn't have a way to
// render raw terminal_id references, so a text concatenation is the
// most useful thing we can surface.
func extractACPToolCallText(blocks []json.RawMessage) string {
	var b strings.Builder
	for _, raw := range blocks {
		var outer struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &outer); err != nil {
			continue
		}
		if outer.Type != "content" || len(outer.Content) == 0 {
			continue
		}
		var inner struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(outer.Content, &inner); err != nil {
			continue
		}
		if inner.Type != "text" || inner.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(inner.Text)
	}
	return b.String()
}

func (c *hermesClient) handleUsageUpdate(data json.RawMessage) {
	var msg struct {
		Usage struct {
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			TotalTokens      int64 `json:"totalTokens"`
			CachedReadTokens int64 `json:"cachedReadTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	c.usageMu.Lock()
	// Usage updates from ACP are cumulative snapshots, so take the latest.
	if msg.Usage.InputTokens > c.usage.InputTokens {
		c.usage.InputTokens = msg.Usage.InputTokens
	}
	if msg.Usage.OutputTokens > c.usage.OutputTokens {
		c.usage.OutputTokens = msg.Usage.OutputTokens
	}
	if msg.Usage.CachedReadTokens > c.usage.CacheReadTokens {
		c.usage.CacheReadTokens = msg.Usage.CachedReadTokens
	}
	c.usageMu.Unlock()
}

// ── Helpers ──

// extractACPSessionID pulls `sessionId` out of a session/new or
// session/resume response. Shared by all ACP backends (hermes, kimi,
// and anything else that follows the standard ACP schema).
func extractACPSessionID(result json.RawMessage) string {
	var r struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.SessionID
}

// hermesToolNameFromTitle extracts a tool name from the ACP tool call title.
// Hermes ACP titles look like "terminal: ls -la", "read: /path/to/file", etc.
// Some titles have no colon (e.g. "execute code").
func hermesToolNameFromTitle(title string, kind string) string {
	// Check exact-match titles first (no colon).
	switch title {
	case "execute code":
		return "execute_code"
	}

	// Try to extract the tool name from before the first colon.
	if idx := strings.Index(title, ":"); idx > 0 {
		name := strings.TrimSpace(title[:idx])
		// Map common ACP title prefixes back to tool names.
		// Some titles include mode info like "patch (replace)", so check prefix.
		switch {
		case name == "terminal":
			return "terminal"
		case name == "read":
			return "read_file"
		case name == "write":
			return "write_file"
		case strings.HasPrefix(name, "patch"):
			return "patch"
		case name == "search":
			return "search_files"
		case name == "web search":
			return "web_search"
		case name == "extract":
			return "web_extract"
		case name == "delegate":
			return "delegate_task"
		case name == "analyze image":
			return "vision_analyze"
		}
		return name
	}

	// Fall back to kind.
	switch kind {
	case "read":
		return "read_file"
	case "edit":
		return "write_file"
	case "execute":
		return "terminal"
	case "search":
		return "search_files"
	case "fetch":
		return "web_search"
	case "think":
		return "thinking"
	default:
		// Preserve a non-empty title when we can't classify it: kimi
		// emits bare titles like "Shell" or "Read file" without any
		// `kind`, so returning an empty string here drops the tool
		// name entirely before kimiToolNameFromTitle can map it.
		// Hermes titles always carry a colon, so hermes never reaches
		// this branch with a non-empty title.
		if title != "" {
			return title
		}
		return kind
	}
}

// ── Provider-error sniffing ──
//
// ACP agents (hermes, kimi, …) all have the same failure mode:
// session/prompt reports stopReason=end_turn even when the underlying
// HTTP call to the configured LLM endpoint returned an error — the
// actionable detail only appears on stderr (e.g.
// `⚠️ API call failed (attempt 1/3): BadRequestError [HTTP 400]` and
// `Error: HTTP 400: Error code: 400 - {'detail': "The '...' model
// is not supported when using Codex with a ChatGPT account."}`).
// The sniffer scans for those patterns so the daemon can surface a
// real failure instead of a generic "empty output".
//
// Parameterised by provider name so both hermes and kimi can share
// the transport: the regexes match format-level signals (HTTP status,
// error-kind tags, "API call failed" banner) that both runtimes emit.
type acpProviderErrorSniffer struct {
	provider string
	mu       sync.Mutex
	remains  []byte   // buffer for a partial trailing line across writes
	lines    []string // captured error lines, bounded
	seen     map[string]bool
}

// acpErrorHeaderRe matches the first line of an API-error block.
// ACP agents typically prefix these with ⚠️ / ❌ and include an HTTP
// status code or a non-retryable-error tag.
var acpErrorHeaderRe = regexp.MustCompile(`(?:⚠️|❌|\[ERROR\]).*(?:BadRequestError|AuthenticationError|RateLimitError|HTTP [0-9]{3}|Non-retryable|API call failed)`)

// acpErrorDetailRe pulls the most useful single-line messages out of
// the subsequent lines of the error block (the one whose "Error:" or
// "Details:" tag actually spells out what happened).
var acpErrorDetailRe = regexp.MustCompile(`(?:Error:|detail:|Details:)\s*(.+)`)

const acpMaxErrorLines = 8

// newACPProviderErrorSniffer returns a sniffer that tags its messages
// with the given provider name (e.g. "hermes", "kimi") so failure
// strings make it obvious which runtime produced the error.
func newACPProviderErrorSniffer(provider string) *acpProviderErrorSniffer {
	return &acpProviderErrorSniffer{provider: provider, seen: map[string]bool{}}
}

// Write implements io.Writer so the sniffer can sit behind an
// io.MultiWriter next to the normal stderr log forwarder.
func (s *acpProviderErrorSniffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := append(s.remains, p...)
	// Keep the final partial line (no trailing newline) for the
	// next write so multi-line error blocks aren't split.
	nl := strings.LastIndexByte(string(data), '\n')
	var complete string
	if nl < 0 {
		s.remains = append(s.remains[:0], data...)
		return len(p), nil
	}
	complete = string(data[:nl])
	s.remains = append(s.remains[:0], data[nl+1:]...)

	for _, line := range strings.Split(complete, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !(acpErrorHeaderRe.MatchString(line) || acpErrorDetailRe.MatchString(line)) {
			continue
		}
		if s.seen[line] {
			continue
		}
		s.seen[line] = true
		s.lines = append(s.lines, line)
		if len(s.lines) > acpMaxErrorLines {
			s.lines = s.lines[len(s.lines)-acpMaxErrorLines:]
		}
	}
	return len(p), nil
}

// message returns a single-line summary suitable for the task
// error field. Prefers the most specific "Error:" / "detail:"
// fragment; falls back to the first captured header line; empty
// when nothing useful was seen.
func (s *acpProviderErrorSniffer) message() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := s.provider + " provider error: "
	for _, line := range s.lines {
		if m := acpErrorDetailRe.FindStringSubmatch(line); m != nil {
			detail := strings.TrimSpace(m[1])
			if detail != "" {
				return prefix + detail
			}
		}
	}
	for _, line := range s.lines {
		if acpErrorHeaderRe.MatchString(line) {
			return prefix + line
		}
	}
	return ""
}
