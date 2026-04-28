package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// codexBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var codexBlockedArgs = map[string]blockedArgMode{
	"--listen": blockedWithValue, // stdio:// transport for daemon communication
}

// codexStderrTailBytes bounds the stderr tail captured for inclusion in
// error messages when codex exits before the JSON-RPC handshake (e.g. the
// user supplied a custom_args flag that the `app-server` subcommand
// rejects). Kept as its own constant so bumping codex independently of
// other agents stays easy if codex starts shipping longer failure traces.
const (
	codexStderrTailBytes                  = 2048
	defaultCodexSemanticInactivityTimeout = 10 * time.Minute
)

// codexBackend implements Backend by spawning `codex app-server --listen stdio://`
// and communicating via JSON-RPC 2.0 over stdin/stdout.
type codexBackend struct {
	cfg Config
}

func (b *codexBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codex"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codex executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	semanticInactivityTimeout := opts.SemanticInactivityTimeout
	if semanticInactivityTimeout == 0 {
		semanticInactivityTimeout = defaultCodexSemanticInactivityTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	codexArgs := append([]string{"app-server", "--listen", "stdio://"}, filterCustomArgs(opts.CustomArgs, codexBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, codexArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", codexArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[codex:stderr] "), codexStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
	}

	b.cfg.Logger.Info("codex started app-server", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	semanticActivityCh := make(chan string, 256)

	var outputMu sync.Mutex
	var output strings.Builder

	// turnDone is set before starting the reader goroutine so there is no
	// race between the lifecycle goroutine writing and the reader reading.
	turnDone := make(chan bool, 1) // true = aborted

	c := &codexClient{
		cfg:                  b.cfg,
		stdin:                stdin,
		pending:              make(map[int]*pendingRPC),
		notificationProtocol: "unknown",
		onMessage: func(msg Message) {
			logCodexAgentMessage(b.cfg.Logger, msg)
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
			trySendString(semanticActivityCh, describeCodexSemanticActivity(msg))
		},
		onSemanticActivity: func(description string) {
			b.cfg.Logger.Debug("codex semantic activity observed", "activity", description)
			trySendString(semanticActivityCh, description)
		},
		onTurnDone: func(aborted bool) {
			select {
			case turnDone <- aborted:
			default:
			}
		},
	}

	// Start reading stdout in background
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
		c.closeAllPending(fmt.Errorf("codex process exited"))
	}()

	// drainAndWait closes stdin so codex shuts down, then joins cmd.Wait().
	// cmd.Wait() is the only Go-stdlib-documented synchronization point for
	// os/exec's internal stderr/stdout copy goroutines — until it returns,
	// stderrBuf may not have observed every byte codex wrote before it
	// exited, and stderrBuf.Tail() can come back empty or truncated. Any
	// code that reads stderrBuf.Tail() must call drainAndWait() first.
	// sync.Once makes it safe to call from both error paths and the deferred
	// cleanup.
	var waitOnce sync.Once
	drainAndWait := func() {
		waitOnce.Do(func() {
			stdin.Close()
			_ = cmd.Wait()
		})
	}

	// Drive the session lifecycle in a goroutine.
	// Shutdown sequence: lifecycle goroutine closes stdin + cancels context →
	// codex process exits → reader goroutine's scanner.Scan() returns false →
	// readerDone closes → lifecycle goroutine collects final output and sends Result.
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer drainAndWait()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string

		// 1. Initialize handshake
		_, err := c.request(runCtx, "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"title":   "Multica Agent SDK",
				"version": "0.2.0",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		})
		if err != nil {
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = withAgentStderr(fmt.Sprintf("codex initialize failed: %v", err), "codex", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.notify("initialized")

		// 2. Start a new thread, or resume the prior one for this issue. When
		// resume fails (thread GCed on the server, schema drift, etc.) we fall
		// back to a fresh thread so the task still makes progress.
		threadID, resumed, err := c.startOrResumeThread(runCtx, opts, b.cfg.Logger)
		if err != nil {
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = withAgentStderr(err.Error(), "codex", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.threadID = threadID
		if resumed {
			b.cfg.Logger.Info("codex thread resumed", "thread_id", threadID)
		} else {
			b.cfg.Logger.Info("codex thread started", "thread_id", threadID)
		}

		// 3. Send turn and wait for completion
		_, err = c.request(runCtx, "turn/start", map[string]any{
			"threadId": threadID,
			"input": []map[string]any{
				{"type": "text", "text": prompt},
			},
		})
		if err != nil {
			drainAndWait() // flush os/exec stderr goroutine before sampling Tail
			finalStatus = "failed"
			finalError = withAgentStderr(fmt.Sprintf("codex turn/start failed: %v", err), "codex", stderrBuf.Tail())
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		lastSemanticActivity := time.Now()
		lastSemanticActivityDescription := "turn/start"
		semanticTimer := time.NewTimer(semanticInactivityTimeout)
		defer semanticTimer.Stop()

		waitingForTurn := true
		for waitingForTurn {
			select {
			case aborted := <-turnDone:
				waitingForTurn = false
				switch {
				case aborted:
					finalStatus = "aborted"
					finalError = "turn was aborted"
				default:
					if errMsg := c.getTurnError(); errMsg != "" {
						finalStatus = "failed"
						finalError = errMsg
					}
				}
			case activity := <-semanticActivityCh:
				lastSemanticActivity = time.Now()
				lastSemanticActivityDescription = activity
				resetTimer(semanticTimer, semanticInactivityTimeout)
			case <-semanticTimer.C:
				waitingForTurn = false
				finalStatus = "timeout"
				finalError = fmt.Sprintf("codex semantic inactivity timeout after %s without agent progress (last activity: %s)", semanticInactivityTimeout, lastSemanticActivityDescription)
				b.cfg.Logger.Warn("codex semantic inactivity timeout",
					"pid", cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", c.turnID,
					"timeout", semanticInactivityTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
					"idle_for", time.Since(lastSemanticActivity).Round(time.Millisecond).String(),
				)
			case <-runCtx.Done():
				waitingForTurn = false
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("codex timed out after %s", timeout)
				} else {
					finalStatus = "aborted"
					finalError = "execution cancelled"
				}
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("codex finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Close stdin and cancel context to signal the app-server to exit.
		// Without this, the long-running codex process keeps stdout open and
		// the reader goroutine blocks forever on scanner.Scan().
		stdin.Close()
		cancel()

		// Wait for the reader goroutine to finish so all output is accumulated.
		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Build usage map from accumulated codex usage.
		// First check JSON-RPC notifications (often empty for Codex).
		var usageMap map[string]TokenUsage
		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		// Fallback: if no usage from JSON-RPC, scan Codex session JSONL logs.
		// Codex writes token_count events to ~/.codex/sessions/YYYY/MM/DD/*.jsonl.
		if u.InputTokens == 0 && u.OutputTokens == 0 {
			if scanned := scanCodexSessionUsage(startTime); scanned != nil {
				u = scanned.usage
				if scanned.model != "" && opts.Model == "" {
					opts.Model = scanned.model
				}
			}
		}

		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
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
			SessionID:  threadID,
			DurationMs: duration.Milliseconds(),
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// startOrResumeThread picks between Codex's thread/resume and thread/start
// based on opts.ResumeSessionID. When a prior thread ID is provided it first
// tries thread/resume; any error (unknown thread, schema mismatch, transport
// failure) is logged and the method falls back to thread/start so the task
// still executes. The returned threadID is what subsequent turn/start calls
// must reference, and resumed indicates whether the prior thread was picked
// up (only useful for logging).
func (c *codexClient) startOrResumeThread(ctx context.Context, opts ExecOptions, logger *slog.Logger) (string, bool, error) {
	if priorThreadID := opts.ResumeSessionID; priorThreadID != "" {
		// thread/resume reuses the thread's persisted model and reasoning
		// effort; only override fields the daemon actually cares about.
		resumeResult, err := c.request(ctx, "thread/resume", map[string]any{
			"threadId":              priorThreadID,
			"cwd":                   opts.Cwd,
			"model":                 nilIfEmpty(opts.Model),
			"developerInstructions": nilIfEmpty(opts.SystemPrompt),
		})
		if err == nil {
			if threadID := extractThreadID(resumeResult); threadID != "" {
				return threadID, true, nil
			}
			logger.Warn("codex thread/resume returned no thread ID; falling back to thread/start", "prior_thread_id", priorThreadID)
		} else {
			logger.Warn("codex thread/resume failed; falling back to thread/start", "prior_thread_id", priorThreadID, "error", err)
		}
	}

	startResult, err := c.request(ctx, "thread/start", map[string]any{
		"model":                  nilIfEmpty(opts.Model),
		"modelProvider":          nil,
		"profile":                nil,
		"cwd":                    opts.Cwd,
		"approvalPolicy":         nil,
		"sandbox":                nil,
		"config":                 nil,
		"baseInstructions":       nil,
		"developerInstructions":  nilIfEmpty(opts.SystemPrompt),
		"compactPrompt":          nil,
		"includeApplyPatchTool":  nil,
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	})
	if err != nil {
		return "", false, fmt.Errorf("codex thread/start failed: %w", err)
	}
	threadID := extractThreadID(startResult)
	if threadID == "" {
		return "", false, fmt.Errorf("codex thread/start returned no thread ID")
	}
	return threadID, false, nil
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func trySendString(ch chan<- string, value string) {
	select {
	case ch <- value:
	default:
	}
}

func logCodexAgentMessage(logger *slog.Logger, msg Message) {
	if logger == nil {
		return
	}
	attrs := []any{
		"type", string(msg.Type),
		"tool", msg.Tool,
		"call_id", msg.CallID,
		"status", msg.Status,
		"content_len", len(msg.Content),
		"output_len", len(msg.Output),
	}
	logger.Info("codex agent message received", attrs...)
	if msg.Type == MessageToolResult {
		logger.Info("codex tool_result observed", "tool", msg.Tool, "call_id", msg.CallID, "output_len", len(msg.Output))
	}
}

func describeCodexSemanticActivity(msg Message) string {
	switch msg.Type {
	case MessageToolUse, MessageToolResult:
		if msg.Tool != "" {
			return fmt.Sprintf("%s:%s", msg.Type, msg.Tool)
		}
	case MessageStatus:
		if msg.Status != "" {
			return fmt.Sprintf("%s:%s", msg.Type, msg.Status)
		}
	}
	return string(msg.Type)
}

// ── codexClient: JSON-RPC 2.0 transport ──

type codexClient struct {
	cfg                Config
	stdin              interface{ Write([]byte) (int, error) }
	mu                 sync.Mutex
	nextID             int
	pending            map[int]*pendingRPC
	threadID           string
	turnID             string
	onMessage          func(Message)
	onSemanticActivity func(description string)
	onTurnDone         func(aborted bool)

	notificationProtocol string // "unknown", "legacy", "raw"
	turnStarted          bool
	completedTurnIDs     map[string]bool

	usageMu sync.Mutex
	usage   TokenUsage // accumulated from turn events

	turnErrorMu sync.Mutex
	turnError   string // captured from turn/completed status=failed or terminal error notifications
}

func (c *codexClient) setTurnError(msg string) {
	if msg == "" {
		return
	}
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	if c.turnError == "" {
		c.turnError = msg
	}
}

func (c *codexClient) getTurnError() string {
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	return c.turnError
}

type pendingRPC struct {
	ch     chan rpcResult
	method string
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

func (c *codexClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
	if method == "turn/start" {
		threadID := ""
		if paramMap, ok := params.(map[string]any); ok {
			threadID, _ = paramMap["threadId"].(string)
		}
		c.cfg.Logger.Info("codex turn/start sent", "request_id", id, "thread_id", threadID)
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

func (c *codexClient) notify(method string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *codexClient) respond(id int, result any) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *codexClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *codexClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// Check if it's a response to our request
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		// Server request (has id + method)
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleServerRequest(raw)
			return
		}
	}

	// Notification (no id, has method)
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *codexClient) handleResponse(raw map[string]json.RawMessage) {
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

func (c *codexClient) handleServerRequest(raw map[string]json.RawMessage) {
	var id int
	_ = json.Unmarshal(raw["id"], &id)

	var method string
	_ = json.Unmarshal(raw["method"], &method)

	// Auto-approve all exec/patch requests in daemon mode
	switch method {
	case "item/commandExecution/requestApproval", "execCommandApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/fileChange/requestApproval", "applyPatchApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	default:
		c.respond(id, map[string]any{})
	}
}

func (c *codexClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}

	// Legacy codex/event notifications
	if method == "codex/event" || strings.HasPrefix(method, "codex/event/") {
		c.notificationProtocol = "legacy"
		msgData, ok := params["msg"]
		if !ok {
			return
		}
		msgMap, ok := msgData.(map[string]any)
		if !ok {
			return
		}
		c.handleEvent(msgMap)
		return
	}

	// Raw v2 notifications
	if c.notificationProtocol != "legacy" {
		if c.notificationProtocol == "unknown" &&
			(method == "turn/started" || method == "turn/completed" ||
				method == "thread/started" || strings.HasPrefix(method, "item/")) {
			c.notificationProtocol = "raw"
		}

		if c.notificationProtocol == "raw" {
			c.handleRawNotification(method, params)
		}
	}
}

func (c *codexClient) handleEvent(msg map[string]any) {
	msgType, _ := msg["type"].(string)

	switch msgType {
	case "task_started":
		c.turnStarted = true
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageStatus, Status: "running", SessionID: c.threadID})
		}
	case "agent_message":
		text, _ := msg["message"].(string)
		if text != "" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: text})
		}
	case "exec_command_begin":
		callID, _ := msg["call_id"].(string)
		command, _ := msg["command"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "exec_command",
				CallID: callID,
				Input:  map[string]any{"command": command},
			})
		}
	case "exec_command_end":
		callID, _ := msg["call_id"].(string)
		output, _ := msg["output"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "exec_command",
				CallID: callID,
				Output: output,
			})
		}
	case "patch_apply_begin":
		callID, _ := msg["call_id"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "patch_apply",
				CallID: callID,
			})
		}
	case "patch_apply_end":
		callID, _ := msg["call_id"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "patch_apply",
				CallID: callID,
			})
		}
	case "task_complete":
		// Extract usage from legacy task_complete if present.
		c.extractUsageFromMap(msg)
		if c.onTurnDone != nil {
			c.onTurnDone(false)
		}
	case "turn_aborted":
		if c.onTurnDone != nil {
			c.onTurnDone(true)
		}
	}
}

