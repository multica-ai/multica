package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// zcodeStreamBackend is the entry point the agent.New("zcode") factory returns.
// At Execute time it probes whether the installed zcode CLI advertises
// --stream-json:
//
//   - If it does (the streaming-enabled zcode-cli-stream fork, or any future
//     upstream release that ships --stream-json), Execute drives
//     `zcode --prompt <prompt> --stream-json` and surfaces thinking, tool
//     calls, tool results, and incremental text as live Messages.
//   - If it does NOT (the published zcode-app-cli, which only supports --json),
//     Execute delegates to zcodeBackend, the --json summary path. This keeps a
//     standard install working instead of failing every turn on an unknown flag.
//
// The event schema on the streaming path is intentionally qwen-compatible:
// zcode-cli's --stream-json emits the same {type, message:{content:[...]}}
// shape qwen emits, so this backend reuses the qwen event/message/content-block
// types and the shared streaming finalize machinery (finalizeStreamResult,
// streamTerminalState, newAgentStreamScanner, trySend) unchanged.
//
// Unlike qwen, zcode has no CLI model override (the model is fixed by
// ~/.zcode/cli/config.json), so the model bucket falls back to "zcode" when no
// model is reported in the stream. Session continuity works the same way: the
// session id arrives on stream events and is returned via Result.SessionID for
// the daemon to feed back as --resume on the next turn.
type zcodeStreamBackend struct {
	cfg Config
}

// zcodeStreamCapability caches the --stream-json capability probe result per
// executable path for the life of the process. --help output is a static
// property of a binary, so re-running it per turn would add startup latency
// for no benefit; the cache keys on the resolved path so two different builds
// of zcode (one streaming, one not) never share a result.
var zcodeStreamCapability sync.Map // execName (string) -> supports (bool)

// zcodeStreamCapabilityProbe is the function that actually probes a binary.
// It is a package-level variable so tests can substitute a stub; production
// code calls it through zcodeSupportsStreamJSON, which memoizes the result.
var zcodeStreamCapabilityProbe = probeZcodeStreamCapability

// zcodeSupportsStreamJSON reports whether the zcode binary at execName
// advertises the --stream-json flag in its --help output. This is the
// capability gate that selects the streaming path vs. the --json fallback.
//
// Failures (binary missing, --help non-zero, exec error) report false so the
// backend degrades to the universally-supported --json mode rather than
// failing every turn on an unknown flag. The result is cached: --help is a
// static property of a binary, and re-running it per turn would only add
// startup latency.
func zcodeSupportsStreamJSON(execName string) bool {
	if v, ok := zcodeStreamCapability.Load(execName); ok {
		return v.(bool)
	}
	supported := zcodeStreamCapabilityProbe(execName)
	zcodeStreamCapability.Store(execName, supported)
	return supported
}

// probeZcodeStreamCapability runs `zcode --help` and reports whether the flag
// is advertised. --help text is read from both stdout and stderr (CLIs differ
// on which stream they use) and a non-zero exit is tolerated (some CLIs exit
// non-zero on --help); the decision is purely whether the flag token appears.
func probeZcodeStreamCapability(execName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, execName, "--help")
	out, _ := cmd.CombinedOutput()
	return strings.Contains(string(out), "--stream-json")
}

// zcodeTerminateGraceNanos optionally overrides, in nanoseconds, how long a
// cancelled zcode process group is given to exit after SIGTERM before it is
// SIGKILLed. Set via atomic store in tests; zero keeps the default.
var zcodeTerminateGraceNanos atomic.Int64

func zcodeTerminateGrace() time.Duration {
	if n := zcodeTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

// buildZcodeStreamArgs constructs the argv for a streaming zcode turn.
func buildZcodeStreamArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"--prompt", prompt, "--stream-json"}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	// ExtraArgs (MULTICA_ZCODE_ARGS, daemon-wide) precede CustomArgs
	// (per-agent), matching the documented precedence and the other backends.
	args = append(args, filterCustomArgs(opts.ExtraArgs, zcodeBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, zcodeBlockedArgs, logger)...)
	return args
}

