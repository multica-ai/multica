package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// deepseekBlockedArgs are flags hardcoded by the daemon that must not
// be overridden by user-configured custom_args. `app-server` is the
// subcommand and `--stdio` selects the ACP JSON-RPC stdio transport;
// overriding either would break the daemon↔DeepSeek communication
// contract.
var deepseekBlockedArgs = map[string]blockedArgMode{
	"app-server": blockedStandalone,
	"--stdio":    blockedStandalone,
}

// deepseekBackend implements Backend by spawning `deepseek app-server
// --stdio` and communicating via the ACP (Agent Client Protocol)
// JSON-RPC 2.0 over stdin/stdout.
//
// DeepSeek-TUI (https://github.com/nicepkg/deepseek-tui) supports ACP
// out of the box via the `deepseek app-server --stdio` subcommand. We
// reuse the existing hermesClient ACP transport since both runtimes
// speak the same protocol — only the binary, env, and tool-name
// extraction differ.
type deepseekBackend struct {
	cfg Config
}

func (b *deepseekBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "deepseek"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("deepseek executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	// `deepseek app-server --stdio` launches the ACP JSON-RPC transport.
	// The daemon auto-approves tool calls in hermesClient.handleAgentRequest
	// by replying "approve_for_session" to every session/request_permission
	// request.
	dsArgs := append([]string{"app-server", "--stdio"}, filterCustomArgs(opts.CustomArgs, deepseekBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, dsArgs...)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", dsArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deepseek stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deepseek stdin pipe: %w", err)
	}
	// Forward stderr to the daemon log and sniff provider-level errors.
	providerErr := newACPProviderErrorSniffer("deepseek")
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[deepseek:stderr] "), providerErr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start deepseek: %w", err)
	}

	b.cfg.Logger.Info("deepseek acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	promptDone := make(chan hermesPromptResult, 1)

	// Reuse the hermesClient ACP transport — DeepSeek speaks the same protocol.
	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		onMessage: func(msg Message) {
			if msg.Type == MessageToolUse {
				msg.Tool = deepseekToolNameFromTitle(msg.Tool)
			}
			if msg.Type == MessageText {
				outputMu.Lock()
				output.WriteString(msg.Content)
				outputMu.Unlock()
			}
			trySend(msgCh, msg)
		},
		onPromptDone: func(result hermesPromptResult) {
			select {
			case promptDone <- result:
			default:
			}
		},
	}

	// Start reading stdout in background.
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
		c.closeAllPending(fmt.Errorf("deepseek process exited"))
	}()

	// Drive the ACP session lifecycle in a goroutine.
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

		// 1. Initialize handshake.
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
			finalError = fmt.Sprintf("deepseek initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":       cwd,
				"sessionId": opts.ResumeSessionID,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek session/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = opts.ResumeSessionID
			_ = result
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "deepseek session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("deepseek session created", "session_id", sessionID)

		// 3. If the caller picked a model, ask deepseek to switch the
		// session to it before we send any prompt.
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("deepseek set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek could not switch to model %q: %v", opts.Model, err)
				resCh <- Result{
					Status:     finalStatus,
					Error:      finalError,
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  sessionID,
				}
				return
			}
			b.cfg.Logger.Info("deepseek session model set", "model", opts.Model)
		}

		// 4. Build the prompt content.
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// 5. Send the prompt and wait for PromptResponse.
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("deepseek timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("deepseek session/prompt failed: %v", err)
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "deepseek cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("deepseek finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// If deepseek produced no visible output but we sniffed a
		// provider-level error on stderr, promote the status to failed
		// and surface the real reason.
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

// deepseekToolNameFromTitle normalises tool names emitted by
// DeepSeek-TUI's ACP server into the snake_case identifiers the
// Multica UI expects.
//
// DeepSeek-TUI follows the ACP spec where `title` is a short
// human-readable label such as "Read file: /path/to/foo.go" or "Run
// command: ls". The mapping is shared with Kimi and Kiro — all three
// use the same capitalised ACP title convention.
func deepseekToolNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}

	if idx := strings.Index(t, ":"); idx > 0 {
		t = strings.TrimSpace(t[:idx])
	}

	lower := strings.ToLower(t)
	switch lower {
	case "read", "read file":
		return "read_file"
	case "write", "write file":
		return "write_file"
	case "edit", "patch":
		return "edit_file"
	case "shell", "bash", "terminal", "run command", "run shell command":
		return "terminal"
	case "search", "grep", "find":
		return "search_files"
	case "glob":
		return "glob"
	case "web search":
		return "web_search"
	case "fetch", "web fetch":
		return "web_fetch"
	case "todo", "todo write":
		return "todo_write"
	}

	return strings.ReplaceAll(lower, " ", "_")
}
