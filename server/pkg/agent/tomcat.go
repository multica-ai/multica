package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Longer than the forwarder's bounded 60s cleanup acknowledgement.
const tomcatShutdownGrace = 65 * time.Second

var tomcatBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone, "serve": blockedStandalone,
	"--help": blockedStandalone, "-h": blockedStandalone,
	"--version": blockedStandalone, "-V": blockedStandalone,
}

type tomcatBackend struct{ cfg Config }

func (b *tomcatBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	// The shared service's working directory is not the task's workspace.
	if !filepath.IsAbs(opts.Cwd) {
		return nil, fmt.Errorf("tomcat requires an explicit absolute cwd")
	}
	info, err := os.Stat(opts.Cwd)
	if err != nil {
		return nil, fmt.Errorf("tomcat cwd: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("tomcat cwd is not a directory: %s", opts.Cwd)
	}
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("tomcat: invalid mcp_config: %w", err)
	}
	executable := b.cfg.ExecutablePath
	if executable == "" {
		executable = "tomcat"
	}
	if _, err := exec.LookPath(executable); err != nil {
		return nil, fmt.Errorf("tomcat executable not found at %q: %w", executable, err)
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	args := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, tomcatBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(executable).exec(runCtx, args...)
	hideAgentWindow(cmd)
	cmd.Dir, cmd.Env = opts.Cwd, buildEnv(b.cfg.Env)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "acp")))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	// EOF asks the forwarder to stop its task before exiting. Do not let
	// CommandContext kill it before that acknowledgement has arrived.
	cmd.Cancel = func() error { _ = stdin.Close(); return nil }
	cmd.WaitDelay = tomcatShutdownGrace + time.Second
	stderr := newStderrTail(newLogWriter(b.cfg.Logger, "[tomcat:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderr
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		return nil, fmt.Errorf("start tomcat: %w", err)
	}

	messages, results := make(chan Message, 256), make(chan Result, 1)
	var streaming, terminal atomic.Bool
	var deliverable acpDeliverableTracker
	completion := make(chan hermesPromptResult, 1)
	c := &hermesClient{
		cfg: b.cfg, stdin: stdin, pending: make(map[int]*pendingRPC), pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool { return streaming.Load() },
		onMessage: func(message Message) {
			if streaming.Load() {
				deliverable.observe(message)
				trySend(messages, message)
			}
		},
		onPromptDone: func(result hermesPromptResult) {
			completion <- result
			switch result.stopReason {
			case "end_turn", "cancelled", "max_tokens", "max_turn_requests", "refusal":
				terminal.Store(true)
			}
		},
	}
	processDone := make(chan struct{})
	var exitErr, scanErr error
	var sawStdout bool
	go func() {
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			sawStdout = true
			c.handleLine(scanner.Text())
		}
		scanErr = scanner.Err()
		c.closeAllPending(io.EOF)
		// Join the reader before Wait closes its pipe; no late send can outlive
		// processDone or race the message channel's close below.
		exitErr = cmd.Wait()
		close(processDone)
	}()
	go func() {
		defer cancel()
		defer close(results)
		started := time.Now()
		result, model := b.runTomcatTurn(runCtx, c, prompt, opts, mcpServers, messages, &streaming)
		_ = stdin.Close()
		cleanupError := ""
		timer := time.NewTimer(tomcatShutdownGrace)
		select {
		case <-processDone:
		case <-timer.C:
			signalProcessGroup(cmd, syscall.SIGKILL)
			_ = stdout.Close()
			<-processDone
			cleanupError = "tomcat shutdown timed out; shared-service cleanup was not acknowledged"
		}
		timer.Stop()
		releaseProcessGroup(cmd)
		// CommandContext can report cancellation during EOF cleanup even when
		// the peer exits successfully. Do not rewrite an already completed turn.
		lateCancel := cmd.ProcessState != nil && cmd.ProcessState.Success() &&
			(errors.Is(exitErr, context.Canceled) || errors.Is(exitErr, context.DeadlineExceeded))
		// The reader is joined: a provider response already being decoded when
		// request selected ctx.Done must still outrank that local cancellation.
		select {
		case final := <-completion:
			c.mergeUsage(final.usage)
			if final.modelID != "" {
				model = final.modelID
			}
			result.Error = ""
			switch final.stopReason {
			case "end_turn":
				result.Status = "completed"
			case "cancelled":
				result.Status, result.Error = "aborted", "tomcat cancelled the prompt"
			default:
				result.Status, result.Error = "failed", fmt.Sprintf("tomcat stopped without completing the turn: %q", final.stopReason)
			}
		default:
		}
		if (exitErr != nil && !lateCancel) || scanErr != nil {
			var exit *exec.ExitError
			// Native Tomcat exits 2 only for argument parsing, before connecting
			// to the shared service. Custom profile commands have no such contract.
			if b.cfg.BuiltinRuntime && cleanupError == "" && !sawStdout && scanErr == nil &&
				errors.As(exitErr, &exit) && exit.ExitCode() == 2 {
				result.Error = strings.TrimSpace(result.Error + " " + fmt.Sprintf(
					"tomcat at %s rejected its arguments (%v); compare `%s acp --help` with the configured arguments",
					cmd.Path, exitErr, cmd.Path))
			} else {
				cleanupError = strings.TrimSpace(cleanupError + " " + fmt.Sprintf("tomcat transport shutdown failed (process: %v, stream: %v)", exitErr, scanErr))
			}
		}
		if cleanupError != "" {
			result.Status = "cleanup_unconfirmed"
			result.Error = strings.TrimSpace(result.Error + " " + cleanupError)
		}
		if result.Error != "" {
			result.Error = withAgentStderr(result.Error, "tomcat", stderr.Tail())
		}
		result.Output, _ = deliverable.result()
		if usage := c.accumulatedUsage(); acpUsagePresent(usage) {
			if model == "" {
				model = "unknown"
			}
			result.Usage = map[string]TokenUsage{model: usage}
		}
		result.DurationMs = time.Since(started).Milliseconds()
		close(messages)
		// Receiving Result authorizes the daemon's existing worktree cleanup.
		// It must therefore happen after the forwarder has actually exited.
		results <- result
	}()
	return &Session{Messages: messages, Result: results, TerminalObserved: terminal.Load, CleanupRequired: true}, nil
}

