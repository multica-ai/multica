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

// MiniMax Code (mcode) over a warm ACP process (`mcode acp` kept alive
// across turns).
//
// The WarmSessionPool pattern is the one codex_process.go, claude_process.go
// and devin_warm.go use: a turn either checks out the process parked for its
// session id (the fingerprint of every session-immutable launch input must
// match) or spawns a fresh pooled process, and a turn that completed cleanly
// parks the process again. MCode's ACP transport makes this natural — the
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
//     (args, cwd, MCP config, brief hash, non-volatile env), so a changed
//     claim evicts the process instead of silently serving the turn with the
//     previous configuration.
//
// MCode-specific shape, kept at parity with the one-shot Execute (mcode.go):
//
//   - No model, auth or effort setup; the public ACP surface declares
//     loadSession:false, so a warm-miss cold fallback always answers a resume
//     with the same typed rejection the one-shot path returns — which is
//     exactly the cost the warm pool exists to avoid. If a later MCode
//     version advertises loadSession, the standard session/load path starts
//     working without a compatibility guess.
//   - No SystemPrompt: the one-shot path ignores it (MCode reads the
//     cwd-scoped AGENTS.md), and so does the warm path on every turn.
//   - The mcodeRequestFailure classification and the pre-session startup
//     delay stay on the warm first turn.
//   - The hermesClient usage accumulator is per-process, so each turn resets
//     it before prompting.

const (
	// mcodeWarmHandshakeTimeout bounds the initialize / session/new /
	// session/load RPCs of a warm turn's first-turn setup. Generous: a cold
	// CLI can take seconds to boot. The prompt itself is not bounded by it —
	// the daemon's watchdogs supervise the turn through its own context.
	mcodeWarmHandshakeTimeout = 30 * time.Second
	// mcodeWarmExitGrace is how long the process gets to exit on its own
	// after stdin closes before the teardown escalates to the process group.
	mcodeWarmExitGrace = 3 * time.Second
	// mcodeWarmTerminateGrace is how long a SIGTERM'd process group gets
	// before the owned-context cancel SIGKILLs it.
	mcodeWarmTerminateGrace = 3 * time.Second
	// mcodeWarmCancelGrace bounds the wait for the prompt to answer after
	// session/cancel; past it the process is considered unresponsive and
	// stopped.
	mcodeWarmCancelGrace = 5 * time.Second
)

// mcodeWarmExitGraceNanos and mcodeWarmTerminateGraceNanos let tests shorten
// the teardown graces — the same relief opencodeTerminateGraceNanos gives the
// opencode warm tests. They are atomic so tests can shorten a grace without
// racing the teardown goroutine that reads it. Zero means use the default.
var (
	mcodeWarmExitGraceNanos      atomic.Int64
	mcodeWarmTerminateGraceNanos atomic.Int64
)

func mcodeWarmExitGraceDuration() time.Duration {
	if n := mcodeWarmExitGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return mcodeWarmExitGrace
}

func mcodeWarmTerminateGraceDuration() time.Duration {
	if n := mcodeWarmTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return mcodeWarmTerminateGrace
}

// mcodeACPNotify writes a JSON-RPC notification without an id (ACP
// session/cancel).
func mcodeACPNotify(c *hermesClient, method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return c.writeLine(append(data, '\n'))
}

// mcodeWarmArgs is the exact command line a pooled `mcode acp` process is
// spawned with, built the same way the one-shot path builds it: the protocol
// subcommand, then the surviving ExtraArgs and CustomArgs. The list doubles
// as a fingerprint input — these flags are fixed for the life of the process,
// so a changed claim must not reuse it.
func (b *mcodeBackend) mcodeWarmArgs(opts ExecOptions) []string {
	args := []string{"acp"}
	args = append(args, filterCustomArgs(opts.ExtraArgs, mcodeBlockedArgs, b.cfg.Logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, mcodeBlockedArgs, b.cfg.Logger)...)
	return args
}

