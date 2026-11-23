package agent

import (
	"context"
	"encoding/json"
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

// claudeProcess is one Claude Code stream-json process. A run either spawns
// one or, when the warm pool is enabled, acquires one an earlier turn of the
// same session parked; the turn lifecycle in Execute is the same for both.
//
// A pooled process outlives the turn that spawned it: it reads volatile task
// credentials from a per-session overlay file and owns a private session dir.
// A one-shot process inherits the task env directly and exits with its turn.
type claudeProcess struct {
	pooled                   bool
	fingerprint              string
	sessionID                string
	cmd                      *exec.Cmd
	stdin                    io.WriteCloser
	stdinOnce                sync.Once
	inputWriter              *claudeInputWriter
	stdout                   io.ReadCloser
	stderrBuf                *stderrTail
	procDone                 chan struct{}
	mcpConfigPath            string
	sessionDir               string
	taskEnvFile              string
	logger                   *slog.Logger
	initialized              bool
	initializedCommands      map[string]bool
	lastCumulativeModelUsage map[string]TokenUsage

	turnMu     sync.Mutex
	activeTurn *claudeTurn
	scanErr    error
	exitErr    error

	closed    atomic.Bool
	closeOnce sync.Once
}

func (p *claudeProcess) Fingerprint() string { return p.fingerprint }

func (p *claudeProcess) Alive() bool {
	if p.closed.Load() {
		return false
	}
	select {
	case <-p.procDone:
		return false
	default:
		return true
	}
}

func (p *claudeProcess) setActiveTurn(t *claudeTurn) {
	p.turnMu.Lock()
	p.activeTurn = t
	p.turnMu.Unlock()
}

// dispatch hands an event to the active turn while holding turnMu, so once
// setActiveTurn(nil) returns no event can touch the finished turn's channels
// or WaitGroup. The handlers never block and never take turnMu.
func (p *claudeProcess) dispatch(deliver func(*claudeTurn)) {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	if p.activeTurn != nil {
		deliver(p.activeTurn)
	}
}

func (p *claudeProcess) getErrors() (scanErr, exitErr error) {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	return p.scanErr, p.exitErr
}

func (p *claudeProcess) closeStdin() {
	p.stdinOnce.Do(func() { _ = p.stdin.Close() })
}

// clearTaskEnv drops the finished turn's credentials from a pooled process's
// overlay so nothing it does while idle can act as that task.
func (p *claudeProcess) clearTaskEnv() {
	if p.taskEnvFile != "" {
		_ = ClearTaskEnvFile(p.taskEnvFile)
	}
}

// Close shuts the process down and removes its temp files. It is also the
// release step for a one-shot process that has already exited.
func (p *claudeProcess) Close() bool {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		p.clearTaskEnv()
		p.closeStdin()

		// Give the CLI a brief window to exit cleanly on stdin EOF before
		// signalling the process group.
		select {
		case <-p.procDone:
		case <-time.After(1500 * time.Millisecond):
			if p.cmd.Process != nil {
				p.signalProcessTree()
			}
			<-p.procDone
		}

		if p.mcpConfigPath != "" {
			cleanupMcpConfigTemp(p.mcpConfigPath)
		}
		if p.sessionDir != "" {
			_ = os.RemoveAll(p.sessionDir)
		}
	})
	return true
}

// terminate stops the process on cancellation or timeout. It EOFs stdin to
// nudge a clean exit, then signals the whole process group so every MCP server
// and tool subprocess claude spawned goes with it.
func (p *claudeProcess) terminate() {
	p.closed.Store(true)
	p.closeStdin()
	if p.cmd.Process != nil {
		p.signalProcessTree()
	}
}

// stopAfterErrorResult terminates a one-shot claude that outlives its error
// result past the grace period, the same way cancellation does.
func (p *claudeProcess) stopAfterErrorResult() {
	select {
	case <-p.procDone:
		return
	case <-time.After(claudeErrorResultGrace()):
	}
	p.logger.Warn("claude still running after an error result; terminating process group", "pid", p.cmd.Process.Pid)
	p.signalProcessTree()
}