func (c *codexClient) handleRawNotification(method string, params map[string]any) {
	// Ignore notifications from threads other than the one we are tracking.
	// Codex multiplexes subagent threads (e.g. memory consolidation) on the
	// same stdio pipe; only our thread should drive turn lifecycle and output.
	//
	// The v2 app-server-protocol schema guarantees a top-level threadId on
	// every notification, so this dispatch-level guard transparently covers
	// every handler below. If a future codex revision introduces notifications
	// without threadId, they fall through (ok=false) — re-audit this guard
	// when bumping codex.
	if threadID, ok := params["threadId"].(string); ok && c.threadID != "" && threadID != c.threadID {
		return
	}

	switch method {
	case "turn/started":
		c.turnStarted = true
		if turnID := extractNestedString(params, "turn", "id"); turnID != "" {
			c.turnID = turnID
		}
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageStatus, Status: "running", SessionID: c.threadID})
		}

	case "turn/completed":
		turnID := extractNestedString(params, "turn", "id")
		status := extractNestedString(params, "turn", "status")
		threadID, _ := params["threadId"].(string)
		c.cfg.Logger.Info("codex turn/completed received", "thread_id", threadID, "turn_id", turnID, "status", status)
		aborted := status == "cancelled" || status == "canceled" ||
			status == "aborted" || status == "interrupted"

		// Capture the error message from failed turns so callers can surface
		// a real reason instead of falling back to "empty output".
		if status == "failed" {
			errMsg := extractNestedString(params, "turn", "error", "message")
			if errMsg == "" {
				errMsg = "codex turn failed"
			}
			c.setTurnError(errMsg)
		}

		if c.completedTurnIDs == nil {
			c.completedTurnIDs = map[string]bool{}
		}
		if turnID != "" {
			if c.completedTurnIDs[turnID] {
				return
			}
			c.completedTurnIDs[turnID] = true
		}

		// Extract usage from turn/completed if present (e.g. params.turn.usage).
		if turn, ok := params["turn"].(map[string]any); ok {
			c.extractUsageFromMap(turn)
		}

		if c.onTurnDone != nil {
			c.onTurnDone(aborted)
		}

	case "error":
		// Top-level protocol error. Retrying notifications (willRetry=true) are
		// transient reconnect attempts; only capture terminal errors so we
		// don't stomp on a real failure later with a retry placeholder.
		willRetry, _ := params["willRetry"].(bool)
		errMsg := extractNestedString(params, "error", "message")
		if errMsg == "" {
			errMsg = extractNestedString(params, "message")
		}
		if errMsg != "" {
			c.cfg.Logger.Warn("codex error notification", "message", errMsg, "will_retry", willRetry)
			if !willRetry {
				c.setTurnError(errMsg)
			}
		}

	case "thread/status/changed":
		statusType := extractNestedString(params, "status", "type")
		threadID, _ := params["threadId"].(string)
		c.cfg.Logger.Info("codex thread/status changed", "thread_id", threadID, "status", statusType, "turn_started", c.turnStarted)

	default:
		if strings.HasPrefix(method, "item/") {
			c.handleItemNotification(method, params)
		}
	}
}

