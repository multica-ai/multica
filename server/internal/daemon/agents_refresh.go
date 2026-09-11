package daemon

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// agentDiscoveryInterval is how often a running daemon re-checks which agent
// CLIs are installed. A round is a handful of exec.LookPath calls — the
// login-shell fallback is separately rate-limited by the much longer
// shellResolveTTL — so this can be short enough that installing a CLI feels
// immediate. Overridable for tests.
var agentDiscoveryInterval = 2 * time.Minute

// agentConvergeMaxBackoff caps the retry delay for a discovered provider that
// keeps failing to register (permanently below the minimum supported version, a
// CLI whose --version never succeeds, a server that keeps rejecting the
// register). Discovery itself stays on agentDiscoveryInterval; only the
// expensive half — version probes plus one register call per workspace — backs
// off, so a stuck provider cannot turn into a busy loop. Overridable for tests.
var agentConvergeMaxBackoff = 30 * time.Minute

// agentVersionRefreshInterval is how often a running daemon re-probes the
// version of every agent CLI it already has registered, so an in-place upgrade
// of codex/claude is picked up without a restart. A round is one `--version`
// fork per installed CLI, fanned out and machine-level — it does not scale with
// workspace count or runtime count. Overridable for tests.
//
// This is the one local probe whose cost grows with the host (one fork per
// installed CLI) and which executes third-party binaries, some of whose
// wrappers have visible side effects when run — so it is the one worth
// lengthening if background probing needs to get cheaper.
//
// It is not lengthened further than selfReloadCheckInterval, though, because
// the round does more than refresh a displayed version string: it also keys
// version-sensitive launch policy, and it is what confirms a CLI has dropped
// below its minimum supported version and must stop being given work. The
// interval is therefore also the window in which an unsupported CLI keeps
// claiming tasks, which is why this tracks the reload check rather than being
// pushed out on cost grounds alone.
var agentVersionRefreshInterval = 10 * time.Minute

// agentDiscoveryLoop keeps the registered runtime set converged on the agent
// CLIs actually installed on this machine, so a CLI installed while the daemon
// is running comes online without a restart (MUL-5439).
//
// Each tick does the cheap half unconditionally: re-probe availability and
// publish anything new. The expensive half (version probes + registration) runs
// only when some discovered provider is not yet registered for every tracked
// workspace — which is derived from live state, not remembered from the round
// that discovered it. That is what makes a failed first attempt retry: a
// provider whose version probe timed out, or whose register call failed, is
// still "missing" on the next tick and gets tried again. It also means a
// provider rejected for being below the minimum version recovers on its own
// after an in-place upgrade.
//
// A second, slower ticker keeps the versions of already-registered providers
// fresh (refreshAgentVersions) — the converge half only ever looks at providers
// that are *missing* a runtime, so without it an in-place CLI upgrade would stay
// invisible until the daemon restarted.
func (d *Daemon) agentDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(agentDiscoveryInterval)
	defer ticker.Stop()
	versionTicker := time.NewTicker(agentVersionRefreshInterval)
	defer versionTicker.Stop()

	var (
		backoff   time.Duration
		nextRetry time.Time
		// lastMismatch is the DSH profile/runtime disagreement the previous tick
		// saw, for the one direction that is throttled to a single forced round.
		lastMismatch dshProfileMismatch
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-versionTicker.C:
			d.refreshAgentVersions(ctx)
		case now := <-ticker.C:
			// Forced by disagreement between two live states, not by a change
			// since the last look: a profile removed between two observations,
			// or before the first one — which is what a restart produces —
			// would otherwise never be noticed. Observed in practice: a profile
			// removed a minute after startup stayed live for three minutes and
			// the server routed a chat task straight into it.
			mismatch := d.dshRuntimeProfileMismatch()
			force := mismatch.forcesEveryTick() || (mismatch != dshMismatchNone && mismatch != lastMismatch)
			lastMismatch = mismatch
			d.convergeAgentRuntimes(ctx, &backoff, &nextRetry, now, force)
		case <-d.agentDiscoveryKick:
			// A local state change (the DSH runtime profile appearing) makes a
			// provider registrable right now. Wait for nothing: the scheduled
			// attempt this replaces can be agentConvergeMaxBackoff away.
			d.convergeAgentRuntimes(ctx, &backoff, &nextRetry, time.Now(), true)
		}
	}
}

