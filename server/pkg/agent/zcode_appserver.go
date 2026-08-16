package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	zcodeStderrTailBytes                  = 2048
	defaultZcodeHandshakeTimeout          = 30 * time.Second
	defaultZcodeSemanticInactivityTimeout = 10 * time.Minute
	defaultZcodeIdleTimeout               = 30 * time.Minute
	defaultZcodeTurnPollInterval          = 5 * time.Second
	zcodeEventChannelSize                 = 256
	zcodeStopTimeout                      = 5 * time.Second
)

var (
	// zcodeIdleTimeoutNanos overrides how long an unused app-server process is
	// kept alive before the reaper closes it. Set via atomic store in tests.
	zcodeIdleTimeoutNanos atomic.Int64
	// zcodeTurnPollIntervalNanos overrides how often the consume loop polls
	// session/read for the terminal state. Set via atomic store in tests.
	zcodeTurnPollIntervalNanos atomic.Int64
	zcodeReaperStarted         atomic.Bool
)

func zcodeIdleTimeout() time.Duration {
	if n := zcodeIdleTimeoutNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return defaultZcodeIdleTimeout
}

func zcodeTurnPollInterval() time.Duration {
	if n := zcodeTurnPollIntervalNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return defaultZcodeTurnPollInterval
}

var errZcodeProcessExited = errors.New("zcode app-server process exited")

// zcodeModelUnavailableCode is ZCode's restore-warning rejection: session/send
// fails with it when a resumed session's stored model is not in the runtime's
// current model catalog. The session is unusable for further turns.
const zcodeModelUnavailableCode = -32031

// zcodeRPCError is a JSON-RPC error returned by the app-server. It carries the
// method that failed so callers can tell a transport failure apart from a
// protocol-level rejection.
type zcodeRPCError struct {
	Method  string
	Code    int
	Message string
}

func (e *zcodeRPCError) Error() string {
	return fmt.Sprintf("zcode %s: %s (code=%d)", e.Method, e.Message, e.Code)
}

// zcodeBackend implements Backend by driving the ZCode Desktop runtime's
// native `zcode app-server` JSON-RPC session protocol.
//
// ZCode app-server speaks a JSON-RPC 2.0-style protocol over NDJSON on stdio.
// The daemon sends {"id", "method", "params"} request lines and consumes
// responses, server-initiated requests (which must be answered), and
// notifications. The turn flow is:
//
//	session/create (or session/resume) → session/subscribe → session/send
//	→ consume session/event / state.updated until the turn ends → (next turn)
//
// Unlike Codex, ZCode sessions live only in the app-server process memory and
// cannot be resumed across processes, so this backend keeps ONE long-lived
// app-server process per runtime (keyed by Config.RuntimeID) in a package-level
// registry and reuses it across turns; an idle reaper closes processes the
// daemon stops using.
type zcodeBackend struct {
	cfg Config
}

// ── long-lived app-server process registry ────────────────────────────────
//
// Each zcodeClient owns one `zcode app-server` subprocess and the JSON-RPC
// transport over its stdio. Processes persist across Backend instances so a
// resumed task (a fresh daemon dispatch with PriorSessionID) can reuse the
// session its prior turn created.

var zcodeProcRegistry sync.Map // key -> *zcodeClient

// ensureZcodeReaper starts the single background goroutine that closes idle
// app-server processes. It runs once for the process lifetime.
func ensureZcodeReaper(logger *slog.Logger) {
	if !zcodeReaperStarted.CompareAndSwap(false, true) {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			deadline := time.Now().Add(-zcodeIdleTimeout())
			zcodeProcRegistry.Range(func(k, v any) bool {
				c := v.(*zcodeClient)
				if time.Unix(0, c.lastActivity.Load()).Before(deadline) && !c.hasActiveHandlers() {
					if logger != nil {
						logger.Info("zcode: closing idle app-server process",
							"key", k, "pid", c.pid(), "idle_for", time.Since(time.Unix(0, c.lastActivity.Load())).Round(time.Second))
					}
					c.close()
					zcodeProcRegistry.Delete(k)
				}
				return true
			})
		}
	}()
}

