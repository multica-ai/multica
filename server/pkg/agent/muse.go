package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// museBlockedArgs protect the MSP stdio transport selected by the daemon.
// `muse serve` is the protocol host; every other subcommand is an interactive
// or one-shot surface that cannot answer a claimed headless task. Keeping these
// tokens out of custom_args prevents a profile or agent from replacing the
// protocol process with a TUI, a login flow, or a one-shot exec that would
// never speak MSP back.
var museBlockedArgs = map[string]blockedArgMode{
	"serve":   blockedStandalone,
	"exec":    blockedStandalone,
	"resume":  blockedStandalone,
	"login":   blockedStandalone,
	"logout":  blockedStandalone,
	"auth":    blockedStandalone,
	"--yolo":  blockedStandalone, // daemon selects approval mode on the wire
	"--print": blockedStandalone,
	"-h":      blockedStandalone,
	"--help":  blockedStandalone,
}

// muse approval mode. Muse selects approval enforcement on the wire, not from
// argv: `muse serve` has no --yolo, and the CLI documents --yolo as a TUI-only
// convenience. `allowAll` is the wire value that means "run without stopping to
// ask", which is the only posture a headless claimed task can hold. The
// container, the per-task worktree and the dropped capabilities are the
// isolation boundary — the same boundary every other backend relies on when it
// passes its own skip-approvals flag.
const museApprovalMode = "allowAll"

// museLauncherVersion identifies Multica's MSP client in the handshake. Muse
// records clientInfo for diagnostics and telemetry only; it carries no
// authority, so this is a stable identifier rather than the daemon version.
const museLauncherVersion = "0.1.0"

var museReaderDrainGrace = 2 * time.Second
var museHandshakeTimeout = 60 * time.Second

var (
	errMuseProcessExited = errors.New("muse process exited")
	errMuseTurnAborted   = errors.New("muse turn aborted")
)

// museBackend runs Meta Muse Code as an MSP (Muse Session Protocol) host over
// `muse serve`, the way mcodeBackend runs MiniMax Code as an ACP host over
// `mcode acp`.
//
// MSP is JSON-RPC 2.0 over stdio, like ACP, but it is a different method
// vocabulary: a session opens with `session/start` (or `session/resume`) and a
// unit of work is submitted with `turn/start`, which answers immediately and
// completes through `turn/completed`. Assistant text, reasoning and tool calls
// arrive as transcript items (`item/started`, `item/delta`, `item/completed`),
// so the adapter's job is to fold those items into Multica's Message stream and
// to read the terminal turn outcome.
//
// Approval is a session property, not a launch flag. A fresh session is created
// with approvalMode=allowAll; a resumed session already carries the mode it was
// started with, so it is re-asserted with `session/setApprovalMode`. That is
// what makes `muse` behave like the other daemon-managed runtimes: nothing waits
// for a human.
type museBackend struct {
	cfg Config
}

// museSession caches the per-run facts the reader needs while folding items.
type museSession struct {
	mu       sync.Mutex
	kinds    map[string]string // itemId -> item kind
	streamed map[string]bool   // itemId -> at least one text delta arrived
	final    string            // last agentMessage whose phase was finalAnswer
	last     string            // last agentMessage text, finalAnswer or not
}

func newMuseSession() *museSession {
	return &museSession{
		kinds:    make(map[string]string),
		streamed: make(map[string]bool),
	}
}

func (s *museSession) setKind(itemID, kind string) {
	if itemID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kinds[itemID] = kind
}

func (s *museSession) kindOf(itemID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kinds[itemID]
}

func (s *museSession) markStreamed(itemID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamed[itemID] = true
}

func (s *museSession) wasStreamed(itemID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streamed[itemID]
}

func (s *museSession) observeMessage(text, phase string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = text
	if phase == "finalAnswer" {
		s.final = text
	}
}

// output is the user-facing result: Muse's own final-answer marking wins, and a
// run that never emitted one falls back to its last assistant message rather
// than reporting an empty success.
func (s *museSession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.final != "" {
		return s.final
	}
	return s.last
}

type museMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newMuseMessageStream(size int) *museMessageStream {
	return &museMessageStream{ch: make(chan Message, size)}
}

func (s *museMessageStream) send(message Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, message)
}

