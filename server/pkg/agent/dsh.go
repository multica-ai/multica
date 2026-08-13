package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// dshBackend drives DeepSeek Harness's one-shot headless profile:
//
//	dsh --profile headless "<task>"
//
// The headless bundle creates a fresh persisted session, submits the task as
// one user message, and prints the last non-empty assistant message to stdout;
// it exits 0 when the turn completed and 1 otherwise (a terminal error reason
// also writes "dsh: <code>: <message>" to stderr). See
// @deepseek-ai/dsh-headless in deepseek-ai/deepseek-harness.
//
// The runner has no streaming surface — nothing reaches stdout until the
// process is about to exit — so this backend forwards a periodic "running"
// status while the process lives. That is both the only live UI signal and
// the daemon's liveness contract: the no-message idle watchdog force-stops a
// run that emits nothing, so a long DeepSeek turn would otherwise be killed
// mid-flight (default window: MULTICA_AGENT_IDLE_WATCHDOG, 30 minutes).
type dshBackend struct {
	cfg Config
}

// dshHeartbeatInterval is the default how often the backend forwards a bare
// "running" status while the dsh process is alive. Five minutes sits well
// inside the 30-minute default no-message watchdog window with room for slow
// Node startup, and the daemon drains bare status messages without persisting
// them, so the transcript is not polluted.
const dshHeartbeatInterval = 5 * time.Minute

// dshHeartbeatIntervalFor narrows the heartbeat to the daemon's per-run
// no-message budget when that budget is configured below the default. The
// daemon passes ExecOptions.IdleWatchdogTimeout for exactly this: a backend
// that emits nothing (dsh prints only at exit) must stay audible inside the
// window, whatever the operator set it to. Half the window leaves room for
// jitter; a zero/absent window keeps the default interval.
func dshHeartbeatIntervalFor(opts ExecOptions) time.Duration {
	if opts.IdleWatchdogTimeout > 0 {
		interval := opts.IdleWatchdogTimeout / 2
		if interval < dshHeartbeatInterval {
			return interval
		}
	}
	return dshHeartbeatInterval
}

// dshModelPatchProvider is the provider key the model-override patch restates.
// The shipped headless profile defaults to provider "deepseek-official"; the
// patch replaces the whole agent-default-model row config, so the provider
// must be restated for the model override to hold. A user who customised
// their profile's provider should leave the Multica model unset and let DSH's
// own configuration decide.
const dshModelPatchProvider = "deepseek-official"

// dshModelIDRe constrains model ids written into the YAML patch. DeepSeek
// model ids are simple slugs that start with a letter or digit
// (deepseek-chat, deepseek-v4-flash, ...); failing closed on anything else
// keeps a hostile model string from injecting YAML into the patch file or
// smuggling launcher flags through --patch.
var dshModelIDRe = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._-]*$")

// buildDshArgs assembles the launcher argv for one headless run. When a model
// is selected it writes a temp --patch overlay that overrides
// agent-default-model, and the caller must remove the returned path (empty
// when no patch was written) once the process exits.
//
// ExtraArgs and CustomArgs are deliberately NOT appended. The headless app
// takes its whole task from the positional tokens after the launcher flags —
// headless-startup joins every remaining token into the task text — and any
// unrecognised option token makes commander exit with an error. Appending
// custom arguments would therefore either corrupt the prompt or fail the run,
// so this runtime simply has no custom-args surface.
func buildDshArgs(prompt string, opts ExecOptions, logger *slog.Logger) ([]string, string, error) {
	args := []string{"--profile", "headless"}
	var patchPath string
	if opts.Model != "" {
		path, err := writeDshModelPatch(opts.Model)
		if err != nil {
			return nil, "", fmt.Errorf("write dsh model patch: %w", err)
		}
		patchPath = path
		args = append(args, "--patch", patchPath)
		logger.Info("dsh model override via --patch", "model", opts.Model)
	}
	args = append(args, prompt)
	return args, patchPath, nil
}

