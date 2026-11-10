package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/agentcli"
)

// managedCLIUpgrader is the process-wide upgrader used by
// Ensure/UpgradeManagedAgentCLI. Tests replace it.
var managedCLIUpgrader = func(d *Daemon) *agentcli.Upgrader {
	u := &agentcli.Upgrader{}
	if d != nil {
		u.ProbeOne = d.probeManagedAgentCLI
	}
	return u
}

// EnsureManagedAgentCLI installs a managed agent CLI when missing and the
// install table has a package. It does not drain tasks or check auth — the
// upgrade job does. No package and no binary returns
// agentcli.ErrNoInstallSource; a binary already present is a no-op.
func (d *Daemon) EnsureManagedAgentCLI(ctx context.Context, cli string) (agentcli.Result, error) {
	return d.runManagedAgentCLI(ctx, cli, false)
}

// UpgradeManagedAgentCLI always runs the install channel (npm @latest when
// the install table has a package). No package returns
// agentcli.ErrNoInstallSource. No drain/auth.
func (d *Daemon) UpgradeManagedAgentCLI(ctx context.Context, cli string) (agentcli.Result, error) {
	return d.runManagedAgentCLI(ctx, cli, true)
}

func (d *Daemon) runManagedAgentCLI(ctx context.Context, cli string, forceUpgrade bool) (agentcli.Result, error) {
	if d == nil {
		return agentcli.Result{}, fmt.Errorf("daemon is nil")
	}
	cli = strings.ToLower(strings.TrimSpace(cli))
	if !agentcli.IsManaged(cli) {
		return agentcli.Result{}, fmt.Errorf("%w: %q", agentcli.ErrUnknownCLI, cli)
	}
	u := managedCLIUpgrader(d)
	var (
		res agentcli.Result
		err error
	)
	if forceUpgrade {
		res, err = u.Upgrade(ctx, cli)
	} else {
		res, err = u.Ensure(ctx, cli)
	}
	if err != nil {
		return res, err
	}
	d.applyManagedCLIUpgradeResult(cli, res)
	return res, nil
}

// applyManagedCLIUpgradeResult refreshes the daemon's probe snapshot after a
// successful ensure/upgrade so heartbeats pick up the new current/path.
func (d *Daemon) applyManagedCLIUpgradeResult(cli string, res agentcli.Result) {
	if d == nil {
		return
	}
	// Prefer the probe from the upgrader; fall back to a path-only row.
	probe := res.Probe
	if probe.CLI == "" {
		probe.CLI = cli
	}
	if probe.ProbedAt == "" {
		probe.ProbedAt = time.Now().UTC().Format(time.RFC3339)
	}
	existing := d.managedCLIProbes()
	by := make(map[string]agentcli.Probe, len(agentcli.ManagedCLIs))
	for _, p := range existing {
		by[p.CLI] = p
	}
	by[cli] = probe
	// Publish the path into agentsAvailable when needed. Full rediscovery
	// (refreshAgentAvailability) stays with the discovery loop so a
	// zero-value Daemon and hosts that already have CLIs on PATH stay quiet.
	if path := strings.TrimSpace(probe.Path); path != "" {
		d.publishManagedCLIPath(cli, path)
	}
	if ver := strings.TrimSpace(probe.Current); ver != "" {
		d.versionsMu.Lock()
		if d.agentVersions == nil {
			d.agentVersions = map[string]string{}
		}
		d.versionsMu.Unlock()
		d.setAgentVersion(cli, ver)
		if path := strings.TrimSpace(probe.Path); path != "" {
			d.resolvedPathsMu.Lock()
			if d.resolvedPaths == nil {
				d.resolvedPaths = make(map[string]healedAgent)
			}
			d.resolvedPaths[cli] = healedAgent{path: path, version: ver}
			d.resolvedPathsMu.Unlock()
		}
	}
	out := make([]agentcli.Probe, 0, len(agentcli.ManagedCLIs))
	for _, name := range agentcli.ManagedCLIs {
		if p, ok := by[name]; ok {
			out = append(out, p)
		}
	}
	d.storeManagedCLIProbes(out)
}

// publishManagedCLIPath records the path selected by Ensure/Upgrade into
// agentsAvailable (and resolvedPaths). Always overwrites a prior brew/custom
// entry so a successful npm --prefix install is what later probes and launches
// see — not the older binary that was still on PATH.
func (d *Daemon) publishManagedCLIPath(cli, path string) {
	path = strings.TrimSpace(path)
	if d == nil || path == "" {
		return
	}
	current := d.agents()
	merged := make(map[string]AgentEntry, len(current)+1)
	for name, e := range current {
		merged[name] = e
	}
	entry := merged[cli]
	entry.Path = path
	merged[cli] = entry
	d.agentsAvailable.Store(&merged)

	d.resolvedPathsMu.Lock()
	if d.resolvedPaths == nil {
		d.resolvedPaths = make(map[string]healedAgent)
	}
	prev := d.resolvedPaths[cli]
	d.resolvedPaths[cli] = healedAgent{path: path, version: prev.version}
	d.resolvedPathsMu.Unlock()
}

// probeManagedAgentCLI re-observes one managed CLI after ensure/upgrade.
func (d *Daemon) probeManagedAgentCLI(ctx context.Context, cli string) (agentcli.Probe, error) {
	cli = strings.ToLower(strings.TrimSpace(cli))
	path := d.resolveManagedCLIPath(cli)
	probe := agentcli.Probe{
		CLI:      cli,
		Path:     path,
		Channel:  classifyAgentCLIChannel(path),
		ProbedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if path == "" {
		return probe, nil
	}
	version, err := detectAgentVersion(ctx, agent.Command{Path: path})
	if err != nil {
		return probe, err
	}
	probe.Current = strings.TrimSpace(version)
	return probe, nil
}

func (d *Daemon) resolveManagedCLIPath(cli string) string {
	envKey := agentcli.PathEnv(cli)
	// Explicit operator pin wins over everything.
	if envKey != "" {
		if p := strings.TrimSpace(os.Getenv(envKey)); p != "" && agentExecutablePresent(p) {
			return p
		}
	}
	// Prefer the npm bootstrap prefix when present so a successful
	// Upgrade/Ensure into ~/.local is not shadowed by an older brew/PATH
	// binary still cached in resolvedPaths / agentsAvailable.
	home, _ := os.UserHomeDir()
	if home != "" {
		candidate := filepath.Join(agentcli.DefaultNPMGlobalPrefix(home), "bin", cli)
		if agentExecutablePresent(candidate) {
			return candidate
		}
	}
	if d != nil {
		if p := d.preferredManagedCLIPath(cli, ""); strings.TrimSpace(p) != "" && agentExecutablePresent(p) {
			return p
		}
		if entry, ok := d.agents()[cli]; ok && agentExecutablePresent(entry.Path) {
			return entry.Path
		}
	}
	return ""
}
