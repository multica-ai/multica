// CEREBRO-PATCH(workspace-copy): TECH-3742 — name-conflict policy.
//
// Uniquely-named entities (agent, skill, connection, group) can clash with an
// existing same-name row in the target workspace. The console asks the admin
// what to do per item; the chosen policy travels on the copy request:
//
//   - skip:      reuse the existing target row (map source → existing, copy no
//     new row). The safe default for a merge — never duplicates a
//     governance skill or an agent that already lives in the target.
//   - overwrite: update the existing target row's carried fields from the
//     source, then map to it. (Skills/connections fall back to skip;
//     only agents implement a true overwrite today.)
//   - keep_both: create a new row with a numeric-suffixed name ("Name (2)").
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type conflictPolicy string

const (
	conflictSkip      conflictPolicy = "skip"
	conflictOverwrite conflictPolicy = "overwrite"
	conflictKeepBoth  conflictPolicy = "keep_both"
)

// parseConflictPolicy maps a request string to a policy, defaulting to skip
// (the safe merge default) for empty or unknown values.
func parseConflictPolicy(s string) conflictPolicy {
	switch conflictPolicy(s) {
	case conflictOverwrite:
		return conflictOverwrite
	case conflictKeepBoth:
		return conflictKeepBoth
	default:
		return conflictSkip
	}
}

// uniqueName returns a name not present in the target workspace's table by
// appending " (N)" with the smallest N ≥ 2 that is free. table must be one of
// the uniquely-named entity tables (skill, agent, workspace_connection,
// cerebro_group) — the column is always (workspace_id, name).
func uniqueName(ctx context.Context, tx pgx.Tx, table string, targetWorkspace pgtype.UUID, base string) (string, error) {
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s (%d)", base, n)
		var exists bool
		// table is a fixed internal constant, never user input — safe to inline.
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM `+table+` WHERE workspace_id = $1 AND name = $2)`,
			targetWorkspace, candidate).Scan(&exists); err != nil {
			return "", fmt.Errorf("unique-name probe: %w", err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find a free name for %q in target", base)
}
