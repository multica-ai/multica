package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Indirections over the real release / version helpers so tests can run the
// auto-update loop deterministically without reaching out to GitHub or
// shelling out to brew/curl. Mirrors the pattern used at the top of daemon.go
// for `isBrewInstall` / `getBrewPrefix` / `matchKnownBrewPrefix`.
var (
	fetchLatestRelease = cli.FetchLatestRelease
	isReleaseVersion   = cli.IsReleaseVersion
	isNewerVersion     = cli.IsNewerVersion
)

// detectSelfVersion runs `<path> --version` and returns the version field.
// Indirection so tests never fork a real process.
var detectSelfVersion = func(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	// Context cancellation only kills the direct child; a descendant that
	// inherited our stdout pipe keeps Output() blocked past the deadline.
	// WaitDelay force-closes the pipes after the kill — without it a replaced
	// binary that misbehaves would park the autoUpdateLoop goroutine forever
	// with the updating flag held. Same guard as detectCLIVersion.
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run %s --version: %w", path, err)
	}
	return ParseSelfVersion(string(out)), nil
}

// ParseSelfVersion pulls the version out of `multica --version` output, whose
// first line is rendered by cmd/multica's version template:
//
//	multica 0.3.7 (commit: abc1234, built: 2026-07-29T10:00:00Z)
//	go: go1.26.1, os/arch: darwin/arm64
//
// The extracted field is exactly what ldflags put in main.version, which is
// also what lands in Config.CLIVersion — so the two are directly comparable.
// Anything that doesn't match the template shape is returned trimmed, which
// compares unequal and is therefore reported rather than silently ignored.
//
// Exported solely so cmd/multica can pin that contract from the producing side:
// this parser and the version template have to agree, and nothing else would
// notice if a template edit silently broke the auto-reload probe.
func ParseSelfVersion(raw string) string {
	line, _, _ := strings.Cut(raw, "\n")
	line = strings.TrimSpace(line)
	if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "multica" {
		return fields[1]
	}
	return line
}

// autoUpdateInitialDelay is how long the loop waits after Run() returns before
// performing its first version check. The daemon has plenty to do at startup
// (auth, register, sync workspaces, kick off heartbeats); we don't want to add
// an outbound HTTPS call to GitHub on top of that. The delay is also short
// enough that a brand-new install with an available update still self-updates
// within a couple of minutes rather than after the full check interval.
var autoUpdateInitialDelay = 2 * time.Minute

// selfReloadCheckInterval is how often the loop compares the version compiled
// into this process against the `--version` output of the binary it would
// re-exec. One fork/exec per tick, machine-level (not per workspace, not per
// runtime), so the cost is fixed no matter how big the daemon's workload is.
// Overridable for tests.
//
// The value bounds how long a user waits after replacing the binary themselves,
// not how often we ship: the "is there a new release" question belongs to
// DefaultAutoUpdateCheckInterval, and only that one should track release
// cadence. It also compounds, because a tick that lands while the daemon is
// busy defers to the next one rather than interrupting a task — so the wait is
// really "the first tick that is both due and idle". Ten minutes keeps the
// average wait around five and the multiplier tolerable on a busy host; an hour
// would not, and would put us back in sight of the "I upgraded and nothing
// happened" problem this check exists to remove.
var selfReloadCheckInterval = 10 * time.Minute

// selfReloadProbeTimeout bounds the `--version` fork/exec. Generous: the point
// of the timeout is to stop a wedged binary from parking the loop goroutine,
// not to police a slow disk.
var selfReloadProbeTimeout = 10 * time.Second

