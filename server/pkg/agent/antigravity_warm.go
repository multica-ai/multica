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

// Antigravity over a warm ACP process (`agy acp` kept alive across turns).
//
// The WarmSessionPool pattern is the one codex_process.go, claude_process.go,
// qoder_warm.go and cursor_warm.go use: a turn either checks out the process
// parked for its session id (the fingerprint of every session-immutable launch
// input must match) or spawns a fresh pooled process, and a turn that completed
// cleanly parks the process again. Antigravity's ACP transport makes this
// natural — the process already holds the live session, so a follow-up turn is
// another `session/prompt` on the same connection, with no re-handshake and no
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
//   - Session-scoped configuration (model, MCP set) is fixed when the session
//     opens; every input that could change it is part of the fingerprint, so
//     a changed claim evicts the process instead of silently serving the turn
//     with the previous configuration.
//
// Dialect decisions this transport owns, mirroring the one-shot print
// transport (antigravity.go) where the behaviors agree:
//
//   - The `agy models` catalog gate runs before the launch, fail-open, the
//     same rule the one-shot path applies: a non-empty model the installed
//     CLI definitively does not advertise fails the claim with an actionable
//     error instead of surfacing later as a "completed" but empty turn.
//   - Model selection runs once per process over the ACP configuration
//     channel (set_config_option on the session's `model` option), so later
//     turns keep the session's settled configuration; a claim that changes
//     the model changes the fingerprint and gets its own process.
//   - The prompt response's usage frame is per-turn on the versions this
//     transport accepts, so it is billed directly — no cumulative settle.
//   - The steering hook relay (supplements) and skill invocations are
//     per-task machinery the pooled process must not inherit; claims that
//     need them stay on the one-shot transport, as do custom launch args.
//   - The runtime brief is not prepended to any prompt: agy loads cwd-scoped
//     context files itself, the same rule the one-shot path runs on.

const (
	// antigravityWarmHandshakeTimeout bounds the initialize / session/load /
	// session/new / model-selection RPCs of a warm turn's first-turn setup.
	// Generous: a cold CLI can take seconds to boot. The prompt itself is not
	// bounded by it — the daemon's watchdogs supervise the turn through its
	// own context.
	antigravityWarmHandshakeTimeout = 30 * time.Second
	// antigravityWarmExitGrace is how long the process gets to exit on its
	// own after stdin closes before the teardown escalates to the process
	// group. agy cleans up out-of-group children on EOF (the one-shot path's
	// graceful shutdown relies on the same window), so the EOF window comes
	// first.
	antigravityWarmExitGrace = 3 * time.Second
	// antigravityWarmTerminateGrace is how long a SIGTERM'd process group
	// gets before the owned-context cancel kills it.
	antigravityWarmTerminateGrace = 3 * time.Second
	// antigravityWarmCancelGrace bounds the wait for the prompt to answer
	// after session/cancel; past it the process is considered unresponsive
	// and stopped.
	antigravityWarmCancelGrace = 5 * time.Second
)

// antigravityWarmExitGraceNanos and antigravityWarmTerminateGraceNanos let
// tests shorten the teardown graces — the same relief
// qoderWarmExitGraceNanos gives the qoder warm tests. They are atomic so
// tests can shorten a grace without racing the teardown goroutine that reads
// it. Zero means use the default.
var (
	antigravityWarmExitGraceNanos      atomic.Int64
	antigravityWarmTerminateGraceNanos atomic.Int64
)

func antigravityWarmExitGraceDuration() time.Duration {
	if n := antigravityWarmExitGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return antigravityWarmExitGrace
}

func antigravityWarmTerminateGraceDuration() time.Duration {
	if n := antigravityWarmTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return antigravityWarmTerminateGrace
}

// antigravityWarmMinVersionMajor/Minor/Patch name the earliest agy release
// with a verified per-turn ACP usage frame (1.5.5); the warm transport bills
// from that frame, so an older CLI stays on the one-shot path (which meters
// from stream-json).
const (
	antigravityWarmMinVersionMajor = 1
	antigravityWarmMinVersionMinor = 5
	antigravityWarmMinVersionPatch = 5
)

