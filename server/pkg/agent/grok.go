package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// grokBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `agent` + `stdio` put the Grok
// Build CLI into ACP server mode (`grok agent stdio`); letting users strip or
// duplicate them would break the daemon↔Grok JSON-RPC transport contract.
// `--always-approve` is daemon-owned so headless ACP always auto-approves
// tool executions — the daemon cannot answer grok's interactive permission
// prompts. `--no-plan` disables plan mode: grok-build is a planning agent
// (agentType "grok-build-plan") that, on a substantive task, enters plan mode
// and stops to ask a clarifying question before editing — which a headless
// daemon can't answer, so the turn ends as stopReason=cancelled with no output.
var grokBlockedArgs = map[string]blockedArgMode{
	"agent":            blockedStandalone,
	"stdio":            blockedStandalone,
	"--always-approve": blockedStandalone,
	"--no-plan":        blockedStandalone,
}

// grokBackend implements Backend by spawning `grok agent stdio` and
// communicating via the ACP (Agent Communication Protocol) JSON-RPC 2.0
// transport over stdin/stdout.
//
// Grok Build is wire-compatible with the same ACP flow Hermes / Kimi / Kiro /
// Qoder use, so we reuse hermesClient and the shared ACP helpers. The one
// Grok-specific wrinkle is an explicit `authenticate` round-trip between
// `initialize` and `session/new`: Grok's initialize response advertises
// `authMethods`, and the client must pick one before any session can be
// created. Grok supports two credential paths and we cover both:
//
//   - `xai.api_key` — used when XAI_API_KEY is configured for the runtime.
//     The right choice for headless / CI hosts with no browser.
//   - `cached_token` — the token persisted by `grok login`, a one-time
//     browser OAuth that also covers a SuperGrok subscription. Grok normally
//     opens a browser on first launch; the daemon is headless, so we never
//     trigger that — `authenticate` is sent with `_meta.headless = true`, and
//     if no credential is present the task fails with a clear hint instead.
//
// See https://docs.x.ai/build/cli/headless-scripting.
type grokBackend struct {
	cfg Config
}

