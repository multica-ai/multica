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

// Cursor over a warm ACP process (`cursor-agent acp` kept alive across turns).
//
// The WarmSessionPool pattern is the one codex_process.go, claude_process.go,
// qoder_warm.go and opencode_warm.go use: a turn either checks out the process
// parked for its session id (the fingerprint of every session-immutable launch
// input must match) or spawns a fresh pooled process, and a turn that completed
// cleanly parks the process again. Cursor's ACP transport makes this natural —
// the process already holds the live session, so a follow-up turn is another
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
//   - Session-scoped configuration (model, MCP set) is fixed when the session
//     opens; every input that could change it is part of the fingerprint, so
//     a changed claim evicts the process instead of silently serving the turn
//     with the previous configuration.
//
// Everything turn-shaped here mirrors the one-shot Execute (cursor.go) where
// the transports agree, and the shared ACP dialect where they diverge:
//
//   - The one-shot `-p` stream-json transport takes `--model`; the ACP
//     transport selects the model with `session/set_model` after the session
//     opens (the ACP handshake advertises a model catalog on session/new).
//     A claim that changes the model changes the fingerprint and gets its own
//     process, so the session's settled configuration is never silently
//     reused for a different claim.
//   - The runtime brief is not prepended to any prompt: cursor-agent loads
//     cwd-scoped context files (AGENTS.md, .cursor/skills/) itself, the same
//     rule the one-shot transport already runs on.
//   - The prompt result's usage snapshot is session-cumulative, so each turn
//     settles it as a delta against the previous turn's snapshot — resumed
//     sessions seed from the next known total without billing history again.
//   - Per-task launches that the ACP transport cannot carry (skill
//     invocations, steering supplements, custom launch args) stay on the
//     one-shot transport: the warm gate hands those claims to the cold path.

const (
	// cursorWarmHandshakeTimeout bounds the initialize / session/load /
	// session/new / set_model RPCs of a warm turn's first-turn setup.
	// Generous: a cold CLI can take seconds to boot. The prompt itself is not
	// bounded by it — the daemon's watchdogs supervise the turn through its
	// own context.
	cursorWarmHandshakeTimeout = 30 * time.Second
	// cursorWarmExitGrace is how long the process gets to exit on its own
	// after stdin closes before the teardown escalates to the process group.
	// Cursor's ACP server may keep worker processes that hold the inherited
	// fds, so the group signal matters (the one-shot path treats the terminal
	// result event as the protocol boundary and kills the tree for the same
	// reason).
	cursorWarmExitGrace = 3 * time.Second
	// cursorWarmTerminateGrace is how long a SIGTERM'd process group gets
	// before the owned-context cancel kills it.
	cursorWarmTerminateGrace = 3 * time.Second
	// cursorWarmCancelGrace bounds the wait for the prompt to answer after
	// session/cancel; past it the process is considered unresponsive and
	// stopped.
	cursorWarmCancelGrace = 5 * time.Second
)

// cursorWarmExitGraceNanos and cursorWarmTerminateGraceNanos let tests
// shorten the teardown graces — the same relief qoderWarmExitGraceNanos gives
// the qoder warm tests. They are atomic so tests can shorten a grace without
// racing the teardown goroutine that reads it. Zero means use the default.
var (
	cursorWarmExitGraceNanos      atomic.Int64
	cursorWarmTerminateGraceNanos atomic.Int64
)

func cursorWarmExitGraceDuration() time.Duration {
	if n := cursorWarmExitGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return cursorWarmExitGrace
}

func cursorWarmTerminateGraceDuration() time.Duration {
	if n := cursorWarmTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return cursorWarmTerminateGrace
}

// cursorWarmEligible reports whether a claim can take the warm transport.
// Claims whose per-task inputs the ACP launch cannot carry stay on the
// one-shot path, which owns the same behavior today:
//
//   - skill invocations build a one-shot prompt shape
//   - steering supplements inject a per-task hook environment the pooled
//     process must not inherit (and cannot refresh per turn)
//   - custom args are only validated against the `-p` transport's flag set
func cursorWarmEligible(opts ExecOptions) bool {
	return opts.SkillInvocation == nil &&
		!opts.EnableTaskSupplement &&
		len(opts.CustomArgs) == 0 &&
		len(opts.ExtraArgs) == 0
}

