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

// geminiBackend implements Backend by spawning Google's AGY CLI
// with `--output-format stream-json` and parsing its NDJSON event stream.
type geminiBackend struct {
	cfg Config
}

func (b *geminiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "agy"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("agy executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildGeminiArgs(prompt, opts, b.cfg.Logger)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildGeminiEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[agy:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so scanner.Scan() unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt geminiStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "init":
				sessionID = evt.SessionID
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "message":
				if evt.Role == "assistant" && evt.Content != "" {
					output.WriteString(evt.Content)
					trySend(msgCh, Message{Type: MessageText, Content: evt.Content})
				}

			case "tool_use":
				var params map[string]any
				if evt.Parameters != nil {
					_ = json.Unmarshal(evt.Parameters, &params)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolID,
					Input:  params,
				})

			case "tool_result":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolID,
					Output: evt.Output,
				})

			case "error":
				trySend(msgCh, Message{
					Type:    MessageError,
					Content: evt.Message,
				})

			case "result":
				if evt.Status == "error" && evt.Error != nil {
					finalStatus = "failed"
					finalError = evt.Error.Message
				}
				if evt.Stats != nil {
					b.accumulateUsage(usage, evt.Stats)
				}
			}
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// accumulateUsage extracts per-model token usage from Gemini's result stats.
func (b *geminiBackend) accumulateUsage(usage map[string]TokenUsage, stats *geminiStreamStats) {
	for model, m := range stats.Models {
		u := usage[model]
		u.InputTokens += int64(m.InputTokens)
		u.OutputTokens += int64(m.OutputTokens)
		u.CacheReadTokens += int64(m.Cached)
		usage[model] = u
	}
}

// ── Gemini stream-json event types ──

type geminiStreamEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// message fields
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	Delta   bool   `json:"delta,omitempty"`

	// tool_use fields
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// tool_result fields
	Status string `json:"status,omitempty"`
	Output string `json:"output,omitempty"`

	// error fields
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`

	// result fields
	Error *geminiStreamError `json:"error,omitempty"`
	Stats *geminiStreamStats `json:"stats,omitempty"`
}

type geminiStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type geminiStreamStats struct {
	TotalTokens  int                         `json:"total_tokens"`
	InputTokens  int                         `json:"input_tokens"`
	OutputTokens int                         `json:"output_tokens"`
	DurationMs   int                         `json:"duration_ms"`
	ToolCalls    int                         `json:"tool_calls"`
	Models       map[string]geminiModelStats `json:"models,omitempty"`
}

type geminiModelStats struct {
	TotalTokens  int `json:"total_tokens"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	Cached       int `json:"cached"`
}

// ── Arg builder ──

// buildGeminiArgs assembles the argv for a one-shot AGY invocation.
//
// Flags:
//
//	-p / --prompt         non-interactive prompt (the user's task)
//	--dangerously-skip-permissions
//	                      auto-approve all tool executions
//	-o stream-json        streaming NDJSON output for live events
//	-m <model>            optional model override
//	-r <session>          resume a previous session (if provided)
//
// geminiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var geminiBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,  // non-interactive prompt
	"--dangerously-skip-permissions": blockedStandalone, // auto-approve tool use
	"--yolo":                         blockedStandalone, // legacy Gemini spelling
	"-o":                             blockedWithValue,  // stream-json output format
}

func buildGeminiArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"-o", "stream-json",
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-r", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, geminiBlockedArgs, logger)...)
	return args
}

// buildGeminiEnv wraps buildEnv and defaults GEMINI_CLI_TRUST_WORKSPACE=true so
// legacy Gemini CLI folder-trust gates do not fail every headless daemon
// invocation with exit code 55 (FatalUntrustedWorkspaceError). AGY stores its
// persistent settings under `~/.gemini/antigravity-cli/settings.json` and the
// daemon supplies AGY's headless permission bypass on the command line, but this
// environment default remains harmless and preserves compatibility for older
// Gemini installations and user-pinned MULTICA_GEMINI_PATH values.
//
// If the caller explicitly sets the same key in cfg.Env it wins, preserving the
// ability to opt back into the check.
func buildGeminiEnv(extra map[string]string) []string {
	const trustKey = "GEMINI_CLI_TRUST_WORKSPACE"
	if _, ok := extra[trustKey]; ok {
		return buildEnv(extra)
	}
	merged := make(map[string]string, len(extra)+1)
	for k, v := range extra {
		merged[k] = v
	}
	merged[trustKey] = "true"
	return buildEnv(merged)
}
