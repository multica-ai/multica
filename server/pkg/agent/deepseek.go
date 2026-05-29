package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// deepseekBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. DeepSeek-TUI 0.8.x removed the
// old `deepseek app-server --stdio` entrypoint, so the daemon now uses the
// current non-interactive JSON command: `deepseek-tui exec --json --auto`.
var deepseekBlockedArgs = map[string]blockedArgMode{
	"app-server":  blockedStandalone,
	"--stdio":     blockedStandalone,
	"exec":        blockedStandalone,
	"--json":      blockedStandalone,
	"--auto":      blockedStandalone,
	"--workspace": blockedWithValue,
	"--model":     blockedWithValue,
	"-p":          blockedWithValue,
	"--prompt":    blockedWithValue,
}

// deepseekBackend implements Backend by spawning `deepseek-tui exec --json
// --auto` and parsing the final JSON result. Current DeepSeek-TUI releases no
// longer expose the old native app-server JSON-RPC transport; this adapter
// therefore reports a final assistant message after the CLI exits rather than
// streaming token/tool deltas while it runs.
type deepseekBackend struct {
	cfg Config
}

type deepseekExecTool struct {
	Name    string          `json:"name"`
	Success bool            `json:"success"`
	Output  string          `json:"output"`
	Error   json.RawMessage `json:"error"`
}

type deepseekExecResult struct {
	Mode    string             `json:"mode"`
	Model   string             `json:"model"`
	Prompt  string             `json:"prompt"`
	Output  string             `json:"output"`
	Tools   []deepseekExecTool `json:"tools"`
	Status  string             `json:"status"`
	Success *bool              `json:"success"`
	Error   json.RawMessage    `json:"error"`
	Usage   json.RawMessage    `json:"usage"`
}

func (b *deepseekBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "deepseek-tui"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("deepseek-tui executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildDeepseekExecArgs(prompt, opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = 10 * time.Second
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[deepseek:stderr] "), &stderr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start deepseek-tui: %w", err)
	}

	b.cfg.Logger.Info("deepseek-tui exec started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		result := parseDeepseekExecResult(stdout.Bytes(), stderr.String(), exitErr)
		if runCtx.Err() == context.DeadlineExceeded {
			result.Status = "timeout"
			result.Error = fmt.Sprintf("deepseek-tui timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			result.Status = "aborted"
			result.Error = "execution cancelled"
		}
		result.DurationMs = duration.Milliseconds()

		for i, tool := range result.deepseekTools {
			toolName := deepseekToolName(tool.Name)
			if toolName == "" {
				continue
			}
			callID := fmt.Sprintf("deepseek-tool-%d", i+1)
			trySend(msgCh, Message{Type: MessageToolUse, Tool: toolName, CallID: callID})
			output := strings.TrimSpace(tool.Output)
			if output == "" {
				output = deepseekErrorString(tool.Error)
			}
			trySend(msgCh, Message{Type: MessageToolResult, Tool: toolName, CallID: callID, Output: output})
		}
		if result.Output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: result.Output})
		}

		b.cfg.Logger.Info("deepseek-tui exec finished", "pid", cmd.Process.Pid, "status", result.Status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status:     result.Status,
			Output:     result.Output,
			Error:      result.Error,
			DurationMs: result.DurationMs,
			Usage:      result.Usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func buildDeepseekExecArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := make([]string, 0, 8+len(opts.CustomArgs))
	if opts.Cwd != "" {
		args = append(args, "--workspace", opts.Cwd)
	}
	args = append(args, "exec", "--json", "--auto")
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, deepseekBlockedArgs, logger)...)

	userText := prompt
	if opts.SystemPrompt != "" {
		userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}
	args = append(args, userText)
	return args
}

type deepseekParsedResult struct {
	Status        string
	Output        string
	Error         string
	DurationMs    int64
	Usage         map[string]TokenUsage
	deepseekTools []deepseekExecTool
}

