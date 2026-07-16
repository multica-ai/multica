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
	Executable        string
	Args              []string
	Cwd               string
	Env               map[string]string
	Isolation         *TaskIsolationPolicy
	WaitDelay         time.Duration
	LeadingExtraFiles []*os.File
}

// CommandBuilder constructs one validated task process. Production callers use
// CommandLauncher; the interface exists so daemon tests can exercise runTask
// without nesting the host isolation facility inside another sandbox.
type CommandBuilder interface {
	Command(context.Context, CommandRequest) (TaskCommand, error)
}

// TaskCommand exposes only the lifecycle and I/O surface required by agent
// backends. The underlying exec.Cmd is intentionally never exposed.
type TaskCommand interface {
	Start() error
	Wait() error
	Run() error
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
	Close() error
	StdoutPipe() (io.ReadCloser, error)
	StdinPipe() (io.WriteCloser, error)
	StderrPipe() (io.ReadCloser, error)
	SetStderr(io.Writer) error
	SetCancel(func() error) error
	Process() *os.Process
	ProcessState() *os.ProcessState
	Environment() []string
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

// PreparedCommand is the only production handle for a validated task process.
type PreparedCommand struct {
	cmd       *exec.Cmd
	resources []io.Closer
	recheck   func() error
	mu        sync.Mutex
	state     preparedCommandState
	closeErr  error
}

type preparedCommandState uint8

const (
	preparedCommandReady preparedCommandState = iota
	preparedCommandStarting
	preparedCommandStarted
	preparedCommandWaiting
	preparedCommandWaited
	preparedCommandClosed
	preparedCommandFailed
)

func newPreparedCommand(cmd *exec.Cmd) *PreparedCommand {
	return &PreparedCommand{cmd: cmd, state: preparedCommandReady}
}

// Command validates and wraps a task-time command without starting it.
func (l *CommandLauncher) Command(ctx context.Context, req CommandRequest) (TaskCommand, error) {
	if l == nil || l.isolation == nil {
		return nil, fmt.Errorf("command launcher isolation is unavailable")
	}
	if req.Isolation == nil {
		return nil, fmt.Errorf("command isolation policy is required")
	}

	executablePath, err := resolveStableSystemPathAlias("executable", req.Executable)
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
	if err := validateProtectedTargetsAgainstCommandPaths(bound.ReadOnlyFiles, executable, cwd); err != nil {
		return nil, err
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
		cmd:       cmd,
		resources: joinIdentityResources(bound, &cwd, &executable),
		recheck:   recheck,
		state:     preparedCommandReady,
	}
	owned = false
	return prepared, nil
}

func (p *PreparedCommand) Start() error {
	if p == nil || p.cmd == nil {
		return fmt.Errorf("prepared command is nil")
	}
	p.mu.Lock()
	if p.state != preparedCommandReady {
		state := p.state
		p.mu.Unlock()
		return fmt.Errorf("prepared command cannot start from state %d", state)
	}
	p.state = preparedCommandStarting
	if p.recheck != nil {
		if err := p.recheck(); err != nil {
			p.state = preparedCommandFailed
			p.releaseLocked()
			p.mu.Unlock()
			return err
		}
	}
	err := p.cmd.Start()
	// Isolation descriptors are only required until the helper is launched.
	// After Start returns, the child either inherited them or failed to start.
	p.releaseLocked()
	if err != nil {
		p.state = preparedCommandFailed
		p.mu.Unlock()
		return err
	}
	p.state = preparedCommandStarted
	p.mu.Unlock()
	return nil
}

func (p *PreparedCommand) Wait() error {
	if p == nil || p.cmd == nil {
		return fmt.Errorf("prepared command is nil")
	}
	p.mu.Lock()
	if p.state != preparedCommandStarted {
		state := p.state
		p.mu.Unlock()
		return fmt.Errorf("prepared command cannot wait from state %d", state)
	}
	p.state = preparedCommandWaiting
	p.mu.Unlock()
	err := p.cmd.Wait()
	p.mu.Lock()
	p.state = preparedCommandWaited
	p.mu.Unlock()
	return err
}

func (p *PreparedCommand) Run() error {
	if err := p.Start(); err != nil {
		return err
	}
	return p.Wait()
}

func (p *PreparedCommand) Output() ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	captureStderr := false
	if err := p.configure(func(cmd *exec.Cmd) error {
		if cmd.Stdout != nil {
			return fmt.Errorf("exec: Stdout already set")
		}
		cmd.Stdout = &stdout
		if cmd.Stderr == nil {
			cmd.Stderr = &stderr
			captureStderr = true
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if captureStderr {
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
	var b bytes.Buffer
	if err := p.configure(func(cmd *exec.Cmd) error {
		if cmd.Stdout != nil {
			return fmt.Errorf("exec: Stdout already set")
		}
		if cmd.Stderr != nil {
			return fmt.Errorf("exec: Stderr already set")
		}
		cmd.Stdout = &b
		cmd.Stderr = &b
		return nil
	}); err != nil {
		return nil, err
	}
	err := p.Run()
	return b.Bytes(), err
}

func (p *PreparedCommand) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == preparedCommandReady {
		p.state = preparedCommandClosed
	}
	p.releaseLocked()
	return p.closeErr
}

func (p *PreparedCommand) releaseLocked() {
	if p.resources == nil {
		return
	}
	p.closeErr = closeAll(p.resources...)
	p.resources = nil
	p.recheck = nil
}

func (p *PreparedCommand) configure(fn func(*exec.Cmd) error) error {
	if p == nil || p.cmd == nil {
		return fmt.Errorf("prepared command is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != preparedCommandReady {
		return fmt.Errorf("prepared command cannot be configured after launch")
	}
	return fn(p.cmd)
}

func (p *PreparedCommand) StdoutPipe() (io.ReadCloser, error) {
	var pipe io.ReadCloser
	err := p.configure(func(cmd *exec.Cmd) (err error) { pipe, err = cmd.StdoutPipe(); return err })
	return pipe, err
}

func (p *PreparedCommand) StdinPipe() (io.WriteCloser, error) {
	var pipe io.WriteCloser
	err := p.configure(func(cmd *exec.Cmd) (err error) { pipe, err = cmd.StdinPipe(); return err })
	return pipe, err
}

func (p *PreparedCommand) StderrPipe() (io.ReadCloser, error) {
	var pipe io.ReadCloser
	err := p.configure(func(cmd *exec.Cmd) (err error) { pipe, err = cmd.StderrPipe(); return err })
	return pipe, err
}

func (p *PreparedCommand) SetStderr(stderr io.Writer) error {
	return p.configure(func(cmd *exec.Cmd) error { cmd.Stderr = stderr; return nil })
}

func (p *PreparedCommand) SetCancel(cancel func() error) error {
	return p.configure(func(cmd *exec.Cmd) error { cmd.Cancel = cancel; return nil })
}

func (p *PreparedCommand) Process() *os.Process {
	if p == nil || p.cmd == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd.Process
}

func (p *PreparedCommand) ProcessState() *os.ProcessState {
	if p == nil || p.cmd == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd.ProcessState
}

func (p *PreparedCommand) Environment() []string {
	if p == nil || p.cmd == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.cmd.Env...)
}

func (c Config) command(ctx context.Context, executable string, args []string, cwd string, waitDelay time.Duration) (TaskCommand, error) {
	return c.commandWithLeadingExtraFiles(ctx, executable, args, cwd, waitDelay, nil)
}

func (c Config) commandWithLeadingExtraFiles(ctx context.Context, executable string, args []string, cwd string, waitDelay time.Duration, leading []*os.File) (TaskCommand, error) {
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
