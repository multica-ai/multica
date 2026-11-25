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

// QwenPaw over a warm ACP process (`qwenpaw acp` kept alive across turns).
//
// The WarmSessionPool pattern is the one codex_process.go, claude_process.go
// and devin_warm.go use: a turn either checks out the process parked for its
// session id (the fingerprint of every session-immutable launch input must
// match) or spawns a fresh pooled process, and a turn that completed cleanly
// parks the process again. QwenPaw's ACP transport makes this natural — the
// process already holds the live session, so a follow-up turn is another
// `session/prompt` on the same connection, with no re-handshake and no second
// cold MCP initialization.
//
// Isolation invariants carried over from the other warm providers:
//
//   - Volatile per-task credentials (MULTICA_TOKEN, …) never enter the pooled
//     process's static environment; they travel through the 0o600 per-session
//     task-env overlay file (TaskEnvFileEnv) that is refreshed before each
//     turn and cleared once the turn's result is decided.
//   - Unhealthy turns are never parked: a cancelled, timed-out, failed or
//     empty turn closes the process, and the next turn cold-starts.
//   - Every input that could change the session is part of the fingerprint
//     (args including the per-task --workspace, cwd, MCP config, brief hash,
//     non-volatile env), so a changed claim evicts the process instead of
//     silently serving the turn with the previous configuration.
//
// QwenPaw-specific shape, kept at parity with the one-shot Execute
// (qwenpaw.go):
//
//   - No model, auth or effort setup: set_model persists to the shared
//     agent.json, so model selection is unsupported (ModelSelectionSupported
//     = false) and usage is always attributed to "unknown".
//   - The hermesClient stays ungated: the one-shot path processes every
//     notification (a resumed session's replay lands in the turn output, and
//     so does a warm first turn's), and no per-turn gate is introduced here.
//   - The hermesClient usage accumulator is per-process, so each turn resets
//     it before prompting.
//   - One deliberate strengthening: the one-shot path drains by waiting for
//     process exit, which a pooled process cannot do. A warm turn drains
//     with the shared notification-quiet window instead, so a prompt
//     response's trailing updates still reach this turn's output.

const (
	// qwenpawWarmHandshakeTimeout bounds the initialize / session/new /
	// session/load RPCs of a warm turn's first-turn setup. Generous: a cold
	// CLI can take seconds to boot. The prompt itself is not bounded by it —
	// the daemon's watchdogs supervise the turn through its own context.
	qwenpawWarmHandshakeTimeout = 30 * time.Second
	// qwenpawWarmExitGrace is how long the process gets to exit on its own
	// after stdin closes before the teardown escalates to the process group.
	qwenpawWarmExitGrace = 3 * time.Second
	// qwenpawWarmTerminateGrace is how long a SIGTERM'd process group gets
	// before the owned-context cancel kills it.
	qwenpawWarmTerminateGrace = 3 * time.Second
	// qwenpawWarmCancelGrace bounds the wait for the prompt to answer after
	// session/cancel; past it the process is considered unresponsive and
	// stopped.
	qwenpawWarmCancelGrace = 5 * time.Second
	// qwenpawWarmDrainGrace bounds the trailing-notification drain after a
	// prompt response (the one-shot path's unbounded process-exit wait,
	// bounded here because the process stays parked).
	qwenpawWarmDrainGrace = 2 * time.Second
)

// qwenpawWarmExitGraceNanos and qwenpawWarmTerminateGraceNanos let tests
// shorten the teardown graces — the same relief opencodeTerminateGraceNanos
// gives the opencode warm tests. They are atomic so tests can shorten a
// grace without racing the teardown goroutine that reads it. Zero means use
// the default.
var (
	qwenpawWarmExitGraceNanos      atomic.Int64
	qwenpawWarmTerminateGraceNanos atomic.Int64
)

func qwenpawWarmExitGraceDuration() time.Duration {
	if n := qwenpawWarmExitGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return qwenpawWarmExitGrace
}

func qwenpawWarmTerminateGraceDuration() time.Duration {
	if n := qwenpawWarmTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return qwenpawWarmTerminateGrace
}