// mcodeWarmFingerprint hashes every session-immutable input of a pooled ACP
// process. ResumeSessionID is deliberately omitted (claude warmFingerprint's
// rule): it selects which conversation a process serves, not how the process
// was built. The env input is the launch environment the one-shot path would
// have used (buildEnv's filtered merge of the daemon environment), before the
// pooled overlay repointing — volatile keys and the overlay path itself are
// excluded by computeWarmSessionFingerprint.
func (b *mcodeBackend) mcodeWarmFingerprint(execPath string, opts ExecOptions, envMap map[string]string) string {
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:   "mcode",
		Executable: execPath,
		Args:       b.mcodeWarmArgs(opts),
		Cwd:        opts.Cwd,
		McpConfig:  opts.McpConfig,
		BriefHash:  opts.WarmPoolBriefHash,
		Env:        envMap,
	})
}

// mcodeWarmLaunchEnv builds the launch environment the one-shot path would
// use (buildEnv over the task env), as a map — the fingerprint input and the
// base for the pooled overlay repointing.
func (b *mcodeBackend) mcodeWarmLaunchEnv() map[string]string {
	return mcodeWarmEnvSliceToMap(buildEnv(b.cfg.Env))
}

// mcodeWarmEnvSliceToMap keeps the last assignment of duplicated keys, the
// same resolution order buildEnv's append produces.
func mcodeWarmEnvSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// executeWarmMcode runs the turn through the warm pool: acquire a parked
// process or spawn a fresh pooled one, run the turn on it, then park or close
// it according to the result. The returned Session forwards the turn's
// message stream directly; its Result is synthesized here so the park
// decision happens before the caller learns the turn ended.
func (b *mcodeBackend) executeWarmMcode(ctx context.Context, execPath, prompt string, opts ExecOptions) (*Session, error) {
	proc, warmHit, err := b.acquireWarmMcode(execPath, opts)
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
			parked = b.cfg.WarmPool.Park("mcode", res.SessionID, proc)
		}
		if !parked {
			proc.Close()
		}
		b.cfg.Logger.Info("mcode finished",
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

// acquireWarmMcode checks out the process parked for opts.ResumeSessionID,
// refreshing its task-env overlay with this turn's credentials, or spawns a
// fresh pooled process.
func (b *mcodeBackend) acquireWarmMcode(execPath string, opts ExecOptions) (*mcodeWarmProcess, bool, error) {
	envMap := b.mcodeWarmLaunchEnv()
	fingerprint := b.mcodeWarmFingerprint(execPath, opts, envMap)

	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("mcode", opts.ResumeSessionID, fingerprint); acquired != nil {
			if proc, ok := acquired.(*mcodeWarmProcess); ok {
				if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm mcode: failed to refresh task env overlay; evicting", "error", err)
					proc.Close()
				} else {
					return proc, true, nil
				}
			} else {
				// Not ours; nothing of this shape should ever be parked under
				// the mcode key. Close it rather than leak the process.
				acquired.Close()
			}
		}
	}

	proc, err := b.spawnWarmMcode(execPath, opts, envMap, fingerprint)
	if err != nil {
		return nil, false, err
	}
	return proc, false, nil
}

// mcodeWarmProcess is one pooled `mcode acp` process holding a live ACP
// connection. The long-lived pieces (process, pipes, stdout reader, client)
// are created once at spawn; per-turn state travels through the bridge, so
// protocol callbacks always land on the turn that is actually prompting.
type mcodeWarmProcess struct {
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

	// bridge forwards protocol callbacks to the active turn.
	bridgeMu         sync.Mutex
	bridgeMsgs       chan<- Message
	bridgeUsage      acpUsageSnapshot
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

func (p *mcodeWarmProcess) currentSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionID
}

func (p *mcodeWarmProcess) storeSessionID(id string) string {
	p.mu.Lock()
	p.sessionID = id
	p.mu.Unlock()
	return id
}

// Fingerprint implements warmPoolProcess: the launch identity this process
// was spawned under.
func (p *mcodeWarmProcess) Fingerprint() string { return p.fingerprint }

