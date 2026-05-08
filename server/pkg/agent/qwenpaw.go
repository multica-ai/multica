package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// qwenpawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. QwenPaw is launched as an ACP
// stdio server and the Multica task directory is passed as the QwenPaw
// workspace so file tools operate inside the isolated task environment.
var qwenpawBlockedArgs = map[string]blockedArgMode{
	"acp":                  blockedStandalone,
	"--bypass-permissions": blockedStandalone,
	"--workspace":          blockedWithValue,
}

// qwenpawBackend implements Backend by spawning `qwenpaw acp` and speaking the
// standard ACP JSON-RPC transport over stdin/stdout. It reuses Multica's shared
// hermesClient ACP adapter because QwenPaw's `qwenpaw acp` command uses the
// official agent-client-protocol SDK.
type qwenpawBackend struct {
	cfg Config
}

func (b *qwenpawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "qwenpaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("qwenpaw executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	qwenpawArgs := []string{"acp"}
	if qwenpawBypassPermissions(b.cfg.Env) {
		qwenpawArgs = append(qwenpawArgs, "--bypass-permissions")
	}
	if opts.Cwd != "" {
		qwenpawArgs = append(qwenpawArgs, "--workspace", opts.Cwd)
	}
	qwenpawArgs = append(qwenpawArgs, filterCustomArgs(opts.CustomArgs, qwenpawBlockedArgs, b.cfg.Logger)...)

	cmd := exec.CommandContext(runCtx, execPath, qwenpawArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", qwenpawArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("qwenpaw stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("qwenpaw stdin pipe: %w", err)
	}
	providerErr := newACPProviderErrorSniffer("qwenpaw")
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[qwenpaw:stderr] "), providerErr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qwenpaw: %w", err)
	}

	b.cfg.Logger.Info("qwenpaw acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

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
			if msg.Type == MessageToolUse {
				msg.Tool = qwenpawToolNameFromTitle(msg.Tool)
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
		c.closeAllPending(fmt.Errorf("qwenpaw process exited"))
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			_ = stdin.Close()
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
			finalError = fmt.Sprintf("qwenpaw initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := qwenpawLoadOrResumeSession(runCtx, c, cwd, opts.ResumeSessionID)
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("qwenpaw session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume; continuing with the new id",
					"backend", "qwenpaw",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("qwenpaw session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "qwenpaw session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("qwenpaw session created", "session_id", sessionID)

		if qwenpawBypassPermissions(b.cfg.Env) {
			if _, err := c.request(runCtx, "session/set_config_option", map[string]any{
				"sessionId":  sessionID,
				"session_id": sessionID,
				"configId":   "mode",
				"config_id":  "mode",
				"value":      "bypassPermissions",
			}); err != nil {
				b.cfg.Logger.Warn("qwenpaw bypassPermissions mode was not accepted; continuing with QwenPaw defaults", "error", err)
			}
		}

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("qwenpaw set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("qwenpaw could not switch to model %q: %v", opts.Model, err)
				resCh <- Result{
					Status:     finalStatus,
					Error:      finalError,
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  sessionID,
				}
				return
			}
			b.cfg.Logger.Info("qwenpaw session model set", "model", opts.Model)
		}

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}
		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}

		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"content":   promptBlocks,
			"prompt":    promptBlocks,
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("qwenpaw timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("qwenpaw session/prompt failed: %v", err)
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "qwenpaw cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("qwenpaw finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		_ = stdin.Close()
		cancel()
		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		if finalStatus == "completed" && finalOutput == "" {
			if msg := providerErr.message(); msg != "" {
				finalStatus = "failed"
				finalError = msg
			}
		}

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
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func qwenpawLoadOrResumeSession(ctx context.Context, c *hermesClient, cwd, sessionID string) ([]byte, error) {
	result, err := c.request(ctx, "session/load", map[string]any{
		"cwd":        cwd,
		"sessionId":  sessionID,
		"mcpServers": []any{},
	})
	if err == nil {
		return result, nil
	}
	return c.request(ctx, "session/resume", map[string]any{
		"cwd":        cwd,
		"sessionId":  sessionID,
		"mcpServers": []any{},
	})
}

func qwenpawBypassPermissions(env map[string]string) bool {
	if value := strings.TrimSpace(env["MULTICA_QWENPAW_BYPASS_PERMISSIONS"]); value != "" {
		switch strings.ToLower(value) {
		case "0", "false", "no", "off":
			return false
		default:
			return true
		}
	}
	if value := strings.TrimSpace(os.Getenv("MULTICA_QWENPAW_BYPASS_PERMISSIONS")); value != "" {
		switch strings.ToLower(value) {
		case "0", "false", "no", "off":
			return false
		default:
			return true
		}
	}
	return true
}

func qwenpawToolNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	if idx := strings.Index(t, ":"); idx > 0 {
		t = strings.TrimSpace(t[:idx])
	}

	lower := strings.ToLower(t)
	// Check exact matches first, then prefix-based fallbacks.
	// Order matters: check exact "file_search" before prefix "file" → "file_search" is wrong.
	switch lower {
	case "read", "read file", "file_read", "file reader":
		return "read_file"
	case "write", "write file", "file_write":
		return "write_file"
	case "edit", "patch", "apply patch":
		return "edit_file"
	case "shell", "bash", "terminal", "execute_shell_command", "run command", "run shell command":
		return "terminal"
	case "glob":
		return "glob"
	case "file_search":
		return "search_files"
	case "search_query":
		return "web_search"
	case "web search":
		return "web_search"
	case "fetch", "web fetch", "open":
		return "web_fetch"
	case "todo", "todo write", "todo_list":
		return "todo_write"
	}
	if strings.HasPrefix(lower, "grep") || strings.HasPrefix(lower, "search") || strings.HasPrefix(lower, "find") {
		return "search_files"
	}
	return strings.ReplaceAll(lower, " ", "_")
}