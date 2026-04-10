package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// hermesBackend implements Backend by spawning `hermes acp` and communicating
// via JSON-RPC 2.0 over stdin/stdout using the ACP (Agent Client Protocol).
// The protocol flow mirrors the Codex backend: initialize → session/new →
// session/prompt, with session/update notifications for streaming output.
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

	cmd := exec.CommandContext(runCtx, execPath, "acp")
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

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
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[hermes:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start hermes: %w", err)
	}

	b.cfg.Logger.Info("hermes started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

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
	}

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

		// 1. Initialize handshake (ACP spec: protocolVersion is an integer, not a string).
		_, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"title":   "Multica Agent SDK",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{
				"fs": map[string]any{
					"readTextFile":  false,
					"writeTextFile": false,
				},
				"terminal": false,
			},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("hermes initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// 2. Create session
		sessionResult, err := c.request(runCtx, "session/new", map[string]any{
			"cwd":        opts.Cwd,
			"mcpServers": []any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("hermes session/new failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		sessionID := extractHermesSessionID(sessionResult)
		if sessionID == "" {
			finalStatus = "failed"
			finalError = "hermes session/new returned no session ID"
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.sessionID = sessionID
		b.cfg.Logger.Info("hermes session created", "session_id", sessionID)

		// 3. Send prompt — use session/load if resuming, else session/prompt
		promptParams := map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": prompt},
			},
		}

		if opts.ResumeSessionID != "" {
			// For resume, use session/load first, then session/prompt.
			_, loadErr := c.request(runCtx, "session/load", map[string]any{
				"sessionId":   opts.ResumeSessionID,
				"cwd":         opts.Cwd,
				"mcpServers":  []any{},
			})
			if loadErr != nil {
				b.cfg.Logger.Warn("hermes session/load failed, starting fresh", "error", loadErr)
			}
		}

		promptResult, err := c.request(runCtx, "session/prompt", promptParams)
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("hermes session/prompt failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// session/prompt returns after execution completes with a stopReason.
		// Check for cancellation vs normal completion.
		stopReason := extractHermesStopReason(promptResult)
		switch stopReason {
		case "cancelled":
			finalStatus = "aborted"
			finalError = "turn was cancelled"
		case "max_tokens", "max_turn_requests":
			finalStatus = "completed"
		default:
			// "end_turn" or empty — normal completion.
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("hermes finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Close stdin and cancel to signal the ACP server to exit.
		stdin.Close()
		cancel()

		// Wait for the reader goroutine to finish so all output is accumulated.
		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Build usage map from accumulated hermes usage.
		var usage map[string]TokenUsage
		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// ── hermesClient: ACP JSON-RPC 2.0 transport ──

type hermesClient struct {
	cfg       Config
	stdin     interface{ Write([]byte) (int, error) }
	mu        sync.Mutex
	nextID    int
	pending   map[int]*pendingRPC
	sessionID string
	onMessage func(Message)

	usageMu sync.Mutex
	usage   TokenUsage
}

func (c *hermesClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
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
	if _, err := c.stdin.Write(data); err != nil {
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

func (c *hermesClient) respond(id int, result any) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
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

	// Check if it's a response to our request.
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		// Server request (has id + method).
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleServerRequest(raw)
			return
		}
	}

	// Notification (no id, has method).
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *hermesClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		return
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
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

func (c *hermesClient) handleServerRequest(raw map[string]json.RawMessage) {
	var id int
	_ = json.Unmarshal(raw["id"], &id)

	var method string
	_ = json.Unmarshal(raw["method"], &method)

	// Auto-approve all permission requests in daemon mode.
	switch method {
	case "session/request_permission":
		c.respond(id, map[string]any{
			"outcome": "allow_once",
		})
	default:
		c.respond(id, map[string]any{})
	}
}

func (c *hermesClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}

	switch method {
	case "session/update":
		c.handleSessionUpdate(params)
	}
}

// handleSessionUpdate processes ACP session/update notifications, which carry
// the streaming output from the agent. Per the ACP spec, each notification
// carries a single update in the "update" field with a "sessionUpdate" type:
// agent_message_chunk, tool_call, tool_call_update, plan.
func (c *hermesClient) handleSessionUpdate(params map[string]any) {
	if params == nil {
		return
	}

	// ACP spec: params.update (singular object with sessionUpdate field).
	update, ok := params["update"].(map[string]any)
	if !ok {
		return
	}
	c.processSingleUpdate(update)
}

func (c *hermesClient) processSingleUpdate(update map[string]any) {
	// ACP spec uses "sessionUpdate" to identify the update kind.
	updateType, _ := update["sessionUpdate"].(string)
	// Fallback: some implementations may use "type" instead.
	if updateType == "" {
		updateType, _ = update["type"].(string)
	}

	switch updateType {
	case "agent_message_chunk":
		c.handleContentUpdate(update)
	case "tool_call":
		c.handleToolCallUpdate(update)
	case "tool_call_update":
		c.handleToolCallUpdate(update)
	case "plan":
		// Plan updates don't map to messages; skip.
	}
}

// handleContentUpdate processes text and thinking content chunks from
// ACP session/update notifications. The ACP spec sends agent_message_chunk
// with a "content" field containing a single content block:
//   {"type": "text", "text": "..."}  or  {"type": "thinking", "text": "..."}
func (c *hermesClient) handleContentUpdate(update map[string]any) {
	content, _ := update["content"].(map[string]any)
	if content == nil {
		return
	}

	contentType, _ := content["type"].(string)
	text, _ := content["text"].(string)

	if text == "" || c.onMessage == nil {
		return
	}

	switch contentType {
	case "thinking":
		c.onMessage(Message{Type: MessageThinking, Content: text})
	case "text":
		c.onMessage(Message{Type: MessageText, Content: text})
	}
}

// handleToolCallUpdate processes tool call start and completion events from
// ACP session/update notifications. ACP uses tool_call for start and
// tool_call_update for progress/completion.
func (c *hermesClient) handleToolCallUpdate(update map[string]any) {
	callID, _ := update["toolCallId"].(string)
	toolName, _ := update["title"].(string)
	status, _ := update["status"].(string)

	// For tool_call (start), status may be "pending" or empty.
	// For tool_call_update, status is "in_progress" or "completed".
	switch status {
	case "pending", "":
		// Tool call started.
		var input map[string]any
		if raw, ok := update["input"]; ok {
			if m, ok := raw.(map[string]any); ok {
				input = m
			}
		}
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: callID,
				Input:  input,
			})
		}
	case "in_progress":
		// Tool is running; no message needed.
	case "completed", "failed":
		// Tool call completed. ACP puts results in "content" (array of content blocks).
		var outputStr string
		if raw, ok := update["content"]; ok {
			outputStr = extractACPContentOutput(raw)
		}
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   toolName,
				CallID: callID,
				Output: outputStr,
			})
		}
	}

	// Extract token usage from tool call updates if present.
	c.extractUsageFromUpdate(update)
}

