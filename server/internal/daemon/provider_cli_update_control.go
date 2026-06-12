package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (d *Daemon) handleProviderCLIUpdate(ctx context.Context, runtimeID string, update *PendingProviderCLIUpdate) {
	if update == nil {
		return
	}
	d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "running"})
	mode, err := parseProviderCLIUpdateMode(update.Mode)
	if err != nil {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": err.Error()})
		return
	}
	if mode != ProviderCLIUpdateDryRun && mode != ProviderCLIUpdateApply {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": "mode must be dry-run or apply"})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(update.Provider))
	rt := d.findRuntime(runtimeID)
	if rt == nil {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": "runtime not found on daemon"})
		return
	}
	if runtimeProvider := strings.ToLower(strings.TrimSpace(rt.Provider)); provider != runtimeProvider {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": fmt.Sprintf("provider %q does not match runtime provider %q", provider, runtimeProvider)})
		return
	}
	entry, ok := d.cfg.Agents[provider]
	if !ok {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": fmt.Sprintf("provider %q is not configured on this daemon", provider)})
		return
	}
	plan, err := d.buildProviderCLIUpdateControlPlan(ctx, provider, entry, update, mode)
	if err != nil {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": err.Error()})
		return
	}
	if !plan.Valid {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": plan.InvalidReason})
		return
	}
	if mode == ProviderCLIUpdateDryRun {
		out, _ := json.Marshal(plan)
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "completed", "output": string(out)})
		return
	}
	if err := d.applyProviderCLIUpdateWithMode(ctx, plan, ProviderCLIUpdateApply, false, false); err != nil {
		d.reportProviderCLIUpdateResult(ctx, runtimeID, update.ID, map[string]any{"status": "failed", "error": err.Error()})
		return
	}
	out, _ := json.Marshal(map[string]any{"provider": plan.Provider, "target_version": plan.TargetVersion, "status": string(providerCLIUpdatePendingVerify)})
	reportCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	d.reportProviderCLIUpdateResult(reportCtx, runtimeID, update.ID, map[string]any{"status": "completed", "output": string(out)})
	cancel()
	d.triggerRestart()
}

func (d *Daemon) buildProviderCLIUpdateControlPlan(ctx context.Context, provider string, entry AgentEntry, update *PendingProviderCLIUpdate, mode ProviderCLIUpdateMode) (ProviderCLIUpdatePlan, error) {
	source, ok := providerCLISources[provider]
	if !ok {
		return d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{Provider: provider, Mode: string(mode)}), nil
	}
	current := d.agentVersion(provider)
	if current == "" {
		detectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if version, err := detectAgentVersion(detectCtx, entry.Path); err == nil {
			current = version
		}
	}
	latest := ""
	target := strings.TrimSpace(update.TargetVersion)
	pinned := ""
	if target == "" {
		pinned = d.cfg.ProviderCLIPinnedVersions[provider]
		if pinned == "" {
			latestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			out, err := providerCLICommandRunner(latestCtx, source.LatestVersionCommandTemplate)
			if err != nil {
				return ProviderCLIUpdatePlan{}, fmt.Errorf("fetch latest provider version: %w", err)
			}
			latest = strings.TrimSpace(out)
		}
	}
	rollback := strings.TrimSpace(update.RollbackVersion)
	if rollback == "" {
		rollback = d.cfg.ProviderCLIRollbackVersions[provider]
	}
	installPath, installPrefix, err := providerCLIInstallLocation(entry.Path, d.cfg.ProviderCLIInstallPrefixes[provider])
	if err != nil {
		return ProviderCLIUpdatePlan{}, err
	}
	return d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{Provider: provider, CurrentVersion: current, LatestVersion: latest, TargetVersion: target, PinnedVersion: pinned, RollbackVersion: rollback, InstallPath: installPath, InstallPrefix: installPrefix, Mode: string(mode)}), nil
}

func (d *Daemon) reportProviderCLIUpdateResult(ctx context.Context, runtimeID, updateID string, payload map[string]any) {
	d.reportUpdateResultWithRetry(ctx, runtimeID, updateID, func(ctx context.Context) error {
		return d.client.ReportProviderCLIUpdateResult(ctx, runtimeID, updateID, payload)
	})
}
