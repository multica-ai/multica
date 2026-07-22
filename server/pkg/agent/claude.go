package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
	if hasManagedMcpConfig(opts.McpConfig) {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			cancel()
			return nil, err
		}
		mcpConfigPath = path
		mcpFileCleanup = func() { cleanupMcpConfigTemp(mcpConfigPath) }
		args = append(args, "--mcp-config", mcpConfigPath)
	}
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
	if err := claudeRootSudoPreflight(args, cmd.Env); err != nil {
		cancel()
		return nil, err
	}

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

	b.cfg.Logger.Info("claude started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	// cmd.Start() succeeded — transfer temp file ownership to the goroutine.
	mcpFileCleanup = nil

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
		if mcpConfigPath != "" {
			defer cleanupMcpConfigTemp(mcpConfigPath)
		}

		startTime := time.Now()
		var lastAssistantText string
		var finalResultText string
		sawResult := false
		resultIsError := false
		var sessionID string
		sawAsyncLaunch := false
		usage := make(map[string]TokenUsage)
		eventCount := 0
		invalidEventCount := 0
		assistantEventCount := 0
		toolUseCount := 0

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
				invalidEventCount++
				continue
			}
			eventCount++

			switch msg.Type {
			case "assistant":
				assistantEventCount++
				assistantText, tools := b.handleAssistant(msg, msgCh, usage)
				toolUseCount += tools
				if tools == 0 {
					lastAssistantText = assistantText
				} else {
					// A turn that invokes a tool is intermediate even when it also
					// contains narration. Do not use it as an empty-result fallback.
					lastAssistantText = ""
				}
			case "user":
				if b.handleUser(msg, msgCh) {
					sawAsyncLaunch = true
				}
			case "system":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				sawResult = true
				finalResultText = msg.ResultText
				resultIsError = msg.IsError
				sessionID = msg.SessionID
				if resultUsage := claudeResultUsage(msg, opts.Model); len(resultUsage) > 0 {
					usage = resultUsage
				}
				closeStdin()
			case "log":
				if msg.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   msg.Log.Level,
						Content: msg.Log.Message,
					})
				}
			case "control_request":
				b.handleControlRequest(msg, stdin)
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			// Scanner stopped consuming stdout. Close the pipe before Wait so a
			// child still writing a malformed/oversized event cannot deadlock on
			// the full OS pipe; the scanner error remains the primary failure.
			_ = stdout.Close()
		}

		closeStdin()

		// Wait for process exit
		exitErr := cmd.Wait()
		duration := time.Since(startTime)
		// writeDone is buffered (cap 1) and the writer always sends — by the
		// time cmd has exited, the prompt write has either succeeded, hit a
		// broken pipe, or been unblocked by the kill that ended cmd.
		writeErr := <-writeDone

		completionGuardError := ""
		if sawAsyncLaunch {
			completionGuardError = "claude launched an async background task; Multica-managed runs require foreground execution"
		}
		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"claude",
			timeout,
			runCtx.Err(),
			writeErr,
			exitErr,
			sessionID,
			streamTerminalState{
				lastAssistantText: lastAssistantText,
				finalResultText:   finalResultText,
				sawResult:         sawResult,
				resultIsError:     resultIsError,
				scanErr:           scanErr,
			},
			completionGuardError,
		)

		// cmd.Wait() has returned — os/exec's stderr copy goroutine has
		// observed every byte claude wrote to stderr before exiting, so
		// stderrBuf.Tail() is safe to sample now. Attach the tail to any
		// non-empty failure message; callers upstream surface this as the
		// task's error field, which is the only place users see it.
		stderrTail := stderrBuf.Tail()
		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrTail)
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:                   "claude",
			cliVersion:                 b.cfg.CLIVersion,
			model:                      opts.Model,
			exitCode:                   streamProcessExitCode(exitErr),
			eventCount:                 eventCount,
			invalidEventCount:          invalidEventCount,
			assistantEventCount:        assistantEventCount,
			toolUseCount:               toolUseCount,
			sawResult:                  sawResult,
			resultIsError:              resultIsError,
			resultBytes:                len(finalResultText),
			lastAssistantBytes:         len(lastAssistantText),
			scannerError:               scanErr != nil,
			anthropicBaseURLConfigured: strings.TrimSpace(b.cfg.Env["ANTHROPIC_BASE_URL"]) != "",
		})

		b.cfg.Logger.Info("claude finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// The account-binding 400 arrives in the result event (finalError);
		// "no conversation found" is printed to stderr. Check both.
		resumeRejected := resumeWasRejected(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError, stderrTail)
		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError, stderrTail)
		if resumeRejected {
			b.cfg.Logger.Info("claude resume was rejected; dropping session id and signalling fresh-session retry",
				"requested_resume", opts.ResumeSessionID,
				"emitted_session", sessionID,
			)
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      reportedSessionID,
			Usage:          usage,
			ResumeRejected: resumeRejected,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *claudeBackend) handleAssistant(msg claudeSDKMessage, ch chan<- Message, usage map[string]TokenUsage) (string, int) {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return "", 0
	}
	var assistantText strings.Builder
	toolUseCount := 0

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
				assistantText.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
			toolUseCount++
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
	return assistantText.String(), toolUseCount
}

