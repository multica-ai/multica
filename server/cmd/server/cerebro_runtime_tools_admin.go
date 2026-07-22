package main

// CEREBRO-PATCH(router-runtime-tools-admin): FIR-3403 keeps the existing
// runtime-tools HTTP contract while sourcing inventory from the capability
// register and authoring access exclusively through canonical tool policy.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
)

type runtimeToolsAdminAdapter struct {
	pool     *pgxpool.Pool
	registry *capabilityregistry.Service
	policy   *toolpolicy.Store
}

func newRuntimeToolsAdminAdapter(pool *pgxpool.Pool, registry *capabilityregistry.Service, policy *toolpolicy.Store) *runtimeToolsAdminAdapter {
	return &runtimeToolsAdminAdapter{pool: pool, registry: registry, policy: policy}
}

func (a *runtimeToolsAdminAdapter) runtimeWorkspace(ctx context.Context, runtimeID pgtype.UUID) (pgtype.UUID, error) {
	var workspaceID pgtype.UUID
	if err := a.pool.QueryRow(ctx, `SELECT workspace_id FROM agent_runtime WHERE id=$1`, runtimeID).Scan(&workspaceID); err != nil {
		return pgtype.UUID{}, err
	}
	return workspaceID, nil
}

func (a *runtimeToolsAdminAdapter) capabilities(ctx context.Context, runtimeID pgtype.UUID) (pgtype.UUID, []capabilityregistry.View, error) {
	workspaceID, err := a.runtimeWorkspace(ctx, runtimeID)
	if err != nil {
		return pgtype.UUID{}, nil, err
	}
	subject := capabilityregistry.Subject{Type: "runtime", ID: util.UUIDToString(runtimeID)}
	caps, err := a.registry.List(ctx, workspaceID, &subject, nil)
	return workspaceID, caps, err
}

func (a *runtimeToolsAdminAdapter) ListTools(ctx context.Context, runtimeID pgtype.UUID) ([]handler.RuntimeToolView, error) {
	workspaceID, caps, err := a.capabilities(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	settings, err := a.policy.ListForSubject(ctx, workspaceID, toolpolicy.LayerRuntime, runtimeID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]toolpolicy.Setting, len(settings))
	for _, setting := range settings {
		if setting.ResourcePattern == "" {
			byKey[setting.ToolKey] = setting.Setting
		}
	}
	out := make([]handler.RuntimeToolView, 0, len(caps))
	for _, cap := range caps {
		out = append(out, capabilityToRuntimeTool(cap, runtimeID, byKey[cap.Key] != toolpolicy.SettingDeny))
	}
	return out, nil
}

// Scan freshness is evidence-owned: capability reports update last_reported_at.
func (a *runtimeToolsAdminAdapter) StampScanned(context.Context, pgtype.UUID) error { return nil }

func capabilityToRuntimeTool(cap capabilityregistry.View, runtimeID pgtype.UUID, enabled bool) handler.RuntimeToolView {
	source := cap.Source
	serverName := ""
	if value, ok := cap.Metadata["server_name"].(string); ok {
		serverName = value
	}
	if serverName != "" {
		source = "mcp"
	}
	last := cap.LastReportedAt.UTC().Format(time.RFC3339)
	return handler.RuntimeToolView{ID: cap.ID, RuntimeID: util.UUIDToString(runtimeID), Name: cap.Key, Source: source, MCPServerName: serverName, Description: cap.Description, Enabled: enabled, LastScannedAt: &last}
}
