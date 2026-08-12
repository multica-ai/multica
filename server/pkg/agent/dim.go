package agent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

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
//     settings across `session/load`, so the config block runs only on fresh
//     sessions. Model override is handled through the standard
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
)

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

	cmd := exec.CommandContext(runCtx, execPath, dimArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", dimArgs)
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

	if err := cmd.Start(); err != nil {
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

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
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
			cancel()
			stdin.Close()
			waitDone := make(chan struct{})
			go func() { _ = cmd.Wait(); close(waitDone) }()
			select {
			case <-waitDone:
			case <-time.After(dimProcessWaitTimeout):
				// The child did not exit on cancellation+EOF within the
				// grace window; force-kill it so the goroutine and its
				// pipes can be reaped rather than leaking indefinitely.
				_ = cmd.Process.Kill()
				<-waitDone
			}
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
		// full-access session stays full-access after load), so the
		// set_config_option block below runs only for freshly created
		// sessions.
		var freshSession bool
		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
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

		// Dim's ACP server hardcodes a read-only permission preset when a
		// session is created, which would silently deny file writes and
		// process spawns. Raise it to full-access for this session and pin
		// the agent mode so the headless task can do real work. A resumed
		// session already carries these settings (verified: full-access
		// survives session/load), so the block is skipped on resume. Both
		// calls are hard-required: if either fails we abort rather than run
		// a turn that is guaranteed to fail.
		if freshSession {
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
					resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), SessionID: sessionID, ResumeRejected: resumeRejected}
					return
				}
				b.cfg.Logger.Info("dim session config set", "config", cfgOpt.id, "value", cfgOpt.value, "session_id", sessionID)
			}
		}

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("dim set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("dim could not switch to model %q: %v", opts.Model, err)
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
			// session/prompt may return before the final prompt notification
			// arrives over the stdout stream. A non-blocking read here would
			// race the stdin.Close()+cancel() below and lose the last
			// assistant message / usage. Wait for the notification with a
			// bounded grace window instead, then drain the pipes.
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
			case <-time.After(dimNotificationQuietTime):
				// The runtime did not emit a terminal notification within
				// the quiet window; proceed with whatever the deliverable
				// captured so far rather than blocking indefinitely.
			}
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