func zcodeProcKey(cfg Config) string {
	if cfg.RuntimeID != "" {
		return "runtime:" + cfg.RuntimeID
	}
	return "exec:" + cfg.ExecutablePath
}

// getZcodeClient returns the live app-server process for cfg, spawning one if
// needed. A dead registry entry (process exited) is replaced.
func getZcodeClient(cfg Config, execName, cwd string) (*zcodeClient, error) {
	ensureZcodeReaper(cfg.Logger)
	key := zcodeProcKey(cfg)
	if v, ok := zcodeProcRegistry.Load(key); ok {
		c := v.(*zcodeClient)
		if !c.dead() {
			return c, nil
		}
		if cfg.Logger != nil {
			cfg.Logger.Warn("zcode: replacing dead app-server process", "key", key, "err", c.processError())
		}
		zcodeProcRegistry.Delete(key)
		c.close()
	}
	c, err := newZcodeClient(cfg, execName, cwd)
	if err != nil {
		return nil, err
	}
	zcodeProcRegistry.Store(key, c)
	return c, nil
}

// ── zcodeClient: JSON-RPC 2.0 transport over `zcode app-server` stdio ─────

type zcodePendingRPC struct {
	ch     chan rpcResult
	method string
}

// zcodeSessionHandler is the per-session sink the stdout reader routes
// session-scoped notifications into. One handler exists per active turn.
type zcodeSessionHandler struct {
	ch chan zcodeSessionEvent
}

// zcodeSessionEvent is a decoded server notification carrying a sessionId.
type zcodeSessionEvent struct {
	method  string
	params  map[string]any
	session string
}

type zcodeClient struct {
	cfg          Config
	execName     string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stop         context.CancelFunc
	mu           sync.Mutex
	closed       bool
	nextID       int
	pending      map[int]*zcodePendingRPC
	processDone  chan struct{}
	processErr   error
	handlers     map[string]*zcodeSessionHandler
	stderrBuf    *stderrTail
	lastActivity atomic.Int64
}

func newZcodeClient(cfg Config, execName, cwd string) (*zcodeClient, error) {
	if _, err := exec.LookPath(execName); err != nil {
		return nil, fmt.Errorf("zcode executable not found at %q: %w", execName, err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, execName, "app-server")
	hideAgentWindow(cmd)
	// Run app-server in its own process group so a stuck/cancelled run can be
	// torn down tree-wide without touching the daemon. Mirrors codex.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 10 * time.Second
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = buildEnv(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(io.Discard, zcodeStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start zcode app-server: %w", err)
	}

	c := &zcodeClient{
		cfg:         cfg,
		execName:    execName,
		cmd:         cmd,
		stdin:       stdin,
		stop:        cancel,
		pending:     make(map[int]*zcodePendingRPC),
		processDone: make(chan struct{}),
		handlers:    make(map[string]*zcodeSessionHandler),
		stderrBuf:   stderrBuf,
	}
	c.lastActivity.Store(time.Now().UnixNano())
	if cfg.Logger != nil {
		cfg.Logger.Info("zcode app-server started", "exec", execName, "pid", cmd.Process.Pid, "cwd", cwd, "runtime_id", cfg.RuntimeID)
	}
	go c.readLoop(stdout)
	return c, nil
}

func (c *zcodeClient) readLoop(stdout io.Reader) {
	scanner := newAgentStreamScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.Contains(line, `"process/resourceSample"`) {
			continue
		}
		c.noteActivity()
		c.handleLine(line)
	}
	if err := scanner.Err(); err != nil {
		c.markProcessExited(fmt.Errorf("%w: %w", errZcodeProcessExited, err))
		return
	}
	c.markProcessExited(errZcodeProcessExited)
}

func (c *zcodeClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}
	idRaw, hasID := raw["id"]
	if hasID {
		if _, hasMethod := raw["method"]; hasMethod {
			// Server-initiated request (string id + method) — must be answered
			// or the app-server stalls waiting for us.
			c.handleServerRequest(idRaw, raw)
			return
		}
		c.handleResponse(raw, idRaw)
		return
	}
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *zcodeClient) handleResponse(raw map[string]json.RawMessage, idRaw json.RawMessage) {
	var id int
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return // not an int id — not one of our requests
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
		pr.ch <- rpcResult{err: &zcodeRPCError{Method: pr.method, Code: rpcErr.Code, Message: rpcErr.Message}}
		return
	}
	pr.ch <- rpcResult{result: raw["result"]}
}

