package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// CommandDisabledEnv disables the built-in non-LLM "command" runtime on both
	// daemon hosts and the API server when set to a truthy boolean value
	// ("1", "true", "yes", "on", case-insensitive). Enabled by default.
	CommandDisabledEnv = "MULTICA_COMMAND_DISABLED"

	commandStreamDrainGrace = 2 * time.Second
	commandMaxStdoutBytes   = 1 << 20 // 1 MiB retained for Result.Output
)

// ErrCommandAgentsDisabled is returned when an operation targets the built-in
// "command" runtime while MULTICA_COMMAND_DISABLED is active.
var ErrCommandAgentsDisabled = errors.New("command agents are disabled")

// CommandAgentsDisabled reports whether MULTICA_COMMAND_DISABLED is set to a
// truthy value in the current process environment.
func CommandAgentsDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(CommandDisabledEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

var (
	// ansiEscapeSequenceRe matches CSI, OSC, and two-byte terminal escape
	// sequences emitted by CLI tools when writing styled output. Stripping
	// these prevents leading escape codes (e.g. `\x1b[1m## Heading`) from
	// breaking CommonMark heading/list/table parsing in Multica comments.
	ansiEscapeSequenceRe = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)
)

// stripANSIAndControl normalizes a single output line from a command process:
//   - Collapses carriage-return (`\r`) spinner rewrites to the final segment
//   - Removes ANSI escape codes
//   - Drops non-tab control characters below 0x20
func stripANSIAndControl(line string) string {
	line = strings.TrimSuffix(line, "\r")
	if idx := strings.LastIndexByte(line, '\r'); idx >= 0 {
		line = line[idx+1:]
	}
	if strings.ContainsRune(line, '\x1b') {
		line = ansiEscapeSequenceRe.ReplaceAllString(line, "")
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, line)
}

