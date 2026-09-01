package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// hermesBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport; overriding it
// would break the daemon↔Hermes communication contract.
//
// `-p`/`--profile` are NOT stripped unconditionally: a skill-less Hermes task
// has no overlay, so its profile selection must pass through to Hermes
// unchanged. The daemon strips the selected occurrence via StripHermesProfileArgs
// only when it actually built the per-task overlay (see the daemon's launch-arg
// handling), so the flag can't re-point HERMES_HOME past the overlay while
// leaving no-overlay tasks' behavior untouched.
var hermesBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

// hermesArgProfileRe mirrors the space-form guard in Hermes'
// hermes_cli.main._apply_profile_override step 1b: a `-p <value>` whose value
// doesn't match the profile-id shape is not a profile selection at all (e.g.
// pytest's `-p no:xdist`), so it is ignored rather than consumed. The inline
// `--profile=<value>` form is NOT guarded here — Hermes forwards it verbatim to
// resolve_profile_env, which validates and hard-fails on an invalid value; that
// validation lives in the daemon-side resolver (execenv.ResolveHermesProfile).
var hermesArgProfileRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// hermesValueFlags and hermesOptionalValueFlags mirror the value-taking flags
// Hermes skips while scanning argv for -p/--profile, so a value like the `coder`
// in `-m coder -p research` is never misread as the profile. Kept in sync with
// _apply_profile_override.value_flags / optional_value_flags.
var hermesValueFlags = map[string]struct{}{
	"-z": {}, "--oneshot": {}, "-m": {}, "--model": {}, "--provider": {},
	"-t": {}, "--toolsets": {}, "-r": {}, "--resume": {}, "-s": {},
	"--skills": {}, "--usage-file": {},
}
var hermesOptionalValueFlags = map[string]struct{}{"-c": {}, "--continue": {}}

// HermesProfileSelection is the profile selection parsed out of custom_args by
// ParseHermesProfileArgs. It carries the exact argv occurrence to consume so the
// daemon-side resolver and the launch-arg stripping act on one authoritative
// parse instead of each re-approximating Hermes' argv handling.
type HermesProfileSelection struct {
	Name    string // the selected value; "" for the empty inline `--profile=` value
	Found   bool   // a -p/--profile selection with a value was matched
	Inline  bool   // matched the `--profile=<value>` form (value validated downstream)
	ArgFrom int    // index of the first token to strip, or -1 when nothing matched
	ArgLen  int    // tokens to strip: 2 for `-p <value>`, 1 for `--profile=<value>`
}

// ParseHermesProfileArgs finds the first Hermes profile selection in custom_args,
// mirroring hermes_cli.main._apply_profile_override step 1/1b: it scans for the
// first `-p`/`--profile <value>` or `--profile=<value>`, skipping value-taking
// flags and stopping at a `--` sentinel or an `mcp add --args` command-argv
// passthrough region. A space-form value that fails the profile-id shape is
// ignored (matches Hermes discarding it). Args are unquoted with the same helper
// as filterCustomArgs so quoting is handled consistently.
func ParseHermesProfileArgs(args []string) HermesProfileSelection {
	none := HermesProfileSelection{ArgFrom: -1}
	i := 0
	for i < len(args) {
		arg := unshellQuoteArg(args[i])
		if arg == "--" {
			break
		}
		if arg == "--args" && hermesInsideMcpAdd(args, i) {
			break
		}
		if arg == "-p" || arg == "--profile" {
			if i+1 < len(args) {
				val := unshellQuoteArg(args[i+1])
				if !hermesArgProfileRe.MatchString(val) {
					return none // step 1b: not a valid profile value, ignore
				}
				return HermesProfileSelection{Name: val, Found: true, ArgFrom: i, ArgLen: 2}
			}
			return none // trailing flag with no value
		}
		if v, ok := strings.CutPrefix(arg, "--profile="); ok {
			return HermesProfileSelection{Name: v, Found: true, Inline: true, ArgFrom: i, ArgLen: 1}
		}
		if _, ok := hermesValueFlags[arg]; ok && i+1 < len(args) {
			i += 2
			continue
		}
		if _, ok := hermesOptionalValueFlags[arg]; ok && i+1 < len(args) &&
			!strings.HasPrefix(unshellQuoteArg(args[i+1]), "-") {
			i += 2
			continue
		}
		i++
	}
	return none
}

// hermesInsideMcpAdd reports whether argv position index sits inside an
// `mcp add ... --args <child argv>` passthrough region, where flags belong to
// the child MCP command and must not be read as Hermes' own profile selector.
func hermesInsideMcpAdd(args []string, index int) bool {
	mcp := -1
	for j := 0; j < index; j++ {
		if unshellQuoteArg(args[j]) == "mcp" {
			mcp = j
			break
		}
	}
	if mcp < 0 {
		return false
	}
	for j := mcp + 1; j < index; j++ {
		if unshellQuoteArg(args[j]) == "add" {
			return true
		}
	}
	return false
}

// hermesACPSubcommand is the subcommand the backend always launches with. It
// sits between the runtime's launch prefix and the agent's custom args, and it
// is an ordinary argv token to Hermes' own parser — which is why the daemon
// cannot reason about a profile selection without it.
const hermesACPSubcommand = "acp"

// hermesCLIArgsFrom assembles the argv the backend passes after the executable
// and its launch prefix, from custom args that are already filtered.
func hermesCLIArgsFrom(filteredCustomArgs []string) []string {
	args := make([]string, 0, 1+len(filteredCustomArgs))
	args = append(args, hermesACPSubcommand)
	return append(args, filteredCustomArgs...)
}

// hermesCLIArgs is what hermesBackend.Execute passes to the launch boundary.
func hermesCLIArgs(customArgs []string, logger *slog.Logger) []string {
	return hermesCLIArgsFrom(filterCustomArgs(customArgs, hermesBlockedArgs, logger))
}

// HermesLaunchArgv returns the exact argv Hermes will parse: the runtime's
// launch prefix, then `acp`, then the agent's custom args after the same
// blocked-flag filtering the backend applies.
//
// The daemon resolves the profile selection from this rather than from a
// hand-assembled approximation. Concatenating prefix and custom args alone
// silently disagrees with the real command line, because the `acp` token
// participates in Hermes' scan: with fixed_args `--model` and custom_args
// `-p research`, the approximation reads `-p` as `--model`'s value and finds
// no selection, while the real `--model acp -p research` skips `--model acp`
// and selects `research`. The overlay would then be seeded from the default
// home while the process runs under a different profile's config.
func HermesLaunchArgv(launchPrefix, customArgs []string, logger *slog.Logger) []string {
	return Command{Prefix: launchPrefix}.Argv(hermesCLIArgs(customArgs, logger)...)
}

// StripHermesProfileSelectors removes every profile selection from the argv
// Hermes will parse and hands each surviving token back to the region it came
// from — launch prefix or custom args.
//
// The daemon calls this only when it built the per-task overlay, where the
// overlay's HERMES_HOME is authoritative and nothing on the command line may
// re-point out of it.
//
// It works on the assembled argv rather than on each region separately for two
// reasons, both of which leave a live selector behind if ignored:
//
//   - A selection can straddle the boundary. A launch prefix ending in a bare
//     `-p` takes the backend's own `acp` token as its value, and neither region
//     contains a complete selection to strip.
//   - Removing one selection promotes the next. Hermes honours the first and
//     ignores the rest, so a single pass can hand the job to a later
//     occurrence — and with the prefix and custom args configured separately,
//     two selections is ordinary configuration rather than a user mistake.
//
// Tokens Hermes itself discards — an invalid profile value, which makes
// ParseHermesProfileArgs report nothing found — are left alone: they redirect
// nothing. The `acp` token is never removed, because the backend re-adds it at
// launch; dropping the flag that captured it is what breaks the selection.
func StripHermesProfileSelectors(launchPrefix, customArgs []string, logger *slog.Logger) ([]string, []string) {
	prefix := append([]string(nil), launchPrefix...)
	custom := append([]string(nil), filterCustomArgs(customArgs, hermesBlockedArgs, logger)...)
	for {
		sel := ParseHermesProfileArgs(Command{Prefix: prefix}.Argv(hermesCLIArgsFrom(custom)...))
		if !sel.Found {
			return prefix, custom
		}
		acpIndex := len(prefix)
		removed := false
		// Walk back to front so earlier indices stay valid as tokens go.
		for i := sel.ArgFrom + sel.ArgLen - 1; i >= sel.ArgFrom; i-- {
			switch {
			case i < acpIndex:
				prefix = append(prefix[:i], prefix[i+1:]...)
				removed = true
			case i == acpIndex:
				// Backend-owned; re-added at launch.
			default:
				if j := i - acpIndex - 1; j < len(custom) {
					custom = append(custom[:j], custom[j+1:]...)
					removed = true
				}
			}
		}
		if !removed {
			// Defensive: a selection always contains a flag from one of the two
			// regions, so this cannot loop forever. Bail rather than spin.
			return prefix, custom
		}
	}
}

// StripHermesProfileArgs removes exactly the argv occurrence ParseHermesProfileArgs
// selected. The daemon calls this only when it built the per-task overlay, so
// Hermes uses the overlay's HERMES_HOME instead of re-resolving the profile —
// while a skill-less task keeps its flags untouched.
func StripHermesProfileArgs(args []string, sel HermesProfileSelection) []string {
	if !sel.Found || sel.ArgFrom < 0 || sel.ArgLen <= 0 {
		return args
	}
	end := sel.ArgFrom + sel.ArgLen
	if end > len(args) {
		end = len(args)
	}
	out := make([]string, 0, len(args)-(end-sel.ArgFrom))
	out = append(out, args[:sel.ArgFrom]...)
	out = append(out, args[end:]...)
	return out
}

// hermesBackend implements Backend by spawning `hermes acp` and communicating
// via the ACP (Agent Communication Protocol) JSON-RPC 2.0 over stdin/stdout.
// This is the same pattern as Codex but with the ACP protocol instead of
// the Codex-specific JSON-RPC methods.
//
// opts.ThinkingLevel is applied through applyACPEffortOption below, driven by
// whatever effort option the session advertises. This provider covers two
// unrelated binaries — Hermes Agent and jcode — with opposite capabilities
// here, so the catalog is the only honest discriminator; see
// acpCatalogThinkingProviders in thinking.go for which is which.
type hermesBackend struct {
	cfg Config
}

var (
	hermesReaderDrainGrace      = 2 * time.Second
	hermesNotificationQuietTime = 250 * time.Millisecond
)