// cursorACPNotify writes a JSON-RPC notification without an id (ACP
// session/cancel).
func cursorACPNotify(c *hermesClient, method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return c.writeLine(append(data, '\n'))
}

// cursorWarmArgs is the exact command line a pooled Cursor ACP process is
// spawned with. The warm gate keeps per-task claims off this transport, so
// the launch line carries hard flags only.
func cursorWarmArgs() []string {
	return []string{"acp"}
}

// cursorWarmFingerprint hashes every session-immutable input of a pooled ACP
// process. ResumeSessionID is deliberately omitted (claude warmFingerprint's
// rule): it selects which conversation a process serves, not how the process
// was built. The env input is the launch environment the one-shot path would
// have used (buildEnv's filtered merge of the daemon environment), before the
// pooled overlay repointing — volatile keys and the overlay path itself are
// excluded by computeWarmSessionFingerprint.
func cursorWarmFingerprint(execPath string, opts ExecOptions, envMap map[string]string) string {
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:   "cursor",
		Executable: execPath,
		Args:       cursorWarmArgs(),
		Cwd:        opts.Cwd,
		Model:      opts.Model,
		McpConfig:  opts.McpConfig,
		BriefHash:  opts.WarmPoolBriefHash,
		Env:        envMap,
	})
}

// executeWarmCursor runs the turn through the warm pool: acquire a parked
// process or spawn a fresh pooled one, run the turn on it, then park or close
// it according to the result. The returned Session forwards the turn's
// message stream directly; its Result is synthesized here so the park
// decision happens before the caller learns the turn ended.
func (b *cursorBackend) executeWarmCursor(ctx context.Context, execPath, prompt string, opts ExecOptions) (*Session, error) {
	proc, warmHit, err := b.acquireWarmCursor(execPath, opts)
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
			parked = b.cfg.WarmPool.Park("cursor", res.SessionID, proc)
		}
		if !parked {
			proc.Close()
		}
		b.cfg.Logger.Info("cursor finished",
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

// acquireWarmCursor checks out the process parked for opts.ResumeSessionID,
// refreshing its task-env overlay with this turn's credentials, or spawns a
// fresh pooled process.
func (b *cursorBackend) acquireWarmCursor(execPath string, opts ExecOptions) (*cursorWarmProcess, bool, error) {
	envMap := cursorWarmEnvSliceToMap(buildEnv(b.cfg.Env))
	fingerprint := cursorWarmFingerprint(execPath, opts, envMap)

	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("cursor", opts.ResumeSessionID, fingerprint); acquired != nil {
			if proc, ok := acquired.(*cursorWarmProcess); ok {
				if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm cursor: failed to refresh task env overlay; evicting", "error", err)
					proc.Close()
				} else {
					return proc, true, nil
				}
			} else {
				// Not ours; nothing of this shape should ever be parked under
				// the cursor key. Close it rather than leak the process.
				acquired.Close()
			}
		}
	}

	proc, err := b.spawnWarmCursor(execPath, opts, envMap, fingerprint)
	if err != nil {
		return nil, false, err
	}
	return proc, false, nil
}

// cursorWarmEnvSliceToMap keeps the last assignment of duplicated keys, the
// same resolution order buildEnv's append produces.
func cursorWarmEnvSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// cursorWarmProcess is one pooled Cursor ACP process holding a live ACP
// connection. The long-lived pieces (process, pipes, stdout reader, client)
// are created once at spawn; per-turn state travels through the bridge, so
// protocol callbacks always land on the turn that is actually prompting.
type cursorWarmProcess struct {
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

	mu             sync.Mutex
	sessionID      string
	effectiveModel string

	// Cumulative-usage baseline: the prompt result's usage snapshot is
	// session-cumulative, so each turn settles it as a delta against the
	// previous turn's snapshot. A loaded session or an intervening missing
	// snapshot has no trusted baseline.
	usageMu        sync.Mutex
	lastUsage      *TokenUsage
	lastUsageKnown bool
	resumed        bool

	// bridge forwards protocol callbacks to the active turn. The deliverable
	// tracker accumulates Result.Output alongside the forwarded stream: the
	// deliverable is the text since the latest tool call — the one-shot
	// stream reader delivers the same selection, because joining every text
	// chunk leaks interim narration into the persisted chat reply.
	bridgeMu          sync.Mutex
	bridgeMsgs        chan<- Message
	bridgeDeliverable *acpDeliverableTracker
	bridgeUsage       acpUsageSnapshot
	bridgeModelID     string
	bridgeStopReason  string
	bridgeSawText     bool
	bridgeSawTool     bool
	streaming         atomic.Bool

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

func (p *cursorWarmProcess) currentSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionID
}

