package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// devinBlockedArgs are flags hardcoded by the daemon that must not be overridden
// by user-configured custom_args. `acp` is the protocol subcommand. Devin ACP
// does not take a root `--permission-mode`; auto-approve via ACP
// session/request_permission. `-p`/`--print` would leave ACP stdio.
var devinBlockedArgs = map[string]blockedArgMode{
	"acp":               blockedStandalone,
	"--yolo":            blockedStandalone,
	"--permission-mode": blockedWithValue,
	"--auto-approve":    blockedStandalone,
	"--approval-mode":   blockedWithValue,
	"-p":                blockedStandalone,
	"--print":           blockedStandalone,
	"--mode":            blockedWithValue,
	"--output-format":   blockedWithValue,
	"--model":           blockedWithValue,
}

// devinBackend implements Backend by spawning `devin acp` and communicating
// via the standard ACP (Agent Client Protocol) JSON-RPC 2.0 transport over
// stdin/stdout.
//
// Devin CLI is host-local ACP (`devin acp`) — not cloud Devin VMs / Playbooks /
// Secrets. It does not take a root `--permission-mode`; auto-approve via ACP
// session/request_permission. Launched via `devin acp`, it implements
// `initialize`, `session/new`, `session/load`, `session/set_config_option`,
// and `session/prompt`. Model selection is confirmed against the session's
// configOptions, because --model may silently fall back to the CLI default.
type devinBackend struct {
	cfg Config
}

// Match the shared ACP backends' bound for trailing notifications and pipes.
var devinReaderDrainGrace = 2 * time.Second

