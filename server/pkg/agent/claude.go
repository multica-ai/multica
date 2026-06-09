package agent

// CEREBRO-PATCH(agent-claude-cerebro): cerebro modification of upstream file

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	runCtx, cancel := runContext(ctx, timeout)

	args := buildClaudeArgs(opts, b.cfg.Logger)

	// If the caller provided an MCP config, write it to a temp file and pass
	// --mcp-config <path> so the agent uses a controlled set of MCP servers
	// instead of inheriting from the outer Claude Code session.
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
		args = append(args, "--mcp-config", mcpConfigPath)
	}
	// Clean up the temp file if we return before the goroutine takes ownership.
	defer func() {
		if mcpFileCleanup != nil {
			mcpFileCleanup()
		}
	}()

	// CEREBRO-PATCH(agent-claude-sandbox): prepareCommand wraps the agent in
	// the daemon sandbox when enabled; falls back to plain exec.CommandContext.
	cmd, sandboxCleanup, err := prepareCommand(runCtx, execPath, args, b.cfg.Sandbox, opts.Cwd, b.cfg.Logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("prepare claude command: %w", err)
	}
	sandboxOwned := true
	defer func() {
		if sandboxOwned {
			sandboxCleanup()
		}
	}()
	hideAgentWindow(cmd)
	b.cfg.Logger.Debug("agent command", "exec", execPath, "args", args)
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

	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	// CEREBRO-PATCH(agent-claude-stderr-tail-cap): claude needs a larger 64KB
	// stderr tail cap (vs upstream's 2KB default) for empty-output diagnostics
	// (JEH-405) — sandbox denials, missing auth, and model rejects often
	// produce more than 2KB of trailing stderr context.
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), stderrTailBytes)

	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	b.cfg.Logger.Info("claude started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	// cmd.Start() succeeded — transfer temp file ownership to the goroutine.
	mcpFileCleanup = nil
	sandboxOwned = false

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// writeClaudeInput runs in its own goroutine so it cannot deadlock
	// against the stdout reader. With --verbose --output-format stream-json
	// the CLI emits a startup banner before reading its first stdin frame;
	// if nothing is draining stdout while we write the prompt, claude blocks
	// writing stdout, never reads stdin, and our Write blocks until runCtx
	// fires. The field symptom is "write |1: The pipe has been ended."
	// surfacing exactly at the per-task timeout when the kill invalidates
	// the still-blocked pipe.
	//
	// Keep stdin open after the initial user message. Claude's stream-json
	// protocol can emit control_request events mid-run and expects matching
	// control_response frames on the same input stream; closing stdin here
	// leaves the child stuck waiting for a response until its own fallback
	// timeout.
	writeDone := make(chan error, 1)
	go func() {
		err := writeClaudeInput(stdin, prompt)
		if err != nil {
			closeStdin()
		}
		writeDone <- err
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer sandboxCleanup()
		if mcpConfigPath != "" {
			defer os.Remove(mcpConfigPath)
		}

		startTime := time.Now()
		var output strings.Builder
		// CEREBRO-PATCH(agent-claude-logs): JEH-1365 — accumulate verbose log messages
		// so the daemon can parse rate-limit/quota signals from the stream (not just
		// the final result text). Anthropic headers like x-ratelimit-* are logged here
		// by Claude Code in --verbose mode and not included in the final output.
		var logs strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)
		// Diagnostics for the empty-output fail mode (JEH-405): count how
		// many lines we couldn't parse and remember the first one. A surge
		// of unparseable lines almost always means the claude stream-json
		// schema drifted or sandbox-exec wrote a banner over stdout.
		var unparseableCount int
		var firstUnparseable string

		// Close stdout when the context is cancelled so scanner.Scan() unblocks.
		go func() {
			<-runCtx.Done()
			closeStdin()
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
				unparseableCount++
				if firstUnparseable == "" {
					firstUnparseable = truncate(line, 200)
					b.cfg.Logger.Warn("claude: unparseable stdout line",
						"error", err,
						"line", firstUnparseable,
					)
				}
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
				sessionID = msg.SessionID
				if msg.ResultText != "" {
					output.Reset()
					output.WriteString(msg.ResultText)
				}
				if resultUsage := claudeResultUsage(msg, opts.Model); len(resultUsage) > 0 {
					usage = resultUsage
				}
				if msg.IsError {
					finalStatus = "failed"
					finalError = msg.ResultText
				}
				closeStdin()
			case "log":
				if msg.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   msg.Log.Level,
						Content: msg.Log.Message,
					})
					// CEREBRO-PATCH(agent-claude-logs): JEH-1365 — accumulate log
					// content for quota-signal parsing after the run completes.
					if msg.Log.Message != "" {
						logs.WriteString(msg.Log.Message)
						logs.WriteByte('\n')
					}
				}
			case "control_request":
				b.handleControlRequest(msg, stdin)
			}
		}

		closeStdin()

		// Wait for process exit
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		exitCode := -1
		if exitErr == nil {
			exitCode = 0
		} else if ee, ok := exitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}

		// writeDone is buffered (cap 1) and the writer always sends — by the
		// time cmd has exited, the prompt write has either succeeded, hit a
		// broken pipe, or been unblocked by the kill that ended cmd.
		writeErr := <-writeDone


		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			finalStatus = "timeout"
			finalError = fmt.Sprintf("claude timed out after %s", timeout)
		case runCtx.Err() == context.Canceled:
			finalStatus = "aborted"
			finalError = "execution cancelled"
		case writeErr != nil && finalStatus == "completed" && sessionID == "":
			// No result event landed and the prompt write failed — claude
			// died before reading the prompt. Surface the write error; the
			// stderr tail attached below carries the real reason.
			finalStatus = "failed"
			finalError = fmt.Sprintf("write claude input: %v", writeErr)
		case exitErr != nil && finalStatus == "completed":
			finalStatus = "failed"
			finalError = fmt.Sprintf("claude exited with error: %v", exitErr)
		}

		// CEREBRO-PATCH(agent-claude-empty-output-diagnostic): empty-output
		// diagnostic (JEH-405). A clean exit with no stdout is otherwise
		// reported as "claude returned empty output" with no reason. Surface
		// the actual signals (exit code, stderr tail, unparseable line count)
		// so the daemon can forward them in the task comment instead of the
		// operator having to ssh into the host and grep daemon.err.log.
		if finalStatus == "completed" && output.Len() == 0 {
			finalError = formatEmptyOutputReason(exitCode, unparseableCount, firstUnparseable, stderrBuf.Tail())
		}
		// CEREBRO-PATCH(agent-claude-provider-limit-output): Claude Code can
		// emit provider quota exhaustion as a normal assistant text turn and
		// exit 0. Treat a short standalone limit message as task failure so the
		// runtime auto-pause path sees the error instead of posting it as a
		// successful agent reply.
		if finalStatus == "completed" && finalError == "" && isClaudeProviderLimitOutput(output.String()) {
			finalStatus = "failed"
			finalError = output.String()
		}

		// cmd.Wait() has returned — os/exec's stderr copy goroutine has
		// observed every byte claude wrote to stderr before exiting, so
		// stderrBuf.Tail() is safe to sample now. Attach the tail to any
		// non-empty failure message; callers upstream surface this as the
		// task's error field, which is the only place users see it.
		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("claude finished",
			"pid", cmd.Process.Pid,
			"status", finalStatus,
			"duration", duration.Round(time.Millisecond).String(),
			"exit_code", exitCode,
			"unparseable_lines", unparseableCount,
		)

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
			Logs:       logs.String(), // CEREBRO-PATCH(agent-claude-logs): JEH-1365
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func isClaudeProviderLimitOutput(output string) bool {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || len(trimmed) > 500 {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "org's monthly usage limit") ||
		strings.Contains(lower, "monthly usage limit")
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
	Model     string          `json:"model,omitempty"`

	// result fields
	ResultText string                            `json:"result,omitempty"`
	IsError    bool                              `json:"is_error,omitempty"`
	DurationMs float64                           `json:"duration_ms,omitempty"`
	NumTurns   int                               `json:"num_turns,omitempty"`
	Usage      *claudeUsage                      `json:"usage,omitempty"`
	ModelUsage map[string]claudeResultModelUsage `json:"modelUsage,omitempty"`

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

type claudeResultModelUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

func claudeResultUsage(msg claudeSDKMessage, fallbackModel string) map[string]TokenUsage {
	if len(msg.ModelUsage) > 0 {
		usage := make(map[string]TokenUsage, len(msg.ModelUsage))
		for model, u := range msg.ModelUsage {
			if model == "" || !claudeUsageHasTokens(u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens) {
				continue
			}
			usage[model] = TokenUsage{
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				CacheReadTokens:  u.CacheReadInputTokens,
				CacheWriteTokens: u.CacheCreationInputTokens,
			}
		}
		if len(usage) > 0 {
			return usage
		}
	}

	model := msg.Model
	if model == "" {
		model = fallbackModel
	}
	if msg.Usage == nil || model == "" || !claudeUsageHasTokens(
		msg.Usage.InputTokens,
		msg.Usage.OutputTokens,
		msg.Usage.CacheReadInputTokens,
		msg.Usage.CacheCreationInputTokens,
	) {
		return nil
	}
	return map[string]TokenUsage{
		model: {
			InputTokens:      msg.Usage.InputTokens,
			OutputTokens:     msg.Usage.OutputTokens,
			CacheReadTokens:  msg.Usage.CacheReadInputTokens,
			CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		},
	}
}

func claudeUsageHasTokens(input, output, cacheRead, cacheWrite int64) bool {
	return input > 0 || output > 0 || cacheRead > 0 || cacheWrite > 0
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
	// `--effort` is owned by the per-agent thinking_level picker so a
	// user-supplied custom_arg cannot silently outvote it. The daemon
	// injects --effort only when opts.ThinkingLevel is set; if a user
	// nevertheless writes it in custom_args we drop the duplicate and
	// log a warning rather than letting the CLI receive two conflicting
	// --effort values.
	"--effort": blockedWithValue,
}

func buildClaudeArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
		"--permission-mode", "bypassPermissions",
		// AskUserQuestion is Claude Code's built-in interactive question tool.
		// The daemon runs Claude in non-interactive stream-json mode and has
		// no UI for the prompt to render in, so a call returns an empty
		// answer and the agent ends up "inferring" silently — the user
		// never sees the question (see GitHub #2588). User-facing
		// clarification belongs in an issue comment instead.
		"--disallowedTools", "AskUserQuestion",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		// Slotted right after --model so the per-session effort runs
		// against the same model selection the args advertise; the CLI
		// itself accepts the flag in any order but this ordering makes
		// the launch line readable in `agent command` logs.
		args = append(args, "--effort", opts.ThinkingLevel)
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
	args = append(args, filterCustomArgs(opts.ExtraArgs, claudeBlockedArgs, logger)...)
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
		if isFilteredChildEnvKey(key) || isStrippedClaudeAuth(key) { // CEREBRO-PATCH(claude-oauth-strips-api-key)
			continue
		}
		env = append(env, entry)
	}
	for k, v := range extra {
		if isStrippedClaudeAuth(k) { // CEREBRO-PATCH(claude-oauth-strips-api-key)
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
}


// isStrippedClaudeAuth — CEREBRO-PATCH(claude-oauth-strips-api-key): Cerebro Claude-agenter
// kører altid OAuth via CLAUDE_CONFIG_DIR. ANTHROPIC_API_KEY/AUTH_TOKEN må aldrig
// arves fra host-env eller injiceres via CustomEnv — det ville få Claude Code til
// at bruge API-key i stedet for OAuth. Undtagelse: en cloud-runtime kan sætte
// MULTICA_RUNTIME_ALLOW_API_KEY for at vælge API-key-auth pr. runtime (opt-in).
func isStrippedClaudeAuth(key string) bool {
	if !isClaudeAuthKey(key) {
		return false
	}
	return os.Getenv("MULTICA_RUNTIME_ALLOW_API_KEY") == "" // CEREBRO-PATCH(claude-oauth-strips-api-key): runtime opt-in
}

func isClaudeAuthKey(key string) bool {
	return key == "ANTHROPIC_API_KEY" || key == "ANTHROPIC_AUTH_TOKEN"
}


// isFilteredChildEnvKey reports whether an inherited env var is an internal
// Claude Code runtime/session marker that must NOT leak into the spawned child
// (otherwise the child mistakes itself for a nested or resumed session, or
// inherits the parent's exec path / transport).
//
// It must NOT strip the user-facing CLAUDE_CODE_* configuration namespace
// (CLAUDE_CODE_GIT_BASH_PATH, CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_USE_VERTEX,
// CLAUDE_CODE_MAX_OUTPUT_TOKENS, CLAUDE_CODE_TMPDIR, ...): users set those
// deliberately and the child needs them. Blanket-stripping the whole prefix is
// what broke Windows — CLAUDE_CODE_GIT_BASH_PATH was silently removed, so Claude
// Code could not find bash.exe and exited immediately. Strip internal markers by
// exact name and let every other CLAUDE_CODE_* var through.
//
// The denylist holds only undocumented, per-process runtime markers. Anything in
// the public env-vars reference (https://code.claude.com/docs/en/env-vars) is
// user config and stays out of this list — including CLAUDE_CODE_TMPDIR, a
// documented temp-dir override under which Claude Code creates its own
// per-session subdir, so inheriting it is harmless.

func isFilteredChildEnvKey(key string) bool {
	switch key {
	case "CLAUDECODE", // "1" when running inside Claude Code
		"CLAUDE_CODE_ENTRYPOINT", // entrypoint marker (cli/sdk-cli/...)
		"CLAUDE_CODE_EXECPATH",   // path to the running CLI binary
		"CLAUDE_CODE_SESSION_ID", // per-session identifier
		"CLAUDE_CODE_SSE_PORT":   // IDE-extension transport port
		return true
	}
	// CLAUDECODE_* (no underscore between CLAUDE and CODE) is wholly internal;
	// keep stripping it. The user-facing config namespace is CLAUDE_CODE_*.
	return strings.HasPrefix(key, "CLAUDECODE_")
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
//
// Shell quoting is stripped from each arg before processing: users commonly
// type custom_args in config fields using shell syntax (e.g.
// --deny-tool='write'). Since the daemon spawns processes directly without a
// shell, those quotes would otherwise be passed literally to the child process,
// which typically rejects them as unrecognised flag values.
func filterCustomArgs(args []string, blocked map[string]blockedArgMode, logger *slog.Logger) []string {
	if len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	skip := false
	for _, raw := range args {
		if skip {
			skip = false
			continue
		}
		arg := unshellQuoteArg(raw)
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

// unshellQuoteArg strips a single layer of shell-style single or double quotes
// from an argument. It handles two forms:
//
//   - --flag='value' or --flag="value" → --flag=value
//   - 'standalone' or "standalone"     → standalone
//
// Only flag-style args (`-x=…`, `--flag=…`) get inline value unquoting. Plain
// assignment syntax like `model="o3"` is left alone because the quotes may be
// semantic for the child process (for example Codex `-c model="o3"`). Only
// matching outer quotes are stripped; no escape processing is done.
func unshellQuoteArg(arg string) string {
	if strings.HasPrefix(arg, "-") {
		if idx := strings.Index(arg, "="); idx > 0 {
			value := arg[idx+1:]
			if unquoted, ok := stripSurroundingQuotes(value); ok {
				return arg[:idx+1] + unquoted
			}
			return arg
		}
	}
	if unquoted, ok := stripSurroundingQuotes(arg); ok {
		return unquoted
	}
	return arg
}

// stripSurroundingQuotes removes a matching outer pair of single or double
// quotes from s and returns (unquoted, true). Returns (s, false) if s does not
// start and end with the same quote character.
func stripSurroundingQuotes(s string) (string, bool) {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1], true
		}
	}
	return s, false
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
	return extractVersionLine(string(data)), nil
}

// extractVersionLine pulls the version line out of a `<cli> --version` capture,
// discarding leading shell noise. On Windows, npm-installed CLI shims (notably
// gemini's) emit `chcp` output like `Active code page: 65001` before the real
// version reaches stdout, and the raw concatenation was being persisted as the
// runtime version (see #2516).
//
// The heuristic: return the first non-empty line that contains a semver-shaped
// token (matches versionRe). Full version strings like "2.1.5 (Claude Code)"
// or "codex-cli 0.118.0" survive unchanged because the whole matching line is
// returned. If no line carries a semver token, fall back to the trimmed raw
// output so unusual version formats aren't silently dropped to empty.
func extractVersionLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if versionRe.MatchString(line) {
			return line
		}
	}
	return strings.TrimSpace(raw)
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

