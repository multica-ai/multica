package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// codeartsTerminateGraceNanos optionally overrides, in nanoseconds, how long a
// cancelled codearts process is given to exit after SIGTERM before it (and its
// whole process group) is SIGKILLed. Zero means use the default. It is atomic
// so tests can shorten the grace without racing the cancellation goroutine that
// reads it. See the cancellation handler in Execute for why termination must
// precede closing the stdout pipe (#4533).
var codeartsTerminateGraceNanos atomic.Int64

func codeartsTerminateGrace() time.Duration {
	if n := codeartsTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

// codeartsBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var codeartsBlockedArgs = map[string]blockedArgMode{
	"--format":                       blockedWithValue,  // JSON output format for daemon communication
	"--auto":                         blockedStandalone, // daemon-owned non-interactive permission mode
	"--sandbox":                      blockedStandalone, // daemon-owned sandbox policy
	"--dir":                          blockedWithValue,  // unsupported; cmd.Dir owns the workdir
	"--variant":                      blockedWithValue,  // unsupported by CodeArts
	"--dangerously-skip-permissions": blockedStandalone, // OpenCode-only permission flag
}

// codeartsBackend is an independent adapter for Huawei Cloud CodeArts CLI.
// CodeArts is derived from OpenCode, but its command surface, launcher
// environment, authentication behavior, and event evolution are owned here so
// changes to the OpenCode backend cannot silently change CodeArts execution.
type codeartsBackend struct {
	cfg Config
}

func newCodeArtsBackend(cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &codeartsBackend{cfg: cfg}, nil
}

