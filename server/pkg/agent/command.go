package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// CommandRequest describes one task-scoped child process. Env is the complete
// child environment; daemon process environment variables are never inherited.
// LeadingExtraFiles occupy child FDs starting at 3 before isolation descriptors.
type CommandRequest struct {
	Executable          string
	Args                []string
	Cwd                 string
	Env                 map[string]string
	Isolation           *TaskIsolationPolicy
	WaitDelay           time.Duration
	LeadingExtraFiles   []*os.File
}

// CommandBuilder constructs one validated task process. Production callers use
// CommandLauncher; the interface exists so daemon tests can exercise runTask
// without nesting the host isolation facility inside another sandbox.
type CommandBuilder interface {
	Command(context.Context, CommandRequest) (*PreparedCommand, error)
}

// CommandLauncher is the only supported constructor for task-time agent
// processes. It validates the complete request before returning a command.
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

// PreparedCommand is the only supported handle for a validated task process.
// It embeds *exec.Cmd so existing backends can configure pipes/env/cancel while
// still going through Start/Wait/Output/CombinedOutput for launch-time recheck
// and isolation resource release.
type PreparedCommand struct {
	*exec.Cmd
	resources []io.Closer
	recheck   func() error
	closeOnce sync.Once
	closeErr  error
	started   bool
}

// Command validates and wraps a task-time command without starting it.
func (l *CommandLauncher) Command(ctx context.Context, req CommandRequest) (*PreparedCommand, error) {
	if l == nil || l.isolation == nil {
		return nil, fmt.Errorf("command launcher isolation is unavailable")
	}
	if req.Isolation == nil {
		return nil, fmt.Errorf("command isolation policy is required")
	}

	executablePath, err := validateAbsolutePath("executable", req.Executable)
	if err != nil {
		return nil, err
	}
	cwdPath, err := validateAbsolutePath("cwd", req.Cwd)
	if err != nil {
		return nil, err
	}

	bound, err := bindTaskIsolationPolicy(*req.Isolation)
	if err != nil {
		return nil, fmt.Errorf("validate command isolation policy: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			_ = bound.Close()
		}
	}()

	cwd, err := currentWorkingDirectoryIdentity(cwdPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if owned {
			_ = cwd.Close()
		}
	}()
	executable, err := executableIdentity(executablePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if owned {
			_ = executable.Close()
		}
	}()

	allowedCwd := append(append([]pathIdentity{}, bound.WritableRoots...), bound.ReadOnlyRoots...)
	if !identityWithinAny(cwd.Path, allowedCwd) {
		return nil, fmt.Errorf("cwd %q is outside task roots", cwd.Path)
	}
	allowedExec := append(append(append([]pathIdentity{}, bound.WritableRoots...), bound.ReadOnlyRoots...), bound.SystemRoots...)
	if !identityWithinAny(executable.Path, allowedExec) {
		return nil, fmt.Errorf("executable %q is outside isolated roots", executable.Path)
	}

	if err := recheckBoundIsolation(bound); err != nil {
		return nil, err
	}
	if err := recheckPathIdentity(&cwd); err != nil {
		return nil, err
	}
	if err := recheckPathIdentity(&executable); err != nil {
		return nil, err
	}

	leading := append([]*os.File(nil), req.LeadingExtraFiles...)
	wrappedExecutable, wrappedArgs, isolationFiles, err := l.isolation.WrapBound(bound, executable, cwd, append([]string(nil), req.Args...), len(leading))
	if err != nil {
		return nil, fmt.Errorf("apply command isolation: %w", err)
	}

	cmd := exec.CommandContext(ctx, wrappedExecutable, wrappedArgs...)
	// Linux bubblewrap starts from "/" and enters the validated CWD via --chdir.
	// Darwin seatbelt is pathname-based and has no object-bound chdir, so the
	// revalidated CWD path is used as cmd.Dir and rechecked again at Start.
	if dir := isolationLaunchDirectory(l.isolation); dir != "" {
		cmd.Dir = dir
	} else {
		cmd.Dir = cwd.Path
	}
	cmd.Env, err = explicitEnvironment(req.Env)
	if err != nil {
		return nil, err
	}
	cmd.WaitDelay = req.WaitDelay
	if len(leading) > 0 || len(isolationFiles) > 0 {
		cmd.ExtraFiles = append(leading, isolationFiles...)
	}
	configureProcessGroup(cmd)
	hideAgentWindow(cmd)

	recheck := func() error {
		if err := recheckBoundIsolation(bound); err != nil {
			return err
		}
		if err := recheckPathIdentity(&cwd); err != nil {
			return err
		}
		return recheckPathIdentity(&executable)
	}

	prepared := &PreparedCommand{
		Cmd:       cmd,
		resources: joinIdentityResources(bound, &cwd, &executable),
		recheck:   recheck,
	}
	owned = false
	return prepared, nil
}

