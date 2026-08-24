package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// zeroclawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport; overriding it would
// break the daemon↔ZeroClaw communication contract. `--help`/`-h` and the
// login/auth flags would switch the CLI into a mode that never starts the
// ACP server.
//
// `--agent` / `--agent-alias` are blocked because `zeroclaw acp` has no such
// flag — clap aborts the process with `error: unexpected argument '--agent'
// found`. takeZeroclawAgentAlias consumes them out of custom_args before this
// filter runs and forwards the value as the session/new `agentAlias` param
// instead; these entries stop a copy arriving through any other path (a
// runtime launch prefix) from reaching argv.
var zeroclawBlockedArgs = map[string]blockedArgMode{
	"acp":           blockedStandalone,
	"--help":        blockedStandalone,
	"-h":            blockedStandalone,
	"login":         blockedStandalone,
	"auth":          blockedStandalone,
	"--login":       blockedStandalone,
	"--auth":        blockedStandalone,
	"--agent":       blockedWithValue,
	"--agent-alias": blockedWithValue,
}

// takeZeroclawAgentAlias consumes the ZeroClaw agent alias from custom_args
// and returns it with the remaining arguments.
//
// ZeroClaw binds every ACP session to an agent alias, resolved in this order:
// the session/new `agentAlias` param, `[acp].default_agent`, then auto-select
// when the config holds exactly one `[agents.<alias>]` entry. With none of
// those, session/new fails with -32602. The alias cannot travel on argv (see
// zeroclawBlockedArgs), so custom_args is the only operator-facing channel and
// the value is lifted out here.
//
// Both `--agent x` and `--agent=x` are accepted, `--agent-alias` is the long
// spelling, and the last occurrence wins so a per-agent entry can override a
// runtime-wide one. An empty or whitespace-only value counts as unset:
// ZeroClaw trims and drops empty aliases the same way, and sending one would
// forfeit its auto-select.
func takeZeroclawAgentAlias(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	alias := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := unshellQuoteArg(args[i])
		flag, value := arg, ""
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag, value, hasInlineValue = arg[:idx], arg[idx+1:], true
		}
		if flag != "--agent" && flag != "--agent-alias" {
			rest = append(rest, args[i])
			continue
		}
		if !hasInlineValue {
			if i+1 >= len(args) {
				continue
			}
			i++
			value = unshellQuoteArg(args[i])
		}
		alias = strings.TrimSpace(value)
	}
	return alias, rest
}

