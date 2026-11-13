package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// OpenCode over ACP (`opencode acp`).
//
// `opencode run --format json` writes a tool_use event only once the tool has
// finished, with the call and its result in one line. The daemon therefore never
// sees a tool in flight on that transport, and a long silent tool — a build, a
// test suite, a delegated subagent — is judged by the short OpenCode idle window
// (DefaultOpenCodeIdleWatchdog, 10 min) instead of the tool budget, so healthy
// runs get force-stopped (GH #9614). Both majors stream tool starts on ACP
// instead: 1.x and 2.x send `tool_call` when the model emits the call and an
// `in_progress` `tool_call_update` carrying the real input the moment the tool
// starts executing. That second frame is where MessageToolUse is emitted (see
// hermesClient.toolInputArrivesInProgress).
//
// What differs between the majors on this transport, all verified against
// opencode-ai 1.19.4 and @opencode/cli 2.2.0 (handshake also on 1.15.0 and
// 2.0.0):
//
//   - Model selection. Both advertise a `model` entry in configOptions and take
//     session/set_config_option; 2.x has no session/set_model at all (-32601).
//     Reasoning effort is the advertised `effort` option on both, so the 2.x
//     `provider/model#variant` spelling is only split, never sent.
//   - MCP. 1.x still honours OPENCODE_CONFIG_CONTENT, which carries every shape
//     the OpenCode translator accepts, so it keeps that channel. 2.x ignores the
//     env var; ACP session/new's `mcpServers` is the channel there, which keeps
//     credentials in the process instead of in a file in the agent's workdir
//     (the reason the run transport refuses MCP on 2.x).
//   - Subagents. 2.x forwards a child session's updates into the parent with
//     `_meta["opencode/child-session"]`. Its tool calls are kept (they are real
//     work in flight); its text is dropped so the parent's output stays the
//     parent's answer, matching the run transport.
//   - Usage. The prompt result's usage is not a per-turn figure on either
//     major: 1.x reports the last assistant message only, 2.x the session
//     total. Usage is therefore summed from the session export, over the
//     assistant messages this turn created (opencodeACPTurnUsage).
//
// Cancellation is `session/cancel`, which on both majors aborts the running
// tool and answers the prompt with stopReason "cancelled". On 2.x it is the
// only reliable stop: the ACP process hosts its server as a separate process
// group, so a group signal alone does not reach the tools.
//
// The run transport is kept as a fallback for everything this file cannot vouch
// for: custom runtime profiles, versions older than the ones checked, agents
// with custom_args (they are `opencode run` flags), and anyone who sets
// MULTICA_OPENCODE_TRANSPORT=run.

// opencodeTransportEnv selects the OpenCode transport. "run" forces the
// `opencode run --format json` path; anything else lets the backend choose.
// It is an operator setting read from the daemon's environment (the daemon
// strips MULTICA_* keys from agent custom_env); Config.Env is consulted first
// so tests can pin a transport per backend.
const opencodeTransportEnv = "MULTICA_OPENCODE_TRANSPORT"

// Oldest releases whose ACP surface (configOptions model + effort selectors,
// loadSession, session/cancel) matches what this file drives. 1.15.0 is the
// first 1.x release that advertises the effort option.
var (
	opencodeACPMinV1 = semver{Major: 1, Minor: 15, Patch: 0}
	opencodeACPMinV2 = semver{Major: 2, Minor: 0, Patch: 0}
)

// Timing knobs. Vars so tests can shorten them.
var (
	// opencodeACPCancelGrace bounds the wait for the prompt to answer after
	// session/cancel before the process is torn down regardless.
	opencodeACPCancelGrace = 5 * time.Second
	// opencodeACPExitGrace is how long the process gets to exit on its own
	// after stdin closes before it is signalled.
	opencodeACPExitGrace = 3 * time.Second
	// opencodeACPDrainGrace bounds the trailing-notification drain after the
	// prompt response, like the other ACP backends' reader drain.
	opencodeACPDrainGrace = 2 * time.Second
	// opencodeACPUsageTimeout bounds the post-run session export.
	opencodeACPUsageTimeout = 15 * time.Second
)