// autoUpdateLoop owns everything that can end in "restart this daemon into a
// different multica binary". It runs two independent checks on one goroutine,
// which is what keeps them from racing each other into triggerRestart:
//
//   - tryAutoUpdate: poll GitHub for a newer release and, when the daemon is
//     idle, run the same brew-or-download upgrade as the server-triggered path.
//   - trySelfReload: notice that the binary on disk was replaced out of band
//     and re-exec into it.
//
// Both are skipped entirely for Desktop-managed daemons: the Electron app ships
// and replaces the CLI binary itself, so self-updating would be clobbered on
// the next launch and re-execing would fight the app's own lifecycle.
//
// The GitHub half is additionally skipped when the operator opted out
// (--no-auto-update / MULTICA_DAEMON_AUTO_UPDATE=false), when the server is
// self-hosted (default-off, MUL-2381), or when the running version isn't a
// tagged release — source builds (`make daemon`) report a `git describe`-style
// version and upgrading them to a public release would silently discard the dev
// work on the machine. None of those apply to the on-disk half, which follows a
// binary the operator installed themselves; see trySelfReload.
//
// Each tick is silent on the happy path so the log stays uncluttered for users
// who run the daemon for weeks at a time.
func (d *Daemon) autoUpdateLoop(ctx context.Context) {
	if d.cfg.LaunchedBy == "desktop" {
		d.logger.Info("auto-update: skipped (managed by Desktop)")
		return
	}

	pullEnabled := d.cfg.AutoUpdateEnabled
	switch {
	case !pullEnabled:
		d.logger.Info("auto-update: disabled")
	case !isReleaseVersion(d.cfg.CLIVersion):
		d.logger.Info("auto-update: skipped (not a release build)", "version", d.cfg.CLIVersion)
		pullEnabled = false
	}
	reloadEnabled := d.cfg.AutoReloadEnabled
	if !reloadEnabled {
		d.logger.Info("auto-reload: disabled")
	}
	if !pullEnabled && !reloadEnabled {
		return
	}

	pullInterval := d.cfg.AutoUpdateCheckInterval
	if pullInterval <= 0 {
		pullInterval = DefaultAutoUpdateCheckInterval
	}
	// Log what each half will do before the startup delay, so a user reading
	// daemon.log right after `daemon start` sees it immediately.
	if pullEnabled {
		d.logger.Info("auto-update: started", "interval", pullInterval, "current", d.cfg.CLIVersion)
	}
	if reloadEnabled {
		d.logger.Info("auto-reload: watching the multica binary on disk",
			"interval", selfReloadCheckInterval, "current", d.cfg.CLIVersion)
	}

	if err := sleepWithContext(ctx, autoUpdateInitialDelay); err != nil {
		return
	}
	if pullEnabled {
		d.tryAutoUpdate(ctx)
	}
	if reloadEnabled {
		d.trySelfReload(ctx)
	}

	// Tickers start only now, after the first check. Starting them before the
	// delay would let an interval shorter than autoUpdateInitialDelay buffer a
	// tick and fire a second check immediately after the first.
	//
	// A disabled half leaves its channel nil, which never fires in a select.
	var pullTick, reloadTick <-chan time.Time
	if pullEnabled {
		ticker := time.NewTicker(pullInterval)
		defer ticker.Stop()
		pullTick = ticker.C
	}
	if reloadEnabled {
		ticker := time.NewTicker(selfReloadCheckInterval)
		defer ticker.Stop()
		reloadTick = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-pullTick:
			d.tryAutoUpdate(ctx)
		case <-reloadTick:
			d.trySelfReload(ctx)
		}
	}
}

