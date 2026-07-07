package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ompBlockedArgs are flags hardcoded by the daemon that must not be overridden
// by user-configured custom_args. `acp` is the protocol subcommand that drives
// the ACP JSON-RPC transport; `--yolo`/`--auto-approve`/`--approval-mode` are
// daemon-owned so headless ACP always runs in bypass-permissions mode (OMP
// otherwise gates bash/edit/delete/move behind an ACP client permission
// prompt); `-p`/`--print`, `--mode` (e.g. `--mode rpc`), and `--output-format`
// would switch the binary out of ACP into a different transport and break the
// daemon↔OMP communication contract.
var ompBlockedArgs = map[string]blockedArgMode{
	"acp":             blockedStandalone,
	"--yolo":          blockedStandalone,
	"--auto-approve":  blockedStandalone,
	"--approval-mode": blockedWithValue,
	"-p":              blockedStandalone,
	"--print":         blockedStandalone,
	"--mode":          blockedWithValue,
	"--output-format": blockedWithValue,
}

// ompBackend implements Backend by spawning `omp acp --yolo` and communicating
// via the standard ACP (Agent Client Protocol) JSON-RPC 2.0 transport over
// stdin/stdout.
//
// OMP (oh-my-pi) is a Pi-derived coding agent that is not wire-compatible with
// the upstream `pi` CLI's bespoke JSON stream mode, so it cannot reuse the pi
// backend. It is, however, ACP-native: launched via `omp acp`, it implements
// `initialize`, `session/new`, `session/load`, `session/resume`,
// `session/set_model`, `session/prompt`, the `session/update` notification
// family, `session/request_permission`, and `mcpServers`. It advertises
// `loadSession` and returns its model catalog from `session/new`, so the
// existing Hermes/Kimi/Kiro/Qoder/Traecli ACP client (hermesClient) drives it
// with only provider-specific launch args and tool-name normalization.
type ompBackend struct {
	cfg Config
}

func (b *ompBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "omp"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("omp executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects) into
	// the array shape ACP session/new and session/load expect. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of silently
	// dropping every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("omp: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	ompArgs := append(
		[]string{"acp", "--yolo"},
		filterCustomArgs(opts.CustomArgs, ompBlockedArgs, b.cfg.Logger)...,
	)
	cmd := exec.CommandContext(runCtx, execPath, ompArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", ompArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omp stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omp stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point (`stderrDone`)
	// that fires before the failure-promotion decision; see the matching
	// comment in hermes.go for why the io.MultiWriter form races with
	// stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("omp")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omp stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start omp: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[omp:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("omp acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		onMessage: func(msg Message) {
			if !streamingCurrentTurn.Load() {
				return
			}
			if msg.Type == MessageToolUse {
				msg.Tool = kimiToolNameFromTitle(msg.Tool)
			}
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
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
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("omp process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
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
			finalError = fmt.Sprintf("omp initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't advertise
		// in its initialize response. See hermes.go for why sending an
		// unsupported transport tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "omp", b.cfg.Logger)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			// OMP advertises loadSession, so resume goes through the standard
			// ACP session/load (same path as Kiro/Traecli). Apply the same
			// defensive id resolution hermes/kimi/kiro use: if OMP echoes a
			// different sessionId, prefer it (the canonical id the backend is
			// committed to) so a silent state reset doesn't pin us to a dead
			// id.
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("omp session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "omp",
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
				finalError = fmt.Sprintf("omp session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "omp session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("omp session created", "session_id", sessionID)

		if opts.Model != "" {
			// OMP exposes the model selector as a session config option, not
			// via session/set_model (which it rejects as "Unknown ACP ext
			// method"). Switch the model with session/set_config_option; the
			// value is a model id from the configOptions catalog (e.g.
			// "zai/glm-5.2"). Verified against omp acp 16.x.
			if _, err := c.request(runCtx, "session/set_config_option", map[string]any{
				"sessionId": sessionID,
				"configId":  "model",
				"value":     opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("omp set_config_option failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("omp could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so the
					// daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_config_option time; clearing session id so the daemon retries fresh",
						"backend", "omp",
						"session_id", sessionID,
					)
					sessionID = ""
				}
				resCh <- Result{
					Status:     finalStatus,
					Error:      finalError,
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  sessionID,
				}
				return
			}
			b.cfg.Logger.Info("omp session model set", "model", opts.Model)
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
				finalError = fmt.Sprintf("omp timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("omp session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes backend: the runtime may echo the
					// requested id back from session/load even when the
					// session is gone, so the stale id only fails here, at
					// prompt time. Empty SessionID lets the daemon's
					// resume-failure fallback retry fresh and store the
					// replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "omp",
						"session_id", sessionID,
					)
					sessionID = ""
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "omp cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens
				c.usage.CacheWriteTokens += pr.usage.CacheWriteTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("omp finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		// Ensure the stderr copier has drained before consulting the
		// provider-error sniffer; see hermes.go for the failure mode.
		<-stderrDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (HTTP 4xx / rate-limit / expired
		// token). Mirrors hermes/kimi/kiro/qoder/traecli.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		c.usageMu.Lock()
		u := c.usage
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
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
