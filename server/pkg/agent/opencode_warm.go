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
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// OpenCode over a warm ACP process (`opencode acp` kept alive across turns).
//
// The WarmSessionPool pattern is the one codex_process.go and claude_process.go
// use: a turn either checks out the process parked for its session id (the
// fingerprint of every session-immutable launch input must match) or spawns a
// fresh pooled process, and a turn that completed cleanly parks the process
// again. The ACP wire protocol makes this natural — the process already holds
// the live session, so a follow-up turn is another `session/prompt` on the
// same connection, with no re-handshake, no model-catalog refresh and no
// second cold MCP initialization.
//
// Isolation invariants carried over from the other warm providers:
//
//   - Volatile per-task credentials (MULTICA_TOKEN, …) never enter the pooled
//     process's static environment; they travel through the 0o600 per-session
//     task-env overlay file (TaskEnvFileEnv) that is refreshed before each
//     turn and cleared once the turn's result is decided.
//   - Unhealthy turns are never parked: a cancelled, timed-out, failed or
//     empty turn closes the process, and the next turn cold-starts.
//   - Session-scoped configuration (model, reasoning effort, MCP set) is
//     fixed when the session opens; every input that could change it is part
//     of the fingerprint, so a changed claim evicts the process instead of
//     silently serving the turn with the previous configuration.
//
// Everything turn-shaped here mirrors executeACP (opencode_acp.go): the
// dialect tuning, model selection, effort option, usage export and the
// empty-step guard are the same decisions on a process that outlives the
// turn. The run transport and the one-shot ACP path stay untouched as the
// fallback: the pool is nil (MULTICA_WARM_POOL off), and the warm gate below
// holds the claim to the same eligibility contract the one-shot transport
// already enforces.

// acpWarmHandshakeTimeout bounds the initialize / session/new / session/load
// RPCs of a warm turn's first-turn setup. Generous: a cold CLI can take
// seconds to boot. The prompt itself is not bounded by it — the daemon's
// watchdogs supervise the turn through its own context.
const acpWarmHandshakeTimeout = 30 * time.Second

// opencodeWarmFingerprint hashes every session-immutable input of a pooled
// ACP process. ResumeSessionID is deliberately omitted (claude warmFingerprint's
// rule): it selects which conversation a process serves, not how the process
// was built. The env input is the launch environment the one-shot path would
// have used (PWD pin, OPENCODE_CONFIG_CONTENT on 1.x), before the pooled
// overlay repointing — volatile keys and the overlay path itself are excluded
// by computeWarmSessionFingerprint.
func (b *opencodeBackend) opencodeWarmFingerprint(execPath string, opts ExecOptions, envMap map[string]string) string {
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:      "opencode",
		Executable:    execPath,
		Args:          []string{"acp"},
		Cwd:           opts.Cwd,
		Model:         opts.Model,
		ThinkingLevel: opts.ThinkingLevel,
		McpConfig:     opts.McpConfig,
		BriefHash:     opts.WarmPoolBriefHash,
		Env:           envMap,
		ExtraFlags:    []string{fmt.Sprintf("usesV2=%t", opencodeUsesV2Contract(b.cfg))},
	})
}

// executeWarmACP runs the turn through the warm pool: acquire a parked
// process or spawn a fresh pooled one, run the turn on it, then park or close
// it according to the result. The returned Session forwards the turn's
// message stream directly; its Result is synthesized here so the park
// decision happens before the caller learns the turn ended.
func (b *opencodeBackend) executeWarmACP(ctx context.Context, execPath, prompt string, opts ExecOptions) (*Session, error) {
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}

	proc, warmHit, err := b.acquireWarmOpenCode(execPath, opts)
	if err != nil {
		return nil, err
	}

	turn, turnErr := proc.runTurn(ctx, prompt, opts)
	if turnErr != nil {
		// The process could not take this turn at all (dead process, invalid
		// mcp_config): it holds nothing reusable.
		proc.Close()
		return nil, turnErr
	}

	resCh := make(chan Result, 1)
	out := &Session{Messages: turn.Messages, Result: resCh}
	go func() {
		res := <-turn.Result
		parked := res.Status == "completed" && res.SessionID != "" && proc.Alive()
		if parked {
			proc.clearTaskEnv()
			parked = b.cfg.WarmPool.Park("opencode", res.SessionID, proc)
		}
		if !parked {
			proc.Close()
		}
		b.cfg.Logger.Info("opencode finished",
			"pid", proc.cmd.Process.Pid,
			"warm_hit", warmHit,
			"warm_parked", parked,
			"status", res.Status,
			"duration", time.Duration(res.DurationMs)*time.Millisecond,
		)
		resCh <- res
	}()
	return out, nil
}

