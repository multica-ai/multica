package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// codebuddyBackend implements Backend by spawning the CodeBuddy CLI
// (a Claude Code fork) with --output-format stream-json.
// It mirrors claude.go's execution model: concurrent stdin/stdout to
// avoid pipe deadlocks, open stdin for control_request auto-approval,
// and runContext for zero-timeout = no-deadline semantics.
type codebuddyBackend struct {
	cfg Config
}

// codebuddyBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// the daemon↔codebuddy communication protocol.
var codebuddyBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedStandalone, // non-interactive mode
	"--output-format":   blockedWithValue,  // stream-json protocol
	"--input-format":    blockedWithValue,  // stream-json protocol
	"--permission-mode": blockedWithValue,  // bypassPermissions for autonomous operation
	"--mcp-config":      blockedWithValue,  // set by daemon from agent.mcp_config
	// `--effort` is owned by the per-agent thinking_level picker so a
	// user-supplied custom_arg cannot silently outvote it.
	"--effort": blockedWithValue,
}

func buildCodebuddyArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
		// CodeBuddy's interactive tools have no UI to render in under the
		// daemon's headless stream-json transport. AskUserQuestion and
		// ExitPlanMode are both exempted from CodeBuddy's permission-mode
		// finalization, so --permission-mode bypassPermissions does NOT
		// auto-approve them — they always reach the permission bridge and
		// stall the turn waiting for a confirmation nobody can give
		// (GitHub #6012). EnterPlanMode is denied alongside them: leaving it
		// enabled would let the model enter a plan mode it then has no tool
		// to leave. Plan-shaped work still happens — the plan is written as
		// ordinary assistant output instead of behind an approval gate.
		//
		// Pass one value per tool: --disallowedTools is variadic and
		// CodeBuddy compares each entry against the tool name exactly
		// (PermissionUtils.matchPermissionRules), so a comma-joined string
		// would match nothing despite what the CLI's own help text claims.
		"--disallowedTools", "AskUserQuestion", "EnterPlanMode", "ExitPlanMode",
	}
	// NOTE: --strict-mcp-config is deliberately never passed. It means "only
	// use servers from --mcp-config", which drops CodeBuddy's user, project
	// AND local scopes. Measured against CodeBuddy 2.x with one real MCP
	// server registered per scope, using process spawns as the oracle:
	//
	//	--mcp-config only ....... managed + user + local  <- what we want
	//	--mcp-config + strict ... managed only
	//	strict only ............. nothing at all
	//
	// The union is also what mergeRuntimeAndAgentMcpConfig promises: adding
	// one managed server must not disable the user's own. CodeBuddy applies
	// it natively, and a managed entry already wins a same-name collision
	// (verified), so the daemon does not pre-merge — see the codebuddy note
	// in loadRuntimeMcpServerConfigs.
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
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
	args = append(args, filterCustomArgs(opts.ExtraArgs, codebuddyBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, codebuddyBlockedArgs, logger)...)
	return args
}