func (b *zcodeStreamBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("zcode prompt must not be empty")
	}

	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "zcode"
	}
	if _, err := exec.LookPath(execName); err != nil {
		return nil, fmt.Errorf("zcode executable not found at %q: %w", execName, err)
	}

	// Capability gate: only zcode-cli builds that advertise --stream-json (the
	// streaming-enabled fork, or a future upstream release that ships it) take
	// the streaming path. The published zcode-app-cli only supports --json, so
	// without this gate every task on a standard install would fail on an
	// unknown flag. Fall back to the JSON summary backend, which is the
	// universally-supported contract.
	if !zcodeSupportsStreamJSON(execName) {
		b.cfg.Logger.Info("zcode --stream-json not advertised; using --json fallback", "exec", execName)
		return (&zcodeBackend{cfg: b.cfg}).Execute(ctx, prompt, opts)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildZcodeStreamArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execName, args...)
	hideAgentWindow(cmd)
	// Run zcode in its own process group so cancellation reaches the whole
	// tree — the zcode CLI plus any tool subprocess it spawns — not just the
	// direct child. The default CommandContext behaviour SIGKILLs only the
	// leader, orphaning descendants that keep the stdout pipe open and keep
	// running after the task is cancelled. This mirrors the fix already made
	// for claude (#5918), codex (#4520), and opencode (#4533).
	configureProcessGroup(cmd)
	// Take over context cancellation: the default would SIGKILL only the leader
	// the instant runCtx is done. We instead drive a graceful group-wide
	// SIGTERM→SIGKILL from the cancellation goroutine below and close stdout
	// only after the tree has been signalled. Returning nil keeps os/exec from
	// racing us with its own kill; WaitDelay remains the hard backstop.
	cmd.Cancel = func() error { return nil }
	// args contain the task prompt; never expose it in daemon logs.
	b.cfg.Logger.Info("agent command", "exec", execName, "provider", "zcode")
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("zcode stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[zcode:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start zcode: %w", err)
	}
	b.cfg.Logger.Info("zcode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead/reused pid.
	procDone := make(chan struct{})

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		started := time.Now()
		state := zcodeStreamState{usage: make(map[string]TokenUsage)}

		// On cancellation / timeout, terminate zcode (and every tool subprocess
		// it spawned) BEFORE unblocking the scanner. SIGTERM the whole process
		// group, give it a grace period, then SIGKILL the group if any member
		// is still alive. SIGKILL is uncatchable, so once delivered no group
		// member can write again — only then is it safe to close the stdout
		// read end as a last-resort unblock for a scanner a wedged descendant
		// still keeps open. WaitDelay is the final backstop.
		go func() {
			select {
			case <-procDone:
				return // finished on its own; nothing to terminate
			case <-runCtx.Done():
			}
			if cmd.Process != nil {
				signalProcessGroup(cmd.Process, syscall.SIGTERM)
				// Escalate to a group SIGKILL unless the WHOLE process group
				// has exited within the grace window. This must key off the
				// process group, not procDone: procDone only means cmd.Wait()
				// returned for the leader, so a SIGTERM-ignoring descendant
				// that does not hold zcode's stdout would let the leader exit,
				// close procDone, and skip the SIGKILL — leaking exactly the
				// orphan this fix targets.
				if !waitProcessGroupGone(cmd.Process, zcodeTerminateGrace()) {
					signalProcessGroup(cmd.Process, syscall.SIGKILL)
				}
			}
			_ = stdout.Close()
		}()

		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// Reuse the qwen event schema — zcode-cli's --stream-json emits the
			// same {type, message:{content:[...]}} shape by design.
			var event qwenStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				state.invalidEventCount++
				continue
			}
			state.eventCount++
			state.lastEventType = event.Type
			handleZcodeStreamEvent(event, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			// Scanner stopped consuming stdout. Close the pipe before Wait so a
			// child still writing a malformed/oversized event cannot deadlock
			// on the full OS pipe; the scanner error remains the primary failure.
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		close(procDone)
		duration := time.Since(started)

		status, output, errMsg := finalizeStreamResult("zcode", timeout, runCtx.Err(), nil, exitErr, state.sessionID, streamTerminalState{
			lastAssistantText: state.lastAssistantText,
			finalResultText:   state.finalResultText,
			sawResult:          state.sawResult,
			resultIsError:      state.resultIsError,
			scanErr:            scanErr,
		}, "")
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "zcode", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider: "zcode", cliVersion: b.cfg.CLIVersion, model: state.model,
			exitCode: streamProcessExitCode(exitErr), eventCount: state.eventCount,
			invalidEventCount: state.invalidEventCount, assistantEventCount: state.assistantEventCount,
			toolUseCount: state.toolUseCount, sawResult: state.sawResult, resultIsError: state.resultIsError,
			resultBytes: len(state.finalResultText), lastAssistantBytes: len(state.lastAssistantText),
			scannerError: scanErr != nil, lastEventType: state.lastEventType,
			unreadableAssistantCount: state.unreadableAssistantCount,
		})
		b.cfg.Logger.Info("zcode finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status: status, Output: output, Error: errMsg, DurationMs: duration.Milliseconds(),
			SessionID:      resolveSessionID(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg),
			Usage:          state.usage,
			ResumeRejected: resumeWasRejected(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg),
		}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

