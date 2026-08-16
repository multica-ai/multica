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

// zcodeBlockedArgs are flags hardcoded by the daemon that user-configured
// custom_args must not override. zcode-acp-server takes no launch arguments
// (it is an ACP server, not a subcommand CLI) and tolerates extra argv, so
// nothing here is protocol-critical today; -h/--help stay blocked so a future
// version that grows a help mode cannot be flipped into it by a custom arg.
var zcodeBlockedArgs = map[string]blockedArgMode{
	"-h":     blockedStandalone,
	"--help": blockedStandalone,
}

// zcodeCancelWaitDelay bounds how long the process may outlive a cancelled
// context. Cancellation is graceful-first: the bridge receives a
// session/cancel notification (it forwards the stop to the zcode runtime and
// suppresses further output), and only after this delay does the exec
// machinery kill whatever is still alive. A var, not a const, so tests can
// shorten it.
var zcodeCancelWaitDelay = 10 * time.Second

// zcodeBackend implements Backend by spawning zcode-acp-server and
// communicating via the ACP (Agent Client Protocol) JSON-RPC 2.0 over
// stdin/stdout.
//
// ZCode (https://z.ai, Zhipu's GLM coding agent) does not speak ACP itself.
// zcode-acp-server (https://github.com/william0wang/zcode-acp, published on
// npm as zcode-acp-server, Apache-2.0) is a community bridge that spawns the
// official desktop-bundled runtime headlessly (`zcode app-server`, the same
// stdio server the ZCode desktop app drives) and exposes it as a standard ACP
// v1 agent. The bridge is a translation layer over the official runtime: it
// does not modify or redistribute ZCode itself, and it reuses the local
// install's credentials (~/.zcode/v2/config.json) rather than accepting any.
// We reuse the existing hermesClient ACP transport — the bridge speaks the
// same protocol as Hermes/Kimi/Kiro/Traecli/QwenPaw — so only the binary and
// a few ZCode-specific behaviours differ.
//
// Behaviour notes verified against zcode-acp-server built from main @a5cb8a1
// driving ZCode CLI 0.16.3 (desktop 3.7.7):
//
//   - `initialize` advertises loadSession plus sessionCapabilities
//     list/resume/fork. Session creation requires the bridge's runtime-
//     preferences handshake fix (william0wang/zcode-acp#38), which shipped
//     in npm 0.2.0 (2026-08-16) — so `zcode-acp-server >= 0.2.0` is the
//     effective version floor.
//   - `session/new` returns a session in the bridge's default mode, yolo —
//     the unattended default this daemon wants, so no mode call is needed.
//   - `session/resume` accepts {cwd, sessionId, mcpServers} like Kimi and
//     re-connects the MCP servers; the bridge retries cold-start resumes
//     internally. Unlike Kimi (whose resume echoes success for a dead
//     session and only fails at prompt time), the bridge rejects an unknown
//     session AT resume — Execute maps that to ResumeRejected so the daemon
//     retries fresh instead of looping on the dead pointer.
//   - `session/setModel` (bridge extension) switches the session's model at
//     runtime; a rejected switch fails the task rather than silently running
//     on a different model.
//   - The bridge streams thought chunks, message chunks, tool calls (with
//     raw input/output), usage updates and plan/todo updates as standard ACP
//     session/update notifications, so a turn keeps the daemon's inactivity
//     watchdog fed throughout (no silent long turns).
//   - Cancellation is process-level: killing the bridge triggers its own
//     watchdog, which reaps the zcode process group, so no group management
//     is needed on this side.
//   - Reasoning effort is wired through the shared ACP effort path: the
//     bridge advertises option id `thought` (category `thought_level`) with
//     the per-model vocabulary the runtime itself resolves (GLM-5.3:
//     low/high/max, default max; GLM-5-Turbo: enabled/off; glm-5.1:
//     enabled/disabled), and applyACPEffortOption passes tokens through
//     verbatim, so the picker offers exactly the levels the session's model
//     understands.
type zcodeBackend struct {
	cfg Config
}

