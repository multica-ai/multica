package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// dimBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport; overriding it
// would break the daemon↔Dim communication contract. `--auth-setup`
// and `--remote` switch the CLI into modes that never start the ACP
// server (interactive login / removed remote flow).
var dimBlockedArgs = map[string]blockedArgMode{
	"acp":          blockedStandalone,
	"--auth-setup": blockedStandalone,
	"--remote":     blockedStandalone,
	"--help":       blockedStandalone,
	"-h":           blockedStandalone,
}

// dimBackend implements Backend by spawning `dim acp` and
// communicating via the standard ACP (Agent Client Protocol) JSON-RPC 2.0
// over stdin/stdout.
//
// Dim (dimcode) exposes its agent loop over ACP through the `dim acp`
// subcommand. The protocol surface matches the shared hermesClient ACP
// transport used by Hermes/Kimi/Kiro/Traecli/QwenPaw — only the binary,
// the session bootstrap, and the tool-name extraction differ.
//
// Notable contract with Dim's ACP server:
//   - `initialize` advertises no authMethods, so no `authenticate` step is
//     needed; the CLI uses the user's existing Dim OAuth login.
//   - `session/new` requires an absolute cwd (relative paths are rejected).
//   - The ACP server hardcodes a read-only permission preset at session
//     creation, which would silently deny every file write and process
//     spawn. The backend therefore issues `session/set_config_option`
//     (permission → full-access, mode → agent) right after session/new so
//     Multica agents can do real work. A resumed session retains these
//     settings across `session/load`, so set_config_option is re-applied on
//     both fresh and resumed sessions (idempotent). Model override is handled through the standard
//     `session/set_model` RPC; the model catalog is advertised by
//     `session/new` under models.availableModels / configOptions.
//   - `session/load` resumes a prior session across processes: dim 0.3.10+
//     releases its per-process session lock within ~5s of the owning process
//     exiting, so a follow-up run in a fresh `dim acp` process can continue
//     the conversation. Earlier 0.3.x builds bound sessions to the creating
//     process permanently; on those the load fails and ResumeRejected lets
//     the daemon retry fresh.
//   - Tool names are already lowercase ("write", "read", "exec"); the
//     shared kimiToolNameFromTitle normalisation keeps the UI identifiers
//     stable.
type dimBackend struct {
	cfg Config
}

var (
	dimReaderDrainGrace      = 2 * time.Second
	dimNotificationQuietTime = 250 * time.Millisecond
	// dimProcessWaitTimeout bounds how long the deferred cleanup waits for
	// the child to exit after cancel+stdin-close before force-killing it. A
	// child that ignores both should not be able to hang Result delivery.
	dimProcessWaitTimeout = 5 * time.Second
	// dimSessionCloseTimeout bounds the best-effort session/close sent before
	// tearing down the process. It is deliberately short so a stuck close
	// cannot delay Result delivery; the next run's session/load is still safe
	// because dim releases the lock on its own after ~5s.
	dimSessionCloseTimeout = 2 * time.Second
	// dimSessionLoadRetryAttempts bounds the number of retries when
	// session/load reports "held by another process" — the prior process's
	// lock has not been released yet. With graceful session/close this is
	// rare (the lock releases immediately), but a force-killed process can
	// take up to ~5s. The retry loop covers that window instead of silently
	// starting a fresh session and losing the conversation.
	dimSessionLoadRetryAttempts = 3
	dimSessionLoadRetryDelay    = 2 * time.Second
)

// dimMinVersion is the minimum dim version that supports cross-run session
// resume via session/load. Earlier builds bind sessions to the creating
// process permanently.
const dimMinVersion = "0.3.10"

