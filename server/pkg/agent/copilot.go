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
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// copilotBackend implements Backend by spawning the GitHub Copilot CLI in
// ACP mode and communicating via JSON-RPC 2.0 over stdin/stdout.
type copilotBackend struct {
	cfg Config
}

// copilotEventState holds mutable state accumulated while processing the JSONL
// event stream. It is shared between production (Execute) and tests via
// handleCopilotEvent, so the parsing logic is never duplicated.
type copilotEventState struct {
	output      strings.Builder
	sessionID   string
	activeModel string
	finalStatus string
	finalError  string
	usage       map[string]TokenUsage
}

func newCopilotEventState(seedModel string) *copilotEventState {
	return &copilotEventState{
		activeModel: seedModel,
		finalStatus: "completed",
		usage:       make(map[string]TokenUsage),
	}
}

// handleCopilotEvent processes a single parsed copilotEvent, updates state,
// and returns zero or more Messages to emit. Extracted so tests can call the
// exact same logic without duplicating the switch body.
func handleCopilotEvent(evt copilotEvent, st *copilotEventState, trace TraceCallback) []Message {
	var msgs []Message

	switch evt.Type {
	case "session.start":
		var ss copilotSessionStart
		if err := json.Unmarshal(evt.Data, &ss); err == nil {
			if ss.SelectedModel != "" {
				st.activeModel = ss.SelectedModel
			}
			if ss.SessionID != "" {
				st.sessionID = ss.SessionID
			}
		}

	case "assistant.message_delta":
		var delta copilotMessageDelta
		if err := json.Unmarshal(evt.Data, &delta); err == nil && delta.DeltaContent != "" {
			st.output.WriteString(delta.DeltaContent)
			msgs = append(msgs, Message{Type: MessageText, Content: delta.DeltaContent})
			emitDisplayEvent(trace, "assistant_text", "Copilot", delta.DeltaContent, nil)
		}

	case "assistant.message":
		var msg copilotAssistantMessage
		if err := json.Unmarshal(evt.Data, &msg); err != nil {
			return nil
		}
		if msg.Content != "" {
			trimmed := strings.TrimSuffix(st.output.String(), msg.Content)
			st.output.Reset()
			st.output.WriteString(trimmed)
			if st.output.Len() > 0 && !strings.HasSuffix(st.output.String(), "\n\n") {
				st.output.WriteString("\n\n")
			}
			st.output.WriteString(msg.Content)
		}
		if msg.ReasoningText != "" {
			msgs = append(msgs, Message{Type: MessageThinking, Content: msg.ReasoningText})
			emitDisplayEvent(trace, "thinking", "Thinking", msg.ReasoningText, nil)
		}
		if msg.OutputTokens > 0 {
			u := st.usage[st.activeModel]
			u.OutputTokens += msg.OutputTokens
			st.usage[st.activeModel] = u
		}
		for _, tr := range msg.ToolRequests {
			var input map[string]any
			if tr.Arguments != nil {
				_ = json.Unmarshal(tr.Arguments, &input)
			}
			msgs = append(msgs, Message{
				Type:   MessageToolUse,
				Tool:   tr.Name,
				CallID: tr.ToolCallID,
				Input:  input,
			})
			emitDisplayEvent(trace, "tool_call", tr.Name, "", map[string]any{"call_id": tr.ToolCallID, "input": input})
		}

	case "assistant.reasoning", "assistant.reasoning_delta":
		var r copilotReasoning
		if err := json.Unmarshal(evt.Data, &r); err == nil {
			text := r.Content
			if text == "" {
				text = r.DeltaContent
			}
			if text != "" {
				msgs = append(msgs, Message{Type: MessageThinking, Content: text})
				emitDisplayEvent(trace, "thinking", "Thinking", text, nil)
			}
		}

	case "tool.execution_complete":
		var tc copilotToolExecComplete
		if err := json.Unmarshal(evt.Data, &tc); err != nil {
			return nil
		}
		if tc.Model != "" {
			st.activeModel = tc.Model
		}
		resultContent := ""
		if tc.Success && tc.Result != nil {
			resultContent = tc.Result.Content
		} else if !tc.Success {
			if tc.Error != nil {
				resultContent = "Error: " + tc.Error.Message
			} else if tc.Result != nil {
				resultContent = tc.Result.Content
			}
		}
		msgs = append(msgs, Message{
			Type:   MessageToolResult,
			CallID: tc.ToolCallID,
			Output: resultContent,
		})
		emitDisplayEvent(trace, "tool_result", "Tool result", resultContent, map[string]any{"call_id": tc.ToolCallID})

	case "assistant.turn_start":
		msgs = append(msgs, Message{Type: MessageStatus, Status: "running"})
		emitDisplayEvent(trace, "status", "Copilot", "running", nil)

	case "session.error":
		var se copilotSessionError
		if err := json.Unmarshal(evt.Data, &se); err == nil {
			st.finalStatus = "failed"
			st.finalError = se.Message
			msgs = append(msgs, Message{Type: MessageLog, Level: "error", Content: se.Message})
		}

	case "session.warning":
		var sw copilotSessionWarning
		if err := json.Unmarshal(evt.Data, &sw); err == nil {
			msgs = append(msgs, Message{Type: MessageLog, Level: "warn", Content: sw.Message})
		}

	case "result":
		if evt.SessionID != "" {
			st.sessionID = evt.SessionID
		}
		if evt.ExitCode != 0 {
			st.finalStatus = "failed"
			st.finalError = withCopilotExitCode(st.finalError, evt.ExitCode)
		}
	}

	return msgs
}