// opencodeUsesACP reports whether this run should use the ACP transport, and if
// not, why (for the log line).
func opencodeUsesACP(cfg Config, opts ExecOptions) (bool, string) {
	if strings.EqualFold(strings.TrimSpace(opencodeTransportSetting(cfg)), "run") {
		return false, opencodeTransportEnv + "=run"
	}
	// A custom profile wraps a binary this package did not choose, so its
	// version string does not establish which protocol it speaks — the same
	// rule opencodeUsesV2Contract applies.
	if !cfg.BuiltinRuntime || len(cfg.LaunchPrefix) != 0 {
		return false, "custom runtime profile"
	}
	version, err := parseSemver(strings.TrimSpace(cfg.CLIVersion))
	if err != nil {
		return false, "unknown OpenCode version"
	}
	switch {
	case version.Major == 1 && version.lessThan(opencodeACPMinV1):
		return false, "OpenCode older than " + fmt.Sprintf("%d.%d.%d", opencodeACPMinV1.Major, opencodeACPMinV1.Minor, opencodeACPMinV1.Patch)
	case version.Major < 1:
		return false, "OpenCode older than 1.x"
	case version.Major >= 2 && version.lessThan(opencodeACPMinV2):
		return false, "unrecognised OpenCode version"
	}
	// custom_args are `opencode run` flags (--agent, --attach, …); `opencode
	// acp` would reject or ignore them, so honour them on the transport they
	// were written for.
	if len(opts.CustomArgs) != 0 {
		return false, "agent has custom_args for `opencode run`"
	}
	return true, ""
}

func opencodeTransportSetting(cfg Config) string {
	if v, ok := cfg.Env[opencodeTransportEnv]; ok {
		return v
	}
	return os.Getenv(opencodeTransportEnv)
}

// opencodeACPToolName keeps OpenCode's own tool ids (bash, read, task, …) so
// the transcript reads the same as on the run transport. 2.x sends the id as
// `name`; 1.x's start frame sends it as `title`.
func opencodeACPToolName(name, title, kind string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	return kind
}

// opencodeACPChildUpdate reports whether a session/update came from a 2.x
// subagent's child session rather than the session Multica prompted.
func opencodeACPChildUpdate(update json.RawMessage) bool {
	var u struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(update, &u) != nil {
		return false
	}
	_, ok := u.Meta["opencode/child-session"]
	return ok
}

// opencodeACPSplitModel splits a 2.x `provider/model#variant` selector. The
// pinned variant wins over the agent-level thinking level, as on the run
// transport (opencodeModelArg).
func opencodeACPSplitModel(model, thinkingLevel string) (string, string) {
	model = strings.TrimSpace(model)
	if base, variant, ok := strings.Cut(model, "#"); ok {
		if variant = strings.TrimSpace(variant); variant != "" {
			return strings.TrimSpace(base), variant
		}
		return strings.TrimSpace(base), thinkingLevel
	}
	return model, thinkingLevel
}

// opencodeACPSelectModel puts the session on model and returns the session's
// configOptions as they stand afterwards, which is what the effort option has
// to be read from: the effort vocabulary belongs to the current model.
func opencodeACPSelectModel(ctx context.Context, request acpRequestFn, sessionID, model string, sessionResult json.RawMessage) (json.RawMessage, error) {
	if model == "" {
		return sessionResult, nil
	}
	if _, ok := findACPConfigOption(sessionResult, "model"); !ok {
		// Not seen on any version this transport is enabled for; kept so an
		// unexpected build degrades to the older ACP model call.
		if _, err := request(ctx, "session/set_model", map[string]any{"sessionId": sessionID, "modelId": model}); err != nil {
			return nil, fmt.Errorf("select model %q: %w", model, err)
		}
		return nil, nil
	}
	result, err := request(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  "model",
		"value":     model,
	})
	if err != nil {
		return nil, fmt.Errorf("select model %q: %w", model, err)
	}
	if current, ok := acpConfigOptionCurrentValue(result, "model"); ok && current != model {
		return nil, fmt.Errorf("select model %q: OpenCode kept the session on %q", model, current)
	}
	return result, nil
}

