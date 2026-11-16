//go:build linux || darwin

package agent

import (
	"context"
	"errors"
	"syscall"
)

func (p *ownedRuntimeProcess) startExitObserver(ctx context.Context, cancellationDone <-chan struct{}, closeOutput func()) {
	p.exitReady = make(chan struct{})
	p.exitObserved = make(chan struct{})
	go p.observeExit(ctx, cancellationDone, closeOutput, observeRuntimeExit)
}

func (p *ownedRuntimeProcess) observeExit(ctx context.Context, cancellationDone <-chan struct{}, closeOutput func(), observe func(int) error) {
	defer close(p.exitReady)
	p.exitErr = observe(p.cmd.Process.Pid)
	if p.exitErr == nil {
		close(p.exitObserved)
	}
	if p.exitErr == nil && ctx.Err() != nil {
		// Preserve the runtime's own SIGTERM/grace/SIGKILL sequence. The
		// leader remains unreaped throughout that cancellation handler.
		<-cancellationDone
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exitErr == nil {
		signalProcessGroup(p.cmd, syscall.SIGKILL)
	} else {
		// Do not signal a group whose exit could not be observed. Until our
		// Wait, the direct child is still owned, except when ECHILD reports
		// that another reaper has already taken it. Unblock the scanner so
		// it can collect and report this failure through Wait.
		if !errors.Is(p.exitErr, syscall.ECHILD) {
			_ = p.cmd.Process.Kill()
		}
		closeOutput()
	}
	// Seal before exposing Wait, including on observation failure. Late
	// cancellation/error-result handlers must never signal a recycled PID.
	p.sealed = true
}
