package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// commandcodeTerminateGraceNanos optionally overrides, in nanoseconds, how long
// a cancelled Command Code process is given to exit after SIGTERM before it
// (and its whole process group) is SIGKILLed. Zero means use the default. It is
// atomic so tests can shorten the grace without racing the cancellation
// goroutine that reads it.
var commandcodeTerminateGraceNanos atomic.Int64

func commandcodeTerminateGrace() time.Duration {
	if n := commandcodeTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

// commandcodeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Letting any of these through
// would break the daemon's own channel: a custom `--output-format text` mutes
// the event stream the scanner reads, and a second `--session` silently
// retargets the run at another transcript.
var commandcodeBlockedArgs = map[string]blockedArgMode{
	"--output-format":                blockedWithValue,     // NDJSON event stream the daemon parses
	"--print":                        blockedOptionalValue, // headless mode; alias of -p
	"-p":                             blockedOptionalValue,
	"--session":                      blockedWithValue,  // daemon owns resume targeting
	"--no-session":                   blockedStandalone, // daemon needs the transcript for resume
	"--resume":                       blockedOptionalValue,
	"--continue":                     blockedStandalone,
	"--model":                        blockedWithValue, // owned by agent.model
	"-m":                             blockedWithValue,
	"--effort":                       blockedWithValue,  // owned by agent.thinking_level
	"--permission-mode":              blockedWithValue,  // daemon runs non-interactive
	"--yolo":                         blockedStandalone, // already implied by the daemon invocation
	"--dangerously-skip-permissions": blockedStandalone,
	"--max-turns":                    blockedWithValue, // owned by ExecOptions.MaxTurns
}

// commandcodeCapHitExitCode is the exit status Command Code uses when a
// headless run stops because --max-turns was reached. The run produced real
// work up to that point, so it is a bounded stop rather than a crash and must
// not be reported as a process failure.
const commandcodeCapHitExitCode = 8

// commandcodeBackend implements Backend by spawning
// `commandcode -p --output-format json` and reading the NDJSON event stream
// from stdout.
//
// Command Code speaks its own event protocol — neither Claude Code's
// stream-json, nor pi's `--mode json`, nor ACP. Every line is one JSON object:
// either {"type":"event","event":{...}} carrying a lifecycle event, or the
// single trailing {"type":"result",...} line that closes the run. The event
// envelope is uniform, so the scanner unwraps once and dispatches on the inner
// type.
//
// One protocol trait shapes the whole backend: Command Code does not stream
// assistant text token by token. A turn's text arrives whole, inside
// `message_end.content`, as Anthropic-shaped blocks. The daemon therefore
// surfaces a turn's prose when the turn closes rather than as it is typed;
// tool activity, by contrast, does stream (tool_running fires before the tool
// executes). See processEvents for the mapping.
type commandcodeBackend struct {
	cfg Config
}

func (b *commandcodeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "commandcode"
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("commandcode executable not found at %q: %w", execPath, err)
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// -p with no positional query makes Command Code read the run message from
	// stdin, which is how the prompt is delivered here (see the writer
	// goroutine below). --skip-onboarding suppresses the interactive taste
	// onboarding a fresh install would otherwise block on, and
	// --no-auto-update keeps a daemon run from swapping the binary mid-flight.
	args := []string{
		"-p",
		"--output-format", "json",
		"--yolo",
		"--skip-onboarding",
		"--no-auto-update",
		"--trust",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--effort", opts.ThinkingLevel)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	if opts.ThreadName != "" {
		args = append(args, "--name", opts.ThreadName)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, commandcodeBlockedArgs, b.cfg.Logger)...)
	// SystemPrompt is deliberately not forwarded: Command Code has no
	// --system-prompt flag, and the daemon already delivers the runtime brief
	// as a per-task context file in the workdir.

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	// Take over context cancellation so the whole process tree gets a graceful
	// SIGTERM→SIGKILL before the stdout read end is closed. Closing the pipe
	// first would leave the child writing into a closed pipe and spinning on
	// EPIPE. Returning nil keeps os/exec from racing us with its own kill.
	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommandWithPrompt(cmd, newAgentCommandLogArgs(args), len(prompt))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	if opts.Cwd != "" {
		// Command Code resolves its project root — skill discovery and the
		// AGENTS.md walk-up — from PWD before falling back to process.cwd(),
		// so cmd.Dir alone would let a daemon's own working directory leak in.
		env = append(env, "PWD="+opts.Cwd)
	}
	cmd.Env = env

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
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[commandcode:stderr] ")

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start commandcode: %w", err)
	}

	b.cfg.Logger.Info("commandcode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead pid.
	procDone := make(chan struct{})

	// Write the prompt from its own goroutine so it cannot deadlock against the
	// stdout reader: a prompt larger than the OS pipe buffer blocks mid-write
	// until the child drains it, and the child cannot drain while nobody reads
	// its stdout. Closing stdin is what ends the prompt — Command Code reads
	// stdin to EOF before starting the run. Keeping the prompt off argv also
	// sidesteps the Windows CreateProcess command-line cap and keeps it out of
	// OS process listings.
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	go func() {
		select {
		case <-procDone:
			return // finished on its own; nothing to terminate
		case <-runCtx.Done():
		}
		// Release a prompt write still blocked on a full stdin pipe.
		closeStdin()
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGTERM)
			select {
			case <-procDone: // exited within the grace window
			case <-time.After(commandcodeTerminateGrace()):
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

		exitErr := cmd.Wait()
		close(procDone)
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		// Wait closes the process pipes, so a prompt write still blocked when
		// the child exited has returned by now. The writer sends exactly once.
		writeErr := <-writeErrCh

		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("commandcode timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		case exitErr != nil && commandcodeExitCode(exitErr) == commandcodeCapHitExitCode && scanResult.sawResultLine:
			// --max-turns was reached. The run stopped on a bound the daemon
			// itself set and still produced a result line, so it completed as
			// specified rather than failing.
			b.cfg.Logger.Info("commandcode stopped at the max-turns cap", "maxTurns", opts.MaxTurns)
		case exitErr != nil && scanResult.status == "completed":
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("commandcode exited with error: %v", exitErr)
		case exitErr != nil && scanResult.status == "failed" && scanResult.errMsg != "":
			scanResult.errMsg = fmt.Sprintf("%s; commandcode exited with error: %v", scanResult.errMsg, exitErr)
		case writeErr != nil && !scanResult.sawResultLine:
			// A failed prompt write is only benign once the run is PROVEN to
			// have finished. Command Code reads stdin to EOF before it does any
			// work, so a run that emitted its result line necessarily received
			// the whole prompt and a late EPIPE just means the pipe closed on
			// the way out. Absence of failure is not that proof: a child that
			// emits nothing and exits 0 would otherwise pass as a clean success
			// even though the prompt never landed.
			if scanResult.errMsg == "" {
				scanResult.errMsg = fmt.Sprintf("commandcode prompt write failed: %v", writeErr)
			} else {
				scanResult.errMsg = fmt.Sprintf("%s; commandcode prompt write failed: %v", scanResult.errMsg, writeErr)
			}
			scanResult.status = "failed"
		}

		b.cfg.Logger.Info("commandcode finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      scanResult.usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// commandcodeExitCode extracts the process exit status from a cmd.Wait error,
// or -1 when the error is not an exit status (start failure, signal kill).
func commandcodeExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// ── Event stream ──

// commandcodeScanResult holds the accumulated state from processing the event
// stream.
type commandcodeScanResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     map[string]TokenUsage
	// sawResultLine is positive evidence that the run reached its own
	// terminator: Command Code emits exactly one {"type":"result"} line and it
	// is the last thing it writes. It is what proves the prompt was consumed
	// and the run finished, as opposed to a stream that simply stopped.
	sawResultLine bool
}

// commandcodeLine is the NDJSON envelope. Exactly one of Event or the result
// fields is populated, selected by Type.
type commandcodeLine struct {
	Type  string            `json:"type"`
	Event *commandcodeEvent `json:"event"`

	// Result-line fields, present only when Type == "result".
	Subtype    string            `json:"subtype"`
	SessionID  string            `json:"sessionId"`
	StopReason string            `json:"stopReason"`
	Usage      *commandcodeUsage `json:"usage"`
	DurationMs int64             `json:"durationMs"`
	FinalText  string            `json:"finalText"`
	Error      *commandcodeError `json:"error"`
}

// commandcodeEvent is the inner event object. Fields are a union across every
// event type; Type selects which are meaningful.
type commandcodeEvent struct {
	Type string `json:"type"`

	SessionID  string `json:"sessionId"`
	TurnNumber int    `json:"turnNumber"`

	// model_request_start / model_request_end
	Model      string            `json:"model"`
	StopReason string            `json:"stopReason"`
	Usage      *commandcodeUsage `json:"usage"`

	// message_end
	Content []commandcodeBlock `json:"content"`

	// tool_running / tool_completed / tool_errored / tool_denied
	ToolCallID  string          `json:"toolCallId"`
	ToolName    string          `json:"toolName"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Reason      string          `json:"reason"`

	// notice / mod_error / run_error
	Message string            `json:"message"`
	Error   *commandcodeError `json:"error"`
}

// commandcodeBlock is one content block of an assistant message. The shape
// mirrors Anthropic's: text, reasoning (Command Code's name for thinking), and
// tool_use.
type commandcodeBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
	// ProviderExecuted marks a tool the provider ran server-side. Those never
	// reach the client as tool calls, so they are skipped rather than surfaced
	// as agent tool activity.
	ProviderExecuted bool `json:"providerExecuted"`
}

type commandcodeUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

type commandcodeError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (e *commandcodeError) String() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Name != "" && e.Message != "":
		return e.Name + ": " + e.Message
	case e.Message != "":
		return e.Message
	default:
		return e.Name
	}
}

// processEvents reads NDJSON lines from r, dispatches them to ch, and returns
// the accumulated result. This is the core scanner loop, extracted for
// testability.
func (b *commandcodeBackend) processEvents(r io.Reader, ch chan<- Message) commandcodeScanResult {
	res := commandcodeScanResult{status: "completed"}

	scanner := bufio.NewScanner(r)
	// Tool results and whole-turn message content routinely exceed the default
	// 64 KiB token; a single file-read result can be far larger.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	// currentModel tracks the model of the in-flight request so usage reported
	// at model_request_end is attributed to the model that actually incurred
	// it. A run that switches models mid-way therefore bills each correctly,
	// rather than folding everything under the configured model.
	currentModel := ""

	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var line commandcodeLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			// A non-JSON line is diagnostic noise from a wrapper script, not a
			// protocol violation worth failing the run over.
			b.cfg.Logger.Debug("commandcode: skipping non-JSON stdout line", "line", truncateUTF8(raw, 256))
			continue
		}

		switch line.Type {
		case "event":
			if line.Event != nil {
				b.handleEvent(*line.Event, ch, &res, &currentModel)
			}
		case "result":
			res.sawResultLine = true
			res.output = line.FinalText
			if line.SessionID != "" {
				res.sessionID = line.SessionID
			}
			if line.Error != nil {
				res.status = "failed"
				if res.errMsg == "" {
					res.errMsg = line.Error.String()
				}
			}
			// subtype names how the run ended. Anything other than a success
			// subtype is a failure the result line is reporting on its own.
			switch line.Subtype {
			case "success", "":
			case "interrupted", "cancelled":
				res.status = "aborted"
			default:
				if res.status == "completed" {
					res.status = "failed"
					if res.errMsg == "" {
						res.errMsg = fmt.Sprintf("commandcode run ended with subtype %q", line.Subtype)
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil && res.status == "completed" {
		res.status = "failed"
		res.errMsg = fmt.Sprintf("commandcode stream read error: %v", err)
	}

	return res
}

func (b *commandcodeBackend) handleEvent(ev commandcodeEvent, ch chan<- Message, res *commandcodeScanResult, currentModel *string) {
	switch ev.Type {
	case "run_start":
		if ev.SessionID != "" {
			res.sessionID = ev.SessionID
			// Pin the resume pointer as early as the protocol allows. A run
			// killed before its result line still leaves the daemon a session
			// id it can resume from.
			ch <- Message{Type: MessageStatus, Status: "started", SessionID: ev.SessionID}
		}

	case "model_request_start":
		if ev.Model != "" {
			*currentModel = ev.Model
		}

	case "model_request_end":
		if ev.Usage == nil {
			return
		}
		model := ev.Model
		if model == "" {
			model = *currentModel
		}
		if model == "" {
			model = "unknown"
		}
		if res.usage == nil {
			res.usage = make(map[string]TokenUsage)
		}
		u := res.usage[model]
		u.InputTokens += ev.Usage.InputTokens
		u.OutputTokens += ev.Usage.OutputTokens
		u.CacheReadTokens += ev.Usage.CacheReadTokens
		u.CacheWriteTokens += ev.Usage.CacheWriteTokens
		res.usage[model] = u

	case "message_end":
		// A turn's assistant content arrives here as one batch — Command Code
		// has no token-level delta event.
		for _, block := range ev.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					ch <- Message{Type: MessageText, Content: block.Text}
				}
			case "reasoning", "thinking":
				if block.Text != "" {
					ch <- Message{Type: MessageThinking, Content: block.Text}
				}
			case "tool_use":
				// Provider-executed tools never run on this machine and have
				// no matching client-side result, so surfacing them would
				// leave a tool call that never closes.
				if block.ProviderExecuted {
					continue
				}
				ch <- Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: block.Input}
			}
		}

	case "tool_running":
		// tool_running is the streaming signal: it fires before the tool
		// executes, whereas the matching tool_use block only arrives when the
		// whole turn closes. Emitting on message_end alone would hold every
		// tool call back until the turn ended.
		ch <- Message{Type: MessageToolUse, Tool: ev.ToolName, CallID: ev.ToolCallID, Content: ev.Description}

	case "tool_completed":
		ch <- Message{Type: MessageToolResult, Tool: ev.ToolName, CallID: ev.ToolCallID, Output: commandcodeToolOutput(ev.Result)}

	case "tool_errored":
		out := commandcodeToolOutput(ev.Result)
		if out == "" {
			out = ev.Message
		}
		ch <- Message{Type: MessageToolResult, Tool: ev.ToolName, CallID: ev.ToolCallID, Output: out}

	case "tool_denied", "tool_hook_blocked":
		reason := ev.Reason
		if reason == "" {
			reason = ev.Message
		}
		ch <- Message{Type: MessageToolResult, Tool: ev.ToolName, CallID: ev.ToolCallID, Output: reason}

	case "interrupted":
		res.status = "aborted"
		if res.errMsg == "" {
			res.errMsg = "run interrupted"
		}

	case "run_error":
		res.status = "failed"
		if res.errMsg == "" {
			res.errMsg = ev.Error.String()
		}
		if msg := ev.Error.String(); msg != "" {
			ch <- Message{Type: MessageError, Content: msg}
		}

	case "mod_error":
		// A mod failing is not fatal to the run; report it without changing
		// the outcome.
		if ev.Message != "" {
			ch <- Message{Type: MessageLog, Level: "warn", Content: ev.Message}
		}

	case "notice", "api_retry", "stream_restart", "continuation_recovery",
		"compaction_start", "compaction_done", "skill_loaded",
		"subagent_start", "subagent_stop", "permission_mode_changed":
		if ev.Message != "" {
			ch <- Message{Type: MessageLog, Level: "info", Content: ev.Message}
		}
	}
}

// commandcodeToolOutput renders a tool result payload as text. Command Code
// sends either a bare string or a structured value; the structured form is
// re-encoded rather than dropped so the transcript keeps what the tool said.
func commandcodeToolOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// ── Model discovery ──

// commandcodeModelLine matches one catalog row of `commandcode --list-models`.
// The command has no JSON mode, so the human-readable table is the only
// catalog source. A row is a provider-qualified id followed by two or more
// spaces and a description:
//
//	deepseek/deepseek-v4-flash             fast hybrid-attention reasoning (default)
//
// Section headers ("Open Source", "Anthropic") carry no slash in their first
// token and the trailing help text ("Pass the full id, …", "Docs:  https://…")
// never matches the id shape, so both fall out without special-casing.
var commandcodeModelLine = regexp.MustCompile(`^(\S+/\S+)\s{2,}(\S.*)$`)

// commandcodeDefaultMarker is how the catalog flags the model a bare run would
// pick. It is stripped from the label so the marker does not leak into the UI.
const commandcodeDefaultMarker = "(default)"

func discoverCommandCodeModels(ctx context.Context, runtimeCmd Command) ([]Model, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "commandcode"
	}
	cmd := runtimeCmd.exec(ctx, "--list-models")
	hideAgentWindow(cmd)
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := startOwnedProcessTree(cmd, runtimeCmd.logger); err != nil {
		return nil, err
	}

	var models []Model
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		m := commandcodeModelLine.FindStringSubmatch(strings.TrimRight(scanner.Text(), " \t"))
		if m == nil {
			continue
		}
		id, label := m[1], strings.TrimSpace(m[2])
		isDefault := strings.Contains(label, commandcodeDefaultMarker)
		if isDefault {
			label = strings.TrimSpace(strings.Replace(label, commandcodeDefaultMarker, "", 1))
		}
		model := Model{ID: id, Label: label, Default: isDefault}
		if provider, _, ok := strings.Cut(id, "/"); ok {
			model.Provider = provider
		}
		models = append(models, model)
	}
	scanErr := scanner.Err()
	exitErr := cmd.Wait()
	releaseProcessGroup(cmd)
	if scanErr != nil {
		return nil, scanErr
	}
	if exitErr != nil {
		return nil, exitErr
	}
	if len(models) == 0 {
		return nil, errors.New("commandcode returned an empty model catalog")
	}
	return models, nil
}
