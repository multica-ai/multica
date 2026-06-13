package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	autoReloadHeartbeatTicks  = 20
	versionProbeTimeout       = 10 * time.Second
	versionReloadSweepTimeout = 30 * time.Second
)

type versionProbeResult struct {
	Version string
	Err     string
	Failed  bool
}

var detectMulticaVersion = func(ctx context.Context, executablePath string) (string, error) {
	cmd := exec.CommandContext(ctx, executablePath, "--version")
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect version for %s: %w", executablePath, err)
	}
	return extractMulticaVersion(string(data)), nil
}

func extractMulticaVersion(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "multica ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
		return line
	}
	return strings.TrimSpace(raw)
}

func (d *Daemon) initVersionReloadBaseline() {
	if !d.cfg.AutoReloadOnVersionChange {
		d.logger.Info("auto-reload: disabled")
		return
	}
	if d.cfg.LaunchedBy == "desktop" {
		d.logger.Info("auto-reload: skipped (managed by Desktop)")
		return
	}

	baseline, ready := d.initialVersionReloadBaseline()
	interval := d.versionReloadInterval()

	d.versionReloadMu.Lock()
	d.versionReloadBaseline = baseline
	d.versionReloadBaselineReady = ready
	d.versionReloadMu.Unlock()

	d.logger.Info("auto-reload: watching CLI versions", "interval", interval, "targets", len(baseline), "baseline_ready", ready)
}

func (d *Daemon) maybeTriggerVersionReloadCheck(ctx context.Context) {
	if ctx.Err() != nil || !d.cfg.AutoReloadOnVersionChange || d.cfg.LaunchedBy == "desktop" {
		return
	}
	if d.updating.Load() || d.RestartBinary() != "" || d.reloadAlreadyPending() {
		return
	}

	d.versionReloadMu.Lock()
	if d.versionReloadBaseline == nil || d.versionReloadCheckInProgress {
		d.versionReloadMu.Unlock()
		return
	}
	d.versionReloadHeartbeatCount++
	if d.versionReloadHeartbeatCount < autoReloadHeartbeatTicks {
		d.versionReloadMu.Unlock()
		return
	}
	d.versionReloadHeartbeatCount = 0
	baseline := cloneVersionProbeMap(d.versionReloadBaseline)
	baselineReady := d.versionReloadBaselineReady
	d.versionReloadCheckInProgress = true
	d.versionReloadMu.Unlock()

	go d.runVersionReloadCheck(ctx, baseline, baselineReady)
}

func (d *Daemon) checkReloadOnVersionChange(ctx context.Context) {
	if ctx.Err() != nil || !d.cfg.AutoReloadOnVersionChange || d.cfg.LaunchedBy == "desktop" {
		return
	}
	if d.updating.Load() || d.RestartBinary() != "" || d.reloadAlreadyPending() {
		return
	}

	d.versionReloadMu.Lock()
	if d.versionReloadBaseline == nil || d.versionReloadCheckInProgress {
		d.versionReloadMu.Unlock()
		return
	}
	baseline := cloneVersionProbeMap(d.versionReloadBaseline)
	baselineReady := d.versionReloadBaselineReady
	d.versionReloadCheckInProgress = true
	d.versionReloadMu.Unlock()
	d.runVersionReloadCheck(ctx, baseline, baselineReady)
}

func (d *Daemon) runVersionReloadCheck(ctx context.Context, baseline map[string]versionProbeResult, baselineReady bool) {
	defer func() {
		d.versionReloadMu.Lock()
		d.versionReloadCheckInProgress = false
		d.versionReloadMu.Unlock()
	}()

	probeCtx, cancel := context.WithTimeout(ctx, versionReloadSweepTimeout)
	defer cancel()

	current, complete := d.currentVersionReloadProbe(probeCtx)
	if !complete {
		d.logger.Warn("auto-reload: version probe timed out; will retry",
			"timeout", versionReloadSweepTimeout)
		return
	}
	if ctx.Err() != nil || d.updating.Load() || d.RestartBinary() != "" || d.reloadAlreadyPending() {
		return
	}
	if !baselineReady {
		d.versionReloadMu.Lock()
		d.versionReloadBaseline = current
		d.versionReloadBaselineReady = true
		d.versionReloadMu.Unlock()
		d.logger.Info("auto-reload: initial CLI version baseline completed", "targets", len(current))
		return
	}
	reason := versionReloadReason(baseline, current)

	d.versionReloadMu.Lock()
	if reason == "" {
		d.versionReloadBaseline = current
		d.versionReloadBaselineReady = true
	}
	d.versionReloadMu.Unlock()

	if reason == "" {
		return
	}
	d.scheduleReloadPending(reason)
}

func (d *Daemon) versionReloadInterval() time.Duration {
	heartbeat := d.cfg.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = DefaultHeartbeatInterval
	}
	return heartbeat * autoReloadHeartbeatTicks
}