func (b *hermesBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "hermes"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("hermes executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP `session/new` expects. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of
	// silently dropping all MCP servers.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("hermes: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// Same assembly HermesLaunchArgv reproduces for the daemon, so the profile
	// the overlay is seeded from is the one this argv actually selects.
	hermesArgs := hermesCLIArgs(opts.CustomArgs, b.cfg.Logger)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, hermesArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(hermesArgs, trustAgentCommandPositional(0, hermesACPSubcommand)))
	agentsMDPresent := false
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
		if _, err := os.Stat(filepath.Join(opts.Cwd, "AGENTS.md")); err == nil {
			agentsMDPresent = true
		}
	}
	b.cfg.Logger.Info("hermes acp starting", "cwd", opts.Cwd, "agents_md_present", agentsMDPresent)
	if opts.SystemPrompt != "" {
		b.cfg.Logger.Debug("hermes ignoring ExecOptions.SystemPrompt; using cwd-scoped context files", "cwd", opts.Cwd)
	}

	env := buildEnv(b.cfg.Env)
	// Enable yolo mode so Hermes auto-approves all tool executions.
	env = append(env, "HERMES_YOLO_MODE=1")
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stdin pipe: %w", err)
	}
	// Forward stderr to the daemon log *and* sniff provider-level
	// errors out of it so we can surface them in the task result.
	// Hermes' session/prompt still reports stopReason=end_turn when
	// the underlying HTTP call to the LLM returns 4xx/5xx, so
	// without this we'd report a misleading "empty output" and hide
	// the real cause (wrong model for the current provider, bad
	// credentials, rate limit, …) in the daemon log.
	//
	// We use StderrPipe + an explicit copier goroutine instead of
	// `cmd.Stderr = io.MultiWriter(...)` so we have a join point
	// (`stderrDone`) before the failure-promotion decision. With the
	// MultiWriter form, exec's internal copy goroutine is only
	// joined by `cmd.Wait()`, which runs in the deferred cleanup —
	// after `promoteACPResultOnProviderError` already consulted the
	// sniffer. That race lost the 429 / usage-limit message under
	// CI load and surfaced as a flaky test
	// (TestHermesBackendPromotesProviderErrorWithNonEmptyOutput).
	providerErr := newACPProviderErrorSniffer("hermes")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start hermes: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[hermes:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("hermes acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Hermes streams interim narration and the final answer as the same
	// agent_message_chunk type; the tracker keeps only the post-tool-call block
	// for Result.Output while retaining the full text for error detection.
	var deliverable acpDeliverableTracker
	// streamingCurrentTurn gates all session updates so that history
	// replay (Hermes sends full prior-turn transcripts on session/resume,
	// and may flush queued chunks before our session/prompt response
	// streams) is dropped instead of duplicating the previous answer
	// into output. We flip it to true only after session/prompt is sent.
	var streamingCurrentTurn atomic.Bool
	// turnActivity counts the session updates accepted for the current turn.
	// Zero means the agent produced nothing at all — no text, no thought, no
	// tool call — which is what separates a dead-session refusal from a model
	// that genuinely refused the request. See hermesResumeSessionLost.
	var turnActivity atomic.Int64

	promptDone := make(chan hermesPromptResult, 1)
	activity := make(chan struct{}, 1)

	c := &hermesClient{
		cfg:                        b.cfg,
		stdin:                      stdin,
		pending:                    make(map[int]*pendingRPC),
		pendingTools:               make(map[string]*pendingToolCall),
		toolStartCarriesFinalInput: b.cfg.BuiltinRuntime,
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
			turnActivity.Add(1)
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
		c.closeAllPending(fmt.Errorf("hermes process exited"))
	}()

	// Drive the ACP session lifecycle in a goroutine.
	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			// Cancellation must be reachable before Wait. A pathological child
			// can close stdout/stderr (so the pipe drain succeeds) but keep the
			// process alive; waiting first would then block until the overall
			// task timeout and make a later deferred cancel ineffective.
			cancel()
			_ = cmd.Wait()
			releaseProcessGroup(cmd)
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		// Set when the ACP runtime refuses the session we asked to
		// resume. Only that is curable by starting a fresh session, so
		// handshake/network failures below must leave it false.
		var resumeRejected bool
		// True only when session/resume actually landed on the session we
		// asked for. Hermes answers a resume of a session its state.db no
		// longer holds by silently creating a fresh one (acp_adapter/server.py
		// resume_session: "not found, creating new"), so this is what decides
		// whether the turn must carry a continuity notice — see the prompt
		// assembly below.
		var resumeLanded bool
		// The stop reason session/prompt reported, when it answered at all.
		// Read once the turn has fully settled — see the resumed-session check
		// after the provider-error promotion below.
		var promptStopReason string
		effectiveModel := strings.TrimSpace(opts.Model)
		// The model id the runtime reports as current right after
		// session/new or session/resume. Used to skip a redundant
		// session/set_model when we would otherwise re-select the model the
		// session is already on (see the set_model gate below).
		var sessionCurrentModel string

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
			finalError = fmt.Sprintf("hermes initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. ACP requires the client to honour
		// agentCapabilities.mcpCapabilities; sending an http/sse entry to
		// a runtime that says it only supports stdio reliably rejects the
		// whole session/new request.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "hermes", b.cfg)

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		// sessionResult is whichever of session/new or session/resume produced
		// this session. It carries the configOptions the effort step below
		// reads, so both branches have to keep hold of it.
		var sessionResult json.RawMessage

		if opts.ResumeSessionID != "" {
			// Per ACP Session Setup, session/resume accepts mcpServers and
			// the runtime re-connects them as part of the resume. Without
			// this, a resumed Hermes task lost access to MCP tools that a
			// fresh task on the same agent would have.
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionResult = result
			sessionID, resumeLanded = resolveHermesResumedSessionID(opts.ResumeSessionID, result)
			if !resumeLanded {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "hermes",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			sessionCurrentModel = extractACPCurrentModelID(result)
			if effectiveModel == "" {
				effectiveModel = sessionCurrentModel
			}
		} else {
			result, err := c.request(runCtx, "session/new", buildHermesSessionParams(cwd, opts.Model, mcpServers))
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionResult = result
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "hermes session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionCurrentModel = extractACPCurrentModelID(result)
			if effectiveModel == "" {
				effectiveModel = sessionCurrentModel
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("hermes session created", "session_id", sessionID)
		// Mid-flight pin: daemon PinTaskSession keys off MessageStatus+SessionID.
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

		// 3. If the caller picked a model (via agent.model from the
		// UI dropdown), ask hermes to switch the session to it
		// before we send any prompt. Hermes' _build_model_state
		// exposes modelId as `provider:model` — we pass that
		// through verbatim. This MUST fail the task on error:
		// if we silently fell back to hermes' default model the
		// user would think their pick was honoured while the
		// task actually ran on something else.
		//
		// Skip the call when the session already reports this exact model as
		// current. Hermes' set_model re-runs provider auto-detection on the
		// model id, and for a `provider:model` id whose parsed provider equals
		// the session's current provider it can mis-route to a different
		// provider (e.g. custom:deepseek-v4-pro → OpenRouter) and fail with an
		// auth error. Re-selecting the model the session is already on is pure
		// downside. An empty sessionCurrentModel (older runtime or unparsable
		// state) falls through and still sends set_model, preserving prior
		// behaviour. See MUL-5029 / NousResearch/hermes-agent#59089.
		if opts.Model != "" && effectiveModel == sessionCurrentModel {
			b.cfg.Logger.Info("hermes session already on requested model; skipping redundant set_model",
				"model", opts.Model,
				"session_id", sessionID,
			)
		} else if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("hermes set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so
					// the daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "hermes",
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
			b.cfg.Logger.Info("hermes session model set", "model", opts.Model)
		}

		// 3b. Apply a persisted thinking override through whichever effort
		// option this session advertises. Which binary answered decides
		// whether anything happens: jcode advertises `reasoning_effort` and
		// threads it into the provider request, while Hermes Agent advertises
		// no configOptions at all and the helper no-ops. Unlike set_model
		// above this must NOT fail the task — an effort we could not apply
		// still runs the prompt at the runtime's own default.
		//
		// sessionResult stops describing the live session once set_model runs,
		// because an ACP effort option may depend on the current model.
		applyACPEffortOption(runCtx, c.request, "hermes", b.cfg.Logger,
			sessionID, sessionResult, opts.ThinkingLevel, opts.Model == "")

		// 4. Send the prompt and wait for PromptResponse.
		//
		// Do NOT prepend opts.SystemPrompt here. Hermes ACP loads project/context
		// files from cwd (AGENTS.md, .agent_context, etc.) itself; duplicating the
		// full runtime brief in the user prompt makes the request much larger and
		// has triggered upstream safety filters on otherwise ordinary tasks.
		// Flip the gate
		// just before the request so any history replay flushed during
		// initialize / session setup stays dropped, but every notification
		// belonging to this turn is processed.
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": hermesTurnText(prompt, opts.ResumeExpected, resumeLanded, opts.ResumeContinuityNotice)},
			},
		})
		if err != nil {
			// If the request itself failed (not just context cancelled),
			// check if the context was cancelled/timed out.
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("hermes timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// The agent no longer knows the session we resumed.
					// Hermes echoes the requested id back from
					// session/resume even when the session is gone, so
					// resolveResumedSessionID can't catch this — it only
					// surfaces here, at prompt time. Return an empty
					// SessionID so the daemon's resume-failure fallback
					// retries with a fresh session and stores the
					// replacement id; keeping the stale id makes every
					// future dispatch on this (agent, issue) fail the
					// same way.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "hermes",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			// The prompt completed. Check if we got a promptDone result
			// from the response parsing.
			select {
			case pr := <-promptDone:
				promptStopReason = pr.stopReason
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "hermes cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForHermesNotificationQuiescence(runCtx, activity, readerDone)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("hermes finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Close stdin first so Hermes can observe EOF and exit cleanly. Keep the
		// process alive while stdout/stderr drain; cancelling at the prompt
		// response boundary can truncate final notifications that arrive just
		// after the response.
		stdin.Close()

		// Wait for the stdout reader and stderr copier so all output is
		// accumulated and the provider-error sniffer
		// has every byte the child wrote before we consult it for failure
		// promotion. Skipping this leaves a small race where stopReason=
		// end_turn arrives over stdout while the stderr 429 / usage-limit
		// lines are still in transit, causing the promoted error message
		// to fall through to the synthetic agent-text fallback. If Hermes does
		// not honor stdin EOF within the bound, cancel it and join both readers
		// before accessing their buffers.
		if !waitForHermesPipeDrain(readerDone, stderrDone, hermesReaderDrainGrace) {
			b.cfg.Logger.Warn("hermes did not close output pipes after stdin EOF; forcing shutdown",
				"pid", cmd.Process.Pid,
				"grace", hermesReaderDrainGrace.String(),
			)
			cancel()
			<-readerDone
			<-stderrDone
		}
		streamingCurrentTurn.Store(false)

		finalOutput, providerErrorOutput := deliverable.result()

		// Hermes reports stopReason=end_turn even when the upstream
		// LLM call ultimately fails (HTTP 429 rate-limit, expired
		// token, ...). promoteACPResultOnProviderError flips the
		// status to "failed" when either the stderr sniffer saw a
		// *terminal* failure marker (not just a transient per-attempt
		// warning), the agent text stream contains the synthetic
		// "API call failed after N retries..." turn the adapter
		// injects on give-up, or there's no output to fall back on.
		// It reads the full text stream, not the deliverable, so a
		// give-up turn that lands before a tool call stays visible.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		// A resumed session Hermes could not rebuild.
		//
		// Evaluated HERE, not at the quiescence boundary above: the quiet
		// window closing does not end the turn. stdin EOF and the pipe drain
		// do, and Hermes legitimately delivers a turn's final chunk in that
		// gap — TestHermesBackendDrainsLateFinalNotificationAfterPromptResponse
		// exists because of it. Deciding earlier would freeze
		// turnActivity == 0 while a real answer was still in flight and
		// discard a healthy session (plus re-run the turn) for a runtime that
		// merely answered slowly. By this point streamingCurrentTurn is off
		// and every accepted update has been counted, so the reading is final.
		//
		// Runs after the promotion above on purpose: when the sniffer captured
		// why the rebuild failed (e.g. the provider identity the session
		// persisted no longer resolves), that message is far more useful to the
		// user than the generic one here, so we only supply a reason when
		// nothing else did. Without this, such a turn reports "completed" with
		// empty output — a task that silently did nothing at all.
		if hermesResumeSessionLost(opts.ResumeSessionID, promptStopReason, turnActivity.Load()) {
			b.cfg.Logger.Warn("resumed session refused with no agent activity; treating it as gone and clearing the session id so the daemon retries fresh",
				"backend", "hermes",
				"session_id", sessionID,
			)
			if finalStatus == "completed" {
				finalStatus = "failed"
				finalError = hermesResumeLostError
			}
			sessionID = ""
			resumeRejected = true
		}

		// Build usage map.
		u := c.accumulatedUsage()

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

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// waitForHermesNotificationQuiescence gives the stdout reader a bounded chance
// to consume session updates emitted just after session/prompt returns. Hermes
// may deliver the final agent_message_chunk after the response; closing stdin
// or cancelling immediately at that boundary loses the user-visible answer.
func waitForHermesNotificationQuiescence(ctx context.Context, activity <-chan struct{}, readerDone <-chan struct{}) {
	waitForACPNotificationQuiescence(ctx, activity, readerDone, hermesNotificationQuietTime, hermesReaderDrainGrace)
}

// acpNotificationQuietTime is the default lull the shared drain waits out
// before concluding an ACP agent has stopped emitting notifications. It is a
// protocol-level heuristic rather than a per-backend trait, so backends that
// have no reason to differ share it; the hard bound stays per-backend.
const acpNotificationQuietTime = 250 * time.Millisecond

// waitForACPNotificationQuiescence gives the shared ACP stdout reader a
// bounded chance to consume notifications a backend may emit just after its
// session/prompt response returns. Closing stdin and cancelling the context at
// the response boundary otherwise races the reader and silently truncates the
// final text or usage update.
//
// It returns as soon as any of these happens, so an agent that holds stdout
// open forever cannot stall the turn: no notification arrived for quiet, the
// reader finished, hard elapsed, or ctx was cancelled.
func waitForACPNotificationQuiescence(ctx context.Context, activity <-chan struct{}, readerDone <-chan struct{}, quiet, hard time.Duration) {
	quietTimer := time.NewTimer(quiet)
	defer quietTimer.Stop()
	hardTimer := time.NewTimer(hard)
	defer hardTimer.Stop()

	for {
		select {
		case <-activity:
			if !quietTimer.Stop() {
				select {
				case <-quietTimer.C:
				default:
				}
			}
			quietTimer.Reset(quiet)
		case <-quietTimer.C:
			return
		case <-readerDone:
			return
		case <-hardTimer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

func waitForHermesPipeDrain(readerDone, stderrDone <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for readerDone != nil || stderrDone != nil {
		select {
		case <-readerDone:
			readerDone = nil
		case <-stderrDone:
			stderrDone = nil
		case <-timer.C:
			return false
		}
	}
	return true
}

// ── hermesClient: ACP JSON-RPC 2.0 transport ──

type hermesPromptResult struct {
	stopReason string
	usage      acpUsageSnapshot
	// modelID is the model the agent actually billed this turn against, as
	// reported on `result._meta.modelId`. Empty for agents that don't report
	// it. Backends use it to attribute usage when the session handshake
	// didn't surface a model id (see grok.go).
	modelID string
}

type hermesClient struct {
	cfg          Config
	stdin        interface{ Write([]byte) (int, error) }
	writeMu      sync.Mutex // serialises stdin.Write calls across goroutines
	mu           sync.Mutex
	nextID       int
	pending      map[int]*pendingRPC
	sessionID    string
	onMessage    func(Message)
	onPromptDone func(hermesPromptResult)
	// selectPermission lets an ACP dialect narrow the generic headless
	// permission policy. Reasonix uses this to reject user questions and
	// fresh-human approvals that also happen to carry allow_once options.
	// Nil preserves the shared ACP policy for existing backends.
	selectPermission func(json.RawMessage) (optionID string, grant bool, ok bool)
	// onNotification observes vendor notifications that are not session/update.
	// Existing backends leave it nil; the callback must do its own method and
	// lifecycle filtering.
	onNotification func(method string, params json.RawMessage)
	// onActivity observes accepted ACP session updates. Hermes and Grok use it
	// to retain a short post-response drain window; other ACP backends leave it
	// nil and keep their existing lifecycle behavior.
	onActivity func()
	// acceptNotification can drop ACP session updates before dispatching to
	// handlers that mutate client state such as usage or pending tool calls.
	acceptNotification func(updateType string) bool
	// toolStartCarriesFinalInput marks a dialect whose tool_call start frame is
	// the only place a call's input ever appears, so MessageToolUse can be
	// emitted as soon as the call starts instead of being held until it
	// completes. Waiting cannot yield more input for such a dialect, and only
	// costs the run its in-flight visibility.
	//
	// It is a vendor-verified compatibility exception, so the hermes backend
	// scopes it to Config.BuiltinRuntime the same way
	// acpToleratesOmittedMcpCapabilities does: `protocol_family: hermes` with
	// `command_name: jcode` reaches this backend as "hermes" while being an
	// unrelated implementation, and only the real Hermes Agent binary is known
	// to behave this way — acp_adapter/tools.py's build_tool_call passes
	// `raw_input=None if tool_name in _POLISHED_TOOLS else arguments`, and
	// build_tool_complete passes none at all. Unset means deferring, so a
	// custom hermes-family runtime that supplies rawInput on a later update
	// still has it recorded.
	//
	// Other backends leave it false too — Kimi streams its args across updates,
	// so for it the start frame is genuinely incomplete.
	toolStartCarriesFinalInput bool

	// pendingTools buffers the args for tool calls whose input streams in
	// across multiple ACP tool_call_update messages (kimi does this —
	// tokens from the LLM arrive one at a time, and each update carries
	// the cumulative args JSON so far). We defer emitting MessageToolUse
	// until we either see status=completed/failed or have a full arg set,
	// so the UI never sees a half-written command like `{"comma`.
	toolMu       sync.Mutex
	pendingTools map[string]*pendingToolCall

	usageMu sync.Mutex
	usage   acpUsageAccumulator

	// terminalEnabled is only set for ACP runtimes whose client-side terminal
	// calls are implemented below. Keeping it opt-in avoids advertising a
	// capability to Hermes-family runtimes that do not need it.
	terminalEnabled bool
	terminalCtx     context.Context
	terminalCwd     string
	terminalEnv     []string
	terminalMu      sync.Mutex
	terminals       map[string]*acpTerminal
	nextTerminalID  int
}

// pendingToolCall buffers state for a tool call while its arguments
// are streaming in. One entry per ACP toolCallId.
type pendingToolCall struct {
	toolName string         // already mapped via hermesToolNameFromTitle
	input    map[string]any // from rawInput when the agent sends it up front (hermes)
	argsText string         // accumulated `content[].text` args (kimi, cumulative)
	emitted  bool           // whether we've already sent MessageToolUse
}

// writeLine serialises concurrent JSON-RPC writes so request() (main
// goroutine) and handleAgentRequest() (reader goroutine) don't
// interleave frames. The pipe itself is atomic for small writes, but
// we also want deterministic ordering under contention.
func (c *hermesClient) writeLine(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(data)
	return err
}

func (c *hermesClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *hermesClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *hermesClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// Agent → client request: has id + method (no result / error yet).
	// Kimi and Hermes both use session/request_permission; if we don't
	// answer, the agent blocks for its internal timeout and the task
	// hangs. HERMES_YOLO_MODE=1 only suppresses Hermes' dangerous-shell-
	// command prompts (tools/approval.py); its ACP edit-approval guard
	// (acp_adapter/edit_approval.py) still asks before every file write,
	// so we must handle these requests for Hermes too.
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleAgentRequest(raw)
			return
		}
	}

	// Notification (no id, has method) — session updates from Hermes.
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

// handleAgentRequest replies to JSON-RPC requests the agent sends
// us (agent → client direction). Kimi's ACP terminal capability is
// implemented here alongside the permission request handling: the daemon is
// headless and cannot actually prompt a user, so we answer permission requests
// ourselves — granting when a safe option is offered, otherwise declining
// just this action or failing closed (see below).
//
// The reply MUST select one of the optionIds the agent actually
// offered — the ACP permission contract is "pick from these options",
// and an id the agent never offered is treated as a denial. Hermes'
// edit-approval path offers only ["allow_once","deny"] and rejects
// anything but exactly "allow_once" (acp_adapter/edit_approval.py), so
// the previous hardcoded "approve_for_session" silently blocked every
// file write on the Hermes ACP runtime (GitHub multica#5300).
// selectACPPermissionOption picks an option the agent offered — a safe
// grant when one exists, otherwise an offered single-use reject to deny
// just this action — and we fail closed with a protocol error when the
// request offers nothing safely selectable, never a permanent grant or a
// whole-turn "cancelled".
func (c *hermesClient) handleAgentRequest(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	rawID, ok := raw["id"]
	if !ok {
		return
	}
	if method == "terminal/wait_for_exit" && c.terminalEnabled {
		// wait_for_exit is intentionally long-lived. The ACP reader must remain
		// available for the output polling and terminal/kill requests Kimi sends
		// while this request is pending.
		id := append(json.RawMessage(nil), rawID...)
		params := append(json.RawMessage(nil), raw["params"]...)
		go func() {
			result, err := c.acpTerminalResponse(method, params)
			if err != nil {
				c.writeAgentRequestResponse(method, map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"error": map[string]any{
						"code":    -32602,
						"message": err.Error(),
					},
				})
				return
			}
			c.writeAgentRequestResponse(method, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  result,
			})
		}()
		return
	}

	var resp map[string]any
	switch method {
	case "terminal/create":
		if !c.terminalEnabled {
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"error": map[string]any{
					"code":    -32601,
					"message": "terminal capability is not enabled",
				},
			}
			break
		}
		result, err := c.acpTerminalCreate(raw["params"])
		if err != nil {
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"error": map[string]any{
					"code":    -32602,
					"message": err.Error(),
				},
			}
			break
		}
		resp = map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(rawID), "result": result}
	case "terminal/output", "terminal/wait_for_exit", "terminal/kill", "terminal/release":
		if !c.terminalEnabled {
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"error": map[string]any{
					"code":    -32601,
					"message": "terminal capability is not enabled",
				},
			}
			break
		}
		result, err := c.acpTerminalResponse(method, raw["params"])
		if err != nil {
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"error": map[string]any{
					"code":    -32602,
					"message": err.Error(),
				},
			}
			break
		}
		resp = map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(rawID), "result": result}
	case "session/request_permission":
		selector := c.selectPermission
		if selector == nil {
			selector = selectACPPermissionOption
		}
		optionID, grant, ok := selector(raw["params"])
		if ok {
			// Select an offered option — either a safe grant (approve) or,
			// when no safe grant exists, an offered reject_once (deny THIS
			// action). Both are ACP "selected" outcomes; we deliberately do
			// NOT reply "cancelled" here, which means the whole prompt turn
			// was cancelled — other ACP backends sharing this client (kimi,
			// kiro, ...) would abort the entire task, not just this action.
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"result": map[string]any{
					"outcome": map[string]any{
						"outcome":  "selected",
						"optionId": optionID,
					},
				},
			}
			if grant {
				c.cfg.Logger.Debug("auto-approved agent permission request", "method", method, "optionId", optionID)
			} else {
				c.cfg.Logger.Warn("no safe grant offered; selecting offered reject option", "method", method, "optionId", optionID)
			}
		} else {
			// The request offered nothing we can safely select: no safe grant
			// and no single-use reject_once (empty, malformed, permanent-only,
			// or reject_always-only). Return a protocol error rather than
			// fabricate an un-offered id or a whole-turn "cancelled".
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"error": map[string]any{
					"code":    -32603,
					"message": "no auto-selectable permission option offered",
				},
			}
			c.cfg.Logger.Warn("no safely selectable permission option offered; returning error", "method", method)
		}
	default:
		// Unknown agent→client method — reply with standard "method
		// not found" so the agent doesn't block waiting for us. Better
		// than silence: the agent can decide how to proceed.
		resp = map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(rawID),
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found: " + method,
			},
		}
		c.cfg.Logger.Debug("unhandled agent→client request", "method", method)
	}

	c.writeAgentRequestResponse(method, resp)
}