// antigravityWarmEligible reports whether a claim can take the warm
// transport. Claims whose per-task inputs the ACP launch cannot carry, and
// CLIs without the per-turn usage frame the warm billing reads, stay on the
// one-shot path:
//
//   - a custom runtime profile's command is not the vendored agy the version
//     gate was verified against
//   - agy 1.5.5 is the earliest release with the per-turn ACP usage frame
//   - skill invocations build a one-shot prompt shape
//   - steering supplements inject a per-task hook environment the pooled
//     process must not inherit (and cannot refresh per turn)
//   - custom args are only validated against the print transport's flag set
func antigravityWarmEligible(cfg Config, opts ExecOptions) bool {
	if !cfg.BuiltinRuntime || len(cfg.LaunchPrefix) != 0 {
		return false
	}
	version, err := parseSemver(cfg.CLIVersion)
	if err != nil || version.lessThan(semver{Major: antigravityWarmMinVersionMajor, Minor: antigravityWarmMinVersionMinor, Patch: antigravityWarmMinVersionPatch}) {
		return false
	}
	return opts.SkillInvocation == nil &&
		!opts.EnableTaskSupplement &&
		len(opts.CustomArgs) == 0 &&
		len(opts.ExtraArgs) == 0
}

// antigravityACPNotify writes a JSON-RPC notification without an id (ACP
// session/cancel).
func antigravityACPNotify(c *hermesClient, method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return c.writeLine(append(data, '\n'))
}

// antigravityWarmArgs is the exact command line a pooled Antigravity ACP
// process is spawned with: the ACP subcommand plus the per-task isolation
// directory (daemon-assigned isolation must survive the warm path — the
// fingerprint pins the cwd, so a changed workdir evicts the process).
func antigravityWarmArgs(cwd string) []string {
	args := []string{"acp"}
	if cwd != "" {
		args = append(args, "--add-dir", filepath.Clean(cwd))
	}
	return args
}

// antigravityWarmFingerprint hashes every session-immutable input of a pooled
// ACP process. ResumeSessionID is deliberately omitted (claude
// warmFingerprint's rule): it selects which conversation a process serves,
// not how the process was built. The env input is the launch environment the
// one-shot path would have used (buildEnv's filtered merge of the daemon
// environment), before the pooled overlay repointing — volatile keys and the
// overlay path itself are excluded by computeWarmSessionFingerprint.
func antigravityWarmFingerprint(execPath string, opts ExecOptions, envMap map[string]string) string {
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:   "antigravity",
		Executable: execPath,
		Args:       antigravityWarmArgs(opts.Cwd),
		Cwd:        opts.Cwd,
		Model:      opts.Model,
		McpConfig:  opts.McpConfig,
		BriefHash:  opts.WarmPoolBriefHash,
		Env:        envMap,
	})
}

