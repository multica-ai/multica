package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// devinBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol subcommand,
// and --permission-mode=dangerous ensures autonomous operation (auto-approve
// every tool) since there is no human in the loop for Multica agent runs.
var devinBlockedArgs = map[string]blockedArgMode{
	"acp":                blockedStandalone,
	"--permission-mode":  blockedWithValue,
	"--config":           blockedWithValue,
	"--agent-config":     blockedWithValue,
}

// devinBackend implements Backend by spawning `devin acp` and communicating
// via the standard ACP JSON-RPC 2.0 transport over stdin/stdout.
//
// Devin CLI (v2026.5.26+) advertises loadSession, returns models from
// session/new, and supports session/set_model, so the existing Hermes/Kimi ACP
// client can drive it with only provider-specific launch and tool-name
// normalization.
type devinBackend struct {
	cfg Config
}

func (b *devinBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "devin"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("devin executable not found at %q: %w", execPath, err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP `session/new` and `session/load` expect.
	// Fail closed on malformed JSON so the launch surfaces the real error
	// instead of silently dropping all MCP servers.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("devin: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	devinArgs := append([]string{"acp", "--permission-mode=dangerous"}, filterCustomArgs(opts.CustomArgs, devinBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, devinArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", devinArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("devin stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("devin stdin pipe: %w", err)
	}
	// StderrPipe + an explicit copier give us a join point
	// (`stderrDone`) that fires before the failure-promotion
	// decision; see the matching comment in hermes.go for why the
	// io.MultiWriter form races with stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("devin")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("devin stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start devin: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[devin:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("devin acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder
	var streamingCurrentTurn atomic.Bool
	var sawCompletedGoalComplete atomic.Bool
	var sawCompletedIssueComment atomic.Bool
	var goalCompleteCallIDs sync.Map
	var issueCommentCallIDs sync.Map

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
				msg.Tool = devinToolNameFromTitle(msg.Tool)
				if msg.Tool == "goal_complete" && msg.CallID != "" {
					goalCompleteCallIDs.Store(msg.CallID, struct{}{})
				}
				if msg.CallID != "" && isDevinIssueCommentAddTool(msg) {
					issueCommentCallIDs.Store(msg.CallID, struct{}{})
				}
			}
			if msg.Type == MessageToolResult {
				if _, ok := goalCompleteCallIDs.LoadAndDelete(msg.CallID); ok {
					sawCompletedGoalComplete.Store(msg.Status == "completed")
				}
				if _, ok := issueCommentCallIDs.LoadAndDelete(msg.CallID); ok {
					sawCompletedIssueComment.Store(msg.Status == "completed")
				}
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
		c.closeAllPending(fmt.Errorf("devin process exited"))
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

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("devin initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime
		// tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "devin", b.cfg.Logger)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("devin session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			// Apply the same defensive resolution kimi/hermes use: if
			// devin echoes a sessionId in the session/load response, prefer
			// it (the canonical id the backend is committed to). When the
			// response is empty or doesn't include sessionId — devin's
			// current observed shape — the helper falls back to the
			// requested id, preserving today's behavior. Fixing this here
			// too means a future devin that DOES return a different id on
			// silent state reset is handled the same way as hermes/kimi.
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "devin",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("devin session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "devin session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("devin session created", "session_id", sessionID)

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("devin set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("devin could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// On a resumed session with a model override, the dead
					// session surfaces here instead of at session/prompt.
					// Same fix as the prompt path below: clear the id so
					// the daemon's resume-failure fallback retries fresh.
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "devin",
						"session_id", sessionID,
					)
					sessionID = ""
				}
				resCh <- Result{
					Status:     finalStatus,
					Error:      finalError,
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  sessionID,
				}
				return
			}
			b.cfg.Logger.Info("devin session model set", "model", opts.Model)
		}

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}
		// Send prompt field per standard ACP shape.
		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    promptBlocks,
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("devin timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("devin session/prompt failed: %v", err)
				if (sawCompletedGoalComplete.Load() || sawCompletedIssueComment.Load()) && isDevinGoalCompleteCloseError(err) {
					b.cfg.Logger.Warn("devin session/prompt failed after completed task result; preserving completed task status", "error", err)
					finalStatus = "completed"
					finalError = ""
				} else if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes backend: the runtime echoes the
					// requested id back from session/resume even when
					// the session is gone, so the stale id only fails
					// here, at prompt time. Empty SessionID lets the
					// daemon's resume-failure fallback retry fresh and
					// store the replacement id.
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "devin",
						"session_id", sessionID,
					)
					sessionID = ""
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "devin cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("devin finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		// Ensure the stderr copier has drained before consulting the
		// provider-error sniffer; see hermes.go for the failure mode.
		<-stderrDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// Promote completed→failed when stderr or the agent text
		// stream show a terminal upstream-LLM failure (HTTP 4xx /
		// rate-limit / expired token). See the helper docs for the
		// full signal set; the key safety property is that transient
		// per-attempt warnings followed by a successful retry stay
		// "completed".
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
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func isDevinGoalCompleteCloseError(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if rpcErr.Method != "session/prompt" || rpcErr.Code != -32603 {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(rpcErr.Message), "Internal error") {
		return false
	}
	return strings.Contains(strings.ToLower(rpcErr.Data), "failed to generate a response")
}

func isDevinIssueCommentAddTool(msg Message) bool {
	if msg.Tool != "terminal" {
		return false
	}
	command, _ := msg.Input["command"].(string)
	return isDevinIssueCommentAddCommand(command)
}

func isDevinIssueCommentAddCommand(command string) bool {
	parts := trimLeadingEnvAssignments(strings.Fields(command))
	// Some runtimes route terminal calls through a shell wrapper such as
	// `sh -c "multica issue comment add ..."`; unwrap a single such layer so
	// the real invocation is still recognized.
	if len(parts) >= 3 && isPOSIXShellName(parts[0]) && parts[1] == "-c" {
		inner := strings.Trim(strings.Join(parts[2:], " "), "\"'")
		parts = trimLeadingEnvAssignments(strings.Fields(inner))
	}
	if len(parts) < 4 {
		return false
	}
	executable := strings.TrimPrefix(parts[0], "./")
	if executable != "multica" && !strings.HasSuffix(executable, "/multica") {
		return false
	}
	return parts[1] == "issue" && parts[2] == "comment" && parts[3] == "add"
}

func devinToolNameFromTitle(title string) string {
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
	case "grep", "search", "find":
		return "search_files"
	case "glob":
		return "glob"
	case "code":
		return "code"
	case "web search":
		return "web_search"
	case "fetch", "web fetch":
		return "web_fetch"
	case "todo", "todo write", "todo list", "todo_list":
		return "todo_write"
	}

	return strings.ReplaceAll(lower, " ", "_")
}