func withCopilotExitCode(msg string, exitCode int) string {
	exitMsg := fmt.Sprintf("copilot exited with code %d", exitCode)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return exitMsg
	}
	if strings.Contains(msg, exitMsg) {
		return msg
	}
	return msg + "; " + exitMsg
}

func (b *copilotBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "copilot"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("copilot executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildCopilotArgs(opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("copilot stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("copilot stdin pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[copilot:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if opts.TraceCallback != nil {
		cmd.Stderr = io.MultiWriter(stderrBuf, newTraceWriter("raw_stderr", opts.TraceCallback))
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start copilot: %w", err)
	}

	b.cfg.Logger.Info("copilot started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	promptDone := make(chan copilotPromptResult, 1)

	c := &copilotACPClient{
		cfg:           b.cfg,
		stdin:         stdin,
		pending:       make(map[int]*pendingRPC),
		pendingTools:  make(map[string]*pendingToolCall),
		onApproval:    opts.OnApproval,
		traceCallback: opts.TraceCallback,
		runCtx:        runCtx,
		onMessage: func(msg Message) {
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
		},
		onPromptDone: func(result copilotPromptResult) {
			select {
			case promptDone <- result:
			default:
			}
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
		if err := scanner.Err(); err != nil {
			slog.Warn("copilot stdout scanner error", "err", err)
		}
		c.closeAllPending(fmt.Errorf("copilot process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			c.flushText()
			c.flushThinking()
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string

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
			finalError = withAgentStderr(fmt.Sprintf("copilot initialize failed: %v", err), "copilot", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.notify("notifications/initialized")

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}
		sessionParams := buildCopilotSessionParams(cwd, opts)
		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			sessionResult, err = c.request(runCtx, "session/resume", map[string]any{
				"cwd":       cwd,
				"sessionId": opts.ResumeSessionID,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = withAgentStderr(fmt.Sprintf("copilot session/resume failed: %v", err), "copilot", stderrBuf.Tail())
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID, _ = resolveResumedSessionID(opts.ResumeSessionID, sessionResult)
		} else {
			sessionResult, err = c.request(runCtx, "session/new", sessionParams)
			if err != nil {
				finalStatus = "failed"
				finalError = withAgentStderr(fmt.Sprintf("copilot session/new failed: %v", err), "copilot", stderrBuf.Tail())
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(sessionResult)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "copilot session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}
		c.sessionID = sessionID

		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": prompt},
			},
		})
		if err != nil {
			switch runCtx.Err() {
			case context.DeadlineExceeded:
				finalStatus = "timeout"
				finalError = fmt.Sprintf("copilot timed out after %s", timeout)
			case context.Canceled:
				finalStatus = "aborted"
				finalError = "execution cancelled"
			default:
				finalStatus = "failed"
				finalError = fmt.Sprintf("copilot session/prompt failed: %v", err)
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" || pr.stopReason == "canceled" {
					finalStatus = "aborted"
					finalError = "copilot cancelled the prompt"
				}
				c.addUsage(pr.usage)
			default:
			}
		}

		c.flushText()
		c.flushThinking()
		stdin.Close()
		cancel()
		<-readerDone

		if finalError != "" {
			finalError = withAgentStderr(finalError, "copilot", stderrBuf.Tail())
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("copilot finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		emitDisplayEvent(opts.TraceCallback, "status", "Copilot", finalStatus, map[string]any{"error": finalError})

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()
		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "copilot"
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

// ── Copilot ACP JSON-RPC client ──

type copilotPromptResult struct {
	stopReason string
	usage      TokenUsage
}

type copilotACPClient struct {
	cfg           Config
	stdin         interface{ Write([]byte) (int, error) }
	writeMu       sync.Mutex
	mu            sync.Mutex
	nextID        int
	pending       map[int]*pendingRPC
	sessionID     string
	onMessage     func(Message)
	onPromptDone  func(copilotPromptResult)
	onApproval    ApprovalCallback
	traceCallback TraceCallback
	runCtx        context.Context

	toolMu       sync.Mutex
	pendingTools map[string]*pendingToolCall

	usageMu sync.Mutex
	usage   TokenUsage

	textMu     sync.Mutex
	textBuffer strings.Builder

	thinkMu     sync.Mutex
	thinkBuffer strings.Builder
}

func (c *copilotACPClient) writeLine(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(data)
	return err
}

func (c *copilotACPClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
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

func (c *copilotACPClient) notify(method string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_ = c.writeLine(data)
}

func (c *copilotACPClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *copilotACPClient) handleLine(line string) {
	if c.traceCallback != nil {
		c.traceCallback("raw_stdout", line, "")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	if c.traceCallback != nil {
		c.traceCallback("provider_event", "", line)
	}

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
			c.flushText()
			c.flushThinking()
			c.handleAgentRequest(raw)
			return
		}
	}

	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *copilotACPClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
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
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		if len(rpcErr.Data) > 0 && string(rpcErr.Data) != "null" {
			pr.ch <- rpcResult{err: fmt.Errorf("%s: %s (code=%d, data=%s)", pr.method, rpcErr.Message, rpcErr.Code, string(rpcErr.Data))}
		} else {
			pr.ch <- rpcResult{err: fmt.Errorf("%s: %s (code=%d)", pr.method, rpcErr.Message, rpcErr.Code)}
		}
		return
	}

	if pr.method == "session/prompt" {
		c.extractPromptResult(raw["result"])
	}
	pr.ch <- rpcResult{result: raw["result"]}
}

func (c *copilotACPClient) handleAgentRequest(raw map[string]json.RawMessage) {
	rawID, ok := raw["id"]
	if !ok {
		return
	}
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	switch method {
	case "session/request_permission":
		optionID := c.choosePermissionOption(raw["params"])
		c.respondRaw(rawID, map[string]any{
			"outcome": map[string]any{
				"outcome":  "selected",
				"optionId": optionID,
			},
		})
	default:
		c.respondErrorRaw(rawID, -32601, "method not found: "+method)
	}
}

func (c *copilotACPClient) respond(id int, result any) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.cfg.Logger.Warn("write copilot response", "error", err)
	}
}

func (c *copilotACPClient) respondRaw(rawID json.RawMessage, result any) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(rawID),
		"result":  result,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.cfg.Logger.Warn("write copilot response", "error", err)
	}
}

func (c *copilotACPClient) respondError(id int, code int, message string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.cfg.Logger.Warn("write copilot error response", "error", err)
	}
}

func (c *copilotACPClient) respondErrorRaw(rawID json.RawMessage, code int, message string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(rawID),
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.cfg.Logger.Warn("write copilot error response", "error", err)
	}
}

type copilotPermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
}

