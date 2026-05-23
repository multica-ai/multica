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

// warpBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var warpBlockedArgs = map[string]blockedArgMode{
	"-p":              blockedWithValue, // prompt
	"--prompt":        blockedWithValue, // prompt
	"--output-format": blockedWithValue, // ndjson stream consumed by daemon
	"-C":              blockedWithValue, // task workdir
	"--cwd":           blockedWithValue, // task workdir
	"--model":         blockedWithValue, // model comes from agent settings
	"--conversation":  blockedWithValue, // session resume pointer
	"--mcp":           blockedWithValue, // daemon-managed MCP payload
}

// warpBackend implements Backend by spawning `oz agent run --output-format ndjson`
// and reading newline-delimited JSON events from stdout.
type warpBackend struct {
	cfg Config
}

func (b *warpBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "oz"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("oz executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildWarpArgs(prompt, opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, args...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("oz stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[warp:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start oz: %w", err)
	}

	b.cfg.Logger.Info("oz started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when the context is cancelled so scanner.Scan unblocks.
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

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt warpEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "system":
				switch evt.EventType {
				case "run_started":
					trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
				case "conversation_started":
					if evt.ConversationID != "" {
						sessionID = evt.ConversationID
					}
				}
				if msg := evt.errorText(); msg != "" {
					finalStatus = "failed"
					finalError = msg
					trySend(msgCh, Message{Type: MessageError, Content: msg})
				}
			case "agent":
				if evt.Text != "" {
					output.WriteString(evt.Text)
					trySend(msgCh, Message{Type: MessageText, Content: evt.Text})
				}
			case "tool_use":
				var in map[string]any
				if len(evt.Input) > 0 {
					_ = json.Unmarshal(evt.Input, &in)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.Tool,
					CallID: evt.CallID,
					Input:  in,
				})
			case "tool_result":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					Tool:   evt.Tool,
					CallID: evt.CallID,
					Output: evt.Output,
				})
			case "error":
				msg := evt.errorText()
				if msg != "" {
					finalStatus = "failed"
					finalError = msg
					trySend(msgCh, Message{Type: MessageError, Content: msg})
				}
			}
		}

		if scanErr := scanner.Err(); scanErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("oz stdout read error: %v", scanErr)
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)
		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("oz timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("oz exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("oz finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      nil,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type warpEvent struct {
	Type string `json:"type"`

	// system events
	EventType      string `json:"event_type,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`

	// text payload
	Text string `json:"text,omitempty"`

	// tool events
	Tool   string          `json:"tool,omitempty"`
	CallID string          `json:"call_id,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output string          `json:"output,omitempty"`

	// error payload
	Error string `json:"error,omitempty"`
}

func (e warpEvent) errorText() string {
	if e.Error != "" {
		return e.Error
	}
	if e.EventType == "error" && e.Text != "" {
		return e.Text
	}
	return ""
}

func buildWarpArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	agentPrompt := prompt
	if opts.SystemPrompt != "" {
		agentPrompt = opts.SystemPrompt + "\n\n---\n\n" + prompt
	}

	args := []string{"agent", "run", "--prompt", agentPrompt, "--output-format", "ndjson"}
	if opts.Cwd != "" {
		args = append(args, "--cwd", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	if len(opts.McpConfig) > 0 {
		args = append(args, "--mcp", string(opts.McpConfig))
	}
	if opts.MaxTurns > 0 {
		logger.Warn("oz does not support max-turns; ignoring", "max_turns", opts.MaxTurns)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, warpBlockedArgs, logger)...)
	return args
}
