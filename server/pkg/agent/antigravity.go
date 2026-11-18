package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// antigravityBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args.
var antigravityBlockedArgs = map[string]blockedArgMode{
	"-p":                             blockedWithValue,
	"--print":                        blockedWithValue,
	"--prompt":                       blockedWithValue,
	"-i":                             blockedStandalone, // interactive mode requires a TTY and cannot run under the daemon
	"--prompt-interactive":           blockedStandalone,
	"-c":                             blockedStandalone, // resume via --conversation, not --continue
	"--continue":                     blockedStandalone,
	"--conversation":                 blockedWithValue, // managed via ExecOptions.ResumeSessionID
	"--model":                        blockedWithValue, // managed via ExecOptions.Model / agent.model
	"--output-format":                blockedWithValue, // stream-json is required for token accounting
	"--print-timeout":                blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone, // always-on in daemon mode
	"--log-file":                     blockedWithValue,  // daemon needs it for session capture
	"--settings":                     blockedWithValue,  // Claude Code-only flag; agy rejects it
	"--notices":                      blockedStandalone,
	"--help":                         blockedStandalone,
	"-h":                             blockedStandalone,
	"--helpfull":                     blockedStandalone,
}

// antigravityACPLaunchArgs returns OS-specific launch flags matching the
// official ACP registry distribution for Google Antigravity.
func antigravityACPLaunchArgs() []string {
	if runtime.GOOS == "linux" {
		return []string{"--uid="}
	}
	return nil
}

// antigravityDefaultExecutable is the standard binary name for the Google
// Antigravity ACP server.
func antigravityDefaultExecutable() string {
	return "agy_acp_server"
}

// antigravityResolveExecutable finds the installed ACP server executable or
// falls back to the default command name.
func antigravityResolveExecutable() string {
	for _, candidate := range []string{"agy_acp_server", "agy_acp_server.par", "antigravity-acp", "agy"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return antigravityDefaultExecutable()
}

// isAntigravityACPExecutable reports whether the resolved executable represents
// an ACP JSON-RPC server binary rather than the legacy agy CLI.
func isAntigravityACPExecutable(execPath string) bool {
	base := filepath.Base(execPath)
	return strings.Contains(base, "acp") || strings.HasSuffix(base, ".par")
}

// ensureAntigravityHarnessEnv checks if ANTIGRAVITY_HARNESS_PATH is set; if not,
// it checks whether localharness_external or localharness exists alongside the
// resolved executable and injects ANTIGRAVITY_HARNESS_PATH so session/new
// succeeds out-of-the-box.
func ensureAntigravityHarnessEnv(env map[string]string, execPath string) map[string]string {
	if env != nil && env["ANTIGRAVITY_HARNESS_PATH"] != "" {
		return env
	}
	if resolved, err := exec.LookPath(execPath); err == nil {
		dir := filepath.Dir(resolved)
		for _, name := range []string{"localharness_external", "localharness"} {
			h := filepath.Join(dir, name)
			if info, err := os.Stat(h); err == nil && !info.IsDir() {
				out := make(map[string]string, len(env)+1)
				for k, v := range env {
					out[k] = v
				}
				out["ANTIGRAVITY_HARNESS_PATH"] = h
				return out
			}
		}
	}
	return env
}

// antigravityToolNameFromTitle extracts a canonical tool name from an ACP
// tool call title. Antigravity ACP emits titles like "Running view_file"
// or "Running run_command".
func antigravityToolNameFromTitle(title, kind string) string {
	t := strings.TrimSpace(title)
	if strings.HasPrefix(t, "Running ") {
		return strings.TrimSpace(strings.TrimPrefix(t, "Running "))
	}
	return hermesToolNameFromTitle(title, kind)
}

// antigravityReaderDrainGrace bounds how long the turn waits for trailing ACP
// notifications after the session/prompt response.
var antigravityReaderDrainGrace = 2 * time.Second

// antigravityBackend implements Backend by spawning either the Google Antigravity
// ACP server (agy_acp_server / agy_acp_server.par) or the legacy agy CLI runner.
type antigravityBackend struct {
	cfg Config
}

func (b *antigravityBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = antigravityResolveExecutable()
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("antigravity executable not found at %q: %w", execPath, err)
	}

	if isAntigravityACPExecutable(execPath) {
		return b.executeACP(ctx, prompt, opts, execPath)
	}
	return b.executeCLI(ctx, prompt, opts, execPath)
}