// executeWarmAntigravity runs the turn through the warm pool: acquire a
// parked process or spawn a fresh pooled one, run the turn on it, then park
// or close it according to the result. The returned Session forwards the
// turn's message stream directly; its Result is synthesized here so the park
// decision happens before the caller learns the turn ended.
func (b *antigravityBackend) executeWarmAntigravity(ctx context.Context, execPath, prompt string, opts ExecOptions) (*Session, error) {
	// Fail-open model validation, the one-shot path's gate: a model the
	// installed CLI definitively does not advertise would exit 0 with empty
	// output and surface as a "completed" but empty turn.
	if opts.Model != "" {
		catalog, _ := ListModels(ctx, "antigravity", b.cfg.commandAt(execPath))
		if err := antigravityModelError(opts.Model, catalog.Models); err != nil {
			return nil, err
		}
	}

	proc, warmHit, err := b.acquireWarmAntigravity(execPath, opts)
	if err != nil {
		return nil, err
	}

	turn, turnErr := proc.runTurn(ctx, prompt, opts)
	if turnErr != nil {
		// The process could not take this turn at all (dead process): it
		// holds nothing reusable.
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
			parked = b.cfg.WarmPool.Park("antigravity", res.SessionID, proc)
		}
		if !parked {
			proc.Close()
		}
		b.cfg.Logger.Info("antigravity finished",
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

// acquireWarmAntigravity checks out the process parked for
// opts.ResumeSessionID, refreshing its task-env overlay with this turn's
// credentials, or spawns a fresh pooled process.
func (b *antigravityBackend) acquireWarmAntigravity(execPath string, opts ExecOptions) (*antigravityWarmProcess, bool, error) {
	envMap := antigravityWarmEnvSliceToMap(buildEnv(b.cfg.Env))
	fingerprint := antigravityWarmFingerprint(execPath, opts, envMap)

	if opts.ResumeSessionID != "" {
		if acquired := b.cfg.WarmPool.Acquire("antigravity", opts.ResumeSessionID, fingerprint); acquired != nil {
			if proc, ok := acquired.(*antigravityWarmProcess); ok {
				if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
					b.cfg.Logger.Warn("warm antigravity: failed to refresh task env overlay; evicting", "error", err)
					proc.Close()
				} else {
					return proc, true, nil
				}
			} else {
				// Not ours; nothing of this shape should ever be parked under
				// the antigravity key. Close it rather than leak the process.
				acquired.Close()
			}
		}
	}

	proc, err := b.spawnWarmAntigravity(execPath, opts, envMap, fingerprint)
	if err != nil {
		return nil, false, err
	}
	return proc, false, nil
}

// antigravityWarmEnvSliceToMap keeps the last assignment of duplicated keys,
// the same resolution order buildEnv's append produces.
func antigravityWarmEnvSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// antigravityWarmProcess is one pooled Antigravity ACP process holding a live
// ACP connection. The long-lived pieces (process, pipes, stdout reader,
// client) are created once at spawn; per-turn state travels through the
// bridge, so protocol callbacks always land on the turn that is actually
// prompting.
type antigravityWarmProcess struct {
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

	// bridge forwards protocol callbacks to the active turn. The deliverable
	// tracker accumulates Result.Output alongside the forwarded stream: the
	// deliverable is the text since the latest tool call — the selection the
	// resident transport delivered (joining every text chunk leaks interim
	// narration into the persisted chat reply).
	bridgeMu          sync.Mutex
	bridgeMsgs        chan<- Message
	bridgeDeliverable *acpDeliverableTracker
	bridgeUsage       *TokenUsage
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

func (p *antigravityWarmProcess) currentSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionID
}

func (p *antigravityWarmProcess) storeSessionID(id string) string {
	p.mu.Lock()
	p.sessionID = id
	p.mu.Unlock()
	return id
}

func (p *antigravityWarmProcess) currentEffectiveModel() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.effectiveModel
}

func (p *antigravityWarmProcess) storeEffectiveModel(model string) {
	p.mu.Lock()
	p.effectiveModel = model
	p.mu.Unlock()
}

// Fingerprint implements warmPoolProcess: the launch identity this process
// was spawned under.
func (p *antigravityWarmProcess) Fingerprint() string { return p.fingerprint }

// Alive implements warmPoolProcess: no teardown is on the floor and the
// stdout reader has not hit EOF (codexProcess's rule — reader EOF is the
// process-gone signal, and stop() owns the single cmd.Wait).
func (p *antigravityWarmProcess) Alive() bool {
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

func (p *antigravityWarmProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// Close terminates the process group and removes the session dir. It is
// idempotent. stdin gets the EOF window first before the group signal: agy
// cleans up out-of-group children on EOF, so killing only the main group
// would strand that work (the one-shot path's shutdown shape).
func (p *antigravityWarmProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.stop(false)
		pid := 0
		if p.cmd.Process != nil {
			pid = p.cmd.Process.Pid
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
		p.cfg.Logger.Info("warm antigravity process closed", "pid", pid)
	})
	return true
}

// stop tears the process down exactly once; force skips the EOF window (an
// unresponsive runtime is gone within antigravityWarmTerminateGrace). Shared
// by Close and the mid-turn teardown paths.
func (p *antigravityWarmProcess) stop(force bool) {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		_ = p.stdin.Close()
		if !force {
			select {
			case <-p.readerDone:
			case <-time.After(antigravityWarmExitGraceDuration()):
				force = true
			}
		}
		if force {
			signalProcessGroup(p.cmd, syscall.SIGTERM)
			select {
			case <-p.readerDone:
			case <-time.After(antigravityWarmTerminateGraceDuration()):
			}
		}
		p.procCancel() // group kill; a no-op for a group that already exited
		_ = p.cmd.Wait()
		<-p.readerDone
		<-p.stderrDone
		releaseProcessGroup(p.cmd)
	})
}