type copilotPermissionParams struct {
	Options  []copilotPermissionOption `json:"options"`
	ToolCall struct {
		ToolCallID string         `json:"toolCallId"`
		Title      string         `json:"title"`
		Kind       string         `json:"kind"`
		RawInput   map[string]any `json:"rawInput"`
		Input      map[string]any `json:"input"`
		Parameters map[string]any `json:"parameters"`
	} `json:"toolCall"`
}

func (c *copilotACPClient) choosePermissionOption(raw json.RawMessage) string {
	params := parseCopilotPermissionParams(raw)
	deny := findCopilotPermissionOption(params.Options, true)
	allow := findCopilotPermissionOption(params.Options, false)
	if deny == "" {
		deny = "deny"
	}
	if allow == "" {
		allow = deny
	}
	if c.onApproval == nil {
		return allow
	}

	req := buildCopilotApprovalRequest(params, deny)
	chosen, approved, err := c.onApproval(c.runCtx, req)
	chosen, _ = SplitApprovalChoice(chosen)
	if err != nil || !approved {
		return deny
	}
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		return deny
	}
	for _, opt := range params.Options {
		if chosen == opt.OptionID {
			return chosen
		}
	}
	return deny
}

func parseCopilotPermissionParams(raw json.RawMessage) copilotPermissionParams {
	var params copilotPermissionParams
	_ = json.Unmarshal(raw, &params)
	return params
}

