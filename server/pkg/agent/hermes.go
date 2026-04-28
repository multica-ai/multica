package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
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
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", hermesArgs)
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
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
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
			result, err := c.request(runCtx, "session/new", buildHermesSessionParams(cwd, opts.Model))
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
		var providerError *ProviderError
		if finalStatus == "completed" && finalOutput == "" {
			if pe, ok := providerErr.classify(); ok {
				finalStatus = "failed"
				finalError = pe.Error()
				providerError = pe
			} else if msg := providerErr.message(); msg != "" {
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
			Status:        finalStatus,
			Output:        finalOutput,
			Error:         finalError,
			ProviderError: providerError,
			DurationMs:    duration.Milliseconds(),
			SessionID:     sessionID,
			Usage:         usageMap,
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
	// acceptNotification can drop ACP session updates before dispatching to
	// handlers that mutate client state such as usage or pending tool calls.
	acceptNotification func(updateType string) bool

	// pendingTools buffers the args for tool calls whose input streams in
	// across multiple ACP tool_call_update messages (kimi does this —
	// tokens from the LLM arrive one at a time, and each update carries
	// the cumulative args JSON so far). We defer emitting MessageToolUse
	// until we either see status=completed/failed or have a full arg set,
	// so the UI never sees a half-written command like `{"comma`.
	toolMu       sync.Mutex
	pendingTools map[string]*pendingToolCall

	usageMu sync.Mutex
	usage   TokenUsage
}

// pendingToolCall buffers state for a tool call while its arguments
// are streaming in. One entry per ACP toolCallId.
type pendingToolCall struct {
	toolName string         // already mapped via hermesToolNameFromTitle
	input    map[string]any // from rawInput when the agent sends it up front (hermes)
	argsText string         // accumulated `content[].text` args (kimi, cumulative)
	emitted  bool           // whether we've already sent MessageToolUse
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

	if method != "session/update" && method != "session/notification" {
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

	updateType, updateData := normalizeACPUpdate(params.Update)
	if c.acceptNotification != nil && !c.acceptNotification(updateType) {
		return
	}

	switch updateType {
	case "agent_message_chunk":
		c.handleAgentMessage(updateData)
	case "agent_thought_chunk":
		c.handleAgentThought(updateData)
	case "tool_call":
		c.handleToolCallStart(updateData)
	case "tool_call_update":
		c.handleToolCallUpdate(updateData)
	case "usage_update":
		c.handleUsageUpdate(updateData)
	case "turn_end":
		c.extractPromptResult(updateData)
	}
}

func normalizeACPUpdate(data json.RawMessage) (string, json.RawMessage) {
	var updateType struct {
		SessionUpdate string `json:"sessionUpdate"`
		Type          string `json:"type"`
	}
	_ = json.Unmarshal(data, &updateType)
	if updateType.SessionUpdate != "" {
		return normalizeACPUpdateType(updateType.SessionUpdate), data
	}
	if updateType.Type != "" {
		return normalizeACPUpdateType(updateType.Type), data
	}

	// Some ACP implementations serialize enum variants as an externally
	// tagged object: {"agentMessageChunk": {"content": ...}}.
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper) == 1 {
		for k, v := range wrapper {
			return normalizeACPUpdateType(k), v
		}
	}

	return "", data
}

func normalizeACPUpdateType(t string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(t), "_", ""), "-", ""))
	switch key {
	case "agentmessagechunk":
		return "agent_message_chunk"
	case "agentthoughtchunk":
		return "agent_thought_chunk"
	case "toolcall":
		return "tool_call"
	case "toolcallupdate":
		return "tool_call_update"
	case "usageupdate":
		return "usage_update"
	case "turnend", "endturn":
		return "turn_end"
	default:
		return ""
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
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	toolName := hermesToolNameFromTitle(msg.Title, msg.Kind)
	if toolName == "" {
		toolName = msg.Name
	}
	rawInput := msg.RawInput
	if rawInput == nil {
		rawInput = msg.Input
	}
	if rawInput == nil {
		rawInput = msg.Parameters
	}

	// Hermes pre-populates rawInput on the initial tool_call — emit
	// MessageToolUse immediately so the UI can show the tool invocation
	// live. Record the emission so handleToolCallUpdate doesn't re-emit
	// on completion.
	if rawInput != nil {
		c.trackTool(msg.ToolCallID, &pendingToolCall{
			toolName: toolName,
			input:    rawInput,
			emitted:  true,
		})
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: msg.ToolCallID,
				Input:  rawInput,
			})
		}
		return
	}

	// Kimi streams args token-by-token across tool_call_update messages;
	// the initial tool_call often carries an empty content block. Buffer
	// the tool and defer MessageToolUse emission to avoid the UI seeing
	// a command with `{""` as its input.
	c.trackTool(msg.ToolCallID, &pendingToolCall{
		toolName: toolName,
		argsText: extractACPToolCallText(msg.Content),
		emitted:  false,
	})
}

