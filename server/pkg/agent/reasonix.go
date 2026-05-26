package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-shellwords"
)

var reasonixBlockedArgs = map[string]blockedArgMode{
	"reasonix": blockedStandalone,
	"acp":      blockedStandalone,
	"--dir":    blockedWithValue,
	"--model":  blockedWithValue,
	"--yolo":   blockedStandalone,
}

// reasonixBackend implements Backend by spawning `reasonix acp` via either the
// direct binary or `npx reasonix acp`, then speaking ACP JSON-RPC 2.0 over
// stdin/stdout. Reasonix currently does not support mid-session model switches
// or session resume, so model selection happens only at process launch.
type reasonixBackend struct {
	cfg Config
}

func (b *reasonixBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath, fixedArgs, err := resolveReasonixCommand(b.cfg.ExecutablePath)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("reasonix executable not found at %q: %w", execPath, err)
	}
	if opts.ResumeSessionID != "" {
		return nil, fmt.Errorf("reasonix does not support session resume")
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	reasonixArgs := buildReasonixArgs(execPath, fixedArgs, opts, b.cfg.Logger)
	cmd := exec.CommandContext(runCtx, execPath, reasonixArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", reasonixArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reasonix stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reasonix stdin pipe: %w", err)
	}
	providerErr := newACPProviderErrorSniffer("reasonix")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reasonix stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start reasonix: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[reasonix:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder
	var streamingCurrentTurn atomic.Bool
	promptDone := make(chan hermesPromptResult, 1)

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		onMessage: func(msg Message) {
			if !streamingCurrentTurn.Load() {
				return
			}
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
		},
		onPromptDone: func(result hermesPromptResult) {
			if !streamingCurrentTurn.Load() {
				return
			}
			select {
			case promptDone <- result:
			default:
			}
		},
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("reasonix process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string

		_, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("reasonix initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}
		result, err := c.request(runCtx, "session/new", map[string]any{
			"cwd":        cwd,
			"mcpServers": []any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("reasonix session/new failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		sessionID = extractACPSessionID(result)
		if sessionID == "" {
			finalStatus = "failed"
			finalError = "reasonix session/new returned no session ID"
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.sessionID = sessionID

		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": prompt},
			},
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("reasonix timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("reasonix session/prompt failed: %v", err)
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "reasonix cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}
		}

		stdin.Close()
		cancel()
		<-readerDone
		<-stderrDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: time.Since(startTime).Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func resolveReasonixCommand(raw string) (string, []string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "npx", []string{"reasonix"}, nil
	}
	parts, err := shellwords.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("parse reasonix command %q: %w", raw, err)
	}
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", nil, fmt.Errorf("parse reasonix command %q: empty command", raw)
	}
	execPath := parts[0]
	fixedArgs := append([]string(nil), parts[1:]...)
	base := strings.ToLower(filepath.Base(execPath))
	if len(fixedArgs) == 0 && base != "reasonix" && base != "reasonix.cmd" {
		fixedArgs = append(fixedArgs, "reasonix")
	}
	return execPath, fixedArgs, nil
}

func buildReasonixArgs(execPath string, fixedArgs []string, opts ExecOptions, logger *slog.Logger) []string {
	args := make([]string, 0, 8+len(opts.CustomArgs))
	args = append(args, fixedArgs...)
	args = append(args, "acp")
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, "--yolo")
	args = append(args, filterCustomArgs(opts.CustomArgs, reasonixBlockedArgs, logger)...)
	return args
}