// convergeAgentRuntimes runs the expensive half of a discovery round: publish
// anything the cheap probe gained, then version-probe and register whatever is
// still missing a runtime.
//
// force ignores the backoff earned by previous failures, for a caller that
// knows the reason a provider could not register has just gone away — and, in
// the other direction, for one that knows a registered provider may just have
// gone bad. backoff and nextRetry are the loop's scheduling state, threaded
// through rather than owned here so the ticker and the kick share one backoff.
func (d *Daemon) convergeAgentRuntimes(
	ctx context.Context,
	backoff *time.Duration,
	nextRetry *time.Time,
	now time.Time,
	force bool,
) {
	gained := d.refreshAgentAvailability()
	missing := d.providersMissingRuntimes()
	// A forced round runs even with nothing missing: that is the shape of "a
	// registered provider just became unusable", which is exactly what the
	// verdict layer needs a round to notice.
	if len(missing) == 0 && !force {
		*backoff = 0
		return
	}
	// A newly discovered provider always gets an immediate attempt; otherwise
	// honor the backoff earned by previous failures unless the caller knows
	// the blocker is gone.
	if len(gained) == 0 && !force && now.Before(*nextRetry) {
		return
	}
	before := len(missing)
	d.convergeRuntimeRegistrations(ctx)
	after := len(d.providersMissingRuntimes())
	// backoff is earned by registration that keeps failing. A round that leaves
	// nothing missing has nothing to back off from — including a forced round
	// that only came here to condemn a registered provider, where before and
	// after are both zero.
	if after == 0 || after < before {
		*backoff = 0
	} else {
		*backoff = nextConvergeBackoff(*backoff)
	}
	*nextRetry = now.Add(*backoff)
}

// dshOnlyVerdicts narrows a round's demotable verdicts to dsh.
//
// Returns nil rather than an empty map when there is nothing, so the caller's
// length check reads the same either way.
func dshOnlyVerdicts(demotable map[string]runtimeVerdict) map[string]runtimeVerdict {
	verdict, ok := demotable["dsh"]
	if !ok {
		return nil
	}
	return map[string]runtimeVerdict{"dsh": verdict}
}

// dshProfileMismatch names a disagreement between the two states the daemon has
// to keep aligned: whether the Multica runtime profile is installed, and whether
// a dsh runtime is registered.
//
// The kind, rather than a bare bool, is what the caller needs, because the two
// directions have opposite termination behaviour — see forcesEveryTick.
type dshProfileMismatch string

const (
	// dshMismatchNone: the two states agree.
	dshMismatchNone dshProfileMismatch = ""
	// dshMismatchRuntimeWithoutProfile is a live runtime whose CLI can no
	// longer start. It is still "available" — discovered, and holding a
	// runtime — so nothing else in the loop looks at it, and the server keeps
	// routing work into a CLI that cannot start.
	dshMismatchRuntimeWithoutProfile dshProfileMismatch = "runtime-without-profile"
	// dshMismatchProfileWithoutRuntime is the mirror image, and is what a
	// manual `dsh plugin --profile multica add` produces.
	dshMismatchProfileWithoutRuntime dshProfileMismatch = "profile-without-runtime"
)

// forcesEveryTick reports whether this mismatch must force a converge round on
// every tick rather than only on the tick that first observed it.
//
// The asymmetry is about which direction a round is guaranteed to make progress
// on. Condemning a live runtime takes TWO probe rounds — the first only starts
// condemnedConfirmWindow — and nothing else schedules the second: with dsh still
// registered nothing is missing a runtime, so an unforced tick returns from
// convergeAgentRuntimes without probing at all. Forcing only the first sighting
// therefore leaves the runtime live and taking work forever, which is the exact
// failure the mismatch check exists to prevent.
//
// The other direction has no such guarantee and must NOT force every tick: a
// profile that is installed but answers nothing keeps that mismatch for as long
// as it is installed, and forcing on it would spend a full probe of every
// provider every agentDiscoveryInterval, permanently bypassing
// agentConvergeMaxBackoff.
func (m dshProfileMismatch) forcesEveryTick() bool {
	return m == dshMismatchRuntimeWithoutProfile
}

// dshRuntimeProfileMismatch names the current disagreement, or dshMismatchNone
// when the two states agree.
//
// Deliberately not a change detector: this is compared on every tick against
// live state, so a profile removed before the daemon's first look — or while it
// was not looking — is caught on the very next tick rather than never.
func (d *Daemon) dshRuntimeProfileMismatch() dshProfileMismatch {
	registered := d.dshRuntimeRegistered()
	if registered == dshMulticaProfilePresent() {
		return dshMismatchNone
	}
	if registered {
		return dshMismatchRuntimeWithoutProfile
	}
	// Only worth a round if there is a dsh CLI to register at all: without one
	// nothing this round does can resolve the mismatch.
	if _, discovered := d.agents()["dsh"]; discovered {
		return dshMismatchProfileWithoutRuntime
	}
	return dshMismatchNone
}