func (d *Daemon) initialVersionReloadBaseline() (map[string]versionProbeResult, bool) {
	out := make(map[string]versionProbeResult, len(d.cfg.Agents)+1)
	out["multica"] = versionProbeResult{Version: strings.TrimSpace(d.cfg.CLIVersion)}
	ready := true

	for _, name := range sortedAgentNames(d.cfg.Agents) {
		if version := d.agentVersion(name); strings.TrimSpace(version) != "" {
			out["agent:"+name] = versionProbeResult{Version: strings.TrimSpace(version)}
			continue
		}
		ready = false
	}
	return out, ready
}

func (d *Daemon) currentVersionReloadProbe(ctx context.Context) (map[string]versionProbeResult, bool) {
	out := make(map[string]versionProbeResult, len(d.cfg.Agents)+1)
	if ctx.Err() != nil {
		return out, false
	}
	out["multica"] = d.probeMulticaVersion(ctx)
	if ctx.Err() != nil {
		return out, false
	}
	for _, name := range sortedAgentNames(d.cfg.Agents) {
		out["agent:"+name] = d.probeAgentVersion(ctx, name, d.cfg.Agents[name].Path)
		if ctx.Err() != nil {
			return out, false
		}
	}
	return out, true
}

func (d *Daemon) probeMulticaVersion(ctx context.Context) versionProbeResult {
	path, err := d.resolveRestartBinary()
	if err != nil {
		return versionProbeResult{Failed: true, Err: err.Error()}
	}
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	version, err := detectMulticaVersion(probeCtx, path)
	if err != nil {
		return versionProbeResult{Failed: true, Err: err.Error()}
	}
	return versionProbeResult{Version: strings.TrimSpace(version)}
}

func (d *Daemon) probeAgentVersion(ctx context.Context, name, path string) versionProbeResult {
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	version, err := detectAgentVersion(probeCtx, path)
	if err != nil {
		return versionProbeResult{Failed: true, Err: err.Error()}
	}
	return versionProbeResult{Version: strings.TrimSpace(version)}
}

func versionReloadReason(previous, current map[string]versionProbeResult) string {
	keys := make([]string, 0, len(previous)+len(current))
	seen := make(map[string]struct{}, len(previous)+len(current))
	for k := range previous {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range current {
		if _, ok := seen[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		prev, prevOK := previous[key]
		cur, curOK := current[key]
		if !prevOK || !curOK {
			return fmt.Sprintf("%s version target set changed", versionReloadLabel(key))
		}
		if versionProbeEquivalent(prev, cur) {
			continue
		}
		return fmt.Sprintf("%s version changed: %s -> %s", versionReloadLabel(key), prev.Display(), cur.Display())
	}
	return ""
}

func versionProbeEquivalent(a, b versionProbeResult) bool {
	if a.Failed || b.Failed {
		return a.Failed == b.Failed
	}
	return a.Version == b.Version
}

func (r versionProbeResult) Display() string {
	if r.Failed {
		if r.Err != "" {
			return "unavailable (" + r.Err + ")"
		}
		return "unavailable"
	}
	if r.Version == "" {
		return "unknown"
	}
	return r.Version
}

func versionReloadLabel(key string) string {
	return strings.TrimPrefix(key, "agent:")
}

func cloneVersionProbeMap(in map[string]versionProbeResult) map[string]versionProbeResult {
	out := make(map[string]versionProbeResult, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedAgentNames(agents map[string]AgentEntry) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (d *Daemon) reloadAlreadyPending() bool {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	return d.reloadPending
}

func (d *Daemon) reloadPendingState() (bool, string) {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	return d.reloadPending, d.reloadPendingReason
}

func (d *Daemon) scheduleReloadPending(reason string) {
	d.claimMu.Lock()
	if d.reloadPending {
		d.claimMu.Unlock()
		return
	}
	d.reloadPending = true
	d.reloadPendingReason = reason
	d.pauseClaims = true
	active := d.activeTasks.Load()
	claims := d.claimsInFlight
	shouldRestart := active == 0 && claims == 0
	d.claimMu.Unlock()

	d.logger.Info("auto-reload: CLI version change detected; pausing new task claims",
		"reason", reason, "active_tasks", active, "claims_in_flight", claims)
	if shouldRestart {
		d.triggerPendingReloadIfIdle()
	}
}

func (d *Daemon) triggerPendingReloadIfIdle() {
	d.claimMu.Lock()
	if !d.reloadPending {
		d.claimMu.Unlock()
		return
	}
	active := d.activeTasks.Load()
	claims := d.claimsInFlight
	reason := d.reloadPendingReason
	if active > 0 || claims > 0 {
		d.claimMu.Unlock()
		d.logger.Info("auto-reload: waiting for daemon to drain before restart",
			"reason", reason, "active_tasks", active, "claims_in_flight", claims)
		return
	}
	d.claimMu.Unlock()

	d.logger.Info("auto-reload: restarting after CLI version change", "reason", reason)
	if !d.triggerRestart() {
		d.clearReloadPendingAndResumeClaims()
	}
}

func (d *Daemon) clearReloadPendingAndResumeClaims() {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.reloadPending = false
	d.reloadPendingReason = ""
	d.pauseClaims = false
}