func buildCopilotApprovalRequest(params copilotPermissionParams, defaultOption string) ApprovalRequest {
	title := params.ToolCall.Title
	if title == "" {
		title = "Copilot permission request"
	}
	detail := ""
	input := params.ToolCall.RawInput
	if input == nil {
		input = params.ToolCall.Input
	}
	if input == nil {
		input = params.ToolCall.Parameters
	}
	if input != nil {
		if b, err := json.Marshal(input); err == nil {
			detail = string(b)
		}
	}
	options := make([]protocol.InteractionOption, 0, len(params.Options))
	for _, opt := range params.Options {
		id := strings.TrimSpace(opt.OptionID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(opt.Name)
		if label == "" {
			label = strings.TrimSpace(opt.Label)
		}
		if label == "" {
			label = id
		}
		options = append(options, protocol.InteractionOption{ID: id, Label: label})
	}
	if len(options) == 0 {
		options = []protocol.InteractionOption{
			{ID: "allow_once", Label: "Allow once"},
			{ID: defaultOption, Label: "Deny"},
		}
	}
	return ApprovalRequest{
		Type:          "permission_request",
		Title:         title,
		Detail:        detail,
		Options:       options,
		DefaultOption: defaultOption,
	}
}

func findCopilotPermissionOption(options []copilotPermissionOption, deny bool) string {
	for _, opt := range options {
		id := strings.ToLower(opt.OptionID)
		kind := strings.ToLower(opt.Kind)
		if deny {
			if strings.Contains(id, "deny") || strings.Contains(id, "reject") || strings.Contains(kind, "deny") || strings.Contains(kind, "reject") {
				return opt.OptionID
			}
			continue
		}
		if strings.Contains(id, "session") || strings.Contains(kind, "always") || strings.Contains(kind, "session") {
			return opt.OptionID
		}
	}
	if deny {
		return ""
	}
	for _, opt := range options {
		id := strings.ToLower(opt.OptionID)
		kind := strings.ToLower(opt.Kind)
		if !strings.Contains(id, "deny") && !strings.Contains(id, "reject") && !strings.Contains(kind, "deny") && !strings.Contains(kind, "reject") {
			return opt.OptionID
		}
	}
	return ""
}

func (c *copilotACPClient) handleNotification(raw map[string]json.RawMessage) {
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
	if updateType != "agent_message_chunk" {
		c.flushText()
	}
	if updateType != "agent_thought_chunk" {
		c.flushThinking()
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

func (c *copilotACPClient) handleAgentMessage(data json.RawMessage) {
	var msg struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Content.Text == "" {
		return
	}
	c.appendText(msg.Content.Text)
}

func (c *copilotACPClient) appendText(text string) {
	c.textMu.Lock()
	c.textBuffer.WriteString(text)
	c.textMu.Unlock()
}

func (c *copilotACPClient) flushText() {
	c.textMu.Lock()
	text := c.textBuffer.String()
	c.textBuffer.Reset()
	c.textMu.Unlock()

	if text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageText, Content: text})
	}
	emitDisplayEvent(c.traceCallback, "assistant_text", "Copilot", text, nil)
}

