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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// zcodeProtocolErrorSessionUnavailable is the structured error code the
// ZCode Protocol server raises when session/resume cannot find the requested
// session ("Session not found: ...", zcodeProtocolErrorCodes,
// packages/shared/src/zcode-protocol/index.ts). It is the only signal this
// backend maps to Result.ResumeRejected: the transcript is gone and only a
// fresh session can cure the run.
const zcodeProtocolErrorSessionUnavailable = -32004

const (
	// defaultZcodeHandshakeTimeout bounds the startup RPCs (session/create,
	// session/resume, session/subscribe, session/send). Matches the Codex
	// app-server default: NDJSON over stdio answers in milliseconds once the
	// Node CLI is up, but first-run cold starts pay module-load time.
	defaultZcodeHandshakeTimeout = 30 * time.Second
	// defaultZcodeInterruptGrace bounds how long the backend waits for
	// `turn.completed` with resultType "cancelled" after session/stop before
	// killing the process tree. Codex's measured interrupt-acknowledgement
	// P99 was 15ms (codex_interrupt_latency_integration_test.go); two seconds
	// keeps more than 100x headroom over the observed worst completion.
	defaultZcodeInterruptGrace = 2 * time.Second
)

// zcodeBlockedArgs are flags and subcommands hardcoded by the daemon that
// must not be overridden by user-configured custom_args or a runtime launch
// prefix. `app-server`/`agent-server` select the protocol subcommand
// (apps/zcode-cli/packages/cli/src/arguments.ts isProtocolServerInvocation
// only classifies those positionals as a protocol server); `--prepare-storage`
// is a one-shot storage bootstrap mode, not a server mode; `--surface`
// rewrites telemetry identity; `--prompt`/`-p`, `tui`, `version`,
// `--version`/`-v`, `--help`/`-h` switch out of server mode entirely
// (isProtocolServerInvocation would no longer classify the invocation as a
// protocol server).
var zcodeBlockedArgs = map[string]blockedArgMode{
	"app-server":        blockedStandalone,
	"agent-server":      blockedStandalone,
	"--prepare-storage": blockedStandalone,
	"--surface":         blockedWithValue,
	"--prompt":          blockedWithValue,
	"-p":                blockedWithValue,
	"tui":               blockedStandalone,
	"version":           blockedStandalone,
	"--version":         blockedStandalone,
	"-v":                blockedStandalone,
	"--help":            blockedStandalone,
	"-h":                blockedStandalone,
}

// zcodeBackend implements Backend by spawning `zcode app-server` and speaking
// the ZCode Protocol: newline-delimited JSON over stdio, with server-pushed
// `session/event` notifications and server-originated requests
// (interaction/requestPermission). Verified against the zcode 0.16.x source
// tree (packages/shared/src/zcode-protocol/index.ts); no initialize handshake
// exists — readiness is the first response to session/create.
//
// Wire-framing notes the implementation depends on:
//
//   - Frames are one JSON object per line, but the wire schema
//     (zcodeProtocolMessageSchema, index.ts:327-333) is strict and has NO
//     `jsonrpc` member: client requests are {id, method, params}, responses
//     {id, result|error}, notifications {method, params}. Sending a
//     JSON-RPC-2.0 `jsonrpc:"2.0"` member would fail the server's strict
//     parse, and the request would be dropped with a -32600 frame instead of
//     dispatched.
//   - Streaming text arrives as `model.streaming` events (kind:
//     text_delta/reasoning_delta, payload carries assistantMessageId and
//     partId). The `part.delta` event type exists in the schema but
//     mapSessionEventType has no branch that produces it, so it never appears
//     on the session/event stream.
//   - `message.upserted` is emitted for user, assistant AND system messages
//     with no role field on the payload (session-mapper.ts maps
//     UserMessage/AssistantMessage/SystemMessage onto it), so every upserted
//     event goes through the role discrimination in zcodeRoleFilter before it
//     may reach the transcript.
type zcodeBackend struct {
	cfg Config
}

// zcodeMessageStream serializes sends and the final close so a late stdout
// reader cannot send on a closed channel. Mirrors zeroclaw/dim/grok.
type zcodeMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newZcodeMessageStream(size int) *zcodeMessageStream {
	return &zcodeMessageStream{ch: make(chan Message, size)}
}

func (s *zcodeMessageStream) send(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, msg)
}

