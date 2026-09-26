package daemon

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/providerusage"
)

const (
	providerUsageInitialDelay = time.Minute
	providerUsageInterval     = 5 * time.Minute
)

// providerUsageLoop samples local CLI and editor plan limits and uploads
// derived snapshots. A collection failure is logged and skipped; it does not
// affect task execution.
func (d *Daemon) providerUsageLoop(ctx context.Context) {
	timer := time.NewTimer(providerUsageInitialDelay)
	defer timer.Stop()
	backoffUntil := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			d.collectProviderUsage(ctx, backoffUntil)
			timer.Reset(providerUsageInterval)
		}
	}
}

func (d *Daemon) collectProviderUsage(ctx context.Context, backoffUntil map[string]time.Time) {
	if d.client == nil || len(d.allRuntimeIDs()) == 0 {
		return
	}
	now := time.Now()
	// Qianwen, Gemini CLI, Amp, Devin, GLM, MiniMax, Ollama, LM Studio,
	// DeepSeek, Perplexity, and Command Code are not collected. They are
	// either not a Multica runtime, or the only CodeNotch path is a browser
	// cookie or a local token ledger rather than a signed-in plan window.
	collectors := []struct {
		provider string
		collect  func(context.Context) providerusage.Result
	}{
		{providerusage.ProviderClaude, providerusage.ClaudeCollector{}.Collect},
		{providerusage.ProviderCursor, providerusage.CursorCollector{}.Collect},
		{providerusage.ProviderCodex, providerusage.CodexCollector{}.Collect},
		{providerusage.ProviderCopilot, providerusage.CopilotCollector{}.Collect},
		{providerusage.ProviderAntigravity, providerusage.AntigravityCollector{}.Collect},
		{providerusage.ProviderGrok, providerusage.GrokCollector{}.Collect},
		{providerusage.ProviderKimi, providerusage.KimiCollector{}.Collect},
		{providerusage.ProviderKiro, providerusage.KiroCollector{}.Collect},
		{providerusage.ProviderOpenCode, providerusage.OpenCodeCollector{}.Collect},
	}
	for _, item := range collectors {
		if until, ok := backoffUntil[item.provider]; ok && now.Before(until) {
			continue
		}
		delete(backoffUntil, item.provider)
		result := item.collect(ctx)
		if result.Backoff > 0 {
			backoffUntil[item.provider] = now.Add(result.Backoff)
		}
		if !result.Upload {
			d.logger.Debug("provider usage collection kept the last snapshot", "provider", item.provider)
			continue
		}
		report := providerUsageReportFrom(result.Snapshot)
		for _, runtimeID := range d.allRuntimeIDs() {
			if err := d.client.ReportProviderUsage(ctx, runtimeID, report); err != nil {
				d.logger.Warn("provider usage upload failed", "provider", item.provider, "runtime_id", runtimeID, "error", err)
			}
		}
	}
}

func providerUsageReportFrom(snapshot providerusage.Snapshot) ProviderUsageReport {
	report := ProviderUsageReport{
		Provider:    snapshot.Provider,
		PlanName:    snapshot.PlanName,
		CollectedAt: snapshot.CollectedAt,
		ReasonCode:  snapshot.ReasonCode,
	}
	for _, window := range snapshot.Windows {
		report.Windows = append(report.Windows, ProviderUsageWindowReport{
			ID:          window.ID,
			PercentUsed: window.PercentUsed,
			ResetsAt:    window.ResetsAt,
		})
	}
	return report
}