// antigravityTurnState holds the ephemeral channel and state for an in-flight turn
// executing on a persistent or single-turn ACP connection.
type antigravityTurnState struct {
	msgCh       chan Message
	promptDone  chan hermesPromptResult
	activity    chan struct{}
	streaming   atomic.Bool
	deliverable acpDeliverableTracker
}

// antigravitySessionConn represents a live, persistent ACP connection to an
// agy_acp_server process.
type antigravitySessionConn struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	client      *hermesClient
	execPath    string
	cwd         string
	sessionID   string
	model       string
	modeSet     bool
	providerErr *acpProviderErrorSniffer
	cancelProc  context.CancelFunc
	readerDone  chan struct{}
	stderrDone  chan struct{}

	mu         sync.Mutex
	turnState  atomic.Pointer[antigravityTurnState]
	lastActive time.Time
	closed     atomic.Bool
	closeOnce  sync.Once
}

func (c *antigravitySessionConn) alive() bool {
	if c.closed.Load() || c.cmd == nil || c.cmd.Process == nil || c.cmd.ProcessState != nil {
		return false
	}
	select {
	case <-c.readerDone:
		return false
	default:
		return true
	}
}

func (c *antigravitySessionConn) close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.cancelProc != nil {
			c.cancelProc()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Wait()
			releaseProcessGroup(c.cmd)
		}
	})
}

// antigravitySessionPool manages warm ACP connections keyed by session ID so that
// follow-up turns on the same session avoid the multi-second startup latency of
// agy_acp_server.par (Python zipapp unpacking, C++ runtime, gRPC connect, OAuth refresh).
type antigravitySessionPool struct {
	mu       sync.Mutex
	sessions map[string]*antigravitySessionConn
}

var globalAntigravityPool = &antigravitySessionPool{
	sessions: make(map[string]*antigravitySessionConn),
}

const antigravitySessionIdleTimeout = 5 * time.Minute

func (p *antigravitySessionPool) get(sessionID, cwd, execPath string) *antigravitySessionConn {
	if sessionID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	conn, ok := p.sessions[sessionID]
	if !ok {
		return nil
	}
	if !conn.alive() || conn.cwd != cwd || conn.execPath != execPath {
		delete(p.sessions, sessionID)
		go conn.close()
		return nil
	}
	return conn
}

func (p *antigravitySessionPool) put(sessionID string, conn *antigravitySessionConn) {
	if sessionID == "" || conn == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	conn.lastActive = time.Now()
	p.sessions[sessionID] = conn
	p.reapLocked(time.Now())
}

func (p *antigravitySessionPool) remove(sessionID string) {
	if sessionID == "" {
		return
	}
	p.mu.Lock()
	conn, ok := p.sessions[sessionID]
	if ok {
		delete(p.sessions, sessionID)
	}
	p.mu.Unlock()
	if conn != nil {
		go conn.close()
	}
}

func (p *antigravitySessionPool) reapLocked(now time.Time) {
	for id, conn := range p.sessions {
		if !conn.alive() || now.Sub(conn.lastActive) > antigravitySessionIdleTimeout {
			delete(p.sessions, id)
			go conn.close()
		}
	}
}

func clearAntigravitySessionPool() {
	globalAntigravityPool.mu.Lock()
	all := globalAntigravityPool.sessions
	globalAntigravityPool.sessions = make(map[string]*antigravitySessionConn)
	globalAntigravityPool.mu.Unlock()
	for _, conn := range all {
		conn.close()
	}
}

func antigravitySessionReuseEnabled() bool {
	return os.Getenv("MULTICA_ANTIGRAVITY_DISABLE_SESSION_REUSE") != "1"
}

func (b *antigravityBackend) executeACP(ctx context.Context, prompt string, opts ExecOptions, execPath string) (*Session, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}

	if opts.ResumeSessionID != "" && antigravitySessionReuseEnabled() {
		if conn := globalAntigravityPool.get(opts.ResumeSessionID, cwd, execPath); conn != nil {
			sess, ok := b.tryExecuteACPOnWarmConn(ctx, prompt, opts, conn)
			if ok {
				return sess, nil
			}
		}
	}

	return b.executeACPNewConn(ctx, prompt, opts, execPath)
}

