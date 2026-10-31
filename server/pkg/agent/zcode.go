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

// zcodeBackend drives the ZCode CLI's native headless protocol:
// `zcode -p <prompt> --output-format stream-json`. The event stream is the
// runtime's own ZCode Protocol JSONL (session.*/turn.*/model.* events), not a
// Claude-compatible schema — captured against zcode-app-cli 3.16.3
// (zcode-runtime 0.17.0). See testdata captures referenced in zcode_test.go.
type zcodeBackend struct {
	cfg Config
}

// zcodeBlockedArgs are owned by Multica. The task prompt (-p/--prompt), the
// stream protocol (--output-format), the session binding (--resume/--continue)
// and the working directory (--cwd) are daemon-owned; overriding any of them
// would cut the daemon↔CLI contract. --model/--effort stay daemon-owned too so
// the agent's model field is the single source of truth for a run.
var zcodeBlockedArgs = map[string]blockedArgMode{
	"-p":               blockedWithValue,
	"--prompt":         blockedWithValue,
	"--output-format":  blockedWithValue,
	"--model":          blockedWithValue,
	"--effort":         blockedWithValue,
	"--resume":         blockedWithValue,
	"-c":               blockedStandalone,
	"--continue":       blockedStandalone,
	"--cwd":            blockedWithValue,
	"--attach":         blockedWithValue,
	"--json":           blockedStandalone,
	"--no-color":       blockedStandalone,
	"--verbose":        blockedStandalone,
	"--locale":         blockedWithValue,
	"--browser-use":    blockedWithValue,
	"--surface":        blockedWithValue,
	"--workflow-mode":  blockedWithValue,
	"--target":         blockedWithValue,
	"--mode":           blockedWithValue,
	"--input-format":   blockedWithValue,
	"--session-id":     blockedWithValue,
	"--session-tool":   blockedWithValue,
	"--allowed-tools":  blockedWithValue,
	"--print":          blockedStandalone,
	"--memory-bench":   blockedStandalone,
	"--target-replace": blockedStandalone,
}

// buildZcodeArgs assembles the argv for a one-shot zcode invocation. The
// prompt is appended LAST (after custom args): ZCode has no stdin prompt mode,
// so `-p <text>` is the only non-interactive entry point, and argv-final keeps
// it in parsing position no matter what fixed args say. On macOS and Linux
// execve passes it through verbatim. On Windows the launcher is rewritten to a
// PowerShell .ps1 invocation (zcode_invocation_windows.go), whose
// re-serialisation loses embedded double quotes — the same limitation
// cursor-agent accepted before #5649 and documented for qwen; a stdin-based
// fix will need upstream support for reading the prompt from stdin.
func buildZcodeArgs(opts ExecOptions, prompt string, logger *slog.Logger) []string {
	args := []string{"--output-format", "stream-json", "--no-color"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, zcodeBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, zcodeBlockedArgs, logger)...)
	args = append(args, "-p", prompt)
	return args
}

