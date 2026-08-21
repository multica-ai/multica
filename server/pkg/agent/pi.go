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
	"strings"
	"sync"
	"time"
)

// piBackend implements Backend by spawning the Pi CLI in non-interactive
// JSON mode (`pi -p --mode json --session <path>`) and parsing its event
// stream on stdout.
//
// It also backs the "omp" (oh-my-pi) provider — omp is a separate CLI
// (https://omp.sh) that is a drop-in fork of pi and speaks the same JSON
// event protocol. The daemon probes a separate `omp` binary and registers
// it under the "omp" key; piBackend uses defaultExecutable so the fallback
// binary name matches the provider key (pi → "pi", omp → "omp") when
// cfg.ExecutablePath is empty.
type piBackend struct {
	cfg               Config
	defaultExecutable string
	// providerLabel is the human-facing name used in log messages and error
	// strings ("pi" or "omp"). Defaults to "pi" when empty so existing callers
	// that construct piBackend directly (tests) keep their original output.
	providerLabel string
}

var (
	piControlTokenRE = regexp.MustCompile(`<\|[A-Za-z0-9_-]+>[A-Za-z0-9_-]*|<[A-Za-z0-9_-]+\|>`)
)

func stripPiToolCallMarkup(s string) string {
	s = stripPiStructuredToolMarkup(s)
	return piControlTokenRE.ReplaceAllString(s, "")
}

func drainPiTextBuffer(buf *strings.Builder, delta string) string {
	buf.WriteString(delta)
	emit, pending := drainPiSanitizedText(buf.String())
	buf.Reset()
	buf.WriteString(pending)
	return emit
}

func flushPiTextBuffer(buf *strings.Builder) string {
	s := buf.String()
	buf.Reset()
	emit, pending := drainPiSanitizedText(s)
	emit += piControlTokenRE.ReplaceAllString(pending, "")
	return emit
}

func drainPiSanitizedText(s string) (string, string) {
	var out strings.Builder
	for i := 0; i < len(s); {
		start, prefixLen := nextPiToolMarkupPrefix(s, i)
		if start == -1 {
			safeLen := safePiTextEmitLen(s[i:])
			out.WriteString(s[i : i+safeLen])
			return piControlTokenRE.ReplaceAllString(out.String(), ""), s[i+safeLen:]
		}
		out.WriteString(s[i:start])
		end, ok := scanPiToolMarkupEnd(s, start+prefixLen)
		if !ok {
			return piControlTokenRE.ReplaceAllString(out.String(), ""), s[start:]
		}
		i = end
	}
	return piControlTokenRE.ReplaceAllString(out.String(), ""), ""
}

func stripPiStructuredToolMarkup(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		start, prefixLen := nextPiToolMarkupPrefix(s, i)
		if start == -1 {
			out.WriteString(s[i:])
			break
		}
		out.WriteString(s[i:start])
		end, ok := scanPiToolMarkupEnd(s, start+prefixLen)
		if !ok {
			out.WriteString(s[start:])
			break
		}
		i = end
	}
	return out.String()
}

func safePiTextEmitLen(s string) int {
	hold := 0
	for _, prefix := range []string{"call:", "response:"} {
		for n := 1; n < len(prefix) && n <= len(s); n++ {
			if strings.HasSuffix(s, prefix[:n]) && n > hold {
				hold = n
			}
		}
	}
	if i := strings.LastIndexByte(s, '<'); i >= 0 && looksLikePiControlTokenPrefix(s[i:]) {
		if len(s)-i > hold {
			hold = len(s) - i
		}
	}
	return len(s) - hold
}

