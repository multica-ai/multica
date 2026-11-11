package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Droid's one-shot stream-json protocol is distinct from Claude's: assistant
// text is a message event, tools use camelCase fields, and completion is final.
// Protocol fields were checked against the official Droid 1.8.0 executable.
type droidBackend struct{ cfg Config }

var droidBlockedArgs = map[string]blockedArgMode{
	"exec":                        blockedStandalone,
	"--":                          blockedStandalone,
	"-o":                          blockedWithValue,
	"--output-format":             blockedWithValue,
	"--input-format":              blockedWithValue,
	"-f":                          blockedWithValue,
	"--file":                      blockedWithValue,
	"--auto":                      blockedWithValue,
	"--skip-permissions-unsafe":   blockedStandalone,
	"--cwd":                       blockedWithValue,
	"-m":                          blockedWithValue,
	"--model":                     blockedWithValue,
	"-s":                          blockedWithValue,
	"--session-id":                blockedWithValue,
	"--fork":                      blockedWithValue,
	"--append-system-prompt":      blockedWithValue,
	"--append-system-prompt-file": blockedWithValue,
	"-w":                          blockedOptionalValue,
	"--worktree":                  blockedOptionalValue,
	"--worktree-dir":              blockedWithValue,
	"--mission":                   blockedStandalone,
	"--list-tools":                blockedStandalone,
	"-h":                          blockedStandalone,
	"--help":                      blockedStandalone,
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

func buildDroidArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"exec", "--output-format", "stream-json", "--auto", "high"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session-id", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(normalizeDroidArgs(opts.ExtraArgs), droidBlockedArgs, logger)...)
	return append(args, filterCustomArgs(normalizeDroidArgs(opts.CustomArgs), droidBlockedArgs, logger)...)
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
	runCtx, cancel := runContext(ctx, opts.Timeout)
	args := buildDroidArgs(opts, b.cfg.Logger)
	runtimeCmd := b.cfg.commandAt(execPath)
	runtimeCmd.Prefix = FilterLaunchPrefix("droid", normalizeDroidArgs(runtimeCmd.Prefix), b.cfg.Logger)
	cmd := runtimeCmd.exec(runCtx, args...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = opts.Cwd
	cmd.Env = buildEnv(b.cfg.Env)
	// Stdin avoids OS command-line size/quoting limits and keeps prompts out of
	// process listings. Droid reads to EOF before executing the task.
	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}
	cmd.Stdin = strings.NewReader(prompt)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "exec")))
	// Keep raw stderr out of daemon logs. Publish only a scrubbed diagnostic
	// after Wait, which also joins the stderr copier before reading the tail.
	stderr := newStderrTail(io.Discard, agentStderrTailBytes)
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("droid stdout pipe: %w", err)
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		_ = stdout.Close()
		return nil, fmt.Errorf("start droid: %w", err)
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("droid does not support max_turns; use the task timeout to bound execution")
	}
	scrub := droidOutputScrubber(b.cfg.Env)
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		started := time.Now()
		state := droidStreamState{model: opts.Model}
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var event droidStreamEvent
			if json.Unmarshal([]byte(scrub(line)), &event) != nil {
				continue
			}
			handleDroidEvent(event, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			// A protocol read failure must also stop the producer, which may
			// otherwise remain blocked writing an oversized event forever.
			cancel()
		}
		exitErr := cmd.Wait()
		// Wait ends CommandContext's watcher. Explicitly stop remaining tools
		// even when Droid exits normally and they have closed their streams.
		signalProcessGroup(cmd, syscall.SIGKILL)
		releaseProcessGroup(cmd)
		terminal := streamTerminalState{
			sawResult: state.sawCompletion, finalResultText: state.finalText,
			resultIsError: state.failed, scanErr: scanErr,
		}
		if state.failed {
			terminal.finalResultText = state.errorText
		}
		runErr := runCtx.Err()
		// A scanner failure cancelled the process for cleanup, not because
		// the user cancelled the task.
		if scanErr != nil && runErr == context.Canceled && ctx.Err() == nil {
			runErr = nil
		}
		status, output, errText := finalizeStreamResult("droid", opts.Timeout, runErr, nil, exitErr, state.sessionID, terminal, "")
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
		sessionID := state.sessionID
		if sessionID == "" {
			sessionID = opts.ResumeSessionID
		}
		resCh <- Result{
			Status: status, Output: output, Error: errText,
			DurationMs: time.Since(started).Milliseconds(), Usage: usage,
			// Without a captured resume rejection, retain the requested pointer
			// on failure and never trigger a fresh-session retry based on prose.
			SessionID: sessionID,
		}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
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
