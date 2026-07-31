package daemon

import (
	"context"
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
func (d *Daemon) agentDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(agentDiscoveryInterval)
	defer ticker.Stop()

	var (
		backoff   time.Duration
		nextRetry time.Time
	)
	for {
		select {
		case <-ctx.Done():
			return
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
// reported on /health. Called at the end of every registration round with the
// providers that round dropped.
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
