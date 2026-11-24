package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const antigravityCleanupSettle = 2 * time.Second

func (p *antigravityProcess) startManaged(ctx context.Context, cmd *exec.Cmd, stderr io.Writer, logger *slog.Logger, exitGrace time.Duration) (bool, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		logger.Warn("agy bounded cleanup unavailable", "err", err)
		return false, nil
	}
	unix.CloseOnExec(queue)
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = unix.Close(queue)
		return false, err
	}
	// The controller sends every group signal before Wait reaps the leader.
	// Keeping os/exec's cancellation hook would bypass that ownership boundary.
	cmd.Cancel = nil
	cmd.WaitDelay = 0
	if err := startOwnedProcessTree(cmd, logger); err != nil {
		_ = unix.Close(queue)
		_ = errPipe.Close()
		return false, err
	}

	errDrained := make(chan struct{})
	go func() {
		defer close(errDrained)
		_, _ = io.Copy(stderr, errPipe)
	}()
	p.streamDone, p.completed = make(chan struct{}), make(chan struct{})
	exitReady := make(chan error, 1)
	go func() {
		defer unix.Close(queue)
		exitReady <- observeAntigravityExit(queue, cmd.Process.Pid)
	}()
	result := make(chan antigravityProcessExit, 1)
	go func() {
		result <- p.manageExit(ctx, cmd, errPipe, exitReady, errDrained, exitGrace, logger)
	}()
	p.wait = func() antigravityProcessExit { return <-result }
	return true, nil
}

func (p *antigravityProcess) manageExit(ctx context.Context, cmd *exec.Cmd, stderr io.Closer, leader <-chan error, stderrDone <-chan struct{}, grace time.Duration, logger *slog.Logger) antigravityProcessExit {
	stdoutDone, terminal := (<-chan struct{})(p.streamDone), (<-chan struct{})(p.completed)
	var timer *time.Timer
	var deadline <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	var outcome antigravityProcessExit
	stopping := false
	leaderWasRunning := false
	ctxDone := ctx.Done()
	stop := func() {
		if timer != nil {
			timer.Stop()
		}
		ctxDone, terminal = nil, nil
		stopping = true
		leaderWasRunning = leader != nil
		outcome.cleanupErr = stopAntigravityGroup(cmd.Process.Pid)
		timer = time.NewTimer(antigravityCleanupSettle)
		deadline = timer.C
	}
	for leader != nil || stdoutDone != nil || stderrDone != nil {
		select {
		case err := <-leader:
			leader = nil
			if err != nil {
				// Without an exit observation we cannot safely signal a
				// numeric process group. The direct wait still owns reaping.
				outcome.cleanupErr = fmt.Errorf("observe agy exit: %w", err)
				_ = p.stdout.Close()
				_ = stderr.Close()
				go func() { _ = cmd.Wait() }()
				return outcome
			}
		case <-stdoutDone:
			stdoutDone = nil
		case <-stderrDone:
			stderrDone = nil
		case <-terminal:
			terminal = nil
			timer = time.NewTimer(grace)
			deadline = timer.C
		case <-ctxDone:
			stop()
		case <-deadline:
			if stopping {
				_ = p.stdout.Close()
				_ = stderr.Close()
				if outcome.cleanupErr == nil {
					outcome.cleanupErr = errors.New("agy did not close its process and output streams after cleanup")
				}
				go func() { _ = cmd.Wait() }()
				return outcome
			}
			logger.Warn("agy did not exit after a terminal result; stopping its process group", "pid", cmd.Process.Pid)
			stop()
		}
	}
	outcome.err = cmd.Wait()
	var exit *exec.ExitError
	if leaderWasRunning && outcome.cleanupErr == nil && errors.As(outcome.err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signal() == syscall.SIGKILL {
			outcome.err = nil
		}
	}
	return outcome
}

func observeAntigravityExit(queue, pid int) error {
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	var events [1]unix.Kevent_t
	for {
		n, err := unix.Kevent(queue, []unix.Kevent_t{change}, events[:], nil)
		if err == unix.EINTR {
			continue
		}
		if err == unix.ESRCH {
			// Registration can lose the race with a very short-lived agy.
			// The child is still waitable because our own Wait has not run.
			return nil
		}
		if err != nil {
			return err
		}
		if n == 1 && events[0].Flags&unix.EV_ERROR != 0 {
			return syscall.Errno(events[0].Data)
		}
		if n == 1 && events[0].Fflags&unix.NOTE_EXIT != 0 {
			return nil
		}
	}
}

func stopAntigravityGroup(pgid int) error {
	deadline := time.Now().Add(collectReapWindow)
	for {
		signalErr := unix.Kill(-pgid, unix.SIGKILL)
		members, err := unix.SysctlKinfoProcSlice("kern.proc.all")
		if err != nil {
			return fmt.Errorf("list agy process group members: %w", err)
		}
		active := false
		for _, member := range members {
			if int(member.Eproc.Pgid) == pgid && member.Proc.P_stat != darwinProcessStateZombie {
				active = true
				break
			}
		}
		if !active {
			return nil
		}
		if signalErr != nil && signalErr != unix.ESRCH {
			return fmt.Errorf("stop agy process group: %w", signalErr)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("agy process group %d did not stop", pgid)
		}
		time.Sleep(collectReapStep)
	}
}