func (b *claudeBackend) handleUser(msg claudeSDKMessage, ch chan<- Message) bool {
	var content claudeMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return false
	}

	sawAsyncLaunch := false
	for _, block := range content.Content {
		if block.Type == "tool_result" {
			resultStr := ""
			if block.Content != nil {
				resultStr = string(block.Content)
				if claudeToolResultHasAsyncLaunch(block.Content) {
					sawAsyncLaunch = true
				}
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: block.ToolUseID,
				Output: resultStr,
			})
		}
	}
	return sawAsyncLaunch
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
	if forceClaudeToolInputForeground(inputMap) {
		b.cfg.Logger.Info("claude: forced foreground tool execution",
			"request_id", msg.RequestID,
			"tool", req.ToolName,
		)
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

func forceClaudeToolInputForeground(input map[string]any) bool {
	if runInBackground, ok := input["run_in_background"].(bool); ok && runInBackground {
		input["run_in_background"] = false
		return true
	}
	return false
}

func claudeToolResultHasAsyncLaunch(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		if claudeMapHasAsyncLaunchStatus(v) {
			return true
		}
		if content, ok := v["content"].([]any); ok {
			return claudeArrayHasAsyncLaunchStatus(content)
		}
	case []any:
		return claudeArrayHasAsyncLaunchStatus(v)
	}
	return false
}

func claudeArrayHasAsyncLaunchStatus(values []any) bool {
	for _, value := range values {
		if item, ok := value.(map[string]any); ok && claudeMapHasAsyncLaunchStatus(item) {
			return true
		}
	}
	return false
}

func claudeMapHasAsyncLaunchStatus(value map[string]any) bool {
	status, ok := value["status"].(string)
	return ok && status == "async_launched"
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
		// Channel full — drop message. Result.Output is finalized independently,
		// so only live transcript consumers are affected.
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
		"--permission-mode", "bypassPermissions",
		// AskUserQuestion is Claude Code's built-in interactive question tool.
		// The daemon runs Claude in non-interactive stream-json mode and has
		// no UI for the prompt to render in, so a call returns an empty
		// answer and the agent ends up "inferring" silently — the user
		// never sees the question (see GitHub #2588). User-facing
		// clarification belongs in an issue comment instead.
		"--disallowedTools", "AskUserQuestion",
	}
	if hasManagedMcpConfig(opts.McpConfig) {
		// A saved agent-level config is authoritative, including an explicitly
		// empty object. With no managed config, omit strict mode so Claude can
		// inherit the user's local runtime MCP servers.
		args = append(args, "--strict-mcp-config")
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
	blockedArgs := claudeBlockedArgs
	if opts.ClaudeSettingsPath != "" {
		// The daemon-owned --settings file is the enforcement layer for disabled
		// inherited skills. Drop competing per-agent/default flags only while that
		// policy is active, then append the managed file last.
		blockedArgs = make(map[string]blockedArgMode, len(claudeBlockedArgs)+1)
		for key, mode := range claudeBlockedArgs {
			blockedArgs[key] = mode
		}
		blockedArgs["--settings"] = blockedWithValue
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, blockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, blockedArgs, logger)...)
	if opts.ClaudeSettingsPath != "" {
		args = append(args, "--settings", opts.ClaudeSettingsPath)
	}
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

