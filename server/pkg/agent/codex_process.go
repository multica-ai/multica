package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// codexSyncWriter serializes concurrent writes to a Codex app-server stdin pipe
// (lifecycle RPCs, server-request approval replies from the stdout reader, and
// mid-turn turn/steer calls).
type codexSyncWriter struct {
	mu sync.Mutex
	w  io.WriteCloser
}

func (w *codexSyncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (w *codexSyncWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Close()
}

type codexTurnHooks struct {
	cfg                      Config
	acceptNotification       func(method string, params map[string]any) bool
	onDiscardedNotification  func(method string, params map[string]any)
	onMessage                func(Message)
	onAgentMessageChunk      func(text string) bool
	onAgentMessage           func(text, phase string)
	onFinalAnswer            func(text string)
	onToolActivity           func(callID string, started bool)
	onSemanticActivity       func(description string)
	onReconnect              func(reason string)
	onTurnDone               func(aborted bool)
	guardianRateLimitCircuit *codexGuardianRateLimitCircuit
	onGuardianRateLimit      func()
	rateLimits               *rateLimitTracker
	preserveUsageBaseline    bool
}

func (c *codexClient) attachTurnHooks(h codexTurnHooks) {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()

	c.cfg = h.cfg
	c.acceptNotification = h.acceptNotification
	c.onDiscardedNotification = h.onDiscardedNotification
	c.onMessage = h.onMessage
	c.onAgentMessageChunk = h.onAgentMessageChunk
	c.onAgentMessage = h.onAgentMessage
	c.onFinalAnswer = h.onFinalAnswer
	c.onToolActivity = h.onToolActivity
	c.legacyTurnID = ""
	c.onSemanticActivity = h.onSemanticActivity
	c.onReconnect = h.onReconnect
	c.onTurnDone = h.onTurnDone
	c.guardianRateLimitCircuit = h.guardianRateLimitCircuit
	c.onGuardianRateLimit = h.onGuardianRateLimit
	c.rateLimits = h.rateLimits

	c.agentMessageStreams = nil
	c.agentMessageOrder = nil
	c.turnCompleted = false
	c.setActiveTurnID("")

	c.turnErrorMu.Lock()
	c.turnError = ""
	c.turnErrorMu.Unlock()

	c.usageMu.Lock()
	c.usage = TokenUsage{}
	if !h.preserveUsageBaseline {
		c.usageTotal = codexRawTokenUsage{}
		c.usageTotalSet = false
	}
	c.usageMu.Unlock()
}

func (c *codexClient) detachTurnHooks() {
	c.hookMu.Lock()
	defer c.hookMu.Unlock()

	c.flushAgentMessageDeltas()
	c.acceptNotification = func(string, map[string]any) bool { return false }
	c.onDiscardedNotification = nil
	c.onMessage = nil
	c.onAgentMessageChunk = nil
	c.onAgentMessage = nil
	c.onFinalAnswer = nil
	c.onToolActivity = nil
	c.legacyTurnID = ""
	c.onSemanticActivity = nil
	c.onReconnect = nil
	c.onTurnDone = nil
	c.onGuardianRateLimit = nil
	c.rateLimits = nil
	c.agentMessageStreams = nil
	c.agentMessageOrder = nil
	c.setActiveTurnID("")
}

func (c *codexClient) flushAgentMessageDeltasLocked() {
	c.hookMu.Lock()
	c.flushAgentMessageDeltas()
	c.hookMu.Unlock()
}

func (c *codexClient) turnUsage() TokenUsage {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return c.usage
}

// codexProcess is one Codex app-server process. A run either spawns one or,
// when the warm pool is enabled, acquires one an earlier turn of the same
// thread parked; the turn lifecycle in executeOnce is the same for both.
//
// A pooled process outlives the turn that spawned it: it reads volatile task
// credentials from a per-session overlay file and owns a private session dir.
// A one-shot process inherits the task env directly and is reaped with its
// turn.
type codexProcess struct {
	pooled        bool
	fingerprint   string
	threadID      string
	cmd           *exec.Cmd
	stdin         *codexSyncWriter
	stdout        io.ReadCloser
	stderrBuf     *stderrTail
	client        *codexClient
	stopProcess   context.CancelFunc
	readerDone    chan struct{}
	sessionDir    string
	taskEnvFile   string
	attempt       int
	launchStarted time.Time
	logger        *slog.Logger

	closed           atomic.Bool
	closeOnce        sync.Once
	cleanupConfirmed bool
}

func (p *codexProcess) Fingerprint() string { return p.fingerprint }

func (p *codexProcess) Alive() bool {
	if p.closed.Load() {
		return false
	}
	if p.client.getProcessErr() != nil {
		return false
	}
	select {
	case <-p.readerDone:
		return false
	default:
		return true
	}
}

// clearTaskEnv drops the finished turn's credentials from a pooled process's
// overlay so nothing it does while idle can act as that task.
func (p *codexProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// endTurn stops a pooled process from feeding the finished turn before it is
// parked or closed. A one-shot process keeps its hooks until it is reaped, so
// whatever it flushes on the way out still reaches the run.
func (p *codexProcess) endTurn() {
	if !p.pooled {
		return
	}
	p.client.detachTurnHooks()
	p.clearTaskEnv()
}

// Close closes stdin and joins the process exactly once, reporting whether
// the whole process tree was confirmed reaped. Read stderrBuf only after it
// returns, when os/exec's copy goroutines have finished.
//
// Wait for readerDone before cmd.Wait so Wait cannot close a pipe under an
// active read. Bound both reader shutdown and process reaping. Scanner
// overflow can leave the child blocked on unread stdout, so cancellation and
// WaitDelay must also cover that path instead of relying on stdin EOF to stop
// the child.
func (p *codexProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		p.clearTaskEnv()
		_ = p.stdin.Close()

		grace := codexGracefulShutdown()
		waitCh := make(chan struct{})
		var waitErr error
		var startWait sync.Once
		startProcessWait := func() {
			startWait.Do(func() {
				go func() {
					waitErr = p.cmd.Wait()
					close(waitCh)
				}()
			})
		}

		// Phase 1: let the reader finish before invoking cmd.Wait().
		select {
		case <-p.readerDone:
			// reader drained cleanly (codex shutdown closed stdout) or aborted
			// early (e.g. scanner overflow). Either way it is now safe to call
			// cmd.Wait().
		case <-time.After(grace):
			// codex did not close stdout within the grace window. Force the
			// shutdown via context cancellation — cmd.Cancel group-kills the
			// tree, the reader unblocks when stdout EOFs, and we proceed to
			// phase 2.
			p.logger.Warn("codex did not close stdout after stdin EOF; forcing shutdown",
				"pid", p.cmd.Process.Pid,
				"grace", grace.String(),
			)
			p.stopProcess()
			// On Windows, Cancel terminates only the direct child. A descendant
			// may keep inherited stdout open indefinitely. Start Wait now so
			// os/exec's WaitDelay closes the pipe after its bounded deadline and
			// lets the reader finish.
			startProcessWait()
			<-waitCh
			select {
			case <-p.readerDone:
			case <-time.After(grace):
				p.logger.Warn("codex stdout reader remained open after bounded process wait",
					"pid", p.cmd.Process.Pid,
					"grace", grace.String(),
				)
			}
		}

		// Phase 2: bound cmd.Wait() in case the process is still alive
		// (scanner-overflow case: reader exited early on its own while codex
		// stayed blocked writing into a full stdout pipe).
		startProcessWait()
		var waitReturned bool
		select {
		case <-waitCh:
			waitReturned = true
		case <-time.After(grace):
			p.logger.Warn("codex process still alive after reader exited; forcing shutdown",
				"pid", p.cmd.Process.Pid,
				"grace", grace.String(),
			)
			p.stopProcess()
			// WaitDelay is the final backstop: even if the group-kill races
			// with an open pipe held by a descendant, cmd.Wait() returns within
			// WaitDelay of the cancel.
			<-waitCh
			waitReturned = true
		}

		// Wait returning with a ProcessState is the os/exec reap boundary. On
		// Unix, ProcessState.Exited reports false for a process terminated by
		// SIGKILL even though Wait successfully reaped it.
		p.cleanupConfirmed = waitReturned && p.cmd.ProcessState != nil && waitProcessGroupGone(p.cmd, grace)
		if codexCleanupConfirmationOverride.Load() < 0 {
			p.cleanupConfirmed = false
		}
		p.logger.Info("codex lifecycle",
			"phase", "cleanup",
			"warm_pool", p.pooled,
			"pid", p.cmd.Process.Pid,
			"process_group", p.cmd.Process.Pid,
			"attempt", p.attempt,
			"latency", time.Since(p.launchStarted).Round(time.Millisecond).String(),
			"reaped", p.cleanupConfirmed,
			"exit_status", codexProcessExitStatus(p.cmd.ProcessState),
			"wait_error", waitErr,
			"stderr_bytes", p.stderrBuf.TotalBytes(),
			"stderr_truncated", p.stderrBuf.TotalBytes() > codexStderrTailBytes,
		)
		// The tree has been reaped and observed; drop ownership. This is the
		// only safe point to do so on Windows, where releasing kills whatever
		// is still inside the job — which is precisely what should happen to
		// anything that outlived the reap above.
		releaseProcessGroup(p.cmd)
		p.stopProcess()
		activeCodexLaunches.Add(-1)
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
	})
	return p.cleanupConfirmed
}