func (c *hermesClient) writeAgentRequestResponse(method string, resp map[string]any) {
	data, err := json.Marshal(resp)
	if err != nil {
		logger := c.cfg.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("marshal agent-request response", "method", method, "error", err)
		return
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		logger := c.cfg.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("write agent-request response", "method", method, "error", err)
	}
}

// acpPermissionOption is one entry in a session/request_permission
// request's `options` array. `kind` is the ACP-level classification
// (allow_once / allow_always / reject_once / reject_always); `optionId`
// is the agent-defined opaque string we echo back to select that option.
type acpPermissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}

// ACP v1 PermissionOptionKind values. Only the two allow kinds grant;
// any other or unknown kind is treated as non-granting so a future or
// abnormal kind can never be auto-approved. reject_once denies a single
// action (the only reject we auto-select — reject_always would persist a
// denial the way allow_always persists a grant).
// https://agentclientprotocol.com/protocol/v1/schema#permissionoptionkind
const (
	acpKindAllowOnce   = "allow_once"
	acpKindAllowAlways = "allow_always"
	acpKindRejectOnce  = "reject_once"
)

// acpSessionScopedOptionIDs are optionIds known to grant for the current
// session only, without persisting a decision. Both Hermes' "allow_session"
// and its permanent "allow_always" option carry ACP kind "allow_always"
// (ACP has no session-scoped kind), so kind alone cannot tell them apart —
// we recognise the session-scoped ones by id. "approve_for_session" is the
// equivalent id other ACP backends use.
var acpSessionScopedOptionIDs = []string{"allow_session", "approve_for_session"}