func (b *zcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "zcode-acp-server"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("zcode-acp-server executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP session/new expects. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of
	// silently dropping every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("zcode: invalid mcp_config: %w", err)
	}
	if len(mcpServers) > 0 {
		// Agent-configured MCP servers flow through session/new's
		// mcpServers (william0wang/zcode-acp#43); verified live that the
		// runtime merges them additively alongside its own local config.
		// Bridges older than that fix silently drop the entries, which the
		// minimum bridge version required here already excludes.
		b.cfg.Logger.Debug("forwarding agent MCP servers to the zcode session",
			"backend", "zcode",
			"servers_configured", len(mcpServers),
		)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// The bridge is an ACP server directly — no protocol subcommand to
	// prepend. The daemon auto-approves in hermesClient.handleAgentRequest
	// by selecting a safe granting option for each session/request_permission
	// request, and the bridge's default session mode is already yolo, so no
	// permission preset needs lifting (unlike dim).
	zcodeArgs := filterCustomArgs(opts.CustomArgs, zcodeBlockedArgs, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, zcodeArgs...)
	hideAgentWindow(cmd)
	// Cancellation is graceful-first: neutralise exec's instant SIGKILL so
	// the watcher below gets to deliver session/cancel first (the bridge
	// forwards the stop to the zcode runtime instead of dying mid-write);
	// WaitDelay is the hard backstop that reaps a bridge ignoring it.
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = zcodeCancelWaitDelay
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", zcodeArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stdin pipe: %w", err)
	}
	// Forward the bridge's stderr to the daemon log for diagnosis. Unlike
	// kimi/hermes there is no provider-error sniffer: those backends sniff
	// because their CLI reports stopReason=end_turn while the real error
	// only appears on stderr. The bridge cannot lie that way — it spawns
	// the zcode runtime with stderr ignored and surfaces terminal failures
	// as RPC errors (after retrying transient ones internally) or as a
	// visible degrade message in the stream — so there is nothing on this
	// stderr for a sniffer to catch that the result path misses.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start zcode-acp-server: %w", err)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(newLogWriter(b.cfg.Logger, "[zcode:stderr] "), stderr)
	}()

	b.cfg.Logger.Info("zcode acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// The bridge streams interim narration and the final answer as the same
	// agent_message_chunk type; the tracker keeps only the post-tool-call
	// block for Result.Output while retaining the full text for error
	// detection.
	var deliverable acpDeliverableTracker
	// streamingCurrentTurn gates session updates until our prompt is in
	// flight. The bridge emits updates outside our turn that must not leak
	// into this task's stream: session/resume publishes an initial
	// usage_update for the resumed context (emitInitialUsage — prior-turn
	// occupancy, not this task's billing), and the per-session background
	// listener may flush events for background turns before our prompt is
	// sent. Flipped to true only after session/prompt is sent. (The bridge's
	// other replay path, session/load streaming conversation history, is not
	// used by this backend.)
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)

	// Reuse the hermesClient ACP transport — the bridge speaks the same protocol.
	// No onActivity hook: that exists to arm the post-response notification
	// drain (hermes/grok), which this backend does not use — see the comment
	// at the session/prompt call below.
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
			// Tool names pass through unmapped: the bridge titles tool
			// calls "<ToolName>: <summary>" ("Bash: echo pong"), and
			// hermesClient's title parser already extracts the prefix
			// verbatim, so the UI sees the runtime's real tool names
			// (Bash/Edit/Read/…) exactly like the Claude stream-json
			// backends report them.
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

	// Start reading stdout in background.
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
		c.closeAllPending(fmt.Errorf("zcode-acp-server process exited"))
	}()

	// Drive the ACP session lifecycle in a goroutine.
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
		// Raw session/new or session/resume result, kept for the effort
		// selector it advertises (applyACPEffortOption below).
		var sessionResult json.RawMessage
		// Set when the ACP runtime refuses the session we asked to
		// resume. Only that is curable by starting a fresh session, so
		// handshake/network failures below must leave it false.
		var resumeRejected bool

		// 1. Initialize handshake.
		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			resCh <- Result{
				Status:     "failed",
				Error:      fmt.Sprintf("zcode initialize failed: %v", err),
				DurationMs: time.Since(startTime).Milliseconds(),
			}
			return
		}

		// bridgeVersion records the agentInfo.version for the known-bad
		// hint below. The hint is reported only on failure (0.2.0 is the
		// first release carrying the runtime-preferences fix, so an exact
		// "0.1.0" here means the user installed the stale npm build).
		bridgeVersion := acpAgentInfoVersion(initResult)

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise: the bridge declares stdio MCP only, and shipping an
		// http/sse entry to a stdio-only runtime tanks session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "zcode", b.cfg)

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			// Per ACP Session Setup, session/resume accepts mcpServers and
			// the runtime re-connects them as part of the resume.
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				// Unlike kimi (whose resume echoes success for a dead
				// session and only fails at prompt time), the bridge
				// rejects an unknown session HERE with "Session not
				// found". Surface that as ResumeRejected with a cleared
				// id so the daemon's resume-failure fallback retries
				// fresh instead of looping on the dead pointer.
				if isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at session/resume; clearing session id so the daemon retries fresh",
						"backend", "zcode",
						"session_id", opts.ResumeSessionID,
					)
					resCh <- Result{
						Status:         "failed",
						Error:          fmt.Sprintf("zcode session/resume failed: %v", err),
						DurationMs:     time.Since(startTime).Milliseconds(),
						ResumeRejected: true,
					}
					return
				}
				resCh <- Result{
					Status:     "failed",
					Error:      fmt.Sprintf("zcode session/resume failed: %v", err),
					DurationMs: time.Since(startTime).Milliseconds(),
				}
				return
			}
			sessionResult = result
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "zcode",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				createErr := fmt.Sprintf("zcode session/new failed: %v", err)
				// The npm 0.1.0 release predates the ZCode >=0.16
				// runtime-preferences handshake fix
				// (william0wang/zcode-acp#38): initialize still succeeds,
				// then session/new hangs ~15s and fails with a create
				// timeout. Attach the upgrade hint only when that exact
				// version reported itself; anything newer (0.2.0+,
				// a main-built fork, empty) is not falsely flagged.
				if bridgeVersion == "0.1.0" {
					createErr += " (bridge 0.1.0 from npm cannot create sessions on ZCode CLI >=0.16; upgrade zcode-acp-server — fixed by william0wang/zcode-acp#38)"
				}
				resCh <- Result{
					Status:     "failed",
					Error:      createErr,
					DurationMs: time.Since(startTime).Milliseconds(),
				}
				return
			}
			sessionResult = result
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				resCh <- Result{
					Status:     "failed",
					Error:      "zcode session/new returned no session ID",
					DurationMs: time.Since(startTime).Milliseconds(),
				}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("zcode session created", "session_id", sessionID)

		// 3. If the caller picked a model (via agent.model from the UI
		// dropdown — the catalog comes from the bridge's ACP
		// configOptions), switch the session to it before sending any
		// prompt. This MUST fail the task on error: silently falling back
		// to the bridge's configured default model would let the user
		// believe their pick was honoured while the task ran on something
		// else.
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/setModel", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("zcode setModel failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("zcode could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session can surface here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so the
					// daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at setModel time; clearing session id so the daemon retries fresh",
						"backend", "zcode",
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
			b.cfg.Logger.Info("zcode session model set", "model", opts.Model)
		}

		// 3b. Apply a persisted thinking override through the shared ACP
		// effort path. As with the other ACP providers, a configuration
		// failure does not block the task: the prompt goes out either way
		// and the warnings are the only record. stateIsCurrent is false
		// after a model switch — ACP options may depend on each other, so
		// the vocabulary advertised at session creation may no longer
		// describe the session, and the runtime's own answer decides.
		applyACPEffortOption(runCtx, c.request, "zcode", b.cfg.Logger, sessionID, sessionResult, opts.ThinkingLevel, opts.Model == "")

		// 4. Send the prompt. No SystemPrompt inlining: the runtime reads
		// its instructions from AGENTS.md in the session cwd (the daemon's
		// execenv writes the brief there; zcode is deliberately not in
		// providerNeedsInlineSystemPrompt), so opts.SystemPrompt is always
		// empty on this path and the prompt goes through verbatim.
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": prompt},
			},
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("zcode timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("zcode session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// Defense-in-depth: the bridge normally rejects a dead
					// session at session/resume (see the resume branch
					// above), so this only fires when the session dies
					// between resume and prompt. Empty SessionID lets the
					// daemon's resume-failure fallback retry fresh and store
					// the replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "zcode",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				// The bridge answers session/prompt with
				// stopReason="cancelled" when the runtime itself ended the
				// turn as cancelled (our session/cancel path) — surface
				// that as an abort, not a completion with no output.
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "zcode cancelled the prompt"
				}
				// Inert today — the bridge's PromptResponse carries only
				// stopReason — but usage is a standard PromptResponse field,
				// so a bridge that populates it is billed with no change
				// here. Meanwhile the live token counters arrive as
				// usage_update notifications the shared client already
				// accumulates.
				c.mergeUsage(pr.usage)
			default:
			}
			// No post-response notification drain (kimi's
			// waitForACPNotificationQuiescence): the bridge's turn loop
			// emits every update for the turn — streamed text, tool calls,
			// the completion snapshot diff and the final usage_update —
			// BEFORE it resolves session/prompt, so there is nothing left
			// to wait for after the response.
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("zcode finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Graceful cancellation: when the run was stopped (task stop or
		// timeout), tell the bridge to stop the turn BEFORE the stdin pipe
		// closes and WaitDelay reaps the process. The bridge maps
		// session/cancel onto the runtime's stop command and suppresses
		// further output, so a cancelled task does not leave the runtime
		// killed mid-write when the bridge cooperates. This must happen
		// here, ahead of the explicit stdin close below: the deferred
		// cleanup runs only after the Result is delivered, and by then the
		// pipe this write needs is already gone. Best-effort by design — a
		// lost notification only means a hard kill, which is what every
		// non-graceful backend does unconditionally.
		if runCtx.Err() != nil {
			zcodeSendSessionCancel(c)
		}
		stdin.Close()
		cancel()

		<-readerDone
		<-stderrDone
		streamingCurrentTurn.Store(false)

		finalOutput, _ := deliverable.result()

		u := c.accumulatedUsage()
		var usageMap map[string]TokenUsage
		if acpTokenUsagePresent(u) {
			key := opts.Model
			if key == "" {
				key = "unknown"
			}
			usageMap = map[string]TokenUsage{key: u}
		}
		// The bridge's usage_update currently carries context-window
		// occupancy ({used, size}) but not billing counters, so most turns
		// report no usage here. When the bridge maps its usage.delta
		// telemetry onto the ACP usage_update token fields, this path
		// picks them up without further changes.

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

// zcodeSendSessionCancel writes a best-effort ACP session/cancel notification
// for the client's current session. It serialises on the client's stdin lock
// so it cannot interleave with an in-flight request frame, and reads the
// session id under the client mutex. Empty session id (cancel before the
// session existed) is a no-op: there is no turn to stop, and the process kill
// handles the rest.
func zcodeSendSessionCancel(c *hermesClient) {
	if c == nil {
		return
	}
	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()
	if sid == "" {
		return
	}
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/cancel",
		"params":  map[string]any{"sessionId": sid},
	})
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.stdin == nil {
		return
	}
	_, _ = c.stdin.Write(append(data, '\n'))
}