// handleServerRequest answers server-initiated requests. In daemon mode there
// is no human, so runtime-preference and permission requests are auto-granted;
// a user-input request is rejected so the turn fails fast instead of hanging.
func (c *zcodeClient) handleServerRequest(id json.RawMessage, raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)
	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	switch method {
	case "session/requestRuntimePreferences":
		c.respond(id, map[string]any{
			"nativeSearchEnhancementsEnabled":      false,
			"memoryEnabled":                        false,
			"askUserQuestionAutoResolutionEnabled": true,
			"modelContextBudgetStrategy":           "preflight-v1",
		})
	case "interaction/requestProviderRuntimeHeaders":
		c.respond(id, map[string]any{"headersApplied": true})
	case "interaction/requestPermission":
		c.respond(id, map[string]any{"decision": "allow"})
	case "interaction/requestUserInput":
		sid, _ := params["sessionId"].(string)
		if c.cfg.Logger != nil {
			c.cfg.Logger.Warn("zcode: agent asked the user a question; failing the turn (no human in daemon mode)", "session_id", sid)
		}
		c.respondError(id, -32000, "user input unavailable in daemon mode")
	default:
		if c.cfg.Logger != nil {
			c.cfg.Logger.Warn("zcode: unhandled app-server request; responding error", "method", method)
		}
		c.respondError(id, -32601, "unsupported zcode app-server request: "+method)
	}
}

func (c *zcodeClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)
	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	sessionID, _ := params["sessionId"].(string)
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	h := c.handlers[sessionID]
	c.mu.Unlock()
	if h == nil {
		return
	}
	select {
	case h.ch <- zcodeSessionEvent{method: method, params: params, session: sessionID}:
	default:
		// Handler backed up; drop rather than block the reader. The consume
		// loop's session/read poll recovers the terminal state.
	}
}

// request sends a JSON-RPC request and waits for its response. It is safe to
// call concurrently; responses are matched by id in the stdout reader.
func (c *zcodeClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.processErr != nil {
		err := c.processErr
		c.mu.Unlock()
		return nil, err
	}
	c.nextID++
	id := c.nextID
	pr := &zcodePendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method, "params": params}
	data, err := json.Marshal(msg)
	if err != nil {
		c.removePending(id)
		return nil, err
	}
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("write %s: %w", method, err)
	}
	c.noteActivity()

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-c.processDone:
		c.removePending(id)
		if err := c.processErr; err != nil {
			return nil, err
		}
		return nil, errZcodeProcessExited
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	}
}

func (c *zcodeClient) respond(id json.RawMessage, result any) {
	data, _ := json.Marshal(map[string]any{"id": id, "result": result})
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
	c.noteActivity()
}

func (c *zcodeClient) respondError(id json.RawMessage, code int, message string) {
	data, _ := json.Marshal(map[string]any{
		"id":    id,
		"error": map[string]any{"code": code, "message": message},
	})
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
	c.noteActivity()
}

func (c *zcodeClient) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *zcodeClient) markProcessExited(err error) {
	if err == nil {
		err = errZcodeProcessExited
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.processErr == nil {
		c.processErr = err
		close(c.processDone)
	}
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *zcodeClient) attachSession(sessionID string) *zcodeSessionHandler {
	h := &zcodeSessionHandler{ch: make(chan zcodeSessionEvent, zcodeEventChannelSize)}
	c.mu.Lock()
	c.handlers[sessionID] = h
	c.mu.Unlock()
	return h
}

func (c *zcodeClient) detachSession(sessionID string) {
	c.mu.Lock()
	delete(c.handlers, sessionID)
	c.mu.Unlock()
}

func (c *zcodeClient) hasActiveHandlers() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.handlers) > 0
}

