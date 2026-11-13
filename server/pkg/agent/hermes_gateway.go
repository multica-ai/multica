package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// StartHermesTaskGateway owns the foreground gateway for a Windows task home.
// ACP's detached auto-start requires breakaway, which would let tool processes
// escape our kill-on-close job. A foreground owner needs no such exception.
// The caller must first retire any previous owner with StopHermesTaskGateway.
func StartHermesTaskGateway(ctx context.Context, cfg Config, home string) (func(), error) {
	version, err := parseSemver(cfg.CLIVersion)
	if runtime.GOOS != "windows" || home == "" || cfg.Env["HERMES_HOME"] != home ||
		err != nil || version.lessThan(semver{Minor: 22}) {
		return func() {}, nil
	}
	return startHermesTaskGateway(ctx, cfg, home, hermesGatewayReserved)
}

func startHermesTaskGateway(ctx context.Context, cfg Config, home string, reserved func(string) (bool, error)) (func(), error) {
	runCtx, cancel := context.WithCancel(ctx)
	cmd := cfg.commandAt(cfg.ExecutablePath).exec(runCtx, "--profile", "default", "gateway", "run", "--quiet", "--external-supervisor")
	hideAgentWindow(cmd)
	cmd.Env = buildEnv(cfg.Env)
	cmd.Env = append(cmd.Env, "HERMES_DISABLE_LAZY_INSTALLS=1", "PYTHONIOENCODING=utf-8", "HERMES_GATEWAY_DETACHED=1")
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		signalProcessGroup(cmd, syscall.SIGKILL)
		return nil
	}
	// Keep gateway output out of ACP's protocol and avoid retaining unredacted
	// startup output in task errors. Hermes writes diagnostics in the task home.
	if err := startOwnedProcessTree(cmd, cfg.Logger); err != nil {
		cancel()
		return nil, fmt.Errorf("start task Hermes gateway: %w", err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		signalProcessGroup(cmd, syscall.SIGKILL)
		releaseProcessGroup(cmd)
		close(done)
	}()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	fail := func(err error) (func(), error) {
		stop()
		return nil, fmt.Errorf("start task Hermes gateway: %w", err)
	}
	startup, finish := context.WithTimeout(ctx, 60*time.Second)
	defer finish()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-startup.Done():
			return fail(startup.Err())
		case <-done:
			if waitErr == nil {
				waitErr = errors.New("gateway exited before reserving its task home")
			}
			return fail(waitErr)
		case <-tick.C:
			owned, err := reserved(home)
			if err != nil {
				return fail(err)
			}
			if owned {
				// This is a startup reservation, not readiness. ACP performs the
				// authenticated readiness handshake and waits for this owner.
				return stop, nil
			}
		}
	}
}

// StopHermesTaskGateway retires the gateway of a daemon-created Hermes home.
// Gateway-backed ACP detaches its execution owner from the stdio process; it
// must exit before the next task inherits this conversation's home, otherwise
// tool shells retain the preceding task's credentials. In-process ACP creates
// no gateway records, so older Hermes versions need no extra invocation.
func StopHermesTaskGateway(cfg Config, home string) error {
	if home == "" || cfg.Env["HERMES_HOME"] != home {
		return nil
	}
	found := false
	for _, name := range []string{"gateway.pid", "gateway.lock"} {
		fi, err := os.Lstat(filepath.Join(home, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("refusing to stop Hermes through non-local %s", name)
		}
		found = true
	}
	if !found {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stop := func(interrupt bool) ([]byte, error) {
		args := []string{"--profile", "default", "gateway", "stop"}
		if interrupt {
			args = append(args, "--yes")
		}
		cmd := cfg.commandAt(cfg.ExecutablePath).exec(ctx, args...)
		hideAgentWindow(cmd)
		cmd.Env = buildEnv(cfg.Env)
		cmd.Env = append(cmd.Env, "HERMES_DISABLE_LAZY_INSTALLS=1")
		return combinedOutputOwned(cmd, cfg.Logger)
	}
	output, err := stop(true)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 &&
		strings.HasSuffix(strings.TrimSpace(string(output)), "error: unrecognized arguments: --yes") {
		// Old in-process ACP has no interrupt flag. A downgraded conversation
		// can retain a gateway lock from a newer release; only retry parser errors.
		_, err = stop(false)
	}
	if err != nil {
		return fmt.Errorf("stop task Hermes gateway: %w", err)
	}
	return nil
}
