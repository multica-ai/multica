package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// reasonixBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport for the Reasonix CLI;
// overriding it would break the daemon↔Reasonix communication contract.
var reasonixBlockedArgs = map[string]blockedArgMode{
	"acp":               blockedStandalone,
	"--model":           blockedWithValue,
	"--profile":         blockedWithValue,
	"--planner":         blockedWithValue,
	"--sandbox-network": blockedWithValue,
	"--sandbox-bash":    blockedWithValue,
	"--workspace-only":  blockedStandalone,
}

func reasonixACPLaunchArgs() []string {
	// Let Reasonix enforce bash sandboxing where the host supports it and
	// degrade according to its host configuration where it does not.
	return []string{
		"acp",
		"--profile", "balanced",
		"--planner", "auto",
		"--sandbox-network", "auto",
		"--sandbox-bash", "auto",
		"--workspace-only",
	}
}

// reasonixReaderDrainGrace bounds how long the turn waits for trailing ACP
// notifications after the session/prompt response. A var, not a const, so
// tests can shorten it. Mirrors qoderReaderDrainGrace / traecliReaderDrainGrace.
var reasonixReaderDrainGrace = 2 * time.Second

// reasonixBackend implements Backend by spawning `reasonix acp` and communicating
// via the ACP (Agent Client Protocol) JSON-RPC 2.0 over stdin/stdout.
//
// Reasonix (https://github.com/esengine/DeepSeek-Reasonix) supports ACP out of
// the box via the `reasonix acp` subcommand. We reuse the existing hermesClient
// transport but keep Reasonix-specific permission, status-usage, and error
// semantics in this adapter.
type reasonixBackend struct {
	cfg Config
}