// selectACPPermissionOption decides how to auto-answer a
// session/request_permission. It returns the offered optionId to select,
// whether that selection grants (true) or denies (false) the action, and
// ok=false when the request offers nothing safely selectable (the caller
// then returns a protocol error rather than fabricate an outcome).
//
// It only ever returns an id the agent actually offered. Per review of
// GitHub multica#5300 it refuses to auto-select a permanent "allow_always"
// grant — on Hermes that persists to the runtime owner's on-disk allowlist
// and would outlive the task (ACP v1 allow_always "remembers the choice").
// Grant nature is decided purely by the explicit ACP kind, never by the
// opaque optionId, so unknown kinds fail closed. Order of preference:
//
//  1. a known session-scoped grant id;
//  2. any single-use (kind=allow_once) grant;
//  3. an offered single-use reject_once — deny just this action rather than
//     reply "cancelled", which other ACP backends read as cancelling the
//     whole prompt turn.
func selectACPPermissionOption(params json.RawMessage) (optionID string, grant bool, ok bool) {
	var p struct {
		Options []acpPermissionOption `json:"options"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return "", false, false
		}
	}

	// 1. A known session-scoped grant id, if actually offered with a grant kind.
	for _, want := range acpSessionScopedOptionIDs {
		for _, opt := range p.Options {
			if opt.OptionID == want && isACPGrantKind(opt.Kind) {
				return opt.OptionID, true, true
			}
		}
	}
	// 2. Any single-use grant. kind=allow_once is inherently scoped to this
	//    one action, so it is safe regardless of the (opaque) optionId — this
	//    also covers agents that use non-standard option ids.
	for _, opt := range p.Options {
		if opt.OptionID != "" && strings.EqualFold(strings.TrimSpace(opt.Kind), acpKindAllowOnce) {
			return opt.OptionID, true, true
		}
	}
	// 3. No safe grant: deny THIS action by selecting an offered reject_once.
	for _, opt := range p.Options {
		if opt.OptionID != "" && strings.EqualFold(strings.TrimSpace(opt.Kind), acpKindRejectOnce) {
			return opt.OptionID, false, true
		}
	}
	// 4. Nothing safely selectable (empty, malformed, permanent-only, or
	//    reject_always-only). Signal the caller to return a protocol error.
	return "", false, false
}

// isACPGrantKind reports whether an ACP PermissionOptionKind grants the
// action. Only the two current allow kinds qualify; every other or unknown
// value is non-granting, so grant detection fails closed.
func isACPGrantKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case acpKindAllowOnce, acpKindAllowAlways:
		return true
	default:
		return false
	}
}

// acpRPCError is a JSON-RPC error frame returned by the agent process.
// It renders exactly like the flat string handleResponse used to build
// with fmt.Errorf, so logs and surfaced task errors are unchanged, but
// keeps the code and message structured so callers can branch on the
// error class (see isACPSessionNotFound) instead of parsing text.
type acpRPCError struct {
	Method  string
	Code    int
	Message string
	Data    string
}

func (e *acpRPCError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("%s: %s (code=%d, data=%s)", e.Method, e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("%s: %s (code=%d)", e.Method, e.Message, e.Code)
}

// isACPSessionNotFound reports whether err is the agent rejecting a
// session id it no longer knows. Runtimes signal this with codes and
// wording that vary — Hermes says "Session not found" under -32603
// (Internal error), Kiro puts "No session found with id ..." in
// `data` under -32603, and kimi-cli raises invalid_params (-32602)
// with {"session_id": "Session not found"} in `data` for every
// unknown-session path (src/kimi_cli/acp/server.py), Reasonix says
// "session/resume: unknown session <id>" under -32602, and ZeroClaw defines
// its own SESSION_NOT_FOUND = -32000 in the implementation-defined range
// (zeroclaw-api/src/jsonrpc.rs) — so neither the code nor one runtime's exact
// wording is discriminating and all of them are matched. The wording check
// still carries the decision: -32000 is a generic server-error code, and a
// transient failure reported under it must not read as a lost session.
func isACPSessionNotFound(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if rpcErr.Code != -32603 && rpcErr.Code != -32602 && rpcErr.Code != -32002 && rpcErr.Code != -32000 {
		return false
	}
	text := strings.ToLower(rpcErr.Message + " " + rpcErr.Data)
	return strings.Contains(text, "session not found") ||
		strings.Contains(text, "no session found") ||
		strings.Contains(text, "unknown session")
}

// isACPHeldByProcess reports whether a session/load error means the session
// is still locked by a prior process that has not released it yet. Dim < 0.3.10
// reports this permanently; 0.3.10+ releases on graceful exit or after ~5s.
// The caller retries a bounded number of times before giving up.
func isACPHeldByProcess(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	text := strings.ToLower(rpcErr.Message + " " + rpcErr.Data)
	return strings.Contains(text, "held by another process")
}

// hermesResumeLostError is the fallback reason for a resumed session Hermes
// could not rebuild, used only when nothing more specific was captured.
const hermesResumeLostError = "hermes could not restore the resumed session; it refused the turn without running the agent"

// hermesResumeSessionLost reports whether a *successful* session/prompt
// response means the session we resumed is gone on the agent side.
//
// isACPSessionNotFound cannot answer this, because Hermes never reports an
// unknown session as a JSON-RPC error. Its ACP adapter answers
// `session/prompt` for a session it cannot load with an ordinary success
// frame carrying stopReason=refusal (acp_adapter/server.py, the one place it
// emits that reason), and `session/resume` returns an ordinary success frame
// whatever happened — the ACP ResumeSessionResponse schema has no sessionId
// field at all, unlike NewSessionResponse. Nothing in the exchange is an
// error, which is why the isACPSessionNotFound branches at set_model and
// prompt time never fire for this runtime and every later dispatch on the same
// (agent, issue) pair loops on the dead session (GH #6150).
//
// This stays necessary alongside resolveHermesResumedSessionID, which reads
// `_meta.hermes.sessionProvenance` to catch the rebind at resume time: that
// covers the runtime answering with a DIFFERENT session, while this covers it
// answering with the one we asked for and then refusing to run it.
//
// Both conditions are required. stopReason=refusal alone is a legitimate
// model refusal, and refusing a resumed turn after real work is not a lost
// session — only a refusal with no agent activity whatsoever (no text, no
// thought, no tool call) is Hermes telling us it never had the session. The
// fresh-session retry this unlocks is itself gated on tools == 0 in
// shouldRetryWithFreshSession, so a turn that acted is never re-run.
func hermesResumeSessionLost(resumeSessionID, stopReason string, turnActivity int64) bool {
	return resumeSessionID != "" && stopReason == "refusal" && turnActivity == 0
}

func (c *hermesClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		// Try float (JSON numbers are floats by default).
		var fid float64
		if err := json.Unmarshal(raw["id"], &fid); err != nil {
			return
		}
		id = int(fid)
	}

	c.mu.Lock()
	pr, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if !ok {
		return
	}

	if errData, hasErr := raw["error"]; hasErr {
		var rpcErr struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		// JSON-RPC `data` carries the provider-specific reason (e.g. Kiro
		// returns "No session found with id" for code=-32603). Surface it
		// in the wrapped error so daemon logs / UI can show *why* the
		// agent failed instead of a bare "Internal error". `data` may be
		// any JSON value: render strings unquoted, everything else as raw
		// JSON.
		detail := ""
		if len(rpcErr.Data) > 0 && string(rpcErr.Data) != "null" {
			var s string
			if err := json.Unmarshal(rpcErr.Data, &s); err == nil {
				detail = s
			} else {
				detail = string(rpcErr.Data)
			}
		}
		pr.ch <- rpcResult{err: &acpRPCError{Method: pr.method, Code: rpcErr.Code, Message: rpcErr.Message, Data: detail}}
	} else {
		// If this is a prompt response, extract usage and stop reason.
		if pr.method == "session/prompt" {
			c.extractPromptResult(raw["result"])
		}
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

func (c *hermesClient) extractPromptResult(data json.RawMessage) {
	var resp struct {
		StopReason string          `json:"stopReason"`
		Usage      json.RawMessage `json:"usage"`
		Meta       json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	pr := hermesPromptResult{
		stopReason: resp.StopReason,
		modelID:    parseACPModelIDFromMeta(resp.Meta),
	}
	var usage acpUsageSnapshot
	if len(resp.Usage) > 0 && string(resp.Usage) != "null" {
		usage = parseACPTokenUsageSnapshot(resp.Usage)
	}
	// Some agents (notably xAI Grok Build) put per-turn metering under
	// result._meta instead of, or in addition to, the standard top-level
	// usage field. Reconcile both shapes so partial mirrors cannot drop a
	// cache bucket or provider-reported cost.
	pr.usage = usage.withFallback(parseACPTokenUsageSnapshotFromMeta(resp.Meta))

	if c.onPromptDone != nil {
		c.onPromptDone(pr)
	}
}

// acpTokenUsagePresent reports whether any token counter is non-zero.
func acpTokenUsagePresent(u TokenUsage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0
}

func acpUsagePresent(u TokenUsage) bool {
	return acpTokenUsagePresent(u) || u.CostUSDTicks > 0
}

// parseACPModelIDFromMeta pulls the model id off an ACP result `_meta`
// object. Grok Build stamps every turn with `_meta.modelId`, which is the
// only authoritative statement of what the turn was billed against — the
// session handshake reports a model id on `session/new` but NOT on
// `session/load`, so a resumed session has no other source.
func parseACPModelIDFromMeta(meta json.RawMessage) string {
	if len(meta) == 0 || string(meta) == "null" {
		return ""
	}
	var r struct {
		ModelID      string `json:"modelId"`
		ModelIDSnake string `json:"model_id"`
	}
	if err := json.Unmarshal(meta, &r); err != nil {
		return ""
	}
	if id := strings.TrimSpace(r.ModelID); id != "" {
		return id
	}
	return strings.TrimSpace(r.ModelIDSnake)
}

func (c *hermesClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)
	if c.onNotification != nil {
		c.onNotification(method, raw["params"])
	}

	if method != "session/update" && method != "session/notification" {
		return
	}

	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	if len(params.Update) == 0 {
		return
	}

	updateType, updateData := normalizeACPUpdate(params.Update)
	if c.acceptNotification != nil && !c.acceptNotification(updateType) {
		return
	}
	if c.onActivity != nil {
		c.onActivity()
	}

	switch updateType {
	case "agent_message_chunk":
		c.handleAgentMessage(updateData)
	case "agent_thought_chunk":
		c.handleAgentThought(updateData)
	case "tool_call":
		c.handleToolCallStart(updateData)
	case "tool_call_update":
		c.handleToolCallUpdate(updateData)
	case "usage_update":
		c.handleUsageUpdate(updateData)
	case "turn_end":
		c.extractPromptResult(updateData)
	}
}

func normalizeACPUpdate(data json.RawMessage) (string, json.RawMessage) {
	var updateType struct {
		SessionUpdate string `json:"sessionUpdate"`
		Type          string `json:"type"`
	}
	_ = json.Unmarshal(data, &updateType)
	if updateType.SessionUpdate != "" {
		return normalizeACPUpdateType(updateType.SessionUpdate), data
	}
	if updateType.Type != "" {
		return normalizeACPUpdateType(updateType.Type), data
	}

	// Some ACP implementations serialize enum variants as an externally
	// tagged object: {"agentMessageChunk": {"content": ...}}.
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper) == 1 {
		for k, v := range wrapper {
			return normalizeACPUpdateType(k), v
		}
	}

	return "", data
}

func normalizeACPUpdateType(t string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(t), "_", ""), "-", ""))
	switch key {
	case "agentmessagechunk":
		return "agent_message_chunk"
	case "agentthoughtchunk":
		return "agent_thought_chunk"
	case "toolcall":
		return "tool_call"
	case "toolcallupdate":
		return "tool_call_update"
	case "usageupdate":
		return "usage_update"
	case "turnend", "endturn":
		return "turn_end"
	default:
		return ""
	}
}

func (c *hermesClient) handleAgentMessage(data json.RawMessage) {
	var msg struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Content.Text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageText, Content: msg.Content.Text})
	}
}

func (c *hermesClient) handleAgentThought(data json.RawMessage) {
	var msg struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Content.Text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageThinking, Content: msg.Content.Text})
	}
}

func (c *hermesClient) handleToolCallStart(data json.RawMessage) {
	var msg struct {
		ToolCallID string            `json:"toolCallId"`
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	toolName := hermesToolNameFromTitle(msg.Title, msg.Kind)
	if toolName == "" {
		toolName = msg.Name
	}
	rawInput := msg.RawInput
	if rawInput == nil {
		rawInput = msg.Input
	}
	if rawInput == nil {
		rawInput = msg.Parameters
	}

	// Hermes pre-populates rawInput on the initial tool_call — emit
	// MessageToolUse immediately so the UI can show the tool invocation
	// live. Record the emission so handleToolCallUpdate doesn't re-emit
	// on completion.
	if rawInput != nil {
		c.trackTool(msg.ToolCallID, &pendingToolCall{
			toolName: toolName,
			input:    rawInput,
			emitted:  true,
		})
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: msg.ToolCallID,
				Input:  rawInput,
			})
		}
		return
	}

	// No rawInput: whatever the start frame carries is in the content blocks.
	argsText := extractACPToolCallText(msg.Content)

	// Emitting at the start frame is what makes an in-flight tool visible at
	// all: nothing is otherwise emitted until tool_call_update completes, so a
	// tool that stalls renders as a blank run with no visible tool call — the
	// symptom in GH#6583 — and, because the daemon's in-flight tool counter
	// only advances on MessageToolUse, such a call never gets charged to
	// AgentToolWatchdog and is judged by the much shorter AgentIdleWatchdog
	// instead, so a legitimately long tool call is force-stopped early.
	//
	// Only a dialect that will never send input later can do this without
	// losing the input — see toolStartCarriesFinalInput.
	if c.toolStartCarriesFinalInput {
		// The content blocks are display output, not input, so they are used as
		// Input only for the one shape that demonstrably IS the invocation.
		// Everything else reports no input rather than passing a rendering
		// ("Preparing write to <path>") off as the call's arguments.
		var input map[string]any
		if acpToolCallStartCarriesInvocation(toolName, argsText) {
			input = parseToolArgsJSON(argsText)
		}
		c.trackTool(msg.ToolCallID, &pendingToolCall{
			toolName: toolName,
			argsText: argsText,
			input:    input,
			emitted:  true,
		})
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: msg.ToolCallID,
				Input:  input,
			})
		}
		return
	}

	// Kimi streams args token-by-token across tool_call_update messages;
	// the initial tool_call often carries an empty content block. Buffer
	// the tool and defer MessageToolUse emission to avoid the UI seeing
	// a command with `{""` as its input.
	c.trackTool(msg.ToolCallID, &pendingToolCall{
		toolName: toolName,
		argsText: argsText,
		emitted:  false,
	})
}

// acpToolCallStartCarriesInvocation reports whether a tool_call start frame's
// content can be reported as the call's input rather than as display output.
//
// It deliberately recognises exactly one shape: a `terminal` call whose content
// is the `$ <command>` line. In ACP, `content` is display output produced by the
// tool call and `rawInput` is the input, so treating arbitrary content as input
// mis-records the invocation. Hermes suppresses rawInput for its whole
// "polished" tool set and renders each one differently: `terminal` renders
// `$ <command>` (the invocation), but `write_file` and `patch` render prose like
// "Preparing write to <path>", and the browser/web/media tools render their own
// summaries. Only the terminal rendering is the command itself; the rest report
// no input at all.
func acpToolCallStartCarriesInvocation(toolName, argsText string) bool {
	if toolName != "terminal" {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(argsText), "$ ")
}

func (c *hermesClient) handleToolCallUpdate(data json.RawMessage) {
	var msg struct {
		ToolCallID string            `json:"toolCallId"`
		Status     string            `json:"status"`
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		RawOutput  json.RawMessage   `json:"rawOutput"`
		Output     json.RawMessage   `json:"output"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	rawInput := msg.RawInput
	if rawInput == nil {
		rawInput = msg.Input
	}
	if rawInput == nil {
		rawInput = msg.Parameters
	}
	title := msg.Title
	if title == "" {
		title = msg.Name
	}

	// Mid-stream: only buffer updates. Kimi emits many of these per
	// tool call, each carrying the cumulative args JSON so far.
	if msg.Status != "completed" && msg.Status != "failed" {
		if pending := c.getPendingTool(msg.ToolCallID); pending != nil && !pending.emitted {
			if text := extractACPToolCallText(msg.Content); text != "" {
				// kimi streams the full cumulative args on every frame;
				// overwrite rather than concatenate.
				pending.argsText = text
			}
		}
		return
	}

	// Completion: emit any deferred MessageToolUse first, then the result.
	pending := c.takePendingTool(msg.ToolCallID)
	c.emitDeferredToolUse(pending, msg.ToolCallID, title, msg.Kind, rawInput)

	output := acpRawText(msg.RawOutput)
	if output == "" {
		output = acpRawText(msg.Output)
	}
	if output == "" {
		output = extractACPToolCallText(msg.Content)
	}
	if c.onMessage != nil {
		c.onMessage(Message{
			Type:   MessageToolResult,
			CallID: msg.ToolCallID,
			Output: output,
			Status: msg.Status,
		})
	}
}

// trackTool stores pending-tool state for a given callID. Lazy-inits
// the map so zero-value hermesClient values (common in tests) don't
// panic on the first tool call.
func (c *hermesClient) trackTool(callID string, p *pendingToolCall) {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		c.pendingTools = make(map[string]*pendingToolCall)
	}
	c.pendingTools[callID] = p
}

// getPendingTool returns the pending entry (may be nil) without
// removing it. Safe to call on a zero-value hermesClient.
func (c *hermesClient) getPendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	return c.pendingTools[callID]
}