func (s *museMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// museRPCError carries the MSP error object so callers can branch on the
// machine-readable kind instead of matching prose.
type museRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *museRPCError) Error() string {
	if e.Message != "" {
		return "muse: " + e.Message
	}
	return fmt.Sprintf("muse: json-rpc error %d", e.Code)
}

// museErrorKind reads the optional structured `data.kind` MSP attaches to an
// error. It is the only stable branch key: message text is provider prose.
func museErrorKind(err error) string {
	var rpcErr *museRPCError
	if !errors.As(err, &rpcErr) || len(rpcErr.Data) == 0 {
		return ""
	}
	var payload struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(rpcErr.Data, &payload) != nil {
		return ""
	}
	return payload.Kind
}

type musePendingCall struct {
	ch chan museCallResult
}

type museCallResult struct {
	result json.RawMessage
	err    error
}

// museClient is the MSP side of the stdio pipe: it writes JSON-RPC requests and
// correlates responses, and hands every notification and server-initiated
// request to the caller's callbacks.
type museClient struct {
	stdin   io.WriteCloser
	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]*musePendingCall

	onNotification func(method string, params json.RawMessage)
	onRequest      func(id json.RawMessage, method string, params json.RawMessage)
}

func newMuseClient(stdin io.WriteCloser) *museClient {
	return &museClient{
		stdin:   stdin,
		pending: make(map[string]*musePendingCall),
	}
}

