package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Write protection for a Triage entry (MUL-7189 §2.1, §2.2).
//
// Triage is not a status — it lives in issue.triage_state — so status, project,
// assignee, priority and labels on a Triage entry are the triager's PROPOSAL,
// and accept is what confirms them. Ordinary writes reach all of those.
//
// The parent is not a proposal, and is the one field held back. Two reasons,
// and the second is behavior rather than display:
//
//   - the routing panel has no parent field. "Merge into an existing issue" is
//     a separate action that links rather than re-parents, so a Triage entry
//     hanging off a parent is a half-state the product never asks for;
//   - a child in Triage carries a proposed status, so it is never terminal.
//     stageBarrierClosed would wait on it forever — holding the whole sibling
//     set, or one stage's frontier — and ChildIssueProgress would count it in
//     the denominator. Both read through parent_issue_id, so refusing the write
//     is what keeps them out of reach.
//
// Carrying the field counts as writing it, even with the value already stored
// and even when null (which clears). The rule stays one a client can predict.

// triageLockedField returns the Triage-locked field a write carries, or "" when
// it carries none.
func triageLockedField(raw map[string]json.RawMessage) string {
	if _, ok := raw["parent_issue_id"]; ok {
		return "parent_issue_id"
	}
	return ""
}

func writeIssueInTriage(w http.ResponseWriter, field string) {
	writeErrorCode(w, http.StatusBadRequest, "issue_in_triage",
		"the issue is in Triage, so its "+field+" cannot be set; accept it out of Triage first")
}

// validateBatchTriageLocks rejects a batch that would set a Triage-locked field
// on any issue in Triage, before anything is written. A silent per-issue skip
// would report a short `{"updated": N}` with no reason attached.
func (h *Handler) validateBatchTriageLocks(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, issueIDs []string, rawUpdates map[string]json.RawMessage) bool {
	field := triageLockedField(rawUpdates)
	if field == "" {
		return true
	}
	ids := make([]pgtype.UUID, 0, len(issueIDs))
	for _, id := range issueIDs {
		// Unparseable ids are skipped by the batch loop as well.
		if parsed, err := util.ParseUUID(id); err == nil {
			ids = append(ids, parsed)
		}
	}
	if len(ids) == 0 {
		return true
	}
	inTriage, err := h.Queries.CountIssuesInTriage(r.Context(), db.CountIssuesInTriageParams{
		WorkspaceID: workspaceID,
		IssueIds:    ids,
	})
	if err != nil {
		slog.Warn("batch update issues: count triage entries", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to validate issues")
		return false
	}
	if inTriage > 0 {
		writeIssueInTriage(w, field)
		return false
	}
	return true
}