// takePendingTool removes and returns the pending entry, or nil if
// none was tracked (e.g. the tool completed before we saw its start,
// or we missed the start frame).
func (c *hermesClient) takePendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	p := c.pendingTools[callID]
	delete(c.pendingTools, callID)
	return p
}

// emitDeferredToolUse emits a buffered MessageToolUse right before the
// matching MessageToolResult. Handles three cases:
//   - hermes tool: already emitted on tool_call → skip
//   - kimi tool with streamed args → parse accumulated JSON as Input
//   - unknown tool (completed arrived without a start frame) →
//     synthesize minimal info from the update's own fields
func (c *hermesClient) emitDeferredToolUse(
	p *pendingToolCall,
	callID, updateTitle, updateKind string,
	updateRawInput map[string]any,
) {
	if p != nil && p.emitted {
		return
	}

	var toolName string
	var input map[string]any

	switch {
	case p != nil && p.input != nil:
		// Pre-buffered rawInput path — shouldn't happen because we set
		// emitted=true in that case, but handle defensively.
		toolName = p.toolName
		input = p.input
	case p != nil:
		toolName = p.toolName
		// A rawInput on the update is the call's actual input, so it wins over
		// whatever the start frame rendered into content. ACP lets a start
		// frame carry only display text ("Preparing write to <path>") and
		// supply rawInput later; without this the display text would be
		// recorded as the invocation and the real input silently dropped.
		if updateRawInput != nil {
			input = updateRawInput
		} else {
			input = parseToolArgsJSON(p.argsText)
		}
	default:
		// No record of the start frame — fall back to the update's own
		// title/kind/rawInput so the UI at least sees the tool name.
		toolName = hermesToolNameFromTitle(updateTitle, updateKind)
		input = updateRawInput
	}

	if c.onMessage == nil {
		return
	}
	c.onMessage(Message{
		Type:   MessageToolUse,
		Tool:   toolName,
		CallID: callID,
		Input:  input,
	})
}

// parseToolArgsJSON turns kimi's accumulated args string into the
// structured map the UI expects under Message.Input. Kimi sends args
// as a JSON-encoded object (`{"command":"echo hi"}`), so a full JSON
// parse recovers the original tool-arg shape. On malformed input
// (streaming glitch, non-JSON tool) we preserve the raw text under a
// `text` key so the UI still has something to render.
func parseToolArgsJSON(argsText string) map[string]any {
	argsText = strings.TrimSpace(argsText)
	if argsText == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(argsText), &m); err == nil {
		return m
	}
	return map[string]any{"text": argsText}
}

// extractACPToolCallText concatenates the rendered text of every ACP
// block in a tool_call / tool_call_update's `content` array.
//
// Handles the two block types kimi emits:
//   - {type:"content", content:{type:"text", text:"..."}} — plain text
//     (shell output, tool args). Text is concatenated verbatim.
//   - {type:"diff", path, oldText, newText} — FileEdit output. Rendered
//     as a minimal unified-diff header so the UI distinguishes writes
//     from reads without needing a diff viewer.
//
// acpRawText renders an ACP output field (rawOutput / output) that may arrive
// as either a JSON string or a structured value. Some model adapters — notably
// Kiro's GPT-5.6 Sol path — send the completed tool_call_update's rawOutput as
// an object like {"items":[{"Json":{...}}]} rather than a string. Declaring
// that field as a Go string made json.Unmarshal fail, which made
// handleToolCallUpdate return early and silently DROP the entire update —
// including its status:"completed" — so the completion signal was lost and the
// task was wrongly marked failed (issue #5509 / MUL-4860). Accept both shapes.
func acpRawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Non-string (object / array / number): keep the raw JSON as text so the
	// output is preserved rather than discarded.
	return string(raw)
}

