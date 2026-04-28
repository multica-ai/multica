package util

import (
	"hash/fnv"

	"github.com/jackc/pgx/v5/pgtype"
)

// workspaceColorPalette is the deterministic color set used by the
// cross-workspace meta view. Frontend trusts the server-derived value, so the
// palette and hash function must stay stable across releases — changing either
// shifts every existing workspace to a new color. See ADR 0001.
var workspaceColorPalette = []string{
	"#ef4444", "#f97316", "#f59e0b", "#eab308",
	"#84cc16", "#22c55e", "#10b981", "#14b8a6",
	"#06b6d4", "#3b82f6", "#8b5cf6", "#ec4899",
}

// WorkspaceColor maps a workspace UUID to a stable palette color via a
// FNV-1a 32-bit hash over the 16 raw UUID bytes, modulo the palette length.
// Returns "" when the UUID is not valid (caller decides how to render).
func WorkspaceColor(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	h := fnv.New32a()
	h.Write(id.Bytes[:])
	return workspaceColorPalette[h.Sum32()%uint32(len(workspaceColorPalette))]
}
