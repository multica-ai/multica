package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// qwenBackend implements Backend by spawning Qwen Code with its native
// non-interactive JSON-lines stream protocol.
type qwenBackend struct {
	cfg Config
}

// qwenBlockedArgs are flags owned by the daemon. Qwen Code's prompt is an
// argument to -p (unlike Claude's stdin frame), so it must also be protected
// from custom_args overrides.
var qwenBlockedArgs = map[string]blockedArgMode{
	"-p":                         blockedWithValue,
	"--prompt":                   blockedWithValue,
	"-o":                         blockedWithValue,
	"--model":                    blockedWithValue,
	"--output-format":            blockedWithValue,
	"--input-format":             blockedWithValue,
	"--include-partial-messages": blockedStandalone,
	"--yolo":                     blockedStandalone,
	"-y":                         blockedStandalone,
	"--approval-mode":            blockedWithValue,
	"--prompt-interactive":       blockedWithValue,
	"-i":                         blockedStandalone,
	"--safe-mode":                blockedStandalone,
}

func buildQwenArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--yolo"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-session-turns", strconv.Itoa(opts.MaxTurns))
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
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
	if hasManagedMcpConfig(opts.McpConfig) {
		return nil, fmt.Errorf("qwen does not support Multica-managed MCP configuration; remove mcp_config to inherit Qwen Code's native settings")
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildQwenArgs(prompt, opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	// Do not log args: they include the complete user prompt after -p.
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
	b.cfg.Logger.Info("qwen started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var lastAssistantText string
		var finalResultText string
		sawResult := false
		resultIsError := false
		var sessionID string
		streamModel := opts.Model
		usage := make(map[string]TokenUsage)
		eventCount := 0
		invalidEventCount := 0
		assistantEventCount := 0
		toolUseCount := 0

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
			var msg qwenSDKMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				invalidEventCount++
				continue
			}
			eventCount++

			switch msg.Type {
			case "assistant":
				assistantEventCount++
				assistantText, tools, model := handleQwenAssistant(msg, msgCh, usage)
				if model != "" {
					streamModel = model
				}
				toolUseCount += tools
				if tools == 0 {
					lastAssistantText = assistantText
				} else {
					lastAssistantText = ""
				}
			case "user":
				handleQwenUser(msg, msgCh)
			case "system":
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				if msg.Model != "" {
					streamModel = msg.Model
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				sawResult = true
				finalResultText = msg.ResultText
				resultIsError = msg.IsError
				if msg.SessionID != "" {
					sessionID = msg.SessionID
				}
				if resultUsage := qwenResultUsage(msg, streamModel); len(resultUsage) > 0 {
					usage = resultUsage
				}
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"qwen", timeout, runCtx.Err(), nil, exitErr, sessionID,
			streamTerminalState{
				lastAssistantText: lastAssistantText,
				finalResultText:   finalResultText,
				sawResult:         sawResult,
				resultIsError:     resultIsError,
				scanErr:           scanErr,
			}, "",
		)
		if finalError != "" {
			finalError = withAgentStderr(finalError, "qwen", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:            "qwen",
			cliVersion:          b.cfg.CLIVersion,
			model:               streamModel,
			exitCode:            streamProcessExitCode(exitErr),
			eventCount:          eventCount,
			invalidEventCount:   invalidEventCount,
			assistantEventCount: assistantEventCount,
			toolUseCount:        toolUseCount,
			sawResult:           sawResult,
			resultIsError:       resultIsError,
			resultBytes:         len(finalResultText),
			lastAssistantBytes:  len(lastAssistantText),
			scannerError:        scanErr != nil,
		})
		b.cfg.Logger.Info("qwen finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed")
		resCh <- Result{Status: finalStatus, Output: finalOutput, Error: finalError, DurationMs: duration.Milliseconds(), SessionID: reportedSessionID, Usage: usage}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type qwenSDKMessage struct {
	Type       string          `json:"type"`
	Message    json.RawMessage `json:"message,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Model      string          `json:"model,omitempty"`
	ResultText string          `json:"result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Usage      *qwenUsage      `json:"usage,omitempty"`
}

type qwenMessageContent struct {
	Model   string             `json:"model"`
	Content []qwenContentBlock `json:"content"`
	Usage   *qwenUsage         `json:"usage,omitempty"`
}

type qwenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
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

func handleQwenAssistant(msg qwenSDKMessage, ch chan<- Message, usage map[string]TokenUsage) (string, int, string) {
	var content qwenMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return "", 0, ""
	}
	if content.Usage != nil && content.Model != "" {
		u := usage[content.Model]
		u.InputTokens += content.Usage.InputTokens
		u.OutputTokens += content.Usage.OutputTokens
		u.CacheReadTokens += content.Usage.CacheReadInputTokens
		u.CacheWriteTokens += content.Usage.CacheCreationInputTokens
		usage[content.Model] = u
	}

	var text strings.Builder
	toolCount := 0
	for _, block := range content.Content {
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
			toolCount++
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: input})
		}
	}
	return text.String(), toolCount, content.Model
}

func handleQwenUser(msg qwenSDKMessage, ch chan<- Message) {
	var content qwenMessageContent
	if err := json.Unmarshal(msg.Message, &content); err != nil {
		return
	}
	for _, block := range content.Content {
		if block.Type != "tool_result" {
			continue
		}
		trySend(ch, Message{Type: MessageToolResult, CallID: block.ToolUseID, Output: qwenToolResultOutput(block.Content)})
	}
}

func qwenToolResultOutput(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func qwenResultUsage(msg qwenSDKMessage, fallbackModel string) map[string]TokenUsage {
	model := msg.Model
	if model == "" {
		model = fallbackModel
	}
	if msg.Usage == nil || model == "" || !qwenUsageHasTokens(msg.Usage) {
		return nil
	}
	return map[string]TokenUsage{model: {
		InputTokens:      msg.Usage.InputTokens,
		OutputTokens:     msg.Usage.OutputTokens,
		CacheReadTokens:  msg.Usage.CacheReadInputTokens,
		CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
	}}
}

func qwenUsageHasTokens(usage *qwenUsage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheReadInputTokens > 0 || usage.CacheCreationInputTokens > 0
}
