package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// devinBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. `acp` is the protocol
// subcommand that drives the ACP JSON-RPC transport for Devin for
// Terminal; overriding it would break the daemon ↔ Devin communication
// contract. `--agent-type` is exposed to users so they can opt into
// Devin's specialized agents (e.g. summarizer).
var devinBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

// devinBackend implements Backend by spawning `devin acp` and communicating
// via the standard ACP (Agent Client Protocol) JSON-RPC 2.0 transport over
// stdin/stdout.
//
// Devin for Terminal (https://docs.devin.ai/) ships an ACP server out of the
// box via the `devin acp` subcommand. We reuse the existing hermesClient ACP
// transport since Devin's wire format matches the protocol Hermes / Kimi /
// Kiro already speak — only the binary, env, and tool-name extraction differ.
//
// Notes on Devin's ACP dialect (verified against devin 2026.4.29-0):
//
//   - agentCapabilities.loadSession is true, so session/load drives resume.
//   - Devin emits session/update notifications with the standard ACP
//     `sessionUpdate` field; hermesClient.normalizeACPUpdate handles this
//     shape natively.
//   - Devin does NOT implement session/set_model. Model selection happens
//     via Devin's own `config_option_update` mechanism (driven by Devin's
//     UI / config file). When opts.Model is set, we surface a warning in
//     the daemon log and continue with Devin's default model rather than
//     failing the task.
//   - Devin's `acp` subcommand does NOT accept the root-level
//     --permission-mode flag. Daemon-mode auto-approval is handled
//     entirely by hermesClient.handleAgentRequest, which inspects the
//     `options` array advertised on each session/request_permission
//     request and selects the most permissive accept-style optionId
//     by canonical ACP `kind` (allow_always > allow_once). For Devin
//     that resolves to optionId="allow_session"; kimi / kiro resolve
//     to "approve_for_session" — same code path, agent-specific IDs.
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

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	devinArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, devinBlockedArgs, b.cfg.Logger)...)
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
	// Forward stderr to the daemon log *and* sniff provider-level
	// errors out of it so they surface in the task result instead of
	// being lost as a misleading "empty output" failure.
	providerErr := newACPProviderErrorSniffer("devin")
	cmd.Stderr = io.MultiWriter(newLogWriter(b.cfg.Logger, "[devin:stderr] "), providerErr)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start devin: %w", err)
	}

	b.cfg.Logger.Info("devin acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

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
				msg.Tool = devinToolNameFromTitle(msg.Tool)
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
		// Single cleanup block, ordered explicitly so we never close
		// msgCh while the reader goroutine could still call trySend
		// on it. Early-return paths (initialize / session/load /
		// session/new / session/prompt failures) used to rely on the
		// deferred close(msgCh) firing without joining the reader,
		// which is a textbook send-on-closed-channel race. Block on
		// readerDone here and the channel closes are guaranteed safe
		// for every exit path.
		defer func() {
			cancel()       // unblock anything bound to runCtx (in-flight RPCs)
			stdin.Close()  // ask the agent to exit gracefully
			_ = cmd.Wait() // wait for the process; stdout closes here
			<-readerDone   // reader's scanner.Scan() returns false, goroutine exits
			close(msgCh)   // safe: the reader was the only msgCh sender, now finished
			close(resCh)   // safe: only this goroutine sends to resCh
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
			finalError = fmt.Sprintf("devin initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			_, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": []any{},
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("devin session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = opts.ResumeSessionID
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": []any{},
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

		// Devin doesn't implement session/set_model — model selection
		// is driven by Devin's own config_option_update mechanism. Log
		// a warning so the requested model isn't silently ignored, but
		// keep the task running with Devin's default model.
		if opts.Model != "" {
			b.cfg.Logger.Warn("devin does not support session/set_model; falling back to Devin's default model", "requested_model", opts.Model)
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

// devinToolNameFromTitle normalizes Devin's ACP tool titles into the
// canonical snake_case identifiers Multica's UI expects. Devin's tool
// naming follows Devin for Terminal's documented tool set (read, write,
// edit, exec, grep, find, etc.), so the mapping is largely a passthrough
// with a few aliases for cosmetic title variations.
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
	case "read", "read file", "view":
		return "read_file"
	case "write", "write file", "create":
		return "write_file"
	case "edit", "patch", "apply", "replace":
		return "edit_file"
	case "exec", "shell", "bash", "terminal", "run", "run command", "run shell command":
		return "terminal"
	case "grep", "search", "search files", "find":
		return "search_files"
	case "glob", "find files":
		return "glob"
	case "fetch", "web fetch", "webfetch":
		return "web_fetch"
	case "web search", "websearch":
		return "web_search"
	case "todo", "todo write", "todo list", "todo_list":
		return "todo_write"
	}

	return strings.ReplaceAll(lower, " ", "_")
}