func (s *zcodeMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *zcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "zcode"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("zcode executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	// The process outlives runCtx on daemon cancellation: session/stop must
	// still reach a live server during the interrupt grace, so the command
	// runs on a detached context that cleanup stops explicitly (same shape as
	// codex executeOnce's processCtx).
	processCtx, stopProcess := context.WithCancel(context.WithoutCancel(runCtx))

	zcodeArgs := append([]string{"app-server"}, filterCustomArgs(opts.CustomArgs, zcodeBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(processCtx, zcodeArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(zcodeArgs, trustAgentCommandPositional(0, "app-server")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	// The app-server is a long-lived process; give cmd.Wait a bounded window
	// after the context is cancelled so descendants that inherit the output
	// pipes cannot hang cleanup forever (same shape as codexProcessWaitDelay).
	cmd.WaitDelay = 10 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		stopProcess()
		return nil, fmt.Errorf("zcode stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		stopProcess()
		return nil, fmt.Errorf("zcode stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point (`stderrDone`) that
	// fires before the failure-promotion decision; see hermes.go for why the
	// copy-goroutine form races less than relying on cmd.Stderr under load.
	// The bounded tail feeds error messages when the CLI exits before
	// emitting a structured error.
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[zcode:stderr] "), agentStderrTailBytes)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		stopProcess()
		return nil, fmt.Errorf("zcode stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		stopProcess()
		return nil, fmt.Errorf("start zcode: %w", err)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrBuf, stderr)
	}()

	b.cfg.Logger.Info("zcode app-server started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newZcodeMessageStream(256)
	resCh := make(chan Result, 1)

	// streamingCurrentTurn gates every session/event so anything the runtime
	// pushes outside our turn — create/subscribe replays, resume transcripts —
	// is dropped instead of landing in the transcript. It must default to
	// false: the server can flush replayed events before it answers the
	// request that triggered them. Flipped to true only just before
	// session/send, and paired with the subscribe acknowledgement's eventSeq
	// baseline as a second guard.
	var streamingCurrentTurn atomic.Bool
	var terminalObserved atomic.Bool

	// The turn input is built before the client so the user-echo
	// discriminator (plan §1.3.1 rule 2) can match it: the backend sends
	// exactly one turn input per run, and the continuity-notice prefix rides
	// in that exact string when the caller supplied one (same contract as
	// codex — the daemon owns the wording and leaves it empty when the prompt
	// already carries the notice).
	userText := prompt
	if opts.ResumeContinuityNotice != "" {
		userText = opts.ResumeContinuityNotice + "\n\n" + prompt
	}
	roleFilter := newZcodeRoleFilter(userText)

	// An empty thinking level is the picker's "Default" — "let the runtime
	// decide". Every other backend implements that by simply omitting the
	// effort flag; zcode cannot: its create rejects a reasoning-capable
	// selection without options.reasoningLevel. So the runtime's decision is
	// resolved here, through the same chain its own client uses (catalog
	// defaultLevel, else the first advertised level — see
	// zcodeResolveThinkingLevel). Resume is exempt: the resumed session
	// carries its persisted level.
	effectiveThinking := opts.ThinkingLevel
	if effectiveThinking == "" && opts.ResumeSessionID == "" {
		if _, _, ok := splitZcodeModelSelection(opts.Model); ok {
			effectiveThinking = zcodeResolveThinkingLevel(ctx, b.cfg.commandAt(execPath), opts.Model)
			if effectiveThinking != "" && b.cfg.Logger != nil {
				b.cfg.Logger.Info("zcode: empty thinking level; using the model's advertised default",
					"backend", "zcode", "model", opts.Model, "thinking_level", effectiveThinking)
			}
		}
	}

	terminalCh := make(chan zcodeTurnOutcome, 4)
	// Delta accounting for the plan §2.5 reconcile. Only the stdout reader
	// goroutine (handleLine → onEvent) touches these, so plain variables are
	// safe: the plan correlates upserted events to the active assistant message
	// by arrival order, and a new assistantMessageId resets the accumulation.
	var activeAssistantID string
	var deltaText strings.Builder

	c := &zcodeClient{
		cfg:         b.cfg,
		stdin:       stdin,
		pending:     make(map[int]*pendingRPC),
		processDone: make(chan struct{}),
	}
	c.onEvent = func(ev zcodeSessionEvent) {
		// Double gate: the in-turn flag plus the subscribe baseline seq.
		if !streamingCurrentTurn.Load() || ev.Seq < c.eventBaseline() {
			return
		}
		switch ev.Type {
		case "turn.completed", "turn.failed":
			out := zcodeParseTurnOutcome(ev)
			if out.terminal {
				terminalObserved.Store(true)
				trySendTerminal(terminalCh, out)
			}
			return
		case "model.streaming":
			var p struct {
				Kind               string `json:"kind"`
				Delta              string `json:"delta"`
				AssistantMessageID string `json:"assistantMessageId"`
				PartID             string `json:"partId"`
			}
			if json.Unmarshal(ev.Payload, &p) != nil {
				return
			}
			switch p.Kind {
			case "text_delta":
				if p.AssistantMessageID != "" && p.AssistantMessageID != activeAssistantID {
					activeAssistantID = p.AssistantMessageID
					deltaText.Reset()
				}
				deltaText.WriteString(p.Delta)
				msgStream.send(Message{Type: MessageText, Content: p.Delta})
			case "reasoning_delta":
				msgStream.send(Message{Type: MessageThinking, Content: p.Delta})
			default:
				// start/finish/error, text_*/reasoning_* boundary frames and
				// tool_input_* previews are ignored: tool lifecycle arrives
				// via tool.updated with authoritative payloads.
			}
			return
		case "message.upserted":
			var p zcodeUpsertedPayload
			if json.Unmarshal(ev.Payload, &p) != nil {
				return
			}
			if !roleFilter.deliver(p) {
				b.cfg.Logger.Debug("zcode message.upserted excluded by role discrimination")
				return
			}
			// The authoritative snapshot reconciles the streamed deltas. The
			// transcript is append-only, so a verbatim match emits nothing, a
			// prefix match emits only the missing suffix, and a divergence
			// falls back to one corrective full text (plan §2.5).
			delivered := deltaText.String()
			switch {
			case p.Content == delivered:
			case strings.HasPrefix(p.Content, delivered):
				if suffix := p.Content[len(delivered):]; suffix != "" {
					msgStream.send(Message{Type: MessageText, Content: suffix})
				}
			default:
				msgStream.send(Message{Type: MessageText, Content: p.Content})
			}
			deltaText.Reset()
			return
		}
		if msg, ok := zcodeEventToMessage(ev); ok {
			msgStream.send(msg)
		}
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("zcode process exited"))
	}()

	go func() {
		defer cancel()
		defer stopProcess()
		defer msgStream.close()
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var finalOutput string
		var sessionID string
		// Set only when the server structurally refuses the resume
		// (-32004 sessionUnavailable). Handshake/transport failures keep it
		// false: they are not curable by a fresh session, per the
		// Result.ResumeRejected contract in agent.go.
		var resumeRejected bool

		handshakeTimeout := opts.HandshakeTimeout
		if handshakeTimeout == 0 {
			handshakeTimeout = defaultZcodeHandshakeTimeout
		}

		classifyStartupFailure := func(err error, what string) (string, string) {
			switch {
			case ctx.Err() != nil:
				return "aborted", "execution cancelled"
			case errors.Is(runCtx.Err(), context.DeadlineExceeded):
				return "timeout", fmt.Sprintf("zcode timed out during %s: %v", what, err)
			default:
				return "failed", fmt.Sprintf("zcode %s failed: %v", what, err)
			}
		}

		// 1. session/create or session/resume.
		// 2. Early session pin so a cancelled run still preserves the resume
		//    pointer.
		// 3. session/subscribe — the legacy session/event stream only flows
		//    once deliveryKind is set (server-operations.ts subscribeSession).
		// 4. session/send — returns immediately; the turn streams as events.
		if opts.ResumeSessionID != "" {
			params := map[string]any{
				"sessionId": opts.ResumeSessionID,
			}
			if opts.ThinkingLevel != "" {
				params["thoughtLevel"] = opts.ThinkingLevel
			}
			if mcpServers, err := buildZcodeMcpServers(opts.McpConfig, b.cfg.Logger); err != nil {
				resCh <- Result{Status: "failed", Error: fmt.Sprintf("zcode mcp_config parse failed: %v", err), DurationMs: time.Since(startTime).Milliseconds(), SessionID: opts.ResumeSessionID}
				return
			} else if len(mcpServers) > 0 {
				// Cold resume rebuilds the runtime from scratch, so MCP
				// servers must be re-injected exactly like session/create.
				params["mcpServers"] = mcpServers
			}
			result, err := c.request(runCtx, "session/resume", params, handshakeTimeout)
			if err != nil {
				if zcodeIsSessionUnavailable(err) {
					b.cfg.Logger.Warn("zcode resumed session not found; the daemon will retry fresh",
						"backend", "zcode",
						"requested_session", opts.ResumeSessionID,
					)
					resCh <- Result{
						Status:         "failed",
						Error:          fmt.Sprintf("zcode session/resume failed: %v", err),
						DurationMs:     time.Since(startTime).Milliseconds(),
						SessionID:      opts.ResumeSessionID,
						ResumeRejected: true,
					}
					return
				}
				status, msg := classifyStartupFailure(err, "session/resume")
				resCh <- Result{
					Status:     status,
					Error:      withAgentStderr(msg, "zcode", stderrBuf.Tail()),
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  opts.ResumeSessionID,
				}
				return
			}
			sessionID = zcodeSessionIDFromSnapshot(result)
			if sessionID == "" {
				sessionID = opts.ResumeSessionID
			}
		} else {
			cwd := opts.Cwd
			if cwd == "" {
				cwd = "."
			}
			params := map[string]any{
				"workspace": map[string]any{
					"workspacePath": cwd,
					// TaskID is unique per task, which is all workspaceKey
					// needs: it scopes storage/identity for the session. The
					// server enforces a non-empty key (-32602 on the strict
					// workspace ref schema, caught by the real-binary smoke
					// test), so callers without a task id — direct API use,
					// integration tests — fall back to the resolved workspace
					// directory: stable per checkout, which matches the key's
					// identity role.
					"workspaceKey": zcodeWorkspaceKey(b.cfg.TaskID, cwd),
				},
				// "immediate" is required for cross-run resume: deferred
				// sessions stay drafts that never enter the session store
				// (server-operations.ts reconcileRecordPersistence).
				"persistence": "immediate",
				// Headless execution cannot answer permission prompts. "yolo"
				// auto-approves; any interaction/requestPermission that still
				// arrives is answered deny by the client (fail-closed).
				"mode": "yolo",
			}
			if providerID, modelID, ok := splitZcodeModelSelection(opts.Model); ok {
				params["model"] = zcodeModelSelection(providerID, modelID, effectiveThinking)
			}
			if effectiveThinking != "" {
				params["thoughtLevel"] = effectiveThinking
			}
			if mcpServers, err := buildZcodeMcpServers(opts.McpConfig, b.cfg.Logger); err != nil {
				resCh <- Result{Status: "failed", Error: fmt.Sprintf("zcode mcp_config parse failed: %v", err), DurationMs: time.Since(startTime).Milliseconds()}
				return
			} else if len(mcpServers) > 0 {
				params["mcpServers"] = mcpServers
			}
			result, err := c.request(runCtx, "session/create", params, handshakeTimeout)
			if err != nil {
				status, msg := classifyStartupFailure(err, "session/create")
				resCh <- Result{
					Status:     status,
					Error:      withAgentStderr(msg, "zcode", stderrBuf.Tail()),
					DurationMs: time.Since(startTime).Milliseconds(),
				}
				return
			}
			sessionID = zcodeSessionIDFromSnapshot(result)
			if sessionID == "" {
				resCh <- Result{
					Status:     "failed",
					Error:      withAgentStderr("zcode session/create returned no sessionId", "zcode", stderrBuf.Tail()),
					DurationMs: time.Since(startTime).Milliseconds(),
				}
				return
			}
		}

		// Early session pin so a cancelled run still preserves resume pointer.
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("zcode session ready", "session_id", sessionID)

		// Subscribe before sending so the baseline seq covers everything the
		// server may flush around session/send. The subscribe response's
		// eventSeq is the lowest seq this client may trust.
		subSeq, err := c.subscribe(runCtx, sessionID, handshakeTimeout)
		if err != nil {
			status, msg := classifyStartupFailure(err, "session/subscribe")
			resCh <- Result{
				Status:     status,
				Error:      withAgentStderr(msg, "zcode", stderrBuf.Tail()),
				DurationMs: time.Since(startTime).Milliseconds(),
				SessionID:  sessionID,
			}
			return
		}
		c.setEventBaseline(subSeq)

		sendParams := map[string]any{
			"sessionId": sessionID,
			"content":   userText,
		}
		if providerID, modelID, ok := splitZcodeModelSelection(opts.Model); ok {
			sendParams["modelSelection"] = zcodeModelSelection(providerID, modelID, effectiveThinking)
		}

		streamingCurrentTurn.Store(true)
		if _, err := c.request(runCtx, "session/send", sendParams, handshakeTimeout); err != nil {
			status, msg := classifyStartupFailure(err, "session/send")
			resCh <- Result{
				Status:     status,
				Error:      withAgentStderr(msg, "zcode", stderrBuf.Tail()),
				DurationMs: time.Since(startTime).Milliseconds(),
				SessionID:  sessionID,
			}
			return
		}

		// The turn now streams as session/event notifications. Wait for the
		// terminal event, a daemon cancellation (session/stop then a bounded
		// grace), or the hard run deadline (kill immediately — the plan
		// deliberately does not interrupt-then-wait on the wall clock).
		interruptGrace := opts.TurnInterruptTimeout
		if interruptGrace == 0 {
			interruptGrace = defaultZcodeInterruptGrace
		}
		outcome, stopped := c.awaitTurn(runCtx, terminalCh, sessionID, interruptGrace)
		if stopped {
			finalStatus = "aborted"
			finalError = "execution cancelled"
			if outcome.terminal {
				// The server landed a proper turn completion inside the stop
				// grace window; trust it over the blanket aborted label.
				terminalObserved.Store(true)
				finalStatus = outcome.status()
				finalOutput = outcome.output
				finalError = outcome.errorText
			}
		} else if outcome.terminal {
			finalStatus = outcome.status()
			finalOutput = outcome.output
			finalError = outcome.errorText
		} else if errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("zcode timed out after %s", timeout)
		} else if ctx.Err() != nil {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else {
			// stdout EOF or process exit without a terminal event.
			finalStatus = "failed"
			finalError = withAgentStderr("zcode process exited before the turn completed", "zcode", stderrBuf.Tail())
		}

		terminalObserved.Store(true)
		duration := time.Since(startTime)
		b.cfg.Logger.Info("zcode finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()
		stopProcess()

		// The app-server may keep the pipes open briefly after the terminal
		// event. Bound the drain so cleanup cannot hang.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), defaultZcodeInterruptGrace)
		select {
		case <-readerDone:
		case <-drainCtx.Done():
		}
		select {
		case <-stderrDone:
		case <-drainCtx.Done():
		}
		drainCancel()
		streamingCurrentTurn.Store(false)

		var usageMap map[string]TokenUsage
		if outcome.terminal {
			if usage, ok := zcodeUsageFromSummary(outcome.usageRaw); ok {
				model := opts.Model
				if _, _, ok := splitZcodeModelSelection(opts.Model); !ok {
					model = "unknown"
				}
				usageMap = map[string]TokenUsage{model: usage}
			}
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      sessionID,
			ResumeRejected: resumeRejected,
			Usage:          usageMap,
		}
	}()

	return &Session{
		Messages:         msgStream.ch,
		Result:           resCh,
		TerminalObserved: terminalObserved.Load,
	}, nil
}

// awaitTurn blocks until the turn reaches a terminal event, the process dies,
// or the run context is done. On daemon cancellation (context cancelled while
// the run deadline has not expired) it first sends session/stop and waits
// interruptGrace for the cancelled turn to land; on hard-deadline expiry it
// returns immediately — the process tree is killed by cmd.Cancel and the plan
// deliberately does not interrupt-then-wait on the wall clock. The second
// return value reports whether the stop path ran, so the caller maps the
// outcome to aborted unless the server reported a proper completion.
func (c *zcodeClient) awaitTurn(runCtx context.Context, terminalCh chan zcodeTurnOutcome, sessionID string, interruptGrace time.Duration) (zcodeTurnOutcome, bool) {
	select {
	case out := <-terminalCh:
		return out, false
	case <-c.processDone:
		return zcodeTurnOutcome{}, false
	case <-runCtx.Done():
		if !errors.Is(runCtx.Err(), context.Canceled) {
			return zcodeTurnOutcome{}, false
		}
		// Daemon/task cancellation: give the server a bounded chance to land
		// a resultType:"cancelled" completion via session/stop. The request
		// runs on a detached context because runCtx is already cancelled.
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(runCtx), interruptGrace)
		defer stopCancel()
		_, _ = c.request(stopCtx, "session/stop", map[string]any{"sessionId": sessionID}, interruptGrace)
		graceTimer := time.NewTimer(interruptGrace)
		defer graceTimer.Stop()
		select {
		case out := <-terminalCh:
			return out, true
		case <-graceTimer.C:
			return zcodeTurnOutcome{}, true
		}
	}
}

// zcodeTurnOutcome is the parsed terminal event for the current turn.
type zcodeTurnOutcome struct {
	terminal   bool
	resultType string
	output     string
	errorText  string
	usageRaw   json.RawMessage
}

func (o zcodeTurnOutcome) status() string {
	if !o.terminal {
		return "failed"
	}
	switch o.resultType {
	case "success":
		return "completed"
	case "cancelled":
		// User interruption is a normal end mapped to aborted (plan §2.5).
		return "aborted"
	default:
		// error_max_turns / error_max_budget / error_during_execution /
		// error_max_tool_calls and any unknown future value.
		return "failed"
	}
}

func trySendTerminal(ch chan zcodeTurnOutcome, out zcodeTurnOutcome) {
	select {
	case ch <- out:
	default:
	}
}

// zcodeParseTurnOutcome extracts the fields the backend needs from
// turn.completed / turn.failed payloads. Unknown shapes parse to a non-
// terminal outcome so a malformed event can never strand the run (the
// context/process branches of awaitTurn still fire).
func zcodeParseTurnOutcome(ev zcodeSessionEvent) zcodeTurnOutcome {
	switch ev.Type {
	case "turn.completed":
		var p struct {
			Response   string          `json:"response"`
			ResultType string          `json:"resultType"`
			Usage      json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(ev.Payload, &p) != nil {
			return zcodeTurnOutcome{}
		}
		out := zcodeTurnOutcome{terminal: true, resultType: p.ResultType, output: p.Response, usageRaw: p.Usage}
		if p.ResultType != "success" && p.ResultType != "cancelled" {
			out.errorText = fmt.Sprintf("zcode turn completed with resultType %q", p.ResultType)
		}
		return out
	case "turn.failed":
		var p struct {
			Error struct {
				Message     string `json:"message"`
				Code        string `json:"code"`
				Retryable   bool   `json:"retryable"`
				Attribution *struct {
					Source            string `json:"source"`
					ProviderErrorCode string `json:"providerErrorCode"`
					Retryable         bool   `json:"retryable"`
				} `json:"attribution"`
			} `json:"error"`
		}
		if json.Unmarshal(ev.Payload, &p) != nil {
			return zcodeTurnOutcome{}
		}
		out := zcodeTurnOutcome{terminal: true, resultType: "error_during_execution", errorText: p.Error.Message}
		if p.Error.Message == "" {
			out.errorText = "zcode turn failed"
		}
		if p.Error.Code != "" {
			out.errorText += fmt.Sprintf(" (code %s)", p.Error.Code)
		}
		if p.Error.Attribution != nil && p.Error.Attribution.ProviderErrorCode != "" {
			out.errorText += fmt.Sprintf(" (provider code %s)", p.Error.Attribution.ProviderErrorCode)
		}
		if p.Error.Retryable || (p.Error.Attribution != nil && (p.Error.Attribution.Retryable || p.Error.Attribution.Source == "network")) {
			// Surface the retryable hint for the platform's retry
			// classification without inventing a bespoke error type (plan §2.5).
			out.errorText += "; provider reports the failure as retryable"
		}
		return out
	}
	return zcodeTurnOutcome{}
}

// zcodeSessionEvent is the subset of the session/event envelope
// (zcodeEventEnvelopeSchema, index.ts:1037-1047) this backend consumes.
// Parsing is deliberately lenient: unknown fields are ignored, because the
// server's own payloads are strict and evolve.
type zcodeSessionEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	TurnID    string          `json:"turnId"`
	Seq       int64           `json:"seq"`
	Payload   json.RawMessage `json:"payload"`
}

// zcodeEventToMessage maps one stateless non-terminal session/event to a
// Message per plan §1.3. model.streaming and message.upserted are deliberately
// NOT handled here: both need per-run state (the delta accumulator and the
// role filter) and are dispatched in Execute's onEvent closure. Unknown event
// types yield no message.
func zcodeEventToMessage(ev zcodeSessionEvent) (Message, bool) {
	switch ev.Type {
	case "turn.started":
		return Message{Type: MessageStatus, Status: "running"}, true
	case "tool.updated":
		var p struct {
			Kind       string          `json:"kind"`
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Input      json.RawMessage `json:"input"`
			Result     *struct {
				Content string `json:"content"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(ev.Payload, &p) != nil || p.ToolCallID == "" {
			return Message{}, false
		}
		switch p.Kind {
		case "scheduled", "started":
			input := map[string]any{}
			if len(p.Input) > 0 {
				_ = json.Unmarshal(p.Input, &input)
			}
			return Message{Type: MessageToolUse, Tool: p.ToolName, CallID: p.ToolCallID, Input: input}, true
		case "result":
			if p.Result == nil {
				return Message{}, false
			}
			return Message{Type: MessageToolResult, Tool: p.ToolName, CallID: p.ToolCallID, Output: p.Result.Content}, true
		case "error":
			if p.Error == nil {
				return Message{}, false
			}
			return Message{Type: MessageToolResult, Tool: p.ToolName, CallID: p.ToolCallID, Output: p.Error.Message}, true
		default:
			// progress/batch/raw are ignored for MVP (plan §1.3).
			return Message{}, false
		}
	default:
		// session.updated, checkpoint.created, streamRecovery.updated,
		// permission.requested (answered deny at the transport), part.*,
		// userInput.* etc. are ignored for MVP.
		return Message{}, false
	}
}

// zcodeUpsertedPayload mirrors zcodeMessageUpsertedEventPayloadSchema
// (index.ts:1271-1279). It carries no role and no message id.
type zcodeUpsertedPayload struct {
	Content         string            `json:"content"`
	Attachments     []json.RawMessage `json:"attachments"`
	ToolCalls       []json.RawMessage `json:"toolCalls"`
	Type            string            `json:"type"`
	CompactBoundary json.RawMessage   `json:"compactBoundary"`
}

// zcodeRoleFilter implements the plan §1.3.1 layered discrimination for
// message.upserted events. It is stateful per run: the backend sends exactly
// one turn input, so the user echo is a single known string.
//
// Rules (first match wins, an excluded event only logs):
//
//  1. system: payload has `type` ("init"|"compact_boundary"|"interrupted") or
//     a compactBoundary → drop. The only wire form with an explicit marker.
//  2. user echo: content equals this run's full sent input exactly → drop,
//     first match only. Exact (not prefix) matching avoids dropping an
//     assistant answer that quotes the prompt; a second occurrence of the
//     same content is delivered (prefer over-delivering to losing assistant
//     text).
//  3. assistant: payload has toolCalls (AssistantMessagePayload only) →
//     deliver. Presence, not count: an empty array is still assistant.
//  4. fallback: bare {content} → deliver as assistant. User payloads may
//     carry optional attachments and are otherwise shape-identical, so the
//     source offers no stronger signal; over-delivering a duplicate text is
//     cheaper than dropping the assistant's answer.
type zcodeRoleFilter struct {
	sentInput        string
	firstEchoMatched bool
}

func newZcodeRoleFilter(sentInput string) *zcodeRoleFilter {
	return &zcodeRoleFilter{sentInput: sentInput}
}

func (f *zcodeRoleFilter) deliver(p zcodeUpsertedPayload) bool {
	if p.Type != "" || len(p.CompactBoundary) > 0 {
		return false // rule 1: system
	}
	if p.Content == f.sentInput && !f.firstEchoMatched {
		f.firstEchoMatched = true
		return false // rule 2: user echo, first occurrence only
	}
	if p.ToolCalls != nil {
		return true // rule 3: assistant (toolCalls present)
	}
	return true // rule 4: fallback delivers
}

// zcodeClient is the ZCode Protocol transport: newline-delimited JSON over
// stdio. Wire frames carry no `jsonrpc` member (see zcodeBackend's doc).
type zcodeClient struct {
	cfg Config

	stdin io.Writer

	mu          sync.Mutex
	nextID      int
	pending     map[int]*pendingRPC
	processDone chan struct{}
	processErr  error

	// baselineSeq is the subscribe acknowledgement's eventSeq: events with a
	// smaller seq were emitted before this client subscribed and are replay.
	baselineSeq int64
	baselineSet bool

	onEvent func(zcodeSessionEvent)
}

// eventBaseline returns the seq floor for events this client accepts. Before
// the subscribe acknowledgement lands it returns the maximum int64 so the
// Execute-side gate rejects everything.
func (c *zcodeClient) eventBaseline() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.baselineSet {
		return int64(^uint64(0) >> 1)
	}
	return c.baselineSeq
}

func (c *zcodeClient) setEventBaseline(seq int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baselineSeq = seq
	c.baselineSet = true
}

// request sends one client→server request and waits for its response.
// timeout > 0 bounds the wait independent of ctx.
func (c *zcodeClient) request(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx := ctx
	cancelRequest := func() {}
	if timeout > 0 {
		requestCtx, cancelRequest = context.WithTimeout(ctx, timeout)
	}
	defer cancelRequest()

	c.mu.Lock()
	if c.processErr != nil {
		err := c.processErr
		c.mu.Unlock()
		return nil, err
	}
	c.nextID++
	id := c.nextID
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	msg := map[string]any{
		"id":     id,
		"method": method,
		"params": params,
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
	case <-c.processDone:
		select {
		case res := <-pr.ch:
			return res.result, res.err
		default:
		}
		c.mu.Lock()
		delete(c.pending, id)
		err := c.processErr
		c.mu.Unlock()
		if requestCtx.Err() != nil {
			return nil, fmt.Errorf("%s aborted: %w", method, requestCtx.Err())
		}
		if err == nil {
			err = fmt.Errorf("zcode process exited during %s", method)
		}
		return nil, err
	case <-requestCtx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s aborted: %w", method, requestCtx.Err())
	}
}

// subscribe registers the desktop-continuous delivery and returns the event
// seq baseline. Without it the server never pushes session/event
// (server-operations.ts gates the push on record.deliveryKind).
func (c *zcodeClient) subscribe(ctx context.Context, sessionID string, timeout time.Duration) (int64, error) {
	result, err := c.request(ctx, "session/subscribe", map[string]any{
		"sessionId":    sessionID,
		"deliveryKind": "desktop-continuous",
	}, timeout)
	if err != nil {
		return 0, err
	}
	var r struct {
		EventSeq int64 `json:"eventSeq"`
	}
	if json.Unmarshal(result, &r) != nil {
		return 0, fmt.Errorf("session/subscribe returned no eventSeq")
	}
	return r.EventSeq, nil
}

// zcodeRPCError mirrors the server's error object on a response frame.
type zcodeRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *zcodeRPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// zcodeIsSessionUnavailable reports whether err is the server's structured
// "Session not found" rejection (-32004), the only resume failure a fresh
// session can cure.
func zcodeIsSessionUnavailable(err error) bool {
	var rpcErr *zcodeRPCError
	if err != nil && errors.As(err, &rpcErr) && rpcErr != nil {
		return rpcErr.Code == zcodeProtocolErrorSessionUnavailable
	}
	return false
}

func (c *zcodeClient) respond(id json.RawMessage, result any) {
	c.writeFrame(map[string]any{"id": json.RawMessage(id), "result": result})
}

// zcodeRuntimePreferencesDefaults answers the server-originated
// session/requestRuntimePreferences request: a real app-server asks for the
// host's runtime preferences while serving session/create and treats a silent
// client as a hard failure (-32022 after its 15s timeout, verified against
// the 0.16.9 binary). Every member except nativeSearchEnhancementsEnabled
// carries a schema default; the values mirror a plain headless host — no
// native search integration, no memory, auto-resolving ask-user questions
// (there is no UI to answer them), and the current default budget strategy.
func zcodeRuntimePreferencesDefaults() map[string]any {
	return map[string]any{
		"nativeSearchEnhancementsEnabled":      false,
		"memoryEnabled":                        false,
		"askUserQuestionAutoResolutionEnabled": true,
		"modelContextBudgetStrategy":           "preflight-v1",
	}
}

// zcodeWriteReverseResult / zcodeWriteReverseUnsupported are the standalone
// (non-client) reverse-request answers used by model discovery, which owns
// its stdin rather than a zcodeClient.
func zcodeWriteReverseResult(w io.Writer, id json.RawMessage, result any) {
	data, err := json.Marshal(map[string]any{"id": json.RawMessage(id), "result": result})
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = w.Write(data)
}

// zcodeWriteReverseUnsupported / zcodeWriteReverseError answer a server-
// originated request the standalone (non-client) discovery loop cannot
// handle. The wire schema (zcodeProtocolMessageSchema) is strict about the
// response shape {id, result|error}: the error must sit at the top level,
// not nested inside result.
func zcodeWriteReverseUnsupported(w io.Writer, id json.RawMessage, method string) {
	zcodeWriteReverseError(w, id, -32601, fmt.Sprintf("method not supported by multica: %s", method))
}

func zcodeWriteReverseError(w io.Writer, id json.RawMessage, code int, message string) {
	data, err := json.Marshal(map[string]any{
		"id":    json.RawMessage(id),
		"error": map[string]any{"code": code, "message": message},
	})
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = w.Write(data)
}

func (c *zcodeClient) respondError(id json.RawMessage, code int, message string) {
	c.writeFrame(map[string]any{
		"id": json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (c *zcodeClient) writeFrame(msg map[string]any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *zcodeClient) closeAllPending(err error) {
	c.mu.Lock()
	if c.processErr == nil {
		c.processErr = err
	}
	select {
	case <-c.processDone:
		// Already closed.
	default:
		close(c.processDone)
	}
	pending := c.pending
	c.pending = make(map[int]*pendingRPC)
	c.mu.Unlock()
	for _, pr := range pending {
		select {
		case pr.ch <- rpcResult{err: err}:
		default:
		}
	}
}

// handleLine dispatches one wire frame. Three shapes reach here:
//   - {id, method, params}: a server-originated request. interaction/
//     requestPermission is answered {"decision":"deny"} fail-closed (the
//     headless client cannot approve); session/requestRuntimePreferences is
//     answered with zcodeRuntimePreferencesDefaults (an unanswered request
//     fails session/create with -32022 after its 15s timeout); every other
//     method gets -32601.
//   - {id, result|error}: a response to one of our requests.
//   - {method, params}: a notification; only session/event is consumed.
func (c *zcodeClient) handleLine(line string) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *zcodeRPCError  `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		if c.cfg.Logger != nil {
			c.cfg.Logger.Debug("zcode unparseable frame", "line", truncateZcodeLogLine(line))
		}
		return
	}
	hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
	switch {
	case hasID && msg.Method != "":
		// Server-originated request.
		switch msg.Method {
		case "interaction/requestPermission":
			c.respond(msg.ID, map[string]any{"decision": "deny"})
		case "session/requestRuntimePreferences":
			c.respond(msg.ID, zcodeRuntimePreferencesDefaults())
		default:
			c.respondError(msg.ID, -32601, fmt.Sprintf("method not supported by multica: %s", msg.Method))
		}
	case hasID:
		// Response to one of our requests. Our ids are plain ints.
		var id int
		if json.Unmarshal(msg.ID, &id) != nil {
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
		if msg.Error != nil {
			trySendRPC(pr, nil, msg.Error)
		} else {
			trySendRPC(pr, msg.Result, nil)
		}
	case msg.Method != "":
		// Notification.
		if msg.Method != "session/event" {
			if c.cfg.Logger != nil {
				c.cfg.Logger.Debug("zcode notification ignored", "method", msg.Method)
			}
			return
		}
		var ev zcodeSessionEvent
		if json.Unmarshal(msg.Params, &ev) != nil {
			return
		}
		if c.onEvent != nil {
			c.onEvent(ev)
		}
	}
}

// trySendRPC resolves one pending request. A nil rpcErr must yield a nil
// error interface value: storing a typed-nil *zcodeRPCError would make
// rpcResult.err a non-nil interface whose "%v" renders "<nil>" and whose
// errors.As match dereferences to a nil pointer downstream.
func trySendRPC(pr *pendingRPC, result json.RawMessage, rpcErr *zcodeRPCError) {
	var err error
	if rpcErr != nil {
		err = rpcErr
	}
	select {
	case pr.ch <- rpcResult{result: result, err: err}:
	default:
	}
}

func truncateZcodeLogLine(line string) string {
	if len(line) <= 512 {
		return line
	}
	return line[:512]
}

// zcodeSessionIDFromSnapshot extracts result.session.sessionId from a
// session/create or session/resume snapshot. The field is `sessionId` —
// zcodeSessionInfoSchema has no `id` member (zcode-protocol-legacy-types.ts).
func zcodeSessionIDFromSnapshot(result json.RawMessage) string {
	var snap struct {
		Session struct {
			SessionID string `json:"sessionId"`
		} `json:"session"`
	}
	if json.Unmarshal(result, &snap) != nil {
		return ""
	}
	return snap.Session.SessionID
}

// splitZcodeModelSelection splits Multica's persisted agent.model string into
// the ZCode {providerId, modelId} pair. The composite form is the runtime's
// own picker format (model-selection.ts formatModelPickerValue), and its
// execution entry rejects a selection without a provider. A string without a
// slash is therefore NOT force-qualified: the backend omits the model and
// lets the runtime default take over rather than fabricate a providerId.
func splitZcodeModelSelection(model string) (providerID, modelID string, ok bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", "", false
	}
	idx := strings.Index(model, "/")
	if idx <= 0 || idx == len(model)-1 {
		return "", "", false
	}
	return model[:idx], model[idx+1:], true
}

// zcodeModelSelection builds the session/create `model` and session/send
// `modelSelection` payload. Reasoning-capable models (e.g. GLM-5.3-Flash)
// reject the selection with "Reasoning level is required" unless
// options.reasoningLevel carries one of the model's advertised levels, so a
// non-empty thinking level rides inside the selection rather than relying on
// the top-level thoughtLevel param (which the server skips when unknown).
func zcodeModelSelection(providerID, modelID, thinkingLevel string) map[string]any {
	selection := map[string]any{
		"providerId": providerID,
		"modelId":    modelID,
	}
	if thinkingLevel != "" {
		selection["options"] = map[string]any{
			"reasoningLevel": thinkingLevel,
		}
	}
	return selection
}

// zcodeWorkspaceKey resolves the session/create workspaceKey: the task id
// when the caller carries one, otherwise the resolved workspace directory —
// the strict schema rejects an empty key, and a per-directory key preserves
// the value's identity role for task-less callers (integration tests, direct
// API use).
func zcodeWorkspaceKey(taskID, cwd string) string {
	if taskID != "" {
		return taskID
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// buildZcodeMcpServers converts Multica's canonical mcp_config JSON into the
// ZCode Protocol mcpServers array. Same parsing skeleton and alt-key
// tolerance as buildACPMcpServers, but the output is built for zcode's strict
// schema (index.ts zcodeProtocolMcpServerSchema): stdio entries carry
// {name, command, args, env}, remote entries {name, type, url, headers}, with
// env/headers as {name,value} pair arrays. Fields the schema does not know
// must never leak through — a strict parse of the whole request would fail.
func buildZcodeMcpServers(raw json.RawMessage, logger *slog.Logger) ([]any, error) {
	trimmed := []byte(strings.TrimSpace(string(raw)))
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return []any{}, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		// Alt-key tolerance matches the ACP builder: runtime-native config
		// files sometimes nest under `servers` (jcode/Kiro) instead of the
		// canonical `mcpServers`.
		var alt struct {
			Servers map[string]json.RawMessage `json:"servers"`
		}
		if json.Unmarshal(trimmed, &alt) == nil && len(alt.Servers) > 0 {
			parsed.McpServers = alt.Servers
			if logger != nil {
				logger.Warn("mcp_config used a non-canonical top-level key; accepted via alt-key tolerance", "backend", "zcode")
			}
		}
	}
	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]any, 0, len(names))
	for _, name := range names {
		entry, err := convertZcodeMcpServer(name, parsed.McpServers[name])
		if err != nil {
			if logger != nil {
				logger.Warn("skipping invalid mcp_config entry", "backend", "zcode", "name", name, "error", err)
			}
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

// convertZcodeMcpServer converts one canonical MCP entry into zcode's wire
// shape. Entries with neither command nor url are skipped with an error so a
// single bad entry cannot fail the launch.
func convertZcodeMcpServer(name string, raw json.RawMessage) (map[string]any, error) {
	var entry struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse entry: %w", err)
	}

	command := strings.TrimSpace(entry.Command)
	url := strings.TrimSpace(entry.URL)

	if command != "" {
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		envArr := make([]map[string]any, 0, len(entry.Env))
		for _, k := range sortedStringMapKeys(entry.Env) {
			envArr = append(envArr, map[string]any{"name": k, "value": entry.Env[k]})
		}
		return map[string]any{
			"name":    name,
			"command": command,
			"args":    args,
			"env":     envArr,
		}, nil
	}

	if url != "" {
		t := strings.ToLower(strings.TrimSpace(entry.Type))
		switch t {
		case "sse":
			t = "sse"
		case "", "http", "streamable-http", "http_streamable":
			// Unknown remote transports degrade to http; zcode's schema only
			// accepts "http" | "sse".
			t = "http"
		default:
			t = "http"
		}
		headerArr := make([]map[string]any, 0, len(entry.Headers))
		for _, k := range sortedStringMapKeys(entry.Headers) {
			headerArr = append(headerArr, map[string]any{"name": k, "value": entry.Headers[k]})
		}
		return map[string]any{
			"name":    name,
			"type":    t,
			"url":     url,
			"headers": headerArr,
		}, nil
	}

	return nil, fmt.Errorf("entry has neither command nor url")
}

// zcodeUsageFromSummary maps a turn-level ModelUsageSummary
// (apps/zcode-cli/packages/contracts/src/model/index.ts) onto TokenUsage.
//
// The four TokenUsage counters are mutually exclusive (agent.go): input must
// not include cache reads/writes and output includes reasoning. zcode's usage
// store treats inputTokens as total input with cache as a breakdown
// (usage-stats-builder.ts), so the cache portions are subtracted here; output
// gains the reasoning tokens for the same reason. Semantics of the turn-level
// summary are verified against 0.16.9 source only — the smoke test calibrates.
// Flooring a negative input at zero keeps a mis-proportioned summary from
// producing nonsense counters.
func zcodeUsageFromSummary(raw json.RawMessage) (TokenUsage, bool) {
	if len(raw) == 0 {
		return TokenUsage{}, false
	}
	var u struct {
		InputTokens      int64 `json:"inputTokens"`
		OutputTokens     int64 `json:"outputTokens"`
		CacheReadTokens  int64 `json:"cacheReadTokens"`
		CacheWriteTokens int64 `json:"cacheWriteTokens"`
		ReasoningTokens  int64 `json:"reasoningTokens"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return TokenUsage{}, false
	}
	input := u.InputTokens - u.CacheReadTokens - u.CacheWriteTokens
	if input < 0 {
		input = 0
	}
	usage := TokenUsage{
		InputTokens:      input,
		OutputTokens:     u.OutputTokens + u.ReasoningTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
	return usage, true
}
