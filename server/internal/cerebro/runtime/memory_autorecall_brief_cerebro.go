package runtime

// memory_autorecall_brief_cerebro.go (FIR-1794 layer 3) adapts the automatic
// memory recall to the daemon claim path. It lives here, in the runtime
// package — which already imports handler — so the handler package never
// imports runtime back (that would be a cycle). handler.applyMemoryAutoRecall
// calls this through the injected handler.CerebroMemoryAutoRecaller interface;
// the router wires *CerebroMemoryAutoRecall in as the concrete value. Same
// seam pattern as APIConnectionToolsForBrief.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"

	"github.com/multica-ai/multica/server/internal/handler"
)

// CerebroMemoryAutoRecall is the claim-path entry to the automatic memory
// recall in memory_autorecall_cerebro.go.
type CerebroMemoryAutoRecall struct {
	Cerebro *cerebrodb.Queries
}

// Compile-time proof the adapter satisfies the claim seam.
var _ handler.CerebroMemoryAutoRecaller = (*CerebroMemoryAutoRecall)(nil)

// AutoRecallBlock builds the bounded recall query from the task signals and
// returns the injectable block ("" when memory is off or nothing recalled).
func (a *CerebroMemoryAutoRecall) AutoRecallBlock(ctx context.Context, workspaceID, agentID, originUser pgtype.UUID, queryParts ...string) string {
	if a == nil || a.Cerebro == nil {
		return ""
	}
	return CerebroMemoryAutoRecallBlock(ctx, a.Cerebro, workspaceID, agentID, originUser, CerebroMemoryAutoRecallQuery(queryParts...))
}