func (b *devinBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "devin"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("devin executable not found at %q: %w", execPath, err)
	}
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("devin working directory: %w", err)
	}

	// Translate the agent's mcp_config (Claude-style object of objects) into
	// the array shape ACP session/new and session/load expect. Fail closed on
	// malformed JSON so the launch surfaces the real error instead of silently
	// dropping every MCP server.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("devin: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	devinArgs := []string{"acp"}
	if model := strings.TrimSpace(opts.Model); model != "" {
		devinArgs = append(devinArgs, "--model", model)
	}
	devinArgs = append(devinArgs, filterCustomArgs(opts.ExtraArgs, devinBlockedArgs, b.cfg.Logger)...)
	devinArgs = append(devinArgs, filterCustomArgs(opts.CustomArgs, devinBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, devinArgs...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = devinReaderDrainGrace
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(devinArgs, trustAgentCommandPositional(0, "acp")))
	cmd.Dir = cwd
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
	// StderrPipe + an explicit copier give us a join point (`stderrDone`)
	// that fires before the failure-promotion decision; see the matching
	// comment in hermes.go for why the io.MultiWriter form races with
	// stopReason=end_turn under load.
	providerErr := newACPProviderErrorSniffer("devin")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("devin stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
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

	var deliverable acpDeliverableTracker
	var streamingCurrentTurn atomic.Bool
	activity := make(chan struct{}, 1)

	promptDone := make(chan hermesPromptResult, 1)

	c := &hermesClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		onActivity: func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
			if !streamingCurrentTurn.Load() {
				return
			}
			if msg.Type == MessageToolUse {
				msg.Tool = kimiToolNameFromTitle(msg.Tool)
			}
			deliverable.observe(msg)
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
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("devin process exited"))
	}()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = stdin.Close()
			// Cancel before Wait: the child may ignore EOF. Wait closes our pipe
			// ends, releasing readers even if an escaped descendant holds them.
			cancel()
			_ = cmd.Wait()
			<-readerDone
			<-stderrDone
			releaseProcessGroup(cmd)
		})
	}

	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer stop()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		var resumeRejected bool
		var sessionResult json.RawMessage

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus, finalError, _ = classifyACPResumeFailure(runCtx, "devin", "initialize", err, timeout, b.cfg.Logger)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		// Drop MCP entries whose remote transport the runtime didn't advertise
		// in its initialize response. See hermes.go for why sending an
		// unsupported transport tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "devin", b.cfg)

		// ACP authentication is optional: the CLI reads the environment or its
		// stored login. Never guess a method or preempt session/new's auth error.
		if methodID := selectDevinAuthMethod(extractACPAuthMethods(initResult), envHasNonEmpty(cmd.Env, "WINDSURF_API_KEY")); methodID != "" {
			authParams := map[string]any{"methodId": methodID}
			if methodID == devinAuthMethodWindsurfAPIKey {
				authParams["_meta"] = map[string]any{"headless": true}
			}
			if _, err := c.request(runCtx, "authenticate", authParams); err != nil {
				finalStatus, finalError, _ = classifyACPResumeFailure(runCtx, "devin", "authenticate", err, timeout, b.cfg.Logger)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			b.cfg.Logger.Info("devin authenticated", "method", methodID)
		}

		if opts.ResumeSessionID != "" {
			if !devinLoadSessionSupported(initResult) {
				finalStatus, finalError, _ = classifyACPResumeFailure(runCtx, "devin", "session/load", fmt.Errorf("runtime does not support session loading"), timeout, b.cfg.Logger)
				resumeRejected = runCtx.Err() == nil
				resCh <- Result{Status: finalStatus, Error: finalError, ResumeRejected: resumeRejected, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus, finalError, resumeRejected = classifyACPResumeFailure(runCtx, "devin", "session/load", err, timeout, b.cfg.Logger)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), ResumeRejected: resumeRejected}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "devin",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			sessionResult = result
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus, finalError, _ = classifyACPResumeFailure(runCtx, "devin", "session/new", err, timeout, b.cfg.Logger)
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
			sessionResult = result
		}

		c.sessionID = sessionID
		effectiveModel, err := confirmDevinModel(runCtx, c.request, sessionID, opts.Model, sessionResult, func(ctx context.Context) ([]byte, error) {
			return devinModelsOutput(ctx, b.cfg.commandAt(execPath), cmd.Env, cwd)
		})
		if err != nil {
			finalStatus, finalError, _ = classifyACPResumeFailure(runCtx, "devin", "model selection", err, timeout, b.cfg.Logger)
			resumeRejected = runCtx.Err() == nil && opts.ResumeSessionID != "" && devinSessionNotFound(err)
			if setupFailureWithholdsSessionID(opts) || resumeRejected {
				sessionID = ""
			}
			resCh <- Result{Status: finalStatus, Error: finalError, SessionID: sessionID, ResumeRejected: resumeRejected, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		b.cfg.Logger.Info("devin session created", "session_id", sessionID)
		// Pin only after model setup succeeds, so a never-prompted session
		// cannot leave a dead resume pointer behind on the task row.
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
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
				if opts.ResumeSessionID != "" && devinSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "devin",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "devin cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
				if pr.modelID != "" {
					effectiveModel = pr.modelID
				}
			default:
			}
			waitForACPNotificationQuiescence(runCtx, activity, readerDone, acpNotificationQuietTime, devinReaderDrainGrace)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("devin finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		_ = stdin.Close()
		if runCtx.Err() == nil {
			waitForHermesPipeDrain(readerDone, stderrDone, devinReaderDrainGrace)
		}
		stop()
		streamingCurrentTurn.Store(false)
		providerErr.Finalize()
		finalOutput, fullOutput := deliverable.result()

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (HTTP 4xx / rate-limit / expired
		// token). Mirrors hermes/kimi/kiro/qoder/traecli.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, fullOutput, providerErr)

		u := c.accumulatedUsage()

		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			model := effectiveModel
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      sessionID,
			Usage:          usageMap,
			ResumeRejected: resumeRejected,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func devinLoadSessionSupported(raw json.RawMessage) bool {
	var init struct {
		Capabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	return json.Unmarshal(raw, &init) == nil && init.Capabilities.LoadSession
}

func devinSessionNotFound(err error) bool {
	var rpcErr *acpRPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == -32016 {
		return acpSessionNotFoundWording(strings.ToLower(rpcErr.Message + " " + rpcErr.Data))
	}
	return isACPSessionNotFound(err)
}

const (
	devinAuthMethodWindsurfAPIKey = "windsurf-api-key"
)

// Only opt into the advertised API-key handshake when its credential exists.
// Unknown or interactive methods do not disable the CLI's stored credentials.
func selectDevinAuthMethod(methods []string, haveWindsurfKey bool) string {
	if haveWindsurfKey {
		for _, method := range methods {
			if strings.TrimSpace(method) == devinAuthMethodWindsurfAPIKey {
				return devinAuthMethodWindsurfAPIKey
			}
		}
	}
	return ""
}

// discoverDevinModels runs `devin models list --format json`.
// Unknown/empty output degrades to an empty catalog (manual entry).
func discoverDevinModels(ctx context.Context, runtimeCmd Command) ([]Model, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "devin"
	}
	if _, err := exec.LookPath(runtimeCmd.Path); err != nil {
		return []Model{}, nil
	}
	stdout, err := devinModelsOutput(ctx, runtimeCmd, nil, "")
	if err != nil {
		return nil, err
	}
	return parseDevinModels(stdout)
}

func devinModelsOutput(ctx context.Context, runtimeCmd Command, env []string, cwd string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := runtimeCmd.exec(runCtx, "models", "list", "--format", "json")
	cmd.Env, cmd.Dir = env, cwd
	hideAgentWindow(cmd)
	stdout, err := outputOwned(cmd, runtimeCmd.logger)
	if err != nil {
		return nil, fmt.Errorf("devin model discovery: %w", err)
	}
	return stdout, nil
}

func parseDevinModels(data []byte) ([]Model, error) {
	type entry struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Label    string `json:"label"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		ModelUID string `json:"model_uid"`
	}
	var families struct {
		Families []struct {
			Slug     string  `json:"slug"`
			Variants []entry `json:"variants"`
		} `json:"families"`
	}
	if err := json.Unmarshal(data, &families); err == nil && len(families.Families) > 0 {
		out := make([]Model, 0, 64)
		seen := map[string]bool{}
		for _, fam := range families.Families {
			provider := strings.TrimSpace(fam.Slug)
			for _, e := range fam.Variants {
				id := strings.TrimSpace(e.ModelUID)
				if id == "" {
					id = strings.TrimSpace(e.ID)
				}
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				label := strings.TrimSpace(e.Label)
				if label == "" {
					label = strings.TrimSpace(e.Name)
				}
				if label == "" {
					label = id
				}
				out = append(out, Model{ID: id, Label: label, Provider: provider})
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	var models []entry
	var wrapper struct {
		Models []entry `json:"models"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Models) > 0 {
		models = wrapper.Models
	} else if err := json.Unmarshal(data, &models); err != nil {
		return []Model{}, nil
	}
	out := make([]Model, 0, len(models))
	seen := map[string]bool{}
	for _, e := range models {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			id = strings.TrimSpace(e.ModelUID)
		}
		if id == "" {
			id = strings.TrimSpace(e.Model)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(e.Name)
		if label == "" {
			label = strings.TrimSpace(e.Label)
		}
		if label == "" {
			label = id
		}
		out = append(out, Model{ID: id, Label: label, Provider: strings.TrimSpace(e.Provider)})
	}
	return out, nil
}
