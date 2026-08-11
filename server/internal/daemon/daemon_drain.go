package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Daemon run-state values for d.daemonState. The health endpoint and CLI
// surface them as "running" / "draining" / "stopped"; shutting_down is the
// brief window between a finish-then-stop drain completing and the process
// actually exiting.
const (
	daemonStateRunning      int32 = 0
	daemonStateDraining     int32 = 1
	daemonStateShuttingDown int32 = 2
)

// claimPauseHolder identifies one caller of the claim-pause ref-count (NEX-38
// decision two). Before the ref-count, a single bool meant any holder that
// released its barrier cleared every other holder's pause too — an auto-update
// finishing could silently cancel a user-initiated drain. Now each holder owns
// its own refs and releasing one can never clear the others.
type claimPauseHolder string

const (
	claimPauseDrain        claimPauseHolder = "drain"         // user-initiated safe shutdown
	claimPauseServerUpdate claimPauseHolder = "server-update" // auto-update / server-triggered CLI update barrier
	claimPauseDemotion     claimPauseHolder = "below-minimum" // below-minimum runtime demotion
)

// drainFileName is the persistence marker written under the workspaces root so
// a daemon restart re-enters draining instead of silently resuming claims
// (AC-13).
const drainFileName = "drain.json"

// drainMonitorInterval controls how often the drain-completion watcher polls.
// Drain completion is not latency-sensitive, so a conservative tick avoids
// waking the daemon needlessly.
const drainMonitorInterval = time.Second

// acquireClaimPause takes one claim-pause ref for holder h. Every successful
// call must be paired with releaseClaimPause(h).
func (d *Daemon) acquireClaimPause(h claimPauseHolder) {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.acquireClaimPauseLocked(h)
}

// acquireClaimPauseLocked is the claimMu-held form of acquireClaimPause.
func (d *Daemon) acquireClaimPauseLocked(h claimPauseHolder) {
	if d.claimPauseRefs == nil {
		d.claimPauseRefs = make(map[claimPauseHolder]int)
	}
	d.claimPauseRefs[h]++
	d.claimPauseTotal++
}

// releaseClaimPause drops one claim-pause ref for holder h. Panics on an
// unbalanced release — that is a programming error that would otherwise
// silently wedge the daemon in a paused or un-paused state.
func (d *Daemon) releaseClaimPause(h claimPauseHolder) {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.releaseClaimPauseLocked(h)
}

// releaseClaimPauseLocked is the claimMu-held form of releaseClaimPause.
func (d *Daemon) releaseClaimPauseLocked(h claimPauseHolder) {
	if d.claimPauseRefs == nil || d.claimPauseRefs[h] <= 0 {
		panic(fmt.Sprintf("releaseClaimPause: unbalanced release for %q", h))
	}
	d.claimPauseRefs[h]--
	if d.claimPauseRefs[h] == 0 {
		delete(d.claimPauseRefs, h)
	}
	d.claimPauseTotal--
}

// claimPaused reports whether any holder currently pauses task claiming.
func (d *Daemon) claimPaused() bool {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	return d.claimPauseTotal > 0
}

// claimPausedLocked is the claimMu-held form of claimPaused.
func (d *Daemon) claimPausedLocked() bool {
	return d.claimPauseTotal > 0
}

// claimPausedForClaimsLocked reports whether the CLAIM ENTRY path is blocked.
// The drain holder deliberately does NOT block claims (NEX-38 corrected
// contract, CEO decision 2026-08-10): a draining runtime keeps claiming and
// completing its pre-boundary queued work — new triggers are rejected
// server-side by AgentReadiness, so the queue only ever holds work accepted
// before the drain boundary. Only the server-update / below-minimum holders
// pause claim entry. trySetClaimBarrier and tryBeginServerUpdate still
// consult claimPausedLocked() (which INCLUDES drain) so auto-update and
// demotion continue to defer while a drain is in effect.
func (d *Daemon) claimPausedForClaimsLocked() bool {
	return d.claimPauseTotal-d.claimPauseRefs[claimPauseDrain] > 0
}

// beginDrain puts the daemon into draining: it stops claiming new tasks while
// in-flight tasks run to completion. The marker is persisted BEFORE the
// in-memory state flips (AC-13), so a crash mid-drain still restores draining
// on the next start rather than silently resuming claims.
func (d *Daemon) beginDrain() error {
	if d.daemonState.Load() == daemonStateDraining {
		// Already draining; heal the marker in case it was removed out from
		// under us, then report success.
		return d.persistDrain()
	}
	if err := d.persistDrain(); err != nil {
		return err
	}
	d.acquireClaimPause(claimPauseDrain)
	d.daemonState.Store(daemonStateDraining)
	d.logger.Info("drain started: not claiming new tasks; in-flight tasks will run to completion")
	return nil
}

// abortDrain returns the daemon to running and resumes claiming. Clears the
// marker, releases the drain ref, and drops any armed finish-then-stop.
func (d *Daemon) abortDrain() error {
	if d.daemonState.Load() != daemonStateDraining {
		return fmt.Errorf("daemon is not draining")
	}
	if err := d.clearDrain(); err != nil {
		return err
	}
	d.releaseClaimPause(claimPauseDrain)
	d.daemonState.Store(daemonStateRunning)
	d.finishThenStop.Store(false)
	d.logger.Info("drain aborted: resuming task claims")
	return nil
}

