package agent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// dimBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport; overriding it
// would break the daemon↔Dim communication contract. `--auth-setup`
// and `--remote` switch the CLI into modes that never start the ACP
// server (interactive login / removed remote flow).
var dimBlockedArgs = map[string]blockedArgMode{
	"acp":         blockedStandalone,
	"--auth-setup": blockedStandalone,
	"--remote":    blockedStandalone,
	"--help":      blockedStandalone,
	"-h":          blockedStandalone,
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
//     Multica agents can do real work. Model override is handled through
//     the standard `session/set_model` RPC; the model catalog is
//     advertised by `session/new` under models.availableModels /
//     configOptions.
//   - Tool names are already lowercase ("write", "read", "exec"); the
//     shared kimiToolNameFromTitle normalisation keeps the UI identifiers
//     stable.
type dimBackend struct {
	cfg Config
}

var (
	dimReaderDrainGrace      = 2 * time.Second
	dimNotificationQuietTime = 250 * time.Millisecond
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
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

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
				// Re-normalise tool titles the same way kimi/traecli do so the
				// UI sees consistent snake_case names ("write" → "write_file").
				msg.Tool = kimiToolNameFromTitle(msg.Tool)
			}
			deliverable.observe(msg)
			msgStream.send(msg)
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
		c.closeAllPending(fmt.Errorf("dim process exited"))
	}()

	go func() {
		defer cancel()
		defer msgStream.close()
		defer close(resCh)
		defer func() {
			stdin.Close()
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

		// Dim sessions are bound to the process that created them: a later
		// process attempting session/load is rejected with "held by another
		// process" even after a clean session/close (verified experimentally).
		// Multica spawns a fresh `dim acp` process per execution, so resuming
		// across tasks is impossible for this runtime. Always start a fresh
		// session instead of attempting session/load, and tell the daemon the
		// requested resume did not happen so it classifies the run correctly
		// (ResumeRejected is only consulted on failed runs).
		if opts.ResumeSessionID != "" {
			b.cfg.Logger.Warn("dim cannot resume sessions across processes (session is bound to the creating process); starting a fresh session",
				"backend", "dim",
				"requested_session", opts.ResumeSessionID,
			)
			resumeRejected = true
		}

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

		c.sessionID = sessionID
		// Early session pin so a cancelled run still preserves resume pointer.
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("dim session created", "session_id", sessionID)

		// Dim's ACP server hardcodes a read-only permission preset when the
		// session is created, which would silently deny file writes and
		// process spawns. Raise it to full-access for this session and pin
		// the agent mode so the headless task can do real work. Both calls
		// are best-effort but hard-required: if either fails we abort rather
		// than run a turn that is guaranteed to fail.
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

		streamingCurrentTurn.Store(true)
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
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "dim cancelled the prompt"
				}
				if effectiveModel == "" {
					effectiveModel = pr.modelID
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens
				c.usageMu.Unlock()
			default:
			}
			waitForDimNotificationQuiescence(runCtx, activity, readerDone)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("dim finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Best-effort session/close so Dim does not accumulate orphaned
		// sessions in its own session list (each multica task leaves one
		// otherwise). Cross-process resume stays impossible either way.
		if sessionID != "" {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			if _, err := c.request(closeCtx, "session/close", map[string]any{
				"sessionId": sessionID,
			}); err != nil {
				b.cfg.Logger.Debug("dim session/close failed (ignored)", "session_id", sessionID, "error", err)
			}
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
		streamingCurrentTurn.Store(false)

		finalOutput, providerErrorOutput := deliverable.result()

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (auth / rate-limit / HTTP 4xx). It reads
		// the full text stream, not the deliverable, so a give-up turn that
		// lands before a tool call stays visible.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		c.usageMu.Lock()
		u := c.accumulatedUsage()
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
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

// waitForDimNotificationQuiescence gives the ACP stdout reader a bounded
// chance to consume notifications that Dim may emit just after the
// session/prompt response. Without this window, cancelling the process at the
// response boundary can truncate the final text or usage update.
func waitForDimNotificationQuiescence(ctx context.Context, activity <-chan struct{}, readerDone <-chan struct{}) {
	waitForACPNotificationQuiescence(ctx, activity, readerDone, dimNotificationQuietTime, dimReaderDrainGrace)
}
