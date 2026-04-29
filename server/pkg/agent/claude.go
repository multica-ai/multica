package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// claudeBackend implements Backend by spawning the Claude Code CLI
// with --output-format stream-json.
type claudeBackend struct {
	cfg Config
}

func (b *claudeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "claude"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	// If the caller provided an MCP config, write it to a temp file; the
	// exclusivity contract (use ONLY those servers) is enforced inside
	// buildClaudeArgs by pairing --mcp-config with --strict-mcp-config.
	// With no per-agent config, the child inherits account-level MCPs from
	// the outer Claude Code session — this is what surfaces connectors like
	// claude.ai Figma.
	var mcpConfigPath string
	var mcpFileCleanup func() // non-nil while this function owns the temp file
	if len(opts.McpConfig) > 0 {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			cancel()
			return nil, err
		}
		mcpConfigPath = path
		mcpFileCleanup = func() { os.Remove(mcpConfigPath) }
	}
	args := buildClaudeArgs(opts, mcpConfigPath, b.cfg.Logger)
	// Clean up the temp file if we return before the goroutine takes ownership.
	defer func() {
		if mcpFileCleanup != nil {
			mcpFileCleanup()
		}
	}()

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
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
			stdin = nil
		}
	}
	// Capture stderr into both the daemon log (as before) and a bounded tail
	// buffer so we can include the last few KB in Result.Error when claude
	// exits unexpectedly. Without the tail, an exit-code-only failure looks
	// like "claude exited with error: exit status 3" — which is useless for
	// root-causing V8 aborts, Bun panics, or any other CLI-side crash.
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}
	if err := writeClaudeInput(stdin, prompt); err != nil {
		// claude almost certainly died during startup (broken pipe). The
		// real reason is sitting in stderrBuf — surface it the same way the
		// post-handshake error path does, otherwise the daemon log is the
		// only place that knows whether it was a V8 abort, a missing native
		// module, or anything else. cmd.Wait() flushes os/exec's stderr
		// copy goroutine, so stderrBuf.Tail() is safe to read.
		closeStdin()
		cancel()
		_ = cmd.Wait()
		return nil, errors.New(withAgentStderr(fmt.Sprintf("write claude input: %v", err), "claude", stderrBuf.Tail()))
	}
	closeStdin()

	b.cfg.Logger.Info("claude started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	// cmd.Start() succeeded — transfer temp file ownership to the goroutine.
	mcpFileCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer os.Remove(mcpConfigPath)
		}

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		// Close stdout when the context is cancelled so scanner.Scan() unblocks.
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

			var msg claudeSDKMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "assistant":
				b.handleAssistant(msg, msgCh, &output, usage)
			case "user":
				b.handleUser(msg, msgCh)
			case "system":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				closeStdin()
				sessionID = msg.SessionID
				if msg.ResultText != "" {
					output.Reset()
					output.WriteString(msg.ResultText)
				}
				if msg.IsError {
					finalStatus = "failed"
					finalError = msg.ResultText
				}
			case "log":
				if msg.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   msg.Log.Level,
						Content: msg.Log.Message,
					})
				}
			}
		}

		// Wait for process exit
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("claude timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("claude exited with error: %v", exitErr)
		}

		// cmd.Wait() has returned — os/exec's stderr copy goroutine has
		// observed every byte claude wrote to stderr before exiting, so
		// stderrBuf.Tail() is safe to sample now. Attach the tail to any
		// non-empty failure message; callers upstream surface this as the
		// task's error field, which is the only place users see it.
		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("claude finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed")
		if reportedSessionID != sessionID {
			b.cfg.Logger.Info("claude resume did not land; clearing fresh session id for daemon fallback",
				"requested_resume", opts.ResumeSessionID,
				"emitted_session", sessionID,
			)
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  reportedSessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *claudeBackend) handleAssistant(msg claudeSDKMessage, ch chan<- Message, output *strings.Builder, usage map[string]TokenUsage) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

	// Accumulate token usage per model.
	if content.Usage != nil && content.Model != "" {
		u := usage[content.Model]
		u.InputTokens += content.Usage.InputTokens
		u.OutputTokens += content.Usage.OutputTokens
		u.CacheReadTokens += content.Usage.CacheReadInputTokens
		u.CacheWriteTokens += content.Usage.CacheCreationInputTokens
		usage[content.Model] = u
	}

	for _, block := range content.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   block.Name,
				CallID: block.ID,
				Input:  input,
			})
		}
	}
}

func (b *claudeBackend) handleUser(msg claudeSDKMessage, ch chan<- Message) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}

	for _, block := range content.Content {
		if block.Type == "tool_result" {
			resultStr := ""
			if block.Content != nil {
				resultStr = string(block.Content)
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: block.ToolUseID,
				Output: resultStr,
			})
		}
	}
}

