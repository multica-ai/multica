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
	// output holds the latest COMPLETE assistant turn, not every turn joined:
	// Result.Output is "final user-facing output selected by the backend"
	// (agent.go), and a tool-using run's earlier turns are narration around the
	// work, not the deliverable (GH #6006). Every turn still reaches the
	// transcript through the emitted MessageText.
	output strings.Builder
	// pendingDelta buffers the turn currently streaming. It is cleared when that
	// turn's authoritative assistant.message lands, so it is non-empty only when
	// the process died mid-turn — the one case where a partial beats the
	// previous turn's complete text.
	pendingDelta strings.Builder
	sessionID    string
	activeModel  string
	finalStatus  string
	finalError   string

	// Token usage arrives on up to three different events, so each source is
	// accumulated separately and resolved once at the end by resolveUsage.
	//
	//	session.shutdown   per-model session totals, full breakdown
	//	assistant.usage    per model call, full breakdown
	//	assistant.message  outputTokens only — legacy, older CLIs
	//
	// They must never be summed together: all three describe the same tokens.
	callUsage     map[string]TokenUsage
	msgUsage      map[string]TokenUsage
	shutdownUsage map[string]TokenUsage
	// resumed marks a run that continued an existing Copilot session, which is
	// what disqualifies the session.shutdown totals — see resolveUsage.
	resumed bool
}

// resolveUsage picks the single best usage source this run produced, most
// complete first.
//
// On a fresh session, session.shutdown is the CLI's own final accounting for
// the whole session — which here IS this run — so it is the most complete
// source available and cannot be short-changed by an individual model call
// that failed to report. It is unusable on a resumed run: the CLI restores its
// accumulators from a checkpoint, so it also carries every earlier turn's
// tokens, and reporting that on a follow-up would bill the same tokens once per
// turn. Under-reporting beats double-billing, so a resumed run falls through.
//
// assistant.usage is per model call, so it measures exactly this run either
// way. assistant.message is last: it only ever carries output tokens, so
// letting it win over a source with the full breakdown would silently drop the
// input and cache tiers.
func (st *copilotEventState) resolveUsage() map[string]TokenUsage {
	if !st.resumed && hasTokens(st.shutdownUsage) {
		return st.shutdownUsage
	}
	if hasTokens(st.callUsage) {
		return st.callUsage
	}
	if hasTokens(st.msgUsage) {
		return st.msgUsage
	}
	return map[string]TokenUsage{}
}

// hasTokens reports whether a source carries any real numbers. Presence alone
// is not enough: every token field on assistant.usage is optional upstream, so
// a usage event that names only a model would otherwise mark that source
// "populated" and shadow one that actually has tokens.
func hasTokens(usage map[string]TokenUsage) bool {
	for _, u := range usage {
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			return true
		}
	}
	return false
}

// addUsage folds one usage record into dst under model, ignoring records that
// carry no tokens at all so an empty one never creates an entry.
//
// Copilot reports cached tokens INSIDE inputTokens (its own event-log reducer
// derives uncached input as inputTokens - cacheRead - cacheWrite), whereas
// TokenUsage.InputTokens is the uncached remainder that prices at the input
// rate. Subtract here so the cache tiers are not billed twice. reasoningTokens
// are deliberately ignored: they are already part of outputTokens.
func addUsage(dst map[string]TokenUsage, model string, input, output, cacheRead, cacheWrite int64) {
	cacheRead = nonNegativeTokens(cacheRead)
	cacheWrite = nonNegativeTokens(cacheWrite)
	output = nonNegativeTokens(output)
	uncachedInput := nonNegativeTokens(input - cacheRead - cacheWrite)
	if uncachedInput == 0 && output == 0 && cacheRead == 0 && cacheWrite == 0 {
		return
	}
	u := dst[model]
	u.InputTokens += uncachedInput
	u.OutputTokens += output
	u.CacheReadTokens += cacheRead
	u.CacheWriteTokens += cacheWrite
	dst[model] = u
}