// close shuts the app-server process down. The reaper (idle processes) and the
// dead-entry replacement path call it; a running turn must be torn down first
// via session/stop so the process is idle when close runs.
func (c *zcodeClient) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	if c.stop != nil {
		defer c.stop()
	}
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		signalProcessGroup(c.cmd, syscall.SIGTERM)
	}
	select {
	case <-c.processDone:
	case <-time.After(zcodeStopTimeout):
		if c.cmd.Process != nil {
			signalProcessGroup(c.cmd, syscall.SIGKILL)
		}
	}
}

func (c *zcodeClient) dead() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.processErr != nil
}

func (c *zcodeClient) processError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.processErr
}

func (c *zcodeClient) stderrTail() string {
	return sanitizeAgentDiagnostic(c.stderrBuf.Tail())
}

func (c *zcodeClient) noteActivity() {
	c.lastActivity.Store(time.Now().UnixNano())
}

func (c *zcodeClient) pid() int {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Pid
	}
	return 0
}

// ── Backend.Execute ───────────────────────────────────────────────────────

func (b *zcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("zcode prompt must not be empty")
	}
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "zcode"
	}
	if _, err := exec.LookPath(execName); err != nil {
		return nil, fmt.Errorf("zcode executable not found at %q: %w", execName, err)
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer close(msgCh)
		defer close(resCh)
		b.runTurn(ctx, prompt, opts, execName, msgCh, resCh)
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *zcodeBackend) runTurn(ctx context.Context, prompt string, opts ExecOptions, execName string, msgCh chan<- Message, resCh chan<- Result) {
	startTime := time.Now()
	proc, err := getZcodeClient(b.cfg, execName, opts.Cwd)
	if err != nil {
		resCh <- Result{Status: "failed", Error: err.Error()}
		return
	}
	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	defer cancel()

	handshakeTimeout := opts.HandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultZcodeHandshakeTimeout
	}
	semanticInactivityTimeout := opts.SemanticInactivityTimeout
	if semanticInactivityTimeout <= 0 {
		semanticInactivityTimeout = defaultZcodeSemanticInactivityTimeout
	}

	providerID, modelID, err := resolveZcodeModel(runCtx, opts, execName, b.cfg.Logger)
	if err != nil {
		resCh <- Result{Status: "failed", Error: err.Error()}
		return
	}

	sessionID, resumed, err := b.ensureSession(runCtx, proc, opts, providerID, modelID, handshakeTimeout)
	if err != nil {
		resCh <- Result{Status: "failed", Error: err.Error()}
		return
	}

	// The caller's continuity notice is prepended only when a resume was
	// expected but the backend landed on a fresh session (see ExecOptions).
	input := prompt
	if opts.ResumeExpected && !resumed {
		input = opts.ResumeContinuityNotice + prompt
	}

	status, out, errMsg, usage, rpcErr := b.runSessionTurn(runCtx, proc, sessionID, input, providerID, modelID, timeout, semanticInactivityTimeout, msgCh)

	// ZCode rejects session/send with -32031 when the resumed session's stored
	// model is not in its current workspace model catalog (a catalog-state
	// mismatch on the resume path). The session is unusable for further turns,
	// so abandon it and retry the turn once on a fresh session instead of
	// failing the task.
	if isZcodeModelUnavailable(rpcErr) {
		if b.cfg.Logger != nil {
			b.cfg.Logger.Warn("zcode: resumed session's model unavailable (-32031); retrying on a fresh session",
				"session_id", sessionID, "error", rpcErr)
		}
		zcodeCloseSession(proc, sessionID)
		freshID, cerr := b.createFreshSession(runCtx, proc, providerID, modelID, handshakeTimeout, opts.Cwd)
		if cerr == nil {
			freshInput := prompt
			if opts.ResumeExpected {
				freshInput = opts.ResumeContinuityNotice + prompt
			}
			if s2, o2, e2, u2, r2 := b.runSessionTurn(runCtx, proc, freshID, freshInput, providerID, modelID, timeout, semanticInactivityTimeout, msgCh); r2 == nil {
				sessionID, status, out, errMsg, usage = freshID, s2, o2, e2, u2
			}
		}
	}

	if b.cfg.Logger != nil {
		b.cfg.Logger.Info("zcode turn finished", "pid", proc.pid(), "session_id", sessionID, "status", status, "duration", time.Since(startTime).Round(time.Millisecond).String())
	}
	resCh <- Result{
		Status:     status,
		Output:     out,
		Error:      errMsg,
		DurationMs: time.Since(startTime).Milliseconds(),
		SessionID:  sessionID,
		Usage:      usage,
	}
}