func (b *codebuddyBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codebuddy"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codebuddy executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildCodebuddyArgs(opts, b.cfg.Logger)

	// A managed agent config is written to a temp file and added with
	// --mcp-config, which ADDS to whatever CodeBuddy loads from its own user,
	// project and local scopes — buildCodebuddyArgs never passes
	// --strict-mcp-config, so those stay on. A managed entry wins a same-name
	// collision natively.
	//
	// Three-state, as everywhere else: a JSON `null` means "inherit the runtime
	// configuration" and skips the flag entirely, while an explicitly empty
	// object is a managed set that happens to contain no servers.
	var mcpConfigPath string
	var mcpFileCleanup func()
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

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
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
		return nil, fmt.Errorf("codebuddy stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codebuddy stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }

	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[codebuddy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start codebuddy: %w", err)
	}

	b.cfg.Logger.Info("codebuddy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	// cmd.Start() succeeded — transfer temp file ownership to the goroutine.
	mcpFileCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Write the prompt in a dedicated goroutine to prevent deadlock.
	// CodeBuddy (like Claude Code) emits a startup banner to stdout before
	// reading stdin; a synchronous write would block once the pipe buffer
	// fills. Keep stdin open after writing so control_request events can
	// be answered mid-run.
	writeDone := make(chan error, 1)
	go func() {
		err := writeCodebuddyInput(stdin, prompt)
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
		usage := make(map[string]TokenUsage)
		eventCount := 0
		invalidEventCount := 0
		assistantEventCount := 0
		toolUseCount := 0
		unreadableAssistantCount := 0

		// Close stdout when the context is cancelled so scanner.Scan() unblocks.
		go func() {
			<-runCtx.Done()
			closeStdin()
			_ = stdout.Close()
		}()

		scanner := newAgentStreamScanner(stdout)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var msg codebuddySDKMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				invalidEventCount++
				continue
			}
			eventCount++

			switch msg.Type {
			case "assistant":
				assistantEventCount++
				turn := b.handleAssistant(msg, msgCh, usage)
				toolUseCount += turn.toolUses
				if !turn.understood {
					unreadableAssistantCount++
				}
				lastAssistantText = turn.resolveFallback(lastAssistantText)
			case "user":
				b.handleUser(msg, msgCh)
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
				if resultUsage := codebuddyResultUsage(msg, opts.Model); len(resultUsage) > 0 {
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
			// Stop a malformed/oversized writer from blocking cmd.Wait after the
			// scanner has stopped consuming stdout.
			_ = stdout.Close()
		}

		closeStdin()

		// Wait for process exit.
		exitErr := cmd.Wait()
		duration := time.Since(startTime)
		// writeDone is buffered (cap 1) and the writer always sends — by the
		// time cmd has exited, the prompt write has either succeeded, hit a
		// broken pipe, or been unblocked by the kill that ended cmd.
		writeErr := <-writeDone

		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"codebuddy",
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
			"",
		)

		if finalError != "" {
			finalError = withAgentStderr(finalError, "codebuddy", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:                   "codebuddy",
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
			unreadableAssistantCount:   unreadableAssistantCount,
			anthropicBaseURLConfigured: strings.TrimSpace(b.cfg.Env["ANTHROPIC_BASE_URL"]) != "",
		})

		b.cfg.Logger.Info("codebuddy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resumeRejected := resumeWasRejected(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError)
		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError)
		if resumeRejected {
			b.cfg.Logger.Info("codebuddy resume was rejected; dropping session id and signalling fresh-session retry",
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

func (b *codebuddyBackend) handleAssistant(msg codebuddySDKMessage, ch chan<- Message, usage map[string]TokenUsage) assistantTurn {
	var content codebuddyMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		// Unreadable body: understood stays false so the caller drops any
		// fallback rather than let an older turn stand in for this one.
		return assistantTurn{}
	}
	turn := assistantTurn{understood: true}
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
		default:
			// A block type we do not render may be carrying the model's answer
			// in a shape we cannot read, so we must not claim this turn was
			// silent. Recognising a new no-text block is a deliberate one-line
			// addition here, not an accident of falling through.
			turn.understood = false
		}
	}
	turn.text = assistantText.String()
	turn.toolUses = toolUseCount
	return turn
}

func (b *codebuddyBackend) handleUser(msg codebuddySDKMessage, ch chan<- Message) {
	var content codebuddyMessageContent
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

func (b *codebuddyBackend) handleControlRequest(msg codebuddySDKMessage, stdin interface{ Write([]byte) (int, error) }) {
	// Auto-approve all tool uses in autonomous/daemon mode.
	var req codebuddyControlRequestPayload
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
				// CodeBuddy's SdkPermissionClient reads `allowed` and treats a
				// missing key as a denial; `behavior` is Claude Code's spelling,
				// which the fork still honours on its other permission paths.
				// Send both so an approval is never read as a silent reject.
				"allowed":      true,
				"behavior":     "allow",
				"updatedInput": inputMap,
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		b.cfg.Logger.Warn("codebuddy: failed to marshal control response", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := stdin.Write(data); err != nil {
		b.cfg.Logger.Warn("codebuddy: failed to write control response", "error", err)
	}
}

func writeCodebuddyInput(w io.Writer, prompt string) error {
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
		return fmt.Errorf("marshal codebuddy input: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

// ── Codebuddy SDK JSON types ──

type codebuddySDKMessage struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`

	// result fields
	ResultText string                               `json:"result,omitempty"`
	IsError    bool                                 `json:"is_error,omitempty"`
	DurationMs float64                              `json:"duration_ms,omitempty"`
	NumTurns   int                                  `json:"num_turns,omitempty"`
	Usage      *codebuddyUsage                      `json:"usage,omitempty"`
	ModelUsage map[string]codebuddyResultModelUsage `json:"modelUsage,omitempty"`

	// log fields
	Log *codebuddyLogEntry `json:"log,omitempty"`

	// control request fields
	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}

type codebuddyLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type codebuddyMessageContent struct {
	Role    string                  `json:"role"`
	Model   string                  `json:"model"`
	Content []codebuddyContentBlock `json:"content"`
	Usage   *codebuddyUsage         `json:"usage,omitempty"`
}

type codebuddyUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type codebuddyResultModelUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

type codebuddyContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type codebuddyControlRequestPayload struct {
	Subtype  string          `json:"subtype"`
	ToolName string          `json:"tool_name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

func codebuddyResultUsage(msg codebuddySDKMessage, fallbackModel string) map[string]TokenUsage {
	if len(msg.ModelUsage) > 0 {
		usage := make(map[string]TokenUsage, len(msg.ModelUsage))
		for model, u := range msg.ModelUsage {
			if model == "" || !codebuddyUsageHasTokens(u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens) {
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
	if msg.Usage == nil || model == "" || !codebuddyUsageHasTokens(
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

func codebuddyUsageHasTokens(input, output, cacheRead, cacheWrite int64) bool {
	return input > 0 || output > 0 || cacheRead > 0 || cacheWrite > 0
}
