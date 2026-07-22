package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// grokBlockedArgs are flags/subcommands hardcoded by the daemon that must not
// be overridden by user-configured custom_args. `agent` + `stdio` select the
// ACP transport; `--always-approve` is daemon-owned so headless Multica runs
// do not block on interactive permission prompts. Switching into
// headless/serve/leader/print modes would break the daemon↔grok ACP contract.
// Model / thinking are managed via session/set_model and --effort.
var grokBlockedArgs = map[string]blockedArgMode{
	"agent":                    blockedStandalone,
	"stdio":                    blockedStandalone,
	"headless":                 blockedStandalone,
	"serve":                    blockedStandalone,
	"leader":                   blockedStandalone,
	"--always-approve":         blockedStandalone,
	"--yolo":                   blockedStandalone,
	"--no-auto-update":         blockedStandalone,
	"--no-alt-screen":          blockedStandalone,
	"-p":                       blockedStandalone,
	"--print":                  blockedStandalone,
	"--single":                 blockedWithValue,
	"--output-format":          blockedWithValue,
	"--permission-mode":        blockedWithValue,
	"-m":                       blockedWithValue,
	"--model":                  blockedWithValue,
	"--reasoning-effort":       blockedWithValue,
	"--effort":                 blockedWithValue,
	"-r":                       blockedWithValue,
	"--resume":                 blockedWithValue,
	"-c":                       blockedStandalone,
	"--continue":               blockedStandalone,
	"-s":                       blockedWithValue,
	"--session-id":             blockedWithValue,
	"--system-prompt-override": blockedWithValue,
	"--cwd":                    blockedWithValue,
	"-w":                       blockedOptionalValue,
	"--worktree":               blockedOptionalValue,
	"--ref":                    blockedWithValue,
	"--fork-session":           blockedStandalone,
}

// grokBackend implements Backend by spawning
// `grok --no-auto-update agent --always-approve [--effort <level>] stdio`
// and communicating via the standard ACP (Agent Client Protocol) JSON-RPC 2.0
// transport over stdin/stdout.
//
// This targets xAI's Grok Build CLI (the `grok` binary). Grok Build exposes
// ACP via `grok agent stdio` (flags such as --always-approve and
// --effort belong on the `agent` command, before the transport
// subcommand). We reuse hermesClient (same as traecli/kimi/kiro/qoder) with
// provider-specific launch args and tool-name normalization.
//
// Capability notes: we attempt session/load on resume and session/set_model
// when a model is requested; if a particular Grok Build version lacks those
// methods, the run fails with a clear error rather than silently continuing
// on the wrong session/model. Real initialize (0.2.x) advertises
// loadSession:true and mcpCapabilities {http, …}.
type grokBackend struct {
	cfg Config
}

var (
	grokReaderDrainGrace      = 2 * time.Second
	grokNotificationQuietTime = 250 * time.Millisecond
)

// grokMessageStream serializes sends and the final close so a late stdout
// reader cannot send on a closed channel. Mirrors traecli/qoder.
type grokMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newGrokMessageStream(size int) *grokMessageStream {
	return &grokMessageStream{ch: make(chan Message, size)}
}

func (s *grokMessageStream) send(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, msg)
}