// Alive implements warmPoolProcess: no teardown is on the floor and the
// stdout reader has not hit EOF (codexProcess's rule — reader EOF is the
// process-gone signal, and stop() owns the single cmd.Wait).
func (p *mcodeWarmProcess) Alive() bool {
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

func (p *mcodeWarmProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// Close terminates the process group and removes the session dir. It is
// idempotent. stdin gets the EOF window first before the group signal; the
// owned-context cancel keeps the one-shot path's SIGKILL-group fallback for
// a runtime that ignores both.
func (p *mcodeWarmProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.stop(false)
		pid := 0
		if p.cmd.Process != nil {
			pid = p.cmd.Process.Pid
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
		p.cfg.Logger.Info("warm mcode process closed", "pid", pid)
	})
	return true
}

// stop tears the process down exactly once; force skips the EOF window (an
// unresponsive runtime is gone within mcodeWarmTerminateGrace). Shared by
// Close and the mid-turn teardown paths.
func (p *mcodeWarmProcess) stop(force bool) {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		_ = p.stdin.Close()
		if !force {
			select {
			case <-p.readerDone:
			case <-time.After(mcodeWarmExitGraceDuration()):
				force = true
			}
		}
		if force {
			signalProcessGroup(p.cmd, syscall.SIGTERM)
			select {
			case <-p.readerDone:
			case <-time.After(mcodeWarmTerminateGraceDuration()):
			}
		}
		// The one-shot path's cmd.Cancel SIGKILLs the whole group; a group
		// that already exited makes this a no-op.
		p.procCancel()
		_ = p.cmd.Wait()
		<-p.readerDone
		<-p.stderrDone
		releaseProcessGroup(p.cmd)
	})
}

// spawnWarmMcode launches a pooled ACP process on its own context with the
// overlay-based credential plumbing. The spawn mirrors the one-shot path's
// (hideAgentWindow, owned process group, SIGKILL-group Cancel, WaitDelay)
// plus the pooled session dir: the task-env overlay file and a private temp
// dir that outlive the task that started the process.
func (b *mcodeBackend) spawnWarmMcode(execPath string, opts ExecOptions, envMap map[string]string, fingerprint string) (*mcodeWarmProcess, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	protocolCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("mcode working directory: %w", err)
	}

	sessionDir, err := os.MkdirTemp("", "multica-mcode-warm-")
	if err != nil {
		return nil, fmt.Errorf("create warm mcode session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("create warm mcode temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("write warm mcode task env overlay: %w", err)
	}
	// The overlay repointing strips the volatile keys from the static child
	// environment, adds the overlay pointer, and moves TMPDIR onto the
	// session dir so per-task temp cleanup cannot pull the rug from under a
	// parked process.
	launchEnv := PreparePooledChildEnv(envMap, taskEnvFile, sessionTempDir)

	args := b.mcodeWarmArgs(opts)
	p := &mcodeWarmProcess{
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
	// One-shot parity: MCode or a tool descendant may ignore stdin EOF, so
	// the context cancel escalates straight to the process-group SIGKILL,
	// and WaitDelay bounds the pipe wait after it.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = mcodeReaderDrainGrace
	cmd.Dir = protocolCwd
	cmd.Env = buildEnv(launchEnv)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "acp")))

	fail := func(err error) (*mcodeWarmProcess, error) {
		procCancel()
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("mcode stdout pipe: %w", err))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("mcode stdin pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(fmt.Errorf("mcode stderr pipe: %w", err))
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fail(fmt.Errorf("start mcode: %w", err))
	}
	p.cmd = cmd
	p.stdin = stdin
	b.cfg.Logger.Info("warm mcode spawned", "pid", cmd.Process.Pid, "cwd", protocolCwd)

	p.providerErr = newACPProviderErrorSniffer("mcode")
	p.stderrDone = make(chan struct{})
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[mcode:stderr] "), p.providerErr), stderr)
	}()

	p.client = &hermesClient{
		cfg:     b.cfg,
		stdin:   stdin,
		pending: make(map[int]*pendingRPC),
		// session/load replays the conversation as session/update; only the
		// turn this process is prompting is that turn's transcript.
		acceptNotification: func(string) bool { return p.streaming.Load() },
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
		p.client.closeAllPending(errMcodeProcessExited)
	}()

	return p, nil
}