// qwenpawACPNotify writes a JSON-RPC notification without an id (ACP
// session/cancel).
func qwenpawACPNotify(c *hermesClient, method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return c.writeLine(append(data, '\n'))
}

// qwenpawWarmArgs is the exact command line a pooled `qwenpaw acp` process
// is spawned with, built the same way the one-shot path builds it: the
// protocol subcommand, the surviving ExtraArgs then CustomArgs, and the
// daemon-owned --workspace. The list doubles as a fingerprint input — these
// flags are fixed for the life of the process, so a changed claim must not
// reuse it.
func (b *qwenpawBackend) qwenpawWarmArgs(opts ExecOptions) []string {
	args := []string{"acp"}
	args = append(args, filterCustomArgs(opts.ExtraArgs, qwenpawBlockedArgs, b.cfg.Logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, qwenpawBlockedArgs, b.cfg.Logger)...)
	if opts.QwenpawWorkspace != "" {
		args = append(args, "--workspace", opts.QwenpawWorkspace)
	}
	return args
}

// qwenpawWarmFingerprint hashes every session-immutable input of a pooled
// ACP process. ResumeSessionID is deliberately omitted (claude
// warmFingerprint's rule): it selects which conversation a process serves,
// not how the process was built. The env input is the launch environment the
// one-shot path would have used (buildEnv's filtered merge of the daemon
// environment), before the pooled overlay repointing — volatile keys and the
// overlay path itself are excluded by computeWarmSessionFingerprint.
func (b *qwenpawBackend) qwenpawWarmFingerprint(execPath string, opts ExecOptions, envMap map[string]string) string {
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:   "qwenpaw",
		Executable: execPath,
		Args:       b.qwenpawWarmArgs(opts),
		Cwd:        opts.Cwd,
		McpConfig:  opts.McpConfig,
		BriefHash:  opts.WarmPoolBriefHash,
		Env:        envMap,
	})
}

// qwenpawWarmLaunchEnv builds the launch environment the one-shot path would
// use (buildEnv over the task env), as a map — the fingerprint input and the
// base for the pooled overlay repointing.
func (b *qwenpawBackend) qwenpawWarmLaunchEnv() map[string]string {
	return qwenpawWarmEnvSliceToMap(buildEnv(b.cfg.Env))
}

// qwenpawWarmEnvSliceToMap keeps the last assignment of duplicated keys, the
// same resolution order buildEnv's append produces.
func qwenpawWarmEnvSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// executeWarmQwenpaw runs the turn through the warm pool: acquire a parked
// process or spawn a fresh pooled one, run the turn on it, then park or close
// it according to the result. The returned Session forwards the turn's
// message stream directly; its Result is synthesized here so the park
// decision happens before the caller learns the turn ended.
func (b *qwenpawBackend) executeWarmQwenpaw(ctx context.Context, execPath, prompt string, opts ExecOptions) (*Session, error) {
	proc, warmHit, err := b.acquireWarmQwenpaw(execPath, opts)
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
			parked = b.cfg.WarmPool.Park("qwenpaw", res.SessionID, proc)
		}
		if !parked {
			proc.Close()
		}
		b.cfg.Logger.Info("qwenpaw finished",
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

// acquireWarmQwenpaw checks out the process parked for opts.ResumeSessionID,
// refreshing its task-env overlay with this turn's credentials, or spawns a
// fresh pooled process.
func (b *qwenpawBackend) acquireWarmQwenpaw(execPath string, opts ExecOptions) (*qwenpawWarmProcess, bool, error) {
	envMap := b.qwenpawWarmLaunchEnv()
	fingerprint := b.qwenpawWarmFingerprint(execPath, opts, envMap)

	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("qwenpaw", opts.ResumeSessionID, fingerprint); acquired != nil {
			if proc, ok := acquired.(*qwenpawWarmProcess); ok {
				if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm qwenpaw: failed to refresh task env overlay; evicting", "error", err)
					proc.Close()
				} else {
					return proc, true, nil
				}
			} else {
				// Not ours; nothing of this shape should ever be parked under
				// the qwenpaw key. Close it rather than leak the process.
				acquired.Close()
			}
		}
	}

	proc, err := b.spawnWarmQwenpaw(execPath, opts, envMap, fingerprint)
	if err != nil {
		return nil, false, err
	}
	return proc, false, nil
}