func (p *PreparedCommand) Start() error {
	if p == nil || p.Cmd == nil {
		return fmt.Errorf("prepared command is nil")
	}
	if p.started {
		return fmt.Errorf("prepared command already started")
	}
	if p.recheck != nil {
		if err := p.recheck(); err != nil {
			_ = p.Close()
			return err
		}
	}
	err := p.Cmd.Start()
	// Isolation descriptors are only required until the helper is launched.
	// After Start returns, the child either inherited them or failed to start.
	_ = p.Close()
	if err != nil {
		return err
	}
	p.started = true
	return nil
}

func (p *PreparedCommand) Wait() error {
	if p == nil || p.Cmd == nil {
		return fmt.Errorf("prepared command is nil")
	}
	err := p.Cmd.Wait()
	_ = p.Close()
	return err
}

func (p *PreparedCommand) Run() error {
	if err := p.Start(); err != nil {
		return err
	}
	return p.Wait()
}

func (p *PreparedCommand) Output() ([]byte, error) {
	if p == nil || p.Cmd == nil {
		return nil, fmt.Errorf("prepared command is nil")
	}
	if p.Stdout != nil {
		return nil, fmt.Errorf("exec: Stdout already set")
	}
	var stdout bytes.Buffer
	p.Stdout = &stdout

	if p.Stderr == nil {
		var stderr bytes.Buffer
		p.Stderr = &stderr
		err := p.Run()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				ee.Stderr = stderr.Bytes()
				return stdout.Bytes(), ee
			}
			return stdout.Bytes(), err
		}
		return stdout.Bytes(), nil
	}
	err := p.Run()
	return stdout.Bytes(), err
}

func (p *PreparedCommand) CombinedOutput() ([]byte, error) {
	if p == nil || p.Cmd == nil {
		return nil, fmt.Errorf("prepared command is nil")
	}
	if p.Stdout != nil {
		return nil, fmt.Errorf("exec: Stdout already set")
	}
	if p.Stderr != nil {
		return nil, fmt.Errorf("exec: Stderr already set")
	}
	var b bytes.Buffer
	p.Stdout = &b
	p.Stderr = &b
	err := p.Run()
	return b.Bytes(), err
}

func (p *PreparedCommand) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		p.closeErr = closeAll(p.resources...)
		p.resources = nil
		p.recheck = nil
	})
	return p.closeErr
}

func (c Config) command(ctx context.Context, executable string, args []string, cwd string, waitDelay time.Duration) (*PreparedCommand, error) {
	return c.commandWithLeadingExtraFiles(ctx, executable, args, cwd, waitDelay, nil)
}

func (c Config) commandWithLeadingExtraFiles(ctx context.Context, executable string, args []string, cwd string, waitDelay time.Duration, leading []*os.File) (*PreparedCommand, error) {
	if c.Launcher == nil {
		return nil, fmt.Errorf("agent command launcher is required")
	}
	return c.Launcher.Command(ctx, CommandRequest{
		Executable:        executable,
		Args:              args,
		Cwd:               cwd,
		Env:               c.Env,
		Isolation:         c.Isolation,
		WaitDelay:         waitDelay,
		LeadingExtraFiles: leading,
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