func parseDeepseekExecResult(stdout []byte, stderrText string, exitErr error) deepseekParsedResult {
	parsed, err := parseDeepseekExecJSON(stdout)
	if err != nil {
		output := strings.TrimSpace(string(stdout))
		errMsg := strings.TrimSpace(stderrText)
		if errMsg == "" && exitErr != nil {
			errMsg = fmt.Sprintf("deepseek-tui exited with error: %v", exitErr)
		}
		if errMsg == "" && output == "" {
			errMsg = "deepseek-tui returned no output"
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("deepseek-tui returned unparseable JSON output: %v", err)
		}
		status := "failed"
		if exitErr == nil && output != "" {
			status = "completed"
		}
		return deepseekParsedResult{Status: status, Output: output, Error: errMsg}
	}

	status := deepseekResultStatus(parsed)
	errMsg := deepseekErrorString(parsed.Error)
	if status == "failed" && errMsg == "" {
		errMsg = strings.TrimSpace(stderrText)
	}
	if status == "failed" && errMsg == "" && exitErr != nil {
		errMsg = fmt.Sprintf("deepseek-tui exited with error: %v", exitErr)
	}

	var usage map[string]TokenUsage
	if u := tokenUsageFromRawMessage(parsed.Usage); tokenUsageHasTokens(u) {
		model := strings.TrimSpace(parsed.Model)
		if model == "" {
			model = "unknown"
		}
		usage = map[string]TokenUsage{model: u}
	}

	return deepseekParsedResult{
		Status:        status,
		Output:        parsed.Output,
		Error:         errMsg,
		Usage:         usage,
		deepseekTools: parsed.Tools,
	}
}

func parseDeepseekExecJSON(raw []byte) (deepseekExecResult, error) {
	var result deepseekExecResult
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return result, fmt.Errorf("empty output")
	}
	if err := json.Unmarshal(trimmed, &result); err == nil {
		return result, nil
	}

	start := bytes.IndexByte(trimmed, '{')
	end := bytes.LastIndexByte(trimmed, '}')
	if start < 0 || end <= start {
		return result, fmt.Errorf("no JSON object found")
	}
	if err := json.Unmarshal(trimmed[start:end+1], &result); err != nil {
		return result, err
	}
	return result, nil
}

func deepseekResultStatus(result deepseekExecResult) string {
	status := strings.ToLower(strings.TrimSpace(result.Status))
	switch status {
	case "completed", "success", "succeeded", "ok":
		return "completed"
	case "cancelled", "canceled", "aborted":
		return "aborted"
	case "timeout", "timed_out":
		return "timeout"
	case "failed", "error":
		return "failed"
	}
	if result.Success != nil {
		if *result.Success {
			return "completed"
		}
		return "failed"
	}
	if deepseekErrorString(result.Error) != "" {
		return "failed"
	}
	return "completed"
}

func deepseekErrorString(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		for _, key := range []string{"message", "error", "detail"} {
			if value, ok := obj[key]; ok {
				return strings.TrimSpace(fmt.Sprint(value))
			}
		}
	}
	return strings.TrimSpace(string(raw))
}

// deepseekToolName normalises tool names from DeepSeek-TUI into the snake_case
// identifiers the Multica UI expects.
func deepseekToolName(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return ""
	}

	lower := strings.ToLower(n)
	switch lower {
	case "read", "read_file":
		return "read_file"
	case "write", "write_file":
		return "write_file"
	case "edit", "edit_file", "patch":
		return "edit_file"
	case "shell", "bash", "terminal", "run_command":
		return "terminal"
	case "search", "grep", "find", "search_files":
		return "search_files"
	case "glob":
		return "glob"
	case "web_search":
		return "web_search"
	case "web_fetch", "fetch":
		return "web_fetch"
	case "todo", "todo_write":
		return "todo_write"
	}

	return strings.ReplaceAll(lower, " ", "_")
}