func (b *zcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "zcode"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("zcode executable not found at %q: %w", execName, err)
	}
	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildZcodeArgs(opts, prompt, b.cfg.Logger)

	cmd, _, _ := b.cfg.commandAt(execName).execVia(runCtx, chooseZcodeInvocation, lookedUp, args, b.cfg.Logger)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
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
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start zcode: %w", err)
	}
	b.cfg.Logger.Info("zcode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		started := time.Now()
		state := zcodeStreamState{model: opts.Model, usage: make(map[string]TokenUsage), resumed: opts.ResumeSessionID != ""}
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
			var event zcodeStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				state.invalidEventCount++
				continue
			}
			state.eventCount++
			state.lastEventType = event.Type
			handleZcodeEvent(event, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(started)

		// zcode exits 0 even when it fails before starting a turn (unknown
		// --model, bad flags), so the terminal stream event — not the exit
		// code — decides the outcome.
		status, output, errMsg := finalizeStreamResult("zcode", timeout, runCtx.Err(), nil, exitErr, state.sessionID, streamTerminalState{
			lastAssistantText: state.lastAssistantText,
			finalResultText:   state.finalResultText,
			sawResult:         state.sawResult,
			resultIsError:     state.resultIsError,
			scanErr:           scanErr,
		}, "")
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "zcode", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider: "zcode", cliVersion: b.cfg.CLIVersion, model: state.model,
			exitCode: streamProcessExitCode(exitErr), eventCount: state.eventCount,
			invalidEventCount: state.invalidEventCount, sawResult: state.sawResult,
			resultIsError: state.resultIsError, resultBytes: len(state.finalResultText),
			lastAssistantBytes: len(state.lastAssistantText),
			scannerError:       scanErr != nil, lastEventType: state.lastEventType,
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

// zcodeStreamEvent is one line of `zcode -p --output-format stream-json`.
// Every event carries top-level sessionId; payloads are type-specific.
type zcodeStreamEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId,omitempty"`
	TurnID    string          `json:"turnId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type zcodeStreamingPayload struct {
	Kind  string `json:"kind"`
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
}

type zcodeUpdatedPayload struct {
	// Terminal success events carry content + stopReason + usage; interim
	// events carry provider/model bookkeeping instead.
	Content    string      `json:"content,omitempty"`
	StopReason string      `json:"stopReason,omitempty"`
	ProviderID string      `json:"providerId,omitempty"`
	ModelID    string      `json:"modelId,omitempty"`
	Usage      *zcodeUsage `json:"usage,omitempty"`
}

type zcodeFailedPayload struct {
	Error *struct {
		Message string `json:"message,omitempty"`
		Detail  string `json:"detail,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

type zcodeUsage struct {
	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	TotalTokens     int64 `json:"totalTokens"`
	CacheReadTokens int64 `json:"cacheReadTokens"`
}

type zcodeStreamState struct {
	sessionID, model, lastAssistantText, finalResultText, lastEventType string
	sawResult, resultIsError                                            bool
	usage                                                               map[string]TokenUsage
	usedResultUsage                                                     bool
	resumed                                                             bool
	eventCount, invalidEventCount, assistantEventCount                  int
}

func handleZcodeEvent(event zcodeStreamEvent, ch chan<- Message, state *zcodeStreamState) {
	if event.SessionID != "" {
		state.sessionID = event.SessionID
	}
	switch event.Type {
	case "turn.started":
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: state.sessionID})
	case "session.resumed":
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: state.sessionID})
	case "model.streaming":
		state.assistantEventCount++
		var payload zcodeStreamingPayload
		if json.Unmarshal(event.Payload, &payload) != nil {
			return
		}
		if payload.Kind == "text_delta" && payload.Delta != "" {
			state.lastAssistantText += payload.Delta
			trySend(ch, Message{Type: MessageText, Content: payload.Delta})
		}
	case "session.updated":
		var payload zcodeUpdatedPayload
		if json.Unmarshal(event.Payload, &payload) != nil {
			return
		}
		if payload.ModelID != "" {
			state.model = payload.ModelID
		}
		if payload.StopReason == "" {
			return
		}
		// Terminal event: content + stopReason + per-run usage.
		state.sawResult = true
		if payload.Content != "" {
			state.finalResultText = payload.Content
		} else {
			state.finalResultText = state.lastAssistantText
		}
		if !state.resumed && payload.Usage != nil && state.model != "" &&
			(payload.Usage.InputTokens > 0 || payload.Usage.OutputTokens > 0) {
			// inputTokens already includes cache reads; keep the shared
			// buckets mutually exclusive like the other stream adapters.
			state.usage = map[string]TokenUsage{state.model: {
				InputTokens:     max(0, payload.Usage.InputTokens-payload.Usage.CacheReadTokens),
				OutputTokens:    payload.Usage.OutputTokens,
				CacheReadTokens: payload.Usage.CacheReadTokens,
			}}
			state.usedResultUsage = true
		}
	case "turn.failed":
		state.sawResult = true
		state.resultIsError = true
		state.finalResultText = zcodeErrorText(event.Payload)
	}
}

func zcodeErrorText(raw json.RawMessage) string {
	var payload zcodeFailedPayload
	if json.Unmarshal(raw, &payload) == nil && payload.Error != nil {
		if payload.Error.Message != "" {
			return payload.Error.Message
		}
		if payload.Error.Detail != "" {
			return payload.Error.Detail
		}
		if payload.Error.Code != "" {
			return payload.Error.Code
		}
	}
	if len(raw) > 0 {
		return string(raw)
	}
	return "zcode returned a turn.failed event without details"
}