func (c *codexClient) handleItemNotification(method string, params map[string]any) {
	item, _ := params["item"].(map[string]any)
	itemType, _ := item["type"].(string)
	itemID, _ := item["id"].(string)
	if isCodexItemProgressActivity(method) && c.onSemanticActivity != nil {
		c.onSemanticActivity(describeCodexItemProgressActivity(method, itemType, itemID))
	}
	if item == nil {
		return
	}

	switch {
	case method == "item/started" && itemType == "commandExecution":
		command, _ := item["command"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "exec_command",
				CallID: itemID,
				Input:  map[string]any{"command": command},
			})
		}

	case method == "item/completed" && itemType == "commandExecution":
		output, _ := item["aggregatedOutput"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "exec_command",
				CallID: itemID,
				Output: output,
			})
		}

	case method == "item/started" && itemType == "fileChange":
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "patch_apply",
				CallID: itemID,
			})
		}

	case method == "item/completed" && itemType == "fileChange":
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "patch_apply",
				CallID: itemID,
			})
		}

	case method == "item/completed" && itemType == "agentMessage":
		text, _ := item["text"].(string)
		if text != "" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: text})
		}
		phase, _ := item["phase"].(string)
		if phase == "final_answer" && c.turnStarted {
			if c.onTurnDone != nil {
				c.onTurnDone(false)
			}
		}
	}
}

