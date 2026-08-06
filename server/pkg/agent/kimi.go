package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// kimiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport for Kimi Code CLI;
// overriding it would break the daemon↔Kimi communication contract.
var kimiBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

// kimiReaderDrainGrace bounds how long the turn waits for trailing ACP
// notifications after the session/prompt response. A var, not a const, so
// tests can shorten it. Mirrors qoderReaderDrainGrace / traecliReaderDrainGrace.
var kimiReaderDrainGrace = 2 * time.Second

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

	// Kimi streams interim narration and the final answer as the same
	// agent_message_chunk type; the tracker keeps only the post-tool-call block
	// for Result.Output while retaining the full text for error detection.
	var deliverable acpDeliverableTracker
	// streamingCurrentTurn gates all session updates so that history replay
	// (Kimi sends full prior-turn transcripts on session/resume, and may
	// flush queued chunks before our session/prompt response streams) is
	// dropped instead of duplicating the previous answer into output. We
	// flip it to true only after session/prompt is sent.
	var streamingCurrentTurn atomic.Bool

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	// Reuse the hermesClient ACP transport — Kimi speaks the same protocol.
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
				// kimi-code 0.33.0 sends no usage at all on this path,
				// so this is inert today; it exists so a future
				// kimi that does populate the ACP result is billed
				// correctly instead of silently dropping cache or cost
				// fields. scanKimiSessionUsage below is the live path.
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, kimiReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("kimi finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

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

		u := c.accumulatedUsage()

		// Fallback: kimi-code 0.33.0 exports no token counters over ACP.
		// Its session/prompt result carries only stopReason, and its
		// `usage_update` notification carries only {used,size} — context
		// window occupancy, not billing. Verified against the CLI Multica's
		// own onboarding installs. Without this scan every kimi task lands
		// on the usage dashboard with no row at all (MUL-5773 / #6448).
		//
		// The counters do exist in kimi's per-session wire log, so read
		// them from there, the same way codex.go falls back to Codex
		// rollouts. Bound by sessionID so a concurrent kimi session is
		// never billed to this task.
		fallbackModel := opts.Model
		if fallbackModel == "" {
			fallbackModel = "unknown"
		}

		var usageMap map[string]TokenUsage
		if acpTokenUsagePresent(u) {
			usageMap = map[string]TokenUsage{fallbackModel: u}
		} else {
			// Keyed per model by the scan itself: a task whose subagent
			// or compaction step ran on a different model must not have
			// that model's tokens priced at the main model's rate.
			usageMap = scanKimiSessionUsage(kimiUsageScan{
				startTime:     startTime,
				kimiHome:      b.cfg.Env["KIMI_CODE_HOME"],
				sessionID:     sessionID,
				resumed:       opts.ResumeSessionID != "",
				fallbackModel: fallbackModel,
			})
			// A future Kimi build may expose provider cost before it exposes
			// token buckets over ACP. Preserve that cost alongside the current
			// wire-log fallback instead of dropping either source.
			if u.CostUSDTicks > 0 {
				modelUsage := usageMap[fallbackModel]
				modelUsage.CostUSDTicks = u.CostUSDTicks
				if usageMap == nil {
					usageMap = make(map[string]TokenUsage)
				}
				usageMap[fallbackModel] = modelUsage
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

// kimiUsageRecordType is the wire-log entry kimi-code writes after every LLM
// call. Its sibling `step.end` loop event repeats the same numbers, so only
// this type may be summed — counting both double-bills every turn.
const kimiUsageRecordType = "usage.record"

// kimiWireUsage is one `usage.record` entry from a kimi session wire log.
//
// The buckets are mutually exclusive: on a verified two-turn session the
// records read {inputOther:4844, inputCacheRead:17664, output:22} and
// {inputOther:272, inputCacheRead:22272, output:22}, and each turn's three
// fields sum exactly to the `used` figure kimi reports over ACP (22530 and
// 22566). So `inputOther` is uncached input only and maps straight onto
// TokenUsage.InputTokens with no cached-prefix subtraction — unlike the ACP
// `inputTokens` case excludeACPCachedInput exists to correct.
type kimiWireUsage struct {
	Type string `json:"type"`
	// Time is the record's epoch-millisecond write time. It is what keeps a
	// resumed session from being billed twice: kimi appends to the same wire
	// log across resumes, so the file still holds the previous task's
	// records.
	Time  int64  `json:"time"`
	Model string `json:"model"`
	Usage struct {
		InputOther         int64 `json:"inputOther"`
		Output             int64 `json:"output"`
		InputCacheRead     int64 `json:"inputCacheRead"`
		InputCacheCreation int64 `json:"inputCacheCreation"`
	} `json:"usage"`
}

// kimiUsageScan parameterises a wire-log scan.
type kimiUsageScan struct {
	startTime time.Time
	kimiHome  string
	sessionID string
	// resumed marks a turn that continued an existing kimi session. Those
	// append to the wire log the previous task already billed, so records are
	// filtered by their own timestamp, not just by the file's mtime.
	resumed bool
	// fallbackModel keys records that carry no model of their own.
	fallbackModel string
}

// scanKimiSessionUsage returns the usage kimi-code recorded for this session,
// bucketed by the model that produced each record.
//
// Records are per-LLM-call and incremental, never cumulative: a verified
// two-step turn logged one record per `llm.request`, with the second call's
// uncached input far below the first's as the prefix moved into cache. So the
// total for a task is the plain sum over every record in the session.
//
// Subagent turns get their own `agents/<name>/wire.jsonl` and may run a
// different model, which is why the result is a map rather than one total:
// billing only `agents/main` would drop delegated work, and folding every
// agent into one model name would price it wrong.
func scanKimiSessionUsage(scan kimiUsageScan) map[string]TokenUsage {
	root := kimiSessionRoot(scan.kimiHome)
	if root == "" {
		return nil
	}

	usage := make(map[string]TokenUsage)
	for _, path := range kimiSessionWireLogs(root, scan.sessionID) {
		// A wire log untouched since before the turn began belongs to an
		// earlier run of a resumed session; billing it would re-charge
		// tokens the previous task already reported.
		if info, err := os.Stat(path); err != nil || info.ModTime().Before(scan.startTime) {
			continue
		}
		accumulateKimiWireUsage(usage, path, scan)
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

// kimiSessionWireLogs returns the wire logs belonging to sessionID.
//
// kimi names the session directory with the ACP session id verbatim
// (`<root>/<workspace>/<sessionID>/agents/<name>/wire.jsonl`), so the session
// is addressed by exact path rather than by globbing every session and
// filtering after: session history only grows, and this runs on the billing
// path at the end of every task. Building the path also keeps the agent's
// session id out of a glob pattern, where a `*` or `[` would be read as a
// wildcard — as would one in a custom KIMI_CODE_HOME.
func kimiSessionWireLogs(root, sessionID string) []string {
	sessionID = strings.TrimSpace(sessionID)
	// Guard the path join: the id comes from the agent.
	if sessionID == "" || sessionID == "." || sessionID == ".." ||
		strings.ContainsRune(sessionID, '/') || strings.ContainsRune(sessionID, os.PathSeparator) {
		return nil
	}

	workspaces, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var paths []string
	for _, workspace := range workspaces {
		if !workspace.IsDir() {
			continue
		}
		agentsDir := filepath.Join(root, workspace.Name(), sessionID, "agents")
		agents, err := os.ReadDir(agentsDir)
		if err != nil {
			continue
		}
		for _, agent := range agents {
			if !agent.IsDir() {
				continue
			}
			paths = append(paths, filepath.Join(agentsDir, agent.Name(), "wire.jsonl"))
		}
	}
	return paths
}

// accumulateKimiWireUsage adds one wire log's records into usage, keyed by the
// model each record names. A malformed or truncated line is skipped rather
// than failing the scan: the log is appended to live, so the tail can be a
// partial write, and reporting the records we could read beats reporting
// nothing.
func accumulateKimiWireUsage(usage map[string]TokenUsage, path string, scan kimiUsageScan) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := newAgentStreamScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(kimiUsageRecordType)) {
			continue
		}
		var record kimiWireUsage
		if err := json.Unmarshal(line, &record); err != nil || record.Type != kimiUsageRecordType {
			continue
		}
		if !kimiWireRecordInTurn(record.Time, scan.startTime, scan.resumed) {
			continue
		}
		model := record.Model
		if model == "" {
			model = scan.fallbackModel
		}
		total := usage[model]
		total.InputTokens += record.Usage.InputOther
		total.OutputTokens += record.Usage.Output
		total.CacheReadTokens += record.Usage.InputCacheRead
		total.CacheWriteTokens += record.Usage.InputCacheCreation
		usage[model] = total
	}
}

// kimiWireRecordInTurn reports whether a usage record belongs to the turn that
// started at startTime.
//
// kimi-code 0.33.0 stamps every usage record, so the untimed case is only a
// guard against a future format change. There it errs toward billing on a
// fresh session (whose records are all ours anyway) and toward dropping on a
// resume, where counting an untimed record risks charging a user twice for
// tokens the earlier task already reported.
func kimiWireRecordInTurn(recordTimeMillis int64, startTime time.Time, resumed bool) bool {
	if recordTimeMillis <= 0 {
		return !resumed
	}
	if startTime.IsZero() {
		return true
	}
	return recordTimeMillis >= startTime.UnixMilli()
}

// kimiSessionRoot resolves kimi's session directory: an explicitly configured
// home first, then the ambient KIMI_CODE_HOME, then ~/.kimi-code. Mirrors
// codexSessionRoot.
//
// The env lookup is not redundant with cfg.Env: the kimi subprocess inherits
// os.Environ() via buildEnv, so a KIMI_CODE_HOME exported to the daemon
// relocates kimi's sessions without ever appearing in cfg.Env. Reading only
// cfg.Env would leave the scan looking in ~/.kimi-code while kimi wrote
// somewhere else. The stat check keeps a home without a sessions directory
// from disabling the scan.
func kimiSessionRoot(kimiHome string) string {
	if kimiHome = strings.TrimSpace(kimiHome); kimiHome == "" {
		kimiHome = strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	}
	if kimiHome != "" {
		dir := filepath.Join(kimiHome, "sessions")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".kimi-code", "sessions")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}
