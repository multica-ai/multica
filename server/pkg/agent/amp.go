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

// Amp uses its native execute/stream-json protocol. Only a terminal result
// completes the run; an assistant end_turn can precede background subagents.
type ampBackend struct{ cfg Config }

var ampBlockedArgs = map[string]blockedArgMode{
	"-x": blockedOptionalValue, "--execute": blockedOptionalValue,
	"-ox": blockedStandalone, "--orb-execute": blockedStandalone,
	"--executor": blockedWithValue, "--runner-dir": blockedWithValue,
	"--stream-json": blockedStandalone, "--stream-json-thinking": blockedStandalone,
	"--stream-json-input": blockedStandalone,
	"-m":                  blockedWithValue, "--mode": blockedWithValue,
	"--mcp-config": blockedWithValue,
	"--no-tui":     blockedStandalone, "--": blockedStandalone,
}

// Unknown options and positionals can select a subcommand or replace stdin.
// Keep the supported native options explicit and never log their values.
var ampNativeArgs = map[string]bool{
	"--settings-file": true, "--features": true, "--fast": false,
	"--plugin-ready-timeout": true, "--timings": false,
	"--visibility": true, "--label": true, "-l": true,
	"--no-archive-after-execute": false,
}

func normalizeAmpArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for _, arg := range args {
		arg = unshellQuoteArg(arg)
		if len(arg) > 2 && arg[0] == '-' && (arg[1] == 'x' || arg[1] == 'm') && arg[2] != '=' {
			arg = arg[:2] + "=" + arg[2:]
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func buildAmpArgs(opts ExecOptions, logger *slog.Logger) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	args := []string{}
	if opts.ResumeSessionID != "" {
		// Never use --last: each task owns its explicit thread pointer.
		args = append(args, "threads", "continue", opts.ResumeSessionID)
	}
	args = append(args, "--execute", "--stream-json", "--executor", "local")
	if opts.Model != "" {
		args = append(args, "--mode", opts.Model)
	}
	for _, custom := range [][]string{opts.ExtraArgs, opts.CustomArgs} {
		filtered := filterCustomArgs(normalizeAmpArgs(custom), ampBlockedArgs, logger)
		for i := 0; i < len(filtered); i++ {
			arg := filtered[i]
			flag, _, inline := strings.Cut(arg, "=")
			takesValue, ok := ampNativeArgs[flag]
			if !ok || (!takesValue && inline) {
				return nil, errors.New("amp custom arguments must use supported execute-mode options; positional prompts, subcommands and unknown options are not allowed")
			}
			args = append(args, arg)
			if takesValue && !inline {
				if i+1 == len(filtered) || strings.HasPrefix(filtered[i+1], "-") {
					return nil, fmt.Errorf("amp custom option %s requires a value", flag)
				}
				i++
				args = append(args, filtered[i])
			}
		}
	}
	return args, nil
}

func (b *ampBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if hasManagedMcpConfig(opts.McpConfig) {
		return nil, fmt.Errorf("amp does not support Multica-managed MCP configuration; configure MCP in Amp itself")
	}
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "amp"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("amp executable not found at %q; install Amp or set MULTICA_AMP_PATH: %w", execPath, err)
	}
	key := os.Getenv("AMP_API_KEY")
	if configured, ok := b.cfg.Env["AMP_API_KEY"]; ok {
		key = configured
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("amp execute mode requires AMP_API_KEY; set an Amp access token in the daemon or agent environment (interactive login is not supported)")
	}
	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n" + prompt
	}
	args, err := buildAmpArgs(opts, b.cfg.Logger)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	runtimeCmd := b.cfg.commandAt(execPath)
	runtimeCmd.Prefix = FilterLaunchPrefix("amp", normalizeAmpArgs(runtimeCmd.Prefix), b.cfg.Logger)
	cmd := runtimeCmd.exec(runCtx, args...)
	hideAgentWindow(cmd)
	// The reader owns cancellation so a late context cancellation cannot kill
	// a completed run before cleanup records why the process was stopped.
	cmd.Cancel = func() error { return nil }
	cmd.Dir = opts.Cwd
	cmd.Env = append(buildEnv(b.cfg.Env), "AMP_SKIP_UPDATE_CHECK=1")
	// Stdin avoids OS command-line size/quoting limits and keeps prompts out of
	// process listings. Amp reads to EOF before executing the task.
	logArgs := newAgentCommandLogArgs(args)
	if opts.ResumeSessionID != "" {
		logArgs = newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "threads"), trustAgentCommandPositional(1, "continue"))
	}
	b.cfg.logAgentCommand(cmd, logArgs)
	// Keep raw stderr out of daemon logs. Publish only a scrubbed diagnostic
	// after draining the private, bounded tail.
	stderr := newStderrTail(io.Discard, agentStderrTailBytes)
	// Own the output pipes: Wait must observe the direct child's exit even
	// when a descendant retains a writer, and must not close unread stdout.
	stdout, outW, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("amp stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		return nil, fmt.Errorf("amp stderr pipe: %w", err)
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	inR, stdin, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		return nil, fmt.Errorf("amp stdin pipe: %w", err)
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
		return nil, fmt.Errorf("start amp: %w", err)
	}
	_ = outW.Close()
	_ = errW.Close()
	_ = inR.Close()
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("amp does not support max_turns; use the task timeout to bound execution")
	}
	scrub := ampOutputScrubber(b.cfg.Env)
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
	reads := make(chan ampStreamRead, 16)
	stopReads := make(chan struct{})
	go readAmpStream(stdout, scrub, reads, stopReads)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		started := time.Now()
		state := ampStreamState{}
		var scanErr, exitErr, inputErr, runErr error
		ctxDone := runCtx.Done()
		var drain *time.Timer
		var drainC <-chan time.Time
		defer func() {
			if drain != nil {
				drain.Stop()
			}
		}()
		setDrainDeadline := func(grace time.Duration) {
			if drain == nil {
				drain = time.NewTimer(grace)
			} else {
				drain.Reset(grace)
			}
			drainC = drain.C
		}
		cleaning := false
		stoppedAfterTerminal := false
		beginCleanup := func() {
			if cleaning {
				return
			}
			cleaning = true
			if !terminalObserved.Load() {
				runErr = runCtx.Err()
			}
			ctxDone = nil
			// Preserve a native exit already available before we stop the tree.
			if waitCh != nil {
				select {
				case exitErr = <-waitCh:
					waitCh = nil
				default:
				}
			}
			if waitCh == nil && (state.sawCompletion || state.failed) && runErr == nil {
				terminalObserved.Store(true)
			}
			if waitCh != nil && terminalObserved.Load() {
				stoppedAfterTerminal = true
				b.cfg.Logger.Warn("amp process still running after terminal result; stopping it during cleanup", "pid", cmd.Process.Pid)
			}
			signalProcessGroup(cmd, syscall.SIGKILL)
			_ = stdin.Close()
			setDrainDeadline(ampStreamDrainGrace)
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
					handleAmpEvent(item.event, msgCh, &state)
					if state.sawCompletion && !cleaning && runCtx.Err() == nil {
						// Publish the protocol boundary before waiting for archival or
						// other CLI cleanup. The daemon must not reclassify it as idle.
						terminalObserved.Store(true)
					}
					if !cleaning && (state.sawCompletion || state.failed) && drain == nil {
						grace := ampStreamDrainGrace
						if state.sawCompletion {
							grace = ampTerminalExitGrace
						}
						setDrainDeadline(grace)
					}
				}
			case exitErr = <-waitCh:
				waitCh = nil
				if stoppedAfterTerminal {
					// This exit acknowledges our cleanup, not an execution failure.
					// A missing acknowledgement still fails at the drain deadline.
					exitErr = nil
				}
				// A launch wrapper can exit before its descendant finishes the
				// protocol. Allow trailing output, bounded even without a timeout.
				if !cleaning {
					setDrainDeadline(ampStreamDrainGrace)
				}
			case inputErr = <-inputCh:
				inputCh = nil
			case <-stderrDone:
				stderrDone = nil
			case <-ctxDone:
				beginCleanup()
			case <-drainC:
				if cleaning {
					b.cfg.Logger.Warn("amp cleanup did not settle before drain deadline", "pid", cmd.Process.Pid)
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
		// An authoritative terminal response means Amp consumed enough input
		// to answer; otherwise retain input transport failures as diagnostics.
		if state.sawCompletion || state.failed {
			inputErr = nil
		}
		// os/exec can return the context error when its no-op Cancel raced a
		// clean exit. It is also cleanup after an already observed result.
		if terminalObserved.Load() && (errors.Is(exitErr, context.Canceled) || errors.Is(exitErr, context.DeadlineExceeded)) {
			exitErr = nil
		}
		status, output, errText := finalizeStreamResult("amp", opts.Timeout, runErr, inputErr, exitErr, state.sessionID, terminal, "")
		if errText != "" {
			errText = sanitizeAgentDiagnostic(scrub(withAgentStderr(errText, "amp", stderr.Tail())))
		}
		usage := state.usage
		b.cfg.Logger.Info("amp finished", "pid", cmd.Process.Pid, "status", status)
		resCh <- Result{
			Status: status, Output: output, Error: errText,
			DurationMs: time.Since(started).Milliseconds(), Usage: usage,
			// Only an observed ID proves a session loaded. An empty ID lets the
			// daemon apply its bounded pre-tool fresh-session recovery policy.
			SessionID: state.sessionID,
		}
	}()
	return &Session{CleanupRequired: true, Messages: msgCh, Result: resCh, TerminalObserved: terminalObserved.Load}, nil
}