// CommandRuntimeConfig is the per-agent runner configuration stored under
// `agent.runtime_config` for agents bound to the built-in "command" runtime.
type CommandRuntimeConfig struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	WorkingDir     string   `json:"working_dir,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// ParseCommandRuntimeConfig parses and validates a Command Agent's
// `runtime_config` JSON payload.
func ParseCommandRuntimeConfig(raw []byte) (CommandRuntimeConfig, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return CommandRuntimeConfig{}, errors.New("runtime_config.command is required for command agents")
	}
	var cfg CommandRuntimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return CommandRuntimeConfig{}, fmt.Errorf("invalid command runtime_config: %w", err)
	}
	cfg.Command = strings.TrimSpace(cfg.Command)
	if cfg.Command == "" {
		return CommandRuntimeConfig{}, errors.New("runtime_config.command is required for command agents")
	}
	if strings.ContainsRune(cfg.Command, '\x00') {
		return CommandRuntimeConfig{}, errors.New("runtime_config.command cannot contain NUL bytes")
	}
	if strings.ContainsAny(cfg.Command, " \t\r\n") {
		return CommandRuntimeConfig{}, errors.New("runtime_config.command must be a single executable token or path; put arguments in runtime_config.args or custom_args")
	}
	for _, arg := range cfg.Args {
		if strings.ContainsRune(arg, '\x00') {
			return CommandRuntimeConfig{}, errors.New("runtime_config.args entries cannot contain NUL bytes")
		}
	}
	cfg.WorkingDir = strings.TrimSpace(cfg.WorkingDir)
	if strings.ContainsRune(cfg.WorkingDir, '\x00') {
		return CommandRuntimeConfig{}, errors.New("runtime_config.working_dir cannot contain NUL bytes")
	}
	if cfg.TimeoutSeconds < 0 {
		return CommandRuntimeConfig{}, errors.New("runtime_config.timeout_seconds must be non-negative")
	}
	return cfg, nil
}

// ValidateCommandRuntimeConfig validates a Command Agent's `runtime_config` JSON payload.
func ValidateCommandRuntimeConfig(raw []byte) error {
	_, err := ParseCommandRuntimeConfig(raw)
	return err
}

// commandBackend executes a deterministic command or script directly without an
// LLM CLI. Task/trigger context is supplied over stdin (typically as a bounded
// JSON document) and stdout/stderr are recorded into the task transcript with
// zero model token usage.
type commandBackend struct {
	cfg Config
}

type commandStreamRead struct {
	line string
	err  error
	done bool
}

type commandStructuredOutput struct {
	MulticaResult *bool  `json:"multica_result,omitempty"`
	Status        string `json:"status,omitempty"`
	Output        string `json:"output,omitempty"`
	Error         string `json:"error,omitempty"`
}

func parseCommandStructuredLine(line string) (commandStructuredOutput, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return commandStructuredOutput{}, false
	}
	var out commandStructuredOutput
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return commandStructuredOutput{}, false
	}
	status := strings.ToLower(strings.TrimSpace(out.Status))
	switch status {
	case "pass", "passed", "ok", "success":
		status = "completed"
	case "fail", "changes_requested":
		status = "failed"
	case "blocked", "tool_error":
		status = "error"
	}
	hasExplicitMarker := out.MulticaResult != nil && *out.MulticaResult
	hasRecognizedStatus := (status == "completed" || status == "failed" || status == "error") &&
		(out.Output != "" || out.Error != "" || hasExplicitMarker)
	if !hasExplicitMarker && !hasRecognizedStatus {
		return commandStructuredOutput{}, false
	}
	out.Status = status
	return out, true
}

func resolveCommandExecutable(execPath, cwd string) (string, error) {
	execPath = strings.TrimSpace(execPath)
	if execPath == "" {
		return "", errors.New("command executable is required; set runtime_config.command on the agent")
	}
	if strings.ContainsAny(execPath, "/\\") && !filepath.IsAbs(execPath) && strings.TrimSpace(cwd) != "" {
		execPath = filepath.Join(cwd, execPath)
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return "", fmt.Errorf("command executable not found at %q: %w", execPath, err)
	}
	return resolved, nil
}

// isCommandToolExitError reports whether a non-nil cmd.Wait() error represents
// an execution/tool failure (killed by a signal, shell 126 permission denied,
// 127 command not found, or signal exit >= 128) rather than a normal check/CI
// non-zero exit code (1..125).
func isCommandToolExitError(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return true
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return true
	}
	code := exitErr.ExitCode()
	return code < 0 || code >= 126
}

func buildCommandEnv(overrides map[string]string) []string {
	merged := map[string]string{
		"CI":          "true",
		"NO_COLOR":    "1",
		"CLICOLOR":    "0",
		"FORCE_COLOR": "0",
		"TERM":        "dumb",
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return buildEnv(merged)
}

func (b *commandBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath, err := resolveCommandExecutable(b.cfg.ExecutablePath, opts.Cwd)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, len(opts.ExtraArgs)+len(opts.CustomArgs))
	args = append(args, opts.ExtraArgs...)
	args = append(args, opts.CustomArgs...)

	runCtx, cancel := runContext(ctx, opts.Timeout)
	runtimeCmd := b.cfg.commandAt(execPath)
	cmd := runtimeCmd.exec(runCtx, args...)
	hideAgentWindow(cmd)
	cmd.Cancel = func() error { return nil }
	cmd.Dir = opts.Cwd
	cmd.Env = buildCommandEnv(b.cfg.Env)

	b.cfg.logAgentCommandWithPrompt(cmd, newAgentCommandLogArgs(args), len(prompt))

	stderr := newStderrTail(io.Discard, agentStderrTailBytes)
	stdout, outW, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("command stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		return nil, fmt.Errorf("command stderr pipe: %w", err)
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	inR, stdin, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		return nil, fmt.Errorf("command stdin pipe: %w", err)
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
		return nil, fmt.Errorf("start command: %w", err)
	}
	_ = outW.Close()
	_ = errW.Close()
	_ = inR.Close()

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	var terminalObserved atomic.Bool

	waitCh := make(chan error, 1)
	go func(done chan<- error) { done <- cmd.Wait() }(waitCh)

	inputCh := make(chan error, 1)
	go func(done chan<- error) {
		var writeErr error
		if prompt != "" {
			_, writeErr = io.WriteString(stdin, prompt)
		}
		_ = stdin.Close()
		done <- writeErr
	}(inputCh)

	stderrDone := make(chan struct{})
	go func(done chan<- struct{}) {
		_, _ = io.Copy(stderr, errR)
		close(done)
	}(stderrDone)

	reads := make(chan commandStreamRead, 16)
	stopReads := make(chan struct{})
	go readCommandStream(stdout, reads, stopReads)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		started := time.Now()

		var (
			outputBuf    strings.Builder
			lastNonEmpty string
			scanErr      error
			exitErr      error
			inputErr     error
			runErr       error
		)
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
		beginCleanup := func() {
			if cleaning {
				return
			}
			cleaning = true
			if !terminalObserved.Load() {
				runErr = runCtx.Err()
			}
			ctxDone = nil
			if waitCh != nil {
				select {
				case exitErr = <-waitCh:
					waitCh = nil
				default:
				}
			}
			if waitCh == nil && runErr == nil {
				terminalObserved.Store(true)
			}
			signalProcessGroup(cmd, syscall.SIGKILL)
			_ = stdin.Close()
			setDrainDeadline(commandStreamDrainGrace)
		}

		for waitCh != nil || reads != nil || stderrDone != nil || inputCh != nil {
			select {
			case item := <-reads:
				if item.done {
					reads, scanErr = nil, item.err
					if scanErr != nil {
						beginCleanup()
						cancel()
					}
				} else {
					line := stripANSIAndControl(item.line)
					if strings.TrimSpace(line) != "" {
						lastNonEmpty = line
					}
					if outputBuf.Len() < commandMaxStdoutBytes {
						if outputBuf.Len() > 0 {
							outputBuf.WriteByte('\n')
						}
						remaining := commandMaxStdoutBytes - outputBuf.Len()
						if len(line) > remaining {
							outputBuf.WriteString(line[:remaining])
						} else {
							outputBuf.WriteString(line)
						}
					}
					// Always include trailing newline on MessageText so the
					// daemon's 500ms transcript coalescer preserves line breaks
					// in Markdown output.
					select {
					case msgCh <- Message{Type: MessageText, Content: line + "\n"}:
					default:
					}
				}
			case exitErr = <-waitCh:
				waitCh = nil
				if runCtx.Err() == nil {
					terminalObserved.Store(true)
				}
				if !cleaning {
					setDrainDeadline(commandStreamDrainGrace)
				}
			case inputErr = <-inputCh:
				inputCh = nil
			case <-stderrDone:
				stderrDone = nil
			case <-ctxDone:
				beginCleanup()
			case <-drainC:
				if cleaning {
					if waitCh != nil {
						exitErr = fmt.Errorf("process did not exit during cleanup")
					}
					waitCh, reads, stderrDone, inputCh = nil, nil, nil, nil
				} else {
					beginCleanup()
				}
			}
			if waitCh == nil && reads == nil {
				beginCleanup()
			}
		}

		close(stopReads)
		_ = stdout.Close()
		_ = errR.Close()
		_ = stdin.Close()
		releaseProcessGroup(cmd)

		if terminalObserved.Load() && (errors.Is(exitErr, context.Canceled) || errors.Is(exitErr, context.DeadlineExceeded)) {
			exitErr = nil
		}

		rawOutput := strings.TrimSpace(outputBuf.String())
		cleanStderr := strings.TrimSpace(stripANSIAndControl(stderr.Tail()))
		structured, hasStructured := parseCommandStructuredLine(lastNonEmpty)

		status := "completed"
		finalOutput := rawOutput
		var errText string

		switch {
		case errors.Is(runErr, context.DeadlineExceeded):
			status = "timeout"
			finalOutput = ""
			if opts.Timeout > 0 {
				errText = fmt.Sprintf("command timed out after %s", opts.Timeout)
			} else {
				errText = "command timed out"
			}
		case errors.Is(runErr, context.Canceled):
			status = "aborted"
			finalOutput = ""
			errText = "command execution cancelled"
		case scanErr != nil:
			status = "failed"
			finalOutput = ""
			errText = fmt.Sprintf("read command output: %v", scanErr)
		case hasStructured && structured.Status == "error":
			status = "failed"
			finalOutput = ""
			if structured.Error != "" {
				errText = structured.Error
			} else if structured.Output != "" {
				errText = structured.Output
			} else {
				errText = "command reported tool error"
			}
		case hasStructured && structured.Status == "failed":
			// Structured check failure (routes to BackStatusKey in workflow).
			status = "check_failed"
			if structured.Output != "" {
				finalOutput = structured.Output
			} else if structured.Error != "" {
				finalOutput = structured.Error
			} else {
				finalOutput = rawOutput
			}
			if structured.Error != "" {
				errText = structured.Error
			} else {
				errText = "command reported check failure"
			}
		case exitErr != nil:
			// Distinguish a normal check/review non-zero exit (exit 1..125 with
			// a stdout report) from a tool/process error (signal crash, exit
			// 126/127, or stderr-only crash with no stdout report).
			if !isCommandToolExitError(exitErr) && rawOutput != "" {
				status = "check_failed"
				finalOutput = rawOutput
				if cleanStderr != "" && !strings.Contains(finalOutput, cleanStderr) {
					finalOutput = finalOutput + "\n\n<details>\n<summary>Command stderr</summary>\n\n```\n" +
						sanitizeAgentDiagnostic(cleanStderr) + "\n```\n</details>"
				}
				errText = exitErr.Error()
			} else {
				status = "failed"
				finalOutput = ""
				errText = exitErr.Error()
			}
		case hasStructured:
			if structured.Output != "" || structured.MulticaResult != nil {
				finalOutput = structured.Output
			}
		default:
			_ = inputErr
		}

		if status == "failed" || status == "timeout" {
			tail := cleanStderr
			if tail == "" && rawOutput != "" && !hasStructured {
				tail = rawOutput
			}
			errText = sanitizeAgentDiagnostic(withAgentStderr(errText, "command", tail))
		} else if errText != "" {
			errText = sanitizeAgentDiagnostic(errText)
		}

		resCh <- Result{
			Status:     status,
			Output:     finalOutput,
			Error:      errText,
			DurationMs: time.Since(started).Milliseconds(),
			Usage:      nil, // Deterministic command executions consume zero LLM tokens.
		}
	}()

	return &Session{
		CleanupRequired:  true,
		Messages:         msgCh,
		Result:           resCh,
		TerminalObserved: terminalObserved.Load,
	}, nil
}

func readCommandStream(r io.Reader, out chan<- commandStreamRead, stop <-chan struct{}) {
	reader := bufio.NewReaderSize(r, 64*1024)
	send := func(item commandStreamRead) bool {
		select {
		case out <- item:
			return true
		case <-stop:
			return false
		}
	}
	for {
		line, err := readAgentStreamLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				send(commandStreamRead{done: true})
				return
			}
			send(commandStreamRead{err: err, done: true})
			return
		}
		if !send(commandStreamRead{line: string(line)}) {
			return
		}
	}
}