// runTurn executes one turn on the live process and returns a turn-scoped
// Session whose Result carries the outcome. Setup failures land in the Result
// too (with the session id and ResumeRejected semantics the one-shot path
// reports, so the daemon's retry can resume cold); only a process that cannot
// take the turn at all comes back as a Go error. The process is left for the
// caller (executeWarmMcode) to park or close — except on setup paths that
// cannot produce a session, which tear it down here.
func (p *mcodeWarmProcess) runTurn(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	p.turnMu.Lock()
	if !p.Alive() {
		p.turnMu.Unlock()
		return nil, errors.New("warm mcode process is not running")
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
			return nil, fmt.Errorf("mcode: invalid mcp_config: %w", err)
		}

		// The setup RPCs sit under two budgets: the turn's execution deadline
		// (above) and this shorter handshake cap. The process is never
		// cancelled through this context — it outlives the turn. A runtime
		// that stops reading holds the synchronous setup writes (a large
		// session/new MCP payload, a slow handshake), so the timeout closes
		// stdin to release them. The callback stays armed across the WHOLE
		// setup — initialize and session/new|load — not just the first RPC: a
		// turn cancelled mid-setup must not wedge on a blocked session/new
		// write. Every failure path below tears the process down anyway; the
		// callback firing there is a no-op.
		setupCtx, cancelSetup := context.WithTimeout(turnCtx, mcodeWarmHandshakeTimeout)
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
			status, errText := mcodeRequestFailure(setupCtx, opts.Timeout, "initialize", err)
			p.Close()
			return result(status, errText, "", false), nil
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime tanks
		// the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(init), "mcode", p.cfg)

		if opts.ResumeSessionID != "" {
			// One-shot parity: the public MCode ACP surface declares
			// loadSession:false, so a resume answers with the same typed
			// rejection — exactly the cold-start cost a warm hit avoids.
			if !mcodeLoadSessionSupported(init) {
				p.Close()
				return result("failed", "mcode ACP does not support session loading; retry with a fresh session", "", true), nil
			}
			if err := mcodeWaitForSessionStartup(setupCtx); err != nil {
				status, errText := mcodeRequestFailure(setupCtx, opts.Timeout, "session/load", err)
				p.Close()
				return result(status, errText, "", false), nil
			}
			loaded, err := p.client.request(setupCtx, "session/load", map[string]any{
				"cwd":        p.protocolCwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText := mcodeRequestFailure(setupCtx, opts.Timeout, "session/load", err)
				rejected := status == "failed" && setupCtx.Err() == nil && isACPSessionNotFound(err)
				p.Close()
				return result(status, errText, "", rejected), nil
			}
			resolved, _ := resolveResumedSessionID(opts.ResumeSessionID, loaded)
			sessionID = p.storeSessionID(resolved)
		} else {
			if err := mcodeWaitForSessionStartup(setupCtx); err != nil {
				status, errText := mcodeRequestFailure(setupCtx, opts.Timeout, "session/new", err)
				p.Close()
				return result(status, errText, "", false), nil
			}
			created, err := p.client.request(setupCtx, "session/new", map[string]any{
				"cwd":        p.protocolCwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText := mcodeRequestFailure(setupCtx, opts.Timeout, "session/new", err)
				p.Close()
				return result(status, errText, "", false), nil
			}
			if sessionID = extractACPSessionID(created); sessionID == "" {
				p.Close()
				return result("failed", "mcode session/new returned no session ID", "", false), nil
			}
			p.storeSessionID(sessionID)
		}
		p.client.sessionID = sessionID
		// Setup is done: the process now has its session, and the prompt's
		// own write window is guarded by the prompt context's callback below.
		stopBlockedSetup()

		p.cfg.Logger.Info("warm mcode session ready", "session_id", sessionID, "resumed", opts.ResumeSessionID != "")
	}

	// The history replay (if session/load sent one) has been answered; from
	// here the stream belongs to the prompted turn. The bridge is wired
	// before the goroutine so the first stream frames cannot slip past it.
	p.bridgeMu.Lock()
	p.bridgeMsgs = msgCh
	p.bridgeUsage = acpUsageSnapshot{}
	p.bridgeStopReason = ""
	p.bridgeOutput.Reset()
	p.bridgeSawText = false
	p.bridgeSawTool = false
	p.bridgeMu.Unlock()

	// The usage accumulator is process-scoped; this turn must not inherit a
	// previous turn's usage_update maxima. Reset happens under turnMu with
	// streaming still down, so no notification can land between the reset
	// and the prompt.
	p.client.usageMu.Lock()
	p.client.usage = acpUsageAccumulator{}
	p.client.usageMu.Unlock()

	trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})

	// NOTE: the bridge is NOT cleared when runTurn returns — the turn's
	// protocol messages keep arriving after the caller takes the Session and
	// must keep flowing into its channel and output builder. The turn
	// goroutine clears the bridge when the result is decided; turnMu plus one
	// turn at a time per process keep turns ordered.

	// No SystemPrompt on any turn, one-shot parity: MCode reads the
	// cwd-scoped AGENTS.md and the one-shot path ignores the field.
	turnOwned = true
	go func() {
		defer cancelTurn()
		defer p.turnMu.Unlock()
		finalStatus := "completed"
		finalError := ""
		resumeRejected := false
		p.streaming.Store(true)
		// The prompt shares the turn's budget — setup time already spent
		// counts against opts.Timeout (the one-shot path's whole-process
		// deadline), so a slow handshake cannot buy the prompt a fresh one.
		promptCtx := turnCtx
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
				p.cfg.Logger.Warn("mcode: turn ended before the prompt was delivered; stopping the process")
				break
			}
			// Ask MCode to stop the turn; it aborts the running tool and
			// answers the prompt. The write runs on its own goroutine so a
			// stalled pipe cannot hold the teardown, and the grace starts now.
			go func() {
				if err := mcodeACPNotify(p.client, "session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
					p.cfg.Logger.Warn("mcode: session/cancel could not be sent", "error", err)
				}
			}()
			select {
			case out := <-promptCh:
				promptErr, answered = out.err, true
			case <-time.After(mcodeWarmCancelGrace):
				p.cfg.Logger.Warn("mcode did not answer session/cancel in time; stopping the process", "grace", mcodeWarmCancelGrace.String())
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
			finalStatus, finalError = "timeout", fmt.Sprintf("mcode timed out during session/prompt after %s", opts.Timeout)
		case promptCtx.Err() == context.Canceled:
			finalStatus, finalError = "aborted", "execution cancelled"
		case promptErr != nil:
			finalStatus, finalError = mcodeRequestFailure(promptCtx, opts.Timeout, "session/prompt", promptErr)
			// The runtime refused the resumed session at prompt time. Clear
			// the id and report ResumeRejected so the daemon drops the stale
			// pointer and retries fresh; an empty session id never parks.
			if opts.ResumeSessionID != "" && isACPSessionNotFound(promptErr) {
				p.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
					"backend", "mcode",
					"session_id", sessionID,
				)
				sessionID = ""
				resumeRejected = true
			}
		case havePrompt && pr.stopReason == "cancelled":
			finalStatus, finalError = "aborted", "execution cancelled"
		case havePrompt && pr.stopReason == "max_turn_requests":
			finalStatus, finalError = "failed", "mcode reached its maximum turn requests"
		}
		if answered && promptCtx.Err() == nil {
			waitForACPNotificationQuiescence(promptCtx, p.activity, p.readerDone, acpNotificationQuietTime, mcodeReaderDrainGrace)
		}
		p.streaming.Store(false)
		if !answered {
			// A turn MCode never answered leaves the process unresponsive
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
		// Attributed to "unknown" — MCode has no session-scoped model
		// selection.
		if havePrompt {
			p.client.mergeUsage(pr.usage)
		}

		var usageMap map[string]TokenUsage
		if accumulated := p.client.accumulatedUsage(); acpUsagePresent(accumulated) {
			// Attributed to "unknown" — MCode has no session-scoped model
			// selection.
			usageMap = map[string]TokenUsage{"unknown": accumulated}
		}

		// Warm-pool invariant (the one-shot path has no counterpart): a turn
		// that closes cleanly with no text, no tool call and no usage at all
		// never reached the provider, and must not be reported — or parked —
		// as a success.
		if finalStatus == "completed" && usageMap == nil && !sawText && !sawTool {
			finalStatus = "failed"
			finalError = "mcode turn ended with no text, no tool call and no reported usage — the provider produced nothing"
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