// stderrTailBytes is the cap on the ring buffer that captures claude's
// stderr. 64 KiB comfortably holds the last screen-or-two of output —
// enough to quote the actual error message without ballooning task
// comments stored on the server.
const stderrTailBytes = 64 * 1024

// ringBuffer is a goroutine-safe writer that retains only the last
// `cap` bytes written to it. Used to keep the tail of a long-running
// process's stderr around for diagnostic quoting.
type ringBuffer struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{cap: cap, buf: make([]byte, 0, cap)}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.cap {
		r.buf = append(r.buf[:0], p[len(p)-r.cap:]...)
		return len(p), nil
	}
	if len(r.buf)+len(p) > r.cap {
		drop := len(r.buf) + len(p) - r.cap
		r.buf = append(r.buf[:0], r.buf[drop:]...)
	}
	r.buf = append(r.buf, p...)
	return len(p), nil
}

func (r *ringBuffer) Snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(string(r.buf))
}

// formatEmptyOutputReason composes the diagnostic string surfaced when
// claude exits with no stdout. Returns an empty string only if every
// signal is also empty (impossible in practice — the exit code is
// always knowable) so callers can rely on the result being usable.
func formatEmptyOutputReason(exitCode, unparseableCount int, firstUnparseable, stderrTail string) string {
	parts := []string{fmt.Sprintf("exit=%d", exitCode)}
	if unparseableCount > 0 {
		parts = append(parts, fmt.Sprintf("unparsed_stdout_lines=%d (first: %q)", unparseableCount, firstUnparseable))
	}
	if stderrTail != "" {
		parts = append(parts, "stderr: "+truncate(stderrTail, 4096))
	}
	return strings.Join(parts, " | ")
}

// truncate returns s clipped to at most n runes, with a trailing
// ellipsis when truncation actually happened. Used to keep diagnostic
// strings bounded so a runaway stderr dump can't bloat task comments.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