// zeroclawResumeSupported reads the capability ZeroClaw derives from whether
// its persistent session store opened successfully. A read-only or unwritable
// config directory leaves resume absent even on a new-enough binary.
func zeroclawResumeSupported(result json.RawMessage) bool {
	var r struct {
		AgentCapabilities struct {
			SessionCapabilities struct {
				Resume *struct{} `json:"resume"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
	}
	return json.Unmarshal(result, &r) == nil && r.AgentCapabilities.SessionCapabilities.Resume != nil
}

// selectZeroclawPermissionOption keeps ordinary single-use tool approvals on
// the shared ACP policy, but fails closed for ZeroClaw's legacy structured
// question bridge. That bridge labels every answer `choice-N` with
// allow_once, so generic auto-approval would silently answer every question
// with the first choice. Returning ok=false makes the shared transport send a
// protocol error: even the option marked reject_once is still mapped back to
// a real answer by ZeroClaw, so selecting it would silently choose the last
// answer rather than reject the question.
func selectZeroclawPermissionOption(params json.RawMessage) (optionID string, grant bool, ok bool) {
	var p struct {
		Options []acpPermissionOption `json:"options"`
	}
	if json.Unmarshal(params, &p) != nil {
		return "", false, false
	}

	isLegacyChoice := len(p.Options) >= 2
	for _, opt := range p.Options {
		if !strings.HasPrefix(opt.OptionID, "choice-") {
			isLegacyChoice = false
			break
		}
	}
	if !isLegacyChoice {
		return selectACPPermissionOption(params)
	}
	return "", false, false
}

// zeroclawSessionNewErrorMessage turns ZeroClaw's agent-selection failure into
// something an operator can act on.
//
// session/new demands `agentAlias` whenever the config does not hold exactly
// one agent — including the zero-agent case, where it asks for the alias of an
// entry that does not exist yet — and no CLI flag can supply one, so the bare
// RPC error leaves no route forward.
func zeroclawSessionNewErrorMessage(err error) string {
	base := fmt.Sprintf("zeroclaw session/new failed: %v", err)
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) || !strings.Contains(rpcErr.Message, "agentAlias") {
		return base
	}
	return base + " — ZeroClaw auto-selects an agent only when its config holds exactly one [agents.<alias>] entry." +
		" Configure the agent you want, then name it with `--agent <alias>` in this runtime's custom args" +
		" or set `[acp].default_agent` in ZeroClaw's config."
}

// zeroclawBackend implements Backend by spawning `zeroclaw acp` and
// communicating via the standard ACP (Agent Client Protocol) JSON-RPC 2.0
// transport over stdin/stdout.
//
// ZeroClaw is a Rust-based, single-binary generic agent runtime (see
// multica-ai/multica#1543). Its ACP server exposes the same protocol
// surface that Hermes/Kimi/Reasonix/Dim/Traecli/Grok/QwenPaw/MCode use, so
// the backend reuses the shared hermesClient ACP transport — only the
// binary, the session bootstrap, and the tool-name extraction differ.
//
// Verified against a real ZeroClaw 0.8.4 binary driven over stdio. That
// handshake is not vanilla ACP, and the departures below are what this file
// is shaped around:
//
//   - The dispatch table is initialize and session/{new,load,resume,close,
//     prompt,stop,cancel,event|update}. There is no `session/set_model` — it
//     answers -32601 — and no handler reads a model param at all. The model
//     belongs to the ZeroClaw agent profile (`agents.<alias>.model_provider`
//     → `[providers.models.<type>.<alias>].model`) and cannot be selected per
//     session, so ModelSelectionSupported opts ZeroClaw out. The
//     `initialize._meta.zeroclaw.defaultModel` value is process-global (the
//     first configured provider model), not the model for the alias later
//     selected by session/new, so it is not used for usage attribution.
//   - session/new returns exactly {sessionId, workspaceDir}: no model
//     catalog, no currentModelId, so there is nothing to discover.
//   - session/new requires `agentAlias` unless exactly one agent is
//     configured or `[acp].default_agent` is set. See takeZeroclawAgentAlias.
//   - Resume goes through session/resume, not session/load: load replays the
//     whole retained transcript back as session/update notifications, which
//     would re-emit prior turns as this turn's output.
//   - No handler reads `params.mcpServers`. MCP is operator-side only,
//     configured in ZeroClaw's own config-dir.
//   - Its SESSION_NOT_FOUND is a custom -32000; see isACPSessionNotFound.
//
// MinVersions requires ZeroClaw 0.8.0+, the first stable release with
// persistent ACP sessions and session/resume. No permission-preset injection
// or separate authenticate step is needed: initialize returns an empty
// `authMethods` and the handshake asks for nothing further.
type zeroclawBackend struct {
	cfg Config
}

var (
	zeroclawReaderDrainGrace      = 2 * time.Second
	zeroclawNotificationQuietTime = 250 * time.Millisecond
)

// zeroclawMessageStream serializes sends and the final close so a late
// stdout reader cannot send on a closed channel. Mirrors dim/grok/traecli.
type zeroclawMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newZeroclawMessageStream(size int) *zeroclawMessageStream {
	return &zeroclawMessageStream{ch: make(chan Message, size)}
}

func (s *zeroclawMessageStream) send(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, msg)
}

func (s *zeroclawMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *zeroclawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "zeroclaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("zeroclaw executable not found at %q: %w", execPath, err)
	}

	// ZeroClaw discards client-supplied MCP servers: none of its session/new,
	// session/load or session/resume handlers reads `params.mcpServers`, and
	// enabling `agents.<alias>.acp_enable_mcp` initialises that agent's OWN
	// `mcp_bundles`, not ours. MCP therefore lives entirely in ZeroClaw's
	// config-dir. providerSupportsMcpConfig hides the MCP tab for this
	// runtime, so reaching here means a value saved before that — warn and
	// continue rather than bricking the task over config we cannot honour.
	if len(opts.McpConfig) > 0 {
		b.cfg.Logger.Warn("zeroclaw ignores MCP servers supplied by Multica; its ACP server reads MCP only from its own config-dir ([[mcp.servers]] + [mcp_bundles.*] + agents.<alias>.mcp_bundles with acp_enable_mcp = true)",
			"backend", "zeroclaw",
		)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	agentAlias, customArgs := takeZeroclawAgentAlias(opts.CustomArgs)
	zeroclawArgs := append([]string{"acp"}, filterCustomArgs(customArgs, zeroclawBlockedArgs, b.cfg.Logger)...)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, zeroclawArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(zeroclawArgs, trustAgentCommandPositional(0, "acp")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zeroclaw stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zeroclaw stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point (`stderrDone`) that
	// fires before the failure-promotion decision; see hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("zeroclaw")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zeroclaw stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start zeroclaw: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[zeroclaw:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("zeroclaw acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newZeroclawMessageStream(256)
	resCh := make(chan Result, 1)

	// ZeroClaw streams interim narration and the final answer as the same
	// agent_message_chunk type; the tracker keeps only the post-tool-call
	// block for Result.Output while retaining the full text for error
	// detection.
	var deliverable acpDeliverableTracker
	// streamingCurrentTurn gates every session update so that anything the
	// runtime pushes outside our turn is dropped instead of landing in the
	// deliverable. It must default to false: ZeroClaw flushes the frames that
	// matter here — session/load's transcript replay — before it answers the
	// request that triggered them, so the gate has to be closed from process
	// start. Flipped to true only just before session/prompt is sent.
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	c := &hermesClient{
		cfg:              b.cfg,
		stdin:            stdin,
		pending:          make(map[int]*pendingRPC),
		pendingTools:     make(map[string]*pendingToolCall),
		selectPermission: selectZeroclawPermissionOption,
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
				// Re-normalise tool titles the same way kimi/traecli/grok/dim
				// do so the UI sees consistent snake_case names.
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
		c.closeAllPending(fmt.Errorf("zeroclaw process exited"))
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
		var effectiveModel string

		// Keep elicitation absent: this headless client cannot collect a user's
		// form response. ZeroClaw's legacy structured-choice bridge is handled
		// fail-closed by selectZeroclawPermissionOption instead.
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
			finalError = fmt.Sprintf("zeroclaw initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" && !zeroclawResumeSupported(initResult) {
			b.cfg.Logger.Warn("zeroclaw persistence is unavailable; the daemon will retry from a rebuilt fresh-session context",
				"backend", "zeroclaw",
				"requested_session", opts.ResumeSessionID,
			)
			resumeRejected = true
			resCh <- Result{
				Status:         "failed",
				Error:          "zeroclaw session/resume unavailable: initialize did not advertise sessionCapabilities.resume",
				DurationMs:     time.Since(startTime).Milliseconds(),
				ResumeRejected: resumeRejected,
			}
			return
		}

		if opts.ResumeSessionID != "" {
			// session/resume, not session/load. Both restore the transcript
			// into the agent and both answer a bare `{}`, but load also
			// replays every retained message back to us as session/update
			// notifications, so a resumed turn would re-emit the previous
			// answer as its own output.
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"sessionId": opts.ResumeSessionID,
			})
			if err != nil {
				if isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("zeroclaw resumed session not found; the daemon will retry fresh",
						"backend", "zeroclaw",
						"requested_session", opts.ResumeSessionID,
					)
					resumeRejected = true
					resCh <- Result{Status: "failed", Error: fmt.Sprintf("zeroclaw session/resume failed: %v", err), DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
					return
				}
				finalStatus = "failed"
				finalError = fmt.Sprintf("zeroclaw session/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("zeroclaw returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "zeroclaw",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
		} else {
			// mcpServers stays empty on purpose: ZeroClaw never reads it.
			// agentAlias is omitted unless the operator named one — sending a
			// guess would forfeit ZeroClaw's sole-agent auto-select and turn a
			// working single-agent install into `Unknown agent`.
			params := map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
			}
			if agentAlias != "" {
				params["agentAlias"] = agentAlias
			}
			result, err := c.request(runCtx, "session/new", params)
			if err != nil {
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("zeroclaw timed out during session/new: %v", timeout)
				} else if runCtx.Err() == context.Canceled {
					finalStatus = "aborted"
					finalError = fmt.Sprintf("zeroclaw aborted: %v", err)
				} else {
					finalStatus = "failed"
					finalError = zeroclawSessionNewErrorMessage(err)
				}
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "zeroclaw session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		// Early session pin so a cancelled run still preserves resume pointer.
		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("zeroclaw session ready", "session_id", sessionID)

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
				finalError = fmt.Sprintf("zeroclaw timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("zeroclaw session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "zeroclaw",
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
					finalError = "zeroclaw cancelled the prompt"
				}
				effectiveModel = pr.modelID
				c.mergeUsage(pr.usage)
			default:
			}
			// Give the stdout reader a bounded chance to consume notifications
			// zeroclaw may emit just after session/prompt returns
			// (agent_message_chunk, usage updates). Closing stdin at the
			// response boundary otherwise races the reader and truncates the
			// final text — the same race fixed for hermes/dim/grok.
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, zeroclawNotificationQuietTime, zeroclawReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("zeroclaw finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		// ZeroClaw's ACP server may keep the process — and the stdout/stderr
		// pipes — open briefly after session/prompt returns. Bound the drain.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), zeroclawReaderDrainGrace)
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

		u := c.accumulatedUsage()

		// ZeroClaw 0.8.4 reports no token counts over ACP — its prompt result
		// is {sessionId, stopReason, content} — so usageMap stays nil today. If
		// a later response reports usage without a per-turn model id, keep it
		// under "unknown": initialize.defaultModel is process-global and can
		// belong to a different agent alias.
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
