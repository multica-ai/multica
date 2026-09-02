package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// museBackend drives Muse Code's native headless JSONL protocol:
// muse exec --json --prompt-file <file> [--model <id>] [--reasoning-effort <level>].
// The event schema is the unified record stream (schema_version 1) observed on
// Muse Code 1.0.1 via `muse exec --json --provider echo` and live
// `muse exec --json --model muse-spark-1.2` captures: payload_type values
// include run.output.delta, task.lifecycle.output, tool.result,
// task.lifecycle.side_effect_intent, and run.terminal.completed.
type museBackend struct {
	cfg Config
}

// museBlockedArgs are daemon-owned flags that must not be overridden via
// custom_args. They protect the prompt delivery, model selection, reasoning
// budget, workspace scoping, session routing and the daemon's headless
// permission/sandbox posture.
var museBlockedArgs = map[string]blockedArgMode{
	"--prompt-file":                    blockedWithValue,
	"--model":                          blockedWithValue,
	"-m":                               blockedWithValue,
	"--reasoning-effort":               blockedWithValue,
	"--reasoning_effort":               blockedWithValue, // defensive alias
	"--workspace":                      blockedWithValue,
	"--session-id":                     blockedWithValue,
	"--worktree":                       blockedOptionalValue,
	"-w":                               blockedOptionalValue,
	"--worktree-base":                  blockedWithValue,
	"--worktree-existing":              blockedWithValue,
	"--max-model-steps":                blockedWithValue,
	"--max-tool-output-bytes":          blockedWithValue,
	"--approval-mode":                  blockedWithValue,
	"--approval-judge":                 blockedWithValue,
	"--trust-workspace":                blockedStandalone,
	"--disable-sandbox":                blockedStandalone,
	"--disable-write":                  blockedStandalone,
	"--disable-shell":                  blockedStandalone,
	"--disable-approval":               blockedStandalone,
	"--yolo":                           blockedStandalone,
	"--json":                           blockedStandalone,
	"--provider":                       blockedWithValue,
	"--preset":                         blockedWithValue,
	"--sandbox-network":                blockedWithValue,
	"exec":                             blockedStandalone, // subcommand
	"serve":                            blockedStandalone,
}

// buildMuseArgs assembles the argv for `muse exec --json`.
//
// The prompt is never part of argv. It is written to a 0600 temp file and
// passed via --prompt-file to avoid argv injection, large-prompt truncation
// and Windows PowerShell re-quoting hazards (mirroring qwen's stdin fix for
// #5649 / #6082). Only content-free, fixed flags remain in argv.
func buildMuseArgs(opts ExecOptions, logger *slog.Logger, promptFile string) []string {
	args := []string{"exec", "--json", "--prompt-file", promptFile}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--reasoning-effort", opts.ThinkingLevel)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session-id", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--workspace", opts.Cwd)
	}
	// Enforce daemon-owned headless posture.
	args = append(args, "--trust-workspace", "--approval-mode", "never", "--sandbox-network", "enabled")
	// Cap steps when the caller provides an explicit bound; otherwise leave the
	// CLI's own default (headless default is generous).
	if opts.MaxTurns > 0 {
		args = append(args, "--max-model-steps", fmt.Sprintf("%d", opts.MaxTurns))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, museBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, museBlockedArgs, logger)...)
	return args
}

