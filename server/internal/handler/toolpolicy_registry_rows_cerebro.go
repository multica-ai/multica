package handler

// CEREBRO-PATCH(toolpolicy-registry-fold-in): FIR-1609 Phase 5 — supply the
// firtal_registry per-data-source projection to the unified tool-policy table.
//
// The tool-policy table handler (internal/cerebro/toolpolicy) is pure: it knows
// nothing about the Firtal Data Registry proxy or about agent_tool_grant. This
// file lives in package handler — which already owns the FDR data-source lister
// and the DB pool — and is injected into the tool-policy handler as its
// RegistryRows hook (wired in cmd/server/router.go). It reads the agent's
// firtal_registry grant (agent_tool_grant.config_json) and the workspace's data
// source catalog, maps both onto the toolpolicy projection types, and returns
// ok=false whenever the fold-in does not apply, so the table is left untouched.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// toolPolicyRegistryRows implements cerebrotoolpolicy.RegistryRowsFunc. It returns
// the data-source catalog + the agent's grant for a (workspace, agent), or
// ok=false when there is nothing to project (registry not configured, no data
// sources). Errors loading the grant or the catalog are returned so the caller
// can log them, but never block the rest of the table.
func (h *Handler) ToolPolicyRegistryRows(ctx context.Context, workspaceID, agentID pgtype.UUID) (cerebrotoolpolicy.RegistryProjection, bool, error) {
	if !agentID.Valid || !workspaceID.Valid {
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

	grant, err := h.loadFirtalRegistryGrant(ctx, agentID)
	if err != nil {
		return cerebrotoolpolicy.RegistryProjection{}, false, err
	}

	rows := make([]cerebrotoolpolicy.RegistryDataSource, 0, len(dataSources))
	for _, ds := range dataSources {
		rows = append(rows, cerebrotoolpolicy.RegistryDataSource{ID: ds.ID, Name: ds.Name})
	}
	return cerebrotoolpolicy.RegistryProjection{DataSources: rows, Grant: grant}, true, nil
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

// loadFirtalRegistryGrant reads the agent's enabled firtal_registry grant
// (agent_tool_grant.config_json) and reduces it to the projection's RegistryGrant.
// A missing or disabled grant yields a zero grant (deny-by-default: no source
// allowed), which is correct — the projection then marks every source Deny.
func (h *Handler) loadFirtalRegistryGrant(ctx context.Context, agentID pgtype.UUID) (cerebrotoolpolicy.RegistryGrant, error) {
	var raw []byte
	err := h.DB.QueryRow(ctx,
		`SELECT config_json FROM agent_tool_grant WHERE agent_id = $1 AND tool_name = $2 AND enabled = true`,
		agentID, cerebrotoolpolicy.RegistryToolKey,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrotoolpolicy.RegistryGrant{}, nil
		}
		return cerebrotoolpolicy.RegistryGrant{}, err
	}
	if len(raw) == 0 {
		return cerebrotoolpolicy.RegistryGrant{}, nil
	}

	var cfg struct {
		AllowedDataSources []string `json:"allowed_data_sources"`
		AllowedAll         bool     `json:"allowed_data_sources_all"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		// A malformed config is treated as no grant (deny-by-default) rather than a
		// hard failure, matching the gateway's defensive parse.
		return cerebrotoolpolicy.RegistryGrant{}, nil
	}
	return cerebrotoolpolicy.RegistryGrant{
		AllowedAll:         cfg.AllowedAll,
		AllowedDataSources: cfg.AllowedDataSources,
	}, nil
}
