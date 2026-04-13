package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// geminiBackend implements Backend by spawning `gemini -p <prompt> -o stream-json -y`
// and reading streaming JSON events from stdout.
type geminiBackend struct {
	cfg Config
}

func (b *geminiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "gemini"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("gemini executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{"-p", prompt, "-o", "stream-json", "-y", "--raw-output", "--accept-raw-output-risk"}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-r", opts.ResumeSessionID)
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("gemini does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gemini stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[gemini:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start gemini: %w", err)
	}

	b.cfg.Logger.Info("gemini started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("gemini timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("gemini exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("gemini finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 {
			model := scanResult.model
			if model == "" {
				model = opts.Model
			}
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// geminiEventResult holds the accumulated state from processing the event stream.
type geminiEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	model     string
	usage     TokenUsage
}

// processEvents reads JSON lines from r, dispatches events to ch, and returns
// the accumulated result.
func (b *geminiBackend) processEvents(r io.Reader, ch chan<- Message) geminiEventResult {
	var output strings.Builder
	var sessionID string
	var model string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event geminiEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		switch event.Type {
		case "init":
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			if event.Model != "" {
				model = event.Model
			}
			trySend(ch, Message{Type: MessageStatus, Status: "initialized"})

		case "message":
			if event.Role == "assistant" && event.Content != "" {
				output.WriteString(event.Content)
				trySend(ch, Message{Type: MessageText, Content: event.Content})
			}

		case "tool_use":
			var input map[string]any
			if event.Parameters != nil {
				_ = json.Unmarshal(event.Parameters, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   event.ToolName,
				CallID: event.ToolID,
				Input:  input,
			})

		case "tool_result":
			outputStr := ""
			if event.Output != nil {
				outputStr = extractToolOutput(event.Output)
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				Tool:   event.ToolName,
				CallID: event.ToolID,
				Output: outputStr,
			})

		case "error":
			errMsg := event.Content
			if errMsg == "" {
				errMsg = "unknown gemini error"
			}
			if b.cfg.Logger != nil {
				b.cfg.Logger.Warn("gemini error event", "error", errMsg)
			}
			trySend(ch, Message{Type: MessageError, Content: errMsg})
			finalStatus = "failed"
			finalError = errMsg

		case "usage":
			if event.UsageData != nil {
				usage.InputTokens += event.UsageData.InputTokens
				usage.OutputTokens += event.UsageData.OutputTokens
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		if b.cfg.Logger != nil {
			b.cfg.Logger.Warn("gemini stdout scanner error", "error", scanErr)
		}
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	return geminiEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		model:     model,
		usage:     usage,
	}
}

// geminiEvent represents a single JSON line from `gemini -o stream-json`.
//
// Event types observed:
//
//	"init"        — session initialization with session_id and model
//	"message"     — text output (role: "user"|"assistant", content, delta)
//	"tool_use"    — tool invocation (tool_name, tool_id, parameters)
//	"tool_result" — tool result (tool_id, status, output)
//	"error"       — error event
//	"usage"       — token usage stats
type geminiEvent struct {
	Type    string          `json:"type"`
	Role    string          `json:"role,omitempty"`
	Content string          `json:"content,omitempty"`
	Delta   bool            `json:"delta,omitempty"`

	// init event
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// tool_use / tool_result
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	Output     any             `json:"output,omitempty"`
	Status     string          `json:"status,omitempty"`

	// usage
	UsageData *geminiUsage `json:"usage,omitempty"`
}

type geminiUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
