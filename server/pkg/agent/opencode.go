package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// opencodeTerminateGraceNanos optionally overrides, in nanoseconds, how long a
// cancelled opencode process is given to exit after SIGTERM before it (and its
// whole process group) is SIGKILLed. Zero means use the default. It is atomic
// so tests can shorten the grace without racing the cancellation goroutine that
// reads it. See the cancellation handler in Execute for why termination must
// precede closing the stdout pipe (#4533).
var opencodeTerminateGraceNanos atomic.Int64

func opencodeTerminateGrace() time.Duration {
	if n := opencodeTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

// opencodeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var opencodeBlockedArgs = map[string]blockedArgMode{
	"--format":                       blockedWithValue,  // json output format for daemon communication
	"--dir":                          blockedWithValue,  // task workdir anchor for skill / AGENTS.md discovery
	"--variant":                      blockedWithValue,  // owned by agent.thinking_level
	"--dangerously-skip-permissions": blockedStandalone, // daemon manages non-interactive permission prompts
}

// opencodeBackend implements Backend by spawning `opencode run --format json`
// and reading streaming JSON events from stdout — the same pattern as Claude.
type opencodeBackend struct {
	cfg Config
}

func (b *opencodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "opencode"
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("opencode executable not found at %q: %w", execPath, err)
	}
	if runtime.GOOS == "windows" {
		if native := resolveOpenCodeNativeFromShim(resolved, os.Stat); native != "" {
			b.cfg.Logger.Info("opencode resolved to native binary to avoid .cmd shim argv truncation", "shim", resolved, "native", native)
			resolved = native
		}
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := []string{"run", "--format", "json", "--dangerously-skip-permissions"}
	// Anchor OpenCode's project discovery (AGENTS.md walk-up + .opencode/skills/
	// project config scan) at the task workdir. Without this, OpenCode falls
	// back to PWD (inherited from the daemon process) or process.cwd(), which
	// in self-host deployments can resolve to the user's shell working
	// directory and silently bypass the per-task workdir — agents lose
	// visibility into their assigned skills and AGENTS.md instructions.
	// PWD is also overridden below because OpenCode prefers PWD over cwd when
	// `--dir` is absent and uses it as the starting point for any further
	// path resolution.
	if opts.Cwd != "" {
		args = append(args, "--dir", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--variant", opts.ThinkingLevel)
	}
	// OpenCode's `run` subcommand has no --prompt flag — passing one makes the
	// CLI exit 1 with a usage dump before sending anything (checked against
	// OpenCode 1.17.7). SystemPrompt is therefore never forwarded; the runtime
	// brief reaches the agent through the per-task AGENTS.md the daemon writes
	// into the workdir, which OpenCode loads itself (MUL-5392). Same constraint
	// as the DevEco backend, which was forked from this one.
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, opencodeBlockedArgs, b.cfg.Logger)...)
	// The task prompt is delivered on stdin, never argv — see the StdinPipe
	// wiring below. `opencode run` merges its variadic [message..] positional
	// with whatever is piped in, so an invocation that passes no positional
	// makes the piped text the entire run message. Inlining it instead fails
	// hard on Windows: CreateProcess caps lpCommandLine at 32,767 characters
	// (8,191 when a .cmd shim routes the call through cmd.exe), and a prompt
	// carrying the workspace's models and skills clears that on its own — the
	// process then never starts and Go surfaces the misleading "The filename or
	// extension is too long" (#6538). Keeping the prompt off argv also stops it
	// from being echoed into the "agent command" log line below.

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	// Run opencode in its own process group so cancellation can reach the
	// whole tree (opencode plus any tool subprocess it spawns), not just the
	// direct child — otherwise a cancelled or restarted run can orphan a
	// descendant that keeps spinning (#4533).
	configureProcessGroup(cmd)
	// Take over context cancellation. The default CommandContext behaviour
	// SIGKILLs only the leader the instant runCtx is done; we instead drive a
	// graceful, group-wide SIGTERM→SIGKILL from the cancellation goroutine
	// below and close the stdout read end only after the tree has been
	// signalled. Returning nil here keeps os/exec from racing us with its own
	// kill; WaitDelay remains the hard backstop.
	cmd.Cancel = func() error { return nil }
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args, "prompt_bytes", len(prompt))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	// Keep daemon-mode runs non-interactive without relying on
	// OPENCODE_PERMISSION. OpenCode deep-merges that env override into user
	// config while preserving existing key order, so a pre-existing
	// permission.question key can be followed by a wildcard allow and bypass
	// the intended question deny. Current OpenCode run sessions inject their
	// own question/plan deny rules after agent config; this flag only answers
	// prompts that survive those explicit denies.
	// Override PWD so the child OpenCode process resolves its discovery root
	// to the task workdir. cmd.Dir alone is not enough: OpenCode reads PWD
	// (inherited from the parent daemon) before falling back to process.cwd()
	// when computing the directory it walks for AGENTS.md / .opencode/skills.
	// See packages/opencode/src/cli/cmd/run.ts in the upstream source.
	if opts.Cwd != "" {
		env = append(env, "PWD="+opts.Cwd)
	}
	// Project agent.mcp_config into OpenCode via OPENCODE_CONFIG_CONTENT —
	// OpenCode's general inline-config injection mechanism that merges at
	// "local" scope (after the project-config loop, before remote / managed
	// configs). MCP is the only field we currently project there; if a
	// future Multica field needs the same channel it would assemble a
	// combined OpenCode config slice before the env append.
	//
	// This deliberately leaves <workdir>/opencode.json untouched — the
	// workdir is reused across turns for the same (agent, issue), and any
	// agent- or user-written model / tools / permission settings in it must
	// survive across runs.
	mcpContent, err := buildOpenCodeMCPConfigContent(opts.McpConfig)
	if err != nil {
		cancel()
		return nil, err
	}
	if mcpContent != "" {
		if _, dup := b.cfg.Env["OPENCODE_CONFIG_CONTENT"]; dup {
			b.cfg.Logger.Warn("agent.custom_env sets OPENCODE_CONFIG_CONTENT but agent.mcp_config takes precedence and overrides it")
		}
		env = append(env, "OPENCODE_CONFIG_CONTENT="+mcpContent)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[opencode:stderr] ")

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	b.cfg.Logger.Info("opencode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead pid.
	procDone := make(chan struct{})

	// Write the prompt from its own goroutine so it cannot deadlock against the
	// stdout reader below: a prompt larger than the OS pipe buffer (~64 KiB)
	// blocks mid-write until OpenCode drains it, and OpenCode cannot drain while
	// nobody is consuming its stdout. Closing stdin is what ends the prompt —
	// OpenCode reads it to EOF (`await Bun.stdin.text()`), so a stdin left open
	// hangs the run forever. Close on every path, success or error.
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	// On cancellation / timeout, terminate opencode (and the tool subprocesses
	// it spawned) BEFORE unblocking the scanner. The previous implementation
	// closed the stdout read end immediately, which left opencode writing into
	// a closed pipe: every write returns EPIPE and, per anomalyco/opencode#33653,
	// can spin the orphaned process at 100% CPU. Instead we SIGTERM the whole
	// process group, give it a grace period to exit cleanly, then SIGKILL it.
	// SIGKILL is uncatchable, so once it is delivered no group member can run
	// (or write) again — only then is it safe to close the stdout read end as a
	// last-resort unblock for a scanner that a wedged descendant still keeps
	// open. WaitDelay is the final backstop (#4533).
	go func() {
		select {
		case <-procDone:
			return // finished on its own; nothing to terminate
		case <-runCtx.Done():
		}
		// Release a prompt write still blocked on a full stdin pipe — an
		// OpenCode that stopped reading before draining it would otherwise
		// strand that goroutine for the lifetime of the daemon.
		closeStdin()
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGTERM)
			select {
			case <-procDone: // exited within the grace window
			case <-time.After(opencodeTerminateGrace()):
				signalProcessGroup(cmd, syscall.SIGKILL)
			}
		}
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)

		// Wait for process exit, then release the cancellation handler.
		exitErr := cmd.Wait()
		close(procDone)
		duration := time.Since(startTime)

		// Wait closes the process pipes, so a prompt write still blocked when
		// OpenCode exited has returned by now. The writer sends exactly once.
		writeErr := <-writeErrCh

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("opencode timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("opencode exited with error: %v", exitErr)
		} else if exitErr != nil && scanResult.noTerminalSignal {
			// Status is already "failed" from the terminal-signal guard; append
			// the process exit detail so a mid-step crash still surfaces the
			// signal / exit code that killed it.
			scanResult.errMsg = fmt.Sprintf("%s; opencode exited with error: %v", scanResult.errMsg, exitErr)
		} else if writeErr != nil && !scanResult.sawTerminalSignal {
			// A failed prompt write is only benign once the run is PROVEN to have
			// finished: OpenCode reads stdin to EOF before it does any work, so a
			// run that reached a terminal signal necessarily received the whole
			// prompt, and an EPIPE recorded after that just means the pipe closed
			// on its way out — failing on it would discard a successful result.
			//
			// Absence of failure is not that proof. status starts at "completed"
			// and processEvents only fails closed on structural evidence, so a
			// child that emits nothing and exits 0 still reports "completed". If
			// the prompt never landed, that is precisely the run we must not pass
			// off as a clean success, so key on sawTerminalSignal instead.
			// Append rather than overwrite so the stream's own diagnosis survives.
			if scanResult.errMsg == "" {
				scanResult.errMsg = fmt.Sprintf("opencode prompt write failed: %v", writeErr)
			} else {
				scanResult.errMsg = fmt.Sprintf("%s; opencode prompt write failed: %v", scanResult.errMsg, writeErr)
			}
			scanResult.status = "failed"
		}

		b.cfg.Logger.Info("opencode finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. OpenCode doesn't report model per-step, so we
		// attribute all usage to the configured model (or "unknown").
		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// ── Event handlers ──

// eventResult holds the accumulated state from processing the event stream.
type eventResult struct {
	status           string
	errMsg           string
	output           string
	sessionID        string
	usage            TokenUsage // accumulated token usage across all steps
	noTerminalSignal bool       // guard fired: the stream ended without evidence the run actually finished
	// sawTerminalSignal is positive evidence that the run actually finished: a
	// step_finish closed the last step with no continuation pending and with
	// something to show for it. It is NOT the negation of noTerminalSignal — a
	// stream with no events at all sets neither, because there is nothing to
	// fail closed on and nothing that proves completion either. Callers that
	// need "this run really completed" must test this field; status defaults to
	// "completed" and cannot carry that meaning on its own.
	sawTerminalSignal bool
}

// processEvents reads JSON lines from r, dispatches events to ch, and returns
// the accumulated result. This is the core scanner loop, extracted for testability.
func (b *opencodeBackend) processEvents(r io.Reader, ch chan<- Message) eventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	// Track step bracketing so a stream that ends mid-step is not mistaken for a
	// clean completion. OpenCode's JSON stream has no terminal result event
	// (unlike Claude's type:"result"), so "no error seen" is not proof the run
	// finished. opencode emits tool_use only on terminal states (completed or
	// error), so a dangling tool call implies an unclosed step — step bracketing
	// is the positive terminal signal. Recovered tool errors (state.status ==
	// "error") are normal in healthy runs and must not affect status.
	//
	// Step bracketing alone is not enough: step_finish carries a reason
	// (FinishReason: "stop", "tool-calls", …), and a run that still has tool
	// results to feed back normally closes its step with reason "tool-calls"
	// before the next step_start. Some providers return "stop" despite emitting
	// tool calls, though, and OpenCode deliberately continues those runs when a
	// non-provider-executed tool result must be fed back to the model. Track both
	// signals so EOF in either continuation gap fails closed. A missing reason
	// retains the older step-bracketing behavior for protocol compatibility.
	openStep := false                // between a step_start and its step_finish
	stepHasContinuationTool := false // current step has a local tool result OpenCode must feed back
	awaitingContinuation := false    // the last step_finish still required another step
	sawStepFinish := false           // at least one step closed; see eventResult.sawTerminalSignal

	// Step bracketing still misses a third shape: a step that opens and closes
	// cleanly while carrying nothing at all — no text, no tool call, and no
	// reported usage whatsoever (#6522, observed as step_finish reason "unknown"
	// with every token counter and the cost at 0). No usage means the provider
	// round-trip never happened, so that step is a dead stream wearing a clean
	// finish, and ending a run on one is another false-green completion.
	//
	// The criterion is deliberately "this step produced nothing", NOT "the run
	// produced no text": a task whose only deliverable is a tool side effect is
	// legitimate and must stay green. Any single sign of life — text, a tool
	// call, or any usage field the protocol reports — keeps the step productive.
	// This is also why the reason itself is not consulted: a missing or
	// unrecognised reason stays terminal for protocol compatibility (see the
	// back-compat regression), and voidness is orthogonal to it.
	stepProducedOutput := false // current step emitted text, a tool call, or reported usage
	lastStepVoid := false       // the most recently closed step produced nothing at all

	scanner := newAgentStreamScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event opencodeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.SessionID != "" {
			sessionID = event.SessionID
		}

		switch event.Type {
		case "text":
			b.handleTextEvent(event, ch, &output)
			if event.Part.Text != "" {
				stepProducedOutput = true
			}
		case "tool_use":
			b.handleToolUseEvent(event, ch)
			stepProducedOutput = true
			if event.Part.Metadata == nil || !event.Part.Metadata.ProviderExecuted {
				stepHasContinuationTool = true
			}
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			openStep = true
			stepHasContinuationTool = false
			awaitingContinuation = false
			stepProducedOutput = false
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			openStep = false
			sawStepFinish = true
			awaitingContinuation = event.Part.Reason == "tool-calls" ||
				(event.Part.Reason != "" && stepHasContinuationTool)
			stepHasContinuationTool = false
			// Accumulate token usage from step_finish events. Only the fields
			// TokenUsage models are billed; every reported field additionally
			// counts as proof the provider round-trip happened, which is what
			// keeps a productive step out of the void-step guard below.
			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
			if stepReportedUsage(&event.Part) {
				stepProducedOutput = true
			}
			lastStepVoid = !stepProducedOutput
		}
	}

	// Check for scanner errors (e.g. broken pipe, read errors).
	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("opencode stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	// Require a positive terminal signal. A clean EOF while a step is still
	// open — right after a step that finished with reason "tool-calls", whose
	// continuation step never started — or on a step that carried nothing at
	// all means the run did not finish: its provider stream died and
	// `opencode run` exited without emitting an error event. Fail closed on
	// that structural evidence rather than reporting a false-green completion.
	noTerminalSignal := false
	if finalStatus == "completed" {
		switch {
		case openStep:
			finalStatus = "failed"
			finalError = "opencode stream ended without a terminal signal (step still open at EOF)"
			noTerminalSignal = true
		case awaitingContinuation:
			finalStatus = "failed"
			finalError = "opencode stream ended without a terminal signal (last step required a continuation that never started)"
			noTerminalSignal = true
		case lastStepVoid:
			finalStatus = "failed"
			finalError = "opencode stream ended on an empty step (no text, no tool call, no reported usage) — the provider produced nothing"
			noTerminalSignal = true
		}
	}

	return eventResult{
		status:            finalStatus,
		errMsg:            finalError,
		output:            output.String(),
		sessionID:         sessionID,
		usage:             usage,
		noTerminalSignal:  noTerminalSignal,
		sawTerminalSignal: sawStepFinish && !noTerminalSignal,
	}
}

