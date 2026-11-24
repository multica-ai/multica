package agent

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"sync/atomic"
	"time"
)

type antigravityProcess struct {
	stdout     io.ReadCloser
	streamDone chan struct{}
	completed  chan struct{}
	terminal   atomic.Bool
	wait       func() antigravityProcessExit
}

type antigravityProcessExit struct {
	err        error
	cleanupErr error
}

func startAntigravityProcess(ctx context.Context, cmd *exec.Cmd, stderr io.Writer, logger *slog.Logger, exitGrace time.Duration, manageCleanup bool) (*antigravityProcess, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &antigravityProcess{stdout: stdout}
	if manageCleanup {
		managed, err := p.startManaged(ctx, cmd, stderr, logger, exitGrace)
		if err != nil {
			_ = stdout.Close()
			return nil, err
		}
		if managed {
			return p, nil
		}
	}

	cmd.Stderr = stderr
	if err := startOwnedProcessTree(cmd, logger); err != nil {
		_ = stdout.Close()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = stdout.Close()
	}()
	p.wait = func() antigravityProcessExit {
		err := cmd.Wait()
		releaseProcessGroup(cmd)
		return antigravityProcessExit{err: err}
	}
	return p, nil
}

func (p *antigravityProcess) observeSuccess(ctx context.Context) {
	if p.completed != nil && ctx.Err() == nil && p.terminal.CompareAndSwap(false, true) {
		close(p.completed)
	}
}

func (p *antigravityProcess) finish() antigravityProcessExit {
	if p.streamDone != nil {
		close(p.streamDone)
	}
	return p.wait()
}