func (b *tomcatBackend) runTomcatTurn(ctx context.Context, c *hermesClient, prompt string, opts ExecOptions, mcpServers []any, messages chan<- Message, streaming *atomic.Bool) (Result, string) {
	result := Result{Status: "failed", SessionID: opts.ResumeSessionID}
	fail := func(method string, err error) Result {
		switch ctx.Err() {
		case context.Canceled:
			result.Status, result.Error = "aborted", "execution cancelled"
		case context.DeadlineExceeded:
			result.Status, result.Error = "timeout", "tomcat execution timed out"
		default:
			result.Error = fmt.Sprintf("tomcat %s failed: %v", method, err)
		}
		return result
	}
	initialized, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": 1, "clientInfo": map[string]any{"name": "multica-agent-sdk", "version": "0.2.0"}, "clientCapabilities": map[string]any{},
	})
	if err != nil {
		return fail("initialize", err), ""
	}
	var handshake struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if json.Unmarshal(initialized, &handshake) != nil || handshake.ProtocolVersion != 1 {
		return fail("initialize", fmt.Errorf("expected ACP protocol version 1")), ""
	}
	mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initialized), "tomcat", b.cfg)
	params := map[string]any{"cwd": opts.Cwd, "mcpServers": mcpServers}
	method := "session/new"
	if opts.ResumeSessionID != "" {
		method = "session/load"
		params["sessionId"] = opts.ResumeSessionID
	}
	session, err := c.request(ctx, method, params)
	if err != nil {
		if opts.ResumeSessionID != "" {
			result.Status, result.Error, result.ResumeRejected = classifyACPResumeFailure(ctx, "tomcat", method, err, opts.Timeout, b.cfg.Logger)
			var rpc *acpRPCError
			if ctx.Err() == nil && errors.As(err, &rpc) && rpc.Code == -32603 && rpc.Message == "unknown_session" {
				result.ResumeRejected = true
			}
			if result.ResumeRejected {
				result.SessionID = ""
			}
			return result, ""
		}
		return fail(method, err), ""
	}
	if opts.ResumeSessionID == "" {
		result.SessionID = extractACPSessionID(session)
	}
	if result.SessionID == "" {
		return fail(method, fmt.Errorf("missing session ID")), ""
	}
	c.sessionID = result.SessionID
	model := tomcatCurrentModel(session)
	if opts.Model != "" {
		configured, err := c.request(ctx, "session/set_config_option", map[string]any{"sessionId": result.SessionID, "configId": "model", "value": opts.Model})
		if err != nil {
			if setupFailureWithholdsSessionID(opts) {
				result.SessionID = ""
			}
			return fail("session/set_config_option", err), model
		}
		model = tomcatCurrentModel(configured)
		session = configured
	}
	if err := setTomcatEffort(ctx, c.request, result.SessionID, opts.ThinkingLevel, session); err != nil {
		if setupFailureWithholdsSessionID(opts) {
			result.SessionID = ""
		}
		return fail("session/set_config_option effort", err), model
	}
	trySend(messages, Message{Type: MessageStatus, Status: "running", SessionID: result.SessionID})
	streaming.Store(true)
	// Runtime instructions and skills are already in AGENTS.md / .agents/skills.
	// Repeating SystemPrompt here would persist them as another user message.
	_, err = c.request(ctx, "session/prompt", map[string]any{"sessionId": result.SessionID, "prompt": []map[string]any{{"type": "text", "text": prompt}}})
	if err != nil {
		return fail("session/prompt", err), model
	}
	result.Error = "tomcat returned no valid prompt result"
	return result, model
}

// Tomcat setup is strict: a requested effort must be applied before any prompt.
func setTomcatEffort(ctx context.Context, request acpRequestFn, sessionID, level string, configured json.RawMessage) error {
	option, ok := parseACPEffortOption(configured)
	if !ok {
		if level == "" {
			return nil
		}
		return errors.New("running Tomcat does not provide an effort option; update and restart Tomcat, or select Default")
	}
	if level == "" {
		level = "default"
	}
	if !option.supports(level) {
		return fmt.Errorf("Tomcat session does not support effort %q", level)
	}
	reply, err := request(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID, "configId": option.ConfigID, "value": level,
	})
	if err != nil {
		return err
	}
	if current, ok := acpConfigOptionCurrentValue(reply, option.ConfigID); !ok || current != level {
		return fmt.Errorf("Tomcat did not confirm effort %q", level)
	}
	return nil
}

func tomcatCurrentModel(raw json.RawMessage) string {
	var response struct {
		ConfigOptions []struct {
			ID           string `json:"id"`
			CurrentValue string `json:"currentValue"`
		} `json:"configOptions"`
	}
	if json.Unmarshal(raw, &response) == nil {
		for _, option := range response.ConfigOptions {
			if option.ID == "model" {
				return option.CurrentValue
			}
		}
	}
	return ""
}