// stepReportedUsage reports whether a step_finish part carries any evidence
// that the provider round-trip actually happened.
//
// OpenCode's protocol keeps reasoning and the aggregate total in fields of
// their own alongside input/output/cache, and reports cost as a sibling of the
// whole token block — a step can legitimately land with reasoning or cost
// positive while input and output are both zero. Checking only input/output
// would therefore call such a step void and fail a healthy run, so every field
// the protocol reports counts. Only an across-the-board zero means no model
// call happened.
//
// The reasoning and total counters are read as evidence only, deliberately not
// folded into TokenUsage: total is derived (adding it would double-count) and
// TokenUsage has no reasoning bucket, so recording either here would change
// billing figures rather than fix this bug.
func stepReportedUsage(part *opencodeEventPart) bool {
	if part.Cost > 0 {
		return true
	}
	t := part.Tokens
	if t == nil {
		return false
	}
	if t.Input > 0 || t.Output > 0 || t.Reasoning > 0 || t.Total > 0 {
		return true
	}
	return t.Cache != nil && (t.Cache.Read > 0 || t.Cache.Write > 0)
}

func (b *opencodeBackend) handleTextEvent(event opencodeEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

// handleToolUseEvent processes "tool_use" events from opencode. A single
// tool_use event contains both the call and result in part.state when the
// tool reaches a terminal state (state.status is "completed" or "error").
func (b *opencodeBackend) handleToolUseEvent(event opencodeEvent, ch chan<- Message) {
	// Extract input from state.input (the tool invocation parameters).
	var input map[string]any
	if event.Part.State != nil && event.Part.State.Input != nil {
		_ = json.Unmarshal(event.Part.State.Input, &input)
	}

	// Emit the tool-use message.
	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   event.Part.Tool,
		CallID: event.Part.CallID,
		Input:  input,
	})

	// Pair every terminal tool-use with a tool-result. The daemon uses this
	// pair to track in-flight tools, so dropping error results would leave its
	// counter permanently elevated and suppress the normal idle watchdog.
	state := event.Part.State
	if state != nil && (state.Status == "completed" || state.Status == "error") {
		outputStr := extractToolOutput(state.Output)
		if state.Status == "error" && state.Error != "" {
			outputStr = state.Error
		}
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   event.Part.Tool,
			CallID: event.Part.CallID,
			Output: outputStr,
		})
	}
}