func (c *museClient) writeLine(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

// request sends one MSP command and waits for its response. The id is a UUIDv7
// because MSP command ids and JSON-RPC ids share a time-ordered vocabulary in
// the reference client, and a strictly increasing id keeps a late response from
// a killed run from colliding with a live one.
func (c *museClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := museUUIDv7()
	call := &musePendingCall{ch: make(chan museCallResult, 1)}

	c.mu.Lock()
	c.pending[id] = call
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	if err := c.writeLine(payload); err != nil {
		return nil, err
	}

	select {
	case res := <-call.ch:
		return res.result, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *museClient) notify(method string, params any) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	return c.writeLine(payload)
}

func (c *museClient) respond(id json.RawMessage, result any) error {
	return c.writeLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

// handleLine parses one newline-delimited frame. It never returns an error:
// a frame it cannot parse is dropped, because the reader's only other job is to
// keep the pipe drained and a single malformed line must not fail the run.
func (c *museClient) handleLine(line string) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *museRPCError   `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return
	}

	// Server-initiated request: method and id together.
	if envelope.Method != "" {
		if len(envelope.ID) > 0 && string(envelope.ID) != "null" {
			if c.onRequest != nil {
				c.onRequest(envelope.ID, envelope.Method, envelope.Params)
			}
			return
		}
		if c.onNotification != nil {
			c.onNotification(envelope.Method, envelope.Params)
		}
		return
	}

	id := museDecodeID(envelope.ID)
	if id == "" {
		return
	}
	c.mu.Lock()
	call := c.pending[id]
	c.mu.Unlock()
	if call == nil {
		return
	}
	if envelope.Error != nil {
		select {
		case call.ch <- museCallResult{err: envelope.Error}:
		default:
		}
		return
	}
	select {
	case call.ch <- museCallResult{result: envelope.Result}:
	default:
	}
}

// closeAllPending releases every waiter when the transport ends, so a hung
// request surfaces a transport failure instead of blocking until the watchdog.
func (c *museClient) closeAllPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]*musePendingCall)
	c.mu.Unlock()

	for _, call := range pending {
		select {
		case call.ch <- museCallResult{err: err}:
		default:
		}
	}
}

// museDecodeID normalises a JSON-RPC id to the map key the client used. MSP
// echoes the client's string id; numeric ids are accepted defensively so a
// server that renumbers cannot wedge the run.
func museDecodeID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	if unquoted, err := strconv.Unquote(text); err == nil {
		return unquoted
	}
	return text
}

// museUUIDv7 returns a time-ordered UUIDv7. MSP requires commandId to be a
// UUIDv7: it derives a fresh turn's turnId from the submitting commandId and
// rejects anything else with invalidParams, so a v4 here would fail every run.
func museUUIDv7() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is unrecoverable for identity generation; fall
		// back to the clock so the run still has a monotonic, well-formed id.
		ms := uint64(time.Now().UnixNano())
		for i := 0; i < 8; i++ {
			b[i] = byte(ms >> (8 * i))
		}
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}

func (b *museBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "muse"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("muse executable not found at %q: %w", execPath, err)
	}

	runCtx, cancel := runContext(ctx, opts.Timeout)
	// --disable-sandbox and --trust-workspace mirror what --yolo enables for a
	// terminal run: the daemon owns the worktree and must let the agent use it,
	// and the Multica brief lives in the workdir as AGENTS.md, which Muse only
	// reads for a trusted workspace. Approval stays on the wire
	// (session/start approvalMode) instead of argv. --disable-request-user-input
	// withholds the clarify tool, because a claimed task has no surface to
	// answer a question on.
	args := []string{"serve", "--disable-sandbox", "--trust-workspace", "--disable-request-user-input"}
	args = append(args, filterCustomArgs(opts.ExtraArgs, museBlockedArgs, b.cfg.Logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, museBlockedArgs, b.cfg.Logger)...)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = museReaderDrainGrace
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "serve")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("muse stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("muse stdin pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("muse stderr pipe: %w", err)
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start muse: %w", err)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(newLogWriter(b.cfg.Logger, "[muse:stderr] "), stderr)
	}()

	b.cfg.Logger.Info("muse serve started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newMuseMessageStream(256)
	resCh := make(chan Result, 1)
	state := newMuseSession()

	var (
		mu               sync.Mutex
		turnDone         = make(chan museTurnOutcome, 1)
		sessionID        string
		usage            map[string]TokenUsage
		outputCache      string
		terminalOutcome  museTurnOutcome
		terminalObserved atomic.Bool
	)

	client := newMuseClient(stdin)
	client.onNotification = func(method string, params json.RawMessage) {
		switch method {
		case "item/started":
			museHandleItemStarted(params, state, msgStream)
		case "item/delta":
			museHandleItemDelta(params, state, msgStream)
		case "item/completed":
			museHandleItemCompleted(params, state, msgStream)
		case "session/tokenUsage":
			if tokens, model := museTokenUsage(params); tokens.reported() {
				mu.Lock()
				usage = map[string]TokenUsage{model: tokens}
				mu.Unlock()
			}
		case "turn/completed":
			outcome := museTurnOutcome{terminal: "failed"}
			var payload struct {
				Terminal string `json:"terminal"`
				Reason   string `json:"reason"`
				Error    *struct {
					Kind    string `json:"kind"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(params, &payload) == nil {
				outcome.terminal = payload.Terminal
				outcome.reason = payload.Reason
				if payload.Error != nil {
					outcome.errKind = payload.Error.Kind
					outcome.errMessage = payload.Error.Message
				}
			}
			mu.Lock()
			terminalOutcome = outcome
			mu.Unlock()
			terminalObserved.Store(true)
			select {
			case turnDone <- outcome:
			default:
			}
		case "session/closed":
			select {
			case turnDone <- museTurnOutcome{terminal: "failed", errMessage: "muse closed the session before the turn completed"}:
			default:
			}
		}
	}
	// Approval is already allowAll, so a request here is an unexpected gate and
	// not something to leave hanging: the MSP contract requires every
	// server-initiated request to be answered, and an unanswered one stalls the
	// turn. Answer with the presentation receipt and let the turn decide.
	client.onRequest = func(id json.RawMessage, method string, _ json.RawMessage) {
		b.cfg.Logger.Warn("muse server request", "method", method)
		_ = client.respond(id, map[string]any{})
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				client.handleLine(line)
			}
		}
		client.closeAllPending(errMuseProcessExited)
	}()
	cleanup := sync.OnceFunc(func() {
		_ = stdin.Close()
		cancel()
		drained := make(chan struct{})
		go func() {
			<-readerDone
			<-stderrDone
			close(drained)
		}()
		timer := time.NewTimer(museReaderDrainGrace)
		defer timer.Stop()
		select {
		case <-drained:
		case <-timer.C:
			// A detached descendant can retain either pipe after Muse exits.
			// Close our readers before Wait so EOF cannot hold up the result.
			_ = stdout.Close()
			_ = stderr.Close()
			<-drained
		}
		_ = cmd.Wait()
		releaseProcessGroup(cmd)
	})

	go func() {
		defer msgStream.close()
		defer close(resCh)
		defer cleanup()

		startTime := time.Now()
		fail := func(status, message string) {
			cleanup()
			resCh <- Result{
				Status:     status,
				Error:      message,
				DurationMs: time.Since(startTime).Milliseconds(),
				SessionID:  sessionID,
			}
		}

		handshakeCtx, handshakeCancel := context.WithTimeout(runCtx, museHandshakeTimeout)
		defer handshakeCancel()

		if _, err := client.request(handshakeCtx, "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "multica_agent",
				"version": museLauncherVersion,
			},
			"capabilities": map[string]any{},
		}); err != nil {
			status, message := museRequestFailure(runCtx, opts.Timeout, "initialize", err)
			fail(status, message)
			return
		}
		if err := client.notify("initialized", nil); err != nil {
			fail("failed", "muse initialize handshake failed: "+err.Error())
			return
		}

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, resumeErr := client.request(runCtx, "session/resume", map[string]any{
				"commandId": museUUIDv7(),
				"sessionId": opts.ResumeSessionID,
				"history":   "inline",
			})
			if resumeErr != nil {
				// A resume the host no longer holds is permanent: only a fresh
				// session can continue, so report it as a rejection and let the
				// daemon retry. A transport failure is NOT that — the session
				// may still be healthy and the platform retry should resume it.
				status, message := museRequestFailure(runCtx, opts.Timeout, "session/resume", resumeErr)
				cleanup()
				resCh <- Result{
					Status:         status,
					Error:          message,
					DurationMs:     time.Since(startTime).Milliseconds(),
					ResumeRejected: museSessionGone(resumeErr),
				}
				return
			}
			sessionID = museSessionID(result)
			if sessionID == "" {
				sessionID = opts.ResumeSessionID
			}
			// Resume restores the mode the session was started with, which is
			// not necessarily ours, so re-assert it before the turn.
			if _, err := client.request(runCtx, "session/setApprovalMode", map[string]any{
				"commandId": museUUIDv7(),
				"sessionId": sessionID,
				"mode":      museApprovalMode,
			}); err != nil {
				status, message := museRequestFailure(runCtx, opts.Timeout, "session/setApprovalMode", err)
				fail(status, message)
				return
			}
			if model := strings.TrimSpace(opts.Model); model != "" {
				if _, err := client.request(runCtx, "session/setModel", map[string]any{
					"commandId": museUUIDv7(),
					"sessionId": sessionID,
					"model":     map[string]any{"modelId": model},
				}); err != nil {
					status, message := museRequestFailure(runCtx, opts.Timeout, "session/setModel", err)
					fail(status, message)
					return
				}
			}
		} else {
			params := map[string]any{
				"commandId":     museUUIDv7(),
				"workspaceRoot": cwd,
				"approvalMode":  museApprovalMode,
			}
			if model := strings.TrimSpace(opts.Model); model != "" {
				params["modelId"] = model
			}
			result, startErr := client.request(runCtx, "session/start", params)
			if startErr != nil {
				status, message := museRequestFailure(runCtx, opts.Timeout, "session/start", startErr)
				fail(status, message)
				return
			}
			sessionID = museSessionID(result)
			if sessionID == "" {
				fail("failed", "muse session/start returned no session id")
				return
			}
			// Publish the session id early so a cancelled run still hands the
			// daemon a usable resume pointer.
			if sid := sessionID; sid != "" {
				msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sid})
			}
		}

		if opts.SystemPrompt != "" {
			b.cfg.Logger.Debug("muse ignoring ExecOptions.SystemPrompt; using cwd-scoped AGENTS.md", "cwd", opts.Cwd)
		}

		turnParams := map[string]any{
			"commandId":   museUUIDv7(),
			"sessionId":   sessionID,
			"input":       []map[string]any{{"type": "text", "text": prompt}},
			"displayText": prompt,
		}
		if effort := strings.TrimSpace(opts.ThinkingLevel); effort != "" {
			turnParams["reasoningEffort"] = effort
		}
		if _, turnErr := client.request(runCtx, "turn/start", turnParams); turnErr != nil {
			status, message := museRequestFailure(runCtx, opts.Timeout, "turn/start", turnErr)
			fail(status, message)
			return
		}

		var outcome museTurnOutcome
		select {
		case outcome = <-turnDone:
		case <-readerDone:
			outcome = museTurnOutcome{terminal: "failed", errMessage: errMuseProcessExited.Error()}
		case <-runCtx.Done():
			outcome = museTurnOutcome{terminal: "timeout"}
		}

		duration := time.Since(startTime)
		cleanup()

		mu.Lock()
		collected := usage
		// A terminal event wins over a simultaneous EOF, session/closed, or
		// cancellation, including events already buffered before cleanup.
		if terminalObserved.Load() {
			outcome = terminalOutcome
		}
		mu.Unlock()
		outputCache = state.output()

		status := "completed"
		var message string
		switch outcome.terminal {
		case "completed":
			if outputCache == "" {
				status = "failed"
				message = "muse completed the turn without a final assistant message"
			}
		case "cancelled":
			status = "aborted"
			message = "execution cancelled"
		case "timeout":
			if runCtx.Err() == context.DeadlineExceeded {
				status = "timeout"
				message = fmt.Sprintf("muse timed out after %s", opts.Timeout)
			} else {
				status = "aborted"
				message = "execution cancelled"
			}
		case "failed":
			status = "failed"
			message = outcome.errorText()
		default:
			if outcome.terminal == "" {
				status = "failed"
				message = "muse ended the turn without a terminal state"
			} else {
				status = "failed"
				message = outcome.errorText()
			}
		}

		resCh <- Result{
			Status:     status,
			Output:     outputCache,
			Error:      message,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      collected,
		}
	}()

	return &Session{Messages: msgStream.ch, Result: resCh, TerminalObserved: terminalObserved.Load}, nil
}