// zcodeStreamState mirrors qwenStreamState but keys usage under the "zcode"
// bucket when no model is reported in the stream (zcode's --stream-json does
// not carry a model field today).
type zcodeStreamState struct {
	sessionID, model, lastAssistantText, finalResultText, lastEventType string
	sawResult, resultIsError                                            bool
	usage                                                               map[string]TokenUsage
	eventCount, invalidEventCount, assistantEventCount, toolUseCount    int
	unreadableAssistantCount                                            int
}

// handleZcodeStreamEvent dispatches a qwen-schema event into Messages and state.
// It reuses handleQwenAssistant/handleQwenUser for content-block parsing
// (thinking/text/tool_use/tool_result) since zcode-cli emits identical blocks.
func handleZcodeStreamEvent(event qwenStreamEvent, ch chan<- Message, state *zcodeStreamState) {
	if event.SessionID != "" {
		state.sessionID = event.SessionID
	}
	switch event.Type {
	case "system":
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: state.sessionID})
	case "assistant":
		state.assistantEventCount++
		turn := handleZcodeAssistantEvent(event.Message, ch, state.usage)
		state.toolUseCount += turn.toolUses
		if !turn.understood {
			state.unreadableAssistantCount++
		}
		state.lastAssistantText = turn.resolveFallback(state.lastAssistantText)
	case "user":
		handleZcodeUserEvent(event.Message, ch)
	case "result":
		state.sawResult = true
		state.resultIsError = event.IsError || event.Subtype == "error" || event.Subtype == "failed"
		if state.resultIsError {
			state.finalResultText = qwenErrorText(event)
		} else {
			state.finalResultText = event.Result
		}
		if usage := zcodeResultUsage(event.Usage); len(usage) > 0 {
			state.usage = usage
		}
	case "error":
		state.sawResult = true
		state.resultIsError = true
		state.finalResultText = qwenErrorText(event)
	}
}

// handleZcodeAssistantEvent reuses qwen's content-block parsing but does not
// track a per-message model (zcode's stream omits model on assistant events).
func handleZcodeAssistantEvent(raw json.RawMessage, ch chan<- Message, usage map[string]TokenUsage) assistantTurn {
	var message qwenMessage
	if json.Unmarshal(raw, &message) != nil {
		return assistantTurn{}
	}
	turn := assistantTurn{understood: true}
	if message.Usage != nil && message.Model != "" {
		usage[message.Model] = qwenTokenUsage(message.Usage)
	}
	var text strings.Builder
	tools := 0
	for _, block := range message.Content {
		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Thinking})
			}
		case "text":
			if block.Text != "" {
				text.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "tool_use":
			tools++
			var input map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: input})
		default:
			turn.understood = false
		}
	}
	turn.text = text.String()
	turn.toolUses = tools
	return turn
}

// handleZcodeUserEvent reuses the same tool_result extraction as qwen.
func handleZcodeUserEvent(raw json.RawMessage, ch chan<- Message) {
	var message qwenMessage
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	for _, block := range message.Content {
		if block.Type == "tool_result" {
			trySend(ch, Message{Type: MessageToolResult, CallID: block.ToolUseID, Output: qwenToolResultOutput(block.Content)})
		}
	}
}

// zcodeResultUsage buckets the terminal usage under "zcode" when no model was
// reported on any assistant event. zcode-cli's --stream-json does not currently
// surface the serving model, so the daemon's usage rollup treats the key as an
// opaque bucket (matching the legacy --json backend's behavior).
func zcodeResultUsage(usage *qwenUsage) map[string]TokenUsage {
	if usage == nil || (usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadInputTokens == 0) {
		return nil
	}
	return map[string]TokenUsage{"zcode": {
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CacheReadTokens: usage.CacheReadInputTokens,
	}}
}
