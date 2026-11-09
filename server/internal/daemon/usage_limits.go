package daemon

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// usageLimitsProviders are the built-in runtimes whose CLIs report the plan
// limits of their login.
var usageLimitsProviders = []string{"claude", "codex"}

func tracksUsageLimits(provider string) bool {
	return slices.Contains(usageLimitsProviders, provider)
}

// probeRateLimits is an indirection so tests never start a real CLI.
var probeRateLimits = func(ctx context.Context, provider string, runtimeCmd agent.Command) (agent.RateLimitSnapshot, error) {
	switch provider {
	case "claude":
		return agent.ProbeClaudeRateLimits(ctx, runtimeCmd)
	case "codex":
		return agent.ProbeCodexRateLimits(ctx, runtimeCmd)
	}
	return agent.RateLimitSnapshot{}, fmt.Errorf("no usage check for %s", provider)
}

const (
	// usageLimitsStartupDelay lets the first registration round finish, so the
	// first check has runtimes to report to.
	usageLimitsStartupDelay = 10 * time.Second
	// usageLimitsRunReportInterval caps reports during a run. Claude sends a
	// rate_limit_event per API response, and the server does not need each one.
	usageLimitsRunReportInterval = 30 * time.Second
	// usageLimitsRefreshFloor ignores refresh requests right after a check.
	usageLimitsRefreshFloor   = 30 * time.Second
	usageLimitsReportTimeout  = 15 * time.Second
	usageLimitsMaxErrorLength = 300
)

type usageLimitsTracker struct {
	mu               sync.Mutex
	snapshots        map[string]agent.RateLimitSnapshot
	lastProbe        map[string]time.Time
	lastRunReport    map[string]time.Time
	runReportPending map[string]bool
	refresh          chan struct{}
}

func (t *usageLimitsTracker) initLocked() {
	if t.snapshots == nil {
		t.snapshots = make(map[string]agent.RateLimitSnapshot)
		t.lastProbe = make(map[string]time.Time)
		t.lastRunReport = make(map[string]time.Time)
		t.runReportPending = make(map[string]bool)
	}
	if t.refresh == nil {
		t.refresh = make(chan struct{}, 1)
	}
}

func (t *usageLimitsTracker) refreshRequests() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	return t.refresh
}

func (t *usageLimitsTracker) requestRefresh() {
	t.mu.Lock()
	t.initLocked()
	refresh := t.refresh
	t.mu.Unlock()
	select {
	case refresh <- struct{}{}:
	default:
	}
}

// startProbe reserves a check. A forced check right after another one is
// skipped, so a burst of refresh clicks costs one CLI start.
func (t *usageLimitsTracker) startProbe(provider string, forced bool, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	if last, ok := t.lastProbe[provider]; forced && ok && now.Sub(last) < usageLimitsRefreshFloor {
		return false
	}
	t.lastProbe[provider] = now
	return true
}

func (t *usageLimitsTracker) current(provider, device string) agent.RateLimitSnapshot {
	if snapshot, ok := t.snapshots[provider]; ok {
		snapshot.Windows = append([]agent.RateLimitWindow(nil), snapshot.Windows...)
		return snapshot
	}
	return agent.RateLimitSnapshot{Provider: provider, Device: device}
}

// recordProbe stores the result of an idle check. A failed check keeps the
// previous windows and records why, so readers can show them as stale.
func (t *usageLimitsTracker) recordProbe(provider, device string, probed agent.RateLimitSnapshot, probeErr error, now time.Time) agent.RateLimitSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	next := t.current(provider, device)
	next.Device = device
	next.CheckedAt = now
	if probeErr != nil {
		next.Error = truncateUsageLimitsError(probeErr.Error())
	} else {
		if probed.AccountID != "" {
			next.AccountID = probed.AccountID
		}
		if probed.Plan != "" {
			next.Plan = probed.Plan
		}
		next.Windows = probed.Windows
		next.UpdatedAt = &now
		next.Source = agent.RateLimitSourceProbe
		next.Error = ""
	}
	if next.Windows == nil {
		next.Windows = []agent.RateLimitWindow{}
	}
	t.snapshots[provider] = next
	return next
}