type museTurnOutcome struct {
	terminal   string
	reason     string
	errKind    string
	errMessage string
}

func (o museTurnOutcome) errorText() string {
	if o.errMessage != "" {
		if o.errKind != "" {
			return o.errKind + ": " + o.errMessage
		}
		return o.errMessage
	}
	if o.reason != "" {
		return o.reason
	}
	return "muse reported a failed turn"
}

// museSessionGone reports whether a session/resume failure means the session is
// permanently unavailable to this host. MSP answers an unknown or unloadable
// session with kind notFound; anything else (a transport drop, a timeout, an
// internal error) may still recover through the platform's own retry, so it
// must not be reported as a rejection.
func museSessionGone(err error) bool {
	switch museErrorKind(err) {
	case "notFound", "sessionNotFound", "unknownSession", "sessionGone":
		return true
	}
	return false
}

func museRequestFailure(ctx context.Context, timeout time.Duration, operation string, err error) (string, string) {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout", fmt.Sprintf("muse timed out during %s after %s", operation, timeout)
	}
	if ctx.Err() == context.Canceled {
		return "aborted", "execution cancelled"
	}
	if errors.Is(err, errMuseProcessExited) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) {
		return "failed", fmt.Sprintf("muse exited before answering %s", operation)
	}
	return "failed", fmt.Sprintf("muse %s failed: %v", operation, err)
}