func looksLikePiControlTokenPrefix(s string) bool {
	if len(s) == 0 || s[0] != '<' || len(s) > 64 {
		return false
	}
	for i := 1; i < len(s); i++ {
		b := s[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '|' || b == '>' {
			continue
		}
		return false
	}
	return true
}

func nextPiToolMarkupPrefix(s string, from int) (int, int) {
	best := -1
	bestLen := 0
	for _, prefix := range []string{"call:", "response:"} {
		if i := strings.Index(s[from:], prefix); i >= 0 {
			abs := from + i
			if best == -1 || abs < best {
				best = abs
				bestLen = len(prefix)
			}
		}
	}
	return best, bestLen
}

func scanPiToolMarkupEnd(s string, i int) (int, bool) {
	nameStart := i
	for i < len(s) && isPiToolNameByte(s[i]) {
		i++
	}
	if i == nameStart || i >= len(s) || s[i] != '{' {
		return 0, false
	}

	const quoteMarker = `<|"|>`
	depth := 0
	inQuote := false
	for i < len(s) {
		if strings.HasPrefix(s[i:], quoteMarker) {
			inQuote = !inQuote
			i += len(quoteMarker)
			continue
		}

		if !inQuote {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					i++
					if strings.HasPrefix(s[i:], "<tool_call|>") {
						i += len("<tool_call|>")
					}
					return i, true
				}
			}
		}
		i++
	}
	return 0, false
}

func isPiToolNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