// dshRuntimeRegistered reports whether any tracked workspace still holds a
// built-in dsh runtime. Custom runtime profiles are excluded: they carry a
// ProfileID and are owned by the drift path, not by the built-in set.
func (d *Daemon) dshRuntimeRegistered() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ws := range d.workspaces {
		for _, id := range ws.runtimeIDs {
			rt, ok := d.runtimeIndex[id]
			if !ok || rt.ProfileID != "" {
				continue
			}
			if rt.Provider == "dsh" {
				return true
			}
		}
	}
	return false
}

// kickAgentDiscovery asks the discovery loop for an immediate convergence
// round. Safe on a zero-value Daemon and from any goroutine: the send is
// non-blocking, so a burst of producers collapses into one round and none of
// them waits on the loop.
func (d *Daemon) kickAgentDiscovery() {
	if d.agentDiscoveryKick == nil {
		return
	}
	select {
	case d.agentDiscoveryKick <- struct{}{}:
	default:
	}
}

// nextConvergeBackoff doubles the retry delay, starting at one discovery
// interval and capped at agentConvergeMaxBackoff.
func nextConvergeBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return agentDiscoveryInterval
	}
	next := current * 2
	if next > agentConvergeMaxBackoff {
		return agentConvergeMaxBackoff
	}
	return next
}

// agents returns the current built-in agent availability set.
//
// The returned map is shared and MUST NOT be mutated: refreshAgentAvailability
// publishes a whole new map rather than editing this one, which is what makes
// unlocked reads from task-execution paths safe.
func (d *Daemon) agents() map[string]AgentEntry {
	if m := d.agentsAvailable.Load(); m != nil {
		return *m
	}
	// Zero-value Daemon (tests construct one directly): fall back to the
	// startup config so behavior matches the pre-refresh code path.
	return d.cfg.Agents
}

// setSkippedAgents replaces the diagnostic "discovered but not registered" set
// reported on /health. Called at the end of every registration round with the
// providers that round dropped.
//
// Purely diagnostic, and deliberately so: it is last-writer-wins across every
// goroutine that probes, so nothing may steer behavior off it. A caller that
// needs to act on a verdict takes it from detectBuiltinRuntimes' return value,
// which describes its own round.
func (d *Daemon) setSkippedAgents(skipped map[string]string) {
	d.skippedAgentsMu.Lock()
	defer d.skippedAgentsMu.Unlock()
	d.skippedAgents = skipped
}

// skippedAgentsSnapshot copies the current skip reasons for the health handler.
func (d *Daemon) skippedAgentsSnapshot() map[string]string {
	d.skippedAgentsMu.RLock()
	defer d.skippedAgentsMu.RUnlock()
	if len(d.skippedAgents) == 0 {
		return nil
	}
	out := make(map[string]string, len(d.skippedAgents))
	for name, reason := range d.skippedAgents {
		out[name] = reason
	}
	return out
}

// refreshAgentAvailability re-runs CLI discovery and publishes providers that
// appeared since the last probe. It performs no registration — that is
// convergeRuntimeRegistrations, driven by live state so a failure retries.
//
// Deliberately one-directional: only providers GAINED are acted on. A provider
// that stops resolving is kept, because a transient environment difference (a
// daemon restarted from a narrower PATH, a version manager mid-upgrade) would
// otherwise tear down a runtime that is working — and possibly executing a
// task. Removal stays the job of an explicit restart, where the user chose the
// environment.
//
// Returns the providers that were added, for logging and tests.
func (d *Daemon) refreshAgentAvailability() []string {
	current := d.agents()
	probed := probeAgentCLIs()

	var gained []string
	for name := range probed {
		if _, known := current[name]; !known {
			gained = append(gained, name)
		}
	}
	if len(gained) == 0 {
		return nil
	}
	sort.Strings(gained)

	// Copy-on-write: build the union, then publish. Entries for providers we
	// already knew about are preserved as-is so this never fights the pinned
	// path / self-heal bookkeeping (resolvedPaths) for a running provider.
	merged := make(map[string]AgentEntry, len(current)+len(gained))
	for name, entry := range current {
		merged[name] = entry
	}
	for _, name := range gained {
		merged[name] = probed[name]
	}
	d.agentsAvailable.Store(&merged)
	d.logger.Info("agent CLI discovered after startup", "providers", gained)
	return gained
}