// extractUsageFromUpdate tries to extract token usage from ACP session/update
// notifications. Usage data may appear in the _meta field.
func (c *hermesClient) extractUsageFromUpdate(update map[string]any) {
	meta, ok := update["_meta"].(map[string]any)
	if !ok {
		return
	}
	usage, ok := meta["usage"].(map[string]any)
	if !ok {
		return
	}

	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	c.usage.InputTokens += hermesInt64(usage, "inputTokens", "input_tokens")
	c.usage.OutputTokens += hermesInt64(usage, "outputTokens", "output_tokens")
	c.usage.CacheReadTokens += hermesInt64(usage, "cacheReadTokens", "cache_read_tokens")
	c.usage.CacheWriteTokens += hermesInt64(usage, "cacheWriteTokens", "cache_write_tokens")
}

// hermesInt64 safely extracts an int64 from a JSON-decoded map value.
func hermesInt64(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		v, ok := data[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			if n != 0 {
				return int64(n)
			}
		case int64:
			if n != 0 {
				return n
			}
		}
	}
	return 0
}

// extractHermesSessionID extracts the session ID from a session/new response.
func extractHermesSessionID(result json.RawMessage) string {
	var r struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.SessionID
}

// extractHermesStopReason extracts the stopReason from a session/prompt response.
func extractHermesStopReason(result json.RawMessage) string {
	var r struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.StopReason
}

// extractACPContentOutput extracts text from an ACP tool_call_update content field.
// ACP content is an array of content blocks, each with {type, content: {type, text}}.
func extractACPContentOutput(raw any) string {
	blocks, ok := raw.([]any)
	if !ok {
		return extractToolOutput(raw)
	}

	var parts []string
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		// Each block may have a nested "content" object with "text".
		if inner, ok := block["content"].(map[string]any); ok {
			if text, ok := inner["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		// Or the block itself may have "text" directly.
		if text, ok := block["text"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}