var grokReaderDrainGrace = 2 * time.Second

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
	// the array shape ACP session/new and session/resume expect. Reuse the
	// shared converter so remote MCP `headers` (e.g. Authorization) survive as
	// [{name, value}] and output is deterministic. Fail closed on malformed
	// JSON so the launch surfaces the real error instead of silently dropping
	// every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("grok: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// Global flags precede the `agent stdio` subcommand. Both are required for
	// headless task execution and mirror how the other autonomous backends run:
	//   --always-approve  auto-approve every tool execution (no interactive
	//                     permission prompt the daemon can't answer).
	//   --no-plan         disable plan mode; grok-build is a planning agent that
	//                     otherwise stops mid-task to ask a clarifying question,
	//                     which a headless daemon can't answer — the turn then
	//                     ends as stopReason=cancelled with no output.
	grokArgs := append(
		[]string{"--always-approve", "--no-plan", "agent", "stdio"},
		filterCustomArgs(opts.CustomArgs, grokBlockedArgs, b.cfg.Logger)...,
	)
	cmd := exec.CommandContext(runCtx, execPath, grokArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", grokArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	apiKeyPresent := grokAPIKeyPresent(b.cfg.Env)

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
	// StderrPipe + an explicit copier give us a join point (`stderrDone`)
	// that fires before the failure-promotion decision; see the matching
	// comment in qoder.go / hermes.go for why the io.MultiWriter form races
	// with stopReason=end_turn under load.
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

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		// Grok's session/request_permission options use IDs the other ACP
		// runtimes don't (e.g. "allow-edits-session"); pick a real allow option
		// from each request so write/exec tools get approved instead of the turn
		// cancelling.
		permissionOptionID: selectACPApprovalOptionID,
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

		// Authenticate before any session can be created. Grok advertises the
		// supported methods in its initialize response; we prefer the API key
		// when one is configured and fall back to the cached token from
		// `grok login`. When no methods are advertised (older protocol / no
		// auth required) we skip the call and proceed straight to session/new,
		// matching the other ACP backends.
		if methodID := pickGrokAuthMethod(extractACPAuthMethods(initResult), apiKeyPresent); methodID != "" {
			if _, err := c.request(runCtx, "authenticate", map[string]any{
				"methodId": methodID,
				"_meta":    map[string]any{"headless": true},
			}); err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok authenticate failed (method %q): %v — run `grok login` on the runtime host or set XAI_API_KEY", methodID, err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			b.cfg.Logger.Info("grok authenticated", "method", methodID)
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime
		// tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "grok", b.cfg.Logger)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok session/resume failed: %v", err)
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
			// Pass the requested model in session/new (mirrors Hermes). Grok
			// resolves its own default when the field is absent; unknown
			// session params are ignored by ACP servers, and the model the
			// runtime actually selected is read back via currentModelId for
			// usage attribution.
			result, err := c.request(runCtx, "session/new", buildHermesSessionParams(cwd, opts.Model, mcpServers))
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
		b.cfg.Logger.Info("grok session created", "session_id", sessionID)

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// Flip just before session/prompt so history replay flushed during setup
		// is dropped; every notification for this turn is processed afterward.
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
					// The runtime may echo the requested id from
					// session/resume and only reject it at prompt time.
					// Empty SessionID lets the daemon retry with a fresh
					// session instead of pinning future runs to the stale id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "grok",
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
					finalError = "grok cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("grok finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		// Grok ACP may keep the process — and the stdout/stderr pipes — open
		// after session/prompt returns. The prompt response is already
		// terminal, so bound the drain: wait for both the stdout reader and the
		// stderr copier, but no longer than the grace window. CommandContext
		// cancellation above is what actually tears the process down. Draining
		// stderr here is what makes the provider-error promotion below see a
		// terminal marker before we decide the final status.
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
		// The stdout reader may still run after the grace window. Flip the
		// stream gate before this goroutine's defer closes msgStream; if a
		// late reader already passed the gate, grokMessageStream serializes
		// send and close so the late send is dropped instead of panicking.
		streamingCurrentTurn.Store(false)

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (HTTP 4xx / rate-limit / expired
		// token). Mirrors hermes/kimi/kiro/qoder; without it a run that
		// exhausts retries still reports "completed" because session/prompt
		// ends with stopReason=end_turn even though grok wrote a terminal error
		// to stderr.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 {
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

	return &Session{Messages: msgStream.ch, Result: resCh}, nil
}

// extractACPAuthMethods pulls the advertised `authMethods` ids out of an ACP
// initialize response. Grok returns entries shaped `{"id": "...", ...}`; a
// missing/empty list means the runtime requires no explicit authenticate step.
func extractACPAuthMethods(result json.RawMessage) []string {
	var r struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil
	}
	out := make([]string, 0, len(r.AuthMethods))
	for _, m := range r.AuthMethods {
		if id := strings.TrimSpace(m.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// pickGrokAuthMethod chooses which advertised auth method to use. Prefers the
// API-key method when XAI_API_KEY is configured, then the cached token written
// by `grok login`, then the first advertised method as a last resort. Returns
// "" when nothing is advertised so the caller skips authenticate entirely.
func pickGrokAuthMethod(methods []string, apiKeyPresent bool) string {
	has := func(want string) bool {
		for _, m := range methods {
			if m == want {
				return true
			}
		}
		return false
	}
	if apiKeyPresent && has("xai.api_key") {
		return "xai.api_key"
	}
	if has("cached_token") {
		return "cached_token"
	}
	if len(methods) > 0 {
		return methods[0]
	}
	return ""
}

// grokAPIKeyPresent reports whether an XAI API key is available to the spawned
// CLI, checking the agent's configured env first and then the daemon process
// environment (buildEnv merges both into the child's env).
func grokAPIKeyPresent(env map[string]string) bool {
	if strings.TrimSpace(env["XAI_API_KEY"]) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("XAI_API_KEY")) != ""
}