func nonNegativeTokens(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

// finalOutput is the deliverable for Result.Output: the last complete assistant
// turn, or the partial turn that was still streaming when the process died.
func (st *copilotEventState) finalOutput() string {
	if st.pendingDelta.Len() > 0 {
		return st.pendingDelta.String()
	}
	return st.output.String()
}

func newCopilotEventState(seedModel string, resumed bool) *copilotEventState {
	return &copilotEventState{
		activeModel:   seedModel,
		finalStatus:   "completed",
		callUsage:     make(map[string]TokenUsage),
		msgUsage:      make(map[string]TokenUsage),
		shutdownUsage: make(map[string]TokenUsage),
		resumed:       resumed,
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
			// Buffer deltas as defense-in-depth: if the process is killed before
			// this turn's assistant.message arrives, we still have its text.
			st.pendingDelta.WriteString(delta.DeltaContent)
			msgs = append(msgs, Message{Type: MessageText, Content: delta.DeltaContent})
		}

	case "assistant.message":
		var msg copilotAssistantMessage
		if err := json.Unmarshal(evt.Data, &msg); err != nil {
			return nil
		}
		// assistant.message carries the full turn content and supersedes both the
		// deltas that streamed it and whatever earlier turn output held: a run
		// that narrates, calls a tool, then answers must deliver the answer, not
		// the pair joined together.
		if msg.Content != "" {
			st.output.Reset()
			st.output.WriteString(msg.Content)
		}
		// Clear unconditionally — this event IS the turn boundary. A tool-only
		// turn reports content:"" (the toolRequests below are the whole turn), and
		// leaving its deltas buffered would let them be stitched onto the NEXT
		// turn's partial text if the process then died mid-stream.
		st.pendingDelta.Reset()
		// The message names the model that produced it. Without this the first
		// turn's tokens land under the seed model — the literal string
		// "copilot" when no model was configured — which no price table maps,
		// so a run that DID report tokens still estimated $0.00.
		if msg.Model != "" {
			st.activeModel = msg.Model
		}
		if msg.ReasoningText != "" {
			msgs = append(msgs, Message{Type: MessageThinking, Content: msg.ReasoningText})
		}
		if msg.OutputTokens > 0 {
			addUsage(st.msgUsage, st.activeModel, 0, msg.OutputTokens, 0, 0)
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

	case "assistant.usage":
		// One record per model API call, with the full token breakdown. This is
		// the only event that reports input and cache tokens at all.
		var u copilotUsageData
		if err := json.Unmarshal(evt.Data, &u); err != nil {
			return nil
		}
		model := u.Model
		// The CLI writes "unknown" when it cannot name the model; keep whatever
		// the session already resolved rather than opening a bogus usage row.
		if model == "" || model == "unknown" {
			model = st.activeModel
		} else {
			st.activeModel = model
		}
		addUsage(st.callUsage, model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)

	case "session.shutdown":
		// Session-wide per-model totals, emitted once as the session tears down.
		var sd copilotShutdownData
		if err := json.Unmarshal(evt.Data, &sd); err != nil {
			return nil
		}
		for model, m := range sd.ModelMetrics {
			if model == "" {
				model = st.activeModel
			}
			addUsage(st.shutdownUsage, model, m.Usage.InputTokens, m.Usage.OutputTokens, m.Usage.CacheReadTokens, m.Usage.CacheWriteTokens)
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
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "copilot"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("copilot executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildCopilotArgs(prompt, opts, b.cfg.Logger)
	cmd, argv0, cmdArgs := b.cfg.commandAt(execName).execVia(runCtx, chooseCopilotInvocation, lookedUp, args, b.cfg.Logger)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", argv0, "args", cmdArgs)
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
		st := newCopilotEventState(seedModel, opts.ResumeSessionID != "")

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

		usage := st.resolveUsage()
		// A run that produced output but no tokens is a silent billing hole:
		// the daemon skips reporting empty usage, so nothing surfaces anywhere
		// downstream. This is the current state on Copilot CLI 1.0.77 (see
		// copilotUsageData), and it went unnoticed until a user reported it —
		// log it so the next regression is visible from the daemon log alone.
		if len(usage) == 0 && st.finalStatus == "completed" {
			b.cfg.Logger.Warn("copilot reported no token usage",
				"session", st.sessionID,
				"model", st.activeModel,
				"hint", "Copilot CLI filters assistant.usage and session.shutdown out of --output-format json; only assistant.message.outputTokens remains, and newer CLIs no longer populate it")
		}

		resCh <- Result{
			Status:     st.finalStatus,
			Output:     st.finalOutput(),
			Error:      st.finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  st.sessionID,
			Usage:      usage,
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
//
// OutputTokens is optional in the CLI's own event schema and is the ONLY token
// field that survives onto the `--output-format json` stream — see the
// suppression note on copilotUsageData.
type copilotAssistantMessage struct {
	MessageID     string               `json:"messageId"`
	Model         string               `json:"model"`
	Content       string               `json:"content"`
	ToolRequests  []copilotToolRequest `json:"toolRequests"`
	OutputTokens  int64                `json:"outputTokens"`
	InteractionID string               `json:"interactionId"`
	ReasoningText string               `json:"reasoningText,omitempty"`
}

// copilotUsageData is data payload for "assistant.usage": one record per model
// API call, carrying the only complete token breakdown the CLI produces.
//
// InputTokens INCLUDES the cached tiers; addUsage subtracts them back out. The
// payload also carries reasoningTokens, deliberately not parsed here: those are
// a subset of outputTokens, so counting them would double-bill reasoning.
//
// NOTE (Copilot CLI 1.0.77): the CLI's JSONL writer drops a fixed set of event
// types before writing stdout, and both "assistant.usage" and
// "session.shutdown" are on that list — so on a current CLI neither reaches us
// and Copilot runs report no tokens at all. Everything here is still parsed
// because older CLIs, and any future build that stops filtering them, deliver
// real numbers through exactly these events. The `result` event we already
// parse carries premiumRequests and durations but no token counts.
type copilotUsageData struct {
	Model            string `json:"model"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	CacheReadTokens  int64  `json:"cacheReadTokens"`
	CacheWriteTokens int64  `json:"cacheWriteTokens"`
}

// copilotShutdownData is data payload for "session.shutdown": session-wide
// totals keyed by model.
type copilotShutdownData struct {
	ModelMetrics map[string]copilotShutdownModelMetric `json:"modelMetrics"`
}

type copilotShutdownModelMetric struct {
	Usage copilotUsageData `json:"usage"`
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
