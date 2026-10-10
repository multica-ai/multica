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
	// Left uncollected after CodeNotch 1.22.0 (v1.21.0...v1.22.0):
	//   Qoder — qoder and qoderclicn are Multica runtimes, but the credit
	//     ring is a WebKit sign-in on qoder.com or qoder.com.cn
	//     (GET /api/v2/me/usages/big_model_credits). That is a website
	//     cookie, not a local CLI session.
	//   Command Code — not a Multica runtime. Extra ~/.commandcode-<slug>
	//     homes each hold an apiKey; they do not add a runtime to attach.
	//   DeepSeek — platform.deepseek.com wallet. Picking the funded wallet
	//     still reports prepaid spend, not a signed-in plan window. The dsh
	//     runtime does not expose that wallet locally.
	// Qianwen, Gemini CLI, Amp, Devin, GLM, MiniMax, Ollama, LM Studio, and
	// Perplexity stay out for the same reasons: not a Multica runtime, or
	// only a browser cookie or a local token ledger.
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
