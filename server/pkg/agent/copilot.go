package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// copilotBackend implements Backend by spawning the GitHub Copilot CLI
// with --output-format json and parsing its JSONL event stream.
//
// The v1 integration uses the -p (pipe) mode which is the stable
// automation/CI channel. The prompt is passed as a CLI argument (not stdin).
// Events arrive as newline-delimited JSON on stdout in the Copilot CLI's
// own envelope format: { "type": "dotted.event.name", "data": {...}, ... }
type copilotBackend struct {
	cfg Config
}

// copilotEventState holds mutable state accumulated while processing the JSONL
// event stream. It is shared between production (Execute) and tests via
// handleCopilotEvent, so the parsing logic is never duplicated.
type copilotEventState struct {
	output      strings.Builder
	sessionID   string
	activeModel string
	finalStatus string
	finalError  string
	usage       map[string]TokenUsage
}

func newCopilotEventState(seedModel string) *copilotEventState {
	return &copilotEventState{
		activeModel: seedModel,
		finalStatus: "completed",
		usage:       make(map[string]TokenUsage),
	}
}

// handleCopilotEvent processes a single parsed copilotEvent, updates state,
// and returns zero or more Messages to emit. Extracted so tests can call the
// exact same logic without duplicating the switch body.
func handleCopilotEvent(evt copilotEvent, st *copilotEventState) []Message {
	var msgs []Message

	switch evt.Type {
	case "session.start":
		var ss copilotSessionStart
		if err := json.Unmarshal(evt.Data, &ss); err == nil {
			if ss.SelectedModel != "" {
				st.activeModel = ss.SelectedModel
			}
			// Capture sessionId from session.start as well: the synthetic
			// "result" event may never arrive (timeout, cancel, crash, or a
			// session.error before result), and without this the daemon
			// reports SessionID="" and the chat-session resume pointer can
			// drift to a stale turn. result still wins when it does arrive.
			if ss.SessionID != "" {
				st.sessionID = ss.SessionID
			}
		}

	case "assistant.message_delta":
		var delta copilotMessageDelta
		if err := json.Unmarshal(evt.Data, &delta); err == nil && delta.DeltaContent != "" {
			// Write to output as defense-in-depth: if the process is killed
			// before the final assistant.message arrives, we still have text.
			st.output.WriteString(delta.DeltaContent)
			msgs = append(msgs, Message{Type: MessageText, Content: delta.DeltaContent})
		}

	case "assistant.message":
		var msg copilotAssistantMessage
		if err := json.Unmarshal(evt.Data, &msg); err != nil {
			return nil
		}
		// assistant.message carries the full turn content. Since deltas
		// already wrote to output incrementally, we reset and write the
		// authoritative content once to avoid double-counting.
		if msg.Content != "" {
			// Separator between turns.
			trimmed := strings.TrimSuffix(st.output.String(), msg.Content)
			st.output.Reset()
			st.output.WriteString(trimmed)
			if st.output.Len() > 0 && !strings.HasSuffix(st.output.String(), "\n\n") {
				st.output.WriteString("\n\n")
			}
			st.output.WriteString(msg.Content)
		}
		if msg.ReasoningText != "" {
			msgs = append(msgs, Message{Type: MessageThinking, Content: msg.ReasoningText})
		}
		if msg.OutputTokens > 0 {
			u := st.usage[st.activeModel]
			u.OutputTokens += msg.OutputTokens
			st.usage[st.activeModel] = u
		}
		for _, tr := range msg.ToolRequests {
			var input map[string]any
			if tr.Arguments != nil {
				_ = json.Unmarshal(tr.Arguments, &input)
			}
			msgs = append(msgs, Message{
				Type:   MessageToolUse,
				Tool:   tr.Name,
				CallID: tr.ToolCallID,
				Input:  input,
			})
		}

	case "assistant.reasoning", "assistant.reasoning_delta":
		// Streaming thinking content — may arrive as full or delta.
		var r copilotReasoning
		if err := json.Unmarshal(evt.Data, &r); err == nil {
			text := r.Content
			if text == "" {
				text = r.DeltaContent
			}
			if text != "" {
				msgs = append(msgs, Message{Type: MessageThinking, Content: text})
			}
		}

	case "tool.execution_complete":
		var tc copilotToolExecComplete
		if err := json.Unmarshal(evt.Data, &tc); err != nil {
			return nil
		}
		if tc.Model != "" {
			st.activeModel = tc.Model
		}
		resultContent := ""
		if tc.Success && tc.Result != nil {
			resultContent = tc.Result.Content
		} else if !tc.Success {
			if tc.Error != nil {
				resultContent = "Error: " + tc.Error.Message
			} else if tc.Result != nil {
				resultContent = tc.Result.Content
			}
		}
		msgs = append(msgs, Message{
			Type:   MessageToolResult,
			CallID: tc.ToolCallID,
			Output: resultContent,
		})

	case "assistant.turn_start":
		msgs = append(msgs, Message{Type: MessageStatus, Status: "running"})

	case "session.error":
		var se copilotSessionError
		if err := json.Unmarshal(evt.Data, &se); err == nil {
			st.finalStatus = "failed"
			st.finalError = se.Message
			msgs = append(msgs, Message{Type: MessageLog, Level: "error", Content: se.Message})
		}

	case "session.warning":
		var sw copilotSessionWarning
		if err := json.Unmarshal(evt.Data, &sw); err == nil {
			msgs = append(msgs, Message{Type: MessageLog, Level: "warn", Content: sw.Message})
		}

	case "result":
		if evt.SessionID != "" {
			st.sessionID = evt.SessionID
		}
		if evt.ExitCode != 0 {
			st.finalStatus = "failed"
			st.finalError = withCopilotExitCode(st.finalError, evt.ExitCode)
		}
	}

	return msgs
}