func (c *copilotACPClient) handleAgentThought(data json.RawMessage) {
	var msg struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Content.Text == "" {
		return
	}
	c.appendThinking(msg.Content.Text)
}

func (c *copilotACPClient) appendThinking(text string) {
	c.thinkMu.Lock()
	c.thinkBuffer.WriteString(text)
	c.thinkMu.Unlock()
}

func (c *copilotACPClient) flushThinking() {
	c.thinkMu.Lock()
	text := c.thinkBuffer.String()
	c.thinkBuffer.Reset()
	c.thinkMu.Unlock()

	if text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageThinking, Content: text})
	}
	emitDisplayEvent(c.traceCallback, "thinking", "Thinking", text, nil)
}

func (c *copilotACPClient) handleToolCallStart(data json.RawMessage) {
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
	toolName := copilotToolName(msg.Name, msg.Title, msg.Kind)
	input := msg.RawInput
	if input == nil {
		input = msg.Input
	}
	if input == nil {
		input = msg.Parameters
	}
	if input == nil {
		input = parseToolArgsJSON(extractACPToolCallText(msg.Content))
	}
	c.trackTool(msg.ToolCallID, &pendingToolCall{toolName: toolName, input: input, emitted: true})
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageToolUse, Tool: toolName, CallID: msg.ToolCallID, Input: input})
	}
	emitDisplayEvent(c.traceCallback, "tool_call", toolName, "", map[string]any{"call_id": msg.ToolCallID, "input": input})
}

