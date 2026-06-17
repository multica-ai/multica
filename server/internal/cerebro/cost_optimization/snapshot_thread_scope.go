// CEREBRO: scope the daemon start-prompt snapshot to the triggering conversation
// thread on channel surfaces (TECH-3691). A named channel is a continuous,
// multi-topic surface: the flat "most recent N comments" snapshot can paste in
// messages from unrelated conversations. When an agent is woken by an @-mention
// in a channel it should only see the thread it was mentioned in, not whatever
// else happened to be said recently. DMs are intentionally left untouched — a DM
// is a single 1:1 relationship where the full recent window is the right context.
package cost_optimization

import (
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ScopeSnapshotToTriggerThread returns the subset of comments that belong to the
// triggering comment's thread — the thread root (the trigger's parent if it is a
// reply, otherwise the trigger itself) plus every direct reply to that root.
//
// It only narrows on channel surfaces with a resolvable trigger thread. For any
// other kind, an absent trigger, or a trigger not present in the supplied
// comments, it returns the input unchanged so issue and DM behavior is identical
// to before. The renderer still excludes the trigger comment itself (it is
// inlined separately), so a brand-new top-level mention with no replies yet
// simply yields an empty thread section rather than unrelated channel chatter.
func ScopeSnapshotToTriggerThread(kind string, comments []db.Comment, triggerCommentID pgtype.UUID) []db.Comment {
	if kind != "channel" || !triggerCommentID.Valid {
		return comments
	}

	root, ok := resolveThreadRoot(comments, triggerCommentID)
	if !ok {
		return comments
	}

	scoped := make([]db.Comment, 0, len(comments))
	for _, c := range comments {
		if c.ID == root || (c.ParentID.Valid && c.ParentID == root) {
			scoped = append(scoped, c)
		}
	}
	return scoped
}

// resolveThreadRoot finds the trigger comment and returns the UUID of its thread
// root: the trigger's parent when it is a reply, otherwise the trigger's own ID.
// ok is false when the trigger is not in the slice (caller then leaves the
// snapshot unscoped rather than guessing).
func resolveThreadRoot(comments []db.Comment, triggerCommentID pgtype.UUID) (pgtype.UUID, bool) {
	for _, c := range comments {
		if c.ID == triggerCommentID {
			if c.ParentID.Valid {
				return c.ParentID, true
			}
			return c.ID, true
		}
	}
	return pgtype.UUID{}, false
}