func isCodexItemProgressActivity(method string) bool {
	switch method {
	case "item/agentMessage/delta",
		"item/commandExecution/outputDelta",
		"item/fileChange/outputDelta",
		"item/mcpToolCall/progress":
		return true
	default:
		return false
	}
}

func describeCodexItemProgressActivity(method, itemType, itemID string) string {
	if itemType == "" {
		itemType = "unknown"
	}
	if itemID == "" {
		return fmt.Sprintf("%s:%s", method, itemType)
	}
	return fmt.Sprintf("%s:%s:%s", method, itemType, itemID)
}

// extractUsageFromMap extracts token usage from a map that may contain
// "usage", "token_usage", or "tokens" fields. Handles various Codex formats.
func (c *codexClient) extractUsageFromMap(data map[string]any) {
	// Try common field names for usage data.
	var usageMap map[string]any
	for _, key := range []string{"usage", "token_usage", "tokens"} {
		if v, ok := data[key].(map[string]any); ok {
			usageMap = v
			break
		}
	}
	if usageMap == nil {
		return
	}

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	// Try various key conventions.
	c.usage.InputTokens += codexInt64(usageMap, "input_tokens", "input", "prompt_tokens")
	c.usage.OutputTokens += codexInt64(usageMap, "output_tokens", "output", "completion_tokens")
	c.usage.CacheReadTokens += codexInt64(usageMap, "cache_read_tokens", "cache_read_input_tokens")
	c.usage.CacheWriteTokens += codexInt64(usageMap, "cache_write_tokens", "cache_creation_input_tokens")
}

