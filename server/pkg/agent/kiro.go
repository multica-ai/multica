package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// kiroReaderDrainGrace bounds how long the turn waits for trailing ACP
// notifications after the session/prompt response. A var, not a const, so
// tests can shorten it. Mirrors qoderReaderDrainGrace / traecliReaderDrainGrace.
var kiroReaderDrainGrace = 2 * time.Second

// kiroBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol subcommand,
// and --trust-all-tools covers Kiro's CLI-level tool gate while
// hermesClient handles ACP session/request_permission auto-approval. In Kiro
// CLI 2.1.1, `-a` is short for --trust-all-tools, not --agent; --agent remains
// allowed so users can select a custom Kiro agent.
var kiroBlockedArgs = map[string]blockedArgMode{
	"acp":               blockedStandalone,
	"-a":                blockedStandalone,
	"--trust-all-tools": blockedStandalone,
	"--trust-tools":     blockedWithValue,
}

// kiroBackend implements Backend by spawning `kiro-cli acp` and communicating
// via the standard ACP JSON-RPC 2.0 transport over stdin/stdout.
//
// Kiro CLI advertises loadSession, returns models from session/new, and supports
// session/set_model, so the existing Hermes/Kimi ACP client can drive it with
// only provider-specific launch and tool-name normalization.
type kiroBackend struct {
	cfg Config
}

// kiroSupplementTimeout bounds the wait for a `_session/steer` acknowledgement.
// A var so tests can shorten it.
var kiroSupplementTimeout = 8 * time.Second

// kiroSupplementSession exposes Kiro CLI's `_session/steer` ACP extension only
// while the matching session/prompt request is in flight. Kiro does not
// advertise the extension in initialize, so the daemon gates the feature by
// CLI version (see SupportsTaskSupplement).
//
// Observed contract (kiro-cli 2.33.0): `_session/steer {sessionId, message}`
// returns `{"queued": true}` immediately. Kiro then injects the message into
// the running turn at the next boundary: after the in-flight tool call
// finishes, or after the model's current message if no tool is running. The
// same session/prompt keeps going and answers it, so "queued" already means
// the current turn will see the message.
type kiroSupplementSession struct {
	client    *hermesClient
	mu        sync.RWMutex
	sessionID string
	active    atomic.Bool
}

func (s *kiroSupplementSession) stop() {
	s.active.Store(false)
}

func (s *kiroSupplementSession) ready() bool {
	if !s.active.Load() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID != ""
}

