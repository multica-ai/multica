package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Droid's one-shot stream-json protocol is distinct from Claude's: assistant
// text is a message event, tools use camelCase fields, and completion is final.
// Protocol fields were checked against the official Droid 1.8.0 executable.
type droidBackend struct{ cfg Config }

var droidBlockedArgs = map[string]blockedArgMode{
	"exec":                         blockedStandalone,
	"--":                           blockedStandalone,
	"-o":                           blockedWithValue,
	"--output-format":              blockedWithValue,
	"--input-format":               blockedWithValue,
	"-f":                           blockedWithValue,
	"--file":                       blockedWithValue,
	"--auto":                       blockedWithValue,
	"--skip-permissions-unsafe":    blockedStandalone,
	"--cwd":                        blockedWithValue,
	"-m":                           blockedWithValue,
	"--model":                      blockedWithValue,
	"-s":                           blockedWithValue,
	"--session-id":                 blockedWithValue,
	"--fork":                       blockedWithValue,
	"--append-system-prompt":       blockedWithValue,
	"--append-system-prompt-file":  blockedWithValue,
	"-w":                           blockedOptionalValue,
	"--worktree":                   blockedOptionalValue,
	"--worktree-dir":               blockedWithValue,
	"--mission":                    blockedStandalone,
	"--worker-model":               blockedWithValue,
	"--worker-reasoning-effort":    blockedWithValue,
	"--validator-model":            blockedWithValue,
	"--validator-reasoning-effort": blockedWithValue,
	"--list-tools":                 blockedStandalone,
	"-v":                           blockedStandalone,
	"--version":                    blockedStandalone,
	"-h":                           blockedStandalone,
	"--help":                       blockedStandalone,
}

// Commander accepts attached short values (-otext, -fother.md). Normalize
// those before the shared filter so they cannot replace our transport/prompt.
func normalizeDroidArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for _, arg := range args {
		arg = unshellQuoteArg(arg)
		if len(arg) > 2 && arg[0] == '-' && strings.ContainsRune("ofmsw", rune(arg[1])) && arg[2] != '=' {
			arg = arg[:2] + "=" + arg[2:]
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

// The one-shot CLI treats an unclaimed positional argument as a replacement
// prompt. Track native option arity so custom configuration cannot shadow stdin.
// Unknown options fail closed until their one-shot semantics are checked.
var droidNativeArgs = map[string]bool{
	"-r": true, "--reasoning-effort": true,
	"--spec-model": true, "--spec-reasoning-effort": true,
	"--only-tools": true, "--add-tools": true, "--remove-tools": true,
	"--settings": true, "--tag": true, "--log-group-id": true,
	"--use-spec": false, "--disable-builtin-skills": false,
}

func buildDroidArgs(opts ExecOptions, logger *slog.Logger) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	args := []string{"exec", "--output-format", "stream-json", "--auto", "high"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session-id", opts.ResumeSessionID)
	}
	for _, custom := range [][]string{opts.ExtraArgs, opts.CustomArgs} {
		filtered := filterCustomArgs(normalizeDroidArgs(custom), droidBlockedArgs, logger)
		for i := 0; i < len(filtered); i++ {
			arg := filtered[i]
			flag, _, inline := strings.Cut(arg, "=")
			// Commander also accepts -rhigh and -r=high.
			if strings.HasPrefix(arg, "-r") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
				flag, inline = "-r", true
			}
			takesValue, ok := droidNativeArgs[flag]
			if !ok {
				return nil, errors.New("droid custom arguments must use supported one-shot options; positional prompts and unknown options are not allowed")
			}
			args = append(args, arg)
			if takesValue && !inline {
				if i+1 == len(filtered) || strings.HasPrefix(filtered[i+1], "-") {
					return nil, fmt.Errorf("droid custom option %s requires a value", flag)
				}
				i++
				args = append(args, filtered[i])
			}
		}
	}
	return args, nil
}

