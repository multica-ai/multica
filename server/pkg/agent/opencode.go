package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	if opts.SystemPrompt != "" {
		args = append(args, "--prompt", opts.SystemPrompt)
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, opencodeBlockedArgs, b.cfg.Logger)...)
	args = append(args, prompt)

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
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
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
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[opencode:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	b.cfg.Logger.Info("opencode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead pid.
	procDone := make(chan struct{})

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
		if cmd.Process != nil {
			signalProcessGroup(cmd.Process, syscall.SIGTERM)
			select {
			case <-procDone: // exited within the grace window
			case <-time.After(opencodeTerminateGrace()):
				signalProcessGroup(cmd.Process, syscall.SIGKILL)
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
	noTerminalSignal bool       // guard fired: stream reached EOF before a step or required continuation completed
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

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

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
		case "tool_use":
			b.handleToolUseEvent(event, ch)
			if event.Part.Metadata == nil || !event.Part.Metadata.ProviderExecuted {
				stepHasContinuationTool = true
			}
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			openStep = true
			stepHasContinuationTool = false
			awaitingContinuation = false
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			openStep = false
			awaitingContinuation = event.Part.Reason == "tool-calls" ||
				(event.Part.Reason != "" && stepHasContinuationTool)
			stepHasContinuationTool = false
			// Accumulate token usage from step_finish events.
			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
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
	// open — or right after a step that finished with reason "tool-calls",
	// whose continuation step never started — means the run did not finish:
	// its provider stream died and `opencode run` exited without emitting an
	// error event. Fail closed on that structural evidence rather than
	// reporting a false-green completion.
	noTerminalSignal := false
	if finalStatus == "completed" && (openStep || awaitingContinuation) {
		finalStatus = "failed"
		if openStep {
			finalError = "opencode stream ended without a terminal signal (step still open at EOF)"
		} else {
			finalError = "opencode stream ended without a terminal signal (last step required a continuation that never started)"
		}
		noTerminalSignal = true
	}

	return eventResult{
		status:           finalStatus,
		errMsg:           finalError,
		output:           output.String(),
		sessionID:        sessionID,
		usage:            usage,
		noTerminalSignal: noTerminalSignal,
	}
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
// tool has completed (state.status == "completed").
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

	// If the tool has completed, also emit a tool-result message.
	if event.Part.State != nil && event.Part.State.Status == "completed" {
		outputStr := extractToolOutput(event.Part.State.Output)
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

	// step_finish reason (FinishReason: "stop", "tool-calls", …). Absent on
	// older opencode versions whose step-finish parts predate the field.
	Reason string `json:"reason,omitempty"`
}

type opencodePartMetadata struct {
	ProviderExecuted bool `json:"providerExecuted,omitempty"`
}

// opencodeTokens represents token usage in a step_finish event.
type opencodeTokens struct {
	Input  int64                `json:"input"`
	Output int64                `json:"output"`
	Cache  *opencodeCacheTokens `json:"cache,omitempty"`
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
