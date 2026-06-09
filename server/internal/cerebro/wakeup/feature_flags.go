// TECH-3176: per-trigger-type on/off gating for agent wakeups.
//
// Each wakeup trigger type maps to a cerebro feature flag whose default is ON
// (see packages/cerebro-feature-flags/registry.ts). Gating is read at the
// WORKSPACE level only — wakeups fire from background sweepers/listeners with
// no request actor, so personal overrides do not apply. A missing override row
// (or any lookup failure) resolves to enabled, preserving current behavior on
// deploy.
package wakeup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
)

// Feature-flag keys, kept in sync with packages/cerebro-feature-flags/registry.ts.
const (
	FlagWakeupTime        = "cerebro_wakeup_time"
	FlagWakeupIssueStatus = "cerebro_wakeup_issue_status"
	FlagWakeupGithubCI    = "cerebro_wakeup_github_ci"
)

// flagForTrigger returns the feature-flag key gating a trigger type, and whether
// a key is known for it. An unknown type is left ungated (enabled).
func flagForTrigger(triggerType string) (string, bool) {
	switch triggerType {
	case TriggerTime:
		return FlagWakeupTime, true
	case TriggerIssueStatus:
		return FlagWakeupIssueStatus, true
	case TriggerGithubCI:
		return FlagWakeupGithubCI, true
	default:
		return "", false
	}
}

// triggerTypeEnabled reports whether a trigger type is enabled for a workspace.
// Workspace override wins when present; otherwise the registry default (ON)
// applies. Any lookup error resolves to enabled so a transient DB issue never
// silently stops wakeups.
func (s *Service) triggerTypeEnabled(ctx context.Context, workspaceID pgtype.UUID, triggerType string) bool {
	key, known := flagForTrigger(triggerType)
	if !known {
		return true
	}
	if s.Cerebro == nil {
		return true
	}
	rows, err := s.Cerebro.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		slog.Warn("cerebro wakeup flag lookup failed", "error", err, "flag_key", key)
		return true
	}
	for _, row := range rows {
		if row.FlagKey == key {
			return row.Enabled
		}
	}
	return true
}