func writeMusePromptToTemp(prompt string) (string, error) {
	f, err := os.CreateTemp("", "multica-muse-prompt-*.md")
	if err != nil {
		return "", fmt.Errorf("create muse prompt temp: %w", err)
	}
	path := f.Name()
	if _, err := io.WriteString(f, prompt); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write muse prompt temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close muse prompt temp: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func cleanupMusePromptTemp(path string) {
	_ = os.Remove(path)
}

func (b *museBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "muse"
	}
	if _, err := exec.LookPath(execName); err != nil {
		return nil, fmt.Errorf("muse executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	promptPath, err := writeMusePromptToTemp(prompt)
	if err != nil {
		cancel()
		return nil, err
	}
	var promptCleanup func()
	promptCleanup = func() { cleanupMusePromptTemp(promptPath) }
	defer func() {
		if promptCleanup != nil {
			promptCleanup()
		}
	}()

	args := buildMuseArgs(opts, b.cfg.Logger, promptPath)
	cmd := b.cfg.commandAt(execName).exec(runCtx, args...)
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
		return nil, fmt.Errorf("muse stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[muse:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start muse: %w", err)
	}
	// Hand off prompt file ownership to the goroutine.
	promptCleanup = nil
	b.cfg.Logger.Info("muse started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model, "thinking", opts.ThinkingLevel)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer cleanupMusePromptTemp(promptPath)

		started := time.Now()
		state := museStreamState{usage: make(map[string]TokenUsage)}

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
			var raw museRawEvent
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				state.invalidEventCount++
				continue
			}
			state.eventCount++
			handleMuseEvent(raw, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(started)

		status, output, errMsg := finalizeStreamResult("muse", timeout, runCtx.Err(), nil, exitErr, state.sessionID, streamTerminalState{
			lastAssistantText: state.lastAssistantText,
			finalResultText:   state.finalResultText,
			sawResult:         state.sawResult,
			resultIsError:     state.resultIsError,
			scanErr:           scanErr,
		}, "")
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "muse", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider: "muse", cliVersion: b.cfg.CLIVersion, model: state.model,
			exitCode: streamProcessExitCode(exitErr), eventCount: state.eventCount,
			invalidEventCount: state.invalidEventCount, assistantEventCount: state.assistantEventCount,
			toolUseCount: state.toolUseCount, sawResult: state.sawResult, resultIsError: state.resultIsError,
			resultBytes: len(state.finalResultText), lastAssistantBytes: len(state.lastAssistantText),
			scannerError: scanErr != nil, lastEventType: state.lastEventType,
			unreadableAssistantCount: state.unreadableAssistantCount,
		})
		b.cfg.Logger.Info("muse finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status: status, Output: output, Error: errMsg, DurationMs: duration.Milliseconds(),
			SessionID: resolveSessionID(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg), Usage: state.usage,
			ResumeRejected: resumeWasRejected(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg),
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// museRawEvent is the outer envelope for `muse exec --json`.
type museRawEvent struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Stream        *museStreamRef  `json:"stream"`
	PayloadType   string          `json:"payload_type"`
	Payload       json.RawMessage `json:"payload"`
}

type museStreamRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type museStreamState struct {
	sessionID, model, lastAssistantText, finalResultText, lastEventType string
	sawResult, resultIsError                                            bool
	usage                                                               map[string]TokenUsage
	eventCount, invalidEventCount, assistantEventCount, toolUseCount    int
	unreadableAssistantCount                                            int
	// toolUseSeen deduplicates tool: side_effect_intent so a single
	// proposal→accepted→scheduled chain does not emit three identical uses.
	toolUseSeen map[string]bool
}

func handleMuseEvent(raw museRawEvent, ch chan<- Message, state *museStreamState) {
	state.lastEventType = raw.PayloadType
	// Extract session id from the outer stream when present.
	if raw.Stream != nil && raw.Stream.Kind == "session" && raw.Stream.ID != "" {
		state.sessionID = raw.Stream.ID
	}
	switch raw.PayloadType {
	case "run.output.delta":
		var p struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw.Payload, &p) != nil {
			return
		}
		if p.Text != "" {
			state.assistantEventCount++
			state.lastAssistantText += p.Text
			trySend(ch, Message{Type: MessageText, Content: p.Text})
		}
	case "run.terminal.completed", "run.terminal.failed":
		var p struct {
			Kind     string  `json:"kind"`
			Terminal string  `json:"terminal"`
			Text     string  `json:"text"`
			Reason   *string `json:"reason"`
		}
		if json.Unmarshal(raw.Payload, &p) != nil {
			return
		}
		state.sawResult = true
		state.finalResultText = p.Text
		if p.Terminal == "failed" || (p.Reason != nil && *p.Reason != "") {
			state.resultIsError = true
		}
		if p.Terminal == "failed" && p.Text == "" && p.Reason != nil {
			state.finalResultText = *p.Reason
		}
	case "tool.result":
		var p struct {
			Kind   string `json:"kind"`
			CallID string `json:"call_id"`
			Text   string `json:"text"`
		}
		if json.Unmarshal(raw.Payload, &p) != nil {
			return
		}
		state.toolUseCount++
		trySend(ch, Message{Type: MessageToolResult, CallID: p.CallID, Output: p.Text})
	case "task.lifecycle.output":
		var p struct {
			Kind  string `json:"kind"`
			Event struct {
				Kind   string `json:"kind"`
				Chunk  string `json:"chunk"`
				TaskID string `json:"task_id"`
			} `json:"event"`
		}
		if json.Unmarshal(raw.Payload, &p) != nil {
			return
		}
		if p.Event.Chunk != "" {
			trySend(ch, Message{Type: MessageToolResult, CallID: p.Event.TaskID, Output: p.Event.Chunk})
		}
	case "task.lifecycle.side_effect_intent":
		var p struct {
			Kind  string `json:"kind"`
			Event struct {
				Kind           string `json:"kind"`
				Operation      string `json:"operation"`
				IdempotencyKey string `json:"idempotency_key"`
				TaskID         string `json:"task_id"`
			} `json:"event"`
		}
		if json.Unmarshal(raw.Payload, &p) != nil {
			return
		}
		if !strings.HasPrefix(p.Event.Operation, "tool:") {
			return
		}
		tool := strings.TrimPrefix(p.Event.Operation, "tool:")
		if tool == "" {
			return
		}
		if state.toolUseSeen == nil {
			state.toolUseSeen = make(map[string]bool)
		}
		key := p.Event.TaskID + ":" + p.Event.Operation
		if state.toolUseSeen[key] {
			return
		}
		state.toolUseSeen[key] = true
		callID := p.Event.IdempotencyKey
		if callID == "" {
			callID = p.Event.TaskID
		}
		// Normalize idempotency_key of form "tool:<call_id>" or "tool:<id>".
		if strings.HasPrefix(callID, "tool:") {
			callID = strings.TrimPrefix(callID, "tool:")
		}
		state.toolUseCount++
		trySend(ch, Message{Type: MessageToolUse, Tool: tool, CallID: callID})
	case "task.lifecycle.proposed", "task.lifecycle.accepted", "task.lifecycle.scheduled", "task.lifecycle.started", "task.lifecycle.status", "task.lifecycle.completed", "task.lifecycle.failed", "task.stream.linked", "runtime.command.accepted", "session.run.linked", "session.workspace_branch.observed", "run.model.configured", "turn.input.user", "run.lifecycle.started":
		// Control/status envelope - extract session id if nested but otherwise ignore.
		// Capture model id from run.model.configured for usage attribution.
		if raw.PayloadType == "run.model.configured" {
			var p struct {
				ModelID string `json:"model_id"`
				Kind    string `json:"kind"`
			}
			if json.Unmarshal(raw.Payload, &p) == nil && p.ModelID != "" {
				state.model = p.ModelID
			}
		}
	default:
		// Unknown payload - ignore but keep lastEventType for diagnostics.
	}
}

// Ensure museBackend satisfies Backend.
var _ Backend = (*museBackend)(nil)
