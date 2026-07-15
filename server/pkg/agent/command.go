package agent

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// CommandRequest describes one task-scoped child process. Env is the complete
// child environment; daemon process environment variables are never inherited.
type CommandRequest struct {
	Executable string
	Args       []string
	Cwd        string
	Env        map[string]string
	Isolation  *TaskIsolationPolicy
	WaitDelay  time.Duration
}

// CommandBuilder constructs one validated task process. Production callers use
// CommandLauncher; the interface exists so daemon tests can exercise runTask
// without nesting the host isolation facility inside another sandbox.
type CommandBuilder interface {
	Command(context.Context, CommandRequest) (*exec.Cmd, error)
}

// CommandLauncher is the only supported constructor for task-time agent
// processes. It validates the complete request before returning an exec.Cmd.
type CommandLauncher struct {
	isolation platformIsolation
}

func newCommandLauncher(isolation platformIsolation) *CommandLauncher {
	return &CommandLauncher{isolation: isolation}
}

// NewCommandLauncher returns a launcher backed by the host's mandatory
// kernel-enforced isolation facility. Unsupported hosts fail closed when used.
func NewCommandLauncher() *CommandLauncher {
	return newCommandLauncher(newPlatformIsolation())
}

// Command validates and wraps a task-time command without starting it.
func (l *CommandLauncher) Command(ctx context.Context, req CommandRequest) (*exec.Cmd, error) {
	if l == nil || l.isolation == nil {
		return nil, fmt.Errorf("command launcher isolation is unavailable")
	}
	if req.Isolation == nil {
		return nil, fmt.Errorf("command isolation policy is required")
	}
	executable, err := validateAbsolutePath("executable", req.Executable)
	if err != nil {
		return nil, err
	}
	cwd, err := validateAbsolutePath("cwd", req.Cwd)
	if err != nil {
		return nil, err
	}
	policy, err := req.Isolation.Validated()
	if err != nil {
		return nil, fmt.Errorf("validate command isolation policy: %w", err)
	}
	if !pathWithinAny(cwd, append(policy.WritableRoots, policy.ReadOnlyRoots...)) {
		return nil, fmt.Errorf("cwd %q is outside task roots", cwd)
	}
	if !pathWithinAny(executable, append(append(policy.WritableRoots, policy.ReadOnlyRoots...), policy.SystemRoots...)) {
		return nil, fmt.Errorf("executable %q is outside isolated roots", executable)
	}
	wrappedExecutable, wrappedArgs, err := l.isolation.Wrap(policy, executable, append([]string(nil), req.Args...))
	if err != nil {
		return nil, fmt.Errorf("apply command isolation: %w", err)
	}

	cmd := exec.CommandContext(ctx, wrappedExecutable, wrappedArgs...)
	cmd.Dir = cwd
	cmd.Env, err = explicitEnvironment(req.Env)
	if err != nil {
		return nil, err
	}
	cmd.WaitDelay = req.WaitDelay
	configureProcessGroup(cmd)
	hideAgentWindow(cmd)
	return cmd, nil
}

func (c Config) command(ctx context.Context, executable string, args []string, cwd string, waitDelay time.Duration) (*exec.Cmd, error) {
	if c.Launcher == nil {
		return nil, fmt.Errorf("agent command launcher is required")
	}
	return c.Launcher.Command(ctx, CommandRequest{
		Executable: executable,
		Args:       args,
		Cwd:        cwd,
		Env:        c.Env,
		Isolation:  c.Isolation,
		WaitDelay:  waitDelay,
	})
}

func explicitEnvironment(values map[string]string) ([]string, error) {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return nil, fmt.Errorf("invalid environment key %q", key)
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("environment value for %q contains NUL", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env, nil
}
