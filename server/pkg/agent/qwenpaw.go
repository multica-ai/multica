package agent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// qwenpawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport; overriding it
// would break the daemon↔QwenPaw communication contract. `--workspace`
// is set per-task by the daemon for skill isolation.
var qwenpawBlockedArgs = map[string]blockedArgMode{
	"acp":         blockedStandalone,
	"--workspace": blockedWithValue,
}

// qwenpawBackend implements Backend by spawning `qwenpaw acp` and
// communicating via the ACP (Agent Client Protocol) JSON-RPC 2.0 over
// stdin/stdout.
//
// QwenPaw's ACP server (`qwenpaw acp`) supports the same protocol that
// Hermes/Kimi/Kiro/Traecli use, so we reuse the hermesClient ACP
// transport — only the binary, env, and tool-name extraction differ.
//
// Minimum supported QwenPaw version: v2.0.1 (the current stable release).
// Every ACP feature the execution path below relies on is present there;
// see TestQwenpawRealACPSmoke for the integration check. The code targets
// the v2.0 ACP API surface — v1.x speaks a different protocol and is not
// supported.
//
// Notable contract with QwenPaw v2.0.1:
//   - `session/new` and `session/load` accept `_meta["qwenpaw.coding_project_dir"]`
//     to enable Coding Mode (qwenpaw.coding_project_dir meta key).
//   - `session/set_model` is NOT called: it persists to agent.json at the
//     agent scope rather than the session scope, so calling it would mutate
//     the user's shared agent config. Model override is therefore declared
//     unsupported (see ModelSelectionSupported), and ListModels returns an
//     empty catalog without spawning a discovery subprocess. QwenPaw did
//     gain a `models` field on `session/new` in v2.1.0-beta.1
//     (agentscope-ai/QwenPaw#6531), but with selection unsupported there is
//     no consumer for that catalog.
//   - Skills are discovered from the workspace's `skills` directory
//     (not `skill_pool`) — see execenv/qwenpaw_workspace.go.
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

	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("qwenpaw: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	// `qwenpaw acp` runs the ACP agent loop over stdio. The daemon
	// auto-approves in hermesClient.handleAgentRequest by selecting
	// a safe granting option for each session/request_permission request.
	qwenpawArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, qwenpawBlockedArgs, b.cfg.Logger)...)

	// Inject --workspace for per-task isolation. This is blocked in
	// qwenpawBlockedArgs so user-defined custom_args cannot override it.
	// The daemon sets it via opts.QwenpawWorkspace.
	if opts.QwenpawWorkspace != "" {
		qwenpawArgs = append(qwenpawArgs, "--workspace", opts.QwenpawWorkspace)
	}

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
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("qwenpaw stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qwenpaw: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[qwenpaw:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("qwenpaw acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	promptDone := make(chan hermesPromptResult, 1)

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		onMessage: func(msg Message) {
			// hermesClient handles ACP tool names via hermesToolNameFromTitle.
			// QwenPaw's tool names are already in the standard format, so no
			// re-normalisation is needed.
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

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
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
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		var resumeRejected bool

		// 1. Initialize handshake.
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
			finalError = fmt.Sprintf("qwenpaw initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "qwenpaw", b.cfg.Logger)

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			loadParams := map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			}
			if cwd != "." {
				loadParams["_meta"] = map[string]any{
					"qwenpaw.coding_project_dir": cwd,
				}
			}
			result, err := c.request(runCtx, "session/load", loadParams)
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("qwenpaw session/load failed: %v", err)
				if isACPSessionNotFound(err) {
					sessionID = ""
					resumeRejected = true
				}
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "qwenpaw",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
		} else {
			newParams := map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			}
			if cwd != "." {
				newParams["_meta"] = map[string]any{
					"qwenpaw.coding_project_dir": cwd,
				}
			}
			result, err := c.request(runCtx, "session/new", newParams)
			if err != nil {
				// Handle context timeout as "session/new failed"
				if runCtx.Err() == context.DeadlineExceeded {
					finalStatus = "timeout"
					finalError = fmt.Sprintf("qwenpaw timed out during session/new: %v", timeout)
				} else if runCtx.Err() == context.Canceled {
					finalStatus = "aborted"
					finalError = fmt.Sprintf("qwenpaw aborted: %v", err)
				} else {
					finalStatus = "failed"
					finalError = fmt.Sprintf("qwenpaw session/new failed: %v", err)
				}
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

		if sessionID == "" {
			// Can't continue without a session.
			finalStatus = "failed"
			finalError = "qwenpaw session ID is empty"
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
			return
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("qwenpaw session created", "session_id", sessionID)

		// 3. Build the prompt content. If we have a system prompt, prepend it.
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// 4. Send the prompt and wait for PromptResponse.
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
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
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					duration := time.Since(startTime)
					b.cfg.Logger.Info("qwenpaw prompt cancelled", "stopReason", pr.stopReason, "duration", duration.Round(time.Millisecond).String())
				}
				c.mergeUsage(pr.usage)
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("qwenpaw finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone
		<-stderrDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		u := c.accumulatedUsage()

		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			// QwenPaw's model selection is unsupported — the backend never
			// sends opts.Model to the agent. Always attribute usage to
			// "unknown" rather than a model that was never applied.
			usageMap = map[string]TokenUsage{"unknown": u}
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      sessionID,
			ResumeRejected: resumeRejected,
			Usage:          usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