// runSessionTurn subscribes to a session, sends one turn, and consumes its
// events. It returns the send RPC error separately so callers can detect a
// ZCode restore-warning rejection (-32031) and fall back to a fresh session.
func (b *zcodeBackend) runSessionTurn(ctx context.Context, proc *zcodeClient, sessionID, input, providerID, modelID string, timeout, semanticInactivityTimeout time.Duration, msgCh chan<- Message) (status, out, errMsg string, usage map[string]TokenUsage, rpcErr *zcodeRPCError) {
	if _, err := proc.request(ctx, "session/subscribe", map[string]any{
		"sessionId": sessionID, "deliveryKind": "desktop-continuous",
	}); err != nil {
		return "failed", "", withAgentStderr(fmt.Sprintf("zcode session/subscribe failed: %v", err), "zcode", proc.stderrTail()), nil, nil
	}
	handler := proc.attachSession(sessionID)
	defer proc.detachSession(sessionID)
	if _, err := proc.request(ctx, "session/send", map[string]any{
		"sessionId": sessionID, "content": input,
	}); err != nil {
		var rpc *zcodeRPCError
		if errors.As(err, &rpc) {
			return "failed", "", withAgentStderr(fmt.Sprintf("zcode session/send failed: %v", err), "zcode", proc.stderrTail()), nil, rpc
		}
		return "failed", "", withAgentStderr(fmt.Sprintf("zcode session/send failed: %v", err), "zcode", proc.stderrTail()), nil, nil
	}
	trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
	state := &zcodeTurnState{
		usage:      map[string]TokenUsage{},
		sessionID:  sessionID,
		providerID: providerID,
		modelID:    modelID,
	}
	status, out, errMsg = b.consumeTurn(ctx, proc, handler, state, timeout, semanticInactivityTimeout, msgCh)
	return status, out, errMsg, state.usage, nil
}

// isZcodeModelUnavailable reports whether err is the ZCode restore-warning
// rejection thrown by session/send when a resumed session's stored model is
// not in the runtime's current model catalog (-32031).
func isZcodeModelUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var rpc *zcodeRPCError
	if !errors.As(err, &rpc) || rpc == nil {
		return false
	}
	return rpc.Code == zcodeModelUnavailableCode
}

// ensureSession creates a session in the shared app-server process, resuming
// the prior one when opts.ResumeSessionID names a session still alive there.
// When resume is requested but the session is gone (process was reaped or the
// daemon restarted), a fresh session is created and resumed=false is returned
// so the caller can prepend the continuity notice.
func (b *zcodeBackend) ensureSession(ctx context.Context, proc *zcodeClient, opts ExecOptions, providerID, modelID string, handshakeTimeout time.Duration) (sessionID string, resumed bool, err error) {
	if opts.ResumeSessionID != "" {
		hctx, hcancel := context.WithTimeout(ctx, handshakeTimeout)
		resp, rerr := proc.request(hctx, "session/resume", map[string]any{"sessionId": opts.ResumeSessionID})
		hcancel()
		if rerr == nil {
			if sid := zcodeResultSessionID(resp); sid == opts.ResumeSessionID {
				return sid, true, nil
			}
			if b.cfg.Logger != nil {
				b.cfg.Logger.Warn("zcode session/resume returned a different session id; starting fresh",
					"requested", opts.ResumeSessionID, "returned", zcodeResultSessionID(resp))
			}
		} else if b.cfg.Logger != nil {
			b.cfg.Logger.Info("zcode session/resume unavailable; starting fresh", "session_id", opts.ResumeSessionID, "error", rerr)
		}
	}

	sid, cerr := b.createFreshSession(ctx, proc, providerID, modelID, handshakeTimeout, opts.Cwd)
	if cerr != nil {
		return "", false, errors.New(withAgentStderr(fmt.Sprintf("zcode session/create failed: %v", cerr), "zcode", proc.stderrTail()))
	}
	return sid, false, nil
}