// spawnWarmAntigravity launches a pooled ACP process on its own context with
// the overlay-based credential plumbing. The spawn mirrors the one-shot
// path's (hideAgentWindow, owned process group) plus the pooled session dir:
// the task-env overlay file and a private temp dir that outlive the task that
// started the process.
func (b *antigravityBackend) spawnWarmAntigravity(execPath string, opts ExecOptions, envMap map[string]string, fingerprint string) (*antigravityWarmProcess, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}
	protocolCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("antigravity working directory: %w", err)
	}

	sessionDir, err := os.MkdirTemp("", "multica-antigravity-warm-")
	if err != nil {
		return nil, fmt.Errorf("create warm antigravity session dir: %w", err)
	}
	sessionTempDir := filepath.Join(sessionDir, "tmp")
	if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("create warm antigravity temp dir: %w", err)
	}
	taskEnvFile := filepath.Join(sessionDir, "task-env.json")
	if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("write warm antigravity task env overlay: %w", err)
	}
	// The overlay repointing strips the volatile keys from the static child
	// environment, adds the overlay pointer, and moves TMPDIR onto the
	// session dir so per-task temp cleanup cannot pull the rug from under a
	// parked process.
	launchEnv := PreparePooledChildEnv(envMap, taskEnvFile, sessionTempDir)

	args := antigravityWarmArgs(opts.Cwd)
	p := &antigravityWarmProcess{
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

	fail := func(err error) (*antigravityWarmProcess, error) {
		procCancel()
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(fmt.Errorf("antigravity stdout pipe: %w", err))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(fmt.Errorf("antigravity stdin pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(fmt.Errorf("antigravity stderr pipe: %w", err))
	}
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fail(fmt.Errorf("start antigravity: %w", err))
	}
	p.cmd = cmd
	p.stdin = stdin
	b.cfg.Logger.Info("warm antigravity spawned", "pid", cmd.Process.Pid, "cwd", protocolCwd, "model", opts.Model)

	p.providerErr = newACPProviderErrorSniffer("antigravity")
	p.stderrDone = make(chan struct{})
	go func() {
		defer close(p.stderrDone)
		_, _ = io.Copy(io.MultiWriter(newLogWriter(b.cfg.Logger, "[agy:stderr] "), p.providerErr), stderr)
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
		// this turn's per-turn usage snapshot and stop reason, which the turn
		// goroutine reads back through the bridge right after the request
		// returns.
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
		p.client.closeAllPending(errors.New("antigravity process exited"))
	}()

	return p, nil
}

// runTurn executes one turn on the live process and returns a turn-scoped
// Session whose Result carries the outcome. Setup failures land in the Result
// too (with the session id and ResumeRejected semantics the one-shot path
// reports, so the daemon's retry can resume cold); only a process that cannot
// take the turn at all comes back as a Go error. The process is left for the
// caller (executeWarmAntigravity) to park or close — except on setup paths
// that cannot produce a session, which tear it down here.
func (p *antigravityWarmProcess) runTurn(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	p.turnMu.Lock()
	if !p.Alive() {
		p.turnMu.Unlock()
		return nil, errors.New("warm antigravity process is not running")
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
		// The ACP mcpServers array stays empty: antigravity's managed MCP
		// configuration is a print-transport surface, and agy's ACP session
		// has no managed-MCP translation (the session handshake still
		// requires the array itself).
		result := func(status, errText string, sid string, rejected bool) *Session {
			p.turnMu.Unlock()
			close(msgCh)
			resCh <- Result{Status: status, Error: errText, SessionID: sid, ResumeRejected: rejected, DurationMs: time.Since(startTime).Milliseconds()}
			return &Session{Messages: msgCh, Result: resCh}
		}

		// The setup RPCs sit under two budgets: the turn's execution deadline
		// (above) and this shorter handshake cap. The process is never
		// cancelled through this context — it outlives the turn. A runtime
		// that stops reading holds the synchronous setup writes, so the
		// timeout closes stdin to release them. The callback stays armed
		// across the WHOLE setup — initialize, session/load|new and model
		// selection — not just the first RPC: a turn cancelled mid-setup must
		// not wedge on a blocked session/new write. Every failure path below
		// tears the process down anyway; the callback firing there is a no-op.
		setupCtx, cancelSetup := context.WithTimeout(turnCtx, antigravityWarmHandshakeTimeout)
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
			status, errText, _ := classifyACPResumeFailure(setupCtx, "antigravity", "initialize", err, opts.Timeout, p.cfg.Logger)
			p.Close()
			return result(status, errText, "", false), nil
		}
		var negotiated struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if json.Unmarshal(init, &negotiated) != nil || negotiated.ProtocolVersion != 1 {
			p.Close()
			return result("failed", "antigravity ACP: unsupported protocol version", "", false), nil
		}

		mcpServers := []any{}
		var sessionResult json.RawMessage
		if opts.ResumeSessionID != "" {
			loaded, err := p.client.request(setupCtx, "session/load", map[string]any{
				"cwd":        p.protocolCwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				status, errText, rejected := classifyACPResumeFailure(setupCtx, "antigravity", "session/load", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, "", rejected), nil
			}
			resolved, changed := resolveResumedSessionID(opts.ResumeSessionID, loaded)
			if changed {
				p.cfg.Logger.Warn("antigravity returned a different session id on resume; continuing with the new id",
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
				status, errText, _ := classifyACPResumeFailure(setupCtx, "antigravity", "session/new", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, "", false), nil
			}
			if sessionID = extractACPSessionID(created); sessionID == "" {
				p.Close()
				return result("failed", "antigravity session/new returned no session ID", "", false), nil
			}
			p.storeSessionID(sessionID)
			sessionResult = created
		}
		p.client.sessionID = sessionID

		if strings.TrimSpace(opts.Model) == "" {
			p.storeEffectiveModel(extractACPCurrentModelID(sessionResult))
		}

		// Session-scoped model selection, the ACP transport's counterpart of
		// the print transport's --model flag. The session must advertise the
		// `model` configuration option: an agy that cannot select the model
		// would silently run the turn on its default while the claim records
		// the requested one. Runs once per process; later turns keep the
		// session's settled configuration, and a claim that changes the model
		// changes the fingerprint and gets its own process.
		if model := strings.TrimSpace(opts.Model); model != "" {
			keep := sessionID
			if setupFailureWithholdsSessionID(opts) {
				// A never-prompted session has no transcript behind it; an id
				// withheld here costs one extra session/new next turn (the
				// qoder set_model failure's rule).
				keep = ""
			}
			if _, ok := findACPConfigOption(sessionResult, "model"); !ok {
				p.Close()
				return result("failed", "antigravity ACP did not advertise the requested model option", keep, false), nil
			}
			if _, err := opencodeACPSelectModel(setupCtx, p.client.request, sessionID, model, sessionResult); err != nil {
				p.cfg.Logger.Warn("antigravity model selection failed", "error", err, "requested_model", model)
				status, errText, _ := classifyACPResumeFailure(setupCtx, "antigravity", "model selection", err, opts.Timeout, p.cfg.Logger)
				p.Close()
				return result(status, errText, keep, false), nil
			}
			p.storeEffectiveModel(model)
			p.cfg.Logger.Info("antigravity session model set", "model", model)
		}
		// Setup is done: the process now has its session, and the prompt's
		// own write window is guarded by the prompt context's callback below.
		stopBlockedSetup()

		p.cfg.Logger.Info("warm antigravity session ready", "session_id", sessionID, "resumed", opts.ResumeSessionID != "")
	}

	// The history replay (if session/load sent one) has been answered; from
	// here the stream belongs to the prompted turn. The bridge is wired
	// before the goroutine so the first stream frames cannot slip past it.
	p.bridgeMu.Lock()
	p.bridgeMsgs = msgCh
	p.bridgeDeliverable = &acpDeliverableTracker{}
	p.bridgeUsage = nil
	p.bridgeStopReason = ""
	p.bridgeSawText = false
	p.bridgeSawTool = false
	p.bridgeMu.Unlock()

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
				p.cfg.Logger.Warn("antigravity: turn ended before the prompt was delivered; stopping the process")
				break
			}
			// Ask agy to stop the turn; it aborts the running tool and
			// answers the prompt. The write runs on its own goroutine so a
			// stalled pipe cannot hold the teardown, and the grace starts
			// now. agy's tool children need the protocol-level abort before
			// teardown — a group signal alone never reaches them (the
			// resident transport's abortSessionCancel rule).
			go func() {
				if err := antigravityACPNotify(p.client, "session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
					p.cfg.Logger.Warn("antigravity: session/cancel could not be sent", "error", err)
				}
			}()
			select {
			case out := <-promptCh:
				promptErr, answered = out.err, true
			case <-time.After(antigravityWarmCancelGrace):
				p.cfg.Logger.Warn("antigravity did not answer session/cancel in time; stopping the process", "grace", antigravityWarmCancelGrace.String())
			}
		}
		stopBlockedWrite()

		var bridgeUsage *TokenUsage
		bridgeStopReason := ""
		havePrompt := false
		if answered && promptErr == nil {
			p.bridgeMu.Lock()
			bridgeUsage, bridgeStopReason = p.bridgeUsage, p.bridgeStopReason
			p.bridgeMu.Unlock()
			havePrompt = true
		}

		switch {
		case promptCtx.Err() == context.DeadlineExceeded:
			finalStatus, finalError = "timeout", fmt.Sprintf("antigravity timed out after %s", opts.Timeout)
		case promptCtx.Err() == context.Canceled:
			finalStatus, finalError = "aborted", "execution cancelled"
		case promptErr != nil:
			finalStatus, finalError = "failed", fmt.Sprintf("antigravity session/prompt failed: %v", promptErr)
			// The runtime may echo the requested id from session/load and
			// only reject it at prompt time. Cold-path parity: clear the id
			// and report ResumeRejected so the daemon drops the stale pointer
			// and retries fresh; an empty session id never parks.
			if opts.ResumeSessionID != "" && isACPSessionNotFound(promptErr) {
				p.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
					"backend", "antigravity",
					"session_id", sessionID,
				)
				sessionID = ""
				resumeRejected = true
			}
		case havePrompt && bridgeStopReason != "" && bridgeStopReason != "end_turn":
			switch bridgeStopReason {
			case "cancelled":
				finalStatus, finalError = "aborted", "antigravity cancelled the prompt"
			default:
				finalStatus, finalError = "failed", "antigravity stopped: "+bridgeStopReason
			}
		}
		if answered && promptCtx.Err() == nil {
			waitForACPNotificationQuiescence(promptCtx, p.activity, p.readerDone, acpNotificationQuietTime, opencodeACPDrainGrace)
		}
		p.streaming.Store(false)
		if !answered {
			// A turn agy never answered leaves the process unresponsive (its
			// stdin write may still be blocked): force it down now. The
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
		// Output selection mirrors the resident rule: the deliverable is the
		// text since the latest tool call, falling back to the latest
		// non-empty block when the turn ends on a tool call. Reading it after
		// unlocking the bridge: the tracker carries its own lock, and a
		// trailing chunk that lands between unlock and result() only makes
		// the answer fuller.
		finalOutput := ""
		if deliverable != nil {
			finalOutput, _ = deliverable.result()
		}

		// The prompt response's usage frame is per-turn on the versions this
		// transport accepts (agy 1.5.5+), so it bills directly — no
		// cumulative settle against previous turns.
		var usageMap map[string]TokenUsage
		if havePrompt && bridgeUsage != nil && acpUsagePresent(*bridgeUsage) {
			billedModel := p.currentEffectiveModel()
			if billedModel == "" {
				billedModel = opts.Model
			}
			if billedModel == "" {
				billedModel = "unknown"
			}
			usageMap = map[string]TokenUsage{billedModel: *bridgeUsage}
		}

		// Warm-pool invariant (the one-shot path has no counterpart): a turn
		// that closes cleanly with no text, no tool call and no usage at all
		// never reached the provider, and must not be reported — or parked —
		// as a success.
		if finalStatus == "completed" && usageMap == nil && !sawText && !sawTool {
			finalStatus = "failed"
			finalError = "antigravity turn ended with no text, no tool call and no reported usage — the provider produced nothing"
		}

		p.bridgeMu.Lock()
		if p.bridgeMsgs == msgCh {
			p.bridgeMsgs = nil
			p.bridgeDeliverable = nil
			p.bridgeUsage = nil
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