// acquireWarmOpenCode checks out the process parked for opts.ResumeSessionID,
// refreshing its task-env overlay with this turn's credentials, or spawns a
// fresh pooled process.
func (b *opencodeBackend) acquireWarmOpenCode(execPath string, opts ExecOptions) (*opencodeWarmProcess, bool, error) {
	envMap, err := b.opencodeWarmLaunchEnv(opts)
	if err != nil {
		return nil, false, err
	}
	fingerprint := b.opencodeWarmFingerprint(execPath, opts, envMap)

	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("opencode", opts.ResumeSessionID, fingerprint); acquired != nil {
			if proc, ok := acquired.(*opencodeWarmProcess); ok {
				if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm opencode: failed to refresh task env overlay; evicting", "error", err)
					proc.Close()
				} else {
					return proc, true, nil
				}
			} else {
				// Not ours; nothing of this shape should ever be parked under
				// the opencode key. Close it rather than leak the process.
				acquired.Close()
			}
		}
	}

	proc, err := b.spawnWarmOpenCode(execPath, opts, envMap, fingerprint)
	if err != nil {
		return nil, false, err
	}
	return proc, false, nil
}

// opencodeWarmLaunchEnv builds the launch environment the one-shot ACP path
// would use (openCodeEnvironment with the same per-major options shape), as a
// map — the fingerprint input and the base for the pooled overlay repointing.
func (b *opencodeBackend) opencodeWarmLaunchEnv(opts ExecOptions) (map[string]string, error) {
	envOpts := opts
	if opencodeUsesV2Contract(b.cfg) {
		// 2.x carries MCP in session/new; the env must not (executeACP's rule).
		envOpts = ExecOptions{Cwd: opts.Cwd}
	}
	env, err := openCodeEnvironment(b.cfg, envOpts)
	if err != nil {
		return nil, err
	}
	return envSliceToMap(env), nil
}

// opencodeWarmProcess is one pooled `opencode acp` process holding a live ACP
// connection. The long-lived pieces (process, pipes, stdout reader, client)
// are created once at spawn; per-turn state travels through the bridge, so
// protocol callbacks always land on the turn that is actually prompting.
type opencodeWarmProcess struct {
	cfg Config

	cmd         *exec.Cmd
	stdin       io.WriteCloser
	client      *hermesClient
	executable  string
	protocolCwd string
	usesV2      bool

	// Pooled-process plumbing. launchEnv is the overlay-repointed child
	// environment (export subprocesses must not reintroduce volatile keys);
	// envMap is the pre-overlay launch environment, the export's provider
	// configuration source and the fingerprint input.
	sessionDir  string
	taskEnvFile string
	launchEnv   map[string]string

	fingerprint string

	mu        sync.Mutex
	sessionID string

	// Cumulative-usage baseline for the 2.x export-failure fallback: a
	// session-total snapshot settles against the previous turn's snapshot.
	usageMu        sync.Mutex
	lastUsage      *TokenUsage
	lastUsageKnown bool
	resumed        bool

	// bridge forwards protocol callbacks to the active turn.
	bridgeMu         sync.Mutex
	bridgeMsgs       chan<- Message
	bridgeUsage      *TokenUsage
	bridgeStopReason string
	bridgeOutput     strings.Builder
	bridgeSawText    bool
	bridgeSawTool    bool
	streaming        atomic.Bool

	activity   chan struct{}
	readerDone chan struct{}
	stderrDone chan struct{}

	// closed flips before teardown starts, so Alive stops advertising the
	// process the moment Close is on the floor.
	closed      atomic.Bool
	procCancel  context.CancelFunc
	stopOnce    sync.Once
	closeOnce   sync.Once
	providerErr *acpProviderErrorSniffer
	promptSent  atomic.Bool

	turnMu sync.Mutex
}

func (p *opencodeWarmProcess) currentSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionID
}

func (p *opencodeWarmProcess) storeSessionID(id string) string {
	p.mu.Lock()
	p.sessionID = id
	p.mu.Unlock()
	return id
}

