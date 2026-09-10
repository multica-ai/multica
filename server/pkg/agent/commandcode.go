package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// commandcodeBackend drives the CommandCode CLI's native NDJSON protocol: `cmd`
// with `--output-format json`, prompt on stdin. The event schema is based on
// CommandCode CLI v1.50.1 captures in research-captures/*.ndjson.
//
// The protocol wraps every streaming event inside an outer envelope: N lines of
// `{"type":"event","event":{...}}` followed by ONE final
// `{"type":"result",...}` line. The inner event carries its own `type`
// (run_start, thinking_delta, text_delta, tool_queued, ...); the parser here
// unwraps the envelope and dispatches on the inner type.
type commandcodeBackend struct {
	cfg Config
}

// commandcodeBlockedArgs are owned by Multica. CommandCode accepts the task
// prompt and the stream protocol as flags, so custom args must not replace
// either. Model/session are also selected by Multica, and the remaining flags
// are daemon-owned permission/behavior flags; users may not disable YOLO mode,
// change the output format, or narrow the tool registry from custom_args.
var commandcodeBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,
	"--print":                        blockedWithValue,
	"--output-format":                blockedWithValue,
	"-m":                             blockedWithValue,
	"--model":                        blockedWithValue,
	"-r":                             blockedWithValue,
	"--resume":                       blockedWithValue,
	"-c":                             blockedStandalone,
	"--continue":                     blockedStandalone,
	"--fork-session":                 blockedWithValue,
	"--session":                      blockedWithValue,
	"--no-auto-update":               blockedStandalone,
	"--permission-mode":              blockedWithValue,
	"--auto-accept":                  blockedStandalone,
	"--yolo":                         blockedStandalone,
	"--dangerously-skip-permissions": blockedStandalone,
	"--plan":                         blockedStandalone,
	"--no-skills":                    blockedStandalone,
	"--skip-onboarding":              blockedStandalone,
	"--tools-all":                    blockedStandalone,
	"--tools-enable":                 blockedWithValue,
}

// buildCommandCodeArgs assembles the argv for a one-shot CommandCode invocation.
//
// The prompt is deliberately NOT part of argv. CommandCode's headless mode
// reads a non-interactive prompt from stdin when none is given via -p, and
// putting the (arbitrarily large, user-influenced) prompt text on the command
// line is not safe on Windows: even after routing a .cmd shim through its
// PowerShell counterpart (commandcode_invocation_windows.go), PowerShell's own
// argument re-serialisation does not survive a value containing embedded double
// quotes — the same class of failure cursor-agent hit before it moved its
// prompt to stdin (#5649). This is that fix's CommandCode equivalent (#6082).
// Only fixed, content-free flags remain in argv; the prompt goes on stdin.
//
// --print (since CommandCode 1.52.0) is what selects non-interactive mode when
// the prompt arrives on stdin. Before 1.52.0 stdin alone was enough; from
// 1.52.0 the CLI aborts with "Interactive mode requires a TTY terminal" unless
// a prompting flag is present. It is argv-only and carries no value, so it
// keeps the Windows argument-serialisation guarantee above intact.
func buildCommandCodeArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"--output-format", "json", "--no-auto-update", "--print"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	// --yolo is daemon-owned: CommandCode's non-interactive mode filters out
	// approval-requiring tools unless bypass mode is active. All other adapters
	// use an equivalent mechanism; this keeps CommandCode headless runs
	// consistent with the rest of the runtime set.
	args = append(args, "--yolo")
	args = append(args, filterCustomArgs(opts.ExtraArgs, commandcodeBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, commandcodeBlockedArgs, logger)...)
	return args
}