func (b *codexBackend) spawnCodexProcess(
	runtimeCmd Command,
	codexArgs []string,
	opts ExecOptions,
	pooled bool,
	fingerprint string,
	handshakeTimeout time.Duration,
	threadHandshakeTimeout time.Duration,
	attempt int,
) (*codexProcess, error) {
	var sessionDir, taskEnvFile string
	childEnv := b.cfg.Env
	if pooled {
		var err error
		sessionDir, err = os.MkdirTemp("", "multica-codex-warm-*")
		if err != nil {
			return nil, fmt.Errorf("create warm codex session dir: %w", err)
		}
		sessionTempDir := filepath.Join(sessionDir, "tmp")
		if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
			_ = os.RemoveAll(sessionDir)
			return nil, fmt.Errorf("create warm codex temp dir: %w", err)
		}
		taskEnvFile = filepath.Join(sessionDir, "task-env.json")
		if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
			_ = os.RemoveAll(sessionDir)
			return nil, fmt.Errorf("write warm codex task env overlay: %w", err)
		}
		childEnv = PreparePooledChildEnv(b.cfg.Env, taskEnvFile, sessionTempDir)
	}
	cleanupOnErr := func() {
		if sessionDir != "" {
			_ = os.RemoveAll(sessionDir)
		}
	}

	// The process context is never tied to the task: the app-server must stay
	// alive long enough to handle turn/interrupt when the task context is
	// cancelled, and a pooled one outlives the task entirely. The lifecycle
	// goroutine stops it explicitly during bounded cleanup.
	processCtx, stopProcess := context.WithCancel(context.Background())
	cmd := runtimeCmd.exec(processCtx, codexArgs...)
	hideAgentWindow(cmd)
	// Run codex in its own process group so a cancel-on-stuck cleanup
	// reaches the whole tree — the codex Node wrapper plus the native
	// Rust app-server it spawns — not just the direct child. Without
	// this, killing the leader leaves grandchildren as orphans that
	// keep consuming memory until the OS reaps them; see #4520, where a
	// scanner overflow during thread/resume otherwise leaked Codex
	// processes indefinitely. configureProcessGroup is a no-op on
	// Windows.
	// Override the default exec.CommandContext cancel behaviour. The
	// default sends SIGKILL only to cmd.Process (the leader); we instead
	// signal the whole process group so descendants die too. Returning
	// nil keeps exec from logging a spurious error; cmd.WaitDelay below
	// still backstops cmd.Wait() if the kill leaves an open pipe.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		return nil
	}
	// Bound the wait after the context is cancelled so a stuck child (or an
	// open pipe held by a grandchild) can't hang cmd.Wait() forever. Matches
	// the other long-lived backends (claude, copilot, cursor, …).
	cmd.WaitDelay = codexProcessWaitDelay()
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(codexArgs, trustAgentCommandPositional(0, "app-server")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(childEnv)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stopProcess()
		cleanupOnErr()
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		stopProcess()
		cleanupOnErr()
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdin := &codexSyncWriter{w: stdinPipe}
	// Codex stderr can contain auth/provider diagnostics. Capture a bounded
	// tail and emit it only through the sanitizer in the cleanup event.
	stderrBuf := newStderrTail(io.Discard, codexStderrTailBytes)
	cmd.Stderr = stderrBuf

	// Start and take ownership of the process tree in one step. On Windows the
	// child is created suspended and placed in a Job Object before it runs, so
	// the cleanup reaches the Node wrapper, the native app-server, and the
	// sandbox helpers underneath them rather than just the direct child. On Unix
	// the process group configured above already covers that and this is a plain
	// Start. Ownership that cannot be taken is logged, not fatal; a child that
	// cannot be resumed is killed and reported here.
	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		stopProcess()
		cleanupOnErr()
		return nil, fmt.Errorf("start codex: %w", err)
	}
	activeLaunches := activeCodexLaunches.Add(1)
	for {
		maxSeen := maxActiveCodexLaunchesObserved.Load()
		if activeLaunches <= maxSeen || maxActiveCodexLaunchesObserved.CompareAndSwap(maxSeen, activeLaunches) {
			break
		}
	}
	launchStarted := time.Now()
	codexVersion := strings.TrimSpace(b.cfg.CodexVersion)
	if codexVersion == "" {
		codexVersion = "unknown"
	}
	b.cfg.Logger.Info("codex lifecycle",
		"phase", "spawn",
		"warm_pool", pooled,
		"task_id", b.cfg.TaskID,
		"runtime_id", b.cfg.RuntimeID,
		"pid", cmd.Process.Pid,
		"process_group", cmd.Process.Pid,
		"cwd", opts.Cwd,
		"attempt", attempt,
		"active_launches", activeLaunches,
		"codex_version", codexVersion,
		"daemon_version", b.cfg.DaemonVersion,
	)

	c := &codexClient{
		cfg:                    b.cfg,
		stdin:                  stdin,
		pending:                make(map[int]*pendingRPC),
		processDone:            make(chan struct{}),
		handshakeTimeout:       handshakeTimeout,
		threadHandshakeTimeout: threadHandshakeTimeout,
		pid:                    cmd.Process.Pid,
		attempt:                attempt,
		activeLaunches:         activeLaunches,
		notificationProtocol:   "unknown",
		acceptNotification:     func(string, map[string]any) bool { return false },
	}

	return &codexProcess{
		pooled:        pooled,
		fingerprint:   fingerprint,
		cmd:           cmd,
		stdin:         stdin,
		stdout:        stdout,
		stderrBuf:     stderrBuf,
		client:        c,
		stopProcess:   stopProcess,
		readerDone:    make(chan struct{}),
		sessionDir:    sessionDir,
		taskEnvFile:   taskEnvFile,
		attempt:       attempt,
		launchStarted: launchStarted,
		logger:        b.cfg.Logger,
	}, nil
}