func (b *droidBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if hasManagedMcpConfig(opts.McpConfig) {
		return nil, fmt.Errorf("droid does not support Multica-managed MCP configuration; configure MCP in Droid itself")
	}
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "droid"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("droid executable not found at %q; install Factory Droid or set MULTICA_DROID_PATH: %w", execPath, err)
	}
	args, err := buildDroidArgs(opts, b.cfg.Logger)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	var systemFile string
	cleanupSystemPrompt := func() {
		if systemFile != "" {
			_ = os.Remove(systemFile)
		}
	}
	startedProcess := false
	defer func() {
		if !startedProcess {
			cleanupSystemPrompt()
		}
	}()
	if opts.SystemPrompt != "" {
		file, err := os.CreateTemp("", "multica-droid-system-*.md")
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create droid system prompt file: %w", err)
		}
		systemFile = file.Name()
		_, writeErr := io.WriteString(file, opts.SystemPrompt)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			cancel()
			return nil, fmt.Errorf("write droid system prompt file: %w", errors.Join(writeErr, closeErr))
		}
		args = append(args, "--append-system-prompt-file", systemFile)
	}
	runtimeCmd := b.cfg.commandAt(execPath)
	runtimeCmd.Prefix = FilterLaunchPrefix("droid", normalizeDroidArgs(runtimeCmd.Prefix), b.cfg.Logger)
	cmd := runtimeCmd.exec(runCtx, args...)
	hideAgentWindow(cmd)
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)
	// Stdin avoids OS command-line size/quoting limits and keeps prompts out of
	// process listings. Droid reads to EOF before executing the task.
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "exec")))
	// Keep raw stderr out of daemon logs. Publish only a scrubbed diagnostic
	// after draining the private, bounded tail.
	stderr := newStderrTail(io.Discard, agentStderrTailBytes)
	// Own the output pipes: Wait must observe the direct child's exit even
	// when a descendant retains a writer, and must not close unread stdout.
	stdout, outW, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("droid stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		return nil, fmt.Errorf("droid stderr pipe: %w", err)
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	inR, stdin, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		return nil, fmt.Errorf("droid stdin pipe: %w", err)
	}
	cmd.Stdin = inR
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		_ = inR.Close()
		_ = stdin.Close()
		return nil, fmt.Errorf("start droid: %w", err)
	}
	startedProcess = true
	_ = outW.Close()
	_ = errW.Close()
	_ = inR.Close()
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("droid does not support max_turns; use the task timeout to bound execution")
	}
	scrub := droidOutputScrubber(b.cfg.Env)
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	var terminalObserved atomic.Bool
	waitCh := make(chan error, 1)
	go func(done chan<- error) { done <- cmd.Wait() }(waitCh)
	inputCh := make(chan error, 1)
	go func(done chan<- error) {
		_, err := io.WriteString(stdin, prompt)
		_ = stdin.Close()
		done <- err
	}(inputCh)
	stderrDone := make(chan struct{})
	go func(done chan<- struct{}) {
		_, _ = io.Copy(stderr, errR)
		close(done)
	}(stderrDone)
	reads := make(chan droidStreamRead, 16)
	stopReads := make(chan struct{})
	go readDroidStream(stdout, scrub, reads, stopReads)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer cleanupSystemPrompt()
		started := time.Now()
		state := droidStreamState{model: opts.Model}
		var scanErr, exitErr, inputErr, runErr error
		ctxDone := runCtx.Done()
		var drain *time.Timer
		var drainC <-chan time.Time
		defer func() {
			if drain != nil {
				drain.Stop()
			}
		}()
		setDrainDeadline := func() {
			if drain == nil {
				drain = time.NewTimer(droidStreamDrainGrace)
			} else {
				drain.Reset(droidStreamDrainGrace)
			}
			drainC = drain.C
		}
		cleaning := false
		beginCleanup := func() {
			if cleaning {
				return
			}
			cleaning = true
			// Preserve the outcome established before bounded cleanup. A later
			// task deadline cannot erase a verified completion and process exit.
			runErr = runCtx.Err()
			ctxDone = nil
			if waitCh == nil && (state.sawCompletion || state.failed) && runErr == nil {
				terminalObserved.Store(true)
			}
			signalProcessGroup(cmd, syscall.SIGKILL)
			_ = stdin.Close()
			setDrainDeadline()
		}
		for waitCh != nil || reads != nil || stderrDone != nil || inputCh != nil {
			select {
			case item := <-reads:
				if item.done {
					reads, scanErr = nil, item.err
					if scanErr != nil {
						// Stop a producer blocked writing an oversized frame, without
						// classifying our internal cancellation as a user abort.
						beginCleanup()
						cancel()
					}
				} else {
					handleDroidEvent(item.event, msgCh, &state)
				}
			case exitErr = <-waitCh:
				waitCh = nil
				// A launch wrapper can exit before its descendant finishes the
				// protocol. Allow trailing output, bounded even without a timeout.
				if !cleaning {
					setDrainDeadline()
				}
			case inputErr = <-inputCh:
				inputCh = nil
			case <-stderrDone:
				stderrDone = nil
			case <-ctxDone:
				beginCleanup()
			case <-drainC:
				if cleaning {
					b.cfg.Logger.Warn("droid cleanup did not settle before drain deadline", "pid", cmd.Process.Pid)
					if waitCh != nil {
						exitErr = fmt.Errorf("process did not exit during cleanup")
					}
					// The pipe closes and stopReads below bound readers even
					// when a writer escaped the owned process tree.
					waitCh, reads, stderrDone, inputCh = nil, nil, nil, nil
				} else {
					beginCleanup()
				}
			}
			if waitCh == nil && (state.sawCompletion || reads == nil) {
				beginCleanup()
			}
		}
		close(stopReads)
		_ = stdout.Close()
		_ = errR.Close()
		_ = stdin.Close()
		releaseProcessGroup(cmd)
		terminal := streamTerminalState{
			sawResult: state.sawCompletion, finalResultText: state.finalText,
			resultIsError: state.failed, scanErr: scanErr,
		}
		if state.failed {
			terminal.finalResultText = state.errorText
		}
		// An authoritative terminal response means Droid consumed enough input
		// to answer; otherwise retain input transport failures as diagnostics.
		if state.sawCompletion || state.failed {
			inputErr = nil
		}
		status, output, errText := finalizeStreamResult("droid", opts.Timeout, runErr, inputErr, exitErr, state.sessionID, terminal, "")
		if errText != "" {
			errText = sanitizeAgentDiagnostic(scrub(withAgentStderr(errText, "droid", stderr.Tail())))
		}
		var usage map[string]TokenUsage
		if state.usage != nil {
			model := state.model
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: *state.usage}
		}
		b.cfg.Logger.Info("droid finished", "pid", cmd.Process.Pid, "status", status)
		cleanupSystemPrompt()
		resCh <- Result{
			Status: status, Output: output, Error: errText,
			DurationMs: time.Since(started).Milliseconds(), Usage: usage,
			// Only an observed ID proves a session loaded. An empty ID lets the
			// daemon apply its bounded pre-tool fresh-session recovery policy.
			SessionID: state.sessionID,
		}
	}()
	return &Session{Messages: msgCh, Result: resCh, TerminalObserved: terminalObserved.Load}, nil
}