func museSessionID(result json.RawMessage) string {
	var payload struct {
		Session struct {
			SessionID string `json:"sessionId"`
		} `json:"session"`
	}
	if json.Unmarshal(result, &payload) != nil {
		return ""
	}
	return payload.Session.SessionID
}

type museItem struct {
	ItemID       string          `json:"itemId"`
	Kind         string          `json:"kind"`
	Status       string          `json:"status"`
	Text         string          `json:"text"`
	Tool         string          `json:"tool"`
	ToolCallID   string          `json:"toolCallId"`
	CallID       string          `json:"callId"`
	Args         json.RawMessage `json:"args"`
	VisibleOut   string          `json:"visibleOutput"`
	Failure      string          `json:"failureReason"`
	MessagePhase string          `json:"messagePhase"`
	DisplayLabel string          `json:"displayLabel"`
}

func museHandleItemStarted(params json.RawMessage, state *museSession, stream *museMessageStream) {
	var payload struct {
		Item museItem `json:"item"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return
	}
	item := payload.Item
	state.setKind(item.ItemID, item.Kind)

	switch item.Kind {
	case "agentMessage":
		// Text arrives as deltas; nothing to emit until the first one.
	case "reasoning":
		stream.send(Message{Type: MessageThinking})
	case "toolCall":
		stream.send(Message{
			Type:   MessageToolUse,
			Tool:   museToolName(item),
			CallID: museCallID(item),
			Input:  museToolInput(item.Args),
		})
	}
}

func museHandleItemDelta(params json.RawMessage, state *museSession, stream *museMessageStream) {
	var payload struct {
		ItemID string `json:"itemId"`
		Field  string `json:"field"`
		Delta  string `json:"delta"`
	}
	if json.Unmarshal(params, &payload) != nil || payload.Delta == "" {
		return
	}
	field := payload.Field
	if field == "" {
		field = "text"
	}
	switch state.kindOf(payload.ItemID) {
	case "agentMessage":
		if field != "text" {
			return
		}
		state.markStreamed(payload.ItemID)
		stream.send(Message{Type: MessageText, Content: payload.Delta})
	case "reasoning":
		if field != "text" && !strings.HasPrefix(field, "summary") {
			return
		}
		stream.send(Message{Type: MessageThinking, Content: payload.Delta})
	}
}

func museHandleItemCompleted(params json.RawMessage, state *museSession, stream *museMessageStream) {
	var payload struct {
		Item museItem `json:"item"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return
	}
	item := payload.Item

	switch item.Kind {
	case "agentMessage":
		state.observeMessage(item.Text, item.MessagePhase)
		// A host that never streamed deltas still has the authoritative text on
		// the completed item; emitting it only when nothing streamed keeps the
		// transcript from showing the reply twice.
		if item.Text != "" && !state.wasStreamed(item.ItemID) {
			stream.send(Message{Type: MessageText, Content: item.Text})
		}
	case "toolCall":
		output := item.VisibleOut
		if output == "" {
			output = item.Failure
		}
		stream.send(Message{
			Type:   MessageToolResult,
			Tool:   museToolName(item),
			CallID: museCallID(item),
			Output: output,
			Status: item.Status,
		})
	}
}