// opencodeACPMcpServers translates mcp_config for a 2.x ACP session. It goes
// through the OpenCode translator first so the accepted input shapes (including
// OpenCode-native `mcp` entries) and the validation are the same as on 1.x.
//
// Two OpenCode features have no ACP representation and fail the run rather
// than silently change what the agent gets: remote OAuth, and a bare
// {"enabled": …} override of a server the user configured elsewhere. A
// per-server timeout is dropped with a warning.
func opencodeACPMcpServers(raw json.RawMessage, cfg Config) ([]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []any{}, nil
	}
	servers, err := translateMCPConfigForOpenCode(raw)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		entry, _ := servers[name].(map[string]any)
		if enabled, ok := entry["enabled"].(bool); ok && !enabled {
			if _, typed := entry["type"]; typed {
				continue // a disabled server is simply not sent
			}
		}
		if _, ok := entry["timeout"]; ok && cfg.Logger != nil {
			cfg.Logger.Warn("opencode: MCP server timeout cannot be passed over ACP; using OpenCode's default", "server", name)
		}
		switch entry["type"] {
		case "local":
			command := stringsFromAny(entry["command"])
			if len(command) == 0 {
				return nil, fmt.Errorf("opencode mcp_config: server %q: local server missing `command`", name)
			}
			out = append(out, map[string]any{
				"name":    name,
				"command": command[0],
				"args":    append([]string{}, command[1:]...),
				"env":     acpNameValues(entry["environment"]),
			})
		case "remote":
			if oauth, ok := entry["oauth"]; ok && oauth != false {
				return nil, fmt.Errorf("opencode mcp_config: server %q uses OAuth, which OpenCode 2.x cannot receive over ACP; use headers, an OpenCode 1.x runtime, or %s=run", name, opencodeTransportEnv)
			}
			url, _ := entry["url"].(string)
			out = append(out, map[string]any{
				"type":    "http",
				"name":    name,
				"url":     url,
				"headers": acpNameValues(entry["headers"]),
			})
		default:
			return nil, fmt.Errorf("opencode mcp_config: server %q is an {\"enabled\": …} override, which OpenCode 2.x cannot receive over ACP", name)
		}
	}
	return out, nil
}

func stringsFromAny(v any) []string {
	switch items := v.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	}
	return nil
}

// acpNameValues turns a JSON object of strings into ACP's sorted
// [{name, value}] list.
func acpNameValues(v any) []map[string]any {
	m, _ := v.(map[string]any)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{"name": k, "value": fmt.Sprint(m[k])})
	}
	return out
}

