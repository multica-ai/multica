package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Write protection for the reserved Triage status (MUL-7189 §2.2).
//
// An issue enters Triage only through Triage intake and leaves it only by being
// accepted, so ordinary issue writes are held to two rules:
//
//   - no write may name `triage` as its target status. issuestatus.Resolve
//     refuses the key, and resolveIssueStatusKeyKind renders that refusal, so
//     every create, update and batch path is covered by the one resolver;
//   - an issue in Triage keeps its status, project and parent. Those are what
//     accept decides, so writing them here would bypass it. Assignee, priority,
//     labels and content stay editable.
//
// A field counts as written when the request carries it, even with the value
// the issue already has. The rule stays one a client can predict — "these
// fields are read-only in Triage" — instead of depending on the current value,
// and a client showing an issue in Triage has no reason to send them.

// triageLockedField returns the first Triage-locked field a write carries, or
// "" when it carries none. statusSet reports whether the request sets a status
// (a null status leaves it unchanged); project_id and parent_issue_id count
// when present at all, because null clears them.
func triageLockedField(statusSet bool, raw map[string]json.RawMessage) string {
	if statusSet {
		return "status"
	}
	for _, field := range []string{"project_id", "parent_issue_id"} {
		if _, ok := raw[field]; ok {
			return field
		}
	}
	return ""
}

func writeStatusReservedForTriage(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusBadRequest, "status_reserved_for_triage",
		`status "triage" is reserved: an issue enters Triage only through Triage intake and leaves it only by being accepted`)
}

func writeIssueInTriage(w http.ResponseWriter, field string) {
	writeErrorCode(w, http.StatusBadRequest, "issue_in_triage",
		"the issue is in Triage, so its "+field+" cannot be changed; accept it out of Triage first")
}

// validateBatchTriageLocks rejects a batch that would write a Triage-locked
// field on any issue in Triage, before anything is written. A silent per-issue
// skip would report a short `{"updated": N}` with no reason attached, which is
// the same failure the batch status check rejects up front for.
func (h *Handler) validateBatchTriageLocks(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, issueIDs []string, statusSet bool, rawUpdates map[string]json.RawMessage) bool {
	field := triageLockedField(statusSet, rawUpdates)
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
	rows, err := h.Queries.ListIssueGCStatuses(r.Context(), db.ListIssueGCStatusesParams{
		WorkspaceID: workspaceID,
		IssueIds:    ids,
	})
	if err != nil {
		slog.Warn("batch update issues: load statuses for triage check", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to validate issues")
		return false
	}
	for _, row := range rows {
		if row.Status == issuestatus.Triage {
			writeIssueInTriage(w, field)
			return false
		}
	}
	return true
}