// finishDrainThenStop arms the graceful auto-close: once draining and
// activeTasks reaches zero, the drain monitor deregisters the runtimes and
// stops the daemon (AC-4).
func (d *Daemon) finishDrainThenStop() error {
	if d.daemonState.Load() != daemonStateDraining {
		return fmt.Errorf("daemon is not draining")
	}
	d.finishThenStop.Store(true)
	d.logger.Info("drain finish-then-stop armed: daemon will stop once in-flight tasks complete")
	return nil
}

// drainFilePath returns the absolute path of the drain persistence marker.
func (d *Daemon) drainFilePath() string {
	return filepath.Join(d.cfg.WorkspacesRoot, drainFileName)
}

// persistDrain writes the drain marker under the workspaces root, ensuring the
// directory exists first.
func (d *Daemon) persistDrain() error {
	if err := os.MkdirAll(d.cfg.WorkspacesRoot, 0o755); err != nil {
		return fmt.Errorf("ensure workspaces root for drain marker: %w", err)
	}
	payload := map[string]string{"state": "draining"}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(d.drainFilePath(), data, 0o600)
}

// clearDrain removes the drain marker. Missing file is not an error.
func (d *Daemon) clearDrain() error {
	err := os.Remove(d.drainFilePath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove drain marker: %w", err)
	}
	return nil
}

// restoreDrainState re-enters draining at startup when a drain marker from a
// previous run exists (AC-13). Called from Run after auth and BEFORE the first
// registration, so the register payload already reports draining instead of
// briefly registering online and then flipping — the window the design's risk
// #3 forbids. A missing or malformed marker is silently a normal start.
func (d *Daemon) restoreDrainState() {
	data, err := os.ReadFile(d.drainFilePath())
	if err != nil {
		if !os.IsNotExist(err) {
			d.logger.Warn("failed to read drain marker — starting in normal mode",
				"path", d.drainFilePath(), "error", err)
		}
		return
	}
	var payload struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.State != "draining" {
		d.logger.Warn("invalid drain marker — starting in normal mode", "path", d.drainFilePath())
		return
	}
	d.acquireClaimPause(claimPauseDrain)
	d.daemonState.Store(daemonStateDraining)
	d.logger.Info("restored draining state from previous run; not claiming new tasks until aborted or completed")
}

// drainMonitorLoop watches for the finish-then-stop trigger: when the daemon is
// draining, finish-then-stop is armed, and BOTH the in-flight count and the
// server-side queued count (cached from heartbeat acks) reach zero, it
// deregisters the runtimes and cancels the root context so Run returns
// (AC-4). The daemon keeps claiming during drain, so it drains the queue
// itself; waiting on the queued count as well as active tasks ensures it only
// exits once every pre-boundary accepted task has been claimed and completed
// (NEX-38 corrected contract). Because activeTasks is already zero, the poll
// loop's shutdown window passes instantly and nothing is interrupted. The
// ordinary stop path (/shutdown immediate cancel + pollLoop 30s cap) is
// untouched (AC-5).
func (d *Daemon) drainMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(drainMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if d.daemonState.Load() != daemonStateDraining || !d.finishThenStop.Load() {
				continue
			}
			if d.activeTasks.Load() != 0 || d.queuedTaskCount() != 0 {
				continue
			}
			d.logger.Info("drain complete: no in-flight or queued tasks; deregistering runtimes and stopping")
			d.deregisterRuntimes()
			d.daemonState.Store(daemonStateShuttingDown)
			if d.cancelFunc != nil {
				d.cancelFunc()
			}
			return
		}
	}
}

// registrationStatus returns the status string to report when registering a
// runtime with the server: "draining" while draining, otherwise "online". This
// is what makes a drained daemon restart register its runtimes as draining
// rather than defaulting them back to online (AC-13).
func (d *Daemon) registrationStatus() string {
	if d.daemonState.Load() == daemonStateDraining {
		return "draining"
	}
	return "online"
}

// daemonStateString reports the run state for /health and CLI status:
// "running", "draining", or "stopped".
func (d *Daemon) daemonStateString() string {
	switch d.daemonState.Load() {
	case daemonStateDraining:
		return "draining"
	case daemonStateShuttingDown:
		return "stopped"
	default:
		return "running"
	}
}

// recordQueuedTasks updates the daemon's cached count of server-side queued
// tasks, sourced from heartbeat acks (design §7). Best-effort: an absent (nil)
// ack keeps the previous value.
func (d *Daemon) recordQueuedTasks(runtimeID string, count *int) {
	if runtimeID == "" || count == nil {
		return
	}
	d.queuedTasksMu.Lock()
	defer d.queuedTasksMu.Unlock()
	if d.queuedTasksByRuntime == nil {
		d.queuedTasksByRuntime = make(map[string]int64)
	}
	d.queuedTasksByRuntime[runtimeID] = int64(*count)
}

// queuedTaskCount sums the per-runtime queued-task counts cached from heartbeat
// acks. It is the daemon's best-effort view of how many tasks the server still
// holds queued for its runtimes while draining (AC-8).
func (d *Daemon) queuedTaskCount() int64 {
	d.queuedTasksMu.Lock()
	defer d.queuedTasksMu.Unlock()
	var total int64
	for _, c := range d.queuedTasksByRuntime {
		total += c
	}
	return total
}