func (b *commandcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = "cmd"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("commandcode executable not found at %q: %w", execName, err)
	}
	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildCommandCodeArgs(opts, b.cfg.Logger)

	cmd, _, _ := b.cfg.commandAt(execName).execVia(runCtx, chooseCommandCodeInvocation, lookedUp, args, b.cfg.Logger)
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
		return nil, fmt.Errorf("commandcode stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("commandcode stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[cmd:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start commandcode: %w", err)
	}
	b.cfg.Logger.Info("commandcode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	// The prompt is delivered on stdin (see buildCommandCodeArgs). Write it from
	// its own goroutine so it cannot deadlock against the stdout reader below:
	// a prompt larger than the OS pipe buffer blocks mid-write until the child
	// drains it, and the child cannot drain while we are not yet reading its
	// stdout. Closing stdin is what signals end-of-prompt — cmd reads to EOF —
	// so we always close, on both the success and error paths.
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		started := time.Now()
		state := commandcodeStreamState{model: opts.Model, usage: make(map[string]TokenUsage)}
		go func() {
			<-runCtx.Done()
			// Closing stdin releases a prompt write still blocked on a full pipe
			// (e.g. the child died before draining it), so that goroutine cannot
			// leak.
			closeStdin()
			_ = stdout.Close()
		}()

		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			raw, ok := parseCommandCodeLine(line)
			if !ok {
				state.invalidEventCount++
				continue
			}
			state.eventCount++
			handleCommandCodePayload(raw, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(started)

		// Wait has already closed the stdin pipe, so a prompt write still
		// blocked on a full pipe has returned by now; the writer sends exactly
		// once.
		writeErr := <-writeErrCh

		status, output, errMsg := finalizeStreamResult("commandcode", timeout, runCtx.Err(), writeErr, exitErr, state.sessionID, streamTerminalState{
			lastAssistantText: state.lastAssistantText,
			finalResultText:   state.finalResultText,
			sawResult:         state.sawResult,
			resultIsError:     state.resultIsError,
			scanErr:           scanErr,
		}, "")
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "commandcode", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider: "commandcode", cliVersion: b.cfg.CLIVersion, model: state.model,
			exitCode: streamProcessExitCode(exitErr), eventCount: state.eventCount,
			invalidEventCount: state.invalidEventCount, assistantEventCount: state.assistantEventCount,
			toolUseCount: state.toolUseCount, sawResult: state.sawResult, resultIsError: state.resultIsError,
			resultBytes: len(state.finalResultText), lastAssistantBytes: len(state.lastAssistantText),
			scannerError: scanErr != nil, lastEventType: state.lastEventType,
			unhandledEventTypeCount: state.unhandledEventTypeCount, unhandledEventTypes: state.unhandledEventTypes,
			unhandledSubtypeCount: state.unhandledSubtypeCount, unreadableAssistantCount: 0,
		})
		b.cfg.Logger.Info("commandcode finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status: status, Output: output, Error: errMsg, DurationMs: duration.Milliseconds(),
			SessionID:      resolveSessionID(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg),
			Usage:          state.usage,
			ResumeRejected: resumeWasRejected(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg),
		}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

// commandcodePayload is the unwrapped envelope. CommandCode emits two outer
// shapes that share one wire: `{"type":"event","event":{...}}` carries a
// streaming inner event in Event; `{"type":"result",...}` carries the terminal
// result flat (Subtype, SessionID, Usage, FinalText, ...).
type commandcodePayload struct {
	Type       string            `json:"type"`
	Event      json.RawMessage   `json:"event,omitempty"`
	Subtype    string            `json:"subtype,omitempty"`
	SessionID  string            `json:"sessionId,omitempty"`
	StopReason string            `json:"stopReason,omitempty"`
	Usage      *commandcodeUsage `json:"usage,omitempty"`
	DurationMs int64             `json:"durationMs,omitempty"`
	FinalText  string            `json:"finalText,omitempty"`
}

// commandcodeInnerEvent is the unwrapped `event` object. Its own `type` selects
// the handler. Fields are a superset of the observed vocabulary; unrecognized
// types are counted, never fatal.
type commandcodeInnerEvent struct {
	Type         string            `json:"type"`
	SessionID    string            `json:"sessionId,omitempty"`
	TurnNumber   int               `json:"turnNumber,omitempty"`
	HadToolCalls bool              `json:"hadToolCalls,omitempty"`
	Model        string            `json:"model,omitempty"`
	StopReason   string            `json:"stopReason,omitempty"`
	Usage        *commandcodeUsage `json:"usage,omitempty"`
	TraceID      string            `json:"traceId,omitempty"`
	ToolCallID   string            `json:"toolCallId,omitempty"`
	ToolName     string            `json:"toolName,omitempty"`
	Description  string            `json:"description,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	Result       json.RawMessage   `json:"result,omitempty"`
	Deferred     bool              `json:"deferred,omitempty"`
	Delta        string            `json:"delta,omitempty"`
	Text         string            `json:"text,omitempty"`
	Content      json.RawMessage   `json:"content,omitempty"`
}

type commandcodeUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

// parseCommandCodeLine parses one NDJSON line into the unwrapped envelope.
// It returns false for lines that are not valid JSON objects with a `type`
// field — that is how the auto-update junk line (`Updated 1.50.0 → 1.50.1`) and
// any other non-JSON stdout pollution is tolerated without breaking the stream.
func parseCommandCodeLine(line string) (commandcodePayload, bool) {
	var payload commandcodePayload
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return commandcodePayload{}, false
	}
	if payload.Type == "" {
		return commandcodePayload{}, false
	}
	return payload, true
}

func handleCommandCodePayload(payload commandcodePayload, ch chan<- Message, state *commandcodeStreamState) {
	if payload.Type == "result" {
		handleCommandCodeResult(payload, state)
		return
	}
	if payload.Type != "event" {
		state.unhandledEventTypeCount++
		state.unhandledEventTypes = appendUnhandledType(state.unhandledEventTypes, payload.Type)
		return
	}
	var inner commandcodeInnerEvent
	if err := json.Unmarshal(payload.Event, &inner); err != nil {
		state.unhandledSubtypeCount++
		return
	}
	state.lastEventType = inner.Type
	handleCommandCodeEvent(inner, ch, state)
}

func handleCommandCodeResult(payload commandcodePayload, state *commandcodeStreamState) {
	state.sawResult = true
	state.resultIsError = payload.Subtype != "" && payload.Subtype != "success"
	if payload.SessionID != "" {
		state.sessionID = payload.SessionID
	}
	state.stopReason = payload.StopReason
	state.finalResultText = payload.FinalText
	if usage := payload.Usage; usage != nil && (usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 0) {
		// The result usage is the authoritative aggregate for the whole run;
		// key it by the last-seen model (empty when the stream carried none).
		// A zero record (e.g. an error result) must not clobber per-model usage
		// accumulated from model_request_end frames.
		state.usage[state.model] = commandcodeTokenUsage(usage)
	}
}

func handleCommandCodeEvent(event commandcodeInnerEvent, ch chan<- Message, state *commandcodeStreamState) {
	switch event.Type {
	case "run_start":
		if event.SessionID != "" {
			state.sessionID = event.SessionID
		}
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: state.sessionID})
	case "turn_start", "turn_end":
		if event.Model != "" {
			state.model = event.Model
		}
		if event.Usage != nil && event.Model != "" {
			state.usage[event.Model] = commandcodeTokenUsage(event.Usage)
		}
	case "message_start", "message_update", "message_end":
		// Content blocks are synthesized from the finer-grained
		// thinking_* / text_delta / tool_* stream below; the message envelope
		// is redundant and deliberately not re-parsed here.
	case "thinking_start":
		state.thinking.WriteString(event.Delta)
	case "thinking_delta":
		state.thinking.WriteString(event.Delta)
		if thinking := state.thinking.String(); thinking != "" {
			trySend(ch, Message{Type: MessageThinking, Content: thinking})
		}
	case "thinking_end":
		if event.Text != "" {
			state.thinking.Reset()
			state.thinking.WriteString(event.Text)
		}
		if thinking := state.thinking.String(); thinking != "" {
			trySend(ch, Message{Type: MessageThinking, Content: thinking})
		}
		state.thinking.Reset()
	case "text_delta":
		if event.Delta != "" {
			state.lastAssistantText += event.Delta
			trySend(ch, Message{Type: MessageText, Content: event.Delta})
		}
	case "model_trace":
		// Opaque trace id for model-router diagnostics; not rendered.
	case "model_request_start":
		if event.Model != "" {
			state.model = event.Model
		}
	case "model_request_end":
		if event.Model != "" {
			state.model = event.Model
		}
		if event.Usage != nil && event.Model != "" {
			state.usage[event.Model] = commandcodeTokenUsage(event.Usage)
		}
	case "tool_queued":
		state.toolUseCount++
		var input map[string]any
		if len(event.Input) > 0 {
			_ = json.Unmarshal(event.Input, &input)
		}
		trySend(ch, Message{Type: MessageToolUse, Tool: event.ToolName, CallID: event.ToolCallID, Input: input})
	case "tool_running":
		// Tool execution in progress; the queued/completed pair brackets the
		// work, so the running frame is not rendered separately.
	case "tool_completed":
		trySend(ch, Message{Type: MessageToolResult, CallID: event.ToolCallID, Output: commandcodeToolResultText(event.Result)})
	case "run_end":
		// Terminal summary frame; the result line carries the canonical final
		// text and usage, so this is not rendered separately.
	default:
		state.unhandledEventTypeCount++
		state.unhandledEventTypes = appendUnhandledType(state.unhandledEventTypes, event.Type)
	}
}

func commandcodeTokenUsage(usage *commandcodeUsage) TokenUsage {
	return TokenUsage{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	}
}

func commandcodeToolResultText(raw json.RawMessage) string {
	var blocks []commandcodeToolResultBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw)
	}
	var sb strings.Builder
	for i, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(block.Text)
		}
	}
	if sb.Len() == 0 {
		return string(raw)
	}
	return sb.String()
}

type commandcodeToolResultBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type commandcodeStreamState struct {
	sessionID, model, lastAssistantText, finalResultText, lastEventType, stopReason string
	thinking                                                                        strings.Builder
	sawResult, resultIsError                                                        bool
	usage                                                                           map[string]TokenUsage
	eventCount, invalidEventCount, assistantEventCount, toolUseCount                int
	unhandledEventTypeCount, unhandledSubtypeCount                                  int
	unhandledEventTypes                                                             string
}

func appendUnhandledType(types, addition string) string {
	if types == "" {
		return addition
	}
	return types + "," + addition
}

// commandcodeModelLine matches a model entry in `cmd --list-models` output.
// Each model line is "<id>  <description>" where the id and description are
// separated by two or more spaces (the human table is column-aligned). Section
// headers ("Open Source", "Anthropic", ...) and the footer are single short
// lines that do not match this shape, so they are skipped automatically.
var commandcodeModelLine = regexp.MustCompile(`^(\S+)\s{2,}(.+)$`)

// discoverCommandCodeModels runs `cmd --list-models` and parses the
// human-readable table into a model catalog. CommandCode prints a text table,
// not JSON — there is no structured catalog endpoint — so we scrape the first
// whitespace-separated token of each aligned line as the model id. A failure at
// any step (binary missing, non-zero exit, unparseable output) degrades to an
// empty catalog so the UI falls back to manual model entry rather than erroring.
func discoverCommandCodeModels(ctx context.Context, runtimeCmd Command) ([]Model, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "cmd"
	}
	if _, err := exec.LookPath(runtimeCmd.Path); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := runtimeCmd.exec(runCtx, "--list-models", "--no-auto-update")
	hideAgentWindow(cmd)
	stdout, err := outputOwned(cmd, runtimeCmd.logger)
	if err != nil || len(stdout) == 0 {
		return []Model{}, nil
	}
	return parseCommandCodeModels(stdout), nil
}

func parseCommandCodeModels(stdout []byte) []Model {
	seen := map[string]bool{}
	var models []Model
	for _, line := range strings.Split(string(stdout), "\n") {
		matches := commandcodeModelLine.FindStringSubmatch(strings.TrimSpace(line))
		if matches == nil {
			continue
		}
		id := strings.TrimSpace(matches[1])
		if id == "" || seen[id] {
			continue
		}
		// Skip the footer line ("Pass the full id, ...") — its first token is
		// not a model id and it has no description column, but guard anyway.
		if strings.HasPrefix(id, "Pass") || strings.HasPrefix(id, "cmd") || strings.HasPrefix(id, "Docs") {
			continue
		}
		seen[id] = true
		models = append(models, Model{ID: id})
	}
	return models
}