const (
	ampStreamDrainGrace  = 2 * time.Second
	ampTerminalExitGrace = 10 * time.Second
)

type ampStreamRead struct {
	event ampStreamEvent
	done  bool
	err   error
}

func readAmpStream(stdout io.Reader, scrub func(string) string, reads chan<- ampStreamRead, stop <-chan struct{}) {
	scanner := newAgentStreamScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		var event ampStreamEvent
		if line == "" || json.Unmarshal([]byte(scrub(line)), &event) != nil {
			continue
		}
		select {
		case reads <- ampStreamRead{event: event}:
		case <-stop:
			return
		}
	}
	select {
	case reads <- ampStreamRead{done: true, err: scanner.Err()}:
	case <-stop:
	}
}

type ampStreamEvent struct {
	Type, Subtype string
	SessionID     string `json:"session_id"`
	Message       struct {
		Model   string `json:"model"`
		Content []struct {
			Type, Text, Thinking, ID, Name string
			Input                          map[string]any  `json:"input"`
			ToolUseID                      string          `json:"tool_use_id"`
			Content                        json.RawMessage `json:"content"`
		} `json:"content"`
	} `json:"message"`
	Result            string   `json:"result"`
	Error             string   `json:"error"`
	IsError           bool     `json:"is_error"`
	PermissionDenials []string `json:"permission_denials"`
	Usage             *struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type ampStreamState struct {
	sessionID, model, finalText, errorText string
	sawCompletion, failed                  bool
	usage                                  map[string]TokenUsage
}

func handleAmpEvent(e ampStreamEvent, ch chan<- Message, s *ampStreamState) {
	if s.sawCompletion {
		return
	}
	if e.SessionID != "" {
		s.sessionID = e.SessionID
	}
	if e.Message.Model != "" {
		s.model = e.Message.Model
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: s.sessionID})
		} else if strings.HasPrefix(e.Subtype, "error_") {
			s.failed, s.errorText = true, e.Error
			trySend(ch, Message{Type: MessageError, Content: sanitizeAgentDiagnostic(e.Error)})
		}
	case "assistant":
		for _, block := range e.Message.Content {
			switch block.Type {
			case "text":
				trySend(ch, Message{Type: MessageText, Content: block.Text, SessionID: s.sessionID})
			case "thinking":
				trySend(ch, Message{Type: MessageThinking, Content: block.Thinking})
			case "tool_use":
				trySend(ch, Message{Type: MessageToolUse, CallID: block.ID, Tool: block.Name, Input: block.Input})
			}
		}
	case "user":
		for _, block := range e.Message.Content {
			if block.Type == "tool_result" {
				trySend(ch, Message{Type: MessageToolResult, CallID: block.ToolUseID, Output: ampToolOutput(block.Content)})
			}
		}
	case "result":
		s.sawCompletion = true
		s.finalText = e.Result
		if len(e.PermissionDenials) > 0 {
			trySend(ch, Message{Type: MessageError, SessionID: s.sessionID, Content: sanitizeAgentDiagnostic(
				"Warning: Amp denied tool permissions: " + strings.Join(e.PermissionDenials, "; ") +
					". Check amp.permissions in the execution machine's Amp settings for unattended commands.")})
		}
		if e.IsError || e.Subtype != "success" {
			s.failed = true
			s.errorText = e.Error
			if s.errorText == "" {
				s.errorText = e.Result
			}
		}
		if e.Usage != nil {
			model := s.model
			if model == "" {
				model = "unknown"
			}
			s.usage = map[string]TokenUsage{model: {
				InputTokens: e.Usage.Input, OutputTokens: e.Usage.Output,
				CacheReadTokens: e.Usage.CacheRead, CacheWriteTokens: e.Usage.CacheWrite,
			}}
		}
	}
}

func ampToolOutput(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct{ Type, Text string }
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

var ampAPIKeyPattern = regexp.MustCompile(`\bsgamp_[A-Za-z0-9_-]+`)

func ampOutputScrubber(env map[string]string) func(string) string {
	key := os.Getenv("AMP_API_KEY")
	if configured, ok := env["AMP_API_KEY"]; ok {
		key = configured
	}
	return func(value string) string {
		if key != "" {
			value = strings.ReplaceAll(value, key, "[REDACTED]")
		}
		return ampAPIKeyPattern.ReplaceAllString(value, "[REDACTED]")
	}
}