// createFreshSession creates a new session in the shared app-server process
// with the resolved model and the daemon's workspace.
func (b *zcodeBackend) createFreshSession(ctx context.Context, proc *zcodeClient, providerID, modelID string, handshakeTimeout time.Duration, cwd string) (string, error) {
	hctx, hcancel := context.WithTimeout(ctx, handshakeTimeout)
	defer hcancel()
	resp, cerr := proc.request(hctx, "session/create", map[string]any{
		"workspace": map[string]any{
			"workspaceKey":  zcodeWorkspaceKey(cwd),
			"workspacePath": cwd,
		},
		"model":       map[string]any{"providerId": providerID, "modelId": modelID},
		"mode":        "build",
		"persistence": "immediate",
	})
	if cerr != nil {
		return "", cerr
	}
	sid := zcodeResultSessionID(resp)
	if sid == "" {
		return "", fmt.Errorf("zcode session/create returned no session id")
	}
	return sid, nil
}

// zcodeCloseSession closes a session in the shared app-server process, freeing
// its resources. Best-effort: a session rejected by -32031 is unusable and only
// needs to be released, not driven to a clean stop.
func zcodeCloseSession(proc *zcodeClient, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), zcodeStopTimeout)
	defer cancel()
	_, _ = proc.request(ctx, "session/close", map[string]any{"sessionId": sessionID})
}

// zcodeTurnState accumulates the live turn.
type zcodeTurnState struct {
	sessionID     string
	providerID    string
	modelID       string
	turnStarted   bool
	lastText      string
	finalResponse string
	usage         map[string]TokenUsage
}

func (b *zcodeBackend) consumeTurn(ctx context.Context, proc *zcodeClient, handler *zcodeSessionHandler, state *zcodeTurnState, timeout, semanticInactivityTimeout time.Duration, msgCh chan<- Message) (status, out, errMsg string) {
	semanticTimer := time.NewTimer(semanticInactivityTimeout)
	defer semanticTimer.Stop()
	pollTicker := time.NewTicker(zcodeTurnPollInterval())
	defer pollTicker.Stop()

	terminal := false
	for !terminal {
		select {
		case ev := <-handler.ch:
			proc.noteActivity()
			resetTimer(semanticTimer, semanticInactivityTimeout)
			if done, st, o, e := b.processEvent(ev, msgCh, state); done {
				terminal, status, out, errMsg = true, st, o, e
			}
		case <-pollTicker.C:
			// Notifications can be dropped by the runtime (a turn may complete
			// internally while emitting nothing), so session/read is the
			// source of truth for the terminal state.
			if state.turnStarted && b.turnDoneByPoll(ctx, proc, state) {
				terminal = true
				status = "completed"
				out = b.readSessionResponse(ctx, proc, state)
			}
		case <-semanticTimer.C:
			terminal = true
			status = "timeout"
			errMsg = fmt.Sprintf("%s after %s without agent progress", "zcode semantic inactivity timeout", semanticInactivityTimeout)
			zcodeStopSession(proc, state.sessionID)
		case <-ctx.Done():
			terminal = true
			if ctx.Err() == context.DeadlineExceeded {
				status = "timeout"
				errMsg = fmt.Sprintf("zcode timed out after %s", timeout)
			} else {
				status = "aborted"
				errMsg = "execution cancelled"
			}
			zcodeStopSession(proc, state.sessionID)
		case <-proc.processDone:
			terminal = true
			status = "failed"
			if pe := proc.processError(); pe != nil {
				errMsg = pe.Error()
			} else {
				errMsg = errZcodeProcessExited.Error()
			}
		}
	}

	// A timeout/abort may still have produced a complete response (the turn
	// finished just as the deadline fired, or notifications were dropped).
	// Recover it so a completed turn is not reported as a failure.
	if status == "timeout" || status == "aborted" {
		// readSessionResponse bounds only a nil ctx, so the recovery read must
		// carry its own deadline or it blocks forever on a hung app-server.
		rctx, rcancel := context.WithTimeout(context.Background(), zcodeStopTimeout)
		recovered := b.readSessionResponse(rctx, proc, state)
		rcancel()
		if recovered != "" {
			status = "completed"
			out = recovered
			errMsg = ""
		}
	}
	return status, out, errMsg
}