func (b *claudeBackend) handleControlRequest(msg claudeSDKMessage, stdin interface{ Write([]byte) (int, error) }) {
	// Auto-approve all tool uses in autonomous/daemon mode.
	var req claudeControlRequestPayload
	if err := json.Unmarshal(msg.Request, &req); err != nil {
		return
	}

	var inputMap map[string]any
	if req.Input != nil {
		_ = json.Unmarshal(req.Input, &inputMap)
	}
	if inputMap == nil {
		inputMap = map[string]any{}
	}

	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": msg.RequestID,
			"response": map[string]any{
				"behavior":     "allow",
				"updatedInput": inputMap,
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		b.cfg.Logger.Warn("claude: failed to marshal control response", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := stdin.Write(data); err != nil {
		b.cfg.Logger.Warn("claude: failed to write control response", "error", err)
	}
}

// ── Claude SDK JSON types ──

type claudeSDKMessage struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`

	// result fields
	ResultText string  `json:"result,omitempty"`
	IsError    bool    `json:"is_error,omitempty"`
	DurationMs float64 `json:"duration_ms,omitempty"`
	NumTurns   int     `json:"num_turns,omitempty"`

	// log fields
	Log *claudeLogEntry `json:"log,omitempty"`

	// control request fields
	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}

type claudeLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type claudeMessageContent struct {
	Role    string               `json:"role"`
	Model   string               `json:"model"`
	Content []claudeContentBlock `json:"content"`
	Usage   *claudeUsage         `json:"usage,omitempty"`
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type claudeControlRequestPayload struct {
	Subtype  string          `json:"subtype"`
	ToolName string          `json:"tool_name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

// ── Shared helpers ──

func trySend(ch chan<- Message, msg Message) {
	select {
	case ch <- msg:
	default:
		// Channel full — drop message. Final output is accumulated separately
		// in Result.Output, so only streaming consumers are affected.
	}
}

// claudeBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// the daemon↔Claude communication protocol.
var claudeBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedStandalone, // non-interactive mode
	"--output-format":   blockedWithValue,  // stream-json protocol
	"--input-format":    blockedWithValue,  // stream-json protocol
	"--permission-mode": blockedWithValue,  // bypassPermissions for autonomous operation
	"--mcp-config":      blockedWithValue,  // set by daemon from agent.mcp_config
}

// buildClaudeArgs produces the CLI args for the Claude child process.
//
// The mcp-config branch preserves the exclusivity contract from #1168:
// when an explicit per-agent MCP config is provided, we also pass
// --strict-mcp-config so the child sees ONLY those servers and does not
// inherit account-level MCPs from the outer Claude Code session. When no
// per-agent config is provided, --strict-mcp-config is omitted and the
// child falls back to its normal account-level MCP list (Figma, etc.).
// Pass mcpConfigPath="" to take the account-level path.
func buildClaudeArgs(opts ExecOptions, mcpConfigPath string, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	if mcpConfigPath != "" {
		args = append(args, "--mcp-config", mcpConfigPath, "--strict-mcp-config")
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, claudeBlockedArgs, logger)...)
	return args
}

func writeClaudeInput(w io.Writer, prompt string) error {
	data, err := buildClaudeInput(prompt)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

func buildClaudeInput(prompt string) ([]byte, error) {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]string{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal claude input: %w", err)
	}
	return append(data, '\n'), nil
}

// resolveSessionID decides which session id to report on the Result. When the
// caller requested --resume but claude emitted a fresh, different session id
// AND the run failed, the resume did not land (claude prints
// "No conversation found with session ID: ..." to stderr, generates a fresh
// session, and exits). Returning "" in that case keeps the daemon's
// retry-with-fresh-session fallback able to trigger, instead of silently
// persisting a brand-new id as if resume had succeeded.
func resolveSessionID(requestedResume, emitted string, failed bool) string {
	if failed && requestedResume != "" && emitted != "" && emitted != requestedResume {
		return ""
	}
	return emitted
}

func buildEnv(extra map[string]string) []string {
	return mergeEnv(os.Environ(), extra)
}

func mergeEnv(base []string, extra map[string]string) []string {
	env := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if isFilteredChildEnvKey(key) {
			continue
		}
		env = append(env, entry)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func isFilteredChildEnvKey(key string) bool {
	return key == "CLAUDECODE" ||
		strings.HasPrefix(key, "CLAUDECODE_") ||
		strings.HasPrefix(key, "CLAUDE_CODE_")
}

// blockedArgMode specifies whether a blocked arg takes a value or is standalone.
type blockedArgMode int

const (
	blockedWithValue  blockedArgMode = iota // flag takes a value (next arg or =value)
	blockedStandalone                       // flag is boolean, no value
)

// filterCustomArgs removes protocol-critical flags from user-configured custom
// args to prevent breaking daemon↔agent communication. Each backend defines its
// own blocked set (the flags it hardcodes). This is intentionally narrow — we
// only block args that would break the communication protocol, not every
// possible dangerous flag. Workspace members are trusted to configure agents
// sensibly, same as with custom_env.
func filterCustomArgs(args []string, blocked map[string]blockedArgMode, logger *slog.Logger) []string {
	if len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	skip := false
	for _, arg := range args {
		if skip {
			skip = false
			continue
		}
		// Check if this arg is a blocked flag or starts with "blockedFlag=".
		flag := arg
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			hasInlineValue = true
		}
		mode, isBlocked := blocked[flag]
		if isBlocked {
			logger.Warn("custom_args: blocked protocol-critical flag, skipping", "flag", flag)
			if mode == blockedWithValue && !hasInlineValue {
				// The next arg is the value for this flag — skip it too.
				skip = true
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// writeMcpConfigToTemp writes raw MCP config JSON to a temporary file and returns
// its path. The caller is responsible for removing the file when done.
func writeMcpConfigToTemp(raw json.RawMessage) (string, error) {
	f, err := os.CreateTemp("", "multica-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("create mcp config temp file: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write mcp config temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close mcp config temp file: %w", err)
	}
	return f.Name(), nil
}

func detectCLIVersion(ctx context.Context, execPath string) (string, error) {
	cmd := exec.CommandContext(ctx, execPath, "--version")
	hideAgentWindow(cmd)
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect version for %s: %w", execPath, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// logWriter adapts a *slog.Logger to an io.Writer for capturing stderr.
type logWriter struct {
	logger *slog.Logger
	prefix string
}

func newLogWriter(logger *slog.Logger, prefix string) *logWriter {
	return &logWriter{logger: logger, prefix: prefix}
}

func (w *logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text != "" {
		w.logger.Debug(w.prefix + text)
	}
	return len(p), nil
}
