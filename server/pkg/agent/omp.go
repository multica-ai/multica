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

// ompBackend implements Backend by spawning the oh-my-pi CLI in
// non-interactive JSON mode (`omp -p --mode json`) and parsing its
// event stream on stdout.
type ompBackend struct {
	cfg Config
}

func (b *ompBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "omp"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("omp executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}

	// omp manages sessions internally at ~/.omp/agent/sessions/.
	// On first run we omit --resume so omp creates a new session.
	// On resume we pass --resume with the session ID captured from
	// the initial stdout header line.
	resumeID := opts.ResumeSessionID

	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildOmpArgs(prompt, resumeID, opts, b.cfg.Logger)

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
		return nil, fmt.Errorf("omp stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[omp:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start omp: %w", err)
	}

	b.cfg.Logger.Info("omp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)
		var sessionID string

		scanner := bufio.NewScanner(stdout)
		// omp message_update events can embed full message partials,
		// so give the scanner generous headroom.
		scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			// First JSON line in omp --mode json is the session header:
			// {"type":"session","version":3,"id":"...","timestamp":"...","cwd":"..."}
			// Capture the session ID for resume.
			if sessionID == "" {
				var hdr ompSessionHeader
				if err := json.Unmarshal([]byte(line), &hdr); err == nil && hdr.Type == "session" && hdr.ID != "" {
					sessionID = hdr.ID
					continue
				}
			}

			var evt ompStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "agent_start":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "message_update":
				if evt.AssistantMessageEvent == nil {
					continue
				}
				switch evt.AssistantMessageEvent.Type {
				case "text_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						output.WriteString(d)
						trySend(msgCh, Message{Type: MessageText, Content: d})
					}
				case "thinking_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: d})
					}
				}

			case "tool_execution_start":
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolCallID,
					Input:  decodeOmpArgs(evt.Args),
				})

			case "tool_execution_end":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolCallID,
					Output: decodeOmpResult(evt.Result),
				})

			case "turn_end":
				if um := decodeOmpUsage(evt.Message); um != nil {
					model := um.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "unknown"
					}
					u := usage[model]
					u.InputTokens += um.Usage.Input
					u.OutputTokens += um.Usage.Output
					u.CacheReadTokens += um.Usage.CacheRead
					u.CacheWriteTokens += um.Usage.CacheWrite
					usage[model] = u
				}

			case "error":
				errText := decodeOmpString(evt.Message)
				trySend(msgCh, Message{Type: MessageError, Content: errText})
				if finalStatus == "completed" {
					finalStatus = "failed"
					finalError = errText
				}

			case "auto_retry_end":
				if !evt.Success && finalStatus == "completed" {
					finalStatus = "failed"
					if evt.FinalError != "" {
						finalError = evt.FinalError
					} else {
						finalError = "omp exhausted automatic retries"
					}
				}
			}
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("omp timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("omp exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("omp finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

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

// ── omp event types ──

// ompSessionHeader is the first JSON line emitted by `omp --mode json`
// in print mode. We capture the session ID for later resume.
type ompSessionHeader struct {
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	ID      string `json:"id"`
	Cwd     string `json:"cwd,omitempty"`
}

// ompStreamEvent is the union of fields consumed from omp's JSON event
// stream. The event shape matches pi's with minor differences: result
// can be any JSON value (not just string), and tool_execution_end uses
// isError (boolean) vs is_error.
type ompStreamEvent struct {
	Type string `json:"type"`

	// message_update
	AssistantMessageEvent *ompAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// tool_execution_start / tool_execution_end
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	// error / turn_end: Message can be string or object
	Message json.RawMessage `json:"message,omitempty"`

	// auto_retry_end
	Success    bool   `json:"success,omitempty"`
	FinalError string `json:"finalError,omitempty"`
}

type ompAssistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

type ompTurnEndMessage struct {
	Role  string   `json:"role,omitempty"`
	Model string   `json:"model,omitempty"`
	Usage *ompUsage `json:"usage,omitempty"`
}

type ompUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	TotalTokens int64 `json:"totalTokens"`
}

func decodeOmpArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func decodeOmpResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func decodeOmpString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func decodeOmpUsage(raw json.RawMessage) *ompTurnEndMessage {
	if len(raw) == 0 {
		return nil
	}
	var m ompTurnEndMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if m.Usage == nil {
		return nil
	}
	return &m
}

// ── Arg builder ──

// ompBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var ompBlockedArgs = map[string]blockedArgMode{
	"-p":         blockedStandalone, // non-interactive mode
	"--print":    blockedStandalone, // alias for -p
	"--mode":     blockedWithValue,  // "json" event stream protocol
	"--resume":   blockedWithValue,  // daemon manages session resume
	"--session":  blockedWithValue,  // alias for --resume
}

// buildOmpArgs assembles the argv for a one-shot omp invocation.
//
// Flags:
//   -p                          non-interactive mode
//   --mode json                 emit one JSON event per line on stdout
//   --resume <id>               resume a previous session (only on resume)
//   --provider <name>           provider override
//   --model <id>                model identifier
//   --append-system-prompt <s>  extra system instructions
//   --auto-approve              skip tool-approval prompts in daemon mode
//
// Custom args appended before the positional prompt.
func buildOmpArgs(prompt, resumeID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--mode", "json",
		"--auto-approve",
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	if opts.Model != "" {
		provider, model := splitOmpModel(opts.Model)
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, ompBlockedArgs, logger)...)
	args = append(args, prompt)
	return args
}

// splitOmpModel parses a "provider/model" string into parts.
func splitOmpModel(s string) (provider, model string) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/"); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "", s
}