// readStdout dispatches every stdout line to the client for the life of the
// process. The caller starts it once the first turn's hooks are attached.
func (p *codexProcess) readStdout() {
	defer close(p.readerDone)
	c := p.client
	scanner := newAgentStreamScanner(p.stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.handleLine(line)
	}
	// A cancelled or crashed app-server may close stdout without an
	// item/completed snapshot. Preserve every complete delta JSON event
	// already parsed; an incomplete final JSON token is ignored by handleLine
	// and can never enter this buffer.
	c.flushAgentMessageDeltasLocked()
	if err := scanner.Err(); err != nil {
		// %w on BOTH: callers match errCodexProcessExited to decide the
		// process is gone, and bufio.ErrTooLong to tell "we could not read
		// the response" apart from "codex died". startOrResumeThread needs
		// that distinction to report an oversized resume as a rejected
		// resume rather than a crash (MUL-5722).
		c.markProcessExited(fmt.Errorf("%w: %w", errCodexProcessExited, err))
		return
	}
	c.markProcessExited(errCodexProcessExited)
}

func (b *codexBackend) warmFingerprint(execPath string, runtimeCmd Command, codexArgs []string, opts ExecOptions) string {
	var configFiles []string
	if codexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"]); codexHome != "" {
		configFiles = append(configFiles, filepath.Join(codexHome, "config.toml"))
	}
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:      "codex",
		Executable:    execPath,
		LaunchPrefix:  runtimeCmd.Prefix,
		Args:          codexArgs,
		Cwd:           opts.Cwd,
		Model:         opts.Model,
		ThinkingLevel: opts.ThinkingLevel,
		ServiceTier:   opts.ServiceTier,
		McpConfig:     opts.McpConfig,
		ConfigFiles:   configFiles,
		BriefHash:     opts.WarmPoolBriefHash,
		Env:           b.cfg.Env,
	})
}

// acquireWarmProcess checks out the process parked for threadID and hands it
// this task's credentials, or returns nil when the run must spawn.
func (b *codexBackend) acquireWarmProcess(threadID, fingerprint string) *codexProcess {
	if threadID == "" {
		return nil
	}
	acquired := b.cfg.WarmPool.Acquire("codex", threadID, fingerprint)
	if acquired == nil {
		return nil
	}
	proc, ok := acquired.(*codexProcess)
	if !ok {
		acquired.Close()
		return nil
	}
	if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
		b.cfg.Logger.Warn("warm codex: failed to refresh task env overlay; evicting", "error", err)
		proc.Close()
		return nil
	}
	return proc
}