func (c *hermesClient) handleToolCallUpdate(data json.RawMessage) {
	var msg struct {
		ToolCallID string            `json:"toolCallId"`
		Status     string            `json:"status"`
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		RawOutput  string            `json:"rawOutput"`
		Output     string            `json:"output"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	rawInput := msg.RawInput
	if rawInput == nil {
		rawInput = msg.Input
	}
	if rawInput == nil {
		rawInput = msg.Parameters
	}
	title := msg.Title
	if title == "" {
		title = msg.Name
	}

	// Mid-stream: only buffer updates. Kimi emits many of these per
	// tool call, each carrying the cumulative args JSON so far.
	if msg.Status != "completed" && msg.Status != "failed" {
		if pending := c.getPendingTool(msg.ToolCallID); pending != nil && !pending.emitted {
			if text := extractACPToolCallText(msg.Content); text != "" {
				// kimi streams the full cumulative args on every frame;
				// overwrite rather than concatenate.
				pending.argsText = text
			}
		}
		return
	}

	// Completion: emit any deferred MessageToolUse first, then the result.
	pending := c.takePendingTool(msg.ToolCallID)
	c.emitDeferredToolUse(pending, msg.ToolCallID, title, msg.Kind, rawInput)

	output := msg.RawOutput
	if output == "" {
		output = msg.Output
	}
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

// trackTool stores pending-tool state for a given callID. Lazy-inits
// the map so zero-value hermesClient values (common in tests) don't
// panic on the first tool call.
func (c *hermesClient) trackTool(callID string, p *pendingToolCall) {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		c.pendingTools = make(map[string]*pendingToolCall)
	}
	c.pendingTools[callID] = p
}

// getPendingTool returns the pending entry (may be nil) without
// removing it. Safe to call on a zero-value hermesClient.
func (c *hermesClient) getPendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	return c.pendingTools[callID]
}

// takePendingTool removes and returns the pending entry, or nil if
// none was tracked (e.g. the tool completed before we saw its start,
// or we missed the start frame).
func (c *hermesClient) takePendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	p := c.pendingTools[callID]
	delete(c.pendingTools, callID)
	return p
}

// emitDeferredToolUse emits a buffered MessageToolUse right before the
// matching MessageToolResult. Handles three cases:
//   - hermes tool: already emitted on tool_call → skip
//   - kimi tool with streamed args → parse accumulated JSON as Input
//   - unknown tool (completed arrived without a start frame) →
//     synthesize minimal info from the update's own fields
func (c *hermesClient) emitDeferredToolUse(
	p *pendingToolCall,
	callID, updateTitle, updateKind string,
	updateRawInput map[string]any,
) {
	if p != nil && p.emitted {
		return
	}

	var toolName string
	var input map[string]any

	switch {
	case p != nil && p.input != nil:
		// Pre-buffered rawInput path — shouldn't happen because we set
		// emitted=true in that case, but handle defensively.
		toolName = p.toolName
		input = p.input
	case p != nil:
		toolName = p.toolName
		input = parseToolArgsJSON(p.argsText)
	default:
		// No record of the start frame — fall back to the update's own
		// title/kind/rawInput so the UI at least sees the tool name.
		toolName = hermesToolNameFromTitle(updateTitle, updateKind)
		input = updateRawInput
	}

	if c.onMessage == nil {
		return
	}
	c.onMessage(Message{
		Type:   MessageToolUse,
		Tool:   toolName,
		CallID: callID,
		Input:  input,
	})
}

// parseToolArgsJSON turns kimi's accumulated args string into the
// structured map the UI expects under Message.Input. Kimi sends args
// as a JSON-encoded object (`{"command":"echo hi"}`), so a full JSON
// parse recovers the original tool-arg shape. On malformed input
// (streaming glitch, non-JSON tool) we preserve the raw text under a
// `text` key so the UI still has something to render.
func parseToolArgsJSON(argsText string) map[string]any {
	argsText = strings.TrimSpace(argsText)
	if argsText == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(argsText), &m); err == nil {
		return m
	}
	return map[string]any{"text": argsText}
}

// extractACPToolCallText concatenates the rendered text of every ACP
// block in a tool_call / tool_call_update's `content` array.
//
// Handles the two block types kimi emits:
//   - {type:"content", content:{type:"text", text:"..."}} — plain text
//     (shell output, tool args). Text is concatenated verbatim.
//   - {type:"diff", path, oldText, newText} — FileEdit output. Rendered
//     as a minimal unified-diff header so the UI distinguishes writes
//     from reads without needing a diff viewer.
//
// Terminal blocks ({type:"terminal", terminalId}) reference a remote
// terminal the client would normally subscribe to via terminal/output;
// we don't advertise terminal capability so we never receive those in
// practice, but if one slips through we skip it (nothing useful to
// surface from a bare ID).
func extractACPToolCallText(blocks []json.RawMessage) string {
	var b strings.Builder
	appendPiece := func(piece string) {
		if piece == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(piece)
	}
	for _, raw := range blocks {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			continue
		}
		switch kind.Type {
		case "content":
			var outer struct {
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(raw, &outer); err != nil || len(outer.Content) == 0 {
				continue
			}
			var inner struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(outer.Content, &inner); err != nil {
				continue
			}
			if inner.Type != "text" {
				continue
			}
			appendPiece(inner.Text)
		case "diff":
			var diff struct {
				Path    string `json:"path"`
				OldText string `json:"oldText"`
				NewText string `json:"newText"`
			}
			if err := json.Unmarshal(raw, &diff); err != nil || diff.Path == "" {
				continue
			}
			// Keep it tiny — a full unified diff can be huge and we're
			// really just recording "this tool wrote to this file".
			// The UI can re-read the file if it needs the actual content.
			var piece strings.Builder
			piece.WriteString("--- ")
			piece.WriteString(diff.Path)
			piece.WriteString("\n+++ ")
			piece.WriteString(diff.Path)
			if diff.OldText == "" {
				piece.WriteString("\n(new file, ")
				piece.WriteString(strconv.Itoa(len(diff.NewText)))
				piece.WriteString(" bytes)")
			} else {
				piece.WriteString("\n(edited: ")
				piece.WriteString(strconv.Itoa(len(diff.OldText)))
				piece.WriteString(" → ")
				piece.WriteString(strconv.Itoa(len(diff.NewText)))
				piece.WriteString(" bytes)")
			}
			appendPiece(piece.String())
		default:
			// terminal blocks, image blocks, unknown future types —
			// ignore. We have no way to inline-render them.
		}
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
// session/resume response. Shared by all ACP backends (hermes, kimi, kiro,
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

// buildHermesSessionParams constructs the params map for the ACP `session/new`
// request. The `model` field is only included when non-empty so Hermes falls
// back to its default only when no explicit model was configured.
func buildHermesSessionParams(cwd, model string) map[string]any {
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	if model != "" {
		params["model"] = model
	}
	return params
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

// acpHTTPStatusRe extracts an HTTP status code from a sniffed line.
var acpHTTPStatusRe = regexp.MustCompile(`HTTP\s+([0-9]{3})`)

// acpRetryAfterRe extracts a Retry-After value (seconds) from a sniffed line.
var acpRetryAfterRe = regexp.MustCompile(`(?i)retry[-_]after[:=\s]+(\d+)`)

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
		if !(acpErrorHeaderRe.MatchString(line) || acpErrorDetailRe.MatchString(line) || acpRetryAfterRe.MatchString(line)) {
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

// classify inspects the sniffed stderr lines and returns a structured
// ProviderError when a transient provider failure is detected.
// It also extracts Retry-After hints when present.
func (s *acpProviderErrorSniffer) classify() (*ProviderError, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.lines) == 0 {
		return nil, false
	}

	var code ErrorCode
	var retryAfter time.Duration

	for _, line := range s.lines {
		if m := acpHTTPStatusRe.FindStringSubmatch(line); m != nil {
			status := m[1]
			switch status {
			case "429":
				code = ErrRateLimited
			case "502", "503", "504":
				code = ErrGatewayError
			case "500":
				code = ErrServiceUnavailable
			}
		}
		if strings.Contains(line, "RateLimitError") {
			code = ErrRateLimited
		}
		if strings.Contains(line, "timeout") || strings.Contains(line, "timed out") {
			code = ErrTimeout
		}
		if strings.Contains(line, "context deadline exceeded") || strings.Contains(line, "context canceled") {
			code = ErrContextExceeded
		}
		if strings.Contains(line, "quota") || strings.Contains(line, "Quota exceeded") || strings.Contains(line, "insufficient_quota") {
			code = ErrQuotaExhausted
		}
		if m := acpRetryAfterRe.FindStringSubmatch(line); m != nil {
			if secs, err := strconv.Atoi(m[1]); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
	}

	if code == "" {
		return nil, false
	}

	msg := s.provider + " provider error"
	for _, line := range s.lines {
		if m := acpErrorDetailRe.FindStringSubmatch(line); m != nil {
			if detail := strings.TrimSpace(m[1]); detail != "" {
				msg = s.provider + " provider error: " + detail
				break
			}
		}
	}

	pe := &ProviderError{Code: code, Message: msg}
	if retryAfter > 0 {
		pe.RetryAfter = retryAfter
	}
	return pe, true
}
