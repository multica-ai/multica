package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const (
	linuxTaskIsolationHelper  = "/usr/bin/bwrap"
	taskExecutionProbeTimeout = 5 * time.Second
)

type taskExecutionProbe struct {
	goos           string
	helper         string
	timeout        time.Duration
	validateHelper func(string) error
	smoke          func(context.Context, string) error
}

// ProbeTaskExecutionCapability verifies that this host can execute tasks with
// the Linux bubblewrap FD-bound isolation contract. Unsupported or uncertain
// environments fail closed.
func ProbeTaskExecutionCapability(ctx context.Context) error {
	return probeTaskExecutionCapability(ctx, taskExecutionProbe{
		goos:           runtime.GOOS,
		helper:         linuxTaskIsolationHelper,
		timeout:        taskExecutionProbeTimeout,
		validateHelper: validateIsolationHelper,
		smoke:          smokeLinuxFDIsolation,
	})
}

func probeTaskExecutionCapability(ctx context.Context, probe taskExecutionProbe) error {
	if probe.goos != "linux" {
		return fmt.Errorf("task execution capability is unsupported on %s", probe.goos)
	}
	if probe.validateHelper == nil || probe.smoke == nil || probe.timeout <= 0 {
		return fmt.Errorf("task execution capability probe is incomplete")
	}
	if err := probe.validateHelper(probe.helper); err != nil {
		return fmt.Errorf("validate Linux task isolation helper: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, probe.timeout)
	defer cancel()
	if err := probe.smoke(probeCtx, probe.helper); err != nil {
		return fmt.Errorf("smoke test Linux FD-bound task isolation: %w", err)
	}
	return nil
}

func smokeLinuxFDIsolation(ctx context.Context, helper string) error {
	root, err := os.MkdirTemp("", "multica-isolation-probe-")
	if err != nil {
		return fmt.Errorf("create probe root: %w", err)
	}
	defer os.RemoveAll(root)

	rootFD, err := os.Open(root)
	if err != nil {
		return fmt.Errorf("open probe root: %w", err)
	}
	defer rootFD.Close()
	usrFD, err := os.Open("/usr")
	if err != nil {
		return fmt.Errorf("open system root: %w", err)
	}
	defer usrFD.Close()

	cmd := exec.CommandContext(ctx, helper,
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
		"--clearenv",
		"--dir", "/probe",
		"--bind-fd", "3", "/probe",
		"--ro-bind-fd", "4", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib64", "/lib64",
		"--chdir", "/probe",
		"--",
		"/usr/bin/true",
	)
	cmd.ExtraFiles = []*os.File{rootFD, usrFD}
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("run %s: %w: %s", helper, err, output)
	}
	return nil
}