func (b *antigravityBackend) tryExecuteACPOnWarmConn(ctx context.Context, prompt string, opts ExecOptions, conn *antigravitySessionConn) (*Session, bool) {
	conn.mu.Lock()
	if !conn.alive() {
		conn.mu.Unlock()
		globalAntigravityPool.remove(opts.ResumeSessionID)
		return nil, false
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	ts := &antigravityTurnState{
		msgCh:      msgCh,
		promptDone: make(chan hermesPromptResult, 1),
		activity:   make(chan struct{}, 1),
	}
	conn.turnState.Store(ts)

	conn.client.readOnly = opts.ReadOnly
	if opts.OnUsage != nil {
		conn.client.onUsage = opts.OnUsage
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer conn.mu.Unlock()
		defer conn.turnState.Store(nil)

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var resumeRejected bool
		sessionID := conn.sessionID
		effectiveModel := conn.model

		// 1. Ensure mode is yolo if not read-only
		if !opts.ReadOnly && !conn.modeSet {
			if _, err := conn.client.request(runCtx, "session/set_mode", map[string]any{
				"sessionId": sessionID,
				"modeId":    "yolo",
			}); err == nil {
				conn.modeSet = true
			}
		}

		// 2. Set model if requested and different from current
		if opts.Model != "" && opts.Model != conn.model {
			if _, err := conn.client.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("antigravity set_session_model on warm session failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity could not switch to model %q: %v", opts.Model, err)
				if isACPSessionNotFound(err) {
					sessionID = ""
					resumeRejected = true
				}
				conn.close()
				globalAntigravityPool.remove(conn.sessionID)
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					SessionID:      sessionID,
					ResumeRejected: resumeRejected,
				}
				return
			}
			effectiveModel = opts.Model
			conn.model = opts.Model
		}

		// 3. Send prompt
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}
		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}

		ts.streaming.Store(true)
		_, err := conn.client.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    promptBlocks,
		})

		var promptSucceeded bool
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("antigravity timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity session/prompt failed: %v", err)
				if isACPSessionNotFound(err) {
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-ts.promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "antigravity cancelled the prompt"
				}
				conn.client.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, ts.activity, conn.readerDone, acpNotificationQuietTime, antigravityReaderDrainGrace)
			promptSucceeded = (finalStatus == "completed")
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("antigravity warm session turn finished", "pid", conn.cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		finalOutput, providerErrorOutput := ts.deliverable.result()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, conn.providerErr)

		u := conn.client.accumulatedUsage()
		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			model := effectiveModel
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		if promptSucceeded && conn.alive() && antigravitySessionReuseEnabled() {
			conn.lastActive = time.Now()
		} else {
			conn.close()
			globalAntigravityPool.remove(conn.sessionID)
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

	return &Session{Messages: msgCh, Result: resCh}, true
}

