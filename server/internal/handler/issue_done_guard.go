package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// inboxTypeAgentDoneBlocked is the inbox notification type raised when the
// human-only-done policy (Config.RequireHumanDone) rejects a machine actor's
// attempt to move an issue into `done`. The matching frontend union member
// lives in packages/core/types/inbox.ts — keep the two in sync.
const inboxTypeAgentDoneBlocked = "agent_done_blocked"

// enforceHumanDone applies the optional "only humans may mark issues done"
// policy (Config.RequireHumanDone). When the policy is on and a machine actor
// (mat_ task token or mcn_ cloud-node PAT) attempts to transition an issue
// INTO `done`, it notifies the owning human via the inbox, writes a 403, and
// returns true. Callers MUST `return` when it returns true — the response has
// already been written.
//
// Only the transition into `done` is gated. Re-saving an already-done issue,
// or setting any other status, is always allowed so an agent can still update
// unrelated fields (description, assignee, in_review, …) without tripping the
// gate.
//
// prevStatus is the issue's current status; pass "" for issue creation (no
// prior state, so a `done` target counts as a transition). issueID may be the
// zero UUID for creation / batch, where there is no single row to link the
// notification to.
func (h *Handler) enforceHumanDone(w http.ResponseWriter, r *http.Request, ws, issueID pgtype.UUID, issueTitle, prevStatus, newStatus string) bool {
	if !h.cfg.RequireHumanDone {
		return false
	}
	if newStatus != "done" || prevStatus == "done" {
		return false
	}
	if !isMachineActor(r) {
		return false
	}
	h.notifyAgentDoneBlocked(r.Context(), r, ws, issueID, issueTitle)
	writeError(w, http.StatusForbidden, "issues can only be marked done by a human")
	return true
}

// notifyAgentDoneBlocked drops an inbox row for the human who owns the agent
// whose `done` transition was just denied. The recipient is the request's
// X-User-ID, which for a mat_/mcn_ credential is the OWNING human (see
// middleware/auth.go) — exactly the person who should review the work and
// close the issue themselves.
//
// Best-effort: the 403 is the authoritative outcome, so a failed inbox write
// is logged and swallowed rather than surfaced.
func (h *Handler) notifyAgentDoneBlocked(ctx context.Context, r *http.Request, ws, issueID pgtype.UUID, issueTitle string) {
	ownerID := requestUserID(r)
	recipientID, err := util.ParseUUID(ownerID)
	if err != nil {
		return
	}

	actorType, actorID := h.resolveActor(r, ownerID, uuidToString(ws))
	var actorUUID pgtype.UUID
	if id, err := util.ParseUUID(actorID); err == nil {
		actorUUID = id
	}

	details, _ := json.Marshal(map[string]string{"attempted_status": "done"})

	if _, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   ws,
		RecipientType: "member",
		RecipientID:   recipientID,
		Type:          inboxTypeAgentDoneBlocked,
		Severity:      "attention",
		IssueID:       issueID,
		Title:         issueTitle,
		Body:          util.StrToText("An agent tried to mark this issue done. Only a human can move an issue to done — review the work and close it yourself."),
		ActorType:     util.StrToText(actorType),
		ActorID:       actorUUID,
		Details:       details,
	}); err != nil {
		slog.Warn("agent done blocked: inbox write failed",
			"error", err,
			"workspace_id", uuidToString(ws),
			"issue_id", uuidToString(issueID))
	}
}
