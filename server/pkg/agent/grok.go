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

// grokBackend implements Backend by spawning Grok Build in headless
// streaming-json mode (`grok -p <prompt> --output-format streaming-json`) and
// parsing its NDJSON event stream on stdout.
type grokBackend struct {
	cfg Config
}

func (b *grokBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "grok"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("grok executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := buildGrokArgs(prompt, opts, b.cfg.Logger)
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
		return nil, fmt.Errorf("grok stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[grok:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start grok: %w", err)
	}

	b.cfg.Logger.Info("grok started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

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
			var evt grokStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				// In case a future/older Grok version emits plain lines despite
				// streaming-json, preserve useful stdout rather than dropping it.
				output.WriteString(line)
				trySend(msgCh, Message{Type: MessageText, Content: line})
				continue
			}

			switch evt.Type {
			case "thought":
				if evt.Data != "" {
					trySend(msgCh, Message{Type: MessageThinking, Content: evt.Data})
				}
			case "text":
				if evt.Data != "" {
					output.WriteString(evt.Data)
					trySend(msgCh, Message{Type: MessageText, Content: evt.Data})
				}
			case "error":
				finalStatus = "failed"
				finalError = evt.Message
				if finalError == "" {
					finalError = evt.Data
				}
				trySend(msgCh, Message{Type: MessageError, Content: finalError})
			case "end":
				if evt.SessionID != "" {
					sessionID = evt.SessionID
					trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
				}
			}
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("grok timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if err := scanner.Err(); err != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("grok stdout scan failed: %v", err)
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("grok exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("grok finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      map[string]TokenUsage{},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type grokStreamEvent struct {
	Type       string `json:"type"`
	Data       string `json:"data,omitempty"`
	Message    string `json:"message,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
}

// grokBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var grokBlockedArgs = map[string]blockedArgMode{
	"-p":                   blockedWithValue,
	"--single":             blockedWithValue,
	"--prompt-file":        blockedWithValue,
	"--prompt-json":        blockedWithValue,
	"--output-format":      blockedWithValue,
	"--always-approve":     blockedStandalone,
	"--no-alt-screen":      blockedStandalone,
	"--disable-web-search": blockedStandalone,
}

func buildGrokArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--always-approve",
		"--output-format", "streaming-json",
		"--no-alt-screen",
		"--disable-web-search",
	}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "-r", opts.ResumeSessionID)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, grokBlockedArgs, logger)...)
	return args
}