// codexInt64 returns the first non-zero int64 value from the map for the given keys.
func codexInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			if v != 0 {
				return int64(v)
			}
		case int64:
			if v != 0 {
				return v
			}
		}
	}
	return 0
}

// ── Codex session log scanner ──

// codexSessionUsage holds usage extracted from a Codex session JSONL file.
type codexSessionUsage struct {
	usage TokenUsage
	model string
}

// scanCodexSessionUsage scans Codex session JSONL files written after startTime
// to extract token usage. Codex writes token_count events to
// ~/.codex/sessions/YYYY/MM/DD/*.jsonl.
func scanCodexSessionUsage(startTime time.Time) *codexSessionUsage {
	root := codexSessionRoot()
	if root == "" {
		return nil
	}

	// Look in today's session directory.
	dateDir := filepath.Join(root,
		fmt.Sprintf("%04d", startTime.Year()),
		fmt.Sprintf("%02d", int(startTime.Month())),
		fmt.Sprintf("%02d", startTime.Day()),
	)

	files, err := filepath.Glob(filepath.Join(dateDir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return nil
	}

	// Only scan files modified after startTime (this task's session).
	var result codexSessionUsage
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.ModTime().Before(startTime) {
			continue
		}
		if u := parseCodexSessionFile(f); u != nil {
			// Take the last matching file's data (usually there's only one per task).
			result = *u
		}
	}

	if result.usage.InputTokens == 0 && result.usage.OutputTokens == 0 {
		return nil
	}
	return &result
}