func (s *kiroSupplementSession) send(ctx context.Context, text string) error {
	if !s.active.Load() {
		return fmt.Errorf("kiro turn is no longer active")
	}
	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()
	if sessionID == "" {
		return fmt.Errorf("kiro session is not ready for steering")
	}
	rpcCtx, cancel := context.WithTimeout(ctx, kiroSupplementTimeout)
	defer cancel()
	response, err := s.client.request(rpcCtx, "_session/steer", map[string]any{
		"sessionId": sessionID,
		"message":   text,
	})
	if err != nil {
		return fmt.Errorf("kiro _session/steer failed: %w", err)
	}
	// A successful JSON-RPC frame alone is not proof of delivery: only
	// queued=true means Kiro accepted the message for the running turn.
	var result struct {
		Queued bool `json:"queued"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return fmt.Errorf("kiro _session/steer returned an invalid result: %w", err)
	}
	if !result.Queued {
		return fmt.Errorf("kiro _session/steer did not queue the message: %s", strings.TrimSpace(string(response)))
	}
	return nil
}

func (b *kiroBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "kiro-cli"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("kiro executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP `session/new` and `session/load` expect.
	// Fail closed on malformed JSON so the launch surfaces the real error
	// instead of silently dropping all MCP servers.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("kiro: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	kiroArgs := append([]string{"acp", "--trust-all-tools"}, filterCustomArgs(opts.CustomArgs, kiroBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, kiroArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(kiroArgs, trustAgentCommandPositional(0, "acp")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kiro stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kiro stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point
	// (`stderrDone`) that fires before the failure-promotion
	// decision; see the matching comment in hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("kiro")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kiro stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start kiro: %w", err)
	}

	// Live panic watch: Kiro can panic inside its agent loop while the ACP
	// process stays up, so session/prompt would never answer (MUL-8188).
	runtimePanic := newACPRuntimePanicDetector()
	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[kiro:stderr] "), providerErr, runtimePanic)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("kiro acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Kiro streams interim narration and the final answer as the same
	// AgentMessageChunk type; the tracker keeps only the post-tool-call block
	// for Result.Output while retaining the full text for error detection.
	var deliverable acpDeliverableTracker
	var streamingCurrentTurn atomic.Bool
	// turnProducedMessage records whether the current prompt emitted any agent
	// message before a runtime panic; a resumed session that panics before its
	// first message is one the runtime cannot drive (MUL-8188).
	var turnProducedMessage atomic.Bool
	// Completion-preservation state for the -32603 close-handshake guard.
	//
	// Kiro raises `session/prompt -32603 "failed to generate a response"`
	// when the model fails to produce its closing turn AFTER the task has
	// already done its work. We keep such a run "completed" only when we
	// have POSITIVE proof the finishing step succeeded — never from the mere
	// absence of a result. A mid-command crash or internal session failure
	// produces the very same "tool use, no result, -32603" shape, and
	// treating that as success would mark a genuinely-unfinished task
	// completed (see the review on #5511 / MUL-4860).
	//
	// The only positive proof available at this layer is a terminal
	// ToolResult with status=="completed" for a finishing tool
	// (goal_complete or `multica issue comment add`). We record the status
	// of the MOST RECENT such result (ordered, keyed per CallID) so a later
	// failed delivery correctly overrides an earlier success — e.g.
	// "progress comment completed → final comment failed → -32603" must stay
	// failed, while "first attempt failed → retry completed → -32603" is a
	// real completion.
	var goalCompleteCallIDs sync.Map
	var issueCommentCallIDs sync.Map
	var finishingMu sync.Mutex
	var lastFinishingResultStatus string // "", "completed", or "failed"

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)
	supplements := &kiroSupplementSession{}

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		onActivity: func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
			if !streamingCurrentTurn.Load() {
				return
			}
			if msg.Type == MessageToolUse {
				msg.Tool = kiroToolNameFromTitle(msg.Tool)
				if msg.Tool == "goal_complete" && msg.CallID != "" {
					goalCompleteCallIDs.Store(msg.CallID, struct{}{})
				}
				// Recognize `multica issue comment add` by its command
				// payload regardless of how the adapter titled the tool.
				// GPT-5.6 Sol may label the shell tool something other than
				// the aliases kiroToolNameFromTitle folds into "terminal"
				// (e.g. "execute"/"run"), so gating on msg.Tool=="terminal"
				// here would miss it entirely (issue #5509). We still require
				// a CallID so the tool use can be correlated to its result.
				if msg.CallID != "" && isKiroIssueCommentAddTool(msg) {
					issueCommentCallIDs.Store(msg.CallID, struct{}{})
				}
			}
			if msg.Type == MessageToolResult {
				_, isGoal := goalCompleteCallIDs.LoadAndDelete(msg.CallID)
				_, isComment := issueCommentCallIDs.LoadAndDelete(msg.CallID)
				if isGoal || isComment {
					// Record this finishing tool's terminal outcome. The
					// most recent result wins, so the guard sees the final
					// delivery attempt's status and a later failure
					// overrides an earlier success.
					finishingMu.Lock()
					lastFinishingResultStatus = msg.Status
					finishingMu.Unlock()
				}
			}
			deliverable.observe(msg)
			turnProducedMessage.Store(true)
			trySend(msgCh, msg)
		},
		onPromptDone: func(result hermesPromptResult) {
			if !streamingCurrentTurn.Load() {
				return
			}
			select {
			case promptDone <- result:
			default:
			}
		},
	}

	supplements.client = c

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
		c.closeAllPending(fmt.Errorf("kiro process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		// Set when the ACP runtime refuses the session we asked to
		// resume. Only that is curable by starting a fresh session, so
		// handshake/network failures below must leave it false.
		var resumeRejected bool
		// Set when the live panic detector aborted session/prompt.
		var runtimePanicked bool
		effectiveModel := strings.TrimSpace(opts.Model)

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("kiro initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime
		// tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "kiro", b.cfg)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				// A runtime that refuses the recorded id has to say so here:
				// without ResumeRejected the daemon reads the bare failure as
				// "checked, not a rejection", keeps the pointer and replays the
				// same dead session on every later turn (GH #8116).
				finalStatus, finalError, resumeRejected = classifyACPResumeFailure(
					runCtx, "kiro", "session/load", err, timeout, b.cfg.Logger)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			// Apply the same defensive resolution kimi/hermes use: if
			// kiro echoes a sessionId in the session/load response, prefer
			// it (the canonical id the backend is committed to). When the
			// response is empty or doesn't include sessionId — kiro's
			// current observed shape — the helper falls back to the
			// requested id, preserving today's behavior. Fixing this here
			// too means a future kiro that DOES return a different id on
			// silent state reset is handled the same way as hermes/kimi.
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "kiro",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kiro session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "kiro session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		supplements.mu.Lock()
		supplements.sessionID = sessionID
		supplements.mu.Unlock()
		b.cfg.Logger.Info("kiro session created", "session_id", sessionID)

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("kiro set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("kiro could not switch to model %q: %v", opts.Model, err)
				if setupFailureWithholdsSessionID(opts) {
					sessionID = ""
				} else if isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so
					// the daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "kiro",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					SessionID:      sessionID,
					ResumeRejected: resumeRejected,
				}
				return
			}
			b.cfg.Logger.Info("kiro session model set", "model", opts.Model)
		}

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}
		// Kiro's published docs use `content`, while Kiro CLI 2.1.1 still
		// requires the standard ACP `prompt` field. Send both so either wire
		// shape can drive the turn.
		// TODO: drop one field once Kiro lands on a single canonical payload.
		streamingCurrentTurn.Store(true)
		// promptCtx lets a live runtime panic abort the in-flight prompt.
		// Without it a Kiro that panicked in its agent loop but kept the ACP
		// process alive leaves session/prompt waiting until the task timeout
		// or the idle watchdog (MUL-8188).
		promptCtx, promptCancel := context.WithCancel(runCtx)
		go func() {
			select {
			case <-runtimePanic.Detected():
				b.cfg.Logger.Warn("kiro runtime panicked mid-turn; aborting the prompt", "pid", cmd.Process.Pid, "session_id", sessionID)
				promptCancel()
			case <-promptCtx.Done():
			}
		}()
		// Steering opens only once the prompt is on the wire and closes as
		// soon as it answers, so a supplement can never target setup or a
		// finished turn.
		_, err = c.requestAndNotifySent(promptCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"content":   promptBlocks,
			"prompt":    promptBlocks,
		}, func() {
			if opts.EnableTaskSupplement {
				supplements.active.Store(true)
			}
		})
		promptCancel()
		supplements.stop()
		if err != nil && runtimePanic.Fired() && runCtx.Err() == nil {
			// Checked before the ctx classifiers: promptCtx was cancelled by
			// the panic, not by the task. The panic may also have taken the
			// process down ("kiro process exited"); either way the panic is
			// the real cause. Error text and resume classification are
			// settled after stderr drains (see below).
			finalStatus = "failed"
			runtimePanicked = true
		} else if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("kiro timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kiro session/prompt failed: %v", err)
				// Preserve completion only on POSITIVE proof: the most
				// recent finishing-tool ToolResult we observed was
				// status=="completed", paired with the specific -32603 close
				// handshake. A missing result is NOT proof — a mid-command
				// crash produces the same result-less shape (Must-fix #1) —
				// and because we key on the most recent outcome, a later
				// failed delivery overrides an earlier success (Must-fix #2).
				finishingMu.Lock()
				lastFinishing := lastFinishingResultStatus
				finishingMu.Unlock()
				if lastFinishing == "completed" && isKiroGoalCompleteCloseError(err) {
					b.cfg.Logger.Warn("kiro session/prompt failed after a completed finishing-tool result; preserving completed task status", "error", err)
					finalStatus = "completed"
					finalError = ""
				} else if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes backend: the runtime echoes the
					// requested id back from session/resume even when
					// the session is gone, so the stale id only fails
					// here, at prompt time. Empty SessionID lets the
					// daemon's resume-failure fallback retry fresh and
					// store the replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "kiro",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				} else if opts.ResumeSessionID != "" && isKiroOversizedHistoryImage(err) {
					// A resumed session whose history contains an image
					// exceeding the provider's max pixel dimensions replays
					// that image on every session/prompt and is rejected
					// before the current turn runs (GH #5975). Unlike a
					// missing session the transcript still exists, so this is
					// permanent for the resume path: only a fresh session
					// without the resume id can recover. Signal
					// ResumeRejected so the daemon retries once from a cold
					// session; keep the (poisoned) session id on the result
					// so it stays visible for auditing — the daemon gates the
					// retry on the boolean, not on an empty id, and a
					// successful fresh retry overwrites it with the new id.
					b.cfg.Logger.Warn("resumed session has an oversized historical image the provider rejects; signaling resume rejection so the daemon retries with a fresh session",
						"backend", "kiro",
						"session_id", sessionID,
					)
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "kiro cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, kiroReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("kiro finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		// Ensure the stderr copier has drained before consulting the
		// provider-error sniffer; see hermes.go for the failure mode.
		<-stderrDone

		// A runtime panic during the prompt is the real failure, whichever
		// path noticed it first: the live detector aborting the prompt, or
		// the process dying and closing stdout before stderr was read. The
		// Message/Location lines trail the header, so the text is only
		// complete now that stderr has drained.
		if runtimePanic.Fired() && (runtimePanicked || finalStatus == "failed") {
			finalStatus = "failed"
			finalError = "kiro runtime panicked: " + runtimePanic.Detail()
			if opts.ResumeSessionID != "" && !turnProducedMessage.Load() {
				// The resumed session loaded but the runtime crashed before
				// the turn produced anything: this session cannot be driven,
				// so let the daemon retry once from a fresh session and
				// retire it. A panic after the turn made progress is not
				// evidence against the session, so it stays resumable.
				b.cfg.Logger.Warn("resumed kiro session panicked before its first message; signaling resume rejection so the daemon retries with a fresh session",
					"session_id", sessionID,
				)
				resumeRejected = true
			}
		}
		finalOutput, providerErrorOutput := deliverable.result()

		// Promote completed→failed when stderr or the agent text
		// stream show a terminal upstream-LLM failure (HTTP 4xx /
		// rate-limit / expired token). See the helper docs for the
		// full signal set; the key safety property is that transient
		// per-attempt warnings followed by a successful retry stay
		// "completed". It reads the full text stream, not the
		// deliverable, so a give-up turn that lands before a tool call
		// stays visible.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		u := c.accumulatedUsage()

		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			model := effectiveModel
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
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

	session := &Session{Messages: msgCh, Result: resCh}
	if opts.EnableTaskSupplement {
		session.Supplement = supplements.send
		session.SupplementReady = supplements.ready
	}
	return session, nil
}

func isKiroGoalCompleteCloseError(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if rpcErr.Method != "session/prompt" || rpcErr.Code != -32603 {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(rpcErr.Message), "Internal error") {
		return false
	}
	return strings.Contains(strings.ToLower(rpcErr.Data), "failed to generate a response")
}

// isKiroOversizedHistoryImage reports whether err is Kiro/upstream rejecting a
// resumed conversation because an image already baked into the session history
// exceeds the provider's maximum allowed pixel dimensions. Kiro surfaces this
// at session/prompt time as a -32603 whose `data` names the offending
// messages[n].content[m].image.source.base64.data block and the dimension
// limit (GH #5975), e.g.:
//
//	session/prompt: Internal error (code=-32603, data=Encountered an error in
//	the response stream: messages.14.content.0.image.source.base64.data: At
//	least one of the image dimensions exceed max allowed size: 8000 pixels)
//
// Both markers must be present so an unrelated -32603 — a transient "failed to
// generate a response" close, or a mid-command crash producing the same
// result-less shape — is never misread as a permanent history incompatibility.
// This is deliberately distinct from isACPSessionNotFound: the session exists,
// only its historical multimodal content is unusable, so the recovery is a
// fresh session started WITHOUT the resume id rather than clearing a dead id.
func isKiroOversizedHistoryImage(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if rpcErr.Method != "session/prompt" || rpcErr.Code != -32603 {
		return false
	}
	data := strings.ToLower(rpcErr.Data)
	return strings.Contains(data, "image.source.base64.data") &&
		strings.Contains(data, "image dimensions exceed max allowed size")
}

// isKiroIssueCommentAddTool reports whether a tool-use message is a
// `multica issue comment add` invocation. It keys purely off the command
// payload, not the normalized tool name: GPT-5.6 Sol adapters may title the
// shell tool with a name that doesn't fold into "terminal" (the earlier
// msg.Tool=="terminal" gate silently dropped those and left completed tasks
// marked failed — see #5509). isKiroIssueCommentAddCommand is strict enough
// that no non-shell tool's input will accidentally match.
func isKiroIssueCommentAddTool(msg Message) bool {
	command, _ := msg.Input["command"].(string)
	return isKiroIssueCommentAddCommand(command)
}

func isKiroIssueCommentAddCommand(command string) bool {
	parts := trimLeadingEnvAssignments(strings.Fields(command))
	// Some runtimes route terminal calls through a shell wrapper such as
	// `sh -c "multica issue comment add ..."`; unwrap a single such layer so
	// the real invocation is still recognized.
	if len(parts) >= 3 && isPOSIXShellName(parts[0]) && parts[1] == "-c" {
		inner := strings.Trim(strings.Join(parts[2:], " "), "\"'")
		parts = trimLeadingEnvAssignments(strings.Fields(inner))
	}
	if len(parts) < 4 {
		return false
	}
	executable := strings.TrimPrefix(parts[0], "./")
	if executable != "multica" && !strings.HasSuffix(executable, "/multica") {
		return false
	}
	return parts[1] == "issue" && parts[2] == "comment" && parts[3] == "add"
}

// trimLeadingEnvAssignments drops leading `KEY=VALUE` tokens so an invocation
// like `MULTICA_TOKEN=x multica issue comment add ...` is still recognized.
func trimLeadingEnvAssignments(parts []string) []string {
	for len(parts) > 0 && isEnvAssignment(parts[0]) {
		parts = parts[1:]
	}
	return parts
}

// isEnvAssignment reports whether tok is a `NAME=value` shell env assignment.
func isEnvAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := tok[i]
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// isPOSIXShellName reports whether tok names a shell that takes `-c <command>`.
func isPOSIXShellName(tok string) bool {
	if i := strings.LastIndexByte(tok, '/'); i >= 0 {
		tok = tok[i+1:]
	}
	switch tok {
	case "sh", "bash", "zsh", "dash":
		return true
	default:
		return false
	}
}

func kiroToolNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}

	if idx := strings.Index(t, ":"); idx > 0 {
		t = strings.TrimSpace(t[:idx])
	}

	lower := strings.ToLower(t)
	switch lower {
	case "read", "read file":
		return "read_file"
	case "write", "write file":
		return "write_file"
	case "edit", "patch":
		return "edit_file"
	case "shell", "bash", "terminal", "run command", "run shell command":
		return "terminal"
	case "grep", "search", "find":
		return "search_files"
	case "glob":
		return "glob"
	case "code":
		return "code"
	case "web search":
		return "web_search"
	case "fetch", "web fetch":
		return "web_fetch"
	case "todo", "todo write", "todo list", "todo_list":
		return "todo_write"
	}

	return strings.ReplaceAll(lower, " ", "_")
}