// recordRun merges windows a running task reported. It returns how long to
// wait before reporting, and false when a report is already scheduled.
func (t *usageLimitsTracker) recordRun(provider, device string, update agent.RateLimitSnapshot, now time.Time) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	next := t.current(provider, device)
	next.Device = device
	next.Windows = agent.MergeRateLimitWindows(next.Windows, update.Windows)
	if update.Plan != "" {
		next.Plan = update.Plan
	}
	next.UpdatedAt = &now
	next.CheckedAt = now
	next.Source = agent.RateLimitSourceRun
	next.Error = ""
	t.snapshots[provider] = next
	if t.runReportPending[provider] {
		return 0, false
	}
	t.runReportPending[provider] = true
	wait := usageLimitsRunReportInterval - now.Sub(t.lastRunReport[provider])
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

func (t *usageLimitsTracker) takeRunReport(provider string, now time.Time) agent.RateLimitSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	t.runReportPending[provider] = false
	t.lastRunReport[provider] = now
	return t.current(provider, "")
}

func (t *usageLimitsTracker) all() []agent.RateLimitSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	providers := make([]string, 0, len(t.snapshots))
	for provider := range t.snapshots {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	snapshots := make([]agent.RateLimitSnapshot, 0, len(providers))
	for _, provider := range providers {
		snapshots = append(snapshots, t.current(provider, ""))
	}
	return snapshots
}

func truncateUsageLimitsError(message string) string {
	if len(message) <= usageLimitsMaxErrorLength {
		return message
	}
	return message[:usageLimitsMaxErrorLength]
}

// usageLimitsLoop checks plan limits on a timer and on request, and hands
// cached limits to runtimes that register later.
func (d *Daemon) usageLimitsLoop(ctx context.Context) {
	interval := d.cfg.UsageLimitsInterval
	if interval <= 0 {
		return
	}
	var runtimeSetCh <-chan struct{}
	if d.runtimeSet != nil {
		ch, unsubscribe := d.runtimeSet.Subscribe()
		defer unsubscribe()
		runtimeSetCh = ch
	}
	refresh := d.usageLimits.refreshRequests()
	timer := time.NewTimer(usageLimitsStartupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			d.checkUsageLimits(ctx, false)
			timer.Reset(interval)
		case <-refresh:
			d.checkUsageLimits(ctx, true)
		case <-runtimeSetCh:
			for _, snapshot := range d.usageLimits.all() {
				d.reportUsageLimits(ctx, snapshot)
			}
		}
	}
}

func (d *Daemon) checkUsageLimits(ctx context.Context, forced bool) {
	agents := d.agents()
	for _, provider := range usageLimitsProviders {
		entry, ok := agents[provider]
		if !ok || len(d.usageLimitsRuntimeIDs(provider)) == 0 {
			continue
		}
		if !d.usageLimits.startProbe(provider, forced, time.Now()) {
			continue
		}
		probed, err := probeRateLimits(ctx, provider, agent.NewCommand(entry.Path, nil))
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			d.logger.Warn("usage limits check failed", "provider", provider, "error", err)
		}
		d.reportUsageLimits(ctx, d.usageLimits.recordProbe(provider, d.cfg.DeviceName, probed, err, time.Now().UTC()))
	}
}

// recordRunRateLimits receives limits from a running task. It runs on the
// backend's protocol reader, so the report happens on its own goroutine.
func (d *Daemon) recordRunRateLimits(provider string, update agent.RateLimitSnapshot) {
	wait, schedule := d.usageLimits.recordRun(provider, d.cfg.DeviceName, update, time.Now().UTC())
	if !schedule {
		return
	}
	time.AfterFunc(wait, func() {
		d.reportUsageLimits(d.daemonLifecycleCtx(), d.usageLimits.takeRunReport(provider, time.Now()))
	})
}

func (d *Daemon) requestUsageLimitsRefresh() {
	d.usageLimits.requestRefresh()
}

// usageLimitsRuntimeIDs lists the built-in runtimes of provider. A custom
// runtime profile may run another binary or login, so it gets no limits.
func (d *Daemon) usageLimitsRuntimeIDs(provider string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for id, rt := range d.runtimeIndex {
		if rt.Provider == provider && rt.ProfileID == "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (d *Daemon) reportUsageLimits(ctx context.Context, snapshot agent.RateLimitSnapshot) {
	if ctx.Err() != nil {
		return
	}
	for _, runtimeID := range d.usageLimitsRuntimeIDs(snapshot.Provider) {
		reportCtx, cancel := context.WithTimeout(ctx, usageLimitsReportTimeout)
		err := d.client.ReportUsageLimits(reportCtx, runtimeID, snapshot)
		cancel()
		if err != nil {
			// An older server has no endpoint for this; nothing to retry.
			d.logger.Debug("usage limits report failed", "runtime_id", runtimeID, "provider", snapshot.Provider, "error", err)
		}
	}
}
