package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	codexThreadLifecycleTimeout = 15 * time.Second
	codexLifecycleCleanupGrace  = 3 * time.Second
)

func (b *codexBackend) ArchiveThread(ctx context.Context, threadID string, opts ExecOptions) error {
	return b.runThreadLifecycleRPC(ctx, "thread/archive", threadID, opts)
}

func (b *codexBackend) UnarchiveThread(ctx context.Context, threadID string, opts ExecOptions) error {
	return b.runThreadLifecycleRPC(ctx, "thread/unarchive", threadID, opts)
}

// runThreadLifecycleRPC starts a bounded, archive-only app-server. The task's
// execution app-server is deliberately gone before the daemon persists the
// terminal result, so reusing it would invert the required ordering. This
// one-shot process reuses the exact runtime command, launch prefix, effective
// args, environment and CODEX_HOME, but never starts a turn or handles a tool.
func (b *codexBackend) runThreadLifecycleRPC(parent context.Context, method, threadID string, opts ExecOptions) error {
	if method != "thread/archive" && method != "thread/unarchive" {
		return fmt.Errorf("unsupported codex thread lifecycle method")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("codex thread lifecycle requires a thread id")
	}

	ctx, cancel := context.WithTimeout(parent, codexThreadLifecycleTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return codexThreadLifecycleError(method, err)
	}

	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "codex"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return fmt.Errorf("codex thread lifecycle executable unavailable")
	}

	codexHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
	if codexHome != "" {
		if err := ensureCodexMcpConfig(filepath.Join(codexHome, "config.toml"), opts.McpConfig, b.cfg.Logger); err != nil {
			return fmt.Errorf("codex thread lifecycle config unavailable")
		}
	} else if hasManagedCodexMcpConfig(opts.McpConfig) {
		return fmt.Errorf("codex thread lifecycle CODEX_HOME unavailable")
	}

	runtimeCmd := b.cfg.commandAt(execPath)
	if codexHome != "" {
		opts.ExtraArgs = filterCodexShellEnvConfigOverrides(opts.ExtraArgs, b.cfg.Logger)
		opts.CustomArgs = filterCodexShellEnvConfigOverrides(opts.CustomArgs, b.cfg.Logger)
		runtimeCmd = runtimeCmd.withFilteredPrefix(func(prefix []string) []string {
			return filterCodexShellEnvConfigOverrides(prefix, b.cfg.Logger)
		})
	}
	if hasManagedCodexMcpConfig(opts.McpConfig) {
		runtimeCmd = runtimeCmd.withFilteredPrefix(func(prefix []string) []string {
			return filterCodexCustomConfigOverrides(prefix, b.cfg.Logger)
		})
	}
	if opts.ServiceTier == codexFastServiceTier {
		runtimeCmd = runtimeCmd.withFilteredPrefix(func(prefix []string) []string {
			return stripCodexFastModeConflicts(prefix, b.cfg.Logger)
		})
	}

	codexArgs := buildCodexArgs(opts, b.cfg.Logger)
	cmd := runtimeCmd.exec(ctx, codexArgs...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = codexLifecycleCleanupGrace
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(codexArgs, trustAgentCommandPositional(0, "app-server")))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex thread lifecycle stdout unavailable")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("codex thread lifecycle stdin unavailable")
	}
	// Archive failures are operational metadata, not provider diagnostics.
	// Discard stderr so credentials or config values echoed by a custom runtime
	// can never cross into the daemon's structured warning.
	cmd.Stderr = io.Discard

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		return fmt.Errorf("codex thread lifecycle process start failed")
	}

	client := &codexClient{
		cfg:                  b.cfg,
		stdin:                stdin,
		pending:              make(map[int]*pendingRPC),
		processDone:          make(chan struct{}),
		handshakeTimeout:     codexThreadLifecycleTimeout,
		pid:                  cmd.Process.Pid,
		rejectServerRequests: true,
	}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				client.handleLine(line)
			}
		}
		if err := scanner.Err(); err != nil {
			client.markProcessExited(fmt.Errorf("%w: stream read failed", errCodexProcessExited))
			return
		}
		client.markProcessExited(errCodexProcessExited)
	}()

	// Exactly one Wait owns the process. Cleanup first gives stdin EOF a short
	// graceful chance, then cancels/kills the whole process group and waits
	// within a fixed post-RPC grace. The lifecycle RPC's 15 s deadline plus this
	// grace stays below the daemon's 30 s task-drain window.
	defer func() {
		_ = stdin.Close()
		graceTimer := time.NewTimer(codexLifecycleCleanupGrace / 3)
		select {
		case <-readerDone:
			if !graceTimer.Stop() {
				select {
				case <-graceTimer.C:
				default:
				}
			}
		case <-graceTimer.C:
			cancel()
			signalProcessGroup(cmd, syscall.SIGKILL)
		}

		waitDone := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(waitDone)
		}()
		waitTimer := time.NewTimer(codexLifecycleCleanupGrace)
		select {
		case <-waitDone:
			if !waitTimer.Stop() {
				select {
				case <-waitTimer.C:
				default:
				}
			}
		case <-waitTimer.C:
			cancel()
			signalProcessGroup(cmd, syscall.SIGKILL)
			<-waitDone
		}
		releaseProcessGroup(cmd)
	}()

	if _, err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "multica-agent-sdk",
			"title":   "Multica Agent SDK",
			"version": "0.2.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return codexThreadLifecycleError("initialize", err)
	}
	client.notify("initialized")
	if _, err := client.request(ctx, method, map[string]any{"threadId": threadID}); err != nil {
		return codexThreadLifecycleError(method, err)
	}
	return nil
}

func codexThreadLifecycleError(phase string, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return fmt.Errorf("codex thread lifecycle %s timed out", phase)
	case errors.Is(err, errCodexProcessExited):
		return fmt.Errorf("codex thread lifecycle %s process exited", phase)
	default:
		// Do not include raw JSON-RPC/provider text. It can be emitted by a
		// custom runtime and may contain auth or configuration material.
		return fmt.Errorf("codex thread lifecycle %s rejected", phase)
	}
}
