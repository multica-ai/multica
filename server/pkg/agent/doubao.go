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

// doubaoBlockedArgs are flags/subcommands hardcoded by the daemon that must not
// be overridden by user-configured custom_args. `acp serve` selects the ACP
// transport (its `--acp` alias is blocked too); `--permission-mode
// bypass_permissions` is daemon-owned so a headless Multica run never stops on
// an interactive approval prompt. `--effort` and `--model` are managed by the
// daemon (thinking level / agent model), and `-C`/`--cwd` are managed through
// ExecOptions.Cwd. Re-issuing any of these from custom_args would break the
// daemon↔doubao ACP contract.
var doubaoBlockedArgs = map[string]blockedArgMode{
	"acp":               blockedStandalone,
	"serve":             blockedStandalone,
	"--acp":             blockedStandalone,
	"--headless":        blockedStandalone,
	"-p":                blockedStandalone,
	"--print":           blockedStandalone,
	"--permission-mode": blockedWithValue,
	"--effort":          blockedWithValue,
	"--model":           blockedWithValue,
	"-m":                blockedWithValue,
	"-C":                blockedWithValue,
	"--cwd":             blockedWithValue,
	"--resume":          blockedWithValue,
	"--continue":        blockedStandalone,
	"-c":                blockedStandalone,
	"--session-id":      blockedWithValue,
}

// doubaoBackend implements Backend by spawning
//
//	doubao acp serve --permission-mode bypass_permissions [--effort <level>] [--model <id>]
//
// and speaking the standard ACP (Agent Client Protocol) v1 JSON-RPC 2.0
// transport over stdin/stdout.
//
// Doubao CLI authenticates out of band with `doubao login`; the ACP server
// embeds that login state and advertises an empty `authMethods` list, so unlike
// grok there is no `authenticate` step between initialize and session/new. The
// initialize result advertises `agentCapabilities.loadSession: true`, so resume
// uses `session/load` (the same method grok uses), not qoder's
// `session/resume`.
//
// Thinking and model are process-level flags on this CLI (`--effort`,
// `--model`), so both are applied at launch. The backend deliberately does not
// call `session/set_model`: this CLI's ACP server is not documented to
// implement it, and the launch flag already selects the model for the process.
// If a future build advertises a session-scoped model API, add it here and to
// ModelSelectionSupported's lifecycle.
type doubaoBackend struct {
	cfg Config
}

var (
	doubaoReaderDrainGrace      = 2 * time.Second
	doubaoNotificationQuietTime = 250 * time.Millisecond
)

// doubaoMessageStream serializes sends and the final close so a late stdout
// reader cannot send on a closed channel. Mirrors qoder/grok.
type doubaoMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newDoubaoMessageStream(size int) *doubaoMessageStream {
	return &doubaoMessageStream{ch: make(chan Message, size)}
}

func (s *doubaoMessageStream) send(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, msg)
}

func (s *doubaoMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *doubaoBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "doubao"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("doubao executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects) into
	// the array shape ACP session/new and session/load expect. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of silently
	// dropping every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("doubao: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// `acp serve` is the documented ACP entry point (`--acp` is an alias).
	// `--permission-mode bypass_permissions` is daemon-owned: a headless run
	// must not stop on an interactive approval prompt. Thinking and model are
	// process-level flags on this CLI, so they are applied here rather than
	// through session/set_model.
	doubaoArgs := []string{"acp", "serve", "--permission-mode", "bypass_permissions"}
	if level := strings.TrimSpace(opts.ThinkingLevel); level != "" {
		doubaoArgs = append(doubaoArgs, "--effort", level)
	}
	doubaoArgs = append(doubaoArgs, filterCustomArgs(opts.CustomArgs, doubaoBlockedArgs, b.cfg.Logger)...)
	if model := strings.TrimSpace(opts.Model); model != "" {
		doubaoArgs = append(doubaoArgs, "--model", model)
	}

	cmd := b.cfg.commandAt(execPath).exec(runCtx, doubaoArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(doubaoArgs,
		trustAgentCommandPositional(0, "acp"),
		trustAgentCommandPositional(1, "serve"),
	))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childEnv := buildEnv(b.cfg.Env)
	cmd.Env = childEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("doubao stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("doubao stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point (`stderrDone`) that
	// fires before the failure-promotion decision; see hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("doubao")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("doubao stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start doubao: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[doubao:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("doubao acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newDoubaoMessageStream(256)
	resCh := make(chan Result, 1)

	// Doubao streams interim narration and the final answer as the same ACP
	// agent_message_chunk type; the tracker keeps only the post-tool-call block
	// for Result.Output while retaining the full text for error detection.
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
				// Re-normalise capitalised titles ("Read file: …") the same way
				// kimi/traecli do so the UI sees consistent snake_case names.
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
		c.closeAllPending(fmt.Errorf("doubao process exited"))
	}()

	go func() {
		defer cancel()
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
			finalError = fmt.Sprintf("doubao initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Doubao embeds `doubao login` state in the ACP channel and advertises
		// `authMethods: []`, so there is no authenticate step. If a future
		// build starts advertising methods, the session ops below surface the
		// real error instead of the daemon guessing a method id.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "doubao", b.cfg)

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
					runCtx, "doubao", "session/load", err, timeout, b.cfg.Logger)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "doubao",
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
				finalError = fmt.Sprintf("doubao session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "doubao session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("doubao session created", "session_id", sessionID)

		userText := prompt
		if opts.SystemPrompt != "" {
			// Doubao also reads AGENTS.md from cwd when the daemon writes it;
			// inline delivery covers the case where it does not.
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// Session pin for the daemon (PinTaskSession keys off
		// MessageStatus+SessionID), sent only once setup has succeeded and the
		// prompt is about to go out. See grok.go for the GH #8116 rationale.
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

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
				finalError = fmt.Sprintf("doubao timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("doubao session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "doubao",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				switch pr.stopReason {
				case "cancelled":
					finalStatus = "aborted"
					finalError = "doubao cancelled the prompt"
				case "max_tokens":
					finalStatus = "failed"
					finalError = "doubao reached its maximum generated tokens (max_tokens)"
				case "max_turn_requests":
					finalStatus = "failed"
					finalError = "doubao reached its maximum turn requests (max_turn_requests)"
				}
				// `session/load` carries no model id (only `session/new`
				// does), so a resumed session with no configured model would
				// otherwise bucket its whole spend under "unknown". The turn's
				// own `_meta.modelId` is authoritative; use it.
				if effectiveModel == "" {
					effectiveModel = pr.modelID
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, doubaoNotificationQuietTime, doubaoReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("doubao finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		// Doubao ACP may keep the process — and the stdout/stderr pipes — open
		// briefly after session/prompt returns. Bound the drain.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), doubaoReaderDrainGrace)
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
		// terminal upstream-LLM failure (auth / rate-limit / HTTP 4xx).
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