func (b *zcodeBackend) processEvent(ev zcodeSessionEvent, msgCh chan<- Message, state *zcodeTurnState) (done bool, status, out, errMsg string) {
	switch ev.method {
	case "session/event":
		return b.processSessionEvent(ev.params, msgCh, state)
	case "state.updated":
		return b.processStateUpdated(ev.params, state)
	case "v4/telemetry/event":
		// Lifecycle/usage counts only; used as a liveness signal.
		return b.processTelemetry(ev.params, state)
	default:
		return false, "", "", ""
	}
}

func (b *zcodeBackend) processSessionEvent(params map[string]any, msgCh chan<- Message, state *zcodeTurnState) (bool, string, string, string) {
	etype, _ := params["type"].(string)
	payload, _ := params["payload"].(map[string]any)
	switch etype {
	case "turn.started":
		state.turnStarted = true
		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
	case "model.streaming":
		// Streaming proves the turn is live even when turn.started was
		// dropped, arming the session/read poll fallback.
		state.turnStarted = true
		kind, _ := payload["kind"].(string)
		delta, _ := payload["delta"].(string)
		switch kind {
		case "text_delta":
			state.lastText += delta
			if delta != "" {
				trySend(msgCh, Message{Type: MessageText, Content: delta})
			}
		case "reasoning_delta":
			if delta != "" {
				trySend(msgCh, Message{Type: MessageThinking, Content: delta})
			}
		}
	case "turn.completed":
		state.turnStarted = true
		response, _ := payload["response"].(string)
		state.finalResponse = response
		if u := zcodePayloadUsage(payload); len(u) > 0 {
			state.usage = u
		}
		return true, "completed", response, ""
	case "turn.failed":
		state.turnStarted = true
		msg := zcodeFailureMessage(payload)
		if msg == "" {
			msg = "zcode turn failed"
		}
		return true, "failed", "", msg
	}
	return false, "", "", ""
}

func (b *zcodeBackend) processStateUpdated(params map[string]any, state *zcodeTurnState) (bool, string, string, string) {
	switch reason, _ := params["reason"].(string); reason {
	case "prompt_started":
		state.turnStarted = true
		return false, "", "", ""
	case "prompt_completed":
		return true, "completed", state.finalResponse, ""
	case "prompt_failed":
		return true, "failed", "", "zcode turn failed"
	default:
		return false, "", "", ""
	}
}

func (b *zcodeBackend) processTelemetry(params map[string]any, state *zcodeTurnState) (bool, string, string, string) {
	if kind, _ := params["kind"].(string); kind == "turn.started" {
		state.turnStarted = true
	}
	return false, "", "", ""
}

// turnDoneByPoll reports whether session/read says the turn is finished (the
// projection left the running state after the turn started).
func (b *zcodeBackend) turnDoneByPoll(ctx context.Context, proc *zcodeClient, state *zcodeTurnState) bool {
	resp, err := proc.request(ctx, "session/read", map[string]any{"sessionId": state.sessionID})
	if err != nil {
		return false
	}
	status := zcodeProjectionStatus(resp)
	return status == "idle" || status == "ended"
}

// readSessionResponse returns the final assistant text for the session, from
// the live state first and session/read as the source of truth.
func (b *zcodeBackend) readSessionResponse(ctx context.Context, proc *zcodeClient, state *zcodeTurnState) string {
	if state.finalResponse != "" {
		return state.finalResponse
	}
	if strings.TrimSpace(state.lastText) != "" {
		return state.lastText
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), zcodeStopTimeout)
		defer cancel()
	}
	resp, err := proc.request(ctx, "session/read", map[string]any{"sessionId": state.sessionID})
	if err != nil {
		return ""
	}
	return zcodeFinalResponse(resp)
}

