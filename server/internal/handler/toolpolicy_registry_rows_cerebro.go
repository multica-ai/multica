package handler

// CEREBRO-PATCH(toolpolicy-registry-fold-in): FIR-1609 Phase 5 — supply the
// firtal_registry per-data-source projection to the unified tool-policy table.
//
// The tool-policy table handler (internal/cerebro/toolpolicy) is pure: it knows
// nothing about the Firtal Data Registry proxy. This
// file lives in package handler — which already owns the FDR data-source lister
// and the DB pool — and is injected into the tool-policy handler as its
// RegistryRows hook (wired in cmd/server/router.go). It reads the workspace's
// data source catalog, maps it onto the toolpolicy projection types, and returns
// ok=false whenever the fold-in does not apply, so the table is left untouched.

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// toolPolicyRegistryRows implements cerebrotoolpolicy.RegistryRowsFunc. It returns
// the data-source catalog for a workspace, or
// ok=false when there is nothing to project (registry not configured, no data
// sources). Errors loading the grant or the catalog are returned so the caller
// can log them, but never block the rest of the table.
func (h *Handler) ToolPolicyRegistryRows(ctx context.Context, workspaceID, agentID pgtype.UUID) (cerebrotoolpolicy.RegistryProjection, bool, error) {
	if !workspaceID.Valid {
		return cerebrotoolpolicy.RegistryProjection{}, false, nil
	}

	dataSources, err := h.listFirtalRegistryDataSourcesForWorkspace(ctx, workspaceID)
	if err != nil {
		return cerebrotoolpolicy.RegistryProjection{}, false, err
	}
	if len(dataSources) == 0 {
		// Registry not configured for this workspace, or an empty catalog: nothing
		// to fold in. Not an error — most workspaces have no FDR.
		return cerebrotoolpolicy.RegistryProjection{}, false, nil
	}

	rows := make([]cerebrotoolpolicy.RegistryDataSource, 0, len(dataSources))
	for _, ds := range dataSources {
		rows = append(rows, cerebrotoolpolicy.RegistryDataSource{ID: ds.ID, Name: ds.Name})
	}
	return cerebrotoolpolicy.RegistryProjection{DataSources: rows}, true, nil
}

// listFirtalRegistryDataSourcesForWorkspace returns the workspace's FDR data
// source catalog, served from the same per-workspace cache the admin picker uses.
// It returns (nil, nil) — not an error — when the registry is not configured, so
// a workspace without FDR simply gets no fold-in rows.
func (h *Handler) listFirtalRegistryDataSourcesForWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]FirtalRegistryDataSource, error) {
	wsID := workspaceID.String()
	if cached, ok := getCachedFirtalRegistryDataSources(wsID); ok {
		return cached, nil
	}

	baseURL, apiKey, err := h.loadFirtalRegistryCredentials(ctx, workspaceID)
	if err != nil {
		// Not configured is the common case for workspaces without FDR; treat it as
		// "no data sources" rather than an error so the table fold-in is silent.
		return nil, nil
	}

	items, err := fetchFirtalRegistryDataSources(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	setCachedFirtalRegistryDataSources(wsID, items)
	return items, nil
}