func (b *antigravityBackend) executeACPNewConn(ctx context.Context, prompt string, opts ExecOptions, execPath string) (*Session, error) {
	// Translate the agent's mcp_config (Claude-style object of objects)
	// into the array shape ACP session/new and session/resume expect.
	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("antigravity: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	antigravityArgs := append(antigravityACPLaunchArgs(), filterCustomArgs(opts.CustomArgs, antigravityBlockedArgs, b.cfg.Logger)...)

	procCtx, cancelProc := context.WithCancel(context.Background())
	cmd := b.cfg.commandAt(execPath).exec(procCtx, antigravityArgs...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(antigravityArgs))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childEnv := ensureAntigravityHarnessEnv(b.cfg.Env, execPath)
	cmd.Env = buildEnv(childEnv)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cancelProc()
		return nil, fmt.Errorf("antigravity stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		cancelProc()
		return nil, fmt.Errorf("antigravity stdin pipe: %w", err)
	}

	providerErr := newACPProviderErrorSniffer("antigravity")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		cancelProc()
		return nil, fmt.Errorf("antigravity stderr pipe: %w", err)
	}

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		cancelProc()
		return nil, fmt.Errorf("start antigravity: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[antigravity:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("antigravity acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}

	conn := &antigravitySessionConn{
		cmd:         cmd,
		stdin:       stdin,
		execPath:    execPath,
		cwd:         cwd,
		providerErr: providerErr,
		cancelProc:  cancelProc,
		readerDone:  make(chan struct{}),
		stderrDone:  stderrDone,
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	ts := &antigravityTurnState{
		msgCh:      msgCh,
		promptDone: make(chan hermesPromptResult, 1),
		activity:   make(chan struct{}, 1),
	}
	conn.turnState.Store(ts)

	c := &hermesClient{
		cfg:                        b.cfg,
		readOnly:                   opts.ReadOnly,
		stdin:                      stdin,
		pending:                    make(map[int]*pendingRPC),
		pendingTools:               make(map[string]*pendingToolCall),
		toolStartCarriesFinalInput: true,
		onUsage:                    opts.OnUsage,
		acceptNotification: func(string) bool {
			curr := conn.turnState.Load()
			return curr != nil && curr.streaming.Load()
		},
		onActivity: func() {
			curr := conn.turnState.Load()
			if curr != nil && curr.streaming.Load() {
				select {
				case curr.activity <- struct{}{}:
				default:
				}
			}
		},
		onMessage: func(msg Message) {
			curr := conn.turnState.Load()
			if curr == nil || !curr.streaming.Load() {
				return
			}
			if msg.Type == MessageToolUse {
				msg.Tool = antigravityToolNameFromTitle(msg.Tool, "")
			}
			curr.deliverable.observe(msg)
			trySend(curr.msgCh, msg)
		},
		onPromptDone: func(result hermesPromptResult) {
			curr := conn.turnState.Load()
			if curr == nil || !curr.streaming.Load() {
				return
			}
			select {
			case curr.promptDone <- result:
			default:
			}
		},
	}
	conn.client = c

	go func() {
		defer close(conn.readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("antigravity process exited"))
	}()

	conn.mu.Lock()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer conn.mu.Unlock()
		defer conn.turnState.Store(nil)

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string
		var resumeRejected bool
		effectiveModel := strings.TrimSpace(opts.Model)

		// 1. Initialize handshake
		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			conn.close()
			resCh <- Result{Status: "failed", Error: fmt.Sprintf("antigravity initialize failed: %v", err), DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "antigravity", b.cfg)

		// 2. Create or resume session
		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				conn.close()
				finalStatus, finalError, resumeRejected = classifyACPResumeFailure(
					runCtx, "antigravity", "session/resume", err, timeout, b.cfg.Logger)
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					ResumeRejected: resumeRejected,
				}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "antigravity",
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
				conn.close()
				resCh <- Result{Status: "failed", Error: fmt.Sprintf("antigravity session/new failed: %v", err), DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				conn.close()
				resCh <- Result{Status: "failed", Error: "antigravity session/new returned no session ID", DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID
		conn.sessionID = sessionID
		conn.model = effectiveModel
		b.cfg.Logger.Info("antigravity session created", "session_id", sessionID)

		// 3. Set mode to "yolo" to auto-approve tool permission requests (unless read-only)
		if !opts.ReadOnly {
			if _, err := c.request(runCtx, "session/set_mode", map[string]any{
				"sessionId": sessionID,
				"modeId":    "yolo",
			}); err == nil {
				conn.modeSet = true
			} else {
				b.cfg.Logger.Debug("antigravity set_mode yolo failed or not supported", "error", err)
			}
		}

		// 4. Set model if specified
		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("antigravity set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity could not switch to model %q: %v", opts.Model, err)
				if setupFailureWithholdsSessionID(opts) {
					sessionID = ""
				} else if isACPSessionNotFound(err) {
					sessionID = ""
					resumeRejected = true
				}
				conn.close()
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					SessionID:      sessionID,
					ResumeRejected: resumeRejected,
				}
				return
			}
			effectiveModel = opts.Model
			conn.model = opts.Model
			b.cfg.Logger.Info("antigravity session model set", "model", opts.Model)
		}

		// 5. Send prompt
		userText := prompt
		if opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}
		promptBlocks := []map[string]any{
			{"type": "text", "text": userText},
		}

		ts.streaming.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    promptBlocks,
		})
		var promptSucceeded bool
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("antigravity timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("antigravity session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "antigravity",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-ts.promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "antigravity cancelled the prompt"
				}
				c.mergeUsage(pr.usage)
			default:
			}
			waitForACPNotificationQuiescence(runCtx, ts.activity, conn.readerDone, acpNotificationQuietTime, antigravityReaderDrainGrace)
			promptSucceeded = (finalStatus == "completed")
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("antigravity finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		finalOutput, providerErrorOutput := ts.deliverable.result()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, providerErrorOutput, providerErr)

		u := c.accumulatedUsage()
		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			model := effectiveModel
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		if promptSucceeded && conn.alive() && antigravitySessionReuseEnabled() && sessionID != "" {
			conn.lastActive = time.Now()
			globalAntigravityPool.put(sessionID, conn)
		} else {
			conn.close()
			globalAntigravityPool.remove(sessionID)
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

// ── Legacy CLI runner (agy -p) ──

type antigravityStreamUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ThinkingTokens   int64 `json:"thinking_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type antigravityStreamStepUpdate struct {
	ConversationID string                  `json:"conversation_id"`
	StepIndex      *int                    `json:"step_index"`
	State          string                  `json:"state"`
	StepType       string                  `json:"step_type"`
	TextDelta      string                  `json:"text_delta"`
	Usage          *antigravityStreamUsage `json:"usage"`
	ToolName       string                  `json:"tool_name"`
	ToolInfo       *antigravityStreamTool  `json:"tool_info"`
}

type antigravityStreamTool struct {
	Name       string          `json:"name"`
	Parameters map[string]any  `json:"parameters"`
	Output     json.RawMessage `json:"output"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type antigravityToolState struct {
	name string
	done bool
}

// Normalize the shared transcript input without duplicating commands or file
// bodies. Preserve collisions and unknown fields, and do not mutate the snapshot.
func antigravityToolInput(parameters map[string]any) map[string]any {
	input := maps.Clone(parameters)
	for _, alias := range []struct {
		from, to   string
		allowEmpty bool
	}{
		{"CommandLine", "command", false},
		{"AbsolutePath", "file_path", false},
		{"TargetFile", "file_path", false},
		{"CodeContent", "content", true},
		{"TargetContent", "old_string", true},
		{"ReplacementContent", "new_string", true},
	} {
		if _, exists := input[alias.to]; exists {
			continue
		}
		if value, ok := parameters[alias.from].(string); ok && (value != "" || alias.allowEmpty) {
			input[alias.to] = value
			delete(input, alias.from)
		}
	}
	return input
}

// Each step has one tool lifecycle, even when agy repeats state snapshots or
// only emits DONE. The existing daemon uploader handles these normal messages;
// tool output must never be appended to the assistant's final answer.
func antigravityToolMessages(step *antigravityStreamStepUpdate, states map[int]antigravityToolState) []Message {
	if step.StepType != "tool" || step.StepIndex == nil || *step.StepIndex < 0 {
		return nil
	}
	active := strings.EqualFold(step.State, "active")
	done := strings.EqualFold(step.State, "done")
	if !active && !done {
		return nil
	}
	index := *step.StepIndex
	state := states[index]
	if state.done {
		return nil
	}
	callID := fmt.Sprintf("agy-step-%d", index)
	var messages []Message
	if state.name == "" {
		name := step.ToolName
		var input map[string]any
		if step.ToolInfo != nil {
			if name == "" {
				name = step.ToolInfo.Name
			}
			input = antigravityToolInput(step.ToolInfo.Parameters)
		}
		if name == "" {
			return nil // A later snapshot may provide the missing metadata.
		}
		state.name = name
		messages = append(messages, Message{Type: MessageToolUse, Tool: name, CallID: callID, Input: input})
	}
	if done {
		var output string
		if info := step.ToolInfo; info != nil {
			if len(info.Output) > 0 && string(info.Output) != "null" {
				if err := json.Unmarshal(info.Output, &output); err != nil {
					output = string(info.Output)
				}
			}
			if info.Error != nil {
				detail := info.Error.Message
				if info.Error.Type != "" {
					if detail != "" {
						detail = info.Error.Type + ": " + detail
					} else {
						detail = info.Error.Type
					}
				}
				// The daemon uploads a bounded prefix of tool output. Put the
				// error first so verbose stdout cannot hide the failure detail.
				if output != "" {
					output = "\n" + output
				}
				output = "Tool error: " + detail + output
			}
		}
		messages = append(messages, Message{Type: MessageToolResult, Tool: state.name, CallID: callID, Output: output})
		state.done = true
	}
	states[index] = state
	return messages
}

type antigravityStreamResult struct {
	ConversationID string                  `json:"conversation_id"`
	Status         string                  `json:"status"`
	Response       string                  `json:"response"`
	Error          string                  `json:"error"`
	Usage          *antigravityStreamUsage `json:"usage"`
}

type antigravityStreamEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	Init           struct {
		Model string `json:"model"`
	} `json:"init"`
	StepUpdate *antigravityStreamStepUpdate `json:"step_update"`
	Result     *antigravityStreamResult     `json:"result"`
}

// antigravityNetworkIssueError is the provider sentence some agy releases
// report when a trailing round trip fails. It carries no Go error text, so it
// has to stay a literal match alongside the transport patterns below.
const antigravityNetworkIssueError = "There was a network issue connecting to the server, please try again."

// agy 1.4.6 may carry this historical/recovered stream error into a resumed
// turn's result even when the current final response is DONE (#9528).
const antigravityStreamInterruptedError = "The stream was interrupted. Please continue the task you were working on."

// antigravityTransportErrorRe matches the causes Go's http client reports when
// a round trip never produced a response: socket, DNS and TLS failures. These
// are the strings that appear inside the `*url.Error` agy wraps as
// `API error (attempt N): request failed: Post "...": <cause>`.
//
// Two things are deliberately excluded.
//
//   - agy's own `request failed:` prefix. It is tempting to match it directly
//     since it marks an http.Client failure, but only one spelling has been
//     observed in the field, and nothing rules out agy reusing the same prefix
//     for an HTTP status error. Matching the cause keeps a provider rejection
//     from being read as transport noise.
//   - Provider-side rejections: quota, capacity, overload, policy and auth all
//     arrive as an HTTP response, so they are decisions about the request
//     rather than a failure to deliver it. Those must stay failures the user
//     sees instead of being smoothed over by a complete-looking answer —
//     reportTaskResult documents failing closed for exactly that reason.
var antigravityTransportErrorRe = regexp.MustCompile(`(?i)(\bEOF\b|connection reset by peer|broken pipe|connection refused|connection timed out|i/o timeout|tls handshake timeout|tls: handshake failure|use of closed network connection|network is unreachable|no such host|server misbehaving|malformed HTTP response|http2: client connection lost|http2: server sent GOAWAY)`)

// antigravityTrailingTransportError reports whether agy's provider error
// describes a transport-level failure rather than a decision the provider made
// about the request.
func antigravityTrailingTransportError(providerError string) bool {
	trimmed := strings.TrimSpace(providerError)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, antigravityNetworkIssueError) || strings.EqualFold(trimmed, antigravityStreamInterruptedError) {
		return true
	}
	return antigravityTransportErrorRe.MatchString(trimmed)
}