const droidStreamDrainGrace = 2 * time.Second

type droidStreamRead struct {
	event droidStreamEvent
	done  bool
	err   error
}

func readDroidStream(stdout io.Reader, scrub func(string) string, reads chan<- droidStreamRead, stop <-chan struct{}) {
	scanner := newAgentStreamScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		var event droidStreamEvent
		if line == "" || json.Unmarshal([]byte(scrub(line)), &event) != nil {
			continue
		}
		select {
		case reads <- droidStreamRead{event: event}:
		case <-stop:
			return
		}
	}
	select {
	case reads <- droidStreamRead{done: true, err: scanner.Err()}:
	case <-stop:
	}
}

type droidStreamEvent struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	Role       string          `json:"role"`
	Text       string          `json:"text"`
	Message    string          `json:"message"`
	FinalText  string          `json:"finalText"`
	SessionID  string          `json:"session_id"`
	Model      string          `json:"model"`
	ID         string          `json:"id"`
	ToolName   string          `json:"toolName"`
	Parameters map[string]any  `json:"parameters"`
	Value      json.RawMessage `json:"value"`
	Error      json.RawMessage `json:"error"`
	IsError    bool            `json:"isError"`
	Usage      *struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type droidStreamState struct {
	sessionID, model, finalText, errorText string
	sawCompletion, failed                  bool
	usage                                  *TokenUsage
}

func handleDroidEvent(e droidStreamEvent, ch chan<- Message, s *droidStreamState) {
	if e.SessionID != "" {
		s.sessionID = e.SessionID
	}
	if e.Model != "" {
		s.model = e.Model
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: s.sessionID})
		}
	case "message":
		if e.Role == "assistant" && e.Text != "" {
			trySend(ch, Message{Type: MessageText, Content: e.Text, SessionID: s.sessionID})
		}
	case "tool_call":
		trySend(ch, Message{Type: MessageToolUse, Tool: e.ToolName, CallID: e.ID, Input: e.Parameters})
	case "tool_result":
		value := e.Value
		if e.IsError {
			value = e.Error
		}
		var output string
		if json.Unmarshal(value, &output) != nil && len(value) > 0 {
			output = string(value)
		}
		trySend(ch, Message{Type: MessageToolResult, CallID: e.ID, Output: output})
	case "completion":
		s.sawCompletion = true
		s.finalText = e.FinalText
		if e.Usage != nil {
			s.usage = &TokenUsage{
				InputTokens: e.Usage.Input, OutputTokens: e.Usage.Output,
				CacheReadTokens: e.Usage.CacheRead, CacheWriteTokens: e.Usage.CacheWrite,
			}
		}
	case "error":
		// Droid 1.8.0 completion has no error flag: an earlier error event
		// must remain authoritative even if a completion follows it.
		s.failed = true
		s.errorText = sanitizeAgentDiagnostic(e.Message)
		if s.errorText == "" {
			s.errorText = "droid execution failed without details"
		}
		trySend(ch, Message{Type: MessageError, Content: s.errorText})
	}
}

var droidAPIKeyPattern = regexp.MustCompile(`\bfk-[A-Za-z0-9_-]{16,}\b`)

func droidOutputScrubber(env map[string]string) func(string) string {
	key := os.Getenv("FACTORY_API_KEY")
	if configured, ok := env["FACTORY_API_KEY"]; ok {
		key = configured
	}
	return func(value string) string {
		if key != "" {
			value = strings.ReplaceAll(value, key, "[REDACTED]")
		}
		return droidAPIKeyPattern.ReplaceAllString(value, "[REDACTED]")
	}
}