// resumeRejectedPhrases are the provider messages that positively identify a
// refused resume, as opposed to a failure that merely happened to occur during
// a resumed run. Shared by the stream-json backends (claude, codebuddy, qwen);
// the ACP backends match structured error codes via isACPSessionNotFound
// instead.
//
// Matching must stay tight: a false positive makes the daemon throw away a
// recoverable session pointer and re-run the task, so anything ambiguous
// belongs out of this list.
var resumeRejectedPhrases = []string{
	// Verified: claude prints this when the transcript named by --resume is
	// absent. Covered by TestResolveSessionID's existing stderr fixture.
	"no conversation found",
	// Verified: qwen-code 0.20.0, captured in
	// testdata/qwen-code-0.20.0-resume-not-found.stderr.txt — "No saved
	// session found with ID <id>. Run `qwen --resume` without an ID to
	// choose from existing sessions."
	"no saved session found",
	// Reported verbatim in multica-ai/multica#5704 against Claude Code
	// 2.1.207 (zh-CN): "400 此 session 已绑定另外的ai账号，请执行 /new 开启新
	// session". This is the account-switch guardrail this signal exists for.
	"已绑定另外",
	// INFERRED, not yet observed: the en-US wording of the same guardrail has
	// not been captured. These are guesses derived from the zh-CN text and
	// should be corrected once a real English sample is available. A miss
	// here degrades to a terminal failure carrying the provider's raw error,
	// which is diagnosable — it does not silently mis-route the run.
	"bound to another account",
	"bound to a different account",
}

// resumeWasRejected reports whether a failed run's --resume was itself
// refused. It is the positive-evidence predicate behind Result.ResumeRejected:
// only a rejected resume can be cured by starting a fresh session.
//
// texts should include every place the provider may have written the reason —
// the final error string and the stderr tail — because the account-binding 400
// arrives in the stream-json result event while "no conversation found" is
// printed to stderr.
func resumeWasRejected(requestedResume, emitted string, failed bool, texts ...string) bool {
	if !failed || requestedResume == "" {
		return false
	}
	for _, text := range texts {
		lower := strings.ToLower(text)
		for _, phrase := range resumeRejectedPhrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	// Claude answered with a different session than the one we asked to
	// continue: the requested transcript did not load.
	return emitted != "" && emitted != requestedResume
}

// resolveSessionID decides which session id to report on the Result. A session
// known to be dead must not be persisted as the resume pointer, so a rejected
// resume reports "" regardless of what claude echoed back.
//
// This is deliberately no longer the signal the daemon reads to decide *why*
// a run failed — Result.ResumeRejected carries that. An empty SessionID is
// also produced by any failure before the first stream message (a 401, a
// missing binary), which is why inferring "the resume was rejected" from it
// was wrong in both directions.
func resolveSessionID(requestedResume, emitted string, failed bool, texts ...string) string {
	if resumeWasRejected(requestedResume, emitted, failed, texts...) {
		return ""
	}
	return emitted
}

func buildEnv(extra map[string]string) []string {
	return mergeEnv(os.Environ(), extra)
}

func claudeRootSudoPreflight(args, env []string) error {
	if !argsRequestBypassPermissions(args) || os.Geteuid() != 0 || envHasSandbox(env) {
		return nil
	}
	return fmt.Errorf("Claude Code refuses bypassPermissions under root/sudo privileges. Run the Multica daemon as a non-root user, or set IS_SANDBOX=1 if running in a genuine container/sandbox")
}

func argsRequestBypassPermissions(args []string) bool {
	for i, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			return true
		}
		if arg == "--permission-mode" && i+1 < len(args) && args[i+1] == "bypassPermissions" {
			return true
		}
	}
	return false
}

