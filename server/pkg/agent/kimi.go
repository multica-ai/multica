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

		// 3. If the caller picked a model (via agent.model from the
		// UI dropdown), ask kimi to switch the session to it before
		// we send any prompt. Kimi's ACP server exposes
		// `session/set_model` and advertises available models via
		// the `models.availableModels` block returned by
		// `session/new` — we pass the chosen modelId through
		// verbatim. This MUST fail the task on error: silently
		// falling back to kimi's default model would let the user
		// believe their pick was honoured while the task actually
		// ran on something else.
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
				wireUsage = waitKimiWireUsage(sessionID, startTime, b.cfg.Logger)
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
				usageMap, _ = readKimiWireUsage(sessionID, startTime, b.cfg.Logger)
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

// kimiWireUsageGrace absorbs clock granularity between the daemon's run
// start and kimi's record timestamps (same host, same clock). It stays
// far below the age of any previous task's records on a resumed session.
const kimiWireUsageGrace = 5 * time.Second

// kimiWireUsagePoll bounds the post-prompt wait for kimi to flush the
// turn's usage.record to the session wire. Kimi answers session/prompt
// before the record hits disk; without a brief poll the teardown SIGKILL
// lands first and usage is lost. Typical flush is well under a second.
const (
	kimiWireUsagePollTimeout  = 2 * time.Second
	kimiWireUsagePollInterval = 100 * time.Millisecond
)

// waitKimiWireUsage polls readKimiWireUsage until the turn's usage
// totals settle — non-empty and unchanged across one more poll interval —
// or the poll budget runs out, in which case the latest read wins (nil
// when nothing was ever found).
func waitKimiWireUsage(sessionID string, since time.Time, logger *slog.Logger) map[string]TokenUsage {
	// Fast path: kimi creates the session wire at session start, long
	// before this post-prompt poll. A first probe matching no wire file
	// at all therefore means this build writes none (older CLI, custom
	// data dir) — bail now instead of burning the whole budget per run.
	usage, foundWire := readKimiWireUsage(sessionID, since, logger)
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
		usage, _ = readKimiWireUsage(sessionID, since, logger)
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
// Only records at or after `since` count: a resumed session's wire
// accumulates every past turn, and re-summing history would double-report
// tokens already billed to earlier tasks. Records are summed per model so
// multi-model runs attribute correctly. The KIMI_CODE_HOME lookup uses the
// daemon process env, which is what the child inherits in the common case.
// The second return value reports whether any wire file matched at all —
// callers use it to tell "this kimi build writes no wire" from "wire
// exists but the turn's record isn't flushed yet". Totals are nil when
// nothing usable is found — the caller then reports no usage, matching
// the pre-recovery behavior.
func readKimiWireUsage(sessionID string, since time.Time, logger *slog.Logger) (map[string]TokenUsage, bool) {
	home := os.Getenv("KIMI_CODE_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".kimi-code")
		}
	}
	if home == "" {
		return nil, false
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*", sessionID, "agents", "*", "wire.jsonl"))
	if err != nil || len(files) == 0 {
		return nil, false
	}
	cutoff := since.Add(-kimiWireUsageGrace).UnixMilli()
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

// accumulateKimiWireFile sums usage.record entries newer than cutoffMs
// into totals, keyed by the record's model id. Malformed lines are
// skipped — the wire is an append-only log best read leniently.
//
// Lines are read with an unbounded bufio.Reader on purpose: tool-output
// frames on this log can exceed any fixed scanner buffer, and a capped
// reader would stop at the oversized line and hide usage.record entries
// appended after it.
func accumulateKimiWireFile(path string, cutoffMs int64, totals map[string]TokenUsage) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	r := bufio.NewReader(fh)
	for {
		line, err := r.ReadBytes('\n')
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

// accumulateKimiWireLine folds one wire line into totals when it is an
// in-window usage.record; anything else is ignored.
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
	if model == "" || rec.Time < cutoffMs {
		return
	}
	u := totals[model]
	u.InputTokens += rec.Usage.InputOther
	u.OutputTokens += rec.Usage.Output
	u.CacheReadTokens += rec.Usage.InputCacheRead
	u.CacheWriteTokens += rec.Usage.InputCacheCreation
	totals[model] = u
}