func zcodeStopSession(proc *zcodeClient, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), zcodeStopTimeout)
	defer cancel()
	_, _ = proc.request(ctx, "session/stop", map[string]any{"sessionId": sessionID})
}

// ── protocol response helpers ─────────────────────────────────────────────

// resolveZcodeModel picks the model the app-server turn should run. zcode has
// no CLI model override; the model is fixed by ~/.zcode/cli/config.json
// (model.main), so the backend resolves it from there (via the shared catalog)
// unless the daemon supplied an explicit provider/model.
func resolveZcodeModel(ctx context.Context, opts ExecOptions, execName string, logger *slog.Logger) (providerID, modelID string, err error) {
	if opts.Model != "" {
		if pid, mid, ok := strings.Cut(opts.Model, "/"); ok && pid != "" && mid != "" {
			return pid, mid, nil
		}
	}
	catalog, cerr := ListModels(ctx, "zcode", execName)
	if cerr == nil {
		for _, m := range catalog.Models {
			if m.Default && m.ID != "" {
				if pid, mid, ok := strings.Cut(m.ID, "/"); ok && pid != "" && mid != "" {
					return pid, mid, nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("zcode: cannot resolve a model to use; set model.main in ~/.zcode/cli/config.json (the runtime has no CLI model override)")
}

// zcodeWorkspaceKey derives a stable workspace key for session/create from the
// work directory. It only needs to identify the workspace within the session;
// the path is authoritative.
func zcodeWorkspaceKey(cwd string) string {
	if cwd == "" {
		return "default"
	}
	if base := filepath.Base(cwd); base != "" && base != "/" && base != "." {
		return base
	}
	return "default"
}

func zcodeResultSessionID(resp json.RawMessage) string {
	var v struct {
		Session struct {
			SessionID string `json:"sessionId"`
		} `json:"session"`
	}
	if json.Unmarshal(resp, &v) != nil {
		return ""
	}
	return v.Session.SessionID
}

func zcodeProjectionStatus(resp json.RawMessage) string {
	var v struct {
		Projection struct {
			Status string `json:"status"`
		} `json:"projection"`
	}
	if json.Unmarshal(resp, &v) != nil {
		return ""
	}
	return v.Projection.Status
}

// zcodeFinalResponse extracts the last assistant text message from a
// session/read snapshot.
func zcodeFinalResponse(resp json.RawMessage) string {
	var v struct {
		Messages []struct {
			Info struct {
				Role string `json:"role"`
			} `json:"info"`
			Parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"messages"`
	}
	if json.Unmarshal(resp, &v) != nil {
		return ""
	}
	for i := len(v.Messages) - 1; i >= 0; i-- {
		m := v.Messages[i]
		if m.Info.Role != "assistant" {
			continue
		}
		var sb strings.Builder
		for _, p := range m.Parts {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		if strings.TrimSpace(sb.String()) != "" {
			return sb.String()
		}
	}
	return ""
}

func zcodePayloadUsage(payload map[string]any) map[string]TokenUsage {
	u, ok := payload["usage"].(map[string]any)
	if !ok {
		return nil
	}
	usage := TokenUsage{
		InputTokens:      zcodeUsageNumber(u["inputTokens"]),
		OutputTokens:     zcodeUsageNumber(u["outputTokens"]),
		CacheReadTokens:  zcodeUsageNumber(u["cacheReadTokens"]),
		CacheWriteTokens: zcodeUsageNumber(u["cacheWriteTokens"]),
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0 {
		return nil
	}
	return map[string]TokenUsage{"zcode": usage}
}

func zcodeUsageNumber(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i
		}
	}
	return 0
}

func zcodeFailureMessage(payload map[string]any) string {
	if errObj, ok := payload["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok && msg != "" {
			return msg
		}
	}
	if msg, ok := payload["message"].(string); ok && msg != "" {
		return msg
	}
	return ""
}