// Terminal blocks ({type:"terminal", terminalId}) reference a terminal whose
// output is served through terminal/output. The textual extractor has no
// payload to duplicate here; the ACP transport retains the terminal output.
func extractACPToolCallText(blocks []json.RawMessage) string {
	var b strings.Builder
	appendPiece := func(piece string) {
		if piece == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(piece)
	}
	for _, raw := range blocks {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			continue
		}
		switch kind.Type {
		case "content":
			var outer struct {
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(raw, &outer); err != nil || len(outer.Content) == 0 {
				continue
			}
			var inner struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(outer.Content, &inner); err != nil {
				continue
			}
			if inner.Type != "text" {
				continue
			}
			appendPiece(inner.Text)
		case "diff":
			var diff struct {
				Path    string `json:"path"`
				OldText string `json:"oldText"`
				NewText string `json:"newText"`
			}
			if err := json.Unmarshal(raw, &diff); err != nil || diff.Path == "" {
				continue
			}
			// Keep it tiny — a full unified diff can be huge and we're
			// really just recording "this tool wrote to this file".
			// The UI can re-read the file if it needs the actual content.
			var piece strings.Builder
			piece.WriteString("--- ")
			piece.WriteString(diff.Path)
			piece.WriteString("\n+++ ")
			piece.WriteString(diff.Path)
			if diff.OldText == "" {
				piece.WriteString("\n(new file, ")
				piece.WriteString(strconv.Itoa(len(diff.NewText)))
				piece.WriteString(" bytes)")
			} else {
				piece.WriteString("\n(edited: ")
				piece.WriteString(strconv.Itoa(len(diff.OldText)))
				piece.WriteString(" → ")
				piece.WriteString(strconv.Itoa(len(diff.NewText)))
				piece.WriteString(" bytes)")
			}
			appendPiece(piece.String())
		default:
			// terminal blocks, image blocks, unknown future types —
			// ignore. We have no way to inline-render them.
		}
	}
	return b.String()
}

func (c *hermesClient) handleUsageUpdate(data json.RawMessage) {
	var msg struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	c.mergeUsage(parseACPTokenUsageSnapshot(msg.Usage))
}

func (c *hermesClient) mergeUsage(usage acpUsageSnapshot) {
	c.usageMu.Lock()
	c.usage.merge(usage)
	c.usageMu.Unlock()
}

func (c *hermesClient) accumulatedUsage() TokenUsage {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return c.usage.TokenUsage
}

// ── Helpers ──

// extractACPSessionID pulls `sessionId` out of a session/new or
// session/resume response. Shared by all ACP backends (hermes, kimi, kiro,
// and anything else that follows the standard ACP schema).
func extractACPSessionID(result json.RawMessage) string {
	var r struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.SessionID
}