// Fingerprint implements warmPoolProcess: the launch identity this process
// was spawned under.
func (p *opencodeWarmProcess) Fingerprint() string { return p.fingerprint }

// Alive implements warmPoolProcess: no teardown is on the floor and the
// stdout reader has not hit EOF (codexProcess's rule — reader EOF is the
// process-gone signal, and stop() owns the single cmd.Wait).
func (p *opencodeWarmProcess) Alive() bool {
	if p.closed.Load() {
		return false
	}
	select {
	case <-p.readerDone:
		return false
	default:
		return true
	}
}

func (p *opencodeWarmProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// Close terminates the process group and removes the session dir. It is
// idempotent. opencode 2.x hosts its server as a separate process group, so
// stdin gets the EOF window first (the CLI cleans up out-of-group children on
// EOF) before the group signal — the same shape executeACP's stop uses.
func (p *opencodeWarmProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.stop(false)
		pid := 0
		if p.cmd.Process != nil {
			pid = p.cmd.Process.Pid
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
		p.cfg.Logger.Info("warm opencode process closed", "pid", pid)
	})
	return true
}

// stop tears the process down exactly once; force skips the EOF window (an
// unresponsive runtime is gone within opencodeTerminateGrace). Shared by
// Close and the mid-turn teardown paths.
func (p *opencodeWarmProcess) stop(force bool) {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		_ = p.stdin.Close()
		if !force {
			select {
			case <-p.readerDone:
			case <-time.After(opencodeACPExitGrace):
				force = true
			}
		}
		if force {
			signalProcessGroup(p.cmd, syscall.SIGTERM)
			select {
			case <-p.readerDone:
			case <-time.After(opencodeTerminateGrace()):
			}
		}
		p.procCancel() // group SIGKILL; a no-op for a group that already exited
		_ = p.cmd.Wait()
		<-p.readerDone
		<-p.stderrDone
		releaseProcessGroup(p.cmd)
	})
}

// spawnWarmOpenCode launches a pooled ACP process on its own context with the
// overlay-based credential plumbing. The spawn mirrors executeACP's
// (Background context, hideAgentWindow, owned process group) plus the pooled
// session dir: the task-env overlay file and a private temp dir that outlive
// the task that started the process.
func (b *opencodeBackend) spawnWarmOpenCode(execPath string, opts ExecOptions, envMap map[string]string, fingerprint string) (*opencodeWarmProcess, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	protocolCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("opencode working directory: %w", err)
	}

	sessionDir, err := os.MkdirTemp("", "multica-opencode-warm-*")
	if err != nil {
		return nil, fmt.Errorf("create warm opencode session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("create warm opencode temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("write warm opencode task env overlay: %w", err)
	}
	// The overlay repointing strips the volatile keys from the static child
	// environment, adds the overlay pointer, and moves TMPDIR onto the
	// session dir so per-task temp cleanup cannot pull the rug from under a
	// parked process.
	launchEnv := PreparePooledChildEnv(envMap, taskEnvFile, sessionTempDir)

	p := &opencodeWarmProcess{
		cfg:         b.cfg,
		executable:  execPath,
		protocolCwd: protocolCwd,
		usesV2:      opencodeUsesV2Contract(b.cfg),
		sessionDir:  sessionDir,
		taskEnvFile: taskEnvFile,
		launchEnv:   launchEnv,
		fingerprint: fingerprint,
		activity:    make(chan struct{}, 1),
	}

	procCtx, procCancel := context.WithCancel(context.Background())
	p.procCancel = procCancel
	cmd := b.cfg.commandAt(execPath).exec(procCtx, "acp")
	hideAgentWindow(cmd)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = protocolCwd
	cmd.Env = buildEnv(launchEnv)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs([]string{"acp"}, trustAgentCommandPositional(0, "acp")))

	fail := func(err error) (*opencodeWarmProcess, error) {
		procCancel()
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("opencode stdout pipe: %w", err))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("opencode stdin pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(fmt.Errorf("opencode stderr pipe: %w", err))
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fail(fmt.Errorf("start opencode: %w", err))
	}
	p.cmd = cmd
	p.stdin = stdin
	b.cfg.Logger.Info("warm opencode spawned", "pid", cmd.Process.Pid, "cwd", protocolCwd, "model", opts.Model)

	p.providerErr = newACPProviderErrorSniffer("opencode")
	p.stderrDone = make(chan struct{})
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[opencode:stderr] "), p.providerErr), stderr)
	}()

	p.client = &hermesClient{
		cfg:                        b.cfg,
		stdin:                      stdin,
		pending:                    make(map[int]*pendingRPC),
		pendingTools:               make(map[string]*pendingToolCall),
		toolInputArrivesInProgress: true,
		toolOutputFromContent:      true,
		toolName:                   opencodeACPToolName,
		// session/load replays the whole conversation as session/update; only
		// the turn this process is prompting is that turn's transcript.
		acceptNotification: func(string) bool { return p.streaming.Load() },
		acceptUpdate: func(updateType string, update json.RawMessage) bool {
			if updateType == "tool_call" || updateType == "tool_call_update" {
				return true
			}
			return !opencodeACPChildUpdate(update)
		},
		onActivity: func() {
			select {
			case p.activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
			if !p.streaming.Load() {
				return
			}
			p.bridgeMu.Lock()
			defer p.bridgeMu.Unlock()
			switch msg.Type {
			case MessageText:
				p.bridgeSawText = true
				p.bridgeOutput.WriteString(msg.Content)
			case MessageToolUse:
				p.bridgeSawTool = true
			}
			if p.bridgeMsgs != nil {
				trySend(p.bridgeMsgs, msg)
			}
		},
		// extractPromptResult fires on the session/prompt RESPONSE: it parses
		// this turn's usage snapshot and stop reason, which the turn goroutine
		// reads back through the bridge right after the request returns. The
		// snapshot is session-cumulative on 2.x, so warmTurnUsage settles it
		// against the previous turn's snapshot.
		onPromptDone: func(result hermesPromptResult) {
			u := result.usage.TokenUsage
			p.bridgeMu.Lock()
			p.bridgeUsage = &u
			p.bridgeStopReason = result.stopReason
			p.bridgeMu.Unlock()
		},
	}

	p.readerDone = make(chan struct{})
	go func() {
		defer close(p.readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				p.client.handleLine(line)
			}
		}
		p.client.closeAllPending(errors.New("opencode process exited"))
	}()

	return p, nil
}

