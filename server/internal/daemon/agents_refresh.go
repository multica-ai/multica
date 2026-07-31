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
var agentVersionRefreshInterval = 5 * time.Minute

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
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-versionTicker.C:
			d.refreshAgentVersions(ctx)
		case now := <-ticker.C:
			gained := d.refreshAgentAvailability()
			missing := d.providersMissingRuntimes()
			if len(missing) == 0 {
				backoff = 0
				continue
			}
			// A newly discovered provider always gets an immediate attempt;
			// otherwise honor the backoff earned by previous failures.
			if len(gained) == 0 && now.Before(nextRetry) {
				continue
			}
			before := len(missing)
			d.convergeRuntimeRegistrations(ctx)
			if len(d.providersMissingRuntimes()) < before {
				backoff = 0
			} else {
				backoff = nextConvergeBackoff(backoff)
			}
			nextRetry = now.Add(backoff)
		}
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
// reported on /health, together with the subset that was dropped for a CONFIRMED
// below-minimum version. Called at the end of every registration round with the
// providers that round dropped.
//
// The two are published together, under one lock, because they describe the same
// round: a reader that sees a provider listed as skipped must be able to tell
// whether the daemon merely failed to read its version or actually verified it
// is too old, and a torn view of that pair would be worse than either alone.
func (d *Daemon) setSkippedAgents(skipped, belowMinimum map[string]string) {
	d.skippedAgentsMu.Lock()
	defer d.skippedAgentsMu.Unlock()
	d.skippedAgents = skipped
	d.belowMinimumAgents = belowMinimum
}

// belowMinimumAgentsSnapshot copies the providers whose last probe confirmed a
// version below the minimum supported one, mapped to that version.
func (d *Daemon) belowMinimumAgentsSnapshot() map[string]string {
	d.skippedAgentsMu.RLock()
	defer d.skippedAgentsMu.RUnlock()
	if len(d.belowMinimumAgents) == 0 {
		return nil
	}
	out := make(map[string]string, len(d.belowMinimumAgents))
	for provider, version := range d.belowMinimumAgents {
		out[provider] = version
	}
	return out
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

// refreshAgentVersions re-probes every installed agent CLI and, when one now
// reports a different version, refreshes the daemon's cached version and
// re-registers the built-in runtimes so the server reports what is on disk.
//
// This is the agent-CLI counterpart to trySelfReload, and it deliberately does
// NOT restart. What a user needs when codex or claude upgrades is that
// subsequent tasks run the new CLI under the new version's rules — not that
// Multica's availability tracks a third party's release cadence. An in-place
// upgrade leaves the pinned path valid, so the new binary is already what
// launches; the two things left stale are the cached version (which keys
// version-sensitive policy such as the Codex sandbox, read back through
// resolveAgentEntry) and the version the server displays. Both are refreshed
// here with running tasks untouched.
//
// detectBuiltinRuntimes does the probing, which buys three properties for free:
// probes fan out, a fast failure is retried (runtimeVersionProbeAttempts), and
// the minimum-version gate still applies. A provider whose probe fails this
// round keeps its previous version and is retried next round — a failed probe
// never looks like a version change.
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
	before := d.agentVersionSnapshot()
	if len(before) == 0 {
		return
	}

	builtins := d.detectBuiltinRuntimes(ctx)
	// Runs before the early return below: a downgrade drops the provider from
	// builtins entirely, so there is no version change left to detect and the
	// machine could otherwise look idle while a too-old CLI keeps taking work.
	if belowMinimum := d.belowMinimumAgentsSnapshot(); len(belowMinimum) > 0 {
		d.demoteBelowMinimumRuntimes(ctx, belowMinimum)
	}
	if len(builtins) == 0 {
		return
	}
	// What this round's payload actually carries, per provider. A provider whose
	// probe failed is absent, which is what stops its pending entry from being
	// confirmed by a registration that never mentioned it.
	carried := make(map[string]string, len(builtins))
	for _, rt := range builtins {
		carried[rt["type"]] = rt["version"]
	}

	var changes []string
	changed := make(map[string]string)
	for provider, version := range carried {
		prev, known := before[provider]
		// A provider we have never seen has no runtime yet, so bringing it online
		// is the converge path's job, not a version change.
		//
		// A provider we know at a BLANK version is the opposite: it registered —
		// it has a runtime — while its version probe was failing, so the server is
		// displaying nothing for it. Nothing else will ever fix that. Converge
		// only looks at providers missing a runtime, and by the next round
		// setAgentVersion has already stored the real version, so no later round
		// sees a change either. Blank -> real is a change, and the only chance to
		// report it is now.
		if !known || prev == version {
			continue
		}
		// A blank *new* version reaches here for any provider with no entry in
		// agent.MinVersions, since the gate passes it through. Treating it as a
		// change would re-register the provider with no version at all, replacing
		// a version the server had with nothing — the same "couldn't read it is
		// not a change" rule trySelfReload applies. setAgentVersion refuses the
		// blank too, so the daemon's own cache keeps the previous value.
		if version == "" {
			d.logger.Warn("agent CLI reported no version; keeping the previous one",
				"provider", provider, "previous", prev)
			continue
		}
		changed[provider] = version
		changes = append(changes, fmt.Sprintf("%s %s -> %s", provider, prev, version))
	}
	d.markAgentVersionsPending(changed)

	// Register only when this round can actually advance something: some pending
	// version that this round's payload carries. Anything marked just above
	// qualifies by construction, so this covers fresh changes too.
	//
	// The narrower condition matters because a pending entry can outlive its
	// provider. An uninstalled CLI is never dropped from the availability set
	// (refreshAgentAvailability is deliberately one-directional), so it keeps
	// failing its probe and never reappears in the payload — and "retry whenever
	// anything is pending" would turn that into one register call per workspace
	// every few minutes, forever. Holding the entry costs nothing; acting on a
	// payload that cannot confirm it costs a request storm.
	pending := d.pendingAgentVersionsSnapshot()
	advances := false
	for provider, version := range pending {
		if carried[provider] == version {
			advances = true
			break
		}
	}
	if !advances {
		return
	}
	if len(changes) > 0 {
		sort.Strings(changes)
		d.logger.Info("agent CLI version changed; refreshing registration without a restart", "changes", changes)
	}
	if !d.reregisterBuiltins(ctx, builtins) {
		return
	}
	// Every workspace accepted this payload, so the server now knows about the
	// versions it carried — and only those.
	d.confirmAgentVersionsRegistered(carried)
}