func (b *opencodeBackend) executeACP(ctx context.Context, execPath, prompt string, opts ExecOptions) (*Session, error) {
	usesV2 := opencodeUsesV2Contract(b.cfg)
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("opencode working directory: %w", err)
	}

	// 1.x: MCP rides OPENCODE_CONFIG_CONTENT exactly as on the run transport.
	// 2.x: MCP goes into session/new, and the env carries none.
	envOpts := opts
	mcpServers := []any{}
	if usesV2 {
		envOpts = ExecOptions{Cwd: opts.Cwd}
		if mcpServers, err = opencodeACPMcpServers(opts.McpConfig, b.cfg); err != nil {
			return nil, err
		}
	}
	env, err := openCodeEnvironment(b.cfg, envOpts)
	if err != nil {
		return nil, err
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// The process runs on its own context so cancelling the task can go
	// through session/cancel first; procCancel is the last-resort group kill.
	procCtx, procCancel := context.WithCancel(context.Background())
	args := []string{"acp"}
	cmd := b.cfg.commandAt(execPath).exec(procCtx, args...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = cwd
	cmd.Env = env
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "acp")))

	fail := func(err error) (*Session, error) {
		procCancel()
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("opencode stdout pipe: %w", err))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("opencode stdin pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(fmt.Errorf("opencode stderr pipe: %w", err))
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fail(fmt.Errorf("start opencode: %w", err))
	}
	b.cfg.Logger.Info("opencode acp started", "pid", cmd.Process.Pid, "cwd", cwd, "model", opts.Model)

	providerErr := newACPProviderErrorSniffer("opencode")
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[opencode:stderr] "), providerErr), stderr)
	}()

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var streaming atomic.Bool
	var sawText, sawTool atomic.Bool
	var outputMu sync.Mutex
	var output strings.Builder
	activity := make(chan struct{}, 1)
	promptDone := make(chan hermesPromptResult, 1)

	c := &hermesClient{
		cfg:                        b.cfg,
		stdin:                      stdin,
		pending:                    make(map[int]*pendingRPC),
		pendingTools:               make(map[string]*pendingToolCall),
		toolInputArrivesInProgress: true,
		toolOutputFromContent:      true,
		toolName:                   opencodeACPToolName,
		// session/load replays the whole conversation as session/update;
		// only the turn this run prompts is this run's transcript.
		acceptNotification: func(string) bool { return streaming.Load() },
		acceptUpdate: func(updateType string, update json.RawMessage) bool {
			if updateType == "tool_call" || updateType == "tool_call_update" {
				return true
			}
			return !opencodeACPChildUpdate(update)
		},
		onActivity: func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
			if !streaming.Load() {
				return
			}
			switch msg.Type {
			case MessageText:
				sawText.Store(true)
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			case MessageToolUse:
				sawTool.Store(true)
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

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				c.handleLine(line)
			}
		}
		c.closeAllPending(errors.New("opencode process exited"))
	}()

	// stop tears the process down exactly once. A graceful stop closes stdin
	// and lets OpenCode exit on EOF before signalling. A forced stop is for a
	// run that was cancelled or timed out without OpenCode answering: closing
	// stdin first releases a prompt write blocked on a full pipe (it holds the
	// client's write lock), and the group is signalled at once, so an
	// unresponsive runtime is gone within opencodeTerminateGrace instead of
	// being waited on.
	// promptSent flips once the whole prompt is in the pipe. Until then
	// OpenCode has not received the turn, so there is nothing for
	// session/cancel to stop.
	var promptSent atomic.Bool

	var stopOnce sync.Once
	stop := func(force bool) {
		stopOnce.Do(func() {
			_ = stdin.Close()
			if !force {
				select {
				case <-readerDone:
				case <-time.After(opencodeACPExitGrace):
					force = true
				}
			}
			if force {
				signalProcessGroup(cmd, syscall.SIGTERM)
				select {
				case <-readerDone:
				case <-time.After(opencodeTerminateGrace()):
				}
			}
			procCancel() // group SIGKILL; a no-op for a group that already exited
			_ = cmd.Wait()
			<-readerDone
			<-stderrDone
			releaseProcessGroup(cmd)
		})
	}

	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer cancel()
		// Setup paths return early; a cancelled or timed-out setup is torn
		// down without the graceful wait.
		defer func() { stop(runCtx.Err() != nil) }()

		// Until the prompt is fully delivered there is no turn for
		// session/cancel to stop, and every request so far is a synchronous
		// write that checks its context only after the write returns. A
		// runtime that stops reading (a large session/new MCP payload, a
		// large prompt) would hold this goroutine in that write, so the
		// deferred stop above would never run. The watcher tears the process
		// down as soon as the run ends in that window; closing stdin is what
		// releases the blocked write. Once the prompt is delivered, the
		// cancel path below owns the shutdown.
		watcherDone := make(chan struct{})
		defer close(watcherDone)
		go func() {
			select {
			case <-runCtx.Done():
				if !promptSent.Load() {
					stop(true)
				}
			case <-watcherDone:
			}
		}()

		startTime := time.Now()
		result := func(status, errText, sessionID string, rejected bool) {
			resCh <- Result{Status: status, Error: errText, SessionID: sessionID, ResumeRejected: rejected, DurationMs: time.Since(startTime).Milliseconds()}
		}

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo":      map[string]any{"name": "multica-agent-sdk", "version": "0.2.0"},
			"clientCapabilities": map[string]any{
				"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
				"terminal": false,
			},
		})
		if err != nil {
			status, errText, _ := classifyACPResumeFailure(runCtx, "opencode", "initialize", err, timeout, b.cfg.Logger)
			result(status, errText, "", false)
			return
		}
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "opencode", b.cfg)

		var sessionID string
		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			loaded, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText, rejected := classifyACPResumeFailure(runCtx, "opencode", "session/load", err, timeout, b.cfg.Logger)
				result(status, errText, "", rejected)
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, loaded)
			if changed {
				b.cfg.Logger.Warn("opencode returned a different session id on resume; continuing with the new id",
					"requested", opts.ResumeSessionID, "actual", sessionID)
			}
			sessionResult = loaded
		} else {
			created, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText, _ := classifyACPResumeFailure(runCtx, "opencode", "session/new", err, timeout, b.cfg.Logger)
				result(status, errText, "", false)
				return
			}
			if sessionID = extractACPSessionID(created); sessionID == "" {
				result("failed", "opencode session/new returned no session ID", "", false)
				return
			}
			sessionResult = created
		}
		c.sessionID = sessionID

		model, level := opencodeACPSplitModel(opts.Model, opts.ThinkingLevel)
		options, err := opencodeACPSelectModel(runCtx, c.request, sessionID, model, sessionResult)
		if err != nil {
			status, errText, _ := classifyACPResumeFailure(runCtx, "opencode", "model selection", err, timeout, b.cfg.Logger)
			keep := sessionID
			if setupFailureWithholdsSessionID(opts) {
				keep = ""
			}
			result(status, errText, keep, false)
			return
		}
		if options != nil {
			applyACPEffortOption(runCtx, c.request, "opencode", b.cfg.Logger, sessionID, options, level, true)
		} else if level != "" {
			b.cfg.Logger.Warn("opencode session advertises no configuration after model selection; ignoring thinking level", "thinkingLevel", level)
		}

		b.cfg.Logger.Info("opencode session ready", "session_id", sessionID, "resumed", opts.ResumeSessionID != "")
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

		// OpenCode reads the runtime brief from the AGENTS.md the daemon writes
		// into the workdir, so SystemPrompt is not prepended — same as the run
		// transport (MUL-5392).
		streaming.Store(true)
		turnStart := time.Now()
		type promptOutcome struct{ err error }
		promptCh := make(chan promptOutcome, 1)
		go func() {
			_, err := c.requestAndNotifySent(context.Background(), "session/prompt", map[string]any{
				"sessionId": sessionID,
				"prompt":    []map[string]any{{"type": "text", "text": prompt}},
			}, func() { promptSent.Store(true) })
			promptCh <- promptOutcome{err: err}
		}()

		finalStatus := "completed"
		var finalError string
		var promptErr error
		answered := false
		select {
		case out := <-promptCh:
			promptErr, answered = out.err, true
		case <-runCtx.Done():
			if !promptSent.Load() {
				b.cfg.Logger.Warn("opencode: run ended before the prompt was delivered; stopping the process")
				break
			}
			// Ask OpenCode to stop the turn; it aborts the running tool and
			// answers the prompt. The write runs on its own goroutine so a
			// stalled pipe cannot hold the teardown, and the grace starts now.
			go func() {
				if err := opencodeACPNotify(c, "session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
					b.cfg.Logger.Warn("opencode: session/cancel could not be sent", "error", err)
				}
			}()
			select {
			case out := <-promptCh:
				promptErr, answered = out.err, true
			case <-time.After(opencodeACPCancelGrace):
				b.cfg.Logger.Warn("opencode did not answer session/cancel in time; stopping the process", "grace", opencodeACPCancelGrace.String())
			}
		}

		var pr hermesPromptResult
		havePrompt := false
		if answered && promptErr == nil {
			select {
			case pr = <-promptDone:
				havePrompt = true
			default:
			}
		}

		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			finalStatus, finalError = "timeout", fmt.Sprintf("opencode timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			finalStatus, finalError = "aborted", "execution cancelled"
		case promptErr != nil:
			finalStatus, finalError = "failed", fmt.Sprintf("opencode session/prompt failed: %v", promptErr)
		case havePrompt:
			switch pr.stopReason {
			case "", "end_turn":
			case "cancelled":
				finalStatus, finalError = "aborted", "opencode cancelled the turn"
			case "max_tokens":
				finalStatus, finalError = "failed", "opencode stopped: the model hit its output length limit"
			case "refusal":
				finalStatus, finalError = "failed", "opencode stopped: the provider refused the request"
			default:
				finalStatus, finalError = "failed", "opencode stopped: "+pr.stopReason
			}
		}
		if answered && runCtx.Err() == nil {
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, opencodeACPDrainGrace)
		}
		streaming.Store(false)
		// A turn OpenCode never answered is forced down; an answered one,
		// including an acknowledged cancel, exits gracefully.
		stop(!answered)
		providerErr.Finalize()

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		usage, usageErr := opencodeACPTurnUsage(b.cfg, execPath, env, cwd, sessionID, turnStart, usesV2)
		if usageErr != nil {
			usage = TokenUsage{}
			// Fall back to what the prompt reported only where that figure is
			// not an overcount: 1.x reports one message (an undercount), and a
			// fresh 2.x session's total is this turn.
			if havePrompt && (!usesV2 || opts.ResumeSessionID == "") {
				usage = pr.usage.TokenUsage
				usage.CostUSDTicks = 0
			}
			b.cfg.Logger.Warn("opencode: could not read this turn's token usage from the session export", "error", usageErr, "fallback", acpTokenUsagePresent(usage))
		}

		// The run transport's empty-step guard (#6522): a turn that closes
		// cleanly with no text, no tool call and no usage at all never reached
		// the provider, and must not be reported as a success.
		if finalStatus == "completed" && usageErr == nil && !sawText.Load() && !sawTool.Load() && !acpTokenUsagePresent(usage) {
			finalStatus = "failed"
			finalError = "opencode turn ended with no text, no tool call and no reported usage — the provider produced nothing"
		}

		var usageMap map[string]TokenUsage
		if acpTokenUsagePresent(usage) {
			name := opts.Model
			if name == "" {
				name = "unknown"
			}
			usageMap = map[string]TokenUsage{name: usage}
		}
		duration := time.Since(startTime)
		b.cfg.Logger.Info("opencode finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())
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

// opencodeACPNotify writes a JSON-RPC notification (no id, no response).
func opencodeACPNotify(c *hermesClient, method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return c.writeLine(append(data, '\n'))
}

// opencodeACPTurnUsage sums the token usage of the assistant messages this turn
// created, read from the session export after the process has exited.
//
// Each OpenCode step is its own assistant message with its own provider call,
// so the turn's usage is their sum — the same figure the run transport adds up
// from step_finish events. The turn's messages are the ones created at or after
// turnStart; the export's timestamps come from the same host clock.
func opencodeACPTurnUsage(cfg Config, execPath string, env []string, cwd, sessionID string, turnStart time.Time, usesV2 bool) (TokenUsage, error) {
	if sessionID == "" || strings.HasPrefix(sessionID, "-") {
		return TokenUsage{}, errors.New("no session id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), opencodeACPUsageTimeout)
	defer cancel()
	args := []string{"export", sessionID}
	if usesV2 {
		// --standalone keeps the export off the resident background service:
		// this run never started one, and should not leave one behind.
		args = []string{"session", "export", sessionID, "--standalone"}
	}
	run := func(args []string) ([]byte, error) {
		cmd := cfg.commandAt(execPath).exec(ctx, args...)
		hideAgentWindow(cmd)
		cmd.Dir, cmd.Env = cwd, env
		cmd.Stderr = &tailBuffer{max: 4096}
		limited := &limitedBuffer{max: openCodeClockExportMaxBytes}
		cmd.Stdout = limited
		err := runOwned(cmd, cfg.Logger)
		if limited.overflow {
			return nil, errors.New("session export exceeds the size limit")
		}
		return limited.buf.Bytes(), err
	}
	// --sanitize redacts message text, which this never needs. Older CLIs
	// lack the flag; retry without it.
	data, err := run(append(args, "--sanitize"))
	if err != nil {
		data, err = run(args)
	}
	if err != nil {
		return TokenUsage{}, fmt.Errorf("session export: %w", err)
	}
	return opencodeExportTurnUsage(data, turnStart.UnixMilli(), opencodeSeparatesReasoning(cfg))
}

// opencodeExportTurnUsage reads both export shapes: 1.x nests each message's
// metadata under `info` with a `role`; 2.x flattens it and names the kind
// `type`.
func opencodeExportTurnUsage(data []byte, sinceMs int64, separateReasoning bool) (TokenUsage, error) {
	type msgTime struct {
		Created int64 `json:"created"`
	}
	type meta struct {
		Role   string          `json:"role"`
		Type   string          `json:"type"`
		Time   msgTime         `json:"time"`
		Tokens *opencodeTokens `json:"tokens"`
	}
	var export struct {
		Messages []struct {
			meta
			Info *meta `json:"info"`
		} `json:"messages"`
	}
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return TokenUsage{}, errors.New("session export is not JSON")
	}
	if err := json.Unmarshal(data[start:], &export); err != nil {
		return TokenUsage{}, fmt.Errorf("parse session export: %w", err)
	}
	var usage TokenUsage
	for _, m := range export.Messages {
		info := m.meta
		if m.Info != nil {
			info = *m.Info
		}
		if info.Role != "assistant" && info.Type != "assistant" {
			continue
		}
		if info.Time.Created < sinceMs || info.Tokens == nil {
			continue
		}
		t := info.Tokens
		usage.InputTokens += t.Input
		usage.OutputTokens += t.Output
		if separateReasoning {
			usage.OutputTokens += t.Reasoning
		}
		if t.Cache != nil {
			usage.CacheReadTokens += t.Cache.Read
			usage.CacheWriteTokens += t.Cache.Write
		}
	}
	return usage, nil
}

// limitedBuffer keeps at most max bytes and records whether more arrived.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room < len(p) {
		l.overflow = true
		if room > 0 {
			l.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return l.buf.Write(p)
}