// codexSessionRoot returns the Codex sessions directory.
func codexSessionRoot() string {
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		dir := filepath.Join(codexHome, "sessions")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	dir := filepath.Join(home, ".codex", "sessions")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// codexSessionTokenCount represents a token_count event in Codex JSONL.
type codexSessionTokenCount struct {
	Type    string `json:"type"`
	Payload *struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage *struct {
				InputTokens           int64 `json:"input_tokens"`
				OutputTokens          int64 `json:"output_tokens"`
				CachedInputTokens     int64 `json:"cached_input_tokens"`
				CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
				ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
			} `json:"total_token_usage"`
			LastTokenUsage *struct {
				InputTokens           int64 `json:"input_tokens"`
				OutputTokens          int64 `json:"output_tokens"`
				CachedInputTokens     int64 `json:"cached_input_tokens"`
				CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
				ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
			} `json:"last_token_usage"`
			Model string `json:"model"`
		} `json:"info"`
		Model string `json:"model"`
	} `json:"payload"`
}

// parseCodexSessionFile extracts the final token_count from a Codex session file.
func parseCodexSessionFile(path string) *codexSessionUsage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var result codexSessionUsage
	found := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Fast pre-filter.
		if !bytesContainsStr(line, "token_count") && !bytesContainsStr(line, "turn_context") {
			continue
		}

		var evt codexSessionTokenCount
		if err := json.Unmarshal(line, &evt); err != nil || evt.Payload == nil {
			continue
		}

		// Track model from turn_context events.
		if evt.Type == "turn_context" && evt.Payload.Model != "" {
			result.model = evt.Payload.Model
			continue
		}

		// Extract token usage from token_count events.
		if evt.Payload.Type == "token_count" && evt.Payload.Info != nil {
			usage := evt.Payload.Info.TotalTokenUsage
			if usage == nil {
				usage = evt.Payload.Info.LastTokenUsage
			}
			if usage != nil {
				cachedTokens := usage.CachedInputTokens
				if cachedTokens == 0 {
					cachedTokens = usage.CacheReadInputTokens
				}
				result.usage = TokenUsage{
					InputTokens:     usage.InputTokens,
					OutputTokens:    usage.OutputTokens + usage.ReasoningOutputTokens,
					CacheReadTokens: cachedTokens,
				}
				if evt.Payload.Info.Model != "" {
					result.model = evt.Payload.Info.Model
				}
				found = true
			}
		}
	}

	if !found {
		return nil
	}
	return &result
}

// bytesContainsStr checks if b contains the string s (without allocating).
func bytesContainsStr(b []byte, s string) bool {
	return strings.Contains(string(b), s)
}

// ── Helpers ──

func extractThreadID(result json.RawMessage) string {
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.Thread.ID
}

func extractNestedString(m map[string]any, keys ...string) string {
	current := any(m)
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[key]
	}
	s, _ := current.(string)
	return s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
