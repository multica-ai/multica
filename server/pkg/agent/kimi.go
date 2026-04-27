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

// kimiBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport for Kimi Code CLI;
// overriding it would break the daemon↔Kimi communication contract.
var kimiBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

// kimiBackend implements Backend by spawning `kimi acp` and communicating
// via the ACP (Agent Client Protocol) JSON-RPC 2.0 over stdin/stdout.
//
// Kimi Code CLI (https://github.com/MoonshotAI/kimi-cli) supports ACP out of
// the box via the `kimi acp` subcommand. We reuse the existing hermesClient
// ACP transport since both runtimes speak the same protocol — only the
// binary, env, and tool-name extraction differ.
type kimiBackend struct {
	cfg Config
}

func (b *kimiBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "kimi"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("kimi executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	// `kimi acp` ignores --yolo / --auto-approve (they're flags on the
	// root `kimi` command, not on the `acp` subcommand). Instead, the
	// daemon auto-approves in hermesClient.handleAgentRequest by replying
	// "approve_for_session" to every session/request_permission request.
	kimiArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, kimiBlockedArgs, b.cfg.Logger)...)
	cmd := exec.CommandContext(runCtx, execPath, kimiArgs...)
	hideAgentWindow(cmd)
	b.cfg.Logger.Info("agent command", "exec", execPath, "args", kimiArgs)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kimi stdin pipe: %w", err)
	}
	// Forward stderr to the daemon log *and* sniff provider-level
	// errors out of it so we can surface them in the task result.
	// Kimi's session/prompt still reports stopReason=end_turn when
	// the underlying HTTP call to api.kimi.com returns 4xx/5xx, so
	// without this the daemon reports a misleading "empty output"
	// and the actionable error (expired token, rate limit, upstream
	// 5xx, …) stays buried in the daemon log.
	providerErr := newACPProviderErrorSniffer("kimi")
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[kimi:stderr] "), providerErr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start kimi: %w", err)
	}

	b.cfg.Logger.Info("kimi acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	promptDone := make(chan hermesPromptResult, 1)

	// Reuse the hermesClient ACP transport — Kimi speaks the same protocol.
	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		onMessage: func(msg Message) {
			// hermesClient.handleToolCallStart has already mapped
			// the raw ACP title via hermesToolNameFromTitle — which
			// covers lowercase hermes-style titles ("read:", "patch
			// (replace)", …) but not capitalised kimi-style ones
			// ("Read file: …", "Run command: …"). Re-normalise so
			// the UI sees consistent snake_case identifiers across
			// both backends. No-op when the name is already normal
			// form (e.g. already mapped to "read_file").
			if msg.Type == MessageToolUse {
				msg.Tool = kimiToolNameFromTitle(msg.Tool)
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
		c.closeAllPending(fmt.Errorf("kimi process exited"))
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
			finalError = fmt.Sprintf("kimi initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// 2. Create or resume a session.
		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		// Attempt to resume a prior session if one was provided. On failure,
		// fall back to a new session so the task is not blocked by a stale or
		// missing session ID. The new session ID is returned in the result so
		// the caller can update its stored mapping.
		if opts.ResumeSessionID != "" {
			_, resumeErr := c.request(runCtx, "session/resume", map[string]any{
				"cwd":       cwd,
				"sessionId": opts.ResumeSessionID,
			})
			if resumeErr != nil {
				b.cfg.Logger.Warn("kimi session/resume failed; falling back to new session",
					"old_session_id", opts.ResumeSessionID, "error", resumeErr)
				// sessionID stays "", session/new runs below.
			} else {
				sessionID = opts.ResumeSessionID
			}
		}

		if sessionID == "" {
			// Either no prior session was provided, or resume failed above.
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "kimi session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if opts.ResumeSessionID != "" {
				b.cfg.Logger.Info("kimi session recovery: new session created after failed resume",
					"old_session_id", opts.ResumeSessionID, "new_session_id", sessionID)
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("kimi session created", "session_id", sessionID)

		// 3. If the caller picked a model (via agent.model from the
		// UI dropdown), ask kimi to switch the session to it before
		// we send any prompt. Kimi's ACP server exposes
		// `session/set_model` and advertises available models via
		// the `models.availableModels` block returned by
		// `session/new` — we pass the chosen modelId through
		// verbatim. This MUST fail the task on error: silently
		// falling back to kimi's default model would let the user
		// believe their pick was honoured while the task actually
		// ran on something else.
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("kimi set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi could not switch to model %q: %v", opts.Model, err)
				resCh <- Result{
					Status:     finalStatus,
					Error:      finalError,
					DurationMs: time.Since(startTime).Milliseconds(),
					SessionID:  sessionID,
				}
				return
			}
			b.cfg.Logger.Info("kimi session model set", "model", opts.Model)
		}

		// 4. Build the prompt content. If we have a system prompt, prepend it.
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		// 5. Send the prompt and wait for PromptResponse.
		_, promptErr := c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
		})

		// If the prompt failed with a stale-session error on a resumed session,
		// create a new session and retry the prompt exactly once.
		if promptErr != nil && isACPStaleSessionError(promptErr) &&
			opts.ResumeSessionID != "" && sessionID == opts.ResumeSessionID {
			b.cfg.Logger.Warn("kimi prompt: stale session after apparent successful resume; retrying on new session",
				"old_session_id", sessionID, "error", promptErr)

			retryResult, newErr := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
			})
			if newErr != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi session recovery failed (session/new): %v", newErr)
				promptErr = nil
			} else {
				newSessionID := extractACPSessionID(retryResult)
				if newSessionID == "" {
					finalStatus = "failed"
					finalError = "kimi session recovery failed: session/new returned no session ID"
					promptErr = nil
				} else {
					b.cfg.Logger.Info("kimi stale-session recovery: retrying prompt on new session",
						"old_session_id", sessionID, "new_session_id", newSessionID)
					sessionID = newSessionID
					c.sessionID = sessionID

					if opts.Model != "" {
						if _, modelErr := c.request(runCtx, "session/set_model", map[string]any{
							"sessionId": sessionID,
							"modelId":   opts.Model,
						}); modelErr != nil {
							b.cfg.Logger.Warn("kimi set_session_model failed on recovery session",
								"error", modelErr, "model", opts.Model)
							finalStatus = "failed"
							finalError = fmt.Sprintf("kimi could not switch to model %q on recovery session: %v", opts.Model, modelErr)
							promptErr = nil
						}
					}

					if finalStatus == "completed" {
						_, promptErr = c.request(runCtx, "session/prompt", map[string]any{
							"sessionId": sessionID,
							"prompt": []map[string]any{
								{"type": "text", "text": userText},
							},
						})
						if promptErr != nil {
							promptErr = fmt.Errorf("stale-session retry: %w", promptErr)
						}
					}
				}
			}
		}

		if promptErr != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("kimi timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("kimi session/prompt failed: %v", promptErr)
			}
		} else if finalStatus == "completed" {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "kimi cancelled the prompt"
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usageMu.Unlock()
			default:
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("kimi finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		<-readerDone

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		// If kimi produced no visible output but we sniffed a
		// provider-level error on stderr (typically HTTP 4xx from
		// api.kimi.com — token expired, rate-limited, upstream
		// 5xx, …), promote the status to failed and surface the
		// real reason. Without this the daemon reports a cryptic
		// "completed + empty output" and the actionable error
		// stays buried in daemon logs.
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

// kimiToolNameFromTitle normalises tool names emitted by Kimi's ACP
// server into the snake_case identifiers the Multica UI expects.
//
// Kimi follows the ACP spec where `title` is a short human-readable
// label such as "Read file: /path/to/foo.go" or "Run command: ls".
// hermesToolNameFromTitle upstream handles hermes' lowercase
// convention ("read:", "patch (replace)") but not kimi's capitalised
// format — so we get called on the already-mapped name from hermes
// and fix up anything that slipped through. Empty input returns "".
func kimiToolNameFromTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}

	// Strip everything after the first colon — ACP titles often look like
	// "Tool Name: argument detail" and we want only the tool name.
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

	// Fallback: snake_case the title so the UI gets a stable identifier.
	return strings.ReplaceAll(lower, " ", "_")
}
