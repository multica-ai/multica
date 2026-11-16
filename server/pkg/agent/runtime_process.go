package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ownedRuntimeProcess keeps the Unix leader waitable until descendant cleanup
// finishes, even when a descendant keeps the stream scanner from reaching EOF.
// Every signal from this runtime must go through signal or waitGroupGone so a
// cancellation racing normal completion cannot use the group after Wait.
type ownedRuntimeProcess struct {
	cmd          *exec.Cmd
	exitReady    chan struct{}
	exitObserved chan struct{}
	exitErr      error
	mu           sync.Mutex
	sealed       bool
}

func startOwnedRuntimeProcess(ctx context.Context, cmd *exec.Cmd, logger *slog.Logger, cancellationDone <-chan struct{}, closeOutput func()) (*ownedRuntimeProcess, error) {
	if err := startOwnedProcessTree(cmd, logger); err != nil {
		return nil, err
	}
	p := &ownedRuntimeProcess{cmd: cmd}
	p.startExitObserver(ctx, cancellationDone, closeOutput)
	return p, nil
}

func (p *ownedRuntimeProcess) Wait() error {
	if p.exitReady != nil {
		<-p.exitReady
	}
	err := p.cmd.Wait()
	p.mu.Lock()
	p.sealed = true
	p.mu.Unlock()
	if p.exitErr != nil {
		return errors.Join(err, fmt.Errorf("observe runtime exit: %w", p.exitErr))
	}
	return err
}

func (p *ownedRuntimeProcess) signal(sig syscall.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sealed {
		signalProcessGroup(p.cmd, sig)
	}
}

func (p *ownedRuntimeProcess) waitGroupGone(timeout time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sealed || waitProcessGroupGone(p.cmd, timeout)
}