// extractACPAgentVersion pulls agentInfo.version out of an initialize
// response. Returns "" if the field is absent (e.g. an older runtime that
// does not report it).
func extractACPAgentVersion(result json.RawMessage) string {
	var resp struct {
		AgentInfo struct {
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return ""
	}
	return resp.AgentInfo.Version
}

// dimVersionSupported reports whether the dim version meets the minimum
// required for cross-run session resume. Uses semver-ish comparison
// (major.minor.patch). Fail-closed: an empty or non-parseable version is
// rejected (returns false) rather than assumed supported, so a runtime that
// cannot prove it is >= 0.3.10 is blocked from session resume (review #3).
func dimVersionSupported(version string) bool {
	version = strings.TrimPrefix(version, "v")
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false // unparseable — fail-closed, do not assume supported
	}
	var maj, min, pat int
	if _, err := fmt.Sscanf(parts[0], "%d", &maj); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &min); err != nil {
		return false
	}
	// patch may have a suffix (e.g. "10-beta"); Sscanf stops at the first
	// non-digit, which is what we want.
	if _, err := fmt.Sscanf(parts[2], "%d", &pat); err != nil {
		return false
	}
	var reqMaj, reqMin, reqPat int
	fmt.Sscanf(dimMinVersion, "%d.%d.%d", &reqMaj, &reqMin, &reqPat)
	if maj != reqMaj {
		return maj > reqMaj
	}
	if min != reqMin {
		return min > reqMin
	}
	return pat >= reqPat
}

// dimMessageStream serializes sends and the final close so a late stdout
// reader cannot send on a closed channel. Mirrors grok/traecli/qoder.
type dimMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newDimMessageStream(size int) *dimMessageStream {
	return &dimMessageStream{ch: make(chan Message, size)}
}

func (s *dimMessageStream) send(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, msg)
}