func (s *grokMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *grokBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "grok"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("grok executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects) into
	// the array shape ACP session/new and session/load expect. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of silently
	// dropping every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("grok: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// Flags on `grok agent` come before the transport subcommand (`stdio`).
	// Thinking is a process-level flag on the agent command; model is set
	// after session create via session/set_model (ACP).
	//
	// `--no-auto-update` is a *global* flag (before the `agent` subcommand):
	// xAI recommends it for headless/ACP/CI runs so a background update check
	// never interferes with an unattended daemon task. It is daemon-owned
	// (grokBlockedArgs) so user custom_args cannot double it or strip it.
	grokArgs := []string{"--no-auto-update", "agent", "--always-approve"}
	if opts.ThinkingLevel != "" {
		grokArgs = append(grokArgs, "--effort", opts.ThinkingLevel)
	}
	grokArgs = append(grokArgs, filterCustomArgs(opts.CustomArgs, grokBlockedArgs, b.cfg.Logger)...)
	grokArgs = append(grokArgs, "stdio")

	cmd := exec.CommandContext(runCtx, execPath, grokArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", grokArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childEnv := buildEnv(b.cfg.Env)
	cmd.Env = childEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point (`stderrDone`) that
	// fires before the failure-promotion decision; see hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("grok")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start grok: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[grok:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("grok acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newGrokMessageStream(256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder
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
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
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
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("grok process exited"))
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
			finalError = fmt.Sprintf("grok initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Grok's ACP surface requires an explicit auth handshake between
		// `initialize` and any session operation: read the advertised
		// authMethods, pick one, and send `authenticate` before session/new
		// or session/load. Skipping this makes a real, logged-in CLI reject
		// every session op (the fake ACP in tests happens to accept them,
		// which is exactly why this must be asserted in tests too). xAI's
		// documented preference is the API key when XAI_API_KEY is set and
		// offered, otherwise the cached login token.
		// Ref: https://docs.x.ai/build/cli/headless-scripting
		methodID, err := selectGrokAuthMethod(extractACPAuthMethods(initResult), envHasNonEmpty(childEnv, "XAI_API_KEY"))
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("grok authentication setup failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		if _, err := c.request(runCtx, "authenticate", map[string]any{
			"methodId": methodID,
			"_meta":    map[string]any{"headless": true},
		}); err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("grok authenticate (%s) failed: %v", methodID, err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		b.cfg.Logger.Info("grok authenticated", "method", methodID)

		// Drop MCP entries whose remote transport the runtime didn't advertise.
		// See hermes.go for why sending an unsupported transport tanks session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "grok", b.cfg.Logger)

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
				finalError = fmt.Sprintf("grok session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "grok",
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
				finalError = fmt.Sprintf("grok session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "grok session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		// Early session pin so a cancelled run still preserves resume pointer.
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("grok session created", "session_id", sessionID)

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("grok set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "grok",
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
			b.cfg.Logger.Info("grok session model set", "model", opts.Model)
		}

		userText := prompt
		if opts.SystemPrompt != "" {
			// Grok also reads AGENTS.md from cwd; inline system prompt covers
			// Multica runtime brief delivery when file injection is not enough.
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
				finalError = fmt.Sprintf("grok timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "grok",
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
					finalError = "grok cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens
				c.usageMu.Unlock()
			default:
			}
			waitForGrokNotificationQuiescence(runCtx, activity, readerDone)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("grok finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		// Grok ACP may keep the process — and the stdout/stderr pipes — open
		// briefly after session/prompt returns. Bound the drain.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), grokReaderDrainGrace)
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

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (auth / rate-limit / HTTP 4xx).
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

// Grok's ACP `authenticate` method ids (from `initialize`.authMethods).
// `xai.api_key` consumes XAI_API_KEY from the environment; `cached_token`
// reuses the credentials written by `grok login`.
const (
	grokAuthMethodAPIKey      = "xai.api_key"
	grokAuthMethodCachedToken = "cached_token"
)

// selectGrokAuthMethod chooses which advertised ACP auth method to use,
// following xAI's documented headless flow: prefer the API key when
// XAI_API_KEY is present in the child env and the CLI offers it, otherwise
// fall back to the cached login token. Grok requires an explicit authenticate
// step: an empty or unknown method list is a protocol/authentication failure,
// not permission to continue directly to session/new.
func selectGrokAuthMethod(methods []string, haveAPIKey bool) (string, error) {
	offered := make(map[string]bool, len(methods))
	for _, m := range methods {
		if m = strings.TrimSpace(m); m != "" {
			offered[m] = true
		}
	}
	if haveAPIKey && offered[grokAuthMethodAPIKey] {
		return grokAuthMethodAPIKey, nil
	}
	if offered[grokAuthMethodCachedToken] {
		return grokAuthMethodCachedToken, nil
	}
	if offered[grokAuthMethodAPIKey] {
		return "", fmt.Errorf("Grok advertised only API-key authentication, but XAI_API_KEY is not set")
	}
	advertised := make([]string, 0, len(offered))
	for method := range offered {
		advertised = append(advertised, method)
	}
	sort.Strings(advertised)
	if len(advertised) == 0 {
		return "", fmt.Errorf("Grok advertised no usable authentication methods; set XAI_API_KEY or run `grok login`")
	}
	return "", fmt.Errorf("Grok advertised unsupported authentication methods %q; update Multica or authenticate with XAI_API_KEY / `grok login`", advertised)
}

// waitForGrokNotificationQuiescence gives the ACP stdout reader a bounded
// chance to consume notifications that Grok may emit just after the
// session/prompt response. Without this window, cancelling the process at the
// response boundary can truncate the final text or usage update.
func waitForGrokNotificationQuiescence(ctx context.Context, activity <-chan struct{}, readerDone <-chan struct{}) {
	quiet := time.NewTimer(grokNotificationQuietTime)
	defer quiet.Stop()
	hard := time.NewTimer(grokReaderDrainGrace)
	defer hard.Stop()

	for {
		select {
		case <-activity:
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(grokNotificationQuietTime)
		case <-quiet.C:
			return
		case <-readerDone:
			return
		case <-hard.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// envHasNonEmpty reports whether an `os/exec`-style env slice
// ("KEY=value" entries) contains key with a non-empty value. Later entries
// win (matching how the OS resolves duplicate keys), so we scan from the end.
func envHasNonEmpty(env []string, key string) bool {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimSpace(env[i][len(prefix):]) != ""
		}
	}
	return false
}