// runTurn executes one turn on the live process and returns a turn-scoped
// Session whose Result carries the outcome. Setup failures land in the Result
// too (with the session id and ResumeRejected semantics the one-shot path
// reports, so the daemon's retry can resume cold); only a process that cannot
// take the turn at all comes back as a Go error. The process is left for the
// caller (executeWarmACP) to park or close — except on setup paths that
// cannot produce a session, which tear it down here.
func (p *opencodeWarmProcess) runTurn(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	p.turnMu.Lock()
	if !p.Alive() {
		p.turnMu.Unlock()
		return nil, errors.New("warm opencode process is not running")
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	startTime := time.Now()
	sessionID := p.currentSessionID()

	if sessionID == "" {
		// First turn on this process: handshake and open the conversation.
		// The MCP set is process-scoped and fixed at session start.
		result := func(status, errText string, sid string, rejected bool) *Session {
			p.turnMu.Unlock()
			close(msgCh)
			resCh <- Result{Status: status, Error: errText, SessionID: sid, ResumeRejected: rejected, DurationMs: time.Since(startTime).Milliseconds()}
			return &Session{Messages: msgCh, Result: resCh}
		}
		var mcpServers []any
		var err error
		if p.usesV2 {
			if mcpServers, err = opencodeACPMcpServers(opts.McpConfig, p.cfg); err != nil {
				p.turnMu.Unlock()
				p.Close()
				return nil, err
			}
		} else {
			mcpServers = []any{}
		}

		// Bound only the setup RPCs by the handshake budget; the prompt
		// itself is supervised by the daemon's watchdogs via ctx. The process
		// is never cancelled through this context — it outlives the turn. A
		// runtime that stops reading (a large session/new MCP payload) would
		// hold the request write, so the timeout closes stdin to release it.
		setupCtx, cancelSetup := context.WithTimeout(ctx, acpWarmHandshakeTimeout)
		defer cancelSetup()
		stopBlockedSetup := context.AfterFunc(setupCtx, func() { _ = p.stdin.Close() })
		init, err := p.client.request(setupCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo":      map[string]any{"name": "multica-agent-sdk", "version": "0.2.0"},
			"clientCapabilities": map[string]any{
				"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
				"terminal": false,
			},
		})
		stopBlockedSetup()
		if err != nil {
			status, errText, _ := classifyACPResumeFailure(setupCtx, "opencode", "initialize", err, opts.Timeout, p.cfg.Logger)
			p.Close()
			return result(status, errText, "", false), nil
		}
		var negotiated struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if json.Unmarshal(init, &negotiated) != nil || negotiated.ProtocolVersion != 1 {
			p.Close()
			return result("failed", "opencode ACP: unsupported protocol version", "", false), nil
		}
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(init), "opencode", p.cfg)

		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			p.resumed = true
			loaded, err := p.client.request(setupCtx, "session/load", map[string]any{
				"cwd":        p.protocolCwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText, rejected := classifyACPResumeFailure(setupCtx, "opencode", "session/load", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, "", rejected), nil
			}
			resolved, changed := resolveResumedSessionID(opts.ResumeSessionID, loaded)
			if changed {
				p.cfg.Logger.Warn("opencode returned a different session id on resume; continuing with the new id",
					"requested", opts.ResumeSessionID, "actual", resolved)
			}
			sessionID = p.storeSessionID(resolved)
			sessionResult = loaded
		} else {
			created, err := p.client.request(setupCtx, "session/new", map[string]any{
				"cwd":        p.protocolCwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText, _ := classifyACPResumeFailure(setupCtx, "opencode", "session/new", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, "", false), nil
			}
			if sessionID = extractACPSessionID(created); sessionID == "" {
				p.Close()
				return result("failed", "opencode session/new returned no session ID", "", false), nil
			}
			p.storeSessionID(sessionID)
			sessionResult = created
		}
		p.client.sessionID = sessionID

		// Session-scoped model and effort selection, the one-shot path's
		// post-session/new RPCs. Runs once per process; later turns keep the
		// session's settled configuration, and a claim that changes model or
		// effort changes the fingerprint and gets its own process. A failure
		// fails the turn and tears the process down — the daemon falls back
		// to a cold path, which runs the same selection on its own process.
		model, level := opencodeACPSplitModel(opts.Model, opts.ThinkingLevel)
		options, err := opencodeACPSelectModel(setupCtx, p.client.request, sessionID, model, sessionResult)
		if err != nil {
			status, errText, _ := classifyACPResumeFailure(setupCtx, "opencode", "model selection", err, opts.Timeout, p.cfg.Logger)
			// The process is untrustworthy whatever the failure: its session
			// now holds a half-applied configuration. The session id stays in
			// the Result (unless withholding applies) so the daemon's cold
			// retry can resume the conversation from the CLI's transcript.
			keep := sessionID
			if setupFailureWithholdsSessionID(opts) {
				keep = ""
			}
			p.Close()
			return result(status, errText, keep, false), nil
		}
		if options != nil {
			applyACPEffortOption(setupCtx, p.client.request, "opencode", p.cfg.Logger, sessionID, options, level, true)
		} else if level != "" {
			p.cfg.Logger.Warn("opencode session advertises no configuration after model selection; ignoring thinking level", "thinkingLevel", level)
		}

		p.cfg.Logger.Info("warm opencode session ready", "session_id", sessionID, "resumed", opts.ResumeSessionID != "")
	}
	// The history replay (if session/load sent one) has been answered; from
	// here the stream belongs to the prompted turn. The bridge is wired
	// before the goroutine so the first stream frames cannot slip past it.
	p.bridgeMu.Lock()
	p.bridgeMsgs = msgCh
	p.bridgeUsage = nil
	p.bridgeStopReason = ""
	p.bridgeOutput.Reset()
	p.bridgeSawText = false
	p.bridgeSawTool = false
	p.bridgeMu.Unlock()

	trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

	// NOTE: the bridge is NOT cleared when runTurn returns — the turn's
	// protocol messages keep arriving after the caller takes the Session and
	// must keep flowing into its channel and output builder. The turn
	// goroutine clears the bridge when the result is decided; turnMu plus one
	// turn at a time per process keep turns ordered.

	// OpenCode reads the runtime brief from the AGENTS.md the daemon writes
	// into the workdir, so SystemPrompt is not prepended (executeACP's rule).
	go func() {
		defer p.turnMu.Unlock()
		finalStatus := "completed"
		finalError := ""
		turnStart := time.Now()
		p.streaming.Store(true)
		promptCtx, cancel := runContext(ctx, opts.Timeout)
		defer cancel()
		p.promptSent.Store(false)
		stopBlockedWrite := context.AfterFunc(promptCtx, func() {
			if !p.promptSent.Load() {
				_ = p.stdin.Close()
			}
		})

		type promptOutcome struct{ err error }
		promptCh := make(chan promptOutcome, 1)
		go func() {
			_, err := p.client.requestAndNotifySent(context.Background(), "session/prompt", map[string]any{
				"sessionId": sessionID,
				"prompt":    []map[string]any{{"type": "text", "text": prompt}},
			}, func() { p.promptSent.Store(true) })
			promptCh <- promptOutcome{err: err}
		}()

		answered := false
		var promptErr error
		select {
		case out := <-promptCh:
			promptErr, answered = out.err, true
		case <-promptCtx.Done():
			if !p.promptSent.Load() {
				p.cfg.Logger.Warn("opencode: turn ended before the prompt was delivered; stopping the process")
				break
			}
			// Ask OpenCode to stop the turn; it aborts the running tool and
			// answers the prompt. The write runs on its own goroutine so a
			// stalled pipe cannot hold the teardown, and the grace starts now.
			go func() {
				if err := opencodeACPNotify(p.client, "session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
					p.cfg.Logger.Warn("opencode: session/cancel could not be sent", "error", err)
				}
			}()
			select {
			case out := <-promptCh:
				promptErr, answered = out.err, true
			case <-time.After(opencodeACPCancelGrace):
				p.cfg.Logger.Warn("opencode did not answer session/cancel in time; stopping the process", "grace", opencodeACPCancelGrace.String())
			}
		}
		stopBlockedWrite()

		var pr hermesPromptResult
		havePrompt := false
		if answered && promptErr == nil {
			p.bridgeMu.Lock()
			pr = hermesPromptResult{stopReason: p.bridgeStopReason, usage: acpUsageSnapshot{TokenUsage: derefTokenUsage(p.bridgeUsage)}}
			p.bridgeMu.Unlock()
			havePrompt = true
		}

		switch {
		case promptCtx.Err() == context.DeadlineExceeded:
			finalStatus, finalError = "timeout", fmt.Sprintf("opencode timed out after %s", opts.Timeout)
		case promptCtx.Err() == context.Canceled:
			finalStatus, finalError = "aborted", "execution cancelled"
		case promptErr != nil:
			finalStatus, finalError = "failed", fmt.Sprintf("opencode session/prompt failed: %v", promptErr)
		case havePrompt:
			switch pr.stopReason {
			case "", "end_turn":
			case "cancelled":
				finalStatus, finalError = "aborted", "opencode cancelled the turn"
			case "max_tokens":
				finalStatus, finalError = "failed", "opencode stopped: the model hit its output length limit"
			case "refusal":
				finalStatus, finalError = "failed", "opencode stopped: the provider refused the request"
			default:
				finalStatus, finalError = "failed", "opencode stopped: "+pr.stopReason
			}
		}
		if answered && promptCtx.Err() == nil {
			waitForACPNotificationQuiescence(promptCtx, p.activity, p.readerDone, acpNotificationQuietTime, opencodeACPDrainGrace)
		}
		p.streaming.Store(false)
		if !answered {
			// A turn OpenCode never answered leaves the process unresponsive
			// (its stdin write may still be blocked): force it down now. The
			// wrapper's later Close is idempotent. An answered turn —
			// including an acknowledged cancel — leaves the process for the
			// wrapper to park or close; nothing here ends its life.
			p.stop(true)
		}
		p.providerErr.Finalize()

		p.bridgeMu.Lock()
		finalOutput := p.bridgeOutput.String()
		snapshot := p.bridgeUsage
		sawText, sawTool := p.bridgeSawText, p.bridgeSawTool
		p.bridgeMu.Unlock()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, p.providerErr)

		usage, usageErr := p.warmTurnUsage(opts, sessionID, turnStart, snapshot)
		if usageErr != nil {
			// Fall back to what the prompt reported only where that figure is
			// not an overcount: 1.x reports one message (an undercount), and a
			// fresh 2.x session's total is this turn (executeACP's rule).
			if havePrompt && (!p.usesV2 || opts.ResumeSessionID == "") && acpTokenUsagePresent(pr.usage.TokenUsage) {
				usage = map[string]TokenUsage{modelName(opts.Model): pr.usage.TokenUsage}
			}
			p.cfg.Logger.Warn("opencode: could not read this turn's token usage from the session export", "error", usageErr, "fallback", usage != nil)
		}

		// The run transport's empty-step guard (#6522): a turn that closes
		// cleanly with no text, no tool call and no usage at all never reached
		// the provider, and must not be reported — or parked — as a success.
		if finalStatus == "completed" && usageErr == nil && usage == nil && !sawText && !sawTool {
			finalStatus = "failed"
			finalError = "opencode turn ended with no text, no tool call and no reported usage — the provider produced nothing"
		}

		p.bridgeMu.Lock()
		if p.bridgeMsgs == msgCh {
			p.bridgeMsgs = nil
			p.bridgeUsage = nil
			p.bridgeStopReason = ""
			p.bridgeOutput.Reset()
			p.bridgeSawText = false
			p.bridgeSawTool = false
		}
		p.bridgeMu.Unlock()
		close(msgCh)

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: time.Since(startTime).Milliseconds(),
			SessionID:  sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// warmTurnUsage is the resident twin of opencodeACPTurnUsage: the export runs
// while the process stays alive (it reads the on-disk session store, not the
// process), and when the export fails, the prompt's usage snapshot settles as
// a delta against the previous turn's — 2.x reports session totals, so re-
// reporting the raw snapshot would bill history again. The export subprocess
// uses the pooled child environment: the static env has no volatile keys to
// leak, and it still carries the provider configuration (OPENCODE_CONFIG_CONTENT
// on 1.x) the export needs.
func (p *opencodeWarmProcess) warmTurnUsage(opts ExecOptions, sessionID string, turnStart time.Time, snapshot *TokenUsage) (map[string]TokenUsage, error) {
	usage, err := opencodeACPTurnUsage(p.cfg, p.executable, buildEnv(p.launchEnv), p.protocolCwd, sessionID, turnStart, p.usesV2)
	var fallback TokenUsage
	if p.usesV2 {
		fallback = p.settleCumulativeUsage(snapshot)
	} else if snapshot != nil {
		fallback = *snapshot
	}
	if err != nil {
		fallback.CostUSDTicks = 0
		if acpTokenUsagePresent(fallback) {
			return map[string]TokenUsage{modelName(opts.Model): fallback}, err
		}
		return nil, err
	}
	if !acpTokenUsagePresent(usage) {
		return nil, nil
	}
	return map[string]TokenUsage{modelName(opts.Model): usage}, nil
}

// settleCumulativeUsage converts a session-total snapshot into the per-turn
// delta the daemon bills, advancing the running baseline. A loaded session or
// an intervening missing snapshot has no trusted baseline: seed from the next
// known total without billing history again (the resident drivers' rule).
func (p *opencodeWarmProcess) settleCumulativeUsage(snapshot *TokenUsage) TokenUsage {
	if snapshot == nil || !acpUsagePresent(*snapshot) {
		p.usageMu.Lock()
		p.lastUsageKnown = false
		p.resumed = true
		p.usageMu.Unlock()
		return TokenUsage{}
	}
	p.usageMu.Lock()
	defer p.usageMu.Unlock()
	known := !p.resumed || p.lastUsageKnown
	p.lastUsageKnown = true
	if !known {
		u := *snapshot
		p.lastUsage = &u
		return TokenUsage{}
	}
	prev := TokenUsage{}
	if p.lastUsage != nil {
		prev = *p.lastUsage
	}
	delta := TokenUsage{
		InputTokens:      warmMaxInt64(0, snapshot.InputTokens-prev.InputTokens),
		OutputTokens:     warmMaxInt64(0, snapshot.OutputTokens-prev.OutputTokens),
		CacheReadTokens:  warmMaxInt64(0, snapshot.CacheReadTokens-prev.CacheReadTokens),
		CacheWriteTokens: warmMaxInt64(0, snapshot.CacheWriteTokens-prev.CacheWriteTokens),
		CostUSDTicks:     warmMaxInt64(0, snapshot.CostUSDTicks-prev.CostUSDTicks),
	}
	u := *snapshot
	p.lastUsage = &u
	return delta
}

func modelName(model string) string {
	if model == "" {
		return "unknown"
	}
	return model
}

func warmMaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func derefTokenUsage(u *TokenUsage) TokenUsage {
	if u == nil {
		return TokenUsage{}
	}
	return *u
}

// envSliceToMap folds a buildEnv-style KEY=VALUE slice into a map. Later
// entries win, matching os/exec's duplicate-key behavior.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			m[k] = v
		}
	}
	return m
}