func (b *reasonixBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "reasonix"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("reasonix executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP `session/new` expects. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of
	// silently dropping all MCP servers.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("reasonix: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// Reasonix multiplexes ordinary tool approvals, protected decisions, and
	// user questions through session/request_permission. The adapter below
	// narrows the generic auto-approval policy; --workspace-only independently
	// keeps configured extra write roots out of unattended tasks.
	reasonixArgs := append(reasonixACPLaunchArgs(), filterCustomArgs(opts.CustomArgs, reasonixBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, reasonixArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", reasonixArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reasonix stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reasonix stdin pipe: %w", err)
	}
	// Forward stderr to the daemon log *and* sniff provider-level
	// errors out of it so we can surface them in the task result.
	// Keep provider-level stderr failures visible in the task result even when
	// the ACP prompt response itself does not preserve their full detail.
	//
	// StderrPipe + an explicit copier give us a join point
	// (`stderrDone`) that fires before the failure-promotion
	// decision; see the matching comment in hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("reasonix")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reasonix stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start reasonix: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[reasonix:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("reasonix acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Reasonix streams interim narration and the final answer as the same
	// agent_message_chunk type; the tracker keeps only the post-tool-call block
	// for Result.Output while retaining the full text for error detection.
	var deliverable acpDeliverableTracker
	// streamingCurrentTurn gates all session updates so that history replay
	// (Reasonix sends full prior-turn transcripts on session/resume, and may
	// flush queued chunks before our session/prompt response streams) is
	// dropped instead of duplicating the previous answer into output. We
	// flip it to true only after session/prompt is sent.
	var streamingCurrentTurn atomic.Bool
	var blockedQuestion atomic.Value // string; set by the stdout reader
	var statusUsage reasonixStatusUsageTracker

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	// Reuse the hermesClient ACP transport — Reasonix speaks the same protocol.
	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		selectPermission: func(params json.RawMessage) (string, bool, bool) {
			if reasonixPermissionMetadataMissing(params) {
				b.cfg.Logger.Warn("reasonix permission request is missing trusted metadata; protected-decision detection may be degraded")
			}
			optionID, grant, ok, question := selectReasonixPermissionOption(params)
			if question != "" {
				blockedQuestion.Store(question)
			}
			return optionID, grant, ok
		},
		onNotification: func(method string, params json.RawMessage) {
			if streamingCurrentTurn.Load() {
				statusUsage.observe(method, params)
			}
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
			// hermesClient.handleToolCallStart has already mapped
			// the raw ACP title via hermesToolNameFromTitle — which
			// covers lowercase hermes-style titles ("read:", "patch
			// (replace)", …) but not capitalised reasonix-style ones
			// ("Read file: …", "Run command: …"). Re-normalise so
			// the UI sees consistent snake_case identifiers across
			// both backends. No-op when the name is already normal
			// form (e.g. already mapped to "read_file").
			if msg.Type == MessageToolUse {
				msg.Tool = reasonixToolNameFromTitle(msg.Tool)
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

	// Start reading stdout in background.
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
		c.closeAllPending(fmt.Errorf("reasonix process exited"))
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
			finalStatus, finalError = reasonixRequestFailure(runCtx, timeout, fmt.Sprintf("reasonix initialize failed: %v", err))
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		warnReasonixCapabilityGaps(b.cfg.Logger, initResult)

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for the why —
		// shipping an http/sse entry to a stdio-only runtime tanks the
		// whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "reasonix", b.cfg)

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		// sessionResult is whichever of session/new or session/resume produced
		// this session. It carries the configOptions that the effort step
		// below reads, so both branches have to keep hold of it.
		var sessionResult json.RawMessage

		if opts.ResumeSessionID != "" {
			// Per ACP Session Setup, session/resume accepts mcpServers and
			// the runtime re-connects them as part of the resume. Without
			// this, a resumed Reasonix task lost access to MCP tools that a
			// fresh task on the same agent would have.
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus, finalError = reasonixRequestFailure(runCtx, timeout, fmt.Sprintf("reasonix session/resume failed: %v", err))
				if finalStatus == "failed" && isACPSessionNotFound(err) {
					resumeRejected = true
					finalError = fmt.Sprintf("reasonix session/resume rejected the saved session: %v", err)
				} else if finalStatus == "failed" && isReasonixSessionLeaseConflict(err) {
					finalError = fmt.Sprintf("reasonix session is already in use; close the other Reasonix window or process first: %v", err)
				}
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					ResumeRejected: resumeRejected,
				}
				return
			}
			sessionResult = result
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "reasonix",
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
				finalStatus, finalError = reasonixRequestFailure(runCtx, timeout, reasonixSessionNewError(err))
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionResult = result
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "reasonix session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("reasonix session created", "session_id", sessionID)

		// 3. If the caller picked a model (via agent.model from the
		// UI dropdown), ask reasonix to switch the session to it before
		// we send any prompt. Reasonix's ACP server exposes
		// `session/set_model` and advertises available models via
		// the `models.availableModels` block returned by
		// `session/new` — we pass the chosen modelId through
		// verbatim. This MUST fail the task on error: silently
		// falling back to reasonix's default model would let the user
		// believe their pick was honoured while the task actually
		// ran on something else.
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("reasonix set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus, finalError = reasonixRequestFailure(runCtx, timeout, fmt.Sprintf("reasonix could not switch to model %q: %v", opts.Model, err))
				if finalStatus == "failed" && opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so
					// the daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "reasonix",
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
			b.cfg.Logger.Info("reasonix session model set", "model", opts.Model)
		}

		// 3b. Apply a persisted thinking override through whichever effort
		// option this session advertises. Unlike set_model above this must NOT
		// fail the task: an effort we could not apply still runs the prompt at
		// the runtime's own default, which is a degraded result rather than a
		// wrong one. The helper logs what actually took effect.
		//
		// sessionResult stops describing the live session once set_model runs
		// above, because reasonix derives the effort catalog from the current
		// model and returns nothing from set_model. Say so, so the helper
		// trusts the runtime's answer over a stale advertised list.
		applyACPEffortOption(runCtx, c.request, "reasonix", b.cfg.Logger,
			sessionID, sessionResult, opts.ThinkingLevel, opts.Model == "")

		// 4. Send the prompt and wait for PromptResponse. Reasonix loads
		// AGENTS.md from cwd, so the daemon deliberately does not duplicate the
		// runtime brief in this user message.
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": prompt},
			},
		})
		if err != nil {
			finalStatus, finalError = reasonixRequestFailure(runCtx, timeout, fmt.Sprintf("reasonix session/prompt failed: %v", err))
			if finalStatus == "failed" {
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes backend: the runtime echoes the
					// requested id back from session/resume even when
					// the session is gone, so the stale id only fails
					// here, at prompt time. Empty SessionID lets the
					// daemon's resume-failure fallback retry fresh and
					// store the replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "reasonix",
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
				case "end_turn":
				case "cancelled":
					finalStatus = "aborted"
					finalError = "reasonix cancelled the prompt"
				case "error":
					finalStatus = "failed"
					finalError = "reasonix ended the prompt with stopReason=error"
				default:
					finalStatus = "failed"
					finalError = fmt.Sprintf("reasonix returned unsupported stopReason %q", pr.stopReason)
				}
				c.mergeUsage(pr.usage)
			default:
				finalStatus = "failed"
				finalError = "reasonix returned no prompt completion result"
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, reasonixReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("reasonix finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		// Ensure the stderr copier has drained before consulting the
		// provider-error sniffer; see hermes.go for the failure mode.
		<-stderrDone
		streamingCurrentTurn.Store(false)

		finalOutput, providerErrorOutput := deliverable.result()

		// Promote completed→failed when stderr or the agent text
		// stream show a terminal upstream-LLM failure (HTTP 4xx /
		// rate-limit / expired token). See the helper docs for the
		// full signal set; the key safety property is that transient
		// per-attempt warnings followed by a successful retry stay
		// "completed". It reads the full text stream, not the
		// deliverable, so a give-up turn that lands before a tool call
		// stays visible.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)
		if question, ok := blockedQuestion.Load().(string); ok && question != "" {
			switch finalStatus {
			case "completed", "failed":
				finalStatus = "failed"
				finalError = question
			}
		}

		u := c.accumulatedUsage()
		statusSnapshot, statusModel := statusUsage.snapshot()
		if !reasonixUsagePresent(u) {
			u = statusSnapshot
		}

		var usageMap map[string]TokenUsage
		if reasonixUsagePresent(u) {
			model := opts.Model
			if model == "" {
				model = statusModel
			}
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

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// reasonixToolNameFromTitle normalises tool names emitted by Reasonix's ACP
// server into the snake_case identifiers the Multica UI expects.
//
// Reasonix follows the ACP spec where `title` is a short human-readable
// label such as "Read file: /path/to/foo.go" or "Run command: ls".
// hermesToolNameFromTitle upstream handles hermes' lowercase
// convention ("read:", "patch (replace)") but not reasonix's capitalised
// format — so we get called on the already-mapped name from hermes
// and fix up anything that slipped through. Empty input returns "".
func reasonixToolNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}

	// Strip everything after the first colon — ACP titles often look like
	// "Tool Name: argument detail" and we want only the tool name.
	if idx := strings.Index(t, ":"); idx > 0 {
		t = strings.TrimSpace(t[:idx])
	}

	lower := strings.ToLower(t)
	switch lower {
	case "read", "read file":
		return "read_file"
	case "write", "write file":
		return "write_file"
	case "edit", "patch":
		return "edit_file"
	case "shell", "bash", "terminal", "run command", "run shell command":
		return "terminal"
	case "search", "grep", "find":
		return "search_files"
	case "glob":
		return "glob"
	case "web search":
		return "web_search"
	case "fetch", "web fetch":
		return "web_fetch"
	case "todo", "todo write":
		return "todo_write"
	}

	// Fallback: snake_case the title so the UI gets a stable identifier.
	return strings.ReplaceAll(lower, " ", "_")
}

var reasonixProtectedPermissionTools = map[string]bool{
	"exit_plan_mode":              true,
	"remember":                    true,
	"forget":                      true,
	"sandbox_escape":              true,
	"config_write":                true,
	"plan_mode_read_only_command": true,
}

type reasonixPermissionRequest struct {
	ToolCall struct {
		ToolCallID string                     `json:"toolCallId"`
		Title      string                     `json:"title"`
		Meta       map[string]json.RawMessage `json:"_meta"`
	} `json:"toolCall"`
	Options []acpPermissionOption `json:"options"`
}

type reasonixPermissionMetadata struct {
	Tool  string `json:"tool"`
	Fresh bool   `json:"fresh"`
}

// selectReasonixPermissionOption prevents two Reasonix-specific request
// classes from falling through the shared "allow_once is safe" rule: user
// questions and decisions that Reasonix explicitly marks fresh-human-only.
func selectReasonixPermissionOption(params json.RawMessage) (optionID string, grant bool, ok bool, blockedQuestion string) {
	var p reasonixPermissionRequest
	if err := json.Unmarshal(params, &p); err != nil {
		return "", false, false, ""
	}

	rejectOnce := func() (string, bool) {
		for _, opt := range p.Options {
			if opt.OptionID != "" && strings.EqualFold(strings.TrimSpace(opt.Kind), acpKindRejectOnce) {
				return opt.OptionID, true
			}
		}
		return "", false
	}

	if reasonixPermissionIsQuestion(p) {
		reason := "Reasonix requested interactive user input, which is unavailable in an unattended Multica task"
		if title := strings.TrimSpace(p.ToolCall.Title); title != "" {
			reason += ": " + clipReasonixPermissionTitle(title)
		}
		if id, found := rejectOnce(); found {
			return id, false, true, reason
		}
		// A protocol error is fail-closed here: current Reasonix treats a
		// failed ask response as unanswered/cancelled instead of proceeding.
		return "", false, false, reason
	}

	meta, _ := reasonixPermissionMeta(p)
	if meta.Fresh || reasonixProtectedPermissionTools[meta.Tool] {
		if id, found := rejectOnce(); found {
			return id, false, true, ""
		}
		return "", false, false, ""
	}

	id, allowed, selectable := selectACPPermissionOption(params)
	return id, allowed, selectable, ""
}

func reasonixPermissionIsQuestion(p reasonixPermissionRequest) bool {
	if strings.HasPrefix(p.ToolCall.ToolCallID, "ask-") {
		return true
	}
	for _, opt := range p.Options {
		if strings.HasSuffix(opt.OptionID, ":cancel") {
			return true
		}
	}
	return false
}

func reasonixPermissionMeta(p reasonixPermissionRequest) (reasonixPermissionMetadata, bool) {
	var meta reasonixPermissionMetadata
	raw, ok := p.ToolCall.Meta["reasonix.io"]
	if !ok || json.Unmarshal(raw, &meta) != nil {
		return reasonixPermissionMetadata{}, false
	}
	meta.Tool = strings.TrimSpace(meta.Tool)
	return meta, meta.Tool != ""
}

func reasonixPermissionMetadataMissing(params json.RawMessage) bool {
	var p reasonixPermissionRequest
	if json.Unmarshal(params, &p) != nil || reasonixPermissionIsQuestion(p) {
		return false
	}
	_, ok := reasonixPermissionMeta(p)
	return !ok
}

func warnReasonixCapabilityGaps(logger *slog.Logger, result json.RawMessage) {
	statusVersion, updateVersion := reasonixStatusCapabilitySchemas(result)
	if statusVersion == 1 && updateVersion == 1 {
		return
	}
	logger.Warn(
		"reasonix ACP status capabilities are unavailable or incompatible; usage and cost reporting may be incomplete",
		"session_status_schema_version", statusVersion,
		"status_update_schema_version", updateVersion,
	)
}

func reasonixStatusCapabilitySchemas(result json.RawMessage) (statusVersion, updateVersion int) {
	var initialized struct {
		AgentCapabilities struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"agentCapabilities"`
	}
	if json.Unmarshal(result, &initialized) != nil {
		return 0, 0
	}
	readVersion := func(key string) int {
		var capability struct {
			SchemaVersion int `json:"schemaVersion"`
		}
		if json.Unmarshal(initialized.AgentCapabilities.Meta[key], &capability) != nil {
			return 0
		}
		return capability.SchemaVersion
	}
	return readVersion("_reasonix.io/session/status"), readVersion("_reasonix.io/session/status_update")
}

func clipReasonixPermissionTitle(title string) string {
	runes := []rune(title)
	if len(runes) <= 240 {
		return title
	}
	return string(runes[:240]) + "…"
}

func isReasonixSessionLeaseConflict(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32600 {
		return false
	}
	text := strings.ToLower(rpcErr.Message + " " + rpcErr.Data)
	return strings.Contains(text, " in use") && strings.Contains(text, "close the other reasonix")
}

func reasonixSessionNewError(err error) string {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "not configured") || strings.Contains(text, "no default_model configured") {
		return fmt.Sprintf("reasonix session/new failed: %v; run `reasonix setup` in the runtime owner's environment and retry", err)
	}
	return fmt.Sprintf("reasonix session/new failed: %v", err)
}

func reasonixRequestFailure(ctx context.Context, timeout time.Duration, fallback string) (string, string) {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		if timeout > 0 {
			return "timeout", fmt.Sprintf("reasonix timed out after %s", timeout)
		}
		return "timeout", "reasonix execution timed out"
	case context.Canceled:
		return "aborted", "execution cancelled"
	default:
		return "failed", fallback
	}
}

type reasonixStatusUsageTracker struct {
	mu       sync.Mutex
	sequence uint64
	usage    TokenUsage
	model    string
}

func (t *reasonixStatusUsageTracker) observe(method string, params json.RawMessage) {
	if method != "_reasonix.io/session/status_update" {
		return
	}
	var update struct {
		SchemaVersion int    `json:"schemaVersion"`
		Sequence      uint64 `json:"sequence"`
		Status        struct {
			Model string `json:"model"`
			Usage struct {
				Turn struct {
					PromptTokens     int64    `json:"promptTokens"`
					CompletionTokens int64    `json:"completionTokens"`
					CacheHitTokens   int64    `json:"cacheHitTokens"`
					CacheMissTokens  int64    `json:"cacheMissTokens"`
					EstimatedCost    *float64 `json:"estimatedCost"`
					Currency         string   `json:"currency"`
				} `json:"turn"`
			} `json:"usage"`
		} `json:"status"`
	}
	if err := json.Unmarshal(params, &update); err != nil || update.SchemaVersion != 1 {
		return
	}

	turn := update.Status.Usage.Turn
	input := turn.CacheMissTokens
	if input == 0 {
		input = turn.PromptTokens - turn.CacheHitTokens
		if input < 0 {
			input = turn.PromptTokens
		}
	}
	u := TokenUsage{
		InputTokens:     input,
		OutputTokens:    turn.CompletionTokens,
		CacheReadTokens: turn.CacheHitTokens,
	}
	if turn.EstimatedCost != nil && isReasonixUSDCurrency(turn.Currency) &&
		*turn.EstimatedCost >= 0 && !math.IsNaN(*turn.EstimatedCost) && !math.IsInf(*turn.EstimatedCost, 0) &&
		*turn.EstimatedCost <= float64(1<<63-1)/float64(CostUSDTicksPerUSD) {
		u.CostUSDTicks = int64(math.Round(*turn.EstimatedCost * float64(CostUSDTicksPerUSD)))
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if update.Sequence < t.sequence {
		return
	}
	t.sequence = update.Sequence
	t.usage = u
	t.model = strings.TrimSpace(update.Status.Model)
}

func (t *reasonixStatusUsageTracker) snapshot() (TokenUsage, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage, t.model
}

func isReasonixUSDCurrency(currency string) bool {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USD", "$", "US$":
		return true
	default:
		return false
	}
}

func reasonixUsagePresent(u TokenUsage) bool {
	return acpTokenUsagePresent(u) || u.CostUSDTicks > 0
}