// demoteBelowMinimumRuntimes takes offline the built-in runtimes of providers
// whose re-probe confirmed a version below the minimum supported one.
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
// Deregistering is the narrowest honest response. It stops the server routing
// work there, the reason is already visible in skipped_agents, and recovery
// needs no new machinery: once the provider is upgraded it has no runtime, which
// puts it back in providersMissingRuntimes and lets the converge path register
// it again. The alternative — keeping the runtime and refusing the launch — puts
// a check on the hot path and leaves the UI advertising a runtime that rejects
// everything.
//
// Only ever acts on builtinProbeBelowMinimum, never on a probe that merely
// failed: an unreadable version is transient, and tearing down a working
// runtime over one is exactly the mistake refreshAgentAvailability's
// one-directional rule exists to avoid.
func (d *Daemon) demoteBelowMinimumRuntimes(ctx context.Context, belowMinimum map[string]string) {
	d.mu.Lock()
	var demoted []string
	demotedProviders := make(map[string]string)
	for _, ws := range d.workspaces {
		kept := ws.runtimeIDs[:0]
		for _, rid := range ws.runtimeIDs {
			rt, ok := d.runtimeIndex[rid]
			if !ok || rt.ProfileID != "" {
				kept = append(kept, rid)
				continue
			}
			version, below := belowMinimum[rt.Provider]
			if !below {
				kept = append(kept, rid)
				continue
			}
			delete(d.runtimeIndex, rid)
			demoted = append(demoted, rid)
			demotedProviders[rt.Provider] = version
		}
		ws.runtimeIDs = kept
	}
	d.mu.Unlock()

	if len(demoted) == 0 {
		return
	}
	d.logger.Warn("agent CLI downgraded below the minimum supported version; taking its runtimes offline",
		"providers", demotedProviders, "runtime_ids", demoted)

	// Best-effort, like every other deregistration path: the daemon has already
	// stopped heartbeating these rows, so the server's stale-heartbeat sweep is
	// the backstop if this call fails.
	if err := d.client.Deregister(ctx, demoted); err != nil {
		d.logger.Warn("deregister after below-minimum downgrade failed",
			"runtime_ids", demoted, "error", err)
	}
	d.notifyRuntimeSetChanged()
}

// reregisterBuiltins re-sends the built-in registration payload for every
// tracked workspace and returns whether all of them succeeded.
func (d *Daemon) reregisterBuiltins(ctx context.Context, builtins []map[string]string) bool {
	d.mu.Lock()
	workspaceIDs := make([]string, 0, len(d.workspaces))
	for id := range d.workspaces {
		workspaceIDs = append(workspaceIDs, id)
	}
	d.mu.Unlock()
	if len(workspaceIDs) == 0 {
		return true
	}
	sort.Strings(workspaceIDs)

	allOK := true
	var changed bool
	for _, id := range workspaceIDs {
		resp, err := d.registerBuiltinRuntimesForWorkspace(ctx, id, builtins)
		if err != nil {
			d.logger.Warn("re-register after agent CLI version change failed; will retry",
				"workspace_id", id, "error", err)
			allOK = false
			continue
		}
		if newIDs, ok := d.mergeBuiltinRegisterResponse(id, resp); ok && len(newIDs) > 0 {
			changed = true
		}
	}
	if changed {
		d.notifyRuntimeSetChanged()
	}
	return allOK
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
	builtins := d.detectBuiltinRuntimes(ctx)
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
		resp, err := d.registerBuiltinRuntimesForWorkspace(ctx, t.id, builtins)
		if err != nil {
			// Left in the missing set on purpose: the next tick retries.
			d.logger.Warn("register newly available runtimes failed; will retry",
				"workspace_id", t.id, "providers", t.missing, "error", err)
			continue
		}
		newIDs, ok := d.mergeBuiltinRegisterResponse(t.id, resp)
		if !ok {
			continue
		}
		if len(newIDs) > 0 {
			changed = true
			d.logger.Info("registered runtime for newly installed agent CLI",
				"workspace_id", t.id, "providers", t.missing, "runtime_ids", newIDs)
		}
	}
	if changed {
		d.notifyRuntimeSetChanged()
	}
}