func (b *codeartsBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codearts"
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("codearts executable not found at %q: %w", execPath, err)
	}
	if runtime.GOOS == "windows" {
		if native := resolveCodeArtsNativeFromShim(resolved, os.Stat); native != "" {
			b.cfg.Logger.Info("codearts resolved to native binary to avoid .cmd shim argv truncation", "shim", resolved, "native", native)
			resolved = native
		}
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := []string{"run", "--format", "json", "--auto"}
	// CodeArts does not expose OpenCode's --dir flag. cmd.Dir and PWD below
	// independently anchor its project and skill discovery at the task workdir.
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		b.cfg.Logger.Warn("codearts does not support thinking-level overrides; ignoring", "thinkingLevel", opts.ThinkingLevel)
	}
	// CodeArts's `run` subcommand has no --prompt flag. The runtime brief reaches
	// the agent through the per-task AGENTS.md the daemon writes into the
	// workdir, and the task prompt is delivered on stdin below.
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("codearts does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, codeartsBlockedArgs, b.cfg.Logger)...)
	// The task prompt is delivered on stdin, never argv — see the StdinPipe
	// wiring below. `codearts run` merges its variadic [message..] positional
	// with whatever is piped in, so an invocation that passes no positional
	// makes the piped text the entire run message. Inlining it instead fails
	// hard on Windows: CreateProcess caps lpCommandLine at 32,767 characters
	// (8,191 when a .cmd shim routes the call through cmd.exe), and a prompt
	// carrying the workspace's models and skills clears that on its own — the
	// process then never starts and Go surfaces the misleading "The filename or
	// extension is too long" (#6538). Keeping the prompt off argv also keeps it
	// out of OS process listings; the shared command logger separately redacts
	// argv values.

	runtimeCmd := b.cfg.commandAt(execPath)
	cmd := runtimeCmd.exec(runCtx, args...)
	hideAgentWindow(cmd)
	// Run codearts in its own process group so cancellation can reach the
	// whole tree (codearts plus any tool subprocess it spawns), not just the
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
	b.cfg.logAgentCommandWithPrompt(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "run")), len(prompt))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildCodeArtsEnv(b.cfg.Env)
	// Override PWD as well as cmd.Dir because the underlying CodeArts engine
	// consults it while resolving project configuration and skills.
	if opts.Cwd != "" {
		env = append(env, "PWD="+opts.Cwd)
	}
	// Project agent.mcp_config into CodeArts via OPENCODE_CONFIG_CONTENT —
	// CodeArts's general inline-config injection mechanism that merges at
	// "local" scope (after the project-config loop, before remote / managed
	// configs). MCP is the only field we currently project there; if a
	// future Multica field needs the same channel it would assemble a
	// combined CodeArts config slice before the env append.
	//
	// This deliberately leaves project configuration untouched because the
	// workdir is reused across turns for the same (agent, issue).
	mcpContent, err := buildCodeArtsMCPConfigContent(opts.McpConfig)
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
		return nil, fmt.Errorf("codearts stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codearts stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[codearts:stderr] ")

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start codearts: %w", err)
	}

	b.cfg.Logger.Info("codearts started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead pid.
	procDone := make(chan struct{})

	// Write the prompt from its own goroutine so it cannot deadlock against the
	// stdout reader below: a prompt larger than the OS pipe buffer (~64 KiB)
	// blocks mid-write until CodeArts drains it, and CodeArts cannot drain while
	// nobody is consuming its stdout. Closing stdin is what ends the prompt —
	// CodeArts reads it to EOF (`await Bun.stdin.text()`), so a stdin left open
	// hangs the run forever. Close on every path, success or error.
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	// On cancellation / timeout, terminate codearts (and the tool subprocesses
	// it spawned) BEFORE unblocking the scanner. The previous implementation
	// closed the stdout read end immediately, which can leave the child writing
	// into a closed pipe and spinning on EPIPE. Instead we SIGTERM the whole
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
		// CodeArts that stopped reading before draining it would otherwise
		// strand that goroutine for the lifetime of the daemon.
		closeStdin()
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGTERM)
			select {
			case <-procDone: // exited within the grace window
			case <-time.After(codeartsTerminateGrace()):
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
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		// Wait closes the process pipes, so a prompt write still blocked when
		// CodeArts exited has returned by now. The writer sends exactly once.
		writeErr := <-writeErrCh

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("codearts timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("codearts exited with error: %v", exitErr)
		} else if exitErr != nil && scanResult.noTerminalSignal {
			// Status is already "failed" from the terminal-signal guard; append
			// the process exit detail so a mid-step crash still surfaces the
			// signal / exit code that killed it.
			scanResult.errMsg = fmt.Sprintf("%s; codearts exited with error: %v", scanResult.errMsg, exitErr)
		} else if writeErr != nil && !scanResult.sawTerminalSignal {
			// A failed prompt write is only benign once the run is PROVEN to have
			// finished: CodeArts reads stdin to EOF before it does any work, so a
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
				scanResult.errMsg = fmt.Sprintf("codearts prompt write failed: %v", writeErr)
			} else {
				scanResult.errMsg = fmt.Sprintf("%s; codearts prompt write failed: %v", scanResult.errMsg, writeErr)
			}
			scanResult.status = "failed"
		}

		b.cfg.Logger.Info("codearts finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. CodeArts doesn't report model per-step, so we
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

// codeArtsEventResult holds the accumulated state from processing the event stream.
type codeArtsEventResult struct {
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
func (b *codeartsBackend) processEvents(r io.Reader, ch chan<- Message) codeArtsEventResult {
	var output strings.Builder
	var unparsedOutput strings.Builder
	var sessionID string
	var usage TokenUsage
	parsedEvents := 0
	finalStatus := "completed"
	var finalError string

	// Track step bracketing so a stream that ends mid-step is not mistaken for a
	// clean completion. CodeArts's JSON stream has no terminal result event
	// (unlike Claude's type:"result"), so "no error seen" is not proof the run
	// finished. codearts emits tool_use only on terminal states (completed or
	// error), so a dangling tool call implies an unclosed step — step bracketing
	// is the positive terminal signal. Recovered tool errors (state.status ==
	// "error") are normal in healthy runs and must not affect status.
	//
	// Step bracketing alone is not enough: step_finish carries a reason
	// (FinishReason: "stop", "tool-calls", …), and a run that still has tool
	// results to feed back normally closes its step with reason "tool-calls"
	// before the next step_start. Some providers return "stop" despite emitting
	// tool calls, though, and CodeArts deliberately continues those runs when a
	// non-provider-executed tool result must be fed back to the model. Track both
	// signals so EOF in either continuation gap fails closed. A missing reason
	// retains the older step-bracketing behavior for protocol compatibility.
	openStep := false                // between a step_start and its step_finish
	stepHasContinuationTool := false // current step has a local tool result CodeArts must feed back
	awaitingContinuation := false    // the last step_finish still required another step
	sawStepFinish := false           // at least one step closed; see codeArtsEventResult.sawTerminalSignal

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

		var event codeartsEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			if unparsedOutput.Len() < 4096 {
				if unparsedOutput.Len() > 0 {
					unparsedOutput.WriteByte('\n')
				}
				remaining := 4096 - unparsedOutput.Len()
				if len(line) > remaining {
					line = line[:remaining]
				}
				unparsedOutput.WriteString(line)
			}
			continue
		}
		parsedEvents++

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
			if codeArtsStepReportedUsage(&event.Part) {
				stepProducedOutput = true
			}
			lastStepVoid = !stepProducedOutput
		}
	}

	// Check for scanner errors (e.g. broken pipe, read errors).
	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("codearts stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	// Require a positive terminal signal. A clean EOF while a step is still
	// open — right after a step that finished with reason "tool-calls", whose
	// continuation step never started — or on a step that carried nothing at
	// all means the run did not finish: its provider stream died and
	// `codearts run` exited without emitting an error event. Fail closed on
	// that structural evidence rather than reporting a false-green completion.
	noTerminalSignal := false
	if finalStatus == "completed" && parsedEvents == 0 {
		finalStatus = "failed"
		finalError = "codearts returned no parseable JSON events"
		if detail := sanitizeCLIOutput(unparsedOutput.String()); detail != "" {
			finalError += ": " + detail
		}
		noTerminalSignal = true
	}
	if finalStatus == "completed" {
		switch {
		case openStep:
			finalStatus = "failed"
			finalError = "codearts stream ended without a terminal signal (step still open at EOF)"
			noTerminalSignal = true
		case awaitingContinuation:
			finalStatus = "failed"
			finalError = "codearts stream ended without a terminal signal (last step required a continuation that never started)"
			noTerminalSignal = true
		case lastStepVoid:
			finalStatus = "failed"
			finalError = "codearts stream ended on an empty step (no text, no tool call, no reported usage) — the provider produced nothing"
			noTerminalSignal = true
		}
	}

	return codeArtsEventResult{
		status:            finalStatus,
		errMsg:            finalError,
		output:            output.String(),
		sessionID:         sessionID,
		usage:             usage,
		noTerminalSignal:  noTerminalSignal,
		sawTerminalSignal: sawStepFinish && !noTerminalSignal,
	}
}

