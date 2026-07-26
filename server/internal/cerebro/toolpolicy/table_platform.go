package toolpolicy

// table_platform.go appends the Multica platform actions (FIR-2594) to the admin
// table. table.go lists the capability-wide rows for every tool a runtime
// reported into cerebro_capability; table_repo.go adds the per-repo rows. Neither
// covers the platform's OWN mutating operations — create an issue, add a comment,
// trigger an autopilot, manage agents/runtimes/grants — because no runtime
// reports them. The platformcatalog package is the code-owned source of truth for
// those; here we turn each catalog entry into one capability-wide row carrying its
// stored per-layer settings and resolved Effective verdict, so an admin sees and
// sets Allow/Ask/Deny on platform actions in the same screen as runtime tools.
//
// Platform rows are emitted on every view, runtime-scoped included: a platform
// action is workspace-global, not tied to the machine an agent runs on, so the
// runtime filter in table.go (which keeps only that runtime's reported tools)
// must not hide them — exactly the reasoning appendRepoRows uses for repo rows.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

// appendPlatformRows adds one row per platform capability (platformcatalog.All)
// with the explicit per-layer settings authored for it in the query's context
// and the resolved Effective verdict. groupIDs is the already-resolved group set
// for the context (see Table), reused so the Group layer resolves against the
// same groups as the capability-wide rows.
func (s *Store) appendPlatformRows(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, out []TableRow) ([]TableRow, error) {
	// IncludePlatform emits the whole catalog; otherwise (agent-start gate only)
	// emit just the surfaced family, so the light surface never leaks the rest of
	// the platform actions (FIR-3091 slice 4).
	caps := platformcatalog.All()
	if !in.IncludePlatform {
		surfaced := caps[:0]
		for _, c := range caps {
			if c.Surfaced {
				surfaced = append(surfaced, c)
			}
		}
		caps = surfaced
	}
	if len(caps) == 0 {
		return out, nil
	}

	keys := make([]string, len(caps))
	for i, c := range caps {
		keys[i] = c.Key
	}
	settings, err := s.loadResourcePolicySettings(ctx, in, groupIDs, resourcePolicyFilter{
		toolKeys: keys,
		scope:    resourcePatternEmpty,
	})
	if err != nil {
		return nil, err
	}

	for _, c := range caps {
		row := TableRow{
			ToolKey:               c.Key,
			Title:                 c.Title,
			Category:              c.Category,
			Source:                platformcatalog.Source,
			ManagedExternally:     c.ManagedExternally,
			ExternalSecurityOwner: c.ExternalSecurityOwner,
			Layers:                map[Layer]Setting{},
			Conditions:            map[Layer]*Condition{},
		}
		// Externally enforced capabilities are visible for auditability but never
		// read a stored policy row. Legacy advisory rows must not make Settings
		// imply that an Allow/Ask/Deny choice changes the live security boundary.
		if cell, ok := settings[resourcePolicyKey{toolKey: c.Key}]; ok && !c.ManagedExternally {
			for l, set := range cell.layers {
				row.Layers[l] = set
			}
			for l, cond := range cell.conditions {
				row.Conditions[l] = cond
			}
			if len(cell.groups) > 0 {
				row.Layers[LayerGroup] = CombineGroups(cell.groups...)
			}
		}
		row.Effective, err = s.resolveTablePermission(ctx, in, c.Key, row.Layers)
		if err != nil {
			return nil, fmt.Errorf("toolpolicy: resolve platform permission %q: %w", c.Key, err)
		}
		out = append(out, row)
	}
	return out, nil
}