func (u antigravityStreamUsage) hasTokens() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0
}

func (u antigravityStreamUsage) tokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:      nonNegativeTokens(u.InputTokens),
		OutputTokens:     nonNegativeTokens(u.OutputTokens),
		CacheReadTokens:  nonNegativeTokens(u.CacheReadTokens),
		CacheWriteTokens: nonNegativeTokens(u.CacheWriteTokens),
	}
}

func sumAntigravityStepUsage(steps map[int]TokenUsage) (TokenUsage, bool) {
	var total TokenUsage
	for _, step := range steps {
		total.InputTokens += step.InputTokens
		total.OutputTokens += step.OutputTokens
		total.CacheReadTokens += step.CacheReadTokens
		total.CacheWriteTokens += step.CacheWriteTokens
	}
	return total, len(steps) > 0
}

func antigravityResultStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "", "SUCCESS", "COMPLETED":
		return "completed"
	case "CANCELLED", "CANCELED":
		return "cancelled"
	case "ABORTED":
		return "aborted"
	case "TIMEOUT", "TIMED_OUT":
		return "timeout"
	default:
		return "failed"
	}
}

// antigravityCompletedDespiteTrailingNetworkError reports whether a turn that
// agy ended in an error actually delivered a finished answer first. All three
// conditions are required: the trailing failure has to be transport-level (a
// provider-side rejection is a real failure), agy has to have handed back a
// non-empty canonical response, and the latest agent_response step has to have
// reached DONE — an ACTIVE step means the answer was still being written when
// the connection went away. The stream-interrupted sentence also requires a
// successful process exit, since a DONE step can precede a genuinely interrupted
// tool call in the same turn.
func antigravityCompletedDespiteTrailingNetworkError(providerError, response string, agentResponseDone, exitedSuccessfully bool) bool {
	if strings.EqualFold(strings.TrimSpace(providerError), antigravityStreamInterruptedError) && !exitedSuccessfully {
		return false
	}
	return antigravityTrailingTransportError(providerError) &&
		strings.TrimSpace(response) != "" &&
		agentResponseDone
}