func (s *dimMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *dimBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "dim"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("dim executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects) into
	// the array shape ACP session/new and session/load expect. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of silently
	// dropping every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("dim: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	dimArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, dimBlockedArgs, b.cfg.Logger)...)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, dimArgs...)
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)
	// Take over context cancellation: the default would SIGKILL only the
	// leader the instant runCtx is done, orphaning descendants. We instead
	// drive a group-wide SIGKILL from the deferred cleanup below. Returning
	// nil keeps os/exec from racing us with its own kill.
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = 10 * time.Second
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(dimArgs, trustAgentCommandPositional(0, "acp")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("dim stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("dim stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point (`stderrDone`) that
	// fires before the failure-promotion decision; see hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("dim")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("dim stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start dim: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[dim:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("dim acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newDimMessageStream(256)
	resCh := make(chan Result, 1)

	// Dim streams interim narration and the final answer as the same
	// agent_message_chunk type; the tracker keeps only the post-tool-call
	// block for Result.Output while retaining the full text for error
	// detection.
	var deliverable acpDeliverableTracker

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		onActivity: func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
			if msg.Type == MessageToolUse {
				// Re-normalise tool titles the same way kimi/traecli do so the
				// UI sees consistent snake_case names ("write" → "write_file").
				msg.Tool = kimiToolNameFromTitle(msg.Tool)
			}
			deliverable.observe(msg)
			msgStream.send(msg)
		},
		onPromptDone: func(result hermesPromptResult) {
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
		c.closeAllPending(fmt.Errorf("dim process exited"))
	}()

	go func() {
		defer cancel()
		defer msgStream.close()
		defer close(resCh)
		// Cleanup ordering matters: cancel the run context first so
		// exec.CommandContext kills the child if it is still alive, THEN
		// close stdin and wait. The previous order (stdin.Close → cmd.Wait →
		// cancel) left cancel() unreachable when a child ignored stdin EOF
		// and never exited, hanging cmd.Wait() forever and never closing
		// resCh. cmd.Wait is bounded so a still-stuck child cannot block the
		// final Result either.
		defer func() {
			// Signal the entire process group (the agent CLI plus any tool
			// subprocesses it spawned) BEFORE closing stdin. exec.
			// CommandContext's cancel is disabled (cmd.Cancel returns nil)
			// so we own all signalling. If we close stdin first, the shell
			// exits on EOF and its process group dissolves before we can
			// SIGKILL the descendants, orphaning them to PPID=1.
			signalProcessGroup(cmd, syscall.SIGKILL)
			cancel()
			stdin.Close()
			waitDone := make(chan struct{})
			go func() { _ = cmd.Wait(); close(waitDone) }()
			select {
			case <-waitDone:
			case <-time.After(dimProcessWaitTimeout):
			}
			waitProcessGroupGone(cmd, dimProcessWaitTimeout)
			// Release the Windows Job Object ownership handle (no-op on Unix)
			// so the OS can finalize any processes that were attached to it.
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
			finalError = fmt.Sprintf("dim initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Require dim >= 0.3.10 (fail-closed): earlier builds bind sessions to
		// the creating process permanently, so cross-run session/load fails with
		// "held by another process" and follow-up runs lose context. 0.3.10
		// releases the lock on graceful exit (session/close) or after ~5s. A
		// version that is empty or cannot be parsed is rejected rather than
		// assumed supported, so a runtime that cannot prove it is >= 0.3.10 is
		// blocked from session resume (review #3).
		dimVersion := extractACPAgentVersion(initResult)
		if !dimVersionSupported(dimVersion) {
			finalStatus = "failed"
			if dimVersion == "" {
				finalError = "dim did not report an agent version: cross-run session resume requires dim >= 0.3.10 (0.3.10+ releases the per-process session lock on exit); please upgrade dimcode"
			} else {
				finalError = fmt.Sprintf("dim %s is too old: cross-run session resume requires dim >= 0.3.10 (0.3.10+ releases the per-process session lock on exit); please upgrade dimcode", dimVersion)
			}
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't advertise.
		// See hermes.go for why sending an unsupported transport tanks session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "dim", b.cfg)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		// Dim (dimcode 0.3.10+) releases its per-process session lock within
		// ~5s of the owning process exiting (graceful close or SIGTERM), so a
		// follow-up run in a fresh `dim acp` process can resume the prior
		// session via the standard ACP `session/load`. Earlier 0.3.x builds
		// bound sessions to the creating process permanently; on those the
		// load fails with "held by another process" and ResumeRejected lets
		// the daemon retry on a fresh session.
		//
		// A loaded session retains its permission/mode config (verified: a
		// full-access session stays full-access after load), but
		// set_config_option is re-applied on both fresh and resumed sessions
		// to guard against a partially configured session being resumed
		// (review #4).
		var freshSession bool
		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			// Retry session/load when the prior process's lock has not been
			// released yet ("held by another process"). With graceful
			// session/close the lock releases immediately, but a force-killed
			// process can take up to ~5s. Retry instead of silently starting
			// a fresh session and losing the conversation (review #1).
			var result json.RawMessage
			var loadErr error
		loadRetry:
			for attempt := 0; attempt <= dimSessionLoadRetryAttempts; attempt++ {
				result, loadErr = c.request(runCtx, "session/load", map[string]any{
					"cwd":        cwd,
					"sessionId":  opts.ResumeSessionID,
					"mcpServers": mcpServers,
				})
				if loadErr == nil {
					break
				}
				if isACPSessionNotFound(loadErr) {
					break // session is gone — no point retrying
				}
				if !isACPHeldByProcess(loadErr) {
					break // different error — let the normal failure path handle it
				}
				if attempt < dimSessionLoadRetryAttempts {
					b.cfg.Logger.Warn("dim session/load: lock not yet released, retrying",
						"backend", "dim",
						"attempt", attempt+1,
						"delay", dimSessionLoadRetryDelay.String(),
					)
					select {
					case <-time.After(dimSessionLoadRetryDelay):
					case <-runCtx.Done():
						loadErr = fmt.Errorf("dim session/load cancelled: %w", runCtx.Err())
						break loadRetry
					}
				}
			}
			err := loadErr
			if err != nil {
				if isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("dim resumed session not found; the daemon will retry fresh",
						"backend", "dim",
						"requested_session", opts.ResumeSessionID,
					)
					resumeRejected = true
					resCh <- Result{Status: "failed", Error: fmt.Sprintf("dim session/load: %v", err), DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
					return
				}
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("dim timed out during session/load: %v", timeout)
				} else if runCtx.Err() == context.Canceled {
					finalStatus = "aborted"
					finalError = fmt.Sprintf("dim aborted: %v", err)
				} else {
					finalStatus = "failed"
					finalError = fmt.Sprintf("dim session/load failed: %v", err)
				}
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			sessionResult = result
			if changed {
				b.cfg.Logger.Warn("dim returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "dim",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		} else {
			freshSession = true
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("dim timed out during session/new: %v", timeout)
				} else if runCtx.Err() == context.Canceled {
					finalStatus = "aborted"
					finalError = fmt.Sprintf("dim aborted: %v", err)
				} else {
					finalStatus = "failed"
					finalError = fmt.Sprintf("dim session/new failed: %v", err)
				}
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			sessionID = extractACPSessionID(result)
			sessionResult = result
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "dim session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		// Early session pin so a cancelled run still preserves resume pointer.
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("dim session ready", "session_id", sessionID, "resumed", !freshSession)

		// Dim's ACP server hardcodes a read-only permission preset at session
		// creation. Raise it to full-access and pin agent mode. This runs on
		// BOTH fresh and resumed sessions (review #4): a resumed session whose
		// first run failed partway through config would carry a half-configured
		// state into the resume. set_config_option is idempotent. On failure we
		// close the session and abort.
		for _, cfgOpt := range []struct {
			id    string
			value string
		}{
			{"permission", "full-access"},
			{"mode", "agent"},
		} {
			if _, err := c.request(runCtx, "session/set_config_option", map[string]any{
				"sessionId": sessionID,
				"configId":  cfgOpt.id,
				"value":     cfgOpt.value,
			}); err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("dim could not set session config %s=%s: %v", cfgOpt.id, cfgOpt.value, err)
				// Best-effort close so a partially configured session is not
				// left behind for the next resume to inherit.
				closeCtx, closeCancel := context.WithTimeout(context.Background(), dimSessionCloseTimeout)
				_, _ = c.request(closeCtx, "session/close", map[string]any{"sessionId": sessionID})
				closeCancel()
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), SessionID: sessionID, ResumeRejected: resumeRejected}
				return
			}
			b.cfg.Logger.Info("dim session config set", "config", cfgOpt.id, "value", cfgOpt.value, "session_id", sessionID)
		}

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("dim set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("dim could not switch to model %q: %v", opts.Model, err)
				// Close the session so a partially configured one (permission/mode
				// set, model not) is not left for the next resume to inherit.
				closeCtx, closeCancel := context.WithTimeout(context.Background(), dimSessionCloseTimeout)
				_, _ = c.request(closeCtx, "session/close", map[string]any{"sessionId": sessionID})
				closeCancel()
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					SessionID:      sessionID,
					ResumeRejected: resumeRejected,
				}
				return
			}
			b.cfg.Logger.Info("dim session model set", "model", opts.Model)
		}

		// Apply the thinking level (if any) via the ACP session config. Dim
		// advertises a `thought_level` configOption in session/new (auto/high/
		// max), so it joins the acpCatalogThinkingProviders list. The effort
		// option may depend on the current model, so apply it after set_model.
		applyACPEffortOption(runCtx, c.request, "dim", b.cfg.Logger,
			sessionID, sessionResult, opts.ThinkingLevel, opts.Model == "")

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("dim timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("dim session/prompt failed: %v", err)
			}
		} else {
			// Check if we got a promptDone result from the response parsing
			// (non-blocking — the response is parsed synchronously during
			// c.request, so promptDone is usually already ready).
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "dim cancelled the prompt"
				}
				if effectiveModel == "" {
					effectiveModel = pr.modelID
				}
				c.mergeUsage(pr.usage)
			default:
			}
			// Give the stdout reader a bounded chance to consume notifications
			// dim may emit just after session/prompt returns (agent_message_chunk,
			// usage updates). Closing stdin at the response boundary otherwise
			// races the reader and truncates the final text — the same race
			// fixed for hermes (TestHermesBackendDrainsLateFinalNotification).
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, dimNotificationQuietTime, dimReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("dim finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Best-effort session/close before tearing down the process: it lets
		// dim release the per-process session lock promptly (the next run can
		// resume immediately) instead of waiting for the 5s idle-release
		// timeout. Bounded and best-effort — a miss just means the next run
		// waits up to 5s for the lock.
		if sessionID != "" {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), dimSessionCloseTimeout)
			_, _ = c.request(closeCtx, "session/close", map[string]any{"sessionId": sessionID})
			closeCancel()
		}

		stdin.Close()
		cancel()

		// Dim's ACP server may keep the process — and the stdout/stderr
		// pipes — open briefly after session/prompt returns. Bound the drain.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), dimReaderDrainGrace)
		select {
		case <-readerDone:
		case <-drainCtx.Done():
		}
		select {
		case <-stderrDone:
		case <-drainCtx.Done():
		}
		drainCancel()

		finalOutput, providerErrorOutput := deliverable.result()

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (auth / rate-limit / HTTP 4xx). It reads
		// the full text stream, not the deliverable, so a give-up turn that
		// lands before a tool call stays visible.
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

	return &Session{Messages: msgStream.ch, Result: resCh}, nil
}
