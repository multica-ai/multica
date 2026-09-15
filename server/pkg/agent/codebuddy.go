package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// codebuddyBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. CodeBuddy enters ACP mode via
// `--acp`; letting users strip or duplicate it (or switch back to print /
// stream-json / serve / multitask) would break the daemon↔CLI contract.
// Model, effort, resume, and MCP travel as ACP RPCs rather than argv.
//
// `--agent` / `--multitask` are blocked because CodeBuddy's ACP / `--serve`
// entry guard rejects them (https://www.codebuddy.cn/docs/cli/acp).
var codebuddyBlockedArgs = map[string]blockedArgMode{
	"--acp":                  blockedStandalone,
	"acp":                    blockedStandalone,
	"-p":                     blockedStandalone,
	"--print":                blockedStandalone,
	"--output-format":        blockedWithValue,
	"--input-format":         blockedWithValue,
	"--permission-mode":      blockedWithValue,
	"--mcp-config":           blockedWithValue,
	"--effort":               blockedWithValue,
	"--model":                blockedWithValue,
	"-m":                     blockedWithValue,
	"--resume":               blockedWithValue,
	"-r":                     blockedWithValue,
	"--continue":             blockedStandalone,
	"-c":                     blockedStandalone,
	"--append-system-prompt": blockedWithValue,
	"--max-turns":            blockedWithValue,
	"--agent":                blockedWithValue,
	"--multitask":            blockedStandalone,
	"--serve":                blockedStandalone,
}

func buildCodebuddyArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"--acp"}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprint(opts.MaxTurns))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, codebuddyBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, codebuddyBlockedArgs, logger)...)
	return args
}

// codebuddyBackend implements Backend by spawning `codebuddy --acp` and
// speaking ACP JSON-RPC 2.0 over stdin/stdout, the same transport Hermes /
// Kimi / Kiro / Qoder / Grok already use.
//
// Official surface (CodeBuddy 2.143.1, https://www.codebuddy.cn/docs/cli/acp):
// initialize → session/new or session/load → session/set_model →
// session/set_config_option (thought_level) → session/prompt.
// `authenticate` is advertised but not required when the CLI is already
// logged in — discovery and a live handshake both reach session/new without
// it, and the advertised methods (iOA / Google / WeChat) are interactive.
// clientCapabilities stay empty so file and terminal tools run in the agent
// process instead of being proxied back to Multica.
type codebuddyBackend struct {
	cfg Config
}

var codebuddyReaderDrainGrace = 2 * time.Second

type codebuddyMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newCodebuddyMessageStream(size int) *codebuddyMessageStream {
	return &codebuddyMessageStream{ch: make(chan Message, size)}
}

func (s *codebuddyMessageStream) send(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, msg)
}

func (s *codebuddyMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *codebuddyBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codebuddy"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codebuddy executable not found at %q: %w", execPath, err)
	}

	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("codebuddy: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	codebuddyArgs := buildCodebuddyArgs(opts, b.cfg.Logger)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, codebuddyArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(codebuddyArgs))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codebuddy stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codebuddy stdin pipe: %w", err)
	}
	providerErr := newACPProviderErrorSniffer("codebuddy")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codebuddy stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start codebuddy: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[codebuddy:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("codebuddy acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgStream := newCodebuddyMessageStream(256)
	resCh := make(chan Result, 1)

	var deliverable acpDeliverableTracker
	var vendorUsage codebuddyUsageTracker
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	c := &hermesClient{
		onNotification: func(method string, params json.RawMessage) {
			if streamingCurrentTurn.Load() {
				vendorUsage.observe(method, params)
			}
		},
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
		c.closeAllPending(fmt.Errorf("codebuddy process exited"))
	}()

	go func() {
		defer cancel()
		defer msgStream.close()
		defer close(resCh)
		defer func() {
			stdin.Close()
			// Cancel before Wait: an initialization failure can leave a
			// child alive even after stdin closes, including with no timeout.
			cancel()
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		var resumeRejected bool
		effectiveModel := strings.TrimSpace(opts.Model)

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			// Empty on purpose: declaring fs / terminal would proxy those
			// tools back to this client, which has no handlers for them.
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("codebuddy initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "codebuddy", b.cfg)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		var sessionResult []byte
		if opts.ResumeSessionID != "" {
			// CodeBuddy advertises loadSession:true (docs + live 2.143.1
			// initialize). session/load is the Zed ACP method; page refresh
			// recovery in the vendor docs also uses loadSession.
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("codebuddy session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: isACPSessionNotFound(err)}
				return
			}
			sessionResult = result
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "codebuddy",
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
				finalError = fmt.Sprintf("codebuddy session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionResult = result
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "codebuddy session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("codebuddy session created", "session_id", sessionID)

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("codebuddy set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("codebuddy could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "codebuddy",
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
			b.cfg.Logger.Info("codebuddy session model set", "model", opts.Model)
		}

		applyACPEffortOption(runCtx, c.request, "codebuddy", b.cfg.Logger,
			sessionID, sessionResult, opts.ThinkingLevel, opts.Model == "")

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
				finalError = fmt.Sprintf("codebuddy timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("codebuddy session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "codebuddy",
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
					finalError = "codebuddy cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, codebuddyReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("codebuddy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		drainCtx, drainCancel := context.WithTimeout(context.Background(), codebuddyReaderDrainGrace)
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
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		u := c.accumulatedUsage()
		if !acpTokenUsagePresent(u) {
			vendor := vendorUsage.total()
			vendor.CostUSDTicks = u.CostUSDTicks
			u = vendor
		}
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