func (p *cursorWarmProcess) storeSessionID(id string) string {
	p.mu.Lock()
	p.sessionID = id
	p.mu.Unlock()
	return id
}

func (p *cursorWarmProcess) currentEffectiveModel() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.effectiveModel
}

func (p *cursorWarmProcess) storeEffectiveModel(model string) {
	p.mu.Lock()
	p.effectiveModel = model
	p.mu.Unlock()
}

// Fingerprint implements warmPoolProcess: the launch identity this process
// was spawned under.
func (p *cursorWarmProcess) Fingerprint() string { return p.fingerprint }

// Alive implements warmPoolProcess: no teardown is on the floor and the
// stdout reader has not hit EOF (codexProcess's rule — reader EOF is the
// process-gone signal, and stop() owns the single cmd.Wait).
func (p *cursorWarmProcess) Alive() bool {
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

func (p *cursorWarmProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// Close terminates the process group and removes the session dir. It is
// idempotent. stdin gets the EOF window first before the group signal — the
// group matters because Cursor's ACP server can keep worker processes holding
// the inherited fds.
func (p *cursorWarmProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.stop(false)
		pid := 0
		if p.cmd.Process != nil {
			pid = p.cmd.Process.Pid
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
		p.cfg.Logger.Info("warm cursor process closed", "pid", pid)
	})
	return true
}

// stop tears the process down exactly once; force skips the EOF window (an
// unresponsive runtime is gone within cursorWarmTerminateGrace). Shared by
// Close and the mid-turn teardown paths.
func (p *cursorWarmProcess) stop(force bool) {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		_ = p.stdin.Close()
		if !force {
			select {
			case <-p.readerDone:
			case <-time.After(cursorWarmExitGraceDuration()):
				force = true
			}
		}
		if force {
			signalProcessGroup(p.cmd, syscall.SIGTERM)
			select {
			case <-p.readerDone:
			case <-time.After(cursorWarmTerminateGraceDuration()):
			}
		}
		p.procCancel() // group kill; a no-op for a group that already exited
		_ = p.cmd.Wait()
		<-p.readerDone
		<-p.stderrDone
		releaseProcessGroup(p.cmd)
	})
}

// spawnWarmCursor launches a pooled ACP process on its own context with the
// overlay-based credential plumbing. The spawn mirrors the one-shot path's
// (hideAgentWindow, owned process group) plus the pooled session dir: the
// task-env overlay file and a private temp dir that outlive the task that
// started the process.
func (b *cursorBackend) spawnWarmCursor(execPath string, opts ExecOptions, envMap map[string]string, fingerprint string) (*cursorWarmProcess, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	protocolCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("cursor working directory: %w", err)
	}

	sessionDir, err := os.MkdirTemp("", "multica-cursor-warm-")
	if err != nil {
		return nil, fmt.Errorf("create warm cursor session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("create warm cursor temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("write warm cursor task env overlay: %w", err)
	}
	// The overlay repointing strips the volatile keys from the static child
	// environment, adds the overlay pointer, and moves TMPDIR onto the
	// session dir so per-task temp cleanup cannot pull the rug from under a
	// parked process.
	launchEnv := PreparePooledChildEnv(envMap, taskEnvFile, sessionTempDir)

	args := cursorWarmArgs()
	p := &cursorWarmProcess{
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

	fail := func(err error) (*cursorWarmProcess, error) {
		procCancel()
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("cursor stdout pipe: %w", err))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("cursor stdin pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(fmt.Errorf("cursor stderr pipe: %w", err))
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fail(fmt.Errorf("start cursor: %w", err))
	}
	p.cmd = cmd
	p.stdin = stdin
	b.cfg.Logger.Info("warm cursor spawned", "pid", cmd.Process.Pid, "cwd", protocolCwd, "model", opts.Model)

	p.providerErr = newACPProviderErrorSniffer("cursor")
	p.stderrDone = make(chan struct{})
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[cursor:stderr] "), p.providerErr), stderr)
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
			if deliverable := p.bridgeDeliverable; deliverable != nil {
				deliverable.observe(msg)
			}
			switch msg.Type {
			case MessageText:
				p.bridgeSawText = true
			case MessageToolUse:
				p.bridgeSawTool = true
			}
			if p.bridgeMsgs != nil {
				trySend(p.bridgeMsgs, msg)
			}
		},
		// extractPromptResult fires on the session/prompt RESPONSE: it parses
		// this turn's usage snapshot, billed model and stop reason, which the
		// turn goroutine reads back through the bridge right after the request
		// returns. The usage snapshot is session-cumulative; the turn
		// settles it against the previous turn's snapshot.
		onPromptDone: func(result hermesPromptResult) {
			p.bridgeMu.Lock()
			p.bridgeUsage = result.usage
			p.bridgeModelID = result.modelID
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
		p.client.closeAllPending(errors.New("cursor process exited"))
	}()

	return p, nil
}

