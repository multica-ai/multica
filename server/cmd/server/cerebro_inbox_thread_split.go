package main

import (
	"context"
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cerebroChannelThreadDetails enriches a channel/DM comment's inbox_item
// details with its thread root so the dynamic inbox can split a reply that
// lands inside a thread into its own inbox row (FIR-1854). For a top-level
// message — or any non-channel issue — it returns the details unchanged.
//
// The dynamic inbox groups channel reply rows by details.thread_root_id and
// renders one row per thread that has unread replies, deep-linking into the
// thread via details.comment_id. Gated client-side by the
// cerebro_inbox_thread_split feature flag.
func cerebroChannelThreadDetails(
	ctx context.Context,
	queries *db.Queries,
	issueKind, workspaceID, commentID string,
	existing []byte,
) []byte {
	if issueKind != "channel" && issueKind != "dm" {
		return existing
	}
	commentUUID, err := util.ParseUUID(commentID)
	if err != nil {
		return existing
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return existing
	}
	// GetThreadRoot walks parent_id up to the row whose parent_id IS NULL; for
	// a root comment it returns that comment itself.
	root, err := queries.GetThreadRoot(ctx, db.GetThreadRootParams{
		CommentID:   commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return existing
	}
	rootID := util.UUIDToString(root.ID)
	// A top-level message is its own thread root — nothing to split out.
	if rootID == "" || rootID == commentID {
		return existing
	}
	enriched, err := json.Marshal(map[string]string{
		"comment_id":          commentID,
		"thread_root_id":      rootID,
		"thread_root_preview": cerebroThreadPreview(root.Content),
	})
	if err != nil {
		return existing
	}
	return enriched
}

// cerebroThreadPreview trims a thread root's body to a short, single-line
// label for the inbox thread row.
func cerebroThreadPreview(content string) string {
	const max = 140
	out := make([]rune, 0, max)
	for _, r := range content {
		switch r {
		case '\n', '\r', '\t':
			r = ' '
		}
		out = append(out, r)
		if len(out) >= max {
			break
		}
	}
	return string(out)
}