func (b *piBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	label := b.providerLabel
	if label == "" {
		label = "pi"
	}
	// Pi trims piped stdin before building its initial message. Reject an empty
	// task here so whitespace-only input cannot turn into a successful process
	// with no turn, no output, and an empty session.
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("%s prompt must not be empty", label)
	}

	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = b.defaultExecutable
	}
	if execName == "" {
		execName = "pi"
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("%s executable not found at %q: %w", label, execName, err)
	}

	timeout := opts.Timeout

	// Pi's --session flag expects a file path where events are appended.
	// The path doubles as our opaque session identifier: we return it as
	// SessionID and expect it back as ResumeSessionID on the next turn.
	sessionPath := opts.ResumeSessionID
	if sessionPath == "" {
		p, err := newPiSessionPath()
		if err != nil {
			return nil, fmt.Errorf("%s session path: %w", label, err)
		}
		sessionPath = p
	}
	if err := ensurePiSessionFile(sessionPath); err != nil {
		return nil, fmt.Errorf("%s session file: %w", label, err)
	}

	runCtx, cancel := runContext(ctx, timeout)

	args := buildPiArgs(sessionPath, opts, b.cfg.Logger)
	cmd, _, _ := b.cfg.commandAt(execName).execVia(runCtx, choosePiInvocation, lookedUp, args, b.cfg.Logger)
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
		return nil, fmt.Errorf("%s stdout pipe: %w", label, err)
	}
	// Pi reads piped stdin to EOF as its initial prompt in print/JSON mode.
	// Keeping user-controlled text off argv prevents the npm PowerShell shim
	// from re-tokenising embedded quotes into CLI flags on Windows (#6457).
	// The explicit close remains part of the #2188 contract too: under systemd,
	// Pi has been observed to wait indefinitely when stdin never reaches EOF.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s stdin pipe: %w", label, err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	cmd.Stderr = newLogWriter(b.cfg.Logger, "["+label+":stderr] ")

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start %s: %w", label, err)
	}

	b.cfg.Logger.Info(label+" started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Write concurrently with stdout consumption. A large prompt can fill the
	// stdin pipe while the child fills stdout; serialising those operations can
	// deadlock both processes. Closing stdin signals the end of Pi's prompt.
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	// Close both pipes when the context is cancelled. Closing stdin releases a
	// writer blocked on a child that stopped reading; closing stdout releases the
	// stream scanner.
	go func() {
		<-runCtx.Done()
		closeStdin()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		finalStatus := "completed"
		var finalError string
		var lastTurnError string
		usage := make(map[string]TokenUsage)

		// Pi message_update events can be large (they embed the full message
		// partial on each delta); the shared stream bound covers that.
		scanner := newAgentStreamScanner(stdout)
		var textBuffer strings.Builder

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var evt piStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "agent_start":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "turn_start":
				output.Reset()
				textBuffer.Reset()
				lastTurnError = ""

			case "message_update":
				if evt.AssistantMessageEvent == nil {
					continue
				}
				switch evt.AssistantMessageEvent.Type {
				case "text_delta":
					if d := drainPiTextBuffer(&textBuffer, evt.AssistantMessageEvent.Delta); d != "" {
						output.WriteString(d)
						trySend(msgCh, Message{Type: MessageText, Content: d})
					}
				case "thinking_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: d})
					}
				}

			case "tool_execution_start":
				var params map[string]any
				if len(evt.Args) > 0 {
					_ = json.Unmarshal(evt.Args, &params)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolCallID,
					Input:  params,
				})

			case "tool_execution_end":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolCallID,
					Output: decodePiResult(evt.Result),
				})

			case "turn_end":
				msg := decodePiMessage(evt.Message)
				if msg == nil {
					continue
				}
				if msg.Usage != nil {
					model := msg.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "unknown"
					}
					u := usage[model]
					u.InputTokens += msg.Usage.Input
					u.OutputTokens += msg.Usage.Output
					u.CacheReadTokens += msg.Usage.CacheRead
					u.CacheWriteTokens += msg.Usage.CacheWrite
					usage[model] = u
				}
				// A turn Pi ends on an error is only terminal when nothing
				// follows it. Pi emits the same stopReason before an automatic
				// retry, and turn_start clears this, so a later successful turn
				// leaves nothing behind.
				if msg.StopReason == "error" {
					lastTurnError = msg.ErrorMessage
					if lastTurnError == "" {
						lastTurnError = label + " ended the turn with an error"
					}
				}

			case "error":
				errText := decodePiString(evt.Message)
				trySend(msgCh, Message{Type: MessageError, Content: errText})
				if finalStatus == "completed" {
					finalStatus = "failed"
					finalError = errText
				}

			case "auto_retry_end":
				if !evt.Success && finalStatus == "completed" {
					finalStatus = "failed"
					if evt.FinalError != "" {
						finalError = evt.FinalError
					} else {
						finalError = label + " exhausted automatic retries"
					}
				}
			}
		}
		if d := flushPiTextBuffer(&textBuffer); d != "" {
			output.WriteString(d)
			trySend(msgCh, Message{Type: MessageText, Content: d})
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		// Wait closes the process pipes, so a prompt write still blocked when the
		// child exited has returned by now. The writer sends exactly once.
		writeErr := <-writeErrCh

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("%s timed out after %s", label, timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("%s exited with error: %v", label, waitErr)
		} else if writeErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("%s prompt write failed: %v", label, writeErr)
		} else if lastTurnError != "" && finalStatus == "completed" {
			// Pi exits 0 after a turn it could not complete and did not retry,
			// and emits neither an `error` event nor `auto_retry_end`. Without
			// this the run reports success with no output.
			finalStatus = "failed"
			finalError = lastTurnError
		}

		b.cfg.Logger.Info(label+" finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionPath,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// ── Pi event types ──

// piStreamEvent is the union of fields we consume from Pi's JSON event
// stream. Fields that can be either string or object across event types
// (e.g. `message`, `result`) are held as json.RawMessage and decoded on
// demand by the switch arms.
type piStreamEvent struct {
	Type string `json:"type"`

	// message_update
	AssistantMessageEvent *piAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// tool_execution_start / tool_execution_end
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	// error: Message is a string. turn_end: Message is an object.
	Message json.RawMessage `json:"message,omitempty"`

	// auto_retry_end
	Success    bool   `json:"success,omitempty"`
	FinalError string `json:"finalError,omitempty"`
}

type piAssistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

type piMessage struct {
	Role  string   `json:"role,omitempty"`
	Model string   `json:"model,omitempty"`
	Usage *piUsage `json:"usage,omitempty"`

	// turn_end carries the terminal state of the turn. Pi sets StopReason to
	// "error" for a provider call it could not complete, whether or not it
	// goes on to retry, and puts the provider's message in ErrorMessage.
	StopReason   string `json:"stopReason,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type piUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	TotalTokens int64 `json:"totalTokens"`
}

func decodePiMessage(raw json.RawMessage) *piMessage {
	if len(raw) == 0 {
		return nil
	}
	var m piMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return &m
}

func decodePiString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func decodePiResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// ── Arg builder ──

// piBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would
// break the daemon↔Pi communication protocol.
var piBlockedArgs = map[string]blockedArgMode{
	"-p":         blockedStandalone, // non-interactive mode
	"--print":    blockedStandalone, // alias for -p
	"--mode":     blockedWithValue,  // "json" event stream protocol
	"--session":  blockedWithValue,  // daemon manages the session path
	"--thinking": blockedWithValue,  // owned by agent.thinking_level
}

// piCustomArgModes mirrors Pi 0.83's built-in parser closely enough to
// distinguish option values from positional messages. Unknown long flags are
// extension flags and may take one optional value; unknown short flags are left
// intact so Pi can report them as it did before.
var piCustomArgModes = map[string]blockedArgMode{
	"--help":                 blockedStandalone,
	"-h":                     blockedStandalone,
	"--version":              blockedStandalone,
	"-v":                     blockedStandalone,
	"--continue":             blockedStandalone,
	"-c":                     blockedStandalone,
	"--resume":               blockedStandalone,
	"-r":                     blockedStandalone,
	"--provider":             blockedWithValue,
	"--model":                blockedWithValue,
	"--api-key":              blockedWithValue,
	"--system-prompt":        blockedWithValue,
	"--append-system-prompt": blockedWithValue,
	"--name":                 blockedWithValue,
	"-n":                     blockedWithValue,
	"--no-session":           blockedStandalone,
	"--session-id":           blockedWithValue,
	"--fork":                 blockedWithValue,
	"--session-dir":          blockedWithValue,
	"--models":               blockedWithValue,
	"--no-tools":             blockedStandalone,
	"-nt":                    blockedStandalone,
	"--no-builtin-tools":     blockedStandalone,
	"-nbt":                   blockedStandalone,
	"--tools":                blockedWithValue,
	"-t":                     blockedWithValue,
	"--exclude-tools":        blockedWithValue,
	"-xt":                    blockedWithValue,
	"--thinking":             blockedWithValue,
	"--export":               blockedWithValue,
	"--extension":            blockedWithValue,
	"-e":                     blockedWithValue,
	"--no-extensions":        blockedStandalone,
	"-ne":                    blockedStandalone,
	"--skill":                blockedWithValue,
	"--prompt-template":      blockedWithValue,
	"--theme":                blockedWithValue,
	"--no-skills":            blockedStandalone,
	"-ns":                    blockedStandalone,
	"--no-prompt-templates":  blockedStandalone,
	"-np":                    blockedStandalone,
	"--no-themes":            blockedStandalone,
	"--no-context-files":     blockedStandalone,
	"-nc":                    blockedStandalone,
	"--list-models":          blockedOptionalValue,
	"--verbose":              blockedStandalone,
	"--approve":              blockedStandalone,
	"-a":                     blockedStandalone,
	"--no-approve":           blockedStandalone,
	"-na":                    blockedStandalone,
	"--offline":              blockedStandalone,
}

// buildPiArgs assembles the argv for a one-shot Pi invocation.
//
// Flags:
//
//	-p                          non-interactive mode (prompt arrives on stdin)
//	--mode json                 emit one JSON event per line on stdout
//	--session <path>            session log file (created upfront, reused on resume)
//	--model <selector>          model selector, passed through verbatim
//	--thinking <level>          per-agent reasoning level override
//
// The prompt is deliberately absent from argv. Pi reads a non-TTY stdin in
// print/JSON mode; using that supported path prevents Windows PowerShell's npm
// shim from re-tokenising prompt content into options.
func buildPiArgs(sessionPath string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--mode", "json",
	}
	if sessionPath != "" {
		args = append(args, "--session", sessionPath)
	}
	// The selector goes to --model whole, and --provider is never synthesized.
	// Pi's own resolver already accepts every shape we hold: a canonical
	// `provider/id`, a bare id, and — crucially — an id that itself contains a
	// slash, which is the normal case for gateway-style providers whose model
	// ids look like `claude/claude-opus-5`. Splitting on the first slash to
	// fill --provider turns that id into a provider name Pi has never heard of,
	// and an unknown --provider is a hard error ("Unknown provider ...") rather
	// than something Pi can recover from — whereas --model alone falls back to
	// matching the full string as a raw model id. Passing less is strictly more
	// capable here (GH #7300).
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--thinking", opts.ThinkingLevel)
	}
	// Note: we intentionally do NOT pass --tools here. Omitting it lets
	// Pi use its full tool registry, including user-installed extension
	// tools. Passing --tools acts as a restrictive allowlist that
	// silently filters out extension-registered tools (#2379).
	// Users who want to restrict tools can do so via custom_args.
	//
	// SystemPrompt is intentionally not forwarded as --append-system-prompt:
	// Pi loads the per-task AGENTS.md the daemon writes into the workdir, so
	// inlining the same runtime brief would duplicate it on every turn.
	// Verified against Pi 0.67.2 (MUL-5392).
	args = append(args, filterPiCustomArgs(opts.CustomArgs, logger)...)
	return args
}

// filterPiCustomArgs removes @file and positional message inputs from
// custom_args. Once the task prompt is delivered on stdin, Pi concatenates the
// first positional message to stdin with no separator. Keeping those tokens
// would silently mutate or replace the task prompt. Option values remain
// supported, including values for extension-provided --long flags.
func filterPiCustomArgs(args []string, logger *slog.Logger) []string {
	args = filterCustomArgs(args, piBlockedArgs, logger)
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "@") {
			if logger != nil {
				logger.Warn("custom_args: Pi file input would alter the stdin task prompt, skipping")
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			if logger != nil {
				logger.Warn("custom_args: Pi positional input would alter the stdin task prompt, skipping")
			}
			continue
		}

		flag := arg
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			hasInlineValue = true
		}
		filtered = append(filtered, arg)
		if hasInlineValue {
			continue
		}

		mode, known := piCustomArgModes[flag]
		if !known {
			if strings.HasPrefix(flag, "--") {
				mode = blockedOptionalValue
			} else {
				continue
			}
		}
		switch mode {
		case blockedWithValue:
			if i+1 < len(args) {
				filtered = append(filtered, args[i+1])
				i++
			}
		case blockedOptionalValue:
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !strings.HasPrefix(args[i+1], "@") {
				filtered = append(filtered, args[i+1])
				i++
			}
		}
	}
	return filtered
}

// ── Session path ──

// piSessionDir returns the directory where Pi session JSONL files live.
// Exported via a helper so the usage scanner (package usage) can point at
// the same location without duplicating the path construction.
func piSessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".multica", "pi-sessions"), nil
}

func newPiSessionPath() (string, error) {
	dir, err := piSessionDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.jsonl", time.Now().UTC().Format("20060102T150405.000000000"))
	return filepath.Join(dir, name), nil
}

// ensurePiSessionFile creates an empty session file if one does not yet
// exist at path. Pi refuses to start when --session points at a missing
// file; paths that already exist (a resumed session) are left untouched.
func ensurePiSessionFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// PiSessionDir exposes piSessionDir to other packages in this module.
func PiSessionDir() (string, error) {
	return piSessionDir()
}
