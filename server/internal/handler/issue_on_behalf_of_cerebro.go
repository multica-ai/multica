package handler

// FIR-4930 — explicit "on behalf of" on an issue.
//
// Background. MUL-2553 introduced a DERIVED human origin: an agent-created
// issue is stamped origin_type='agent_task' + origin_id=<agent_task_queue.id>,
// and the human is read back through agent_task_queue.original_user_id. For a
// single delegation chain that is exactly right.
//
// It breaks for an autopilot that fans work out to many different owners. Every
// task the autopilot starts carries the AUTOPILOT CREATOR as original_user_id,
// so every issue the agent files resolves to that one person — regardless of
// who actually owns the work. FIR-4921 (Deploy review: invoice-warnings) is the
// concrete case: the reviews are owned per app, but all of them attributed to
// the workspace owner and landed in his inbox.
//
// The fix is an explicit override column, issue.on_behalf_of_user_id. The rule
// everywhere is the same and it matters that it is the same in all three
// places (read, list filter, auto-subscribe):
//
//	explicit stamp set  → that member, and ONLY that member
//	explicit stamp NULL → fall back to the derived agent_task chain
//
// If the explicit value were merely OR-ed into the filter instead of taking
// precedence, an issue stamped for the app owner would STILL match the
// autopilot creator through the derived chain, and the wrong human would keep
// seeing it — which is the whole complaint.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolveExplicitOnBehalfOf returns the human explicitly stamped on the issue,
// or nil when the issue carries no explicit stamp (the caller then falls back
// to the derived agent_task chain).
func (h *Handler) resolveExplicitOnBehalfOf(ctx context.Context, issue db.Issue) *OnBehalfOfRef {
	if !issue.OnBehalfOfUserID.Valid {
		return nil
	}
	user, err := h.Queries.GetUser(ctx, issue.OnBehalfOfUserID)
	if err != nil {
		return nil
	}
	return &OnBehalfOfRef{
		UserID:    uuidToString(issue.OnBehalfOfUserID),
		Name:      user.Name,
		AvatarURL: textToPtr(user.AvatarUrl),
	}
}

// onBehalfOfWherePredicate is the shared list/count filter for
// ?on_behalf_of_ids=. Explicit stamp wins; the derived agent_task chain only
// applies to issues with no explicit stamp.
func onBehalfOfWherePredicate(ids []pgtype.UUID, addArg func(any) string) string {
	arg := addArg(ids)
	return fmt.Sprintf(`(
    i.on_behalf_of_user_id = ANY(%[1]s::uuid[])
    OR (
        i.on_behalf_of_user_id IS NULL
        AND i.origin_type = 'agent_task'
        AND EXISTS (
            SELECT 1 FROM agent_task_queue atq
             WHERE atq.id = i.origin_id
               AND atq.original_user_id = ANY(%[1]s::uuid[])
        )
    )
)`, arg)
}

// errOnBehalfOfNotAMember is the fail-closed rejection message. It is phrased
// for the CLI user, who passed a name or a UUID and needs to know which of the
// two checks rejected it.
const errOnBehalfOfNotAMember = "on_behalf_of_user_id must be an active member of this workspace"

// validateOnBehalfOfUserID resolves the requested human and enforces the
// authorization boundary. Deliberately fail-closed but no tighter than the use
// case needs:
//
//   - the target must be a MEMBER (a real human) of THIS workspace — an agent
//     id, a member of another workspace, or a stale user id is rejected;
//   - an agent may stamp any member of its own workspace, not only the human
//     who happened to trigger its current run. Restricting it to the trigger
//     would re-create the bug: the Deploy reviews autopilot is triggered by the
//     workspace owner and must stamp the app owner.
//
// Returns ok=false after writing the HTTP error.
func (h *Handler) validateOnBehalfOfUserID(ctx context.Context, w http.ResponseWriter, workspaceID pgtype.UUID, raw string) (pgtype.UUID, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		// Explicit empty string clears the stamp (issue update only).
		return pgtype.UUID{}, true
	}
	userID, ok := parseUUIDOrBadRequest(w, trimmed, "on_behalf_of_user_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, errOnBehalfOfNotAMember)
		return pgtype.UUID{}, false
	}
	return userID, true
}

// stampIssueOnBehalfOf writes the explicit stamp and returns the refreshed row.
// A failure here is returned to the caller rather than swallowed: an issue that
// silently keeps the wrong human is exactly the bug this feature removes.
func (h *Handler) stampIssueOnBehalfOf(ctx context.Context, issue db.Issue, userID pgtype.UUID) (db.Issue, error) {
	return h.Queries.SetIssueOnBehalfOf(ctx, db.SetIssueOnBehalfOfParams{
		OnBehalfOfUserID: userID,
		ID:               issue.ID,
		WorkspaceID:      issue.WorkspaceID,
	})
}

// onBehalfOfEventValue is what the issue:created / issue:updated payload
// carries so the subscriber listener can auto-subscribe the stamped human
// instead of the derived one. Empty string means "no explicit stamp".
func onBehalfOfEventValue(issue db.Issue) string {
	if !issue.OnBehalfOfUserID.Valid {
		return ""
	}
	return uuidToString(issue.OnBehalfOfUserID)
}
