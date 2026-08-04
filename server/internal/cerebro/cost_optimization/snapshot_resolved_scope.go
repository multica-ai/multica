// CEREBRO: keep settled discussion out of the daemon start-prompt snapshot
// (FIR-4500). A resolved thread root means the conversation is closed — the CLI
// already folds those away on `multica issue comment list`. The snapshot did not,
// so every run re-read discussion nobody needs, and a Handoff with --start-new
// (which closes the old thread and opens a fresh one) had the very thread it just
// closed pasted back into the fresh run's start prompt.
package cost_optimization

import (
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DropResolvedThreads removes every comment whose thread root is resolved. The
// triggering comment's own thread is always kept: an agent woken inside a
// resolved thread still needs the context it was woken in.
func DropResolvedThreads(comments []db.Comment, triggerCommentID pgtype.UUID) []db.Comment {
	keepRoot, hasKeep := resolveThreadRoot(comments, triggerCommentID)

	resolved := make(map[pgtype.UUID]bool, len(comments))
	for _, c := range comments {
		if !c.ParentID.Valid && c.ResolvedAt.Valid && !(hasKeep && c.ID == keepRoot) {
			resolved[c.ID] = true
		}
	}
	if len(resolved) == 0 {
		return comments
	}

	kept := make([]db.Comment, 0, len(comments))
	for _, c := range comments {
		root := c.ID
		if c.ParentID.Valid {
			root = c.ParentID
		}
		if !resolved[root] {
			kept = append(kept, c)
		}
	}
	return kept
}
