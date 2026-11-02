package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agentcli"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// managedCLIObservation is one provider's result from a version-probe round.
// An empty version means this round did not read one; the snapshot then
// keeps the last version the daemon already trusts.
type managedCLIObservation struct {
	version string
	path    string
}

// noteManagedCLIProbe records a codex/claude/grok observation from the
// version-probe loop. Other providers are ignored. The caller holds no lock;
// mu is the probe loop's mutex.
func (d *Daemon) noteManagedCLIProbe(mu *sync.Mutex, obs map[string]managedCLIObservation, name, version, path string) {
	if d == nil || !agentcli.IsManaged(name) {
		return
	}
	mu.Lock()
	obs[name] = managedCLIObservation{version: version, path: path}
	mu.Unlock()
}

// rememberManagedCLIProbes turns this round's observations into the snapshot
// heartbeats and registrations forward. It does not exec a CLI: versions come
// from the round that just ran, or from the existing agent version cache.
func (d *Daemon) rememberManagedCLIProbes(obs map[string]managedCLIObservation) {
	if d == nil {
		return
	}
	d.storeManagedCLIProbes(d.buildManagedCLIProbes(obs))
}

func (d *Daemon) buildManagedCLIProbes(obs map[string]managedCLIObservation) []agentcli.Probe {
	probedAt := time.Now().UTC().Format(time.RFC3339)
	agents := d.agents()
	out := make([]agentcli.Probe, 0, len(agentcli.ManagedCLIs))
	for _, cli := range agentcli.ManagedCLIs {
		observed, seen := obs[cli]
		entry, installed := agents[cli]
		if !seen && !installed {
			// Not on this machine. Do not reuse a stale version cache: that
			// would report a CLI the probe round did not find.
			out = append(out, agentcli.Probe{
				CLI:      cli,
				Channel:  agentcli.ChannelUnknown,
				ProbedAt: probedAt,
			})
			continue
		}
		version := strings.TrimSpace(observed.version)
		if version == "" {
			version = strings.TrimSpace(d.agentVersion(cli))
		}
		fallback := observed.path
		if fallback == "" && installed {
			fallback = entry.Path
		}
		path := d.preferredManagedCLIPath(cli, fallback)
		out = append(out, agentcli.Probe{
			CLI:      cli,
			Current:  version,
			Path:     path,
			Channel:  classifyAgentCLIChannel(path),
			ProbedAt: probedAt,
		})
	}
	return out
}

func (d *Daemon) preferredManagedCLIPath(provider, fallback string) string {
	d.resolvedPathsMu.RLock()
	healed, ok := d.resolvedPaths[provider]
	d.resolvedPathsMu.RUnlock()
	if ok && agentExecutablePresent(healed.path) {
		return healed.path
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	if entry, ok := d.agents()[provider]; ok {
		return entry.Path
	}
	return ""
}

// classifyAgentCLIChannel reports npm, brew, binary, or unknown.
// Symlinks are followed so an npm or Homebrew shim is not reported as a
// loose binary. A path with no marker that exists as a file is binary.
// No path (CLI not installed) is unknown.
func classifyAgentCLIChannel(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return agentcli.ChannelUnknown
	}
	resolved := path
	if target, err := filepath.EvalSymlinks(path); err == nil && target != "" {
		resolved = target
	}
	if channel := agentcli.ChannelFromPath(resolved); channel != "" {
		return channel
	}
	if channel := agentcli.ChannelFromPath(path); channel != "" {
		return channel
	}
	if agentcli.HasNPMPrefix(resolved, os.Stat) || agentcli.HasNPMPrefix(path, os.Stat) {
		return agentcli.ChannelNPM
	}
	return agentcli.ChannelBinary
}

func (d *Daemon) storeManagedCLIProbes(probes []agentcli.Probe) {
	d.agentCLIProbesMu.Lock()
	defer d.agentCLIProbesMu.Unlock()
	d.agentCLIProbes = append([]agentcli.Probe(nil), probes...)
}

// managedCLIProbes copies the last snapshot for a register or heartbeat body.
// An empty snapshot (the daemon has not probed yet) is omitted by callers so
// an early heartbeat cannot wipe probes the server already stored.
func (d *Daemon) managedCLIProbes() []agentcli.Probe {
	if d == nil {
		return nil
	}
	d.agentCLIProbesMu.Lock()
	defer d.agentCLIProbesMu.Unlock()
	if len(d.agentCLIProbes) == 0 {
		return nil
	}
	return append([]agentcli.Probe(nil), d.agentCLIProbes...)
}

func (d *Daemon) attachManagedCLIProbes(req map[string]any) {
	probes := d.managedCLIProbes()
	if len(probes) == 0 || req == nil {
		return
	}
	req["agent_cli_probes"] = probes
}

// protocolAgentCLIProbes is the WebSocket heartbeat shape. JSON matches
// agentcli.Probe, which the HTTP heartbeat body uses directly.
func protocolAgentCLIProbes(probes []agentcli.Probe) []protocol.AgentCLIProbe {
	if len(probes) == 0 {
		return nil
	}
	out := make([]protocol.AgentCLIProbe, 0, len(probes))
	for _, probe := range probes {
		out = append(out, protocol.AgentCLIProbe{
			CLI:      probe.CLI,
			Current:  probe.Current,
			Path:     probe.Path,
			Channel:  probe.Channel,
			ProbedAt: probe.ProbedAt,
		})
	}
	return out
}