func (c *copilotACPClient) handleToolCallUpdate(data json.RawMessage) {
	var msg struct {
		ToolCallID string            `json:"toolCallId"`
		Status     string            `json:"status"`
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		RawOutput  json.RawMessage   `json:"rawOutput"`
		Output     string            `json:"output"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if msg.Status != "completed" && msg.Status != "failed" {
		return
	}
	pending := c.takePendingTool(msg.ToolCallID)
	input := msg.RawInput
	if input == nil {
		input = msg.Input
	}
	if input == nil {
		input = msg.Parameters
	}
	if pending == nil || !pending.emitted {
		toolName := copilotToolName(msg.Name, msg.Title, msg.Kind)
		if pending != nil {
			toolName = pending.toolName
			if pending.input != nil {
				input = pending.input
			}
		}
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageToolUse, Tool: toolName, CallID: msg.ToolCallID, Input: input})
		}
		emitDisplayEvent(c.traceCallback, "tool_call", toolName, "", map[string]any{"call_id": msg.ToolCallID, "input": input})
	}
	output := extractCopilotRawOutput(msg.RawOutput)
	if output == "" {
		output = msg.Output
	}
	if output == "" {
		output = extractACPToolCallText(msg.Content)
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageToolResult, CallID: msg.ToolCallID, Output: output})
	}
	emitDisplayEvent(c.traceCallback, "tool_result", "Tool result", output, map[string]any{"call_id": msg.ToolCallID})
}

func (c *copilotACPClient) trackTool(callID string, p *pendingToolCall) {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		c.pendingTools = make(map[string]*pendingToolCall)
	}
	c.pendingTools[callID] = p
}

func (c *copilotACPClient) takePendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	p := c.pendingTools[callID]
	delete(c.pendingTools, callID)
	return p
}

func (c *copilotACPClient) handleUsageUpdate(data json.RawMessage) {
	var msg struct {
		Usage struct {
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			CachedReadTokens int64 `json:"cachedReadTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	c.addUsage(TokenUsage{
		InputTokens:     msg.Usage.InputTokens,
		OutputTokens:    msg.Usage.OutputTokens,
		CacheReadTokens: msg.Usage.CachedReadTokens,
	})
}

func (c *copilotACPClient) extractPromptResult(data json.RawMessage) {
	var resp struct {
		StopReason string `json:"stopReason"`
		Usage      *struct {
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			CachedReadTokens int64 `json:"cachedReadTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	result := copilotPromptResult{stopReason: resp.StopReason}
	if resp.Usage != nil {
		result.usage = TokenUsage{
			InputTokens:     resp.Usage.InputTokens,
			OutputTokens:    resp.Usage.OutputTokens,
			CacheReadTokens: resp.Usage.CachedReadTokens,
		}
	}
	if c.onPromptDone != nil {
		c.onPromptDone(result)
	}
}

func (c *copilotACPClient) addUsage(u TokenUsage) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if u.InputTokens > c.usage.InputTokens {
		c.usage.InputTokens = u.InputTokens
	}
	if u.OutputTokens > c.usage.OutputTokens {
		c.usage.OutputTokens = u.OutputTokens
	}
	if u.CacheReadTokens > c.usage.CacheReadTokens {
		c.usage.CacheReadTokens = u.CacheReadTokens
	}
}

// extractCopilotRawOutput handles rawOutput which can be either a plain string
// or an object like {"content": "...", "detailedContent": "..."}.
func extractCopilotRawOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	// Try object with content field.
	var obj struct {
		Content string `json:"content"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		if obj.Content != "" {
			return obj.Content
		}
		if obj.Message != "" {
			return obj.Message
		}
	}
	return ""
}

func copilotToolName(name, title, kind string) string {
	if name != "" {
		return name
	}
	if t := hermesToolNameFromTitle(title, kind); t != "" {
		return t
	}
	if kind != "" {
		return kind
	}
	return "tool"
}

// ── Copilot CLI JSONL event types ──
//
// Copilot CLI v1.0.28+ with --output-format json emits JSONL on stdout.
// Each line is a JSON object with:
//
//	{ "type": "dotted.event.name", "data": {...}, "id": "...",
//	  "timestamp": "...", "parentId": "...", "ephemeral": bool }
//
// The final line is a synthetic "result" event with top-level fields:
//
//	{ "type": "result", "sessionId": "...", "exitCode": 0, "usage": {...} }

// copilotEvent is the envelope for all Copilot JSONL events.
type copilotEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	ParentID  string          `json:"parentId,omitempty"`
	Ephemeral bool            `json:"ephemeral,omitempty"`

	// Top-level fields on the synthetic "result" event only.
	SessionID string              `json:"sessionId,omitempty"`
	ExitCode  int                 `json:"exitCode,omitempty"`
	Usage     *copilotResultUsage `json:"usage,omitempty"`
}

// copilotSessionStart is data payload for "session.start".
type copilotSessionStart struct {
	SessionID     string `json:"sessionId"`
	SelectedModel string `json:"selectedModel"`
}

