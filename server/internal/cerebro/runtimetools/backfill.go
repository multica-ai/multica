// Backfill of the per-runtime tool registry from the legacy world (JEH-1710 bid 6).
//
// Before bid 6, agents picked up the legacy `agent_tool_grant` rows directly
// in the firtal gateway executor, and the per-runtime registry from migration
// 9032 was unused. Bid 5 wired the cascade so the executor prefers
// cerebro_runtime_tool grants when the runtime has rows, falling back to
// legacy when it doesn't. That fallback path stays for autopilot / no-user
// runs but otherwise needs to die — which only happens once every existing
// runtime has at least one row in cerebro_runtime_tool.
//
// Two things make that true:
//
//   - Cloud tools (built-in, defined in package runtime via legacyGatewayToolMeta
//     and multicaMCPToolMatrix) are static across deploys. We seed them
//     here, idempotently, for every runtime on every server start.
//   - MCP tools are dynamic per runtime — the daemon scans them via
//     mcpscan and reports back through RecordScan within 15 minutes of a
//     deploy. No seeding needed.
//
// The seeder also validates each runtime's tools_config JSON so a corrupt
// blob logs a clear warning the admin can act on. PRD asked for a UI
// surface for malformed configs; for now we only log.

package runtimetools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

// BackfillToolMeta is the static metadata for one cloud (built-in) tool the
// seeder needs to insert. It mirrors the relevant fields of the
// runtime.ToolMeta type without taking a dependency on package runtime
// (which would cause an import cycle: runtimetools is imported by main, and
// runtime imports runtimetools transitively through the executor seam).
type BackfillToolMeta struct {
	Name        string
	Description string
}

// SeedCloudToolsForAllRuntimes is the bid 6 data backfill: every existing
// agent_runtime row gets a cerebro_runtime_tool row for every cloud built-in
// tool, with enabled=true so admins/owners (who bypass via
// ResolveCerebroAgentToolAccess) can use them immediately. Non-admin users
// still need an explicit user_grant or group_grant — that is the PRD's
// default-deny rule and is enforced by the cascade query, not by this seed.
//
// The function is idempotent: re-running it for the same set of tools and
// runtimes is a no-op (ON CONFLICT preserves existing rows, including any
// admin who has disabled a tool via the admin UI).
//
// Tools listed in `cloud` should be the callable cloud-side built-ins —
// excluded / not-yet-implemented tools should be filtered out by the caller
// so the admin UI does not advertise them.
//
// Malformed tools_config JSON is logged at WARN level with the runtime id
// so the admin can find and fix it. We never block startup on a malformed
// row — the daemon's parseMcpConfig already tolerates that, and the seeder
// must not make a single bad row fatal for the rest.
func SeedCloudToolsForAllRuntimes(ctx context.Context, pool *pgxpool.Pool, cloud []BackfillToolMeta) error {
	if pool == nil {
		return fmt.Errorf("seed cloud tools: nil pool")
	}
	if len(cloud) == 0 {
		// No cloud tools to seed — still scan tools_config for malformed
		// rows so the admin gets the warning regardless.
		return scanToolsConfigForMalformed(ctx, pool)
	}

	runtimes, err := listRuntimesForSeed(ctx, pool)
	if err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}

	svc := New(pool)
	enabled := true
	for _, rt := range runtimes {
		if !rt.id.Valid {
			continue
		}
		if rt.toolsConfig != nil {
			if jerr := validateToolsConfig(rt.toolsConfig); jerr != nil {
				slog.Warn("runtime tools_config is malformed; admin must fix",
					"runtime_id", util.UUIDToString(rt.id),
					"error", jerr,
				)
			}
		}
		for _, t := range cloud {
			if t.Name == "" {
				continue
			}
			if _, err := svc.UpsertTool(ctx, UpsertToolInput{
				RuntimeID:   rt.id,
				ToolName:    t.Name,
				Source:      SourceCloud,
				Description: t.Description,
				Enabled:     &enabled,
			}); err != nil {
				return fmt.Errorf("upsert cloud tool %s for runtime %s: %w",
					t.Name, util.UUIDToString(rt.id), err)
			}
		}
	}
	return nil
}

// scanToolsConfigForMalformed is the no-cloud-tools branch: walk every
// runtime, validate tools_config JSON, log warnings. Kept as a separate
// function so the no-op case is cheap and explicit.
func scanToolsConfigForMalformed(ctx context.Context, pool *pgxpool.Pool) error {
	runtimes, err := listRuntimesForSeed(ctx, pool)
	if err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}
	for _, rt := range runtimes {
		if rt.toolsConfig == nil {
			continue
		}
		if jerr := validateToolsConfig(rt.toolsConfig); jerr != nil {
			slog.Warn("runtime tools_config is malformed; admin must fix",
				"runtime_id", util.UUIDToString(rt.id),
				"error", jerr,
			)
		}
	}
	return nil
}

// seedRuntime is the row shape the seeder reads from agent_runtime. We
// keep it private and pull only the columns we need so changes to other
// runtime columns don't ripple through.
type seedRuntime struct {
	id          pgtype.UUID
	toolsConfig []byte
}

func listRuntimesForSeed(ctx context.Context, pool *pgxpool.Pool) ([]seedRuntime, error) {
	rows, err := pool.Query(ctx, `SELECT id, tools_config FROM agent_runtime`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []seedRuntime
	for rows.Next() {
		var r seedRuntime
		if err := rows.Scan(&r.id, &r.toolsConfig); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// validateToolsConfig returns nil if the blob is empty or parses as the
// Claude Code mcp-config shape, and an error describing the problem
// otherwise. The shape rule is intentionally loose — extra top-level keys
// are fine (Claude Code tolerates them), but mcpServers must be either
// absent or an object.
func validateToolsConfig(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("tools_config is not a JSON object: %w", err)
	}
	servers, ok := doc["mcpServers"]
	if !ok || len(servers) == 0 {
		return nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(servers, &asMap); err != nil {
		return fmt.Errorf("mcpServers is not a JSON object: %w", err)
	}
	return nil
}