// signalProcessTree SIGTERMs the process group, escalates to SIGKILL unless
// the whole group exits within the grace window, then closes stdout to unblock
// a reader a wedged descendant still holds open.
//
// The escalation keys off the process group, not procDone: procDone only means
// cmd.Wait() returned for the leader, so a SIGTERM-ignoring descendant that
// does not hold claude's stdout would otherwise skip the SIGKILL and leak.
// SIGKILL is uncatchable, so once delivered no group member can write again
// and closing stdout is safe. WaitDelay is the final backstop (#5918).
func (p *claudeProcess) signalProcessTree() {
	signalProcessGroup(p.cmd, syscall.SIGTERM)
	if !waitProcessGroupGone(p.cmd, claudeTerminateGrace()) {
		signalProcessGroup(p.cmd, syscall.SIGKILL)
	}
	_ = p.stdout.Close()
}

func (b *claudeBackend) spawnClaudeProcess(ctx context.Context, execPath string, opts ExecOptions, pooled bool, fingerprint string) (*claudeProcess, *claudeUsageSnapshot, error) {
	var sessionDir, taskEnvFile string
	childEnv := b.cfg.Env
	if pooled {
		var err error
		sessionDir, err = os.MkdirTemp("", "multica-claude-warm-*")
		if err != nil {
			return nil, nil, fmt.Errorf("create warm claude session dir: %w", err)
		}
		sessionTempDir := filepath.Join(sessionDir, "tmp")
		if err := os.MkdirAll(sessionTempDir, 0o700); err != nil {
			_ = os.RemoveAll(sessionDir)
			return nil, nil, fmt.Errorf("create warm claude temp dir: %w", err)
		}
		taskEnvFile = filepath.Join(sessionDir, "task-env.json")
		if err := WriteTaskEnvFile(taskEnvFile, b.cfg.Env); err != nil {
			_ = os.RemoveAll(sessionDir)
			return nil, nil, fmt.Errorf("write warm claude task env overlay: %w", err)
		}
		childEnv = PreparePooledChildEnv(b.cfg.Env, taskEnvFile, sessionTempDir)
	}

	args := buildClaudeArgs(opts, b.cfg.Logger)

	// If the caller provided an MCP config, write it to a temp file and pass
	// --mcp-config <path> so the agent uses a controlled set of MCP servers
	// instead of inheriting from the outer Claude Code session.
	var mcpConfigPath string
	if hasManagedMcpConfig(opts.McpConfig) {
		path, err := writeMcpConfigToTemp(opts.McpConfig)
		if err != nil {
			if sessionDir != "" {
				_ = os.RemoveAll(sessionDir)
			}
			return nil, nil, err
		}
		mcpConfigPath = path
		args = append(args, "--mcp-config", mcpConfigPath)
	}

	cleanupOnErr := func() {
		if mcpConfigPath != "" {
			cleanupMcpConfigTemp(mcpConfigPath)
		}
		if sessionDir != "" {
			_ = os.RemoveAll(sessionDir)
		}
	}

	cmd := b.cfg.commandAt(execPath).exec(ctx, args...)
	hideAgentWindow(cmd)
	// Take over context cancellation: the default kills the whole group the
	// instant ctx is done. claudeProcess.terminate instead drives a graceful
	// group-wide SIGTERM→SIGKILL and closes stdout only after the tree has been
	// signalled. Returning nil keeps os/exec from racing it with its own kill;
	// WaitDelay remains the hard backstop.
	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(childEnv)
	if err := claudeRootSudoPreflight(args, cmd.Env); err != nil {
		cleanupOnErr()
		return nil, nil, err
	}

	var usageSnapshot *claudeUsageSnapshot
	if opts.ResumeSessionID != "" {
		snapshot, snapshotErr := captureClaudeUsageSnapshot(cmd.Env, cmd.Dir, opts.ResumeSessionID)
		if snapshotErr != nil {
			b.cfg.Logger.Warn("claude usage baseline unavailable; ignoring reported session totals, keeping per-run stream usage", "error", snapshotErr)
		} else {
			usageSnapshot = snapshot
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanupOnErr()
		return nil, nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cleanupOnErr()
		return nil, nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	// Capture stderr into both the daemon log and a bounded tail buffer so we
	// can include the last few KB in Result.Error when claude exits
	// unexpectedly. Without the tail, an exit-code-only failure looks like
	// "claude exited with error: exit status 3" — which is useless for
	// root-causing V8 aborts, Bun panics, or any other CLI-side crash.
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		_ = stdin.Close()
		cleanupOnErr()
		return nil, nil, fmt.Errorf("start claude: %w", err)
	}

	b.cfg.Logger.Info("claude started", "warm_pool", pooled, "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	proc := &claudeProcess{
		pooled:        pooled,
		fingerprint:   fingerprint,
		cmd:           cmd,
		stdin:         stdin,
		inputWriter:   &claudeInputWriter{w: stdin},
		stdout:        stdout,
		stderrBuf:     stderrBuf,
		procDone:      make(chan struct{}),
		mcpConfigPath: mcpConfigPath,
		sessionDir:    sessionDir,
		taskEnvFile:   taskEnvFile,
		logger:        b.cfg.Logger,
	}
	return proc, usageSnapshot, nil
}

// readStdout routes every stdout event to the active turn for the life of the
// process, then reaps it. procDone closes only after the exit status is
// recorded. The caller starts it once the first turn is attached.
func (p *claudeProcess) readStdout() {
	defer close(p.procDone)
	scanner := newAgentStreamScanner(p.stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg claudeSDKMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			p.dispatch((*claudeTurn).noteInvalidEvent)
			continue
		}
		p.dispatch(func(turn *claudeTurn) { turn.handleMessage(msg) })
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		// Scanner stopped consuming stdout. Close the pipe before Wait so a
		// child still writing a malformed/oversized event cannot deadlock on
		// the full OS pipe; the scanner error remains the primary failure.
		_ = p.stdout.Close()
	}
	p.closeStdin()
	exitErr := p.cmd.Wait()
	// The leader is reaped; drop ownership. On Windows that closes the Job
	// Object, which kills anything still inside it — precisely what should
	// happen to a descendant that outlived the CLI (GH #7522).
	releaseProcessGroup(p.cmd)
	p.turnMu.Lock()
	p.scanErr = scanErr
	p.exitErr = exitErr
	p.turnMu.Unlock()
}