// refreshAgentVersions re-probes every installed agent CLI and re-registers the
// built-in runtimes whenever the server has not been told a version this daemon
// has already detected, so what the server reports matches what is on disk.
//
// This is the agent-CLI counterpart to trySelfReload, and it deliberately does
// NOT restart. What a user needs when codex or claude upgrades is that
// subsequent tasks run the new CLI under the new version's rules — not that
// Multica's availability tracks a third party's release cadence. An in-place
// POSIX upgrade changes the binary behind the pinned path; a Windows installer
// upgrade retargets the stable junction resolved by resolveAgentEntry. The two
// things left stale are the cached version (which keys version-sensitive policy
// such as the Codex sandbox) and the version the server displays. Both are
// refreshed here with running tasks untouched.
//
// detectBuiltinRuntimes does the probing, which buys three properties for free:
// probes fan out, a fast failure is retried (runtimeVersionProbeAttempts), and
// the minimum-version gate still applies. A provider whose probe fails this
// round keeps its previous version and is retried next round — a failed probe
// never looks like a version change.
//
// What to send is decided per WORKSPACE, from workspaceState.builtinVersions —
// the versions the last register call the server accepted for that workspace
// actually carried — never from a before/after diff of the shared version
// cache. The cache is written by every path that probes, so a diff only sees a
// change when this round happens to be the first writer: a converge round, a
// new workspace registering, or a self-heal at task launch will all move it
// first and leave this round seeing nothing to do — while the workspaces those
// paths did not touch keep the old version indefinitely.
//
// The acknowledgement is per-workspace rather than a global pending set for
// the same reason in reverse: registration entry points are not serialized, so
// a register that probed BEFORE an upgrade can land AFTER a refresh round has
// updated every workspace it could see. A global "done" flag cleared at that
// moment would strand the late workspace on the old version forever. Scoped to
// the workspace, the late-landing call simply records what it sent, the record
// disagrees with the next probe round, and that round re-registers exactly the
// workspace that fell behind.
//
// Re-registration carries the whole built-in set because that is the shape the
// register endpoint upserts, so one provider's upgrade re-sends its neighbours
// too. That is safe rather than disruptive: the unchanged entries upsert
// identically, and mergeBuiltinRegisterResponse swaps a server-side ID rotation
// in place instead of accumulating a duplicate — the workspace still holds
// exactly one runtime per provider, and no running task is touched.
// This runs unconditionally rather than yielding when the converge half has
// work to do, even though the two occasionally probe in the same window. A
// provider that is permanently below the minimum supported version never leaves
// providersMissingRuntimes — that is by design, so it recovers on its own after
// an upgrade — so yielding to it would let one stuck CLI silently disable version
// refresh for every healthy provider on the machine, forever.
func (d *Daemon) refreshAgentVersions(ctx context.Context) {
	// Nothing has ever been version-detected: the daemon is still starting up,
	// and initial registration is the sync path's job.
	if !d.hasDetectedAgentVersions() {
		return
	}

	builtins, demotable, _ := d.detectBuiltinRuntimes(ctx)
	// Runs before the early return below: a condemned provider drops out of
	// builtins entirely, so there is no version change left to detect and the
	// machine could otherwise look idle while an unusable CLI keeps taking work.
	if len(demotable) > 0 {
		d.demoteUnusableRuntimes(ctx, demotable)
	}
	if len(builtins) == 0 {
		return
	}
	// What this round's payload actually carries, per provider. A provider whose
	// probe failed is absent — and only carried providers are compared below,
	// which is what stops an uninstalled CLI from triggering registrations. It
	// is never dropped from the availability set (refreshAgentAvailability is
	// deliberately one-directional), so it keeps failing its probe and never
	// reappears in the payload; comparing records against a payload that cannot
	// mention it would otherwise turn into one register call per workspace
	// every few minutes, forever.
	carried := builtinVersionsFromPayload(builtins)

	// A workspace is behind when some carried provider's last accepted register
	// call for it carried a different version. A provider with no record for a
	// workspace is skipped: it has never registered there, so bringing it
	// online is the converge path's job, not a version change.
	d.mu.Lock()
	var behind []string
	transitions := make(map[string]struct{})
	for id, ws := range d.workspaces {
		lagging := false
		for provider, version := range carried {
			sent, has := ws.builtinVersions[provider]
			if !has || sent == version {
				continue
			}
			lagging = true
			if sent == "" {
				transitions[fmt.Sprintf("%s %s", provider, version)] = struct{}{}
			} else {
				transitions[fmt.Sprintf("%s %s -> %s", provider, sent, version)] = struct{}{}
			}
		}
		if lagging {
			behind = append(behind, id)
		}
	}
	d.mu.Unlock()
	if len(behind) == 0 {
		return
	}
	sort.Strings(behind)
	advancing := make([]string, 0, len(transitions))
	for t := range transitions {
		advancing = append(advancing, t)
	}
	sort.Strings(advancing)
	d.logger.Info("agent CLI version not yet on the server; refreshing registration without a restart",
		"versions", advancing, "workspace_ids", behind)

	// Register only the workspaces that are behind. A failure records nothing,
	// so that workspace stays behind and the next round retries it.
	var changed bool
	for _, id := range behind {
		// Send, merge and clean up as one ordered step — see
		// workspaceRegisterLock.
		_ = d.withWorkspaceRegisterLock(id, func() error {
			resp, err := d.registerBuiltinRuntimesForWorkspaceLocked(ctx, id, builtins)
			if err != nil {
				d.logger.Warn("re-register after agent CLI version change failed; will retry",
					"workspace_id", id, "error", err)
				return nil
			}
			newIDs, revived, ok := d.mergeBuiltinRegisterResponse(id, resp)
			if !ok {
				return nil
			}
			if len(newIDs) > 0 {
				changed = true
			}
			d.deregisterRevivedRuntimes(ctx, id, revived)
			return nil
		})
	}
	if changed {
		d.notifyRuntimeSetChanged()
	}
}