// qwenpawWarmProcess is one pooled `qwenpaw acp` process holding a live ACP
// connection. The long-lived pieces (process, pipes, stdout reader, client)
// are created once at spawn; per-turn state travels through the bridge, so
// protocol callbacks always land on the turn that is actually prompting.
type qwenpawWarmProcess struct {
	cfg Config

	cmd         *exec.Cmd
	stdin       io.WriteCloser
	client      *hermesClient
	executable  string
	protocolCwd string

	// Pooled-process plumbing. launchEnv is the overlay-repointed child
	// environment; envMap is the pre-overlay launch environment and the
	// fingerprint input.
	sessionDir  string
	taskEnvFile string
	launchEnv   map[string]string

	fingerprint string

	mu        sync.Mutex
	sessionID string

	// bridge forwards protocol callbacks to the active turn. The client is
	// ungated (one-shot parity), so the bridge stays wired across turns and
	// every callback lands on whoever is prompting.
	bridgeMu         sync.Mutex
	bridgeMsgs       chan<- Message
	bridgeUsage      acpUsageSnapshot
	bridgeStopReason string
	bridgeOutput     strings.Builder
	bridgeSawText    bool
	bridgeSawTool    bool

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

func (p *qwenpawWarmProcess) currentSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionID
}

func (p *qwenpawWarmProcess) storeSessionID(id string) string {
	p.mu.Lock()
	p.sessionID = id
	p.mu.Unlock()
	return id
}

// Fingerprint implements warmPoolProcess: the launch identity this process
// was spawned under.
func (p *qwenpawWarmProcess) Fingerprint() string { return p.fingerprint }

// Alive implements warmPoolProcess: no teardown is on the floor and the
// stdout reader has not hit EOF (codexProcess's rule — reader EOF is the
// process-gone signal, and stop() owns the single cmd.Wait).
func (p *qwenpawWarmProcess) Alive() bool {
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

func (p *qwenpawWarmProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// Close terminates the process group and removes the session dir. It is
// idempotent. stdin gets the EOF window first before the group signal.
func (p *qwenpawWarmProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.stop(false)
		pid := 0
		if p.cmd.Process != nil {
			pid = p.cmd.Process.Pid
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
		p.cfg.Logger.Info("warm qwenpaw process closed", "pid", pid)
	})
	return true
}

// stop tears the process down exactly once; force skips the EOF window (an
// unresponsive runtime is gone within qwenpawWarmTerminateGrace). Shared by
// Close and the mid-turn teardown paths.
func (p *qwenpawWarmProcess) stop(force bool) {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		_ = p.stdin.Close()
		if !force {
			select {
			case <-p.readerDone:
			case <-time.After(qwenpawWarmExitGraceDuration()):
				force = true
			}
		}
		if force {
			signalProcessGroup(p.cmd, syscall.SIGTERM)
			select {
			case <-p.readerDone:
			case <-time.After(qwenpawWarmTerminateGraceDuration()):
			}
		}
		p.procCancel() // group kill; a no-op for a group that already exited
		_ = p.cmd.Wait()
		<-p.readerDone
		<-p.stderrDone
		releaseProcessGroup(p.cmd)
	})
}

