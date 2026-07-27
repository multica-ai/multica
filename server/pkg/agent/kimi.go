package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// kimiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport for Kimi Code CLI;
// overriding it would break the daemon↔Kimi communication contract.
var kimiBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

// kimiBackend implements Backend by spawning `kimi acp` and communicating
// via the ACP (Agent Client Protocol) JSON-RPC 2.0 over stdin/stdout.
//
// Kimi Code CLI (https://github.com/MoonshotAI/kimi-cli) supports ACP out of
// the box via the `kimi acp` subcommand. We reuse the existing hermesClient
// ACP transport since both runtimes speak the same protocol — only the
// binary, env, and tool-name extraction differ.
type kimiBackend struct {
	cfg Config
}

func (b *kimiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "kimi"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("kimi executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP `session/new` expects. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of
	// silently dropping all MCP servers.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("kimi: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// `kimi acp` ignores --yolo / --auto-approve (they're flags on the
	// root `kimi` command, not on the `acp` subcommand). Instead, the
	// daemon auto-approves in hermesClient.handleAgentRequest by selecting
	// a safe granting option the agent offered (see
	// selectACPApprovalOptionID) for each session/request_permission request.
	kimiArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, kimiBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, kimiArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", kimiArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	// Resolve the effective Kimi data directory from the child-process
	// environment. The daemon's own KIMI_CODE_HOME may differ (custom_env,
	// task-local overrides), and usage/failure recovery must read the files
	// the child actually writes.
	kimiHome := kimiHomeFromEnv(cmd.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stdin pipe: %w", err)
	}
	// Forward stderr to the daemon log *and* sniff provider-level
	// errors out of it so we can surface them in the task result.
	// Kimi's session/prompt still reports stopReason=end_turn when
	// the underlying HTTP call to api.kimi.com returns 4xx/5xx, so
	// without this the daemon reports a misleading "empty output"
	// and the actionable error (expired token, rate limit, upstream
	// 5xx, …) stays buried in the daemon log.
	//
	// StderrPipe + an explicit copier give us a join point
	// (`stderrDone`) that fires before the failure-promotion
	// decision; see the matching comment in hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("kimi")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start kimi: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[kimi:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("kimi acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	promptDone := make(chan hermesPromptResult, 1)

	// Reuse the hermesClient ACP transport — Kimi speaks the same protocol.
	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		onMessage: func(msg Message) {
			// hermesClient.handleToolCallStart has already mapped
			// the raw ACP title via hermesToolNameFromTitle — which
			// covers lowercase hermes-style titles ("read:", "patch
			// (replace)", …) but not capitalised kimi-style ones
			// ("Read file: …", "Run command: …"). Re-normalise so
			// the UI sees consistent snake_case identifiers across
			// both backends. No-op when the name is already normal
			// form (e.g. already mapped to "read_file").
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
		c.closeAllPending(fmt.Errorf("kimi process exited"))
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
		// Per-model usage recovered from kimi's on-disk session wire
		// when the ACP stream carries none (always, on kimi ≤ 0.27).
		var wireUsage map[string]TokenUsage
		// Largest usage.record timestamp already present in the session
		// wire right after session/new or session/resume. Only records
		// strictly after this snapshot are counted, preventing double-
		// billing on resumed sessions.
		var wireSnapshotMs int64 = -1

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
			finalStatus = "failed"
			finalError = fmt.Sprintf("kimi initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for the why —
		// shipping an http/sse entry to a stdio-only runtime tanks the
		// whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "kimi", b.cfg.Logger)

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			// Per ACP Session Setup, session/resume accepts mcpServers and
			// the runtime re-connects them as part of the resume. Without
			// this, a resumed Kimi task lost access to MCP tools that a
			// fresh task on the same agent would have.
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi session/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "kimi",
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
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "kimi session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("kimi session created", "session_id", sessionID)

		// Snapshot the session wire before the prompt so usage and failure
		// recovery only counts records appended by this turn. On a resumed
		// session the wire holds every past turn; without the snapshot we
		// would double-bill tokens already billed to earlier tasks.
		wireSnapshotMs = snapshotKimiWire(sessionID, kimiHome, b.cfg.Logger)

		// 3. If the caller picked a model (via agent.model from the
		// UI dropdown), ask kimi to switch the session to it before
		// we send any prompt. Kimi's ACP server exposes
		// `session/set_model` and advertises available models via
		// `configOptions` (kimi-code ≥ 0.29) or the legacy
		// `models.availableModels` block returned by `session/new` —
		// we pass the chosen modelId through verbatim. This MUST fail
		// the task on error: silently falling back to kimi's default
		// model would let the user believe their pick was honoured
		// while the task actually ran on something else.
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("kimi set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so
					// the daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "kimi",
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
			b.cfg.Logger.Info("kimi session model set", "model", opts.Model)
		}

		// 3b. If the caller picked a thinking level, ask kimi to set it
		// via session/set_config_option before the prompt. The runtime
		// advertises the control as the configOptions select with
		// category "thought_level" and id "thinking". Like set_model,
		// this fails the task on error so a persisted thinking_level
		// that the runtime rejects doesn't silently fall back.
		if opts.ThinkingLevel != "" {
			if _, err := c.request(runCtx, "session/set_config_option", map[string]any{
				"sessionId": sessionID,
				"configId":  "thinking",
				"value":     opts.ThinkingLevel,
			}); err != nil {
				b.cfg.Logger.Warn("kimi set_config_option/thinking failed", "error", err, "requested_level", opts.ThinkingLevel)
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi could not set thinking level %q: %v", opts.ThinkingLevel, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at set_config_option time; clearing session id so the daemon retries fresh",
						"backend", "kimi",
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
			b.cfg.Logger.Info("kimi session thinking level set", "level", opts.ThinkingLevel)
		}

		// 4. Build the prompt content. If we have a system prompt, prepend it.
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// 5. Send the prompt and wait for PromptResponse.
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("kimi timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes backend: the runtime echoes the
					// requested id back from session/resume even when
					// the session is gone, so the stale id only fails
					// here, at prompt time. Empty SessionID lets the
					// daemon's resume-failure fallback retry fresh and
					// store the replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "kimi",
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
					finalError = "kimi cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}

			// Kimi answers session/prompt *before* flushing the turn's
			// usage.record to the session wire, and the teardown below
			// SIGKILLs the process — without this pause the record is
			// lost and the run reports no usage. Poll briefly while the
			// process is still alive, but only when the ACP stream
			// itself carried no usage (kimi ≤ 0.27 never sends any).
			c.usageMu.Lock()
			acpUsageEmpty := c.usage.InputTokens == 0 && c.usage.OutputTokens == 0 &&
				c.usage.CacheReadTokens == 0 && c.usage.CacheWriteTokens == 0
			c.usageMu.Unlock()
			if acpUsageEmpty && sessionID != "" && finalStatus == "completed" {
				wireUsage = waitKimiWireUsage(sessionID, time.UnixMilli(wireSnapshotMs), kimiHome, b.cfg.Logger)
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("kimi finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		// Ensure the stderr copier has drained before consulting the
		// provider-error sniffer; see hermes.go for the failure mode.
		<-stderrDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Promote completed→failed when stderr or the agent text
		// stream show a terminal upstream-LLM failure (HTTP 4xx /
		// rate-limit / expired token). See the helper docs for the
		// full signal set; the key safety property is that transient
		// per-attempt warnings followed by a successful retry stay
		// "completed".
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		} else {
			// Kimi (verified on 0.27.0) reports no usage over ACP at
			// all: session/prompt returns only stopReason and no
			// usage_update notifications flow. The per-turn usage lands
			// in the session's wire.jsonl instead, recovered above so
			// the run report carries the real model id and token counts
			// (and therefore cost).
			usageMap = wireUsage
			if usageMap == nil && sessionID != "" && finalStatus != "completed" {
				// Failed/aborted/timeout runs skipped the polling wait
				// above (only worthwhile on the completed path, where a
				// flush is imminent). By now the process is dead and the
				// wire holds whatever it will hold, so a single
				// non-polling read recovers the partial turn's usage
				// with no added latency. The daemon bills usage even
				// for cancelled tasks (see the ReportTaskUsage comment
				// in server/internal/daemon/daemon.go), so the report
				// should carry what the run consumed. Still nil when
				// nothing usable was found — the run then reports no
				// usage, matching prior behavior.
				usageMap, _ = readKimiWireUsage(sessionID, time.UnixMilli(wireSnapshotMs), kimiHome, b.cfg.Logger)
			}
		}

		// Kimi answers session/prompt with stopReason=end_turn even when
		// the turn died provider-side (upstream kimi bug; the failure is
		// only recorded in the session's logs/kimi-code.log). A
		// "completed" run with no output and no ACP-stream usage is
		// therefore not a valid empty completion: cross-check the session
		// log for an in-window turn failure and fail loudly with the real
		// provider error instead of reporting a silent success. The
		// session id is dropped as well — a failed turn can leave the
		// context corrupt (empty assistant message) and brick every
		// follow-up resume, so the empty SessionID routes the daemon's
		// resume-failure fallback to a fresh session. Healthy empty
		// completions flushed a usage.record; the snapshot keeps stale
		// failure entries from previous runs out of the check.
		if finalStatus == "completed" && finalOutput == "" && sessionID != "" &&
			u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 && u.CacheWriteTokens == 0 {
			if failMsg, failed := readKimiTurnFailure(sessionID, time.UnixMilli(wireSnapshotMs), kimiHome, b.cfg.Logger); failed {
				b.cfg.Logger.Warn("kimi turn failed provider-side behind end_turn; failing the run and dropping the poisoned session",
					"session_id", sessionID,
					"error", failMsg,
				)
				finalStatus = "failed"
				finalError = failMsg
				sessionID = ""
			}
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

// kimiToolNameFromTitle normalises tool names emitted by Kimi's ACP
// server into the snake_case identifiers the Multica UI expects.
//
// Kimi follows the ACP spec where `title` is a short human-readable
// label such as "Read file: /path/to/foo.go" or "Run command: ls".
// hermesToolNameFromTitle upstream handles hermes' lowercase
// convention ("read:", "patch (replace)") but not kimi's capitalised
// format — so we get called on the already-mapped name from hermes
// and fix up anything that slipped through. Empty input returns "".
func kimiToolNameFromTitle(title string) string {
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

// kimiWireUsagePoll bounds the post-prompt wait for kimi to flush the
// turn's usage.record to the session wire. Kimi answers session/prompt
// before the record hits disk; without a brief poll the teardown SIGKILL
// lands first and usage is lost. Typical flush is well under a second.
const (
	kimiWireUsagePollTimeout  = 2 * time.Second
	kimiWireUsagePollInterval = 100 * time.Millisecond
)

const (
	// kimiLineBufSize is the chunk size used when scanning wire/log files.
	// It is large enough to absorb most lines in one ReadSlice but small
	// enough that a 5 MiB tool-output frame is never held whole.
	kimiLineBufSize = 64 * 1024
	// kimiMaxTargetLineBytes caps how much of a target line we are willing
	// to buffer. Usage records and turn-failure log lines are well under
	// this; oversized target lines are skipped rather than risking OOM.
	kimiMaxTargetLineBytes = 1024 * 1024
)

// kimiReadLineBounded reads one line from r. If the line does not look like
// a target line according to isTarget, it is discarded chunk by chunk without
// ever buffering the whole line. Target lines are buffered only up to
// kimiMaxTargetLineBytes; anything beyond that is discarded.
func kimiReadLineBounded(r *bufio.Reader, isTarget func([]byte) bool) ([]byte, error) {
	chunk, err := r.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		return chunk, err
	}
	// Line longer than the internal buffer. Decide from the leading chunk
	// whether this is a line we care about.
	if !isTarget(chunk) {
		for err == bufio.ErrBufferFull {
			chunk, err = r.ReadSlice('\n')
		}
		return nil, err
	}
	// Target line that exceeds the buffer: accumulate up to the cap.
	var buf bytes.Buffer
	buf.Grow(len(chunk))
	buf.Write(chunk)
	for err == bufio.ErrBufferFull {
		chunk, err = r.ReadSlice('\n')
		if buf.Len()+len(chunk) > kimiMaxTargetLineBytes {
			// Too big: discard the remainder and treat as skipped.
			for err == bufio.ErrBufferFull {
				chunk, err = r.ReadSlice('\n')
			}
			return nil, err
		}
		buf.Write(chunk)
	}
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf.Bytes(), err
}

// waitKimiWireUsage polls readKimiWireUsage until the turn's usage
// totals settle — non-empty and unchanged across one more poll interval —
// or the poll budget runs out, in which case the latest read wins (nil
// when nothing was ever found).
func waitKimiWireUsage(sessionID string, since time.Time, home string, logger *slog.Logger) map[string]TokenUsage {
	// Fast path: kimi creates the session wire at session start, long
	// before this post-prompt poll. A first probe matching no wire file
	// at all therefore means this build writes none (older CLI, custom
	// data dir) — bail now instead of burning the whole budget per run.
	usage, foundWire := readKimiWireUsage(sessionID, since, home, logger)
	if !foundWire {
		return nil
	}

	// Settle rather than returning on the first non-empty read: the main
	// agent's record can land a beat before a sub-agent's, and the
	// per-model totals only stop growing once every agent wire for this
	// session has flushed. On budget expiry return the latest read —
	// partial usage still beats none.
	deadline := time.Now().Add(kimiWireUsagePollTimeout)
	for !time.Now().After(deadline) {
		time.Sleep(kimiWireUsagePollInterval)
		prev := usage
		usage, _ = readKimiWireUsage(sessionID, since, home, logger)
		if usage != nil && maps.Equal(usage, prev) {
			return usage
		}
	}
	return usage
}

// readKimiWireUsage recovers per-model token usage from kimi's on-disk
// session wire (<KIMI_CODE_HOME|~/.kimi-code>/sessions/<cwd-hash>/<sessionID>/agents/*/wire.jsonl).
// Kimi appends one record per turn:
//
//	{"type":"usage.record","model":"kimi-code/k3","usageScope":"turn",
//	 "usage":{"inputOther":1884,"output":35,"inputCacheRead":19200,"inputCacheCreation":0},
//	 "time":1784398522242}
//
// Only records strictly after `since` count: a resumed session's wire
// accumulates every past turn, and re-summing history would double-report
// tokens already billed to earlier tasks. The caller snapshots the wire
// right after session/new or session/resume and passes that snapshot as
// `since`, so previous task records are ignored regardless of clock skew.
// Records are summed per model so multi-model runs attribute correctly.
// The `home` argument must be the effective Kimi data directory (resolved
// from the child-process environment by the caller). The second return value
// reports whether any wire file matched at all — callers use it to tell
// "this kimi build writes no wire" from "wire exists but the turn's record
// isn't flushed yet". Totals are nil when nothing usable is found — the
// caller then reports no usage, matching the pre-recovery behavior.
func readKimiWireUsage(sessionID string, since time.Time, home string, logger *slog.Logger) (map[string]TokenUsage, bool) {
	if home == "" {
		return nil, false
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*", sessionID, "agents", "*", "wire.jsonl"))
	if err != nil || len(files) == 0 {
		return nil, false
	}
	cutoff := since.UnixMilli()
	totals := map[string]TokenUsage{}
	for _, f := range files {
		if err := accumulateKimiWireFile(f, cutoff, totals); err != nil {
			logger.Debug("kimi wire usage read failed", "file", f, "error", err)
		}
	}
	if len(totals) == 0 {
		return nil, true
	}
	return totals, true
}

// accumulateKimiWireFile sums usage.record entries strictly after cutoffMs
// into totals, keyed by the record's model id. Malformed lines are
// skipped — the wire is an append-only log best read leniently.
//
// Non-target lines are discarded chunk-by-chunk so a huge tool-output frame
// never gets buffered whole; target lines are buffered only up to
// kimiMaxTargetLineBytes.
func accumulateKimiWireFile(path string, cutoffMs int64, totals map[string]TokenUsage) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	r := bufio.NewReaderSize(fh, kimiLineBufSize)
	for {
		line, err := kimiReadLineBounded(r, isUsageRecordLine)
		if len(line) > 0 {
			accumulateKimiWireLine(line, cutoffMs, totals)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// isUsageRecordLine reports whether a (possibly partial) line looks like a
// usage.record entry. The marker sits near the start, so a leading chunk is
// enough to decide.
func isUsageRecordLine(line []byte) bool {
	return bytes.Contains(line, []byte(`"usage.record"`))
}

// accumulateKimiWireLine folds one wire line into totals when it is a
// usage.record strictly after cutoffMs; anything else is ignored.
func accumulateKimiWireLine(line []byte, cutoffMs int64, totals map[string]TokenUsage) {
	// Cheap pre-filter: almost no wire line is a usage record, and
	// lines carrying tool output can be large.
	if !bytes.Contains(line, []byte(`"usage.record"`)) {
		return
	}
	var rec struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Usage struct {
			InputOther         int64 `json:"inputOther"`
			Output             int64 `json:"output"`
			InputCacheRead     int64 `json:"inputCacheRead"`
			InputCacheCreation int64 `json:"inputCacheCreation"`
		} `json:"usage"`
		Time int64 `json:"time"` // epoch ms
	}
	if err := json.Unmarshal(line, &rec); err != nil || rec.Type != "usage.record" {
		return
	}
	model := strings.TrimSpace(rec.Model)
	if model == "" || rec.Time <= cutoffMs {
		return
	}
	u := totals[model]
	u.InputTokens += rec.Usage.InputOther
	u.OutputTokens += rec.Usage.Output
	u.CacheReadTokens += rec.Usage.InputCacheRead
	u.CacheWriteTokens += rec.Usage.InputCacheCreation
	totals[model] = u
}

// kimiHomeFromEnv extracts KIMI_CODE_HOME from an env slice (last wins),
// falling back to ~/.kimi-code. This lets the kimi backend read session
// files from the same directory the child process writes them to, even when
// the daemon's own environment points elsewhere.
func kimiHomeFromEnv(env []string) string {
	prefix := "KIMI_CODE_HOME="
	var home string
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			home = strings.TrimPrefix(e, prefix)
		}
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".kimi-code")
		}
	}
	return home
}

// snapshotKimiWire returns the largest usage.record timestamp (epoch ms)
// already present in the session's wire files right after session/new or
// session/resume. The caller passes this value back as the `since` cutoff
// for readKimiWireUsage and readKimiTurnFailure, so only records appended
// after the snapshot are counted.
//
// When the session has no wire file yet (fresh session, older build) or
// the wire holds no usage.record, the snapshot falls back to the current
// time. That keeps stale failure log entries from previous runs from
// looking in-window when there is no usage record to anchor the cutoff.
func snapshotKimiWire(sessionID string, home string, logger *slog.Logger) int64 {
	if home == "" {
		return time.Now().UnixMilli()
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*", sessionID, "agents", "*", "wire.jsonl"))
	if err != nil || len(files) == 0 {
		return time.Now().UnixMilli()
	}
	var maxTime int64 = -1
	for _, f := range files {
		t, err := maxKimiWireRecordTime(f)
		if err != nil {
			logger.Debug("kimi wire snapshot read failed", "file", f, "error", err)
			continue
		}
		if t > maxTime {
			maxTime = t
		}
	}
	if maxTime < 0 {
		return time.Now().UnixMilli()
	}
	return maxTime
}

// maxKimiWireRecordTime returns the largest usage.record timestamp in a
// single wire file, or -1 when the file contains no usage.record lines.
func maxKimiWireRecordTime(path string) (int64, error) {
	fh, err := os.Open(path)
	if err != nil {
		return -1, err
	}
	defer fh.Close()
	var maxTime int64 = -1
	r := bufio.NewReaderSize(fh, kimiLineBufSize)
	for {
		line, err := kimiReadLineBounded(r, isUsageRecordLine)
		if len(line) > 0 {
			if t := kimiWireRecordTime(line); t > maxTime {
				maxTime = t
			}
		}
		if err != nil {
			if err == io.EOF {
				return maxTime, nil
			}
			return maxTime, err
		}
	}
}

// kimiWireRecordTime extracts the timestamp from a usage.record line, or
// -1 if the line is not a usage.record or is malformed.
func kimiWireRecordTime(line []byte) int64 {
	if !bytes.Contains(line, []byte(`"usage.record"`)) {
		return -1
	}
	var rec struct {
		Type string `json:"type"`
		Time int64  `json:"time"`
	}
	if err := json.Unmarshal(line, &rec); err != nil || rec.Type != "usage.record" {
		return -1
	}
	return rec.Time
}

// Markers recorded in the session's logs/kimi-code.log when a turn dies
// provider-side while ACP still reports stopReason=end_turn (upstream
// kimi bug; see M-95). The ERROR line marks the failure, the WARN line
// carries the provider error payload:
//
//	2026-07-19T15:03:24.931Z ERROR turn failed  turnId=1
//	2026-07-19T15:03:24.933Z WARN  acp: turn ended with failed reason  error="{...}"
const (
	kimiTurnFailedMarker       = "turn failed"
	kimiTurnFailedErrorPrefix  = " ERROR turn failed"
	kimiTurnFailedDetailMarker = "acp: turn ended with failed reason"
)

// kimiTurnFailedErrorRe extracts the slog-style quoted error attribute
// from the detail line: `error="{\"code\":\"provider.api_error\",...}"`.
var kimiTurnFailedErrorRe = regexp.MustCompile(`\berror="((?:[^"\\]|\\.)*)"`)

// readKimiTurnFailure cross-checks the session's logs/kimi-code.log (same
// directory layout as the wire: sessions/<cwd-hash>/<sessionID>/logs/) for
// a turn failure recorded at or after `since`, returning a message that
// surfaces the provider error. It exists because kimi reports failed
// turns over ACP as stopReason=end_turn with no output, so the session
// log is the only place the failure — and its cause — is visible.
//
// The caller snapshots the wire right after session/new or session/resume
// and passes that snapshot as `since`, so failures from previous runs are
// ignored. Lines whose timestamp can't be parsed are skipped for the same
// reason. Returns ("", false) when no log exists at all (older kimi builds,
// custom data dir) — no signal, never a false positive.
func readKimiTurnFailure(sessionID string, since time.Time, home string, logger *slog.Logger) (string, bool) {
	if home == "" {
		return "", false
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*", sessionID, "logs", "kimi-code.log"))
	if err != nil || len(files) == 0 {
		return "", false
	}
	failed := false
	detail := ""
	for _, f := range files {
		fileDetail, fileFailed, err := scanKimiTurnFailureLog(f, since)
		if err != nil {
			logger.Debug("kimi turn-failure log read failed", "file", f, "error", err)
			continue
		}
		if fileFailed {
			failed = true
			if fileDetail != "" {
				detail = fileDetail
			}
		}
	}
	if !failed {
		return "", false
	}
	const prefix = "kimi turn failed provider-side (acp reported end_turn)"
	if detail == "" {
		return prefix + "; see the session's logs/kimi-code.log", true
	}
	return prefix + ": " + detail, true
}

// scanKimiTurnFailureLog reads one kimi-code.log and reports whether it
// holds an in-window turn failure, plus the extracted provider error
// detail from the most recent matching WARN line ("" when only the bare
// ERROR marker matched). Non-target lines are discarded chunk-by-chunk so
// an oversized line can't hide failure entries appended after it.
func scanKimiTurnFailureLog(path string, cutoff time.Time) (string, bool, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = fh.Close() }()
	found := false
	detail := ""
	r := bufio.NewReaderSize(fh, kimiLineBufSize)
	for {
		line, err := kimiReadLineBounded(r, isKimiTurnFailureLine)
		if len(line) > 0 {
			if d, ok := parseKimiTurnFailureLine(line, cutoff); ok {
				found = true
				if d != "" {
					detail = d
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return detail, found, err
		}
	}
	return detail, found, nil
}

// isKimiTurnFailureLine reports whether a (possibly partial) line looks like
// a turn-failure marker. Both markers appear near the start of their lines;
// the bare ERROR marker requires the level prefix so echoed content does not
// false-match.
func isKimiTurnFailureLine(line []byte) bool {
	return bytes.Contains(line, []byte(kimiTurnFailedErrorPrefix)) ||
		bytes.Contains(line, []byte(kimiTurnFailedDetailMarker))
}

// parseKimiTurnFailureLine reports whether one kimi-code.log line records
// a turn failure at or after cutoff. Lines are Go-slog text
// ("<rfc3339> <LEVEL> <msg> key=value …"); the leading timestamp decides
// whether the entry belongs to the current run.
func parseKimiTurnFailureLine(line []byte, cutoff time.Time) (string, bool) {
	hasErrorMarker := bytes.Contains(line, []byte(kimiTurnFailedErrorPrefix))
	hasDetailMarker := bytes.Contains(line, []byte(kimiTurnFailedDetailMarker))
	if !hasErrorMarker && !hasDetailMarker {
		return "", false
	}
	sp := bytes.IndexByte(line, ' ')
	if sp <= 0 {
		return "", false
	}
	ts, err := time.Parse(time.RFC3339Nano, string(line[:sp]))
	if err != nil || ts.Before(cutoff) {
		return "", false
	}
	if !hasDetailMarker {
		// Bare ERROR marker; the paired WARN detail line carries the cause.
		return "", true
	}
	return extractKimiTurnFailureDetail(line), true
}

// extractKimiTurnFailureDetail pulls the provider error out of the WARN
// detail line's error attribute — a quoted JSON payload such as
// {"code":"provider.api_error","message":"400 the message at position 168
// ..."}. Returns "code: message" (or just the message), "" when the
// attribute is missing or malformed.
func extractKimiTurnFailureDetail(line []byte) string {
	m := kimiTurnFailedErrorRe.FindSubmatch(line)
	if m == nil {
		return ""
	}
	raw, err := strconv.Unquote(`"` + string(m[1]) + `"`)
	if err != nil {
		return ""
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	switch {
	case payload.Code != "" && payload.Message != "":
		return payload.Code + ": " + payload.Message
	default:
		return payload.Message
	}
}