// extractACPAuthMethods returns the `authMethods` ids advertised in an ACP
// `initialize` response, in the order the agent listed them. Agents that
// require authentication (e.g. xAI's Grok Build) enumerate the accepted
// methods here; per the ACP flow the client MUST send `authenticate` with one
// of these ids before `session/new` / `session/load`. Agents that need no
// explicit auth omit the field, so an empty slice means "skip authenticate".
// A malformed response degrades to an empty slice (fail open on parsing so we
// don't wedge agents that never needed the step).
func extractACPAuthMethods(result json.RawMessage) []string {
	var r struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil
	}
	ids := make([]string, 0, len(r.AuthMethods))
	for _, m := range r.AuthMethods {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// extractACPCurrentModelID pulls the model selected by the ACP runtime out of
// a session/new or session/resume response. Hermes returns this when it uses
// its own default model, so token usage can still be attributed to a real model
// even when Multica did not pass an explicit agent.model override.
func extractACPCurrentModelID(result json.RawMessage) string {
	var r struct {
		Models struct {
			CurrentModelID      string `json:"currentModelId"`
			CurrentModelIDSnake string `json:"current_model_id"`
		} `json:"models"`
		CurrentModelID      string `json:"currentModelId"`
		CurrentModelIDSnake string `json:"current_model_id"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	for _, candidate := range []string{
		r.Models.CurrentModelID,
		r.Models.CurrentModelIDSnake,
		r.CurrentModelID,
		r.CurrentModelIDSnake,
	} {
		if model := strings.TrimSpace(candidate); model != "" {
			return model
		}
	}
	return ""
}

// resolveResumedSessionID picks which session id we should treat as live
// after a `session/resume` round-trip. Hermes (and other ACP servers)
// return the canonical sessionId in the response — when the local
// state.db has been wiped, the server silently creates a brand-new
// session and returns its new id rather than failing. If we keep using
// our requested id in that case, every subsequent session/prompt is
// addressed to a session the server doesn't know about and fails with
// JSON-RPC -32603. Returns (chosenID, changed). When the response is
// malformed or omits sessionId we fall back to the requested id so the
// happy path keeps working against older / non-conforming servers.
func resolveResumedSessionID(requested string, response json.RawMessage) (string, bool) {
	got := extractACPSessionID(response)
	if got == "" {
		return requested, false
	}
	return got, got != requested
}

// resolveHermesResumedSessionID is the Hermes-specific reading of a
// session/resume response: it returns the live session id and whether the
// resume actually LANDED on the session we asked for.
//
// The shared resolver above is not enough here because ACP's
// ResumeSessionResponse has no sessionId field at all (acp/schema.py
// ResumeSessionResponse), unlike NewSessionResponse. Hermes answers a resume
// of a session its state.db no longer holds by silently creating a fresh one
// (acp_adapter/server.py resume_session: "not found, creating new") and
// replies with an ordinary success frame, so the rebind used to be
// indistinguishable from a real resume: the daemon re-sent the same dead id
// every turn and the user got a conversation that restarted from zero with no
// error anywhere (GH #6806).
//
// Hermes does report the session it actually served the request with on
// `_meta.hermes.sessionProvenance`, which is the only signal available. A
// response carrying neither shape falls back to the requested id and reports
// landed=true, so a runtime that reports nothing keeps its previous behaviour
// rather than declaring every turn's history lost.
//
// That residual blind spot is bounded on the daemon side: the resume gate only
// hands a session id to a run whose session store actually holds a transcript
// (sessionHomeReachable), so a silent runtime can misreport a stale session but
// never an absent one.
func resolveHermesResumedSessionID(requested string, response json.RawMessage) (string, bool) {
	got := extractACPSessionID(response)
	if got == "" {
		got = extractACPProvenanceSessionID(response)
	}
	if got == "" {
		return requested, true
	}
	return got, got == requested
}

// extractACPProvenanceSessionID pulls the session id Hermes reports on
// `_meta.hermes.sessionProvenance.acpSessionId` — the ACP-facing id of the
// session the server actually served the request with. `currentHermesSessionId`
// is the agent-internal id of the same session and is deliberately not read:
// the two diverge when Hermes compresses a session into a successor, and only
// the ACP id is the one we address later prompts to.
func extractACPProvenanceSessionID(result json.RawMessage) string {
	var r struct {
		Meta struct {
			Hermes struct {
				SessionProvenance struct {
					ACPSessionID string `json:"acpSessionId"`
				} `json:"sessionProvenance"`
			} `json:"hermes"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return strings.TrimSpace(r.Meta.Hermes.SessionProvenance.ACPSessionID)
}

// hermesTurnText prepends the caller's continuity notice when this task meant
// to continue a conversation but the resume landed on a fresh session. Same
// contract as the codex backend's codexTurnInput: the notice is empty whenever
// the daemon's own prompt already carries it, so a turn can never pay for the
// paragraph twice.
func hermesTurnText(prompt string, resumeExpected, resumeLanded bool, notice string) string {
	if resumeExpected && !resumeLanded {
		return notice + prompt
	}
	return prompt
}

// buildHermesSessionParams constructs the params map for the ACP `session/new`
// request. The `model` field is only included when non-empty so Hermes falls
// back to its default only when no explicit model was configured.
//
// mcpServers should be the ACP-shaped array produced by buildACPMcpServers
// from the agent's mcp_config; a nil slice is normalised to an empty array
// so the wire request always carries the field (ACP requires it).
func buildHermesSessionParams(cwd, model string, mcpServers []any) map[string]any {
	if mcpServers == nil {
		mcpServers = []any{}
	}
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": mcpServers,
	}
	if model != "" {
		params["model"] = model
	}
	return params
}

// buildACPMcpServers translates an agent's Claude-style mcp_config
// (`{"mcpServers": {"<name>": {...}}}`) into the array shape that ACP's
// `session/new` and `session/load` requests expect.
//
// Each Claude-style entry maps to one of:
//
//   - Stdio:  `{name, command, args, env: [{name,value}, ...]}` —
//     when the entry has a `command` field. No `type` field is emitted;
//     ACP treats untagged entries as stdio.
//   - HTTP / SSE: `{type, name, url, headers: [{name,value}, ...]}` —
//     when the entry has a `url` field. `type` defaults to "http"; Claude's
//     "sse" and "streamable-http" / "http_streamable" aliases are accepted.
//
// Empty / null input returns an empty slice — the launch proceeds with no
// MCP servers (the existing default for ACP backends). Malformed top-level
// JSON returns an error so the launch fails closed, mirroring codex's
// `renderCodexMcpServersBlock` contract. Individual entries that have
// neither `command` nor `url` are skipped with a warning rather than
// failing the whole launch, so a single bad entry can't kill the agent.
//
// Output entries are sorted by name and each entry's env / headers are
// sorted by key, so the wire request is deterministic across reruns —
// useful for tests, log diffs, and reproducibility.
func buildACPMcpServers(raw json.RawMessage, logger *slog.Logger) ([]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []any{}, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		warnNonCanonicalMcpConfigKey(trimmed, logger)
		return []any{}, nil
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]any, 0, len(names))
	for _, name := range names {
		entry, err := convertACPMcpServer(name, parsed.McpServers[name])
		if err != nil {
			if logger != nil {
				logger.Warn("skipping invalid mcp_config entry", "name", name, "error", err)
			}
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

// acpAltMcpConfigKeys are top-level keys that runtime-native MCP config
// files use instead of Multica's canonical `mcpServers`: jcode and Kiro
// nest under `servers`, OpenCode under `mcp`, Codex's TOML under
// `mcp_servers`.
var acpAltMcpConfigKeys = []string{"servers", "mcp", "mcp_servers"}

// warnNonCanonicalMcpConfigKey logs when an mcp_config carries no
// `mcpServers` key but does hold servers under a runtime-native one.
//
// mcp_config is stored as opaque JSON, so pasting a runtime's own config
// file in is both easy and — until this warning — completely silent: the
// config saves, the daemon forwards an empty array, and the agent runs
// with no MCP tools and nothing logged anywhere (#6540). We only warn
// rather than adopt the entries, because the surrounding entry shapes are
// not guaranteed to match ACP's and guessing risks forwarding a
// half-understood config.
func warnNonCanonicalMcpConfigKey(raw json.RawMessage, logger *slog.Logger) {
	if logger == nil {
		return
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return
	}
	if _, ok := top["mcpServers"]; ok {
		return
	}
	for _, key := range acpAltMcpConfigKeys {
		nested, ok := top[key]
		if !ok {
			continue
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(nested, &entries); err != nil || len(entries) == 0 {
			continue
		}
		logger.Warn("mcp_config has no \"mcpServers\" key, so no MCP servers will be sent to the runtime",
			"found_key", key,
			"server_count", len(entries),
			"hint", `mcp_config must be shaped {"mcpServers": {"<name>": {...}}}; a runtime's own config file is not accepted verbatim`)
		return
	}
}

// convertACPMcpServer converts a single Claude-style entry into the ACP
// McpServer wire shape. Returns an error for entries that can't be
// classified (no command and no url).
func convertACPMcpServer(name string, raw json.RawMessage) (map[string]any, error) {
	var entry struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse entry: %w", err)
	}

	command := strings.TrimSpace(entry.Command)
	url := strings.TrimSpace(entry.URL)

	if command != "" {
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		envArr := make([]map[string]any, 0, len(entry.Env))
		for _, k := range sortedStringMapKeys(entry.Env) {
			envArr = append(envArr, map[string]any{
				"name":  k,
				"value": entry.Env[k],
			})
		}
		return map[string]any{
			"name":    name,
			"command": command,
			"args":    args,
			"env":     envArr,
		}, nil
	}

	if url != "" {
		t := strings.ToLower(strings.TrimSpace(entry.Type))
		switch t {
		case "sse":
			t = "sse"
		case "", "http", "streamable-http", "http_streamable":
			t = "http"
		default:
			// Unknown remote transport — degrade to "http" rather than fail.
			// ACP servers that don't recognise the type will reject the
			// session/new request and surface a real error to the user.
			t = "http"
		}
		headerArr := make([]map[string]any, 0, len(entry.Headers))
		for _, k := range sortedStringMapKeys(entry.Headers) {
			headerArr = append(headerArr, map[string]any{
				"name":  k,
				"value": entry.Headers[k],
			})
		}
		return map[string]any{
			"type":    t,
			"name":    name,
			"url":     url,
			"headers": headerArr,
		}, nil
	}

	return nil, fmt.Errorf("entry has neither command nor url")
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// acpMcpTransportCapabilities reports which remote MCP transports the ACP
// runtime advertised in its `initialize` response. Stdio is always
// supported (it's the baseline transport and the spec does not gate it),
// so it's not represented here.
// acpMcpCapabilityDeclaration classifies what an ACP `initialize` response
// told us about remote MCP transports. All three states filter identically
// under ACP v1 — an undeclared transport is unsupported — but the
// omitted-capabilities exception may only key off genuine silence, so
// "the runtime said nothing" has to stay distinguishable from "we could not
// read what the runtime said".
type acpMcpCapabilityDeclaration int

const (
	// acpMcpCapabilitiesInvalid is the zero value on purpose: a response we
	// could not parse, a non-object block, or one whose fields have the
	// wrong types. Anything that reaches this state fails closed, so an
	// accidental zero value can never widen access.
	acpMcpCapabilitiesInvalid acpMcpCapabilityDeclaration = iota
	// acpMcpCapabilitiesOmitted is a well-formed response that carries no
	// mcpCapabilities block at all — the only state the exception accepts.
	acpMcpCapabilitiesOmitted
	// acpMcpCapabilitiesDeclared is a usable block, whose HTTP/SSE fields
	// are authoritative (including when both are false).
	acpMcpCapabilitiesDeclared
)

type acpMcpTransportCapabilities struct {
	Declaration acpMcpCapabilityDeclaration
	HTTP        bool
	SSE         bool
}

// extractACPMcpCapabilities reads `agentCapabilities.mcpCapabilities.http`
// and `.sse` out of an ACP `initialize` response.
//
// Per ACP v1 capability negotiation, "Clients and Agents MUST treat all
// capabilities omitted in the initialize request as UNSUPPORTED", and
// `http` / `sse` have no default beyond false. Every state therefore
// resolves to "neither transport supported"; the classification only
// decides whether the omitted-capabilities exception may apply.
//
// Each level is decoded as raw JSON rather than straight into the target
// struct, because encoding/json leaves a non-pointer destination untouched
// and reports no error when it decodes `null`. A single Unmarshal would
// therefore read `null`, `{"agentCapabilities":null}` and a genuinely
// silent response as the same thing, letting an unreadable response take
// the exception. ACP's InitializeResponse is an object and
// `agentCapabilities` is not nullable, so those two are malformed, not
// silent.
//
// See https://agentclientprotocol.com/protocol/v1/initialization#capabilities
// and https://pkg.go.dev/encoding/json#Unmarshal for the null rule.
func extractACPMcpCapabilities(result json.RawMessage) acpMcpTransportCapabilities {
	invalid := acpMcpTransportCapabilities{Declaration: acpMcpCapabilitiesInvalid}
	omitted := acpMcpTransportCapabilities{Declaration: acpMcpCapabilitiesOmitted}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(result, &top); err != nil || top == nil {
		return invalid
	}
	rawAgentCaps, ok := top["agentCapabilities"]
	if !ok {
		// A well-formed response that declares no capabilities at all.
		return omitted
	}
	var agentCaps map[string]json.RawMessage
	if err := json.Unmarshal(rawAgentCaps, &agentCaps); err != nil || agentCaps == nil {
		return invalid
	}
	rawMcp, ok := agentCaps["mcpCapabilities"]
	if !ok {
		// The real hermes 0.18.2 shape: capabilities declared, this block
		// genuinely absent. Only this state can reach the exception.
		return omitted
	}
	rawMcp = bytes.TrimSpace(rawMcp)
	if bytes.Equal(rawMcp, []byte("null")) {
		return invalid
	}
	var caps struct {
		HTTP bool `json:"http"`
		SSE  bool `json:"sse"`
	}
	// A malformed block (wrong field types, non-object) is unusable. An
	// unusable declaration is not silence, so it must not reach the
	// omitted-capabilities exception — it fails closed instead.
	if err := json.Unmarshal(rawMcp, &caps); err != nil {
		return invalid
	}
	return acpMcpTransportCapabilities{
		Declaration: acpMcpCapabilitiesDeclared,
		HTTP:        caps.HTTP,
		SSE:         caps.SSE,
	}
}

// acpRuntimesToleratingOmittedMcpCapabilities lists the ACP providers whose
// own shipped binary was verified to accept http/sse McpServer entries on
// session/new even though its initialize response carries no
// mcpCapabilities block.
//
// ACP v1 requires an omitted capability to be read as unsupported, so this
// is a deliberate, narrow exception to the spec default rather than a
// replacement for it. hermes 0.18.2 declares no mcpCapabilities yet accepts
// both remote transports, so applying the default there silently discarded
// every remote MCP server its users configured, with the drop visible only
// in the daemon log (#6540).
//
// Only add a provider here with evidence from its real binary — an ACP
// implementation that omits capabilities *and* rejects remote entries would
// turn a missing tool into a failed session, so the fail-closed default has
// to stay the rule for everything unverified.
var acpRuntimesToleratingOmittedMcpCapabilities = map[string]bool{
	"hermes": true,
}

// acpToleratesOmittedMcpCapabilities reports whether this launch may fall
// back to the omitted-capabilities exception.
//
// The provider name alone is not enough to answer that. A custom runtime
// profile keeps its protocol family as the provider and only swaps in its
// own command, so `protocol_family: hermes` with `command_name: jcode`
// reaches this backend as "hermes" while being a binary nobody verified —
// exactly the unverified implementation the allowlist exists to exclude.
// Config.BuiltinRuntime is the daemon's report of which case this is, and
// it defaults to false so any caller that doesn't set it fails closed.
func acpToleratesOmittedMcpCapabilities(backend string, cfg Config) bool {
	return cfg.BuiltinRuntime && acpRuntimesToleratingOmittedMcpCapabilities[backend]
}

// filterACPMcpServersByCapability drops remote MCP entries whose transport
// the runtime did not advertise in its initialize response. Stdio entries
// (no `type` field) always pass through.
//
// Sending an http/sse entry to a runtime that doesn't support it is a
// protocol violation per the ACP spec and can reject the whole session/new
// request with a JSON-RPC error. Dropping the offending entries lets the
// rest of the session start instead of tanking every task on that agent.
//
// The single exception is a launch that acpToleratesOmittedMcpCapabilities
// accepts whose response declared no capabilities at all; see there and
// acpRuntimesToleratingOmittedMcpCapabilities for why that case is carved
// out and why it stays narrow. A response we could not read is not silence
// and never qualifies.
func filterACPMcpServersByCapability(
	servers []any,
	caps acpMcpTransportCapabilities,
	backend string,
	cfg Config,
) []any {
	logger := cfg.Logger
	if len(servers) == 0 {
		return servers
	}
	if caps.Declaration == acpMcpCapabilitiesOmitted && acpToleratesOmittedMcpCapabilities(backend, cfg) {
		if logger != nil && acpHasRemoteMcpEntry(servers) {
			logger.Info("runtime declared no mcpCapabilities; forwarding remote MCP servers under this runtime's verified exception",
				"backend", backend)
		}
		return servers
	}
	filtered := make([]any, 0, len(servers))
	for _, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			filtered = append(filtered, raw)
			continue
		}
		transport, _ := entry["type"].(string)
		switch transport {
		case "http":
			if !caps.HTTP {
				if logger != nil {
					logger.Warn("dropping http MCP server: runtime did not advertise mcpCapabilities.http",
						"backend", backend, "name", entry["name"])
				}
				continue
			}
		case "sse":
			if !caps.SSE {
				if logger != nil {
					logger.Warn("dropping sse MCP server: runtime did not advertise mcpCapabilities.sse",
						"backend", backend, "name", entry["name"])
				}
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// acpHasRemoteMcpEntry reports whether any entry uses a remote transport,
// i.e. whether the capability question is relevant at all for this config.
func acpHasRemoteMcpEntry(servers []any) bool {
	for _, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if transport, _ := entry["type"].(string); transport == "http" || transport == "sse" {
			return true
		}
	}
	return false
}

// hermesToolNameFromTitle extracts a tool name from the ACP tool call title.
// Hermes ACP titles look like "terminal: ls -la", "read: /path/to/file", etc.
// Some titles have no colon (e.g. "execute code").
func hermesToolNameFromTitle(title string, kind string) string {
	// Check exact-match titles first (no colon).
	switch title {
	case "execute code":
		return "execute_code"
	}

	// Try to extract the tool name from before the first colon.
	if idx := strings.Index(title, ":"); idx > 0 {
		name := strings.TrimSpace(title[:idx])
		// Map common ACP title prefixes back to tool names.
		// Some titles include mode info like "patch (replace)", so check prefix.
		switch {
		case name == "terminal":
			return "terminal"
		case name == "read":
			return "read_file"
		case name == "write":
			return "write_file"
		case strings.HasPrefix(name, "patch"):
			return "patch"
		case name == "search":
			return "search_files"
		case name == "web search":
			return "web_search"
		case name == "extract":
			return "web_extract"
		case name == "delegate":
			return "delegate_task"
		case name == "analyze image":
			return "vision_analyze"
		}
		return name
	}

	// Fall back to kind.
	switch kind {
	case "read":
		return "read_file"
	case "edit":
		return "write_file"
	case "execute":
		return "terminal"
	case "search":
		return "search_files"
	case "fetch":
		return "web_search"
	case "think":
		return "thinking"
	default:
		// Preserve a non-empty title when we can't classify it: kimi
		// emits bare titles like "Shell" or "Read file" without any
		// `kind`, so returning an empty string here drops the tool
		// name entirely before kimiToolNameFromTitle can map it.
		// Hermes titles always carry a colon, so hermes never reaches
		// this branch with a non-empty title.
		if title != "" {
			return title
		}
		return kind
	}
}

// ── Provider-error sniffing ──
//
// ACP agents (hermes, kimi, …) all have the same failure mode:
// session/prompt reports stopReason=end_turn even when the underlying
// HTTP call to the configured LLM endpoint returned an error — the
// actionable detail only appears on stderr (e.g.
// `⚠️ API call failed (attempt 1/3): BadRequestError [HTTP 400]` and
// `Error: HTTP 400: Error code: 400 - {'detail': "The '...' model
// is not supported when using Codex with a ChatGPT account."}`).
// The sniffer scans for those patterns so the daemon can surface a
// real failure instead of a generic "empty output".
//
// Parameterised by provider name so both hermes and kimi can share
// the transport: the regexes match format-level signals (HTTP status,
// error-kind tags, "API call failed" banner) that both runtimes emit.
//
// The sniffer distinguishes *transient* per-attempt warnings (e.g.
// "API call failed (attempt 1/3): RateLimitError [HTTP 429]" — followed
// by a successful retry) from *terminal* exhausted failures (e.g.
// "API call failed after 3 retries: ..." or "❌ ... Non-retryable"):
// `message()` returns whichever was last seen, while `terminalMessage()`
// returns non-empty only when a terminal-failure marker was matched.
// Promotion to status="failed" must use `terminalMessage()`, otherwise
// a successful retry following an early per-attempt warning would be
// wrongly marked as failed.
type acpProviderErrorSniffer struct {
	provider string
	mu       sync.Mutex
	remains  []byte   // buffer for a partial trailing line across writes
	lines    []string // captured error lines, bounded
	seen     map[string]bool
	terminal bool // sticky: at least one line matched acpTerminalErrorRe
	// echoJSON tracks an incomplete structured payload from a Python
	// INFO/DEBUG root-logger record. The JSON scanner state is persisted
	// across Write calls so only actual payload continuations are skipped.
	echoJSON     bool
	echoDepth    int
	echoInString bool
	echoEscaped  bool
}

// acpErrorHeaderRe matches the first line of an API-error block.
// ACP agents typically prefix these with ⚠️ / ❌ and include an HTTP
// status code or a non-retryable-error tag.
var acpErrorHeaderRe = regexp.MustCompile(`(?:⚠️|❌|\[ERROR\]).*(?:BadRequestError|AuthenticationError|RateLimitError|HTTP [0-9]{3}|Non-retryable|API call failed)`)

// acpErrorDetailRe pulls the most useful single-line messages out of
// the subsequent lines of the error block (the one whose "Error:" or
// "Details:" tag actually spells out what happened).
var acpErrorDetailRe = regexp.MustCompile(`(?:Error:|detail:|Details:)\s*(.+)`)

// acpTerminalErrorRe matches markers that only appear when the
// adapter has *given up* on the upstream call — either after
// exhausting retries ("after N retries"), or because the error is
// classified as non-retryable up front (Non-retryable, BadRequest /
// Authentication errors, ❌ / [ERROR] log levels). Per-attempt
// warnings ("(attempt 1/3)") deliberately do NOT match this pattern.
var acpTerminalErrorRe = regexp.MustCompile(`(?:❌|\[ERROR\]|after \d+ retr|Non-retryable|BadRequestError|AuthenticationError)`)

// acpAgentOutputTerminalRe matches the synthetic agent-text turn that
// hermes-style ACP adapters inject when they exhaust retries against
// the upstream LLM ("API call failed after 3 retries: HTTP 429..."),
// surfaced via session/update agent_message_chunk and ending up in the
// final output buffer. Per-attempt warnings (which only go to stderr
// and use "(attempt N/M)" phrasing) won't match.
var acpAgentOutputTerminalRe = regexp.MustCompile(`API call failed after \d+ retr(?:y|ies)`)

const acpMaxErrorLines = 8

// acpLogRecordPrefixRe matches the start of a Python `logging` record that
// Hermes writes to stderr: "YYYY-MM-DD HH:MM:SS[,mmm] [LEVEL] logger: ...".
// Capture groups are level, logger, and payload. Matching every logger is
// important because a non-root ERROR record also ends a preceding root INFO
// record and must remain visible to the provider-error matchers.
var acpLogRecordPrefixRe = regexp.MustCompile(
	`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?:,[0-9]+)? \[([A-Z]+)\] ([A-Za-z0-9_.-]+):\s*(.*)$`,
)

// acpEchoLogLevels are the Python root-logger levels Hermes uses purely to
// echo conversation and tool data to stderr. Those records are arbitrary
// payload, never a provider error, even when they embed strings such as
// "❌", "Error:", "HTTP 429", or "API call failed". ERROR / WARNING /
// CRITICAL records are real diagnostics and must still be matched.
var acpEchoLogLevels = map[string]bool{"INFO": true, "DEBUG": true}

// acpMaxErrorLineLen bounds the length of the persisted provider-error
// summary. Genuine ACP provider-error lines are short (an error header plus
// a one-line "Error: ..." detail — see TestHermesProviderErrorSniffer, ~150
// bytes), but an oversized echo that slips past the log-record filter could
// otherwise store tens of KB. This bound is applied only when BUILDING the
// stored message, never as a precondition for *classifying* a line as an
// error: a real provider failure whose single line happens to exceed this
// length must still fail the run, not be silently dropped (GitHub
// multica#5862 — do not gate matching on length).
const acpMaxErrorLineLen = 4096

// newACPProviderErrorSniffer returns a sniffer that tags its messages
// with the given provider name (e.g. "hermes", "kimi") so failure
// strings make it obvious which runtime produced the error.
func newACPProviderErrorSniffer(provider string) *acpProviderErrorSniffer {
	return &acpProviderErrorSniffer{provider: provider, seen: map[string]bool{}}
}

// Write implements io.Writer so the sniffer can sit behind an
// io.MultiWriter next to the normal stderr log forwarder.
func (s *acpProviderErrorSniffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := append(s.remains, p...)
	// Keep the final partial line (no trailing newline) for the
	// next write so multi-line error blocks aren't split.
	nl := strings.LastIndexByte(string(data), '\n')
	var complete string
	if nl < 0 {
		s.remains = append(s.remains[:0], data...)
		return len(p), nil
	}
	complete = string(data[:nl])
	s.remains = append(s.remains[:0], data[nl+1:]...)

	for _, rawLine := range strings.Split(complete, "\n") {
		rawLine = strings.TrimSuffix(rawLine, "\r")
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		// INFO/DEBUG root records are conversation/tool echoes and never
		// reach the error matchers. When their payload is structured JSON,
		// track brace depth across physical lines and skip only until that
		// JSON closes. A complete single-line echo therefore cannot hide a
		// following bare provider error. Any Python logger prefix also
		// starts a new record, so non-root ERROR diagnostics remain visible
		// (GitHub multica#5862).
		if m := acpLogRecordPrefixRe.FindStringSubmatch(line); m != nil {
			s.resetEchoJSON()
			if acpEchoLogLevels[m[1]] && m[2] == "root" {
				s.startEchoJSON(m[3])
				continue
			}
		} else if s.echoJSON {
			// A strong, unindented provider-error header is a new bare
			// error block even if a malformed echo never closed. Indented
			// error-looking text remains part of the structured echo. This
			// also applies when the echo was truncated inside a JSON string:
			// an unescaped physical newline is not valid inside that string.
			indented := rawLine[0] == ' ' || rawLine[0] == '\t'
			bareError := strings.HasPrefix(line, "⚠️") ||
				strings.HasPrefix(line, "❌") ||
				strings.HasPrefix(line, "[ERROR]")
			if !indented && bareError && acpErrorHeaderRe.MatchString(line) {
				s.resetEchoJSON()
			} else {
				s.consumeEchoJSON(rawLine)
				continue
			}
		}
		if !(acpErrorHeaderRe.MatchString(line) || acpErrorDetailRe.MatchString(line)) {
			continue
		}
		if acpTerminalErrorRe.MatchString(line) {
			s.terminal = true
		}
		if s.seen[line] {
			continue
		}
		s.seen[line] = true
		s.lines = append(s.lines, line)
		if len(s.lines) > acpMaxErrorLines {
			s.lines = s.lines[len(s.lines)-acpMaxErrorLines:]
		}
	}
	return len(p), nil
}

func (s *acpProviderErrorSniffer) startEchoJSON(payload string) {
	payload = strings.TrimSpace(payload)
	if payload == "" || (payload[0] != '{' && payload[0] != '[') {
		return
	}
	s.echoJSON = true
	s.consumeEchoJSON(payload)
}

// consumeEchoJSON tracks only structural JSON bytes. It avoids buffering
// arbitrary conversation/tool payloads while still recognizing their exact
// multi-line boundary, including braces inside quoted strings.
func (s *acpProviderErrorSniffer) consumeEchoJSON(fragment string) {
	for i := 0; i < len(fragment); i++ {
		switch {
		case s.echoEscaped:
			s.echoEscaped = false
		case s.echoInString:
			switch fragment[i] {
			case '\\':
				s.echoEscaped = true
			case '"':
				s.echoInString = false
			}
		default:
			switch fragment[i] {
			case '"':
				s.echoInString = true
			case '{', '[':
				s.echoDepth++
			case '}', ']':
				if s.echoDepth > 0 {
					s.echoDepth--
				}
			}
		}
	}
	if s.echoDepth == 0 {
		s.resetEchoJSON()
	}
}

func (s *acpProviderErrorSniffer) resetEchoJSON() {
	s.echoJSON = false
	s.echoDepth = 0
	s.echoInString = false
	s.echoEscaped = false
}

// message returns a single-line summary suitable for the task
// error field. Prefers the most specific "Error:" / "detail:"
// fragment; falls back to the first captured header line; empty
// when nothing useful was seen.
//
// NOTE: a non-empty message() can describe a *transient* per-attempt
// warning that was followed by a successful retry. Code that flips
// task status to "failed" must instead use terminalMessage() — see
// the type doc above.
func (s *acpProviderErrorSniffer) message() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.messageLocked()
}

// terminalMessage returns the same single-line summary as message()
// but only when the sniffer has seen at least one line matching
// acpTerminalErrorRe — i.e. the adapter has given up retrying. This
// is the signal callers should use to decide whether to promote a
// run from "completed" to "failed". Returns empty if all captured
// lines look like transient retry warnings.
func (s *acpProviderErrorSniffer) terminalMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.terminal {
		return ""
	}
	return s.messageLocked()
}

// messageLocked is the lock-held implementation shared by message()
// and terminalMessage(). Caller must hold s.mu.
func (s *acpProviderErrorSniffer) messageLocked() string {
	prefix := s.provider + " provider error: "
	for _, line := range s.lines {
		if m := acpErrorDetailRe.FindStringSubmatch(line); m != nil {
			detail := strings.TrimSpace(m[1])
			if detail != "" {
				return acpTruncateError(prefix + detail)
			}
		}
	}
	for _, line := range s.lines {
		if acpErrorHeaderRe.MatchString(line) {
			return acpTruncateError(prefix + line)
		}
	}
	return ""
}

// acpTruncateError bounds a persisted provider-error summary to
// acpMaxErrorLineLen bytes on a valid UTF-8 boundary, marking any cut.
// Classification already happened by the time this runs; it only limits
// how much of an oversized message is stored, never whether a run fails.
func acpTruncateError(msg string) string {
	if len(msg) <= acpMaxErrorLineLen {
		return msg
	}
	return strings.ToValidUTF8(msg[:acpMaxErrorLineLen], "") + "…(truncated)"
}

// promoteACPResultOnProviderError flips finalStatus to "failed" if
// either (a) the stderr sniffer captured a terminal-failure marker,
// (b) the adapter injected a synthetic "API call failed after N
// retries..." turn into the agent text stream, or (c) output was
// empty AND the sniffer captured anything at all (no real result to
// fall back on, even from a transient-only sequence). Returns the
// updated (status, error) pair; callers should overwrite their
// locals with the result.
//
// This is the shared post-processing step for hermes/kimi/kiro.
// Without it, runs that exhaust retries against the upstream LLM
// (HTTP 429, expired token, …) silently report as "completed"
// because session/prompt still ends with stopReason=end_turn — see
// GitHub multica#1952.
func promoteACPResultOnProviderError(finalStatus, finalError, finalOutput string, sniffer *acpProviderErrorSniffer) (string, string) {
	if finalStatus != "completed" {
		return finalStatus, finalError
	}
	if msg := sniffer.terminalMessage(); msg != "" {
		return "failed", msg
	}
	if acpAgentOutputTerminalRe.MatchString(finalOutput) {
		msg := sniffer.message()
		if msg == "" {
			msg = sniffer.provider + " provider error: " + acpAgentOutputTerminalRe.FindString(finalOutput)
		}
		return "failed", msg
	}
	if finalOutput == "" {
		if msg := sniffer.message(); msg != "" {
			return "failed", msg
		}
	}
	return finalStatus, finalError
}