func envHasSandbox(env []string) bool {
	for i := len(env) - 1; i >= 0; i-- {
		key, value, ok := strings.Cut(env[i], "=")
		if key != "IS_SANDBOX" {
			continue
		}
		if !ok {
			return false
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	}
	return false
}

func mergeEnv(base []string, extra map[string]string) []string {
	env := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		// MULTICA_* in the daemon's own environment is not task context. Drop
		// the inherited namespace for every backend and append only the values
		// daemon.go explicitly assembled for this task below.
		if isFilteredChildEnvKey(key) || strings.HasPrefix(strings.ToUpper(key), "MULTICA_") {
			continue
		}
		env = append(env, entry)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
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
	blockedWithValue     blockedArgMode = iota // flag takes a value (next arg or =value)
	blockedStandalone                          // flag is boolean, no value
	blockedOptionalValue                       // flag may take the next non-flag arg or =value
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
	for i := 0; i < len(args); i++ {
		raw := args[i]
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
				i++
			} else if mode == blockedOptionalValue && !hasInlineValue && i+1 < len(args) &&
				!strings.HasPrefix(unshellQuoteArg(args[i+1]), "-") {
				// Optional values are consumed only when the next token is not
				// another flag, so a boolean form cannot swallow an unrelated
				// option.
				i++
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

// writeMcpConfigToTemp writes MCP config JSON to a temporary file and returns
// its path. The caller is responsible for removing it via cleanupMcpConfigTemp.
func writeMcpConfigToTemp(raw json.RawMessage) (string, error) {
	dir, err := os.MkdirTemp("", "multica-mcp-*")
	if err != nil {
		return "", fmt.Errorf("create mcp config temp dir: %w", err)
	}
	data, err := hardenBrowserMcpConfig(raw, dir)
	if err != nil {
		cleanupMcpConfigTemp(filepath.Join(dir, "mcp-config.json"))
		return "", err
	}
	path := filepath.Join(dir, "mcp-config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanupMcpConfigTemp(path)
		return "", fmt.Errorf("write mcp config temp file: %w", err)
	}
	return path, nil
}

func cleanupMcpConfigTemp(path string) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if strings.HasPrefix(filepath.Base(dir), "multica-mcp-") {
		_ = os.RemoveAll(dir)
		return
	}
	_ = os.Remove(path)
}

// detectVersionTimeout bounds a single `<cli> --version` probe. Version
// detection runs inside the daemon's blocking preflight (registerRuntimesForWorkspace),
// so a CLI that never returns from `--version` — e.g. a brew-installed claude
// wedged by a bun regression (MUL-3812) — would otherwise stall the whole
// registration loop, the daemon would never flip /health from "starting" to
// "running", and *every* runtime on the host would appear disconnected. A real
// `--version` returns well under this bound even on a cold cache or with
// Windows AV scanning; the timeout exists only to fail a wedged probe fast and
// in isolation so the remaining runtimes still register. A var (not const) so
// tests can shrink it without waiting out the real bound.
var detectVersionTimeout = 10 * time.Second

func detectCLIVersion(ctx context.Context, execPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, detectVersionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath, "--version")
	hideAgentWindow(cmd)
	// exec.CommandContext only kills the direct child on timeout. A broken CLI
	// (node/bun shim) can leave grandchildren that inherited and still hold our
	// stdout pipe open, and cmd.Output() blocks in Wait() until that pipe
	// closes — defeating the timeout above. WaitDelay forces the pipes shut and
	// reaps shortly after the context fires so this call always returns.
	cmd.WaitDelay = 2 * time.Second
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