// demoteUnusableRuntimes takes offline the built-in runtimes of providers whose
// re-probe reached a confirmed verdict about the binary on disk: a version below
// the minimum supported one, or a file the OS refuses to execute at all.
//
// Without this, an in-place DOWNGRADE is the one machine change nothing reacts
// to. probeBuiltinRuntime drops the provider from the registration payload, so
// refreshAgentVersions sees no version to compare; the runtime row survives
// because the built-in merge is additive; and the provider is absent from
// providersMissingRuntimes precisely because it still holds a runtime — so
// converge ignores it too. The daemon keeps routing tasks to a binary it has
// already verified it cannot run correctly, under the previously cached
// version's policy.
//
// A CLI that cannot exec reaches the same dead end by a different road
// (MUL-6164): an npm placeholder stub passes every "is it installed" check the
// daemon makes, so the runtime stays online and every task it claims dies at
// fork/exec. Treating that as transient meant the user saw the same failure once
// per task instead of once, and had no signal the runtime was the problem.
//
// Deregistering is the narrowest honest response. It stops the server routing
// work there, the reason is already visible in skipped_agents, and recovery
// needs no new machinery: once the provider is upgraded it has no runtime, which
// puts it back in providersMissingRuntimes and lets the converge path register
// it again. The alternative — keeping the runtime and refusing the launch — puts
// a check on the hot path and leaves the UI advertising a runtime that rejects
// everything.
//
// Only ever acts on a confirmed verdict, never on a probe that merely failed:
// an unreadable version is transient, and tearing down a working runtime over
// one is exactly the mistake refreshAgentAvailability's one-directional rule
// exists to avoid. detectBuiltinRuntimes owns that distinction, including the
// confirmation window a not-executable verdict must clear first.
//
// causes maps each condemned provider to the evidence against it, which is
// already user-visible in skipped_agents and is what the log below reports.
func (d *Daemon) demoteUnusableRuntimes(ctx context.Context, causes map[string]runtimeVerdict) {
	// Serialize with task claiming the way the restart paths do: the barrier
	// only sets when no claim is in flight and no task is running, so a
	// runtime is never deregistered under a task that is still executing —
	// the server's sweep would fail-and-retry that task while the local
	// process keeps going, and an upgrade-back would revive the runtime ID
	// into a genuine duplicate execution. A busy daemon defers to the next
	// refresh tick; the too-old CLI keeps its runtimes a little longer, which
	// is the pre-demotion status quo, not a new exposure.
	if !d.trySetClaimBarrier() {
		d.logger.Info("defer runtime demotion: task or claim in flight",
			"providers", causes)
		return
	}
	defer d.releaseClaimBarrier()

	d.mu.Lock()
	var demoted []string
	var installWaits []dshInstallWait
	// Grouped per workspace because the cleanup below has to run under each
	// workspace's register lock — see deregisterDroppedRuntimes.
	demotedByWorkspace := make(map[string][]string)
	demotedProviders := make(map[string]string)
	offlineReasons := make(map[string]RuntimeOfflineReason)
	for workspaceID, ws := range d.workspaces {
		// A fresh array, not ws.runtimeIDs[:0]: the health handler copies this
		// slice header under d.mu and serializes it after releasing the lock
		// (removeStaleRuntime keeps the same rule), so filtering in place would
		// write into a slice another goroutine is reading.
		kept := ws.runtimeIDs[:0:0]
		for _, rid := range ws.runtimeIDs {
			rt, ok := d.runtimeIndex[rid]
			if !ok || rt.ProfileID != "" {
				kept = append(kept, rid)
				continue
			}
			cause, condemned := causes[rt.Provider]
			if !condemned {
				kept = append(kept, rid)
				continue
			}
			delete(d.runtimeIndex, rid)
			demoted = append(demoted, rid)
			demotedByWorkspace[workspaceID] = append(demotedByWorkspace[workspaceID], rid)
			demotedProviders[rt.Provider] = cause.reason
			// Carried to the server per runtime row: a client asking "why is my
			// agent offline" reads it from there, and only this side knows both
			// the verdict and the repair command (MUL-6164).
			if cause.offline != nil {
				offlineReasons[rid] = *cause.offline
				// A reason that says an install is running is a promise this
				// row comes back on its own. Remember it so the install can
				// take the promise back if it ends without a profile.
				if cause.offline.Installing {
					installWaits = append(installWaits, dshInstallWait{
						workspaceID: workspaceID, runtimeID: rid, reason: *cause.offline,
					})
				}
			}
			// The runtime is gone, so the record of what was registered for it
			// goes too. When the provider recovers, converge re-registers it and
			// the record is re-seeded — first sighting is converge's job, not a
			// version change for the refresh round to chase.
			delete(ws.builtinVersions, rt.Provider)
		}
		ws.runtimeIDs = kept
	}
	// Remember the verdict before releasing d.mu. Removing the rows is not
	// enough: a register sent before this demotion is still in flight, and its
	// response would re-index the provider the moment it lands. The apply paths
	// consult this under the same lock, so the two are totally ordered.
	d.markProvidersDemotedLocked(causes)
	d.dshInstallWaits = append(d.dshInstallWaits, installWaits...)
	d.mu.Unlock()

	if len(demoted) == 0 {
		return
	}
	d.logger.Warn("agent CLI is no longer usable on this machine; taking its runtimes offline",
		"providers", demotedProviders, "runtime_ids", demoted)

	// One workspace at a time, each under its own register lock: the rows were
	// dropped under d.mu but the requests go out without it, so an upgrade that
	// recovered the provider in between must not be undone by this older
	// cleanup. Sorted so the log order is deterministic; never two locks at
	// once, so there is no ordering to deadlock on.
	workspaceIDs := make([]string, 0, len(demotedByWorkspace))
	for workspaceID := range demotedByWorkspace {
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)
	for _, workspaceID := range workspaceIDs {
		_ = d.withWorkspaceRegisterLock(workspaceID, func() error {
			d.deregisterDroppedRuntimes(ctx, workspaceID, demotedByWorkspace[workspaceID],
				"agent CLI unusable", offlineReasons)
			return nil
		})
	}
	d.notifyRuntimeSetChanged()
}

