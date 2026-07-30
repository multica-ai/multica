package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// descriptionVersionCoalesceWindow is how long a member's editing session stays
// open. The description editor autosaves on a 1.5s debounce, so one sitting
// produces tens of writes; without coalescing the history panel and the
// timeline would both be unreadable and activity_log would grow a row per
// typing pause. Ten minutes is long enough to survive reading/thinking pauses
// inside one edit and short enough that coming back after lunch reads as a new
// version.
//
// Agent writes ignore this window entirely — see descriptionVersionCoalesces.
const descriptionVersionCoalesceWindow = 10 * time.Minute

// descriptionVersionListCap bounds the history panel payload. Session
// coalescing already keeps the real count small; this is a safety net.
const descriptionVersionListCap = 200

// DescriptionVersionResponse is one entry in the description history.
//
// Content is omitted from list responses (`omitempty` plus a nil field) so the
// panel can render the version list without shipping every snapshot; the diff
// view fetches the two sides it needs by id.
type DescriptionVersionResponse struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	ParentVersionID *string `json:"parent_version_id"`
	ActorType       *string `json:"actor_type"`
	ActorID         *string `json:"actor_id"`
	SourceTaskID    *string `json:"source_task_id,omitempty"`
	Content         *string `json:"content,omitempty"`
	AddedLines      int32   `json:"added_lines"`
	RemovedLines    int32   `json:"removed_lines"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	// IsOriginal marks the seeded V0 — the description as it stood before the
	// first tracked edit. The UI labels it differently because it has no
	// "changed by" story: nobody wrote it in a session we observed.
	IsOriginal bool `json:"is_original"`
}

func descriptionVersionToResponse(v db.IssueDescriptionVersion, includeContent bool) DescriptionVersionResponse {
	resp := DescriptionVersionResponse{
		ID:              uuidToString(v.ID),
		IssueID:         uuidToString(v.IssueID),
		ParentVersionID: uuidToPtr(v.ParentVersionID),
		ActorType:       textToPtr(v.ActorType),
		ActorID:         uuidToPtr(v.ActorID),
		SourceTaskID:    uuidToPtr(v.SourceTaskID),
		AddedLines:      v.AddedLines,
		RemovedLines:    v.RemovedLines,
		CreatedAt:       timestampToString(v.CreatedAt),
		UpdatedAt:       timestampToString(v.UpdatedAt),
		IsOriginal:      !v.ParentVersionID.Valid,
	}
	if includeContent {
		content := v.Content
		resp.Content = &content
	}
	return resp
}

// descriptionVersionWrite is what UpdateIssue hands to the event payload so the
// activity listener can stamp the timeline entry without re-reading the row.
type descriptionVersionWrite struct {
	VersionID       string
	ParentVersionID string
	AddedLines      int
	RemovedLines    int
}

// recordDescriptionVersion persists a description change as a version row and
// returns what the timeline needs to render its diff badge.
//
// This runs inline in the UpdateIssue request rather than in an event listener
// on purpose: listeners are fire-and-forget, and a silently dropped version
// would make the whole history untrustworthy. It is still best-effort in the
// sense that a failure here does not fail the user's edit — the description
// write already committed — but it is logged at error level, not debug.
//
// forceNewVersion makes the write start a fresh version instead of extending
// the open session. Restore passes it: a restore is a deliberate act, not a
// continuation of whatever the actor was typing, and coalescing it would
// overwrite the very version being restored away from — silently destroying the
// content the feature exists to protect.
func (h *Handler) recordDescriptionVersion(
	r *http.Request,
	issue db.Issue,
	prevDescription, newDescription string,
	actorType, actorID string,
	forceNewVersion bool,
) (descriptionVersionWrite, bool) {
	ctx := r.Context()

	// The task the writing agent is currently running on this issue, if any.
	// Agent sessions are bounded by their run rather than by wall-clock time.
	sourceTaskID := h.commentSourceTaskIDForIssue(r, issue)

	latest, err := h.Queries.GetLatestIssueDescriptionVersion(ctx, issue.ID)
	if err != nil {
		// No history yet. Seed V0 from the pre-edit content so the very first
		// rewrite — typically an agent replacing the human's original request —
		// has something to diff against. Without this the most valuable
		// comparison in the whole feature is the one that does not exist.
		seeded, seedErr := h.seedOriginalDescriptionVersion(ctx, issue, prevDescription)
		if seedErr != nil {
			slog.Error("description version: failed to seed original",
				"issue_id", uuidToString(issue.ID), "error", seedErr)
			return descriptionVersionWrite{}, false
		}
		latest = seeded
	}

	added, removed := util.CountLineDiff(latest.Content, newDescription)

	if !forceNewVersion && descriptionVersionCoalesces(latest, actorType, actorID, sourceTaskID) {
		// Rewrite the open session row. Counts are recomputed against the
		// session's PARENT, never against the row being replaced, so a session
		// that adds a line and then deletes it settles back to zero rather than
		// accumulating churn.
		base := ""
		if latest.ParentVersionID.Valid {
			parent, parentErr := h.Queries.GetIssueDescriptionVersion(ctx, db.GetIssueDescriptionVersionParams{
				ID:          latest.ParentVersionID,
				WorkspaceID: issue.WorkspaceID,
			})
			if parentErr != nil {
				slog.Error("description version: failed to load parent for recount",
					"version_id", uuidToString(latest.ID), "error", parentErr)
				return descriptionVersionWrite{}, false
			}
			base = parent.Content
		}
		sessionAdded, sessionRemoved := util.CountLineDiff(base, newDescription)
		updated, updateErr := h.Queries.UpdateIssueDescriptionVersionContent(ctx, db.UpdateIssueDescriptionVersionContentParams{
			ID:           latest.ID,
			Content:      newDescription,
			AddedLines:   int32(sessionAdded),
			RemovedLines: int32(sessionRemoved),
		})
		if updateErr != nil {
			slog.Error("description version: failed to extend session",
				"version_id", uuidToString(latest.ID), "error", updateErr)
			return descriptionVersionWrite{}, false
		}
		return descriptionVersionWrite{
			VersionID:       uuidToString(updated.ID),
			ParentVersionID: uuidToString(updated.ParentVersionID),
			AddedLines:      sessionAdded,
			RemovedLines:    sessionRemoved,
		}, true
	}

	created, err := h.Queries.CreateIssueDescriptionVersion(ctx, db.CreateIssueDescriptionVersionParams{
		WorkspaceID:     issue.WorkspaceID,
		IssueID:         issue.ID,
		ParentVersionID: latest.ID,
		ActorType:       util.StrToText(actorType),
		ActorID:         optionalActorUUID(actorID),
		SourceTaskID:    sourceTaskID,
		Content:         newDescription,
		AddedLines:      int32(added),
		RemovedLines:    int32(removed),
	})
	if err != nil {
		slog.Error("description version: failed to create",
			"issue_id", uuidToString(issue.ID), "error", err)
		return descriptionVersionWrite{}, false
	}
	return descriptionVersionWrite{
		VersionID:       uuidToString(created.ID),
		ParentVersionID: uuidToString(created.ParentVersionID),
		AddedLines:      added,
		RemovedLines:    removed,
	}, true
}

// seedOriginalDescriptionVersion writes V0: the description as it stood before
// the first tracked edit, attributed to whoever created the issue and stamped
// with the issue's creation time so the history reads chronologically.
//
// Existing issues therefore gain history from their next edit forward. There is
// no content to backfill — the old values were overwritten in place long before
// this table existed.
func (h *Handler) seedOriginalDescriptionVersion(
	ctx context.Context,
	issue db.Issue,
	originalDescription string,
) (db.IssueDescriptionVersion, error) {
	return h.Queries.CreateIssueDescriptionVersion(ctx, db.CreateIssueDescriptionVersionParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		// No parent — this is the root of the chain and what IsOriginal keys on.
		ParentVersionID: pgtype.UUID{},
		ActorType:       util.StrToText(issue.CreatorType),
		ActorID:         issue.CreatorID,
		Content:         originalDescription,
		AddedLines:      0,
		RemovedLines:    0,
		CreatedAt:       issue.CreatedAt,
	})
}

// descriptionVersionCoalesces decides whether a write extends the open session
// or starts a new version.
//
// Two different notions of "session" by actor kind:
//   - agent: the run. One `multica issue update` per task is the natural
//     boundary and it is exact, so no time window applies — an agent that
//     rewrites the description three times in one run still produces one
//     version. A write with no resolvable task falls through to the time rule.
//   - member: a wall-clock window, because the editor's autosave gives us no
//     other signal about where one sitting ends.
//
// V0 is never extended: it represents the pre-history original and must stay
// pinned as the comparison baseline.
func descriptionVersionCoalesces(
	latest db.IssueDescriptionVersion,
	actorType, actorID string,
	sourceTaskID pgtype.UUID,
) bool {
	if !latest.ParentVersionID.Valid {
		return false
	}
	if latest.ActorType.String != actorType || uuidToString(latest.ActorID) != actorID {
		return false
	}
	if sourceTaskID.Valid && latest.SourceTaskID.Valid {
		return uuidToString(latest.SourceTaskID) == uuidToString(sourceTaskID)
	}
	if sourceTaskID.Valid != latest.SourceTaskID.Valid {
		// One side is a run and the other is not: different sessions even when
		// the actor matches.
		return false
	}
	if !latest.UpdatedAt.Valid {
		return false
	}
	return time.Since(latest.UpdatedAt.Time) <= descriptionVersionCoalesceWindow
}

// optionalActorUUID converts an actor id that may legitimately be empty (a
// system actor) into a nullable UUID without panicking on the empty string.
func optionalActorUUID(actorID string) pgtype.UUID {
	if actorID == "" {
		return pgtype.UUID{}
	}
	parsed, err := util.ParseUUID(actorID)
	if err != nil {
		return pgtype.UUID{}
	}
	return parsed
}

// ListIssueDescriptionVersions returns the description history, newest first.
// Snapshots are omitted; GetIssueDescriptionVersion serves content by id.
func (h *Handler) ListIssueDescriptionVersions(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	versions, err := h.Queries.ListIssueDescriptionVersions(r.Context(), db.ListIssueDescriptionVersionsParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       descriptionVersionListCap,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list description versions")
		return
	}
	resp := make([]DescriptionVersionResponse, 0, len(versions))
	for _, v := range versions {
		resp = append(resp, descriptionVersionToResponse(v, false))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetIssueDescriptionVersion returns a single version including its snapshot.
func (h *Handler) GetIssueDescriptionVersion(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	versionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "versionId"), "versionId")
	if !ok {
		return
	}
	version, err := h.Queries.GetIssueDescriptionVersion(r.Context(), db.GetIssueDescriptionVersionParams{
		ID:          versionID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || uuidToString(version.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "description version not found")
		return
	}
	writeJSON(w, http.StatusOK, descriptionVersionToResponse(version, true))
}

// RestoreIssueDescriptionVersion writes a past snapshot back onto the issue.
//
// Restore is an ordinary edit, not a rewind: it goes through the same update
// path, so it produces a NEW version attributed to whoever restored, and no
// history is ever deleted. The activity it emits is tagged with
// restored_from_version_id so the timeline can say "restored to <date>" instead
// of the generic "updated the description".
//
// Note this cannot start an agent run: run dispatch keys on assignee/status
// changes only, and description @mentions notify members without enqueuing.
func (h *Handler) RestoreIssueDescriptionVersion(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	versionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "versionId"), "versionId")
	if !ok {
		return
	}
	ctx := r.Context()
	workspaceID := uuidToString(issue.WorkspaceID)

	version, err := h.Queries.GetIssueDescriptionVersion(ctx, db.GetIssueDescriptionVersionParams{
		ID:          versionID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || uuidToString(version.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "description version not found")
		return
	}

	prevDescription := issue.Description.String
	if prevDescription == version.Content {
		// Already the live text — nothing to restore. Return the issue as-is
		// rather than minting an empty version.
		writeJSON(w, http.StatusOK, issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID)))
		return
	}

	updated, err := h.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:            issue.ID,
		Description:   pgtype.Text{String: version.Content, Valid: true},
		AssigneeType:  issue.AssigneeType,
		AssigneeID:    issue.AssigneeID,
		StartDate:     issue.StartDate,
		DueDate:       issue.DueDate,
		ParentIssueID: issue.ParentIssueID,
		ProjectID:     issue.ProjectID,
		Stage:         issue.Stage,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore description")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	write, recorded := h.recordDescriptionVersion(r, updated, prevDescription, version.Content, actorType, actorID, true)

	prefix := h.getIssuePrefix(ctx, updated.WorkspaceID)
	resp := issueToResponse(updated, prefix)

	payload := map[string]any{
		"issue":               resp,
		"assignee_changed":    false,
		"status_changed":      false,
		"priority_changed":    false,
		"project_changed":     false,
		"start_date_changed":  false,
		"due_date_changed":    false,
		"description_changed": true,
		"title_changed":       false,
		"prev_title":          updated.Title,
		"prev_assignee_type":  textToPtr(issue.AssigneeType),
		"prev_assignee_id":    uuidToPtr(issue.AssigneeID),
		"prev_status":         issue.Status,
		"prev_priority":       issue.Priority,
		"prev_start_date":     dateToPtr(issue.StartDate),
		"prev_due_date":       dateToPtr(issue.DueDate),
		"prev_description":    &prevDescription,
		"creator_type":        issue.CreatorType,
		"creator_id":          uuidToString(issue.CreatorID),
		// Distinguishes this from a normal edit in the timeline copy.
		"restored_from_version_id": uuidToString(version.ID),
	}
	if recorded {
		payload["description_version_id"] = write.VersionID
		payload["description_added_lines"] = write.AddedLines
		payload["description_removed_lines"] = write.RemovedLines
	}
	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, payload)

	slog.Info("issue description restored",
		"issue_id", uuidToString(issue.ID), "version_id", uuidToString(version.ID))
	writeJSON(w, http.StatusOK, resp)
}

// DescriptionVersionDetails builds the activity `details` blob for a
// description change. Carrying the counts on the activity is what lets the
// timeline render "+12 −3" without a second request per entry.
//
// Exported for the activity listener in cmd/server, which is the only writer of
// activity_log rows.
func DescriptionVersionDetails(payload map[string]any) json.RawMessage {
	details := map[string]any{}
	if v, ok := payload["description_version_id"].(string); ok && v != "" {
		details["version_id"] = v
	}
	if v, ok := payload["description_parent_version_id"].(string); ok && v != "" {
		details["parent_version_id"] = v
	}
	if v, ok := payload["description_added_lines"].(int); ok {
		details["added_lines"] = v
	}
	if v, ok := payload["description_removed_lines"].(int); ok {
		details["removed_lines"] = v
	}
	if v, ok := payload["restored_from_version_id"].(string); ok && v != "" {
		details["restored_from_version_id"] = v
	}
	if len(details) == 0 {
		return json.RawMessage("{}")
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}
