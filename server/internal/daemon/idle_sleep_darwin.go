package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type caffeinateIdleSleepAssertion struct {
	mu     sync.Mutex
	logger *slog.Logger
	cancel context.CancelFunc
	done   chan struct{}
}

func newIdleSleepAssertion(logger *slog.Logger) idleSleepAssertion {
	return &caffeinateIdleSleepAssertion{logger: logger}
}

func (a *caffeinateIdleSleepAssertion) Acquire() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	a.cancel = cancel
	a.done = make(chan struct{})
	go a.supervise(ctx, cmd, a.done)
	return nil
}

func (a *caffeinateIdleSleepAssertion) Release() {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	a.cancel = nil
	a.done = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (a *caffeinateIdleSleepAssertion) supervise(ctx context.Context, cmd *exec.Cmd, done chan<- struct{}) {
	defer close(done)
	for {
		err := cmd.Wait()
		if ctx.Err() != nil {
			return
		}
		if a.logger != nil {
			a.logger.Warn("idle sleep assertion process exited; restarting", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		cmd = exec.CommandContext(ctx, "/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
		if err := cmd.Start(); err != nil {
			if a.logger != nil {
				a.logger.Warn("restart idle sleep assertion process failed", "error", err)
			}
			continue
		}
	}
}
