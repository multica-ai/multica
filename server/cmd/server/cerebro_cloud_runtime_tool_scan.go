package main

// CEREBRO-PATCH(cloud-runtime-tool-scan-meta): FIR-2284 — build the callable
// built-in tool list the cloud-runtime "Scan now" records. Mirrors the cloud
// tool seed filter (implemented + newly_implemented) so a cloud-runtime scan
// surfaces exactly the gateway tools an agent can actually call, never the
// explicitly-excluded ones.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrocapabilityregistry "github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/cloudtoolscan"
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	"github.com/multica-ai/multica/server/internal/util"
)

func callableCloudToolMeta() []cloudtoolscan.ToolMeta {
	meta := cerebroruntime.AllBuiltinToolMeta()
	out := make([]cloudtoolscan.ToolMeta, 0, len(meta))
	for _, m := range meta {
		if m.Status != cerebroruntime.ToolStatusImplemented &&
			m.Status != cerebroruntime.ToolStatusNewlyImplemented {
			continue
		}
		out = append(out, cloudtoolscan.ToolMeta{Name: m.Name, Description: m.Description})
	}
	return out
}

// CEREBRO-PATCH(cloud-runtime-capability-startup-scan): TECH-3657 — keep the
// capability register (the unified tool-policy matrix's source) in step with the
// gateway's built-in tool set on every server start. Without this, a cloud
// runtime's capability rows only refreshed on a manual "Scan now", so a tool
// added to legacyGatewayToolMeta after the last manual scan (e.g. create_file)
// stayed invisible in the admin matrix even though it was live and callable.
// Mirrors the inventory backfill (SeedBuiltinCloudToolsForAllRuntimes), which
// already runs on startup but only seeds cerebro_runtime_tool, not the register.
func seedCloudCapabilitiesForAllRuntimes(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("seed cloud capabilities: nil pool")
	}
	scanner := cloudtoolscan.New(
		cerebrocapabilityregistry.New(pool),
		runtimetools.New(pool),
		callableCloudToolMeta(),
	)

	rows, err := pool.Query(ctx, `
		SELECT id, workspace_id
		FROM agent_runtime
		WHERE runtime_mode = 'cloud' OR provider = 'firtal-gateway'`)
	if err != nil {
		return fmt.Errorf("list cloud runtimes: %w", err)
	}
	defer rows.Close()

	type cloudRuntime struct {
		id          pgtype.UUID
		workspaceID pgtype.UUID
	}
	var runtimes []cloudRuntime
	for rows.Next() {
		var rt cloudRuntime
		if err := rows.Scan(&rt.id, &rt.workspaceID); err != nil {
			return fmt.Errorf("scan cloud runtime: %w", err)
		}
		runtimes = append(runtimes, rt)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate cloud runtimes: %w", err)
	}

	for _, rt := range runtimes {
		if !rt.id.Valid || !rt.workspaceID.Valid {
			continue
		}
		if err := scanner.Scan(ctx, rt.id, rt.workspaceID); err != nil {
			// Never fail startup on one runtime — log and continue, mirroring
			// the inventory backfill's per-runtime tolerance.
			slog.Warn("cloud capability startup scan failed",
				"runtime_id", util.UUIDToString(rt.id),
				"error", err,
			)
			continue
		}
	}
	return nil
}
