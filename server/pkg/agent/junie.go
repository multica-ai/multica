package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// junieReaderDrainGrace bounds the wait for notifications emitted immediately
// after session/prompt completes. Junie can resolve the JSON-RPC request before
// the last session/notification is flushed to stdout, so stopping at the RPC
// response would occasionally lose final text or usage. It is a variable so
// tests can shorten it without slowing the package suite.
var junieReaderDrainGrace = 2 * time.Second

// junieBlockedArgs contains flags whose values are owned by the ACP adapter.
// Letting custom_args override any of these could switch stdout away from
// JSON-RPC, inject a second prompt, or make Junie resume a different session
// from the one Multica records. Other Junie flags remain available so the
// common per-agent custom_args escape hatch keeps working.
var junieBlockedArgs = map[string]blockedArgMode{
	"--acp":              blockedWithValue,
	"--task":             blockedWithValue,
	"--prompt":           blockedWithValue,
	"--resume":           blockedWithValue,
	"--session-id":       blockedWithValue,
	"--input-format":     blockedWithValue,
	"--output-format":    blockedWithValue,
	"--json-output-file": blockedWithValue,
	"--gateway":          blockedWithValue,
	"--gateway-url":      blockedWithValue,
}

// junieBackend implements Backend by spawning `junie --acp=true` and speaking
// the standard ACP JSON-RPC 2.0 protocol over stdin/stdout.
//
// Authentication and provider configuration deliberately remain owned by
// Junie. The child inherits JUNIE_API_KEY and Junie's user configuration, which
// includes JetBrains Account, BYOK, and local-model endpoints. This adapter does
// not invoke Junie's interactive authentication method: doing so could open a
// browser in an unattended daemon run and would make Multica responsible for a
// credential lifecycle it does not store.
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

	// Convert Multica's persisted MCP object into ACP's session-level array.
	// Malformed configuration fails before process launch instead of silently
	// starting Junie without the servers the user selected.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("junie: invalid mcp_config: %w", err)
	}

	runCtx, cancel := runContext(ctx, opts.Timeout)
	// Junie accepts both "--acp true" and "--acp=true". Keep the latter as one
	// argv token so custom-argument filtering cannot separate the flag from its
	// value and so launch/discovery use the exact same transport entry point.
	junieArgs := append([]string{"--acp=true"}, filterCustomArgs(opts.CustomArgs, junieBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, junieArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(junieArgs))
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
	// Stderr must be copied explicitly rather than attached only as a writer:
	// stderrDone is the join point that guarantees provider diagnostics have
	// reached the sniffer before a nominal end_turn is promoted or finalized.
	providerErr := newACPProviderErrorSniffer("junie")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("junie stderr pipe: %w", err)
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start junie: %w", err)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[junie:stderr] "), providerErr), stderr)
	}()

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	// Junie uses the same AgentMessageChunk shape for intermediate narration
	// and the deliverable answer. The shared tracker retains the post-tool-call
	// answer for Result.Output while messages continue streaming to clients.
	var deliverable acpDeliverableTracker
	// session/new and session/load may replay stored notifications. Keep the
	// gate closed until configuration is complete so old transcript entries do
	// not appear as output from the new Multica turn.
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
				msg.Tool = kimiToolNameFromTitle(msg.Tool)
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

	// One scanner owns stdout for the entire process. hermesClient correlates
	// responses with pending requests and handles Junie's camel-case ACP update
	// variants, permission requests, attachments, thinking, tools, and usage.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				c.handleLine(line)
			}
		}
		c.closeAllPending(fmt.Errorf("junie process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			_ = stdin.Close()
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError, sessionID string
		// Only a confirmed stale ACP session sets ResumeRejected. Authentication,
		// transport, and model errors are not recoverable by silently discarding
		// the user's conversation, so they must preserve the original failure.
		var resumeRejected bool
		effectiveModel := strings.TrimSpace(opts.Model)
		resultNow := func() Result {
			return Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), SessionID: sessionID, ResumeRejected: resumeRejected}
		}

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion":    1,
			"clientInfo":         map[string]any{"name": "multica-agent-sdk", "version": "0.2.0"},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("junie initialize failed: %v", err)
			resCh <- resultNow()
			return
		}
		// A Junie build may advertise only stdio MCP. Sending an unsupported
		// HTTP/SSE entry makes the whole session creation fail, so filter remote
		// transports against the capabilities returned by this exact process.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "junie", b.cfg)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}
		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			// Junie exposes ACP loadSession rather than a CLI --resume path. MCP is
			// still supplied on load so Multica-managed servers remain available
			// when a task continues in another daemon process.
			sessionResult, err = c.request(runCtx, "session/load", map[string]any{
				"cwd": cwd, "sessionId": opts.ResumeSessionID, "mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("junie session/load failed: %v", err)
				resumeRejected = isACPSessionNotFound(err)
				resCh <- resultNow()
				return
			}
			sessionID, _ = resolveResumedSessionID(opts.ResumeSessionID, sessionResult)
		} else {
			sessionResult, err = c.request(runCtx, "session/new", map[string]any{"cwd": cwd, "mcpServers": mcpServers})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("junie session/new failed: %v", err)
				resCh <- resultNow()
				return
			}
			sessionID = extractACPSessionID(sessionResult)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "junie session/new returned no session ID"
				resCh <- resultNow()
				return
			}
		}
		c.sessionID = sessionID
		b.cfg.Logger.Info("junie session created", "session_id", sessionID)
		// Persist the ACP session as soon as it exists. If the daemon terminates
		// during model setup or prompting, the next attempt can still resume it.
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		if effectiveModel == "" {
			effectiveModel = extractACPCurrentModelID(sessionResult)
		}

		if opts.Model != "" {
			// Junie 26.8.31 advertises the model as a config option and currently
			// rejects ACP session/set_model. Treat option values as opaque strings:
			// custom local-provider IDs contain delimiters that Multica must never
			// parse, normalize, or reconstruct.
			modelResult, modelErr := c.request(runCtx, "session/set_config_option", map[string]any{
				"sessionId": sessionID, "configId": "model", "value": opts.Model,
			})
			if modelErr != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("junie could not switch to model %q: %v", opts.Model, modelErr)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(modelErr) {
					sessionID = ""
					resumeRejected = true
				}
				resCh <- resultNow()
				return
			}
			// Junie returns refreshed, model-dependent options from this call.
			// Effort vocabularies can differ by model, so the effort request below
			// must inspect this response instead of the stale session/new catalog.
			sessionResult = modelResult
			effectiveModel = opts.Model
		}
		applyACPEffortOption(runCtx, c.request, "junie", b.cfg.Logger, sessionID, sessionResult, opts.ThinkingLevel, true)

		// Do not append opts.SystemPrompt here. Junie consumes repository-level
		// AGENTS.md generated by execenv; duplicating it in every ACP prompt would
		// waste context and can produce conflicting instruction precedence.
		// hermesClient answers standard ACP permission requests, so there is also
		// no need to rewrite the user's persistent Junie brave_mode setting.
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    []map[string]any{{"type": "text", "text": prompt}},
		})
		if err != nil {
			switch runCtx.Err() {
			case context.DeadlineExceeded:
				finalStatus, finalError = "timeout", fmt.Sprintf("junie timed out after %s", opts.Timeout)
			case context.Canceled:
				finalStatus, finalError = "aborted", "execution cancelled"
			default:
				finalStatus, finalError = "failed", fmt.Sprintf("junie session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				switch pr.stopReason {
				case "", "end_turn":
				case "cancelled":
					finalStatus, finalError = "aborted", "junie cancelled the prompt"
				case "error":
					finalStatus, finalError = "failed", "junie ended the prompt with stopReason=error"
				default:
					finalStatus, finalError = "failed", fmt.Sprintf("junie returned unsupported stopReason %q", pr.stopReason)
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, junieReaderDrainGrace)
		}

		duration := time.Since(startTime)
		// Stop input and cancel the owned process group before joining both pipe
		// readers. This covers timeout/cancellation as well as grandchildren Junie
		// may start for local model gateways or tools, without leaving late
		// notifications racing the closed result channels.
		_ = stdin.Close()
		cancel()
		<-readerDone
		<-stderrDone
		streamingCurrentTurn.Store(false)
		finalOutput, providerErrorOutput := deliverable.result()
		// Some provider/authentication failures are printed only to stderr or as
		// streamed text while ACP still returns end_turn. Promote those runs to a
		// failure after all output has been joined so Multica never records a
		// false successful completion.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		// Usage is keyed by the model actually selected for this session. Keep an
		// explicit unknown bucket when Junie reports tokens without exposing a
		// current model rather than dropping valid accounting data.
		var usageMap map[string]TokenUsage
		if usage := c.accumulatedUsage(); acpUsagePresent(usage) {
			if effectiveModel == "" {
				effectiveModel = "unknown"
			}
			usageMap = map[string]TokenUsage{effectiveModel: usage}
		}
		resCh <- Result{Status: finalStatus, Output: finalOutput, Error: finalError, DurationMs: duration.Milliseconds(), SessionID: sessionID, ResumeRejected: resumeRejected, Usage: usageMap}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