// dshInstallWait is one runtime row whose offline reason claims an automatic
// profile install will bring it back.
type dshInstallWait struct {
	workspaceID string
	runtimeID   string
	reason      RuntimeOfflineReason
}

// withdrawDshInstallWait rewrites the offline reason of every runtime that was
// taken offline while an automatic profile install was still running, once that
// install has ended without producing a profile.
//
// Without it the claim is permanent. `installing` is what tells the server this
// particular offline runtime is worth queueing behind (MUL-6164), and the row
// carrying it is written exactly once, by the demotion — which also removes the
// runtime from the index, so no later round finds a runtime to condemn and no
// later deregistration is sent. An install that fails after that point leaves
// every assignment and @mention for the agent queueing behind a wait that has
// already given up, which is the failure the structured reason exists to
// prevent, not a new one to introduce.
//
// Best-effort, like every other deregistration path: the reason is a hint on a
// row that is already offline, and the row is corrected again the moment the
// provider registers.
func (d *Daemon) withdrawDshInstallWait(ctx context.Context) {
	d.mu.Lock()
	waits := d.dshInstallWaits
	d.dshInstallWaits = nil
	d.mu.Unlock()
	if len(waits) == 0 {
		return
	}

	byWorkspace := make(map[string][]dshInstallWait, len(waits))
	for _, wait := range waits {
		byWorkspace[wait.workspaceID] = append(byWorkspace[wait.workspaceID], wait)
	}
	workspaceIDs := make([]string, 0, len(byWorkspace))
	for workspaceID := range byWorkspace {
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)

	for _, workspaceID := range workspaceIDs {
		_ = d.withWorkspaceRegisterLock(workspaceID, func() error {
			runtimeIDs := make([]string, 0, len(byWorkspace[workspaceID]))
			reasons := make(map[string]RuntimeOfflineReason, len(byWorkspace[workspaceID]))
			for _, wait := range byWorkspace[workspaceID] {
				reason := wait.reason
				reason.Installing = false
				reason.Detail = dshInstallGaveUpReason
				runtimeIDs = append(runtimeIDs, wait.runtimeID)
				reasons[wait.runtimeID] = reason
			}
			// Same tracking re-check every deregistration path makes: a
			// register that brought the runtime back between the demotion and
			// now is newer than this correction, and its row already says
			// online.
			runtimeIDs = d.untrackedRuntimeIDs(runtimeIDs)
			if len(runtimeIDs) == 0 {
				return nil
			}
			d.logger.Warn("automatic DSH profile install gave up; withdrawing the wait from its runtimes",
				"workspace_id", workspaceID, "runtime_ids", runtimeIDs)
			if err := d.client.Deregister(ctx, runtimeIDs, reasons); err != nil {
				d.logger.Warn("withdrawing the DSH install wait failed",
					"workspace_id", workspaceID, "runtime_ids", runtimeIDs, "error", err)
			}
			return nil
		})
	}
}