// copilotAssistantMessage is data payload for "assistant.message".
type copilotAssistantMessage struct {
	MessageID     string               `json:"messageId"`
	Content       string               `json:"content"`
	ToolRequests  []copilotToolRequest `json:"toolRequests"`
	OutputTokens  int64                `json:"outputTokens"`
	InteractionID string               `json:"interactionId"`
	ReasoningText string               `json:"reasoningText,omitempty"`
}

// copilotToolRequest is one tool invocation inside assistant.message.
type copilotToolRequest struct {
	ToolCallID       string          `json:"toolCallId"`
	Name             string          `json:"name"`
	Arguments        json.RawMessage `json:"arguments"`
	Type             string          `json:"type"`
	IntentionSummary string          `json:"intentionSummary,omitempty"`
}

// copilotMessageDelta is data payload for "assistant.message_delta".
type copilotMessageDelta struct {
	MessageID    string `json:"messageId"`
	DeltaContent string `json:"deltaContent"`
}

// copilotToolExecComplete is data payload for "tool.execution_complete".
type copilotToolExecComplete struct {
	ToolCallID    string             `json:"toolCallId"`
	Model         string             `json:"model"`
	InteractionID string             `json:"interactionId"`
	Success       bool               `json:"success"`
	Result        *copilotToolResult `json:"result,omitempty"`
	Error         *copilotToolError  `json:"error,omitempty"`
}

type copilotToolResult struct {
	Content         string `json:"content"`
	DetailedContent string `json:"detailedContent,omitempty"`
}

type copilotToolError struct {
	Message string `json:"message"`
}

// copilotReasoning is data payload for "assistant.reasoning" / "assistant.reasoning_delta".
type copilotReasoning struct {
	Content      string `json:"content,omitempty"`
	DeltaContent string `json:"deltaContent,omitempty"`
}

// copilotSessionError is data payload for "session.error".
type copilotSessionError struct {
	ErrorType string `json:"errorType"`
	Message   string `json:"message"`
}

// copilotSessionWarning is data payload for "session.warning".
type copilotSessionWarning struct {
	WarningType string `json:"warningType"`
	Message     string `json:"message"`
}

// copilotResultUsage is the usage on the final "result" line.
type copilotResultUsage struct {
	PremiumRequests    float64             `json:"premiumRequests"`
	TotalAPIDurationMs int64               `json:"totalApiDurationMs"`
	SessionDurationMs  int64               `json:"sessionDurationMs"`
	CodeChanges        *copilotCodeChanges `json:"codeChanges,omitempty"`
}

type copilotCodeChanges struct {
	LinesAdded    int      `json:"linesAdded"`
	LinesRemoved  int      `json:"linesRemoved"`
	FilesModified []string `json:"filesModified"`
}

// ── Arg builder ──

// copilotBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var copilotBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedWithValue,
	"--output-format":   blockedWithValue,
	"--allow-all":       blockedStandalone, // tools + paths + URLs
	"--allow-all-tools": blockedStandalone,
	"--allow-all-paths": blockedStandalone,
	"--allow-all-urls":  blockedStandalone,
	"--yolo":            blockedStandalone,
	"--no-ask-user":     blockedStandalone,
	"--resume":          blockedWithValue,  // managed via session/resume
	"--acp":             blockedStandalone, // daemon owns the ACP transport
}

func buildCopilotArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"--acp"}
	args = append(args, filterCustomArgs(opts.CustomArgs, copilotBlockedArgs, logger)...)
	return args
}

func buildCopilotSessionParams(cwd string, opts ExecOptions) map[string]any {
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	if opts.Model != "" {
		params["model"] = opts.Model
	}
	if opts.OnApproval == nil {
		params["config"] = []map[string]any{
			{"name": "allow_all", "value": "on"},
		}
	}
	return params
}