// handleErrorEvent processes "error" events from opencode. OpenCode can exit
// with RC=0 even on errors (e.g. invalid model), so error events are the
// reliable signal for failures.
func (b *opencodeBackend) handleErrorEvent(event opencodeEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown opencode error"
	}

	b.cfg.Logger.Warn("opencode error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

// resolveOpenCodeNativeFromShim returns the path to the native OpenCode
// executable bundled inside the npm package, given the path to the npm
// `opencode.cmd` shim that PATH lookup found on Windows. Returns "" if shim
// doesn't end in `.cmd` or no candidate npm platform package has a bundled
// native binary present.
//
// Windows batch argument forwarding via `%*` does not preserve newlines, so
// multi-line positional argv is truncated at the first newline before the
// shim hands off to the JS entrypoint. Daemon prompts can include literal
// newlines (system prompt + user message), which makes the agent see only
// the first line. Native binary spawn skips the cmd.exe layer entirely.
//
// Layout when installed via `npm install -g opencode-ai`:
//
//	<prefix>\opencode.cmd                                                                       (shim)
//	<prefix>\node_modules\opencode-ai\node_modules\opencode-windows-{x64,x64-baseline,arm64}\bin\opencode.exe (native)
//
// `opencode-windows-x64-baseline` ships for older CPUs without AVX2;
// `opencode-windows-arm64` ships for Surface / Copilot+ PC hosts.
// Candidates are tried in GOARCH-preferred order so the most likely match
// for the current host comes first.
//
// statFn is injected so this is testable on non-Windows hosts.
func resolveOpenCodeNativeFromShim(shimPath string, statFn func(string) (os.FileInfo, error)) string {
	if !strings.EqualFold(filepath.Ext(shimPath), ".cmd") {
		return ""
	}
	prefix := filepath.Dir(shimPath)
	for _, pkg := range opencodeWindowsPackageCandidates(runtime.GOARCH) {
		candidate := filepath.Join(prefix, "node_modules", "opencode-ai", "node_modules", pkg, "bin", "opencode.exe")
		if _, err := statFn(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// opencodeWindowsPackageCandidates returns the npm platform package names
// that may host the bundled `opencode.exe` on Windows, ordered so the most
// likely match for the given GOARCH comes first. ARM64 hosts try the arm64
// build first; everything else tries x64, then the baseline x64 build for
// older CPUs without AVX2, then arm64 as a final fallback. Cost is one
// extra statFn call per miss when the GOARCH-preferred package isn't
// installed.
func opencodeWindowsPackageCandidates(goarch string) []string {
	switch goarch {
	case "arm64":
		return []string{"opencode-windows-arm64", "opencode-windows-x64", "opencode-windows-x64-baseline"}
	default:
		return []string{"opencode-windows-x64", "opencode-windows-x64-baseline", "opencode-windows-arm64"}
	}
}

// extractToolOutput converts the tool state output (which may be a string or
// structured object) into a string.
func extractToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

// ── JSON types for `opencode run --format json` stdout events ──

// opencodeEvent represents a single JSON line from `opencode run --format json`.
//
// Event types observed in real output:
//
//	"step_start"  — agent step begins
//	"text"        — text output from agent (part.text)
//	"tool_use"    — tool invocation with call and result (part.tool, part.callID, part.state)
//	"error"       — error from opencode (error.name, error.data.message)
//	"step_finish" — agent step completes (includes token usage)
type opencodeEvent struct {
	Type      string            `json:"type"`
	Timestamp int64             `json:"timestamp,omitempty"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      opencodeEventPart `json:"part"`
	Error     *opencodeError    `json:"error,omitempty"`
}

// opencodeEventPart represents the part field in an opencode event.
type opencodeEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	// Text events
	Text string `json:"text,omitempty"`

	// Tool use events
	Tool   string             `json:"tool,omitempty"`
	CallID string             `json:"callID,omitempty"`
	State  *opencodeToolState `json:"state,omitempty"`
	// OpenCode excludes provider-executed tools when deciding whether a tool
	// result requires another model step.
	Metadata *opencodePartMetadata `json:"metadata,omitempty"`

	// step_finish token usage
	Tokens *opencodeTokens `json:"tokens,omitempty"`

	// step_finish cost, a sibling of the token block rather than a member of
	// it. Read only as round-trip evidence by stepReportedUsage; opencode's
	// billing figures come from the token counters above.
	Cost float64 `json:"cost,omitempty"`

	// step_finish reason (FinishReason: "stop", "tool-calls", …). Absent on
	// older opencode versions whose step-finish parts predate the field.
	Reason string `json:"reason,omitempty"`
}

type opencodePartMetadata struct {
	ProviderExecuted bool `json:"providerExecuted,omitempty"`
}

// opencodeTokens represents token usage in a step_finish event. Reasoning and
// Total are separate counters in the protocol, not components of Input/Output,
// so a step can report either while both of those are zero; they are parsed so
// stepReportedUsage can see them.
type opencodeTokens struct {
	Input     int64                `json:"input"`
	Output    int64                `json:"output"`
	Reasoning int64                `json:"reasoning,omitempty"`
	Total     int64                `json:"total,omitempty"`
	Cache     *opencodeCacheTokens `json:"cache,omitempty"`
}

type opencodeCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// opencodeToolState represents the state of a tool invocation.
type opencodeToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// opencodeError represents an error event from opencode.
type opencodeError struct {
	Name string           `json:"name,omitempty"`
	Data *opencodeErrData `json:"data,omitempty"`
}

// Message returns the human-readable error message.
func (e *opencodeError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type opencodeErrData struct {
	Message string `json:"message,omitempty"`
}