// tryAutoUpdate runs one check-and-maybe-upgrade cycle. Bails early on any of:
// already updating (server-triggered upgrade in flight), active tasks (defer
// to next tick — we never interrupt running agents), version fetch failure,
// or no newer release. The function never returns an error: a check that
// fails today will be retried at the next tick, and we don't want a transient
// network blip to escalate to a process-level shutdown.
func (d *Daemon) tryAutoUpdate(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	// Don't race the server-triggered update path. If a manual update from
	// the Runtimes page is already in flight, let it finish and re-check next
	// tick (by which time we'll either be on the new binary or it failed and
	// we can retry).
	if d.updating.Load() {
		d.logger.Debug("auto-update: skip — update already in progress")
		return
	}
	// Cheap pre-fetch idle check: the release-metadata fetch below makes an
	// HTTPS call to GitHub, and there is no point paying that cost (or the
	// rate-limit budget) when we already know we are going to defer. A task
	// that starts between this load and the barrier check below is caught
	// by the strict re-check under claimMu inside trySetClaimBarrier.
	if running := d.activeTasks.Load(); running > 0 {
		d.logger.Debug("auto-update: skip — tasks running", "active", running)
		return
	}

	release, err := fetchLatestRelease()
	if err != nil {
		d.logger.Warn("auto-update: fetch latest release failed — will retry", "error", err)
		return
	}
	if release == nil || release.TagName == "" {
		return
	}
	if !isNewerVersion(release.TagName, d.cfg.CLIVersion) {
		return
	}

	// CAS the updating flag so a concurrent server-triggered handleUpdate
	// dropped onto a heartbeat tick can't double-fire. Release on every exit
	// path before triggerRestart — once that lands, the daemon ctx is
	// cancelled and the flag dies with the process.
	if !d.updating.CompareAndSwap(false, true) {
		d.logger.Debug("auto-update: skip — update already in progress (raced)")
		return
	}
	released := false
	defer func() {
		if !released {
			d.updating.Store(false)
		}
	}()

	// Strict barrier: between the cheap pre-fetch idle check and now the
	// release fetch took anywhere from tens of milliseconds (typical) to
	// seconds (slow link, GitHub hiccup), plenty of time for a poller to
	// claim a fresh task. trySetClaimBarrier checks claimsInFlight +
	// activeTasks under claimMu and only flips pauseClaims to true if both
	// are zero, so once it returns true we can run the upgrade knowing that
	// no in-flight task will be cancelled by triggerRestart.
	if !d.trySetClaimBarrier() {
		d.logger.Info("auto-update: deferring — task or claim in flight at barrier check")
		return
	}
	barrierReleased := false
	defer func() {
		if !barrierReleased {
			d.releaseClaimBarrier()
		}
	}()

	d.logger.Info("auto-update: newer release available, upgrading",
		"current", d.cfg.CLIVersion, "target", release.TagName)

	output, err := d.runUpdateFn(release.TagName)
	if err != nil {
		d.logger.Warn("auto-update: upgrade failed — will retry", "error", err, "output", output)
		return
	}

	d.logger.Info("auto-update: upgrade completed, restarting", "target", release.TagName, "output", output)
	if !d.triggerRestart() {
		// The upgrade landed but the handoff target could not be resolved, so
		// no process exit is coming. Fall through to both deferred restores:
		// holding the updating flag and the claim barrier for a restart that
		// will never happen stops this daemon from claiming any task, forever.
		// The next tick retries — and since the binary on disk is already the
		// new one, trySelfReload will keep trying too.
		d.logger.Error("auto-update: upgrade completed but restart could not be scheduled — resuming claims")
		return
	}
	// triggerRestart cancels the root context, which causes Run() to return
	// and the parent (cmd_daemon.go) to re-exec the new binary. Leave both
	// the updating flag and the claim barrier held — process exit is
	// imminent and clearing either would open a window for new claims / a
	// second auto-update tick to fire mid-shutdown.
	released = true
	barrierReleased = true
}