func withCopilotExitCode(msg string, exitCode int) string {
	exitMsg := fmt.Sprintf("copilot exited with code %d", exitCode)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return exitMsg
	}
	if strings.Contains(msg, exitMsg) {
		return msg
	}
	return msg + "; " + exitMsg
}

func (b *copilotBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "copilot"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("copilot executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildCopilotArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("copilot stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[copilot:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start copilot: %w", err)
	}

	b.cfg.Logger.Info("copilot started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		seedModel := opts.Model
		if seedModel == "" {
			seedModel = "copilot"
		}
		st := newCopilotEventState(seedModel)

		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt copilotEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				slog.Warn("copilot event parse failed", "err", err, "line", line)
				continue
			}

			for _, m := range handleCopilotEvent(evt, st) {
				trySend(msgCh, m)
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("copilot stdout scanner error", "err", err)
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			st.finalStatus = "timeout"
			st.finalError = fmt.Sprintf("copilot timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			st.finalStatus = "aborted"
			st.finalError = "execution cancelled"
		} else if exitErr != nil && st.finalStatus == "completed" {
			st.finalStatus = "failed"
			st.finalError = fmt.Sprintf("copilot exited with error: %v", exitErr)
		}
		if st.finalError != "" {
			st.finalError = withAgentStderr(st.finalError, "copilot", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("copilot finished", "pid", cmd.Process.Pid, "status", st.finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     st.finalStatus,
			Output:     st.output.String(),
			Error:      st.finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  st.sessionID,
			Usage:      st.usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// ── Copilot CLI JSONL event types ──
//
// Copilot CLI v1.0.28+ with --output-format json emits JSONL on stdout.
// Each line is a JSON object with:
//
//	{ "type": "dotted.event.name", "data": {...}, "id": "...",
//	  "timestamp": "...", "parentId": "...", "ephemeral": bool }
//
// The final line is a synthetic "result" event with top-level fields:
//
//	{ "type": "result", "sessionId": "...", "exitCode": 0, "usage": {...} }

// copilotEvent is the envelope for all Copilot JSONL events.
type copilotEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	ParentID  string          `json:"parentId,omitempty"`
	Ephemeral bool            `json:"ephemeral,omitempty"`

	// Top-level fields on the synthetic "result" event only.
	SessionID string              `json:"sessionId,omitempty"`
	ExitCode  int                 `json:"exitCode,omitempty"`
	Usage     *copilotResultUsage `json:"usage,omitempty"`
}

// copilotSessionStart is data payload for "session.start".
type copilotSessionStart struct {
	SessionID     string `json:"sessionId"`
	SelectedModel string `json:"selectedModel"`
}

// copilotAssistantMessage is data payload for "assistant.message".
type copilotAssistantMessage struct {
	MessageID     string               `json:"messageId"`
	Content       string               `json:"content"`
	ToolRequests  []copilotToolRequest `json:"toolRequests"`
	OutputTokens  int64                `json:"outputTokens"`
	InteractionID string               `json:"interactionId"`
	ReasoningText string               `json:"reasoningText,omitempty"`
}

// copilotToolRequest is one tool invocation inside assistant.message.
type copilotToolRequest struct {
	ToolCallID       string          `json:"toolCallId"`
	Name             string          `json:"name"`
	Arguments        json.RawMessage `json:"arguments"`
	Type             string          `json:"type"`
	IntentionSummary string          `json:"intentionSummary,omitempty"`
}

// copilotMessageDelta is data payload for "assistant.message_delta".
type copilotMessageDelta struct {
	MessageID    string `json:"messageId"`
	DeltaContent string `json:"deltaContent"`
}

// copilotToolExecComplete is data payload for "tool.execution_complete".
type copilotToolExecComplete struct {
	ToolCallID    string             `json:"toolCallId"`
	Model         string             `json:"model"`
	InteractionID string             `json:"interactionId"`
	Success       bool               `json:"success"`
	Result        *copilotToolResult `json:"result,omitempty"`
	Error         *copilotToolError  `json:"error,omitempty"`
}

type copilotToolResult struct {
	Content         string `json:"content"`
	DetailedContent string `json:"detailedContent,omitempty"`
}

type copilotToolError struct {
	Message string `json:"message"`
}

// copilotReasoning is data payload for "assistant.reasoning" / "assistant.reasoning_delta".
type copilotReasoning struct {
	Content      string `json:"content,omitempty"`
	DeltaContent string `json:"deltaContent,omitempty"`
}

// copilotSessionError is data payload for "session.error".
type copilotSessionError struct {
	ErrorType string `json:"errorType"`
	Message   string `json:"message"`
}

// copilotSessionWarning is data payload for "session.warning".
type copilotSessionWarning struct {
	WarningType string `json:"warningType"`
	Message     string `json:"message"`
}

// copilotResultUsage is the usage on the final "result" line.
type copilotResultUsage struct {
	PremiumRequests    float64             `json:"premiumRequests"`
	TotalAPIDurationMs int64               `json:"totalApiDurationMs"`
	SessionDurationMs  int64               `json:"sessionDurationMs"`
	CodeChanges        *copilotCodeChanges `json:"codeChanges,omitempty"`
}

type copilotCodeChanges struct {
	LinesAdded    int      `json:"linesAdded"`
	LinesRemoved  int      `json:"linesRemoved"`
	FilesModified []string `json:"filesModified"`
}

// ── Arg builder ──

// copilotBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var copilotBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedWithValue,
	"--output-format":   blockedWithValue,
	"--allow-all":       blockedStandalone, // tools + paths + URLs
	"--allow-all-tools": blockedStandalone,
	"--allow-all-paths": blockedStandalone,
	"--allow-all-urls":  blockedStandalone,
	"--yolo":            blockedStandalone,
	"--no-ask-user":     blockedStandalone,
	"--resume":          blockedWithValue,  // managed via ExecOptions.ResumeSessionID
	"--acp":             blockedStandalone, // prevent switching to ACP mode
}

// buildCopilotArgs assembles the argv for a one-shot copilot invocation.
//
//	copilot -p "<prompt>" --output-format json --allow-all --no-ask-user
//	        [--resume <session-id>] [--model <model>]
func buildCopilotArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--allow-all", // tools + paths + URLs — full headless mode
		"--no-ask-user",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, copilotBlockedArgs, logger)...)
	return args
}
