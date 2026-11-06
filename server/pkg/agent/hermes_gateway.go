package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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
