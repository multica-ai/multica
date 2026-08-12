package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// zcodeStreamBackend drives the ZCode CLI in streaming NDJSON mode
// (`zcode --prompt <prompt> --stream-json [--resume <sessionId>] [--cwd <dir>]`).
//
// This backend requires zcode-app-cli with the streaming patch (fork
// tornado404/zcode-cli, branch feat/stream-json-output) or any upstream release
// that ships equivalent --stream-json support. The event schema is intentionally
// qwen-compatible: zcode-cli's patch maps its internal runtime events
// (model_streaming, tool_call_*, turn_complete) into the same
// {type, message:{content:[...]}} shape qwen emits, so this backend reuses the
// qwen event/message/content-block types and the shared streaming finalize
// machinery (finalizeStreamResult, streamTerminalState, newAgentStreamScanner,
// trySend) unchanged.
//
// Unlike qwen, zcode has no CLI model override (the model is fixed by
// ~/.zcode/cli/config.json), so the model bucket falls back to "zcode" when no
// model is reported in the stream. Session continuity works the same way: the
// session id arrives on stream events and is returned via Result.SessionID for
// the daemon to feed back as --resume on the next turn.
type zcodeStreamBackend struct {
	cfg Config
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

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildZcodeStreamArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execName, args...)
	hideAgentWindow(cmd)
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
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		started := time.Now()
		state := zcodeStreamState{usage: make(map[string]TokenUsage)}
		go func() {
			<-runCtx.Done()
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
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
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