// deregisterRevivedRuntimes takes offline the rows a register response brought
// back for a provider that was demoted while that register was in flight.
//
// The local apply already refused them, so without this the server would be the
// only side still believing the runtime is online: it would keep the row in the
// runtime list and route work to a daemon that no longer tracks it, and those
// tasks would sit unclaimed until the stale-heartbeat sweep. Best-effort for the
// same reason every other deregistration path is — the sweep is the backstop.
//
// The caller must hold the workspace's register lock (workspaceRegisterLock).
func (d *Daemon) deregisterRevivedRuntimes(ctx context.Context, workspaceID string, revived revivedRuntimes) {
	runtimeIDs := revived.ids
	// Re-check tracking first: the rejection happened under d.mu but this call
	// runs without it, and a legitimate recovery register landing in that gap
	// re-creates the same row — usually under the same ID. Deregistering then
	// would take the recovered runtime offline on the strength of an older
	// decision. Anything the daemon tracks now is newer than this cleanup, and
	// the register lock is what stops a recovery slipping in between this check
	// and the request below.
	runtimeIDs = d.untrackedRuntimeIDs(runtimeIDs)
	if len(runtimeIDs) == 0 {
		return
	}
	d.logger.Warn("register response revived a demoted runtime; taking it offline again",
		"workspace_id", workspaceID, "runtime_ids", runtimeIDs)
	// Re-send the cause: that register's upsert overwrote the runtime row's
	// metadata, so the reason the demotion stored is gone and the server would
	// otherwise be left with a bare "offline" — which reads as "wait for the
	// machine" on every admission path (MUL-6164).
	if err := d.client.Deregister(ctx, runtimeIDs, revived.reasonsFor(runtimeIDs)); err != nil {
		d.logger.Warn("deregister of revived demoted runtimes failed",
			"workspace_id", workspaceID, "runtime_ids", runtimeIDs, "error", err)
	}
}

// deregisterDroppedRuntimes takes offline the server-side rows a convergence
// decided this workspace should no longer host — a provider removed from the
// daemon's config, a disabled custom profile, a workspace converging to zero.
//
// Same contract as deregisterRevivedRuntimes: the caller must hold the
// workspace's register lock, so the tracking re-check and the request are one
// ordered step against every other registration for this workspace. Without the
// lock this is a TOCTOU — a recovery register completing between the two puts
// the row back and this older cleanup knocks it out, leaving the daemon tracking
// a runtime the server has offline.
//
// Best-effort: the daemon has already stopped heartbeating these rows, so the
// server's stale-heartbeat sweep is the backstop if the call fails.
func (d *Daemon) deregisterDroppedRuntimes(ctx context.Context, workspaceID string, runtimeIDs []string, reason string, offlineReasons map[string]RuntimeOfflineReason) {
	runtimeIDs = d.untrackedRuntimeIDs(runtimeIDs)
	if len(runtimeIDs) == 0 {
		return
	}
	if err := d.client.Deregister(ctx, runtimeIDs, offlineReasons); err != nil {
		d.logger.Warn("deregister of dropped runtimes failed",
			"workspace_id", workspaceID, "runtime_ids", runtimeIDs, "reason", reason, "error", err)
	}
}