// codeArtsStepReportedUsage reports whether a step_finish part carries any evidence
// that the provider round-trip actually happened.
//
// CodeArts's protocol keeps reasoning and the aggregate total in fields of
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
func codeArtsStepReportedUsage(part *codeartsEventPart) bool {
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

func (b *codeartsBackend) handleTextEvent(event codeartsEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

// handleToolUseEvent processes "tool_use" events from codearts. A single
// tool_use event contains both the call and result in part.state when the
// tool reaches a terminal state (state.status is "completed" or "error").
func (b *codeartsBackend) handleToolUseEvent(event codeartsEvent, ch chan<- Message) {
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
		outputStr := codeArtsExtractToolOutput(state.Output)
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

// handleErrorEvent processes "error" events from codearts. CodeArts can exit
// with RC=0 even on errors (e.g. invalid model), so error events are the
// reliable signal for failures.
func (b *codeartsBackend) handleErrorEvent(event codeartsEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown codearts error"
	}

	b.cfg.Logger.Warn("codearts error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

// resolveCodeArtsNativeFromShim returns the native executable bundled next to
// the Windows launcher. The launcher is a batch file and cannot be spawned
// reliably by the daemon when prompts contain newlines or large payloads.
func resolveCodeArtsNativeFromShim(shimPath string, statFn func(string) (os.FileInfo, error)) string {
	if !strings.EqualFold(filepath.Base(shimPath), "codearts.cmd") {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(shimPath), "bin", "codearts.exe")
	if _, err := statFn(candidate); err != nil {
		return ""
	}
	return candidate
}

// codeArtsExtractToolOutput converts the tool state output (which may be a string or
// structured object) into a string.
func codeArtsExtractToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

// ── JSON types for `codearts run --format json` stdout events ──

// codeartsEvent represents a single JSON line from `codearts run --format json`.
//
// Event types observed in real output:
//
//	"step_start"  — agent step begins
//	"text"        — text output from agent (part.text)
//	"tool_use"    — tool invocation with call and result (part.tool, part.callID, part.state)
//	"error"       — error from codearts (error.name, error.data.message)
//	"step_finish" — agent step completes (includes token usage)
type codeartsEvent struct {
	Type      string            `json:"type"`
	Timestamp int64             `json:"timestamp,omitempty"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      codeartsEventPart `json:"part"`
	Error     *codeartsError    `json:"error,omitempty"`
}

// codeartsEventPart represents the part field in an codearts event.
type codeartsEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	// Text events
	Text string `json:"text,omitempty"`

	// Tool use events
	Tool   string             `json:"tool,omitempty"`
	CallID string             `json:"callID,omitempty"`
	State  *codeartsToolState `json:"state,omitempty"`
	// CodeArts excludes provider-executed tools when deciding whether a tool
	// result requires another model step.
	Metadata *codeartsPartMetadata `json:"metadata,omitempty"`

	// step_finish token usage
	Tokens *codeartsTokens `json:"tokens,omitempty"`

	// step_finish cost, a sibling of the token block rather than a member of
	// it. Read only as round-trip evidence by codeArtsStepReportedUsage; codearts's
	// billing figures come from the token counters above.
	Cost float64 `json:"cost,omitempty"`

	// step_finish reason (FinishReason: "stop", "tool-calls", …). Absent on
	// older codearts versions whose step-finish parts predate the field.
	Reason string `json:"reason,omitempty"`
}

type codeartsPartMetadata struct {
	ProviderExecuted bool `json:"providerExecuted,omitempty"`
}

// codeartsTokens represents token usage in a step_finish event. Reasoning and
// Total are separate counters in the protocol, not components of Input/Output,
// so a step can report either while both of those are zero; they are parsed so
// codeArtsStepReportedUsage can see them.
type codeartsTokens struct {
	Input     int64                `json:"input"`
	Output    int64                `json:"output"`
	Reasoning int64                `json:"reasoning,omitempty"`
	Total     int64                `json:"total,omitempty"`
	Cache     *codeartsCacheTokens `json:"cache,omitempty"`
}

type codeartsCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// codeartsToolState represents the state of a tool invocation.
type codeartsToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// codeartsError represents an error event from codearts.
type codeartsError struct {
	Name string           `json:"name,omitempty"`
	Data *codeartsErrData `json:"data,omitempty"`
}

// Message returns the human-readable error message.
func (e *codeartsError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type codeartsErrData struct {
	Message string `json:"message,omitempty"`
}

// buildCodeArtsEnv reproduces the environment set by the official CodeArts
// launcher when the daemon bypasses its Windows batch shim. Agent custom_env
// wins over these defaults, matching the rest of the backend contract.
func buildCodeArtsEnv(extra map[string]string) []string {
	defaults := map[string]string{
		"package_tag":                    "undefined",
		"CODEAGENT_USE_MESSAGE_FEEDBACK": "false",
		"OMO_SEND_ANONYMOUS_TELEMETRY":   "0",
		"SCENARIO":                       "codeartsdoer",
		"OPENCODE_CHANNEL":               "latest",
		"OPENCODE_CONFIG_FILE":           "codearts_cli.json,codearts_cli.jsonc",
		"OPENCODE_MODE":                  "tui",
		"PLUGIN_ENV":                     "hc",
		"OPENCODE_DISABLE_MODELS_FETCH":  "1",
		"OPENCODE_DISABLE_AUTOUPDATE":    "true",
		"OPENCODE_ALWAYS_NOTIFY_UPDATE":  "false",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		configDir := filepath.Join(home, ".codeartsdoer")
		defaults["KERNEL_DATA_DIR"] = filepath.Join(configDir, "cli-data")
		defaults["KERNEL_CONFIG_DIR"] = configDir
		defaults["OPENCODE_CONFIG"] = filepath.Join(configDir, "codearts_cli.json")
	}
	for key, value := range extra {
		defaults[key] = value
	}
	return mergeEnv(os.Environ(), defaults)
}

// buildCodeArtsMCPConfigContent uses the configuration schema shared by the
// CodeArts engine and OpenCode. Only this schema translation is shared; the
// CodeArts process and event adapter remain independent.
func buildCodeArtsMCPConfigContent(raw json.RawMessage) (string, error) {
	return buildOpenCodeMCPConfigContent(raw)
}

// discoverCodeArtsModels uses the model catalog command exposed by CodeArts.
// CodeArts does not expose OpenCode variants, so variant metadata is removed.
func discoverCodeArtsModels(ctx context.Context, runtimeCmd Command) ([]Model, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "codearts"
	}
	resolved, err := exec.LookPath(runtimeCmd.Path)
	if err != nil {
		return []Model{}, nil
	}
	runtimeCmd.Path = resolved
	if runtime.GOOS == "windows" {
		if native := resolveCodeArtsNativeFromShim(runtimeCmd.Path, os.Stat); native != "" {
			runtimeCmd.Path = native
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	run := func(args ...string) []Model {
		cmd := runtimeCmd.exec(runCtx, args...)
		hideAgentWindow(cmd)
		cmd.Env = buildCodeArtsEnv(nil)
		out, _ := outputOwned(cmd, runtimeCmd.logger)
		models := parseOpenCodeModels(string(out))
		for i := range models {
			models[i].Thinking = nil
		}
		return models
	}

	models := run("models", "--verbose")
	if len(models) == 0 {
		models = run("models")
	}
	return models, nil
}

var ansiControlSequenceRe = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func sanitizeCLIOutput(raw string) string {
	raw = ansiControlSequenceRe.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "\r", "")
	return strings.TrimSpace(raw)
}
