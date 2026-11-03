package claude

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// Command is how to start one adapter process: the packaged Electron binary
// run as Node with the claude-host entry.
type Command struct {
	Path string
	Args []string
	Env  []string
	Dir  string
}

// AdapterCommand builds the command for electron and entry. The daemon's own
// ELECTRON_* variables are dropped so Desktop's Electron configuration never
// leaks into the adapter.
func AdapterCommand(electron, entry string, env []string) Command {
	out := []string{"ELECTRON_RUN_AS_NODE=1"}
	for _, kv := range env {
		if !strings.HasPrefix(kv, "ELECTRON_") {
			out = append(out, kv)
		}
	}
	return Command{Path: electron, Args: []string{entry}, Env: out}
}

const (
	readyTimeout = 30 * time.Second
	closeTimeout = 10 * time.Second
	killWait     = 5 * time.Second
)

// Process is one running adapter.
type Process struct {
	*Conn
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	exited chan struct{}
	exitMu sync.Mutex
	exit   error
	Ready  Ready
	// spawnTook and readyTook are set before Start returns.
	spawnTook, readyTook time.Duration
}

// StartDurations is how long launching the process and its ready frame took.
func (p *Process) StartDurations() (spawn, ready time.Duration) {
	return p.spawnTook, p.readyTook
}

// Supports reports whether the adapter announced a capability.
func (p *Process) Supports(capability string) bool {
	return slices.Contains(p.Ready.Capabilities, capability)
}

// Start launches the adapter and waits for its ready frame.
func Start(ctx context.Context, command Command, handler Handler, logger *slog.Logger) (*Process, error) {
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = command.Env
	cmd.Dir = command.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	began := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude adapter: %w", err)
	}
	p := &Process{cmd: cmd, stdin: stdin, exited: make(chan struct{}), spawnTook: time.Since(began)}
	p.Conn = NewConn(stdout, stdin, handler)
	stderrDone := make(chan struct{})
	go func() { defer close(stderrDone); drainStderr(stderr, logger) }()
	go func() {
		// Wait closes the pipes, so every read must finish first. A reader
		// that stopped on a protocol error leaves the process running; kill it.
		<-p.Conn.readDone
		if errors.Is(p.Err(), ErrProtocol) {
			_ = cmd.Process.Kill()
		}
		<-stderrDone
		err := cmd.Wait()
		p.exitMu.Lock()
		p.exit = err
		p.exitMu.Unlock()
		close(p.exited)
		p.Fail(fmt.Errorf("%w: adapter exited: %v", ErrClosed, err))
	}()

	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	waiting := time.Now()
	ready, err := p.WaitReady(readyCtx)
	if err != nil {
		p.kill()
		return nil, fmt.Errorf("claude adapter did not become ready: %w", err)
	}
	p.Ready = ready
	p.readyTook = time.Since(waiting)
	return p, nil
}

// Close asks the adapter to end the session so Claude Code persists its
// transcript, then closes stdin; a process that does not exit in time is
// killed.
func (p *Process) Close(ctx context.Context) error {
	closeCtx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()
	_, reqErr := p.Request(closeCtx, "close", nil)
	_ = p.stdin.Close()
	select {
	case <-p.exited:
	case <-time.After(killWait):
		p.kill()
	}
	if reqErr != nil && !errors.Is(reqErr, ErrClosed) {
		return reqErr
	}
	return nil
}

// Exited is closed when the process has exited.
func (p *Process) Exited() <-chan struct{} { return p.exited }

// ExitErr is the process exit status once Exited is closed.
func (p *Process) ExitErr() error {
	p.exitMu.Lock()
	defer p.exitMu.Unlock()
	return p.exit
}

func (p *Process) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	<-p.exited
}

// drainStderr forwards adapter diagnostics to the log. The adapter never
// writes prompts, tool input or credentials there.
func drainStderr(r io.Reader, logger *slog.Logger) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && logger != nil {
			logger.Debug("claude adapter stderr", "text", strings.TrimSpace(string(buf[:n])))
		}
		if err != nil {
			return
		}
	}
}
