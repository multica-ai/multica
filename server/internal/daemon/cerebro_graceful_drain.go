package daemon

// Graceful drain (FIR-3758, Spor A of FIR-3756).
//
// Problem this solves. When the daemon receives SIGTERM (a production deploy
// or a container/Sliplane restart), notifyShutdownContext cancels the root
// context. handleTask derives the agent subprocess context from that same
// root ctx, so every in-flight agent is killed the instant SIGTERM lands. The
// server then fails those tasks and orphan-recovery (upstream MUL-1128) re-runs
// them — which is exactly the "byge af failed runs" a restart produces today.
// pollLoop's fixed 30s wait never protected a running agent: the agent was
// already dead before the wait began.
//
// What the feature changes, when enabled:
//  1. taskParentCtx hands handleTask a context that is NOT a child of the
//     SIGTERM-cancelled root ctx, so a restart no longer reaches in-flight
//     agents.
//  2. On shutdown, drainInFlightTasks stops new claims and lets in-flight
//     tasks run to completion within a bounded window. Only tasks still
//     running when the window expires are cancelled; those fall back to
//     Spor B recovery (the sibling sub-issue) on the next boot.
//
// The window MUST be aligned with the container stop-grace period (Docker
// `--stop-timeout` / Sliplane "termination grace period"): the orchestrator
// sends SIGKILL after that grace no matter what the process is doing, so a
// window longer than the grace cannot save a task. Keep the two in lockstep —
// see docs/cerebro-patches.md.
//
// Default OFF. Enable per fleet with MULTICA_DAEMON_GRACEFUL_DRAIN=true and
// tune the window with MULTICA_DAEMON_GRACEFUL_DRAIN_WINDOW (Go duration,
// e.g. "90s"). With the feature off, behaviour is byte-for-byte the legacy
// 30s wait against already-cancelled tasks.

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultGracefulDrainWindow is used when the feature is enabled but no
	// explicit window is configured. Kept modest so it stays under a typical
	// container stop-grace unless ops widens both together.
	DefaultGracefulDrainWindow = 25 * time.Second

	// legacyDrainTimeout preserves the pre-feature behaviour: whether the
	// feature is off (tasks already cancelled with the root ctx) or on (a
	// post-window cancel grace), we wait at most this long for the in-flight
	// goroutines to unwind before returning from pollLoop.
	legacyDrainTimeout = 30 * time.Second
)

// GracefulDrainConfig is the parsed, immutable feature configuration.
type GracefulDrainConfig struct {
	Enabled bool
	Window  time.Duration
}

// cerebroDrainState holds the runtime state for graceful drain. Its zero
// value is valid: a Daemon constructed as &Daemon{} (as tests do) drains with
// the feature off. All fields are touched only from the single pollLoop
// goroutine, so no additional locking is required.
type cerebroDrainState struct {
	once   sync.Once
	ctx    context.Context
	cancel context.CancelFunc
}

// loadGracefulDrainConfig reads the feature env vars. Called from LoadConfig.
func loadGracefulDrainConfig() GracefulDrainConfig {
	cfg := GracefulDrainConfig{Window: DefaultGracefulDrainWindow}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_DAEMON_GRACEFUL_DRAIN"))) {
	case "true", "1", "yes", "on":
		cfg.Enabled = true
	}
	if raw := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_GRACEFUL_DRAIN_WINDOW")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.Window = d
		}
	}
	return cfg
}

// gracefulDrainEnabled reports whether the feature is on for this daemon.
func (d *Daemon) gracefulDrainEnabled() bool {
	return d.cfg.GracefulDrain.Enabled
}

// taskParentCtx returns the context that handleTask (and therefore the agent
// subprocess) runs under.
//
//   - feature OFF: the root ctx, so behaviour is unchanged — a restart cancels
//     in-flight tasks immediately, exactly as before.
//   - feature ON: a context rooted at context.Background() whose cancel is
//     held by the daemon and fired only by drainInFlightTasks. Cancelling the
//     root ctx (SIGTERM) therefore does NOT reach in-flight tasks.
//
// The enabled context is created once and shared across every runtime poller
// so a single drain cancel tears down all in-flight tasks together. Called
// only from pollLoop's syncPollers (one goroutine), so the sync.Once is a
// belt-and-suspenders guard rather than a contended path.
func (d *Daemon) taskParentCtx(rootCtx context.Context) context.Context {
	if !d.gracefulDrainEnabled() {
		return rootCtx
	}
	d.drain.once.Do(func() {
		d.drain.ctx, d.drain.cancel = context.WithCancel(context.Background())
	})
	return d.drain.ctx
}

// drainInFlightTasks waits for in-flight handleTask goroutines to finish on
// shutdown, then returns so pollLoop can exit and the process can terminate.
// pollLoop calls it from the ctx.Done branch after every poller has stopped,
// so no new task can start once we are here.
//
//   - feature OFF: legacy behaviour — the tasks were cancelled with the root
//     ctx, so we just wait up to legacyDrainTimeout for them to unwind.
//   - feature ON: stop new claims, then wait up to the configured window for
//     the tasks to finish on their own. Only if the window expires do we
//     cancel the task context, killing the stragglers so the process can
//     exit; those runs fall back to Spor B recovery.
func (d *Daemon) drainInFlightTasks(taskWG *sync.WaitGroup) {
	waitDone := make(chan struct{})
	go func() { taskWG.Wait(); close(waitDone) }()

	if !d.gracefulDrainEnabled() {
		d.logger.Info("poll loop stopping, waiting for in-flight tasks", "max_wait", legacyDrainTimeout)
		select {
		case <-waitDone:
		case <-time.After(legacyDrainTimeout):
			d.logger.Warn("timed out waiting for in-flight tasks")
		}
		return
	}

	// Belt-and-suspenders: make sure no poller still mid-decision can claim a
	// fresh task while we drain. pollLoop is already tearing pollers down.
	d.setClaimBarrierForDrain()

	window := d.cfg.GracefulDrain.Window
	if window <= 0 {
		window = DefaultGracefulDrainWindow
	}
	d.logger.Info("graceful drain: restart requested — letting in-flight tasks finish", "window", window)
	select {
	case <-waitDone:
		d.logger.Info("graceful drain: all in-flight tasks finished before window elapsed")
		return
	case <-time.After(window):
	}

	// Window elapsed with tasks still running. Cancel them so the process can
	// exit; orphan-recovery (Spor B) resumes them on the next boot.
	d.logger.Warn("graceful drain: window elapsed — cancelling remaining tasks", "window", window)
	if d.drain.cancel != nil {
		d.drain.cancel()
	}
	select {
	case <-waitDone:
	case <-time.After(legacyDrainTimeout):
		d.logger.Warn("graceful drain: tasks still running after post-window cancel grace")
	}
}

// setClaimBarrierForDrain sets pauseClaims so any poller between slot
// acquisition and ClaimTask backs off instead of claiming fresh work during
// drain. Unlike the auto-update barrier (trySetClaimBarrier) it does not
// require idleness — we are shutting down and simply want new claims stopped.
func (d *Daemon) setClaimBarrierForDrain() {
	d.claimMu.Lock()
	d.pauseClaims = true
	d.claimMu.Unlock()
}