var antigravitySelectedModelRe = regexp.MustCompile(
	`Propagating selected model override to backend:\s*label="([^"\r\n]+)"`,
)

func readAntigravitySelectedModel(logPath, streamModel, configuredModel string) string {
	if model := strings.TrimSpace(streamModel); model != "" {
		return model
	}
	if logPath != "" {
		if data, err := os.ReadFile(logPath); err == nil {
			matches := antigravitySelectedModelRe.FindAllSubmatch(data, -1)
			if len(matches) > 0 {
				return strings.TrimSpace(string(matches[len(matches)-1][1]))
			}
		}
	}
	if model := strings.TrimSpace(configuredModel); model != "" {
		return model
	}
	return "unknown"
}

func (b *antigravityBackend) executeCLI(ctx context.Context, prompt string, opts ExecOptions, execPath string) (*Session, error) {
	if opts.Model != "" {
		catalog, _ := ListModels(ctx, "antigravity", b.cfg.commandAt(execPath))
		if err := antigravityModelError(opts.Model, catalog.Models); err != nil {
			return nil, err
		}
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	logFile, err := os.CreateTemp("", "multica-agy-log-*.log")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create agy log file: %w", err)
	}
	logPath := logFile.Name()
	_ = logFile.Close()

	args := buildAntigravityArgs(prompt, logPath, timeout, opts, b.cfg.Logger)

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("agy stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[agy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		cancel()
		_ = os.Remove(logPath)
		return nil, fmt.Errorf("start agy: %w", err)
	}

	b.cfg.Logger.Info("agy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer os.Remove(logPath)

		startTime := time.Now()
		var output strings.Builder
		var streamModel string
		var streamSessionID string
		var streamResultStatus string
		var streamResultError string
		var streamResponse string
		var streamResultUsage *antigravityStreamUsage
		streamStepUsage := make(map[int]TokenUsage)
		streamTools := make(map[int]antigravityToolState)
		streamLatestAgentResponseStep := -1
		streamLatestAgentResponseDone := false
		finalStatus := "completed"
		var finalError string

		scanner := newAgentStreamScanner(stdout)

		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

	streamLoop:
		for scanner.Scan() {
			line := scanner.Text()
			var event antigravityStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err == nil && event.Event != "" {
				switch event.Event {
				case "init":
					streamSessionID = event.ConversationID
					streamModel = event.Init.Model
				case "step_update":
					if event.StepUpdate == nil {
						continue
					}
					if event.StepUpdate.ConversationID != "" {
						streamSessionID = event.StepUpdate.ConversationID
					}
					for _, message := range antigravityToolMessages(event.StepUpdate, streamTools) {
						select {
						case msgCh <- message:
						case <-runCtx.Done():
							break streamLoop
						}
					}
					if event.StepUpdate.StepType == "agent_response" && event.StepUpdate.StepIndex != nil && *event.StepUpdate.StepIndex >= streamLatestAgentResponseStep {
						streamLatestAgentResponseStep = *event.StepUpdate.StepIndex
						streamLatestAgentResponseDone = strings.EqualFold(event.StepUpdate.State, "done")
					}
					if event.StepUpdate.StepType == "agent_response" && event.StepUpdate.TextDelta != "" {
						output.WriteString(event.StepUpdate.TextDelta)
						trySend(msgCh, Message{Type: MessageText, Content: event.StepUpdate.TextDelta})
					}
					if strings.EqualFold(event.StepUpdate.State, "done") && event.StepUpdate.StepIndex != nil && event.StepUpdate.Usage != nil && event.StepUpdate.Usage.hasTokens() {
						streamStepUsage[*event.StepUpdate.StepIndex] = event.StepUpdate.Usage.tokenUsage()
					}
				case "result":
					if event.Result == nil {
						continue
					}
					if event.Result.ConversationID != "" {
						streamSessionID = event.Result.ConversationID
					}
					streamResultStatus = event.Result.Status
					streamResultError = event.Result.Error
					streamResponse = event.Result.Response
					if event.Result.Usage != nil && event.Result.Usage.hasTokens() {
						streamResultUsage = event.Result.Usage
					}
				}
				continue
			}
			chunk := line
			if output.Len() > 0 {
				output.WriteByte('\n')
				chunk = "\n" + line
			}
			output.WriteString(line)
			if chunk != "" {
				trySend(msgCh, Message{Type: MessageText, Content: chunk})
			}
		}
		if err := scanner.Err(); err != nil {
			b.cfg.Logger.Warn("agy stdout scanner error", "err", err)
		}

		waitErr := cmd.Wait()
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		sessionID := streamSessionID
		if sessionID == "" {
			sessionID = readAntigravityConversationID(logPath)
		}

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("agy timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if status := antigravityResultStatus(streamResultStatus); finalStatus == "completed" && status != "completed" {
			if antigravityCompletedDespiteTrailingNetworkError(streamResultError, streamResponse, streamLatestAgentResponseDone, waitErr == nil) {
				// agy can emit a complete DONE response and only then fail a
				// follow-up network operation. Preserve the finished answer instead
				// of presenting that trailing transport error as a failed turn.
				b.cfg.Logger.Warn("agy reported a trailing network error after a completed response", "err", streamResultError)
			} else {
				finalStatus = status
				finalError = streamResultError
				if finalError == "" {
					finalError = fmt.Sprintf("agy returned status %s", streamResultStatus)
				}
			}
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy exited with error: %v", waitErr)
		} else if finalStatus == "completed" && antigravityPrintTimedOut(logPath) {
			finalStatus = "timeout"
			finalError = fmt.Sprintf(
				"agy --print-timeout elapsed after %s waiting for the agent response; a long-running command likely outlived the print timeout",
				antigravityPrintTimeout(timeout),
			)
		} else if providerErr := antigravityProviderError(logPath); finalStatus == "completed" && providerErr != "" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("agy provider error: %s", providerErr)
		}
		if finalError != "" {
			finalError = withAgentStderr(finalError, "agy", stderrBuf.Tail())
		}

		finalOutput := output.String()
		if streamResponse != "" {
			if finalOutput == "" {
				trySend(msgCh, Message{Type: MessageText, Content: streamResponse})
			}
			finalOutput = streamResponse
		}
		if finalStatus == "completed" && strings.TrimSpace(finalOutput) == "" {
			if recovered := readAntigravityTranscriptOutput(logPath, sessionID); recovered != "" {
				finalOutput = recovered
				trySend(msgCh, Message{Type: MessageText, Content: recovered})
				b.cfg.Logger.Info("agy recovered empty stdout from transcript", "bytes", len(recovered))
			}
		}

		b.cfg.Logger.Info("agy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		usage, hasStepUsage := sumAntigravityStepUsage(streamStepUsage)
		if !hasStepUsage && streamResultUsage != nil {
			usage = streamResultUsage.tokenUsage()
		}
		usageByModel := map[string]TokenUsage{}
		if usage != (TokenUsage{}) {
			model := readAntigravitySelectedModel(logPath, streamModel, opts.Model)
			usageByModel[model] = usage
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      usageByModel,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

var antigravityConversationIDRe = regexp.MustCompile(
	`conversation=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`,
)

var antigravityPrintTimeoutRe = regexp.MustCompile(`Print mode: timed out after \d+ polls`)

var antigravityProviderErrorRe = regexp.MustCompile(`agent executor error:\s*(.+)`)

func antigravityPrintTimedOut(logPath string) bool {
	if logPath == "" {
		return false
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return false
	}
	return antigravityPrintTimeoutRe.Match(data)
}

func antigravityProviderError(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := antigravityProviderErrorRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSpace(string(matches[len(matches)-1][1]))
}

func readAntigravityConversationID(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := antigravityConversationIDRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

var antigravityAppDataDirRe = regexp.MustCompile(`CLI app data directory:\s*(.+)`)

type antigravityTranscriptRecord struct {
	Type    string          `json:"type"`
	Source  string          `json:"source"`
	Status  string          `json:"status"`
	Content json.RawMessage `json:"content"`
}

func readAntigravityTranscriptOutput(logPath, conversationID string) string {
	if logPath == "" || conversationID == "" {
		return ""
	}
	appDataDir := readAntigravityAppDataDir(logPath)
	if appDataDir == "" {
		return ""
	}
	transcriptPath := filepath.Join(
		appDataDir, "brain", conversationID, ".system_generated", "logs", "transcript.jsonl",
	)
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var parts []string
	scanner := newAgentStreamScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec antigravityTranscriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type == "USER_INPUT" {
			parts = parts[:0]
			continue
		}
		if rec.Type != "PLANNER_RESPONSE" || rec.Source != "MODEL" || rec.Status != "DONE" {
			continue
		}
		var text string
		if err := json.Unmarshal(rec.Content, &text); err != nil {
			continue
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func readAntigravityAppDataDir(logPath string) string {
	if logPath == "" {
		return ""
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	m := antigravityAppDataDirRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func buildAntigravityArgs(prompt, logPath string, timeout time.Duration, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, "--print-timeout", antigravityFormatTimeout(antigravityPrintTimeout(timeout)))
	args = append(args, "--log-file", logPath)
	if opts.ResumeSessionID != "" {
		args = append(args, "--conversation", opts.ResumeSessionID)
	}
	if opts.Cwd != "" {
		args = append(args, "--add-dir", filepath.Clean(opts.Cwd))
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, antigravityBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, antigravityBlockedArgs, logger)...)
	return args
}

// antigravityModelError returns an actionable error when `model` is non-empty
// and definitively absent from `available`.
func antigravityModelError(model string, available []Model) error {
	if model == "" || len(available) == 0 {
		return nil
	}
	ids := make([]string, 0, len(available))
	for _, m := range available {
		if m.ID == model {
			return nil
		}
		ids = append(ids, m.ID)
	}
	return fmt.Errorf(
		"antigravity model %q is not available; pick one of: %s",
		model, strings.Join(ids, ", "),
	)
}

const antigravityNoCapPrintTimeout = 24 * time.Hour

func antigravityPrintTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return antigravityNoCapPrintTimeout
}

func antigravityFormatTimeout(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	return d.String()
}