// spawnWarmQwenpaw launches a pooled ACP process on its own context with the
// overlay-based credential plumbing. The spawn mirrors the one-shot path's
// (hideAgentWindow, owned process group) plus the pooled session dir: the
// task-env overlay file and a private temp dir that outlive the task that
// started the process.
func (b *qwenpawBackend) spawnWarmQwenpaw(execPath string, opts ExecOptions, envMap map[string]string, fingerprint string) (*qwenpawWarmProcess, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	protocolCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("qwenpaw working directory: %w", err)
	}

	sessionDir, err := os.MkdirTemp("", "multica-qwenpaw-warm-")
	if err != nil {
		return nil, fmt.Errorf("create warm qwenpaw session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("create warm qwenpaw temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("write warm qwenpaw task env overlay: %w", err)
	}
	// The overlay repointing strips the volatile keys from the static child
	// environment, adds the overlay pointer, and moves TMPDIR onto the
	// session dir so per-task temp cleanup cannot pull the rug from under a
	// parked process.
	launchEnv := PreparePooledChildEnv(envMap, taskEnvFile, sessionTempDir)

	args := b.qwenpawWarmArgs(opts)
	p := &qwenpawWarmProcess{
		cfg:         b.cfg,
		executable:  execPath,
		protocolCwd: protocolCwd,
		sessionDir:  sessionDir,
		taskEnvFile: taskEnvFile,
		launchEnv:   launchEnv,
		fingerprint: fingerprint,
		activity:    make(chan struct{}, 1),
	}

	procCtx, procCancel := context.WithCancel(context.Background())
	p.procCancel = procCancel
	cmd := b.cfg.commandAt(execPath).exec(procCtx, args...)
	hideAgentWindow(cmd)
	cmd.Dir = protocolCwd
	cmd.Env = buildEnv(launchEnv)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "acp")))

	fail := func(err error) (*qwenpawWarmProcess, error) {
		procCancel()
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("qwenpaw stdout pipe: %w", err))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("qwenpaw stdin pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(fmt.Errorf("qwenpaw stderr pipe: %w", err))
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fail(fmt.Errorf("start qwenpaw: %w", err))
	}
	p.cmd = cmd
	p.stdin = stdin
	b.cfg.Logger.Info("warm qwenpaw spawned", "pid", cmd.Process.Pid, "cwd", protocolCwd)

	p.providerErr = newACPProviderErrorSniffer("qwenpaw")
	p.stderrDone = make(chan struct{})
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[qwenpaw:stderr] "), p.providerErr), stderr)
	}()

	// Ungated client, one-shot parity: QwenPaw's tool names are already in
	// the standard format (no kimi title mapping), and every notification is
	// processed — a resumed session's replay lands in the turn output exactly
	// as it does on the one-shot path. onActivity only feeds the warm turn's
	// drain window; it gates nothing.
	p.client = &hermesClient{
		cfg:     b.cfg,
		stdin:   stdin,
		pending: make(map[int]*pendingRPC),
		onActivity: func() {
			select {
			case p.activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(msg Message) {
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
		// reads back through the bridge right after the request returns.
		onPromptDone: func(result hermesPromptResult) {
			p.bridgeMu.Lock()
			p.bridgeUsage = result.usage
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
		p.client.closeAllPending(errors.New("qwenpaw process exited"))
	}()

	return p, nil
}

// qwenpawWarmSessionParams builds the session/new / session/load params,
// including the Coding Mode _meta the one-shot path sends when the cwd is a
// real directory.
func (p *qwenpawWarmProcess) qwenpawWarmSessionParams(resume bool, sessionID string, mcpServers []any) map[string]any {
	var params map[string]any
	if resume {
		params = map[string]any{
			"cwd":        p.protocolCwd,
			"sessionId":  sessionID,
			"mcpServers": mcpServers,
		}
	} else {
		params = map[string]any{
			"cwd":        p.protocolCwd,
			"mcpServers": mcpServers,
		}
	}
	if p.protocolCwd != "." {
		params["_meta"] = map[string]any{
			"qwenpaw.coding_project_dir": p.protocolCwd,
		}
	}
	return params
}

// runTurn executes one turn on the live process and returns a turn-scoped
// Session whose Result carries the outcome. Setup failures land in the Result
// too (with the session id and ResumeRejected semantics the one-shot path
// reports, so the daemon's retry can resume cold); only a process that cannot
// take the turn at all comes back as a Go error. The process is left for the
// caller (executeWarmQwenpaw) to park or close — except on setup paths that
// cannot produce a session, which tear it down here.
func (p *qwenpawWarmProcess) runTurn(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	p.turnMu.Lock()
	if !p.Alive() {
		p.turnMu.Unlock()
		return nil, errors.New("warm qwenpaw process is not running")
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	startTime := time.Now()
	firstTurn := p.currentSessionID() == ""
	sessionID := p.currentSessionID()

	// The turn's execution budget covers setup and prompt together — the
	// one-shot path applies opts.Timeout to the whole process lifetime, so a
	// warm first turn must not buy a fresh budget after the handshake; the
	// handshake keeps its own shorter cap on top of it (below). A turn whose
	// budget runs out mid-setup reports timeout and tears the process down —
	// never a late success that could be parked.
	turnCtx, cancelTurn := runContext(ctx, opts.Timeout)
	turnOwned := false // flipped once the turn goroutine owns cancelTurn
	defer func() {
		if !turnOwned {
			cancelTurn()
		}
	}()

	if firstTurn {
		// First turn on this process: handshake and open the conversation.
		// The MCP set is process-scoped and fixed at session start.
		result := func(status, errText string, sid string, rejected bool) *Session {
			p.turnMu.Unlock()
			close(msgCh)
			resCh <- Result{Status: status, Error: errText, SessionID: sid, ResumeRejected: rejected, DurationMs: time.Since(startTime).Milliseconds()}
			return &Session{Messages: msgCh, Result: resCh}
		}

		// Fail closed on malformed JSON so the launch surfaces the real error
		// instead of silently dropping every MCP server.
		mcpServers, err := buildACPMcpServers(opts.McpConfig, p.cfg.Logger)
		if err != nil {
			p.turnMu.Unlock()
			p.Close()
			return nil, fmt.Errorf("qwenpaw: invalid mcp_config: %w", err)
		}

		// The setup RPCs sit under two budgets: the turn's execution deadline
		// (above) and this shorter handshake cap. The process is never
		// cancelled through this context — it outlives the turn. A runtime
		// that stops reading holds the synchronous setup writes (a large
		// session/new MCP payload, a slow handshake), so the timeout closes
		// stdin to release them. The callback stays armed across the WHOLE
		// setup — initialize, session/new|load — not just the first RPC: a
		// turn cancelled mid-setup must not wedge on a blocked session/new
		// write. Every failure path below tears the process down anyway; the
		// callback firing there is a no-op.
		setupCtx, cancelSetup := context.WithTimeout(turnCtx, qwenpawWarmHandshakeTimeout)
		defer cancelSetup()
		stopBlockedSetup := context.AfterFunc(setupCtx, func() { _ = p.stdin.Close() })
		init, err := p.client.request(setupCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "multica-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			p.Close()
			return result("failed", fmt.Sprintf("qwenpaw initialize failed: %v", err), "", false), nil
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime tanks
		// the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(init), "qwenpaw", p.cfg)

		if opts.ResumeSessionID != "" {
			loaded, err := p.client.request(setupCtx, "session/load", p.qwenpawWarmSessionParams(true, opts.ResumeSessionID, mcpServers))
			if err != nil {
				// One-shot parity: a bare failure with the not-found shape
				// sets ResumeRejected (curable by a fresh session); other
				// failures stay plain failures.
				errText := fmt.Sprintf("qwenpaw session/load failed: %v", err)
				rejected := setupCtx.Err() == nil && isACPSessionNotFound(err)
				p.Close()
				return result("failed", errText, "", rejected), nil
			}
			resolved, changed := resolveResumedSessionID(opts.ResumeSessionID, loaded)
			if changed {
				p.cfg.Logger.Warn("qwenpaw returned a different session id on resume; continuing with the new id",
					"requested", opts.ResumeSessionID, "actual", resolved)
			}
			sessionID = p.storeSessionID(resolved)
		} else {
			created, err := p.client.request(setupCtx, "session/new", p.qwenpawWarmSessionParams(false, "", mcpServers))
			if err != nil {
				switch setupCtx.Err() {
				case context.DeadlineExceeded:
					p.Close()
					return result("timeout", fmt.Sprintf("qwenpaw timed out during session/new: %v", opts.Timeout), "", false), nil
				case context.Canceled:
					p.Close()
					return result("aborted", fmt.Sprintf("qwenpaw aborted: %v", err), "", false), nil
				}
				p.Close()
				return result("failed", fmt.Sprintf("qwenpaw session/new failed: %v", err), "", false), nil
			}
			if sessionID = extractACPSessionID(created); sessionID == "" {
				p.Close()
				return result("failed", "qwenpaw session/new returned no session ID", "", false), nil
			}
			p.storeSessionID(sessionID)
		}
		p.client.sessionID = sessionID
		// Setup is done: the process now has its session, and the prompt's
		// own write window is guarded by the prompt context's callback below.
		stopBlockedSetup()

		p.cfg.Logger.Info("warm qwenpaw session ready", "session_id", sessionID, "resumed", opts.ResumeSessionID != "")
	}

	// The bridge is wired before the goroutine so the first stream frames
	// cannot slip past it. The client is ungated, so a resumed session's
	// replay flows into this turn's output exactly as on the one-shot path.
	p.bridgeMu.Lock()
	p.bridgeMsgs = msgCh
	p.bridgeUsage = acpUsageSnapshot{}
	p.bridgeStopReason = ""
	p.bridgeOutput.Reset()
	p.bridgeSawText = false
	p.bridgeSawTool = false
	p.bridgeMu.Unlock()

	// The usage accumulator is process-scoped; this turn must not inherit a
	// previous turn's usage_update maxima. Reset happens under turnMu, before
	// the prompt goes out.
	p.client.usageMu.Lock()
	p.client.usage = acpUsageAccumulator{}
	p.client.usageMu.Unlock()

	trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

	// NOTE: the bridge is NOT cleared when runTurn returns — the turn's
	// protocol messages keep arriving after the caller takes the Session and
	// must keep flowing into its channel and output builder. The turn
	// goroutine clears the bridge when the result is decided; turnMu plus one
	// turn at a time per process keep turns ordered.

	// The runtime brief rides the first prompt of a process only: a live
	// session already carries it in the conversation history, and re-sending
	// it every turn would repeat it (the one-shot path prepends per process,
	// and each of its processes lives exactly one turn).
	turnOwned = true
	go func() {
		defer cancelTurn()
		defer p.turnMu.Unlock()
		finalStatus := "completed"
		finalError := ""
		resumeRejected := false
		p.promptSent.Store(false)
		// The prompt shares the turn's budget — setup time already spent
		// counts against opts.Timeout (the one-shot path's whole-process
		// deadline), so a slow handshake cannot buy the prompt a fresh one.
		promptCtx := turnCtx
		stopBlockedWrite := context.AfterFunc(promptCtx, func() {
			if !p.promptSent.Load() {
				_ = p.stdin.Close()
			}
		})

		userText := prompt
		if firstTurn && opts.SystemPrompt != "" {
			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		type promptOutcome struct{ err error }
		promptCh := make(chan promptOutcome, 1)
		go func() {
			_, err := p.client.requestAndNotifySent(context.Background(), "session/prompt", map[string]any{
				"sessionId": sessionID,
				"prompt":    []map[string]any{{"type": "text", "text": userText}},
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
				p.cfg.Logger.Warn("qwenpaw: turn ended before the prompt was delivered; stopping the process")
				break
			}
			// Ask QwenPaw to stop the turn; it aborts the running tool and
			// answers the prompt. The write runs on its own goroutine so a
			// stalled pipe cannot hold the teardown, and the grace starts now.
			go func() {
				if err := qwenpawACPNotify(p.client, "session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
					p.cfg.Logger.Warn("qwenpaw: session/cancel could not be sent", "error", err)
				}
			}()
			select {
			case out := <-promptCh:
				promptErr, answered = out.err, true
			case <-time.After(qwenpawWarmCancelGrace):
				p.cfg.Logger.Warn("qwenpaw did not answer session/cancel in time; stopping the process", "grace", qwenpawWarmCancelGrace.String())
			}
		}
		stopBlockedWrite()

		var pr hermesPromptResult
		havePrompt := false
		if answered && promptErr == nil {
			p.bridgeMu.Lock()
			pr = hermesPromptResult{stopReason: p.bridgeStopReason, usage: p.bridgeUsage}
			p.bridgeMu.Unlock()
			havePrompt = true
		}

		switch {
		case promptCtx.Err() == context.DeadlineExceeded:
			finalStatus, finalError = "timeout", fmt.Sprintf("qwenpaw timed out after %s", opts.Timeout)
		case promptCtx.Err() == context.Canceled:
			finalStatus, finalError = "aborted", "execution cancelled"
		case promptErr != nil:
			finalStatus, finalError = "failed", fmt.Sprintf("qwenpaw session/prompt failed: %v", promptErr)
			// One-shot parity: a runtime that refuses the resumed session at
			// prompt time clears the id and reports ResumeRejected so the
			// daemon retries fresh; an empty session id never parks.
			if opts.ResumeSessionID != "" && isACPSessionNotFound(promptErr) {
				p.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
					"backend", "qwenpaw",
					"session_id", sessionID,
				)
				sessionID = ""
				resumeRejected = true
			}
		case havePrompt && pr.stopReason == "cancelled":
			// One-shot parity: a cancelled stop reason is logged, not
			// promoted to an aborted turn.
			p.cfg.Logger.Info("qwenpaw prompt cancelled", "stopReason", pr.stopReason, "duration", time.Since(startTime).Round(time.Millisecond).String())
		}
		if answered && promptCtx.Err() == nil {
			// Warm-specific drain: the one-shot path waits for process exit,
			// which a parked process cannot do; the shared quiet window still
			// lets a prompt response's trailing updates reach this turn.
			waitForACPNotificationQuiescence(promptCtx, p.activity, p.readerDone, acpNotificationQuietTime, qwenpawWarmDrainGrace)
		}
		if !answered {
			// A turn QwenPaw never answered leaves the process unresponsive
			// (its stdin write may still be blocked): force it down now. The
			// wrapper's later Close is idempotent.
			p.stop(true)
		}
		p.providerErr.Finalize()

		p.bridgeMu.Lock()
		finalOutput := p.bridgeOutput.String()
		sawText, sawTool := p.bridgeSawText, p.bridgeSawTool
		p.bridgeMu.Unlock()
		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, p.providerErr)

		// Terminal usage = this turn's usage_update snapshots + the prompt
		// result's usage, reconciled with the shared max consolidation (the
		// accumulator was reset at turn start, so no prior turn can leak in).
		if havePrompt {
			p.client.mergeUsage(pr.usage)
		}
		u := p.client.accumulatedUsage()

		var usageMap map[string]TokenUsage
		if acpUsagePresent(u) {
			// QwenPaw's model selection is unsupported — the backend never
			// sends opts.Model to the agent. Always attribute usage to
			// "unknown" rather than a model that was never applied.
			usageMap = map[string]TokenUsage{"unknown": u}
		}

		// Warm-pool invariant (the one-shot path has no counterpart): a turn
		// that closes cleanly with no text, no tool call and no usage at all
		// never reached the provider, and must not be reported — or parked —
		// as a success.
		if finalStatus == "completed" && usageMap == nil && !sawText && !sawTool {
			finalStatus = "failed"
			finalError = "qwenpaw turn ended with no text, no tool call and no reported usage — the provider produced nothing"
		}

		p.bridgeMu.Lock()
		if p.bridgeMsgs == msgCh {
			p.bridgeMsgs = nil
			p.bridgeUsage = acpUsageSnapshot{}
			p.bridgeStopReason = ""
			p.bridgeOutput.Reset()
			p.bridgeSawText = false
			p.bridgeSawTool = false
		}
		p.bridgeMu.Unlock()
		close(msgCh)

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     time.Since(startTime).Milliseconds(),
			SessionID:      sessionID,
			Usage:          usageMap,
			ResumeRejected: resumeRejected,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}