// providersMissingRuntimes returns the discovered providers that do not have a
// built-in runtime registered for every tracked workspace.
//
// This is the retry signal for the discovery loop, and it is derived from live
// state rather than remembered: a provider only leaves this set once the daemon
// actually holds a runtime row for it in every workspace it watches. Custom
// profile runtimes (ProfileID set) are ignored — they register from a
// workspace's profile list, not from CLI discovery, and a profile that happens
// to share a protocol_family with a built-in must not mask the built-in.
//
// Returns nothing when no workspace is tracked yet: there is nothing to
// register against, and the initial registration will carry the full set.
func (d *Daemon) providersMissingRuntimes() []string {
	available := d.agents()
	if len(available) == 0 {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.workspaces) == 0 {
		return nil
	}
	missing := make(map[string]struct{})
	for _, ws := range d.workspaces {
		registered := make(map[string]struct{}, len(ws.runtimeIDs))
		for _, id := range ws.runtimeIDs {
			rt, ok := d.runtimeIndex[id]
			if !ok || rt.ProfileID != "" {
				continue
			}
			registered[rt.Provider] = struct{}{}
		}
		for name := range available {
			if _, ok := registered[name]; !ok {
				missing[name] = struct{}{}
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	out := make([]string, 0, len(missing))
	for name := range missing {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// convergeRuntimeRegistrations registers the current built-in set for every
// tracked workspace that is missing one of them.
//
// One version-probe round serves every workspace (built-in CLIs are per
// machine, not per workspace). Failures are logged and left for the next
// discovery tick — nothing here records "already handled", so a partial
// failure across workspaces retries only the workspaces that still need it.
//
// Strictly built-ins: this path never fetches, sends, or observes custom runtime
// profiles. Custom profile add/edit/disable remains owned by the drift path
// (refreshWorkspaceRuntimeProfiles), which is the only place allowed to cache a
// profile-set signature.
//
// RecoverOrphans is deliberately NOT called: unlike the runtime_gone recovery
// path, the surviving runtime IDs may still be executing tasks for the user,
// and failing those as orphans would kill live work (MUL-3332).
func (d *Daemon) convergeRuntimeRegistrations(ctx context.Context) {
	// detectBuiltinRuntimes version-gates the availability set and publishes
	// this round's drops for /health, so a provider that cannot register still
	// gets a visible reason even though registration is skipped.
	builtins, demotable, _ := d.detectBuiltinRuntimes(ctx)
	// Condemned providers drop out of builtins entirely, so acting on them
	// before the early return is what keeps a dsh that goes bad mid-flight from
	// staying online: the profile removed while dsh is registered leaves nothing
	// missing a runtime, and a round reached only for that has no registration
	// left to do. demoteUnusableRuntimes owns the claim barrier and the
	// seq-stamped hold, so running it here is safe.
	//
	// Restricted to dsh on purpose. Every other provider is demoted from
	// refreshAgentVersions alone, on its own ten-minute cadence, and that is the
	// schedule their verdicts were tuned against; letting a converge round
	// condemn them too would move Claude Code, Codex and the rest onto a
	// different one as a side effect of a DSH fix. dsh is the only provider with
	// a precondition that can change without any version changing, which is why
	// it is the only one that needs a round it can be forced into.
	if dshDemotable := dshOnlyVerdicts(demotable); len(dshDemotable) > 0 {
		d.demoteUnusableRuntimes(ctx, dshDemotable)
	}
	if len(builtins) == 0 {
		return
	}
	detected := make(map[string]struct{}, len(builtins))
	for _, rt := range builtins {
		detected[rt["type"]] = struct{}{}
	}

	d.mu.Lock()
	type target struct {
		id      string
		missing []string
	}
	var targets []target
	for id, ws := range d.workspaces {
		registered := make(map[string]struct{}, len(ws.runtimeIDs))
		for _, rid := range ws.runtimeIDs {
			rt, ok := d.runtimeIndex[rid]
			if !ok || rt.ProfileID != "" {
				continue
			}
			registered[rt.Provider] = struct{}{}
		}
		var missing []string
		for name := range detected {
			if _, ok := registered[name]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			targets = append(targets, target{id: id, missing: missing})
		}
	}
	d.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })

	var changed bool
	for _, t := range targets {
		// Send, merge and clean up as one ordered step — see
		// workspaceRegisterLock.
		_ = d.withWorkspaceRegisterLock(t.id, func() error {
			resp, err := d.registerBuiltinRuntimesForWorkspaceLocked(ctx, t.id, builtins)
			if err != nil {
				// Left in the missing set on purpose: the next tick retries.
				d.logger.Warn("register newly available runtimes failed; will retry",
					"workspace_id", t.id, "providers", t.missing, "error", err)
				return nil
			}
			newIDs, revived, ok := d.mergeBuiltinRegisterResponse(t.id, resp)
			if !ok {
				return nil
			}
			if len(newIDs) > 0 {
				changed = true
				d.logger.Info("registered runtime for newly installed agent CLI",
					"workspace_id", t.id, "providers", t.missing, "runtime_ids", newIDs)
			}
			d.deregisterRevivedRuntimes(ctx, t.id, revived)
			return nil
		})
	}
	if changed {
		d.notifyRuntimeSetChanged()
	}
}
