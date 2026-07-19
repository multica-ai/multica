package workflows

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// FlagWorkflowsEngine is the cerebro feature flag (a sub-flag under
// cerebro_workflows) that runs the workflow engine for a workspace. It
// replaces the CEREBRO_WORKFLOWS_ENABLED env var as the primary toggle so the
// engine can be flipped per workspace from the feature-flag UI without a
// redeploy. The env var (Service.enabled) remains a global force-on master
// switch for ops / tests / self-host.
const FlagWorkflowsEngine = "cerebro_workflows_engine"

// engineFlagTTL bounds how long a per-workspace engine-flag resolution is
// cached. The listener consults engineEnabledForWorkspace on every bus event,
// so caching keeps the hot path off the DB while still picking up a toggle
// within a minute.
const engineFlagTTL = time.Minute

type engineFlagEntry struct {
	enabled   bool
	expiresAt time.Time
}

// engineEnabledForWorkspace reports whether the workflow engine should act for
// the given workspace. The env master switch (Service.enabled) forces it on
// everywhere; otherwise the per-workspace cerebro_workflows_engine feature
// flag decides (default off). Resolutions are cached for engineFlagTTL.
func (s *Service) engineEnabledForWorkspace(ctx context.Context, wsID pgtype.UUID) bool {
	if s.enabled {
		return true
	}
	if s.queries == nil || !wsID.Valid {
		return false
	}
	if v, ok := s.engineFlagCache.Load(wsID.Bytes); ok {
		if e := v.(engineFlagEntry); nowFunc().Before(e.expiresAt) {
			return e.enabled
		}
	}
	rows, err := s.queries.ListCerebroWorkspaceFeatureFlags(ctx, wsID)
	if err != nil {
		slog.Warn("workflow engine flag: workspace lookup failed",
			"workspace_id", uuidString(wsID), "error", err)
		return false
	}
	on := workflowsEngineFlagOn(rows)
	s.engineFlagCache.Store(wsID.Bytes, engineFlagEntry{enabled: on, expiresAt: nowFunc().Add(engineFlagTTL)})
	return on
}

// workflowsEngineFlagOn is the pure resolution of the engine flag from a
// workspace's feature-flag override rows: the workspace override wins when
// present, otherwise the engine defaults off.
func workflowsEngineFlagOn(rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow) bool {
	for _, r := range rows {
		if r.FlagKey == FlagWorkflowsEngine {
			return r.Enabled
		}
	}
	return false
}
