package agent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// junieBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. --acp=true selects ACP mode;
// letting custom_args drop or replace it would break the daemon↔Junie
// communication contract the same way an overridden `acp` subcommand would
// for Kiro/Reasonix. Junie CLI documents this as a flag (`--acp=true`), not a
// subcommand — a different shape from Kiro's `acp --trust-all-tools` and
// Reasonix's `acp --profile ...`.
var junieBlockedArgs = map[string]blockedArgMode{
	"--acp": blockedWithValue,
}

// junieReaderDrainGrace bounds how long the turn waits for trailing ACP
// notifications after the session/prompt response. A var, not a const, so
// tests can shorten it. Mirrors kiroReaderDrainGrace / reasonixReaderDrainGrace.
var junieReaderDrainGrace = 2 * time.Second

// junieBackend implements Backend by spawning `junie --acp=true` and
// communicating via the standard ACP JSON-RPC 2.0 transport over
// stdin/stdout.
//
// JetBrains Junie CLI advertises ACP support through the --acp=true flag
// (confirmed against junie.jetbrains.com/docs/junie-cli-acp.html), unlike
// Kiro's `acp` subcommand or Reasonix's `acp` subcommand with extra flags, so
// the existing Hermes/Kimi/Kiro ACP client can drive it with only
// provider-specific launch args.
//
// Two things this adapter deliberately does NOT do, pending a live protocol
// check against an installed `junie` binary (see the issue's "Protocol
// check" step, same as the Kiro PR):
//
//  1. Permission auto-approval uses the generic kind-based
//     selectACPPermissionOption (see hermes.go) unmodified rather than a
//     Junie-specific selectPermission override. That function already grants
//     purely off the standard ACP PermissionOptionKind values
//     (allow_once/allow_always/reject_once), so it should work for any
//     ACP-compliant agent out of the box; Reasonix only needed an override
//     because it multiplexes protected decisions and free-form questions
//     through the same request_permission call, which has not been observed
//     for Junie.
//  2. No junieToolNameFromTitle mapping (contrast kiroToolNameFromTitle /
//     reasonixToolNameFromTitle): hermesClient already falls back to the
//     standard ACP ToolCallKind (read/edit/execute/search/fetch/think) when a
//     title doesn't parse, which should cover a compliant agent reasonably
//     well. Add a Junie-specific title mapper here once real tool_call
//     titles from a live session are known to need it.
type junieBackend struct {
	cfg Config
}

func (b *junieBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "junie"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("junie executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP `session/new` and `session/load` expect.
	// Fail closed on malformed JSON so the launch surfaces the real error
	// instead of silently dropping all MCP servers.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("junie: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	junieArgs := append([]string{"--acp=true"}, filterCustomArgs(opts.CustomArgs, junieBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, junieArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(junieArgs, trustAgentCommandPositional(0, "--acp=true")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("junie stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("junie stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point
	// (`stderrDone`) that fires before the failure-promotion
	// decision; see the matching comment in hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("junie")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("junie stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start junie: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[junie:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("junie acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var deliverable acpDeliverableTracker
	// streamingCurrentTurn gates all session updates so that any history
	// replay or queued notifications from a resumed session are dropped
	// instead of duplicating a previous answer into output. It flips to
	// true only after session/prompt is sent. Mirrors kiro.go / reasonix.go.
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	// Reuse the hermesClient ACP transport — Junie speaks the same protocol.
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
			deliverable.observe(msg)
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
		c.closeAllPending(fmt.Errorf("junie process exited"))
	}()

	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			// Cancellation must be reachable before Wait. A pathological child
			// can close stdout/stderr (so the pipe drain succeeds) but keep the
			// process alive; waiting first would then block until the overall
			// task timeout and make a later deferred cancel ineffective.
			cancel()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		// Set when the ACP runtime refuses the session we asked to
		// resume. Only that is curable by starting a fresh session, so
		// handshake/network failures below must leave it false.
		var resumeRejected bool
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
			finalError = fmt.Sprintf("junie initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime
		// tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "junie", b.cfg)

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
				finalStatus = "failed"
				finalError = fmt.Sprintf("junie session/load failed: %v", err)
				if isACPSessionNotFound(err) {
					// The runtime refused the session outright, so flag the
					// rejection: without it the daemon reads the zero value as
					// "checked, and this was not a rejection" and keeps
					// replaying the dead id on every later turn instead of
					// retrying from a fresh session. sessionID is still empty
					// on this path — cleared explicitly to stay in lockstep
					// with the set_model and prompt branches below.
					b.cfg.Logger.Warn("resumed session not found at session/load time; clearing session id so the daemon retries fresh",
						"backend", "junie",
						"requested_session", opts.ResumeSessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					ResumeRejected: resumeRejected,
				}
				return
			}
			// Apply the same defensive resolution kimi/hermes/kiro use: if
			// junie echoes a sessionId in the session/load response, prefer
			// it (the canonical id the backend is committed to). When the
			// response is empty or doesn't include sessionId, the helper
			// falls back to the requested id.
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "junie",
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
				finalError = fmt.Sprintf("junie session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "junie session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("junie session created", "session_id", sessionID)

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("junie set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("junie could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so
					// the daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "junie",
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
			b.cfg.Logger.Info("junie session model set", "model", opts.Model)
		}

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    promptBlocks,
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("junie timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("junie session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes/kiro backends: the runtime may echo the
					// requested id back from session/load even when the
					// session is gone, so the stale id only fails here, at
					// prompt time. Empty SessionID lets the daemon's
					// resume-failure fallback retry fresh and store the
					// replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "junie",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "junie cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, junieReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("junie finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		// Ensure the stderr copier has drained before consulting the
		// provider-error sniffer; see hermes.go for the failure mode.
		<-stderrDone

		finalOutput, providerErrorOutput := deliverable.result()

		// Promote completed→failed when stderr or the agent text
		// stream show a terminal upstream-LLM failure (HTTP 4xx /
		// rate-limit / expired token). See the helper docs for the
		// full signal set; the key safety property is that transient
		// per-attempt warnings followed by a successful retry stay
		// "completed".
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

	return &Session{Messages: msgCh, Result: resCh}, nil
}
