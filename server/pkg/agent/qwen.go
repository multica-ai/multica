package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// qwenBackend drives Qwen Code's native non-interactive JSONL protocol:
// qwen -p <prompt> --output-format stream-json. The event schema is based on
// Qwen Code 0.20.0 captures in testdata/qwen-code-0.20.0-stream-json.jsonl.
type qwenBackend struct {
	cfg Config
}

// qwenBlockedArgs are owned by Multica. Qwen accepts the task prompt and stream
// protocol as flags, so custom args must not replace either. Model/session are
// also selected by Multica, and safe mode disables the QWEN.md context file.
var qwenBlockedArgs = map[string]blockedArgMode{
	"-p":                   blockedWithValue,
	"--prompt":             blockedWithValue,
	"-i":                   blockedWithValue,
	"--prompt-interactive": blockedWithValue,
	"-o":                   blockedWithValue,
	"--output-format":      blockedWithValue,
	"-m":                   blockedWithValue,
	"--model":              blockedWithValue,
	"-r":                   blockedWithValue,
	"--resume":             blockedWithValue,
	"-c":                   blockedStandalone,
	"--continue":           blockedStandalone,
	"--chat-recording":     blockedWithValue,
	"--mcp-config":         blockedWithValue,
	"--safe-mode":          blockedStandalone,
}

func buildQwenArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"-p", prompt, "--output-format", "stream-json"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, qwenBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, qwenBlockedArgs, logger)...)
	return args
}

func (b *qwenBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "qwen"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("qwen executable not found at %q: %w", execPath, err)
	}
	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildQwenArgs(prompt, opts, b.cfg.Logger)

	// Qwen Code 0.20.0 accepts a JSON string or a file path through
	// --mcp-config. Materialise a managed config into a 0600 temp file so it
	// does not appear in argv or logs, then remove it when the process exits.
	var mcpConfigPath string
	var mcpFileCleanup func()
	if hasManagedMcpConfig(opts.McpConfig) {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("write qwen mcp_config: %w", err)
		}
		mcpConfigPath = path
		mcpFileCleanup = func() { cleanupMcpConfigTemp(mcpConfigPath) }
		args = append(args, "--mcp-config", mcpConfigPath)
	}
	// Clean up if a later setup step returns before the result goroutine owns it.
	defer func() {
		if mcpFileCleanup != nil {
			mcpFileCleanup()
		}
	}()
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	// args contain the task prompt; never expose it in daemon logs.
	b.cfg.Logger.Info("agent command", "exec", execPath, "provider", "qwen")
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("qwen stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[qwen:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qwen: %w", err)
	}
	// cmd.Start succeeded; result goroutine now owns cleanup.
	mcpFileCleanup = nil
	b.cfg.Logger.Info("qwen started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer cleanupMcpConfigTemp(mcpConfigPath)
		}

		started := time.Now()
		state := qwenStreamState{model: opts.Model, usage: make(map[string]TokenUsage)}
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
			var event qwenStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				state.invalidEventCount++
				continue
			}
			state.eventCount++
			state.lastEventType = event.Type
			handleQwenEvent(event, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		duration := time.Since(started)

		status, output, errMsg := finalizeStreamResult("qwen", timeout, runCtx.Err(), nil, exitErr, state.sessionID, streamTerminalState{
			lastAssistantText: state.lastAssistantText,
			finalResultText:   state.finalResultText,
			sawResult:         state.sawResult,
			resultIsError:     state.resultIsError,
			scanErr:           scanErr,
		}, "")
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "qwen", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider: "qwen", cliVersion: b.cfg.CLIVersion, model: state.model,
			exitCode: streamProcessExitCode(exitErr), eventCount: state.eventCount,
			invalidEventCount: state.invalidEventCount, assistantEventCount: state.assistantEventCount,
			toolUseCount: state.toolUseCount, sawResult: state.sawResult, resultIsError: state.resultIsError,
			resultBytes: len(state.finalResultText), lastAssistantBytes: len(state.lastAssistantText),
			scannerError: scanErr != nil, lastEventType: state.lastEventType,
		})
		b.cfg.Logger.Info("qwen finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status: status, Output: output, Error: errMsg, DurationMs: duration.Milliseconds(),
			SessionID: resolveSessionID(opts.ResumeSessionID, state.sessionID, status == "failed"), Usage: state.usage,
		}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

type qwenStreamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Usage     *qwenUsage      `json:"usage,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

type qwenMessage struct {
	Model   string             `json:"model,omitempty"`
	Content []qwenContentBlock `json:"content"`
	Usage   *qwenUsage         `json:"usage,omitempty"`
}

type qwenUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
}

type qwenContentBlock struct {
	Type      string          `json:"type"`
	Thinking  string          `json:"thinking,omitempty"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type qwenStreamState struct {
	sessionID, model, lastAssistantText, finalResultText, lastEventType string
	sawResult, resultIsError                                            bool
	usage                                                               map[string]TokenUsage
	eventCount, invalidEventCount, assistantEventCount, toolUseCount    int
}

func handleQwenEvent(event qwenStreamEvent, ch chan<- Message, state *qwenStreamState) {
	if event.SessionID != "" {
		state.sessionID = event.SessionID
	}
	if event.Model != "" {
		state.model = event.Model
	}
	switch event.Type {
	case "system":
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: state.sessionID})
	case "assistant":
		state.assistantEventCount++
		text, tools, model := handleQwenAssistant(event.Message, ch, state.usage)
		if model != "" {
			state.model = model
		}
		state.toolUseCount += tools
		if tools == 0 && text != "" {
			state.lastAssistantText = text
		} else if tools > 0 {
			state.lastAssistantText = ""
		}
	case "user":
		handleQwenUser(event.Message, ch)
	case "result":
		state.sawResult = true
		state.resultIsError = event.IsError || event.Subtype == "error" || event.Subtype == "failed"
		if state.resultIsError {
			// Qwen 0.20.0 result errors omit result; their actionable detail
			// is in error.message.
			state.finalResultText = qwenErrorText(event)
		} else {
			state.finalResultText = event.Result
		}
		if usage := qwenResultUsage(event.Usage, state.model); len(usage) > 0 {
			state.usage = usage
		}
	case "error":
		// Be fail-closed if a later Qwen release emits a terminal error event.
		state.sawResult = true
		state.resultIsError = true
		state.finalResultText = qwenErrorText(event)
	}
}

func handleQwenAssistant(raw json.RawMessage, ch chan<- Message, usage map[string]TokenUsage) (string, int, string) {
	var message qwenMessage
	if json.Unmarshal(raw, &message) != nil {
		return "", 0, ""
	}
	if message.Usage != nil && message.Model != "" {
		usage[message.Model] = qwenTokenUsage(message.Usage)
	}
	var text strings.Builder
	tools := 0
	for _, block := range message.Content {
		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Thinking})
			}
		case "text":
			if block.Text != "" {
				text.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "tool_use":
			tools++
			var input map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: input})
		}
	}
	return text.String(), tools, message.Model
}

func handleQwenUser(raw json.RawMessage, ch chan<- Message) {
	var message qwenMessage
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	for _, block := range message.Content {
		if block.Type == "tool_result" {
			trySend(ch, Message{Type: MessageToolResult, CallID: block.ToolUseID, Output: qwenToolResultOutput(block.Content)})
		}
	}
}

func qwenTokenUsage(usage *qwenUsage) TokenUsage {
	return TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CacheReadInputTokens}
}

func qwenResultUsage(usage *qwenUsage, model string) map[string]TokenUsage {
	if usage == nil || model == "" || (usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadInputTokens == 0) {
		return nil
	}
	return map[string]TokenUsage{model: qwenTokenUsage(usage)}
}

func qwenToolResultOutput(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func qwenErrorText(event qwenStreamEvent) string {
	if event.Result != "" {
		return event.Result
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(event.Error, &body) == nil && body.Message != "" {
		return body.Message
	}
	if len(event.Error) > 0 {
		return string(event.Error)
	}
	return "qwen returned an error event without details"
}