// writeDshModelPatch materialises a 0600 temp patch overlay that replaces the
// agent-default-model row's config with the selected model. The patch is a
// file rather than argv so the model never appears in process listings, and
// --patch is a launcher flag so the headless app never sees it.
func writeDshModelPatch(model string) (string, error) {
	if !dshModelIDRe.MatchString(model) {
		return "", fmt.Errorf("invalid dsh model id %q", model)
	}
	content := fmt.Sprintf("- id: agent-default-model\n  config:\n    provider: %s\n    model: %s\n", dshModelPatchProvider, model)
	f, err := os.CreateTemp("", "multica-dsh-model-*.yml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	remove := func() {
		_ = os.Remove(path)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		remove()
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		remove()
		return "", err
	}
	if err := f.Close(); err != nil {
		remove()
		return "", err
	}
	return path, nil
}

// cleanupDshModelPatch removes the temp model patch once the run is over.
func cleanupDshModelPatch(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// Execute implements Backend by spawning "dsh --profile headless <prompt>"
// in the task workdir and reading the final assistant text from stdout.
func (b *dshBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "dsh"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("dsh executable not found at %q: %w", execPath, err)
	}
	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args, patchPath, err := buildDshArgs(prompt, opts, b.cfg.Logger)
	if err != nil {
		cancel()
		return nil, err
	}

	// Windows npm installs dsh as a .cmd/.ps1 launcher pair; route through
	// PowerShell -File dsh.ps1 so Go passes each argv element as a discrete
	// token and cmd.exe %* cannot re-tokenise the multi-line task prompt.
	argv0, fullArgs, ok := platformDshInvocation(execPath, args, b.cfg.Logger)
	if !ok {
		argv0 = execPath
		fullArgs = args
	}
	cmd := exec.CommandContext(runCtx, argv0, fullArgs...)
	hideAgentWindow(cmd)
	// args contain the task prompt; never expose it in daemon logs.
	b.cfg.Logger.Info("agent command", "exec", execPath, "provider", "dsh")
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanupDshModelPatch(patchPath)
		return nil, fmt.Errorf("dsh stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[dsh:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		cleanupDshModelPatch(patchPath)
		return nil, fmt.Errorf("start dsh: %w", err)
	}
	b.cfg.Logger.Info("dsh started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer cleanupDshModelPatch(patchPath)

		started := time.Now()
		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		// Heartbeat: keep the daemon's no-message idle watchdog fed while dsh
		// runs without emitting anything. The interval tracks the watchdog
		// window the daemon configured for this run (see
		// dshHeartbeatIntervalFor).
		heartbeat := time.NewTicker(dshHeartbeatIntervalFor(opts))
		defer heartbeat.Stop()
		heartbeatDone := make(chan struct{})
		defer close(heartbeatDone)
		go func() {
			for {
				select {
				case <-heartbeat.C:
					trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
				case <-heartbeatDone:
					return
				}
			}
		}()

		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), dshMaxOutputBytes+1)
		var output strings.Builder
		for scanner.Scan() {
			output.WriteString(scanner.Text())
			output.WriteString("\n")
		}
		scanErr := scanner.Err()
		exitErr := cmd.Wait()
		duration := time.Since(started)

		status, errMsg := "completed", ""
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			status = "timeout"
			errMsg = fmt.Sprintf("dsh timed out after %s", timeout)
		case errors.Is(runCtx.Err(), context.Canceled):
			status = "aborted"
			errMsg = "execution cancelled"
		case scanErr != nil:
			status = "failed"
			errMsg = fmt.Sprintf("dsh stdout read error: %v", scanErr)
		case exitErr != nil:
			status = "failed"
			errMsg = fmt.Sprintf("dsh exited with error: %v", exitErr)
		}
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "dsh", stderrBuf.Tail())
		}
		outputText := strings.TrimSuffix(output.String(), "\n")
		// The transcript keeps whatever dsh printed even on failure, but the
		// terminal contract is fail-closed: Result.Output stays empty unless
		// the run completed, so upstream fallbacks never mistake a partial
		// answer for a final one.
		if outputText != "" {
			trySend(msgCh, Message{Type: MessageText, Content: outputText})
		}
		b.cfg.Logger.Info("dsh finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status:     status,
			Output:     outputText,
			Error:      errMsg,
			DurationMs: duration.Milliseconds(),
		}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

// dshMaxOutputBytes bounds a single stdout line from dsh. The final answer is
// written as one text block, so a longer line means the CLI is misbehaving or
// a future version changed shape — fail loudly rather than truncate.
const dshMaxOutputBytes = 2 << 20