func museToolName(item museItem) string {
	if item.Tool != "" {
		return item.Tool
	}
	if item.DisplayLabel != "" {
		return item.DisplayLabel
	}
	return "tool"
}

func museCallID(item museItem) string {
	if item.CallID != "" {
		return item.CallID
	}
	if item.ToolCallID != "" {
		return item.ToolCallID
	}
	return item.ItemID
}

// museToolInput decodes the model-authored argument JSON. Muse documents the
// field as the verbatim argument string, so the string is unwrapped first; an
// already-decoded object is accepted too, because both spellings reach a client
// that did not author the record. A value that is not a JSON object is surfaced
// under a single key rather than dropped, so the board can still show what was
// used.
func museToolInput(raw json.RawMessage) map[string]any {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil
	}
	var input map[string]any
	if json.Unmarshal(raw, &input) == nil {
		return input
	}
	var verbatim string
	if json.Unmarshal(raw, &verbatim) == nil {
		if strings.TrimSpace(verbatim) == "" {
			return nil
		}
		if json.Unmarshal([]byte(verbatim), &input) == nil {
			return input
		}
		return map[string]any{"arguments": verbatim}
	}
	return map[string]any{"arguments": text}
}

// museTokenUsage maps MSP's per-turn counters onto Multica's TokenUsage. The
// four Multica fields are mutually exclusive: MSP reports promptTokens including
// the cached prefix, so cache reads are subtracted out of the input total rather
// than added to it.
func (u TokenUsage) reported() bool {
	return u.InputTokens != 0 || u.OutputTokens != 0 || u.CacheReadTokens != 0 || u.CacheWriteTokens != 0
}

func museTokenUsage(params json.RawMessage) (TokenUsage, string) {
	var payload struct {
		ModelID string `json:"modelId"`
		Usage   struct {
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			CacheReadTokens  int64 `json:"cacheReadTokens"`
			CacheWriteTokens int64 `json:"cacheWriteTokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return TokenUsage{}, ""
	}
	usage := TokenUsage{
		InputTokens:      payload.Usage.InputTokens - payload.Usage.CacheReadTokens - payload.Usage.CacheWriteTokens,
		OutputTokens:     payload.Usage.OutputTokens,
		CacheReadTokens:  payload.Usage.CacheReadTokens,
		CacheWriteTokens: payload.Usage.CacheWriteTokens,
	}
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	model := payload.ModelID
	if model == "" {
		model = "unknown"
	}
	return usage, model
}