// trySelfReload restarts the daemon when the multica binary on disk no longer
// reports the version compiled into this process.
//
// This is the out-of-band half of self-update. `brew upgrade multica`, a
// re-download, or a developer's `make build` all replace the binary at the same
// path, and the daemon then keeps serving the version it booted with: its own
// version string is frozen at compile time, and the pinned-path self-heal in
// resolveAgentEntry only fires when a path *vanishes*, which an in-place
// overwrite never does.
//
// tryAutoUpdate does eventually recover the most common shape of this — the
// running version is older than the latest release, so it re-runs the upgrade
// (a no-op under brew) and restarts. What it cannot recover is the rest:
// self-hosted daemons where auto-update is default-off (MUL-2381), dev builds
// skipped by isReleaseVersion, and installing something GitHub doesn't consider
// newer (a deliberate downgrade, or an intermediate version). It is also up to
// a full check interval slow. This check closes all of that, which is why it
// has its own switch (--no-auto-reload / MULTICA_DAEMON_AUTO_RELOAD=false /
// disable_auto_reload) rather than riding on MULTICA_DAEMON_AUTO_UPDATE.
//
// Restart mechanics are shared with tryAutoUpdate rather than reinvented:
// the same trySetClaimBarrier keeps a running task from being interrupted, and
// the same triggerRestart performs the handoff. There is deliberately no
// pending-restart state machine and no drain hook — a busy daemon just defers
// to the next tick, so the check cannot leave claiming paused while waiting for
// something to wake it. The deferral is recorded in reloadPendingReason purely
// so /health can explain why the daemon is still on the old version.
//
// A failed probe is never treated as a version change. The binary is most
// likely to be unreadable during the few hundred milliseconds of the very
// upgrade this check exists to detect (vanished path, ETXTBSY — the same class
// runtimeVersionProbeAttempts documents), and "couldn't read the version" is
// not a reason to restart into one.
func (d *Daemon) trySelfReload(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if d.RestartBinary() != "" {
		return
	}
	// Acquire update ownership rather than sampling it. A plain Load() is only a
	// hint: handleUpdate could win the CAS in the gap between the read and
	// triggerRestart, and this path would then cancel the root context while the
	// binary is still being replaced — re-execing a half-written file and cutting
	// the server-triggered update's status reporting off partway. The CAS is the
	// only thing that actually arbitrates, because the claim barrier does not:
	// handleUpdate reaches it through tryBeginServerUpdate, and the two never
	// inspect each other's state. Same shape tryAutoUpdate uses.
	//
	// Released on every exit below except a successful handoff, where the process
	// is about to go down and clearing it would let a second attempt start
	// mid-shutdown.
	if !d.updating.CompareAndSwap(false, true) {
		d.logger.Debug("auto-reload: skip — update already in progress")
		return
	}
	released := false
	defer func() {
		if !released {
			d.updating.Store(false)
		}
	}()

	// Probe the binary the restart would actually exec, not os.Executable():
	// under brew those differ, and following the stable symlink is the whole
	// point of restartTargetBinary.
	target, err := d.restartTargetBinary()
	if err != nil {
		d.logger.Warn("auto-reload: could not resolve own executable — will retry", "error", err)
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, selfReloadProbeTimeout)
	defer cancel()
	onDisk, err := detectSelfVersion(probeCtx, target)
	if err != nil {
		d.logger.Warn("auto-reload: version probe failed — will retry, not treating it as a change",
			"binary", target, "error", err)
		return
	}
	// Either side blank makes the comparison meaningless, and acting on it would
	// be worse than doing nothing: a blank on-disk version is a probe that
	// technically exited 0 without telling us anything, and a blank running
	// version would restart into a successor that reads blank too — a reload
	// every tick, forever.
	if onDisk == "" || d.cfg.CLIVersion == "" {
		d.logger.Warn("auto-reload: version unavailable — will retry, not treating it as a change",
			"binary", target, "on_disk", onDisk, "running", d.cfg.CLIVersion)
		return
	}
	if onDisk == d.cfg.CLIVersion {
		d.clearReloadPending()
		return
	}

	reason := fmt.Sprintf("multica binary on disk reports %s, running %s", onDisk, d.cfg.CLIVersion)
	d.setReloadPending(reason)

	if !d.trySetClaimBarrier() {
		d.logger.Info("auto-reload: deferring — task or claim in flight at barrier check", "reason", reason)
		return
	}
	barrierReleased := false
	defer func() {
		if !barrierReleased {
			d.releaseClaimBarrier()
		}
	}()

	d.logger.Info("auto-reload: restarting into the binary on disk", "reason", reason, "binary", target)
	if !d.triggerRestart() {
		// Resolution failed at the handoff. Both deferred restores run: a failed
		// restart must never cost the daemon its ability to claim, nor leave the
		// update flag held against a restart that is not coming. The next tick
		// re-probes from scratch, so there is no stale baseline to re-detect.
		return
	}
	released = true
	barrierReleased = true
}

// setReloadPending records that a multica version change is confirmed but the
// restart is waiting for the daemon to go idle. Purely diagnostic — surfaced on
// /health and `daemon status` — and it gates nothing, so it can never park the
// daemon the way a pending-state machine could.
func (d *Daemon) setReloadPending(reason string) {
	d.reloadPendingReason.Store(&reason)
}

// clearReloadPending drops the note once the on-disk version matches again
// (the operator reverted the binary before the daemon went idle).
func (d *Daemon) clearReloadPending() {
	d.reloadPendingReason.Store(nil)
}

// reloadPending reports the deferred-restart reason, empty when none.
func (d *Daemon) reloadPending() string {
	if reason := d.reloadPendingReason.Load(); reason != nil {
		return *reason
	}
	return ""
}