// warmFingerprint hashes every session-immutable launch input. It omits
// --resume so the process a first turn spawned and the follow-up turn that
// resumes its session share one key.
func (b *claudeBackend) warmFingerprint(execPath string, opts ExecOptions) string {
	optsForFP := opts
	optsForFP.ResumeSessionID = ""
	var configFiles []string
	if opts.ClaudeSettingsPath != "" {
		configFiles = append(configFiles, opts.ClaudeSettingsPath)
	}
	return computeWarmSessionFingerprint(WarmSessionFingerprintInput{
		Provider:      "claude",
		Executable:    execPath,
		LaunchPrefix:  b.cfg.LaunchPrefix,
		Args:          buildClaudeArgs(optsForFP, b.cfg.Logger),
		Cwd:           opts.Cwd,
		Model:         opts.Model,
		ThinkingLevel: opts.ThinkingLevel,
		McpConfig:     opts.McpConfig,
		ConfigFiles:   configFiles,
		BriefHash:     opts.WarmPoolBriefHash,
		Env:           b.cfg.Env,
		ExtraFlags:    []string{fmt.Sprintf("supplement=%t", opts.EnableTaskSupplement)},
	})
}

// acquireWarmProcess checks out the process parked for sessionID and hands it
// this task's credentials, or returns nil when the run must spawn.
func (b *claudeBackend) acquireWarmProcess(sessionID, fingerprint string) *claudeProcess {
	if sessionID == "" {
		return nil
	}
	acquired := b.cfg.WarmPool.Acquire("claude", sessionID, fingerprint)
	if acquired == nil {
		return nil
	}
	proc, ok := acquired.(*claudeProcess)
	if !ok {
		acquired.Close()
		return nil
	}
	if err := WriteTaskEnvFile(proc.taskEnvFile, b.cfg.Env); err != nil {
		b.cfg.Logger.Warn("warm claude: failed to refresh task env overlay; evicting", "error", err)
		proc.Close()
		return nil
	}
	return proc
}