// runTurn executes one turn on the live process and returns a turn-scoped
// Session whose Result carries the outcome. Setup failures land in the Result
// too (with the session id and ResumeRejected semantics the one-shot path
// reports, so the daemon's retry can resume cold); only a process that cannot
// take the turn at all comes back as a Go error. The process is left for the
// caller (executeWarmCursor) to park or close — except on setup paths that
// cannot produce a session, which tear it down here.
func (p *cursorWarmProcess) runTurn(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	p.turnMu.Lock()
	if !p.Alive() {
		p.turnMu.Unlock()
		return nil, errors.New("warm cursor process is not running")
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
			return nil, fmt.Errorf("cursor: invalid mcp_config: %w", err)
		}

		// The setup RPCs sit under two budgets: the turn's execution deadline
		// (above) and this shorter handshake cap. The process is never
		// cancelled through this context — it outlives the turn. A runtime
		// that stops reading holds the synchronous setup writes (a large
		// session/new MCP payload, a slow handshake), so the timeout closes
		// stdin to release them. The callback stays armed across the WHOLE
		// setup — initialize, session/load|new and set_model — not just the
		// first RPC: a turn cancelled mid-setup must not wedge on a blocked
		// session/new write. Every failure path below tears the process down
		// anyway; the callback firing there is a no-op.
		setupCtx, cancelSetup := context.WithTimeout(turnCtx, cursorWarmHandshakeTimeout)
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
			status, errText, _ := classifyACPResumeFailure(setupCtx, "cursor", "initialize", err, opts.Timeout, p.cfg.Logger)
			p.Close()
			return result(status, errText, "", false), nil
		}
		var negotiated struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if json.Unmarshal(init, &negotiated) != nil || negotiated.ProtocolVersion != 1 {
			p.Close()
			return result("failed", "cursor ACP: unsupported protocol version", "", false), nil
		}

		// Drop MCP entries whose remote transport the runtime didn't
		// advertise. See the matching comment in hermes.go for why
		// unconditionally sending http/sse to a stdio-only ACP runtime tanks
		// the whole session/new.
		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(init), "cursor", p.cfg)

		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			p.resumed = true
			loaded, err := p.client.request(setupCtx, "session/load", map[string]any{
				"cwd":        p.protocolCwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText, rejected := classifyACPResumeFailure(setupCtx, "cursor", "session/load", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, "", rejected), nil
			}
			resolved, changed := resolveResumedSessionID(opts.ResumeSessionID, loaded)
			if changed {
				p.cfg.Logger.Warn("cursor returned a different session id on resume; continuing with the new id",
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
				status, errText, _ := classifyACPResumeFailure(setupCtx, "cursor", "session/new", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, "", false), nil
			}
			if sessionID = extractACPSessionID(created); sessionID == "" {
				p.Close()
				return result("failed", "cursor session/new returned no session ID", "", false), nil
			}
			p.storeSessionID(sessionID)
			sessionResult = created
		}
		p.client.sessionID = sessionID

		if strings.TrimSpace(opts.Model) == "" {
			p.storeEffectiveModel(extractACPCurrentModelID(sessionResult))
		}

		// Session-scoped model selection, the ACP transport's counterpart of
		// the one-shot `--model` flag. Runs once per process; later turns keep
		// the session's settled configuration, and a claim that changes the
		// model changes the fingerprint and gets its own process.
		if model := strings.TrimSpace(opts.Model); model != "" {
			if _, err := p.client.request(setupCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   model,
			}); err != nil {
				p.cfg.Logger.Warn("cursor set_model failed", "error", err, "requested_model", model)
				status, errText, _ := classifyACPResumeFailure(setupCtx, "cursor", "set_model", err, opts.Timeout, p.cfg.Logger)
				rejected := setupCtx.Err() == nil && opts.ResumeSessionID != "" && isACPSessionNotFound(err)
				keep := sessionID
				if setupFailureWithholdsSessionID(opts) || rejected {
					keep = ""
				}
				p.Close()
				return result(status, errText, keep, rejected), nil
			}
			p.storeEffectiveModel(model)
			p.cfg.Logger.Info("cursor session model set", "model", model)
		}
		// Setup is done: the process now has its session, and the prompt's
		// own write window is guarded by the prompt context's callback below.
		stopBlockedSetup()

		p.cfg.Logger.Info("warm cursor session ready", "session_id", sessionID, "resumed", opts.ResumeSessionID != "")
	}

	// The history replay (if session/load sent one) has been answered; from
	// here the stream belongs to the prompted turn. The bridge is wired
	// before the goroutine so the first stream frames cannot slip past it.
	p.bridgeMu.Lock()
	p.bridgeMsgs = msgCh
	p.bridgeDeliverable = &acpDeliverableTracker{}
	p.bridgeUsage = acpUsageSnapshot{}
	p.bridgeModelID = ""
	p.bridgeStopReason = ""
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
				p.cfg.Logger.Warn("cursor: turn ended before the prompt was delivered; stopping the process")
				break
			}
			// Ask Cursor to stop the turn; it aborts the running tool and
			// answers the prompt. The write runs on its own goroutine so a
			// stalled pipe cannot hold the teardown, and the grace starts now.
			go func() {
				if err := cursorACPNotify(p.client, "session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
					p.cfg.Logger.Warn("cursor: session/cancel could not be sent", "error", err)
				}
			}()
			select {
			case out := <-promptCh:
				promptErr, answered = out.err, true
			case <-time.After(cursorWarmCancelGrace):
				p.cfg.Logger.Warn("cursor did not answer session/cancel in time; stopping the process", "grace", cursorWarmCancelGrace.String())
			}
		}
		stopBlockedWrite()

		var pr hermesPromptResult
		havePrompt := false
		if answered && promptErr == nil {
			p.bridgeMu.Lock()
			pr = hermesPromptResult{stopReason: p.bridgeStopReason, usage: p.bridgeUsage, modelID: p.bridgeModelID}
			p.bridgeMu.Unlock()
			havePrompt = true
		}

		switch {
		case promptCtx.Err() == context.DeadlineExceeded:
			finalStatus, finalError = "timeout", fmt.Sprintf("cursor timed out after %s", opts.Timeout)
		case promptCtx.Err() == context.Canceled:
			finalStatus, finalError = "aborted", "execution cancelled"
		case promptErr != nil:
			finalStatus, finalError = "failed", fmt.Sprintf("cursor session/prompt failed: %v", promptErr)
			// The runtime may echo the requested id from session/load and
			// only reject it at prompt time. Cold-path parity: clear the id
			// and report ResumeRejected so the daemon drops the stale pointer
			// and retries fresh; an empty session id never parks.
			if opts.ResumeSessionID != "" && isACPSessionNotFound(promptErr) {
				p.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
					"backend", "cursor",
					"session_id", sessionID,
				)
				sessionID = ""
				resumeRejected = true
			}
		case havePrompt && pr.stopReason != "" && pr.stopReason != "end_turn":
			switch pr.stopReason {
			case "cancelled":
				finalStatus, finalError = "aborted", "cursor cancelled the prompt"
			default:
				finalStatus, finalError = "failed", "cursor stopped: "+pr.stopReason
			}
		}
		if answered && promptCtx.Err() == nil {
			waitForACPNotificationQuiescence(promptCtx, p.activity, p.readerDone, acpNotificationQuietTime, opencodeACPDrainGrace)
		}
		p.streaming.Store(false)
		if !answered {
			// A turn Cursor never answered leaves the process unresponsive
			// (its stdin write may still be blocked): force it down now. The
			// wrapper's later Close is idempotent. An answered turn —
			// including an acknowledged cancel — leaves the process for the
			// wrapper to park or close; nothing here ends its life.
			p.stop(true)
		}
		p.providerErr.Finalize()

		p.bridgeMu.Lock()
		deliverable := p.bridgeDeliverable
		sawText, sawTool := p.bridgeSawText, p.bridgeSawTool
		p.bridgeMu.Unlock()
		// Output selection: the deliverable is the text since the latest
		// tool call, falling back to the latest non-empty block when the
		// turn ends on a tool call — the one-shot stream reader's rule.
		// Reading it after unlocking the bridge: the tracker carries its own
		// lock, and a trailing chunk that lands between unlock and result()
		// only makes the answer fuller.
		finalOutput := ""
		if deliverable != nil {
			finalOutput, _ = deliverable.result()
		}

		// Terminal usage = this turn's usage_update snapshots + the prompt
		// result's usage, reconciled with the shared max consolidation (the
		// accumulator was reset at turn start, so no prior turn can leak in).
		// The snapshot is session-cumulative, so it settles as a delta
		// against the previous turn's snapshot before billing.
		if havePrompt {
			p.client.mergeUsage(pr.usage)
		}
		u := p.client.accumulatedUsage()

		var usageMap map[string]TokenUsage
		if delta := p.settleCumulativeUsage(u); acpUsagePresent(delta) {
			model := pr.modelID
			if model == "" {
				model = p.currentEffectiveModel()
			}
			if model == "" {
				model = opts.Model
			}
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: delta}
		}

		// Warm-pool invariant (the one-shot path has no counterpart): a turn
		// that closes cleanly with no text, no tool call and no usage at all
		// never reached the provider, and must not be reported — or parked —
		// as a success.
		if finalStatus == "completed" && usageMap == nil && !sawText && !sawTool {
			finalStatus = "failed"
			finalError = "cursor turn ended with no text, no tool call and no reported usage — the provider produced nothing"
		}

		p.bridgeMu.Lock()
		if p.bridgeMsgs == msgCh {
			p.bridgeMsgs = nil
			p.bridgeDeliverable = nil
			p.bridgeUsage = acpUsageSnapshot{}
			p.bridgeModelID = ""
			p.bridgeStopReason = ""
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

// settleCumulativeUsage converts a session-total snapshot into the per-turn
// delta the daemon bills, advancing the running baseline. A loaded session or
// an intervening missing snapshot has no trusted baseline: seed from the next
// known total without billing history again (the resident drivers' rule).
func (p *cursorWarmProcess) settleCumulativeUsage(snapshot TokenUsage) TokenUsage {
	if !acpUsagePresent(snapshot) {
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
		u := snapshot
		p.lastUsage = &u
		return TokenUsage{}
	}
	prev := TokenUsage{}
	if p.lastUsage != nil {
		prev = *p.lastUsage
	}
	delta := TokenUsage{
		InputTokens:      cursorWarmMaxInt64(0, snapshot.InputTokens-prev.InputTokens),
		OutputTokens:     cursorWarmMaxInt64(0, snapshot.OutputTokens-prev.OutputTokens),
		CacheReadTokens:  cursorWarmMaxInt64(0, snapshot.CacheReadTokens-prev.CacheReadTokens),
		CacheWriteTokens: cursorWarmMaxInt64(0, snapshot.CacheWriteTokens-prev.CacheWriteTokens),
		CostUSDTicks:     cursorWarmMaxInt64(0, snapshot.CostUSDTicks-prev.CostUSDTicks),
	}
	u := snapshot
	p.lastUsage = &u
	return delta
}

// cursorWarmMaxInt64 is the warm file's local max(0, n): the settle below
// subtracts cumulative snapshots and clamps regressions to zero.
func cursorWarmMaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
