package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
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
// `initialize`, `session/new`, `session/load`, `session/resume`,
// `session/set_model`, `session/prompt`, the `session/update` notification
// family, `session/request_permission`, and `mcpServers`. It advertises
// `loadSession` and returns its model catalog from `session/new`, so the
// existing Hermes/Kimi/Kiro/Qoder/Traecli ACP client (hermesClient) drives it
// with only provider-specific launch args and tool-name normalization.
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
	devinArgs = append(devinArgs, filterCustomArgs(opts.CustomArgs, devinBlockedArgs, b.cfg.Logger)...)
	cmd := b.cfg.commandAt(execPath).exec(runCtx, devinArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(devinArgs, trustAgentCommandPositional(0, "acp")))
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
		effectiveModel := strings.TrimSpace(opts.Model)

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

		// Drop MCP entries whose remote transport the runtime didn't advertise
		// in its initialize response. See hermes.go for why sending an
		// unsupported transport tanks the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "devin", b.cfg)

		// When initialize advertises authMethods, the ACP host must send
		// authenticate before session/new. Desktop-spawned `devin acp` ignores
		// local `devin auth` and waits for this. Standalone often advertises
		// nothing (empty = skip).
		if methodID, authErr := selectDevinAuthMethod(extractACPAuthMethods(initResult), envHasNonEmpty(cmd.Env, "WINDSURF_API_KEY")); authErr != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("devin authentication setup failed: %v", authErr)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		} else if methodID != "" {
			authParams := map[string]any{"methodId": methodID}
			if methodID == devinAuthMethodWindsurfAPIKey {
				authParams["_meta"] = map[string]any{"headless": true}
			}
			if _, err := c.request(runCtx, "authenticate", authParams); err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("devin authenticate (%s) failed: %v — run `devin auth login` or set WINDSURF_API_KEY", methodID, err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			b.cfg.Logger.Info("devin authenticated", "method", methodID)
		}

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			// Devin advertises loadSession; resume uses ACP session/load.
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
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "devin",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
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
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("devin session created", "session_id", sessionID)
		// Surface the session id on the message bus immediately so the daemon
		// can PinTaskSession mid-flight. Without this, a daemon restart during
		// a long Devin turn loses the resume pointer.
		trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

		// Model is set at launch via `devin acp --model`. Live CLI 3000.5.20
		// accepts that flag on the acp subcommand. Do not use OMP's
		// session/set_config_option.

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
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					// See the hermes backend: the runtime may echo the
					// requested id back from session/load even when the
					// session is gone, so the stale id only fails here, at
					// prompt time. Empty SessionID lets the daemon's
					// resume-failure fallback retry fresh and store the
					// replacement id.
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
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens
				c.usage.CacheWriteTokens += pr.usage.CacheWriteTokens
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

		// Promote completed→failed when stderr or the agent text stream show a
		// terminal upstream-LLM failure (HTTP 4xx / rate-limit / expired
		// token). Mirrors hermes/kimi/kiro/qoder/traecli.
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

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

func (b *devinBackend) applyBuiltinRuntimeOverrides(desc BuiltinRuntime) {
	if desc.DefaultExecutable != "" && b.cfg.ExecutablePath == "" {
		b.cfg.ExecutablePath = desc.DefaultExecutable
	}
}

const (
	devinAuthMethodWindsurfAPIKey = "windsurf-api-key"
)

// selectDevinAuthMethod picks an ACP authenticate method advertised by
// initialize. Empty methods means skip (standalone `devin acp` often uses
// local `devin auth` without an explicit handshake).
func selectDevinAuthMethod(methods []string, haveWindsurfKey bool) (string, error) {
	if len(methods) == 0 {
		return "", nil
	}
	offered := make(map[string]bool, len(methods))
	for _, m := range methods {
		if m = strings.TrimSpace(m); m != "" {
			offered[m] = true
		}
	}
	if haveWindsurfKey && offered[devinAuthMethodWindsurfAPIKey] {
		return devinAuthMethodWindsurfAPIKey, nil
	}
	for _, candidate := range []string{"devin-cli", "cached-login", "local", "login"} {
		if offered[candidate] {
			return candidate, nil
		}
	}
	// Standalone `devin acp` advertises interactive browser login. Multica
	// cannot complete that from a daemon; skip the handshake and rely on
	// stored `devin auth` / WINDSURF_API_KEY. If both browser and
	// windsurf-api-key are advertised and no key is set, still skip — do
	// not treat it as "windsurf-only".
	if offered["devin-browser"] {
		return "", nil
	}
	if offered[devinAuthMethodWindsurfAPIKey] {
		return "", fmt.Errorf("devin acp advertised only windsurf-api-key; set WINDSURF_API_KEY or run `devin auth login`")
	}
	advertised := make([]string, 0, len(offered))
	for method := range offered {
		advertised = append(advertised, method)
	}
	sort.Strings(advertised)
	return "", fmt.Errorf("devin acp advertised unsupported auth methods %q", advertised)
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
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := runtimeCmd.exec(runCtx, "models", "list", "--format", "json")
	hideAgentWindow(cmd)
	stdout, err := cmd.Output()
	if err != nil || len(stdout) == 0 {
		return []Model{}, nil
	}
	return parseDevinModels(stdout)
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
