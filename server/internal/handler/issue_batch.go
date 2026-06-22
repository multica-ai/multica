package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
	// Fail any linked autopilot runs before delete (ON DELETE SET NULL clears issue_id).
	h.Queries.FailAutopilotRunsByIssue(r.Context(), issue.ID)

	// Collect all attachment URLs (issue-level + comment-level) before CASCADE delete.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

	err := h.Queries.DeleteIssue(r.Context(), db.DeleteIssueParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	// Always emit the resolved UUID — frontend caches key by UUID, so an
	// identifier-style payload ("MUL-123") would leave stale entries on
	// other clients after an identifier-path delete.
	resolvedID := uuidToString(issue.ID)
	h.publish(protocol.EventIssueDeleted, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": resolvedID})
	slog.Info("issue deleted", append(logger.RequestAttrs(r), "issue_id", resolvedID, "workspace_id", uuidToString(issue.WorkspaceID))...)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Batch operations
// ---------------------------------------------------------------------------

type BatchUpdateIssuesRequest struct {
	IssueIDs []string           `json:"issue_ids"`
	Updates  UpdateIssueRequest `json:"updates"`
}

func (h *Handler) BatchUpdateIssues(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req BatchUpdateIssuesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Detect which fields in "updates" were explicitly set (including null).
	var rawTop map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawTop)
	var rawUpdates map[string]json.RawMessage
	if raw, exists := rawTop["updates"]; exists {
		json.Unmarshal(raw, &rawUpdates)
	}

	// Short-circuit when no mutation field is present in `updates`. Without
	// this, the loop below runs N no-op UPDATEs (every if-guard skips, every
	// COALESCE preserves the existing value) and reports `{"updated": N}` —
	// the response cheerfully claims success while nothing changed. Most
	// real-world cases that hit this path are caller mistakes (status placed
	// at the top level, "update" misspelled as singular). Telling the truth
	// here — `{"updated": 0}` — keeps the wire shape stable while making the
	// count match reality. See multica-ai/multica#1660.
	hasMutation := req.Updates.Title != nil ||
		req.Updates.Description != nil ||
		req.Updates.Status != nil ||
		req.Updates.Priority != nil ||
		req.Updates.Position != nil
	if !hasMutation {
		for _, k := range []string{"assignee_type", "assignee_id", "start_date", "due_date", "parent_issue_id", "project_id"} {
			if _, ok := rawUpdates[k]; ok {
				hasMutation = true
				break
			}
		}
	}
	if !hasMutation {
		writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
		return
	}
	if req.Updates.Status != nil {
		if !validateIssueEnum(w, "status", *req.Updates.Status, validIssueStatuses) {
			return
		}
	}
	if req.Updates.Priority != nil {
		if !validateIssueEnum(w, "priority", *req.Updates.Priority, validIssuePriorities) {
			return
		}
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	updated := 0
	for _, issueID := range req.IssueIDs {
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			continue
		}
		prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			continue
		}

		params := db.UpdateIssueParams{
			ID:            prevIssue.ID,
			AssigneeType:  prevIssue.AssigneeType,
			AssigneeID:    prevIssue.AssigneeID,
			StartDate:     prevIssue.StartDate,
			DueDate:       prevIssue.DueDate,
			ParentIssueID: prevIssue.ParentIssueID,
			ProjectID:     prevIssue.ProjectID,
		}

		if req.Updates.Title != nil {
			params.Title = pgtype.Text{String: *req.Updates.Title, Valid: true}
		}
		if req.Updates.Description != nil {
			params.Description = pgtype.Text{String: *req.Updates.Description, Valid: true}
		}
		if req.Updates.Status != nil {
			params.Status = pgtype.Text{String: *req.Updates.Status, Valid: true}
		}
		if req.Updates.Priority != nil {
			params.Priority = pgtype.Text{String: *req.Updates.Priority, Valid: true}
		}
		if req.Updates.Position != nil {
			params.Position = pgtype.Float8{Float64: *req.Updates.Position, Valid: true}
		}
		if _, ok := rawUpdates["assignee_type"]; ok {
			if req.Updates.AssigneeType != nil {
				params.AssigneeType = pgtype.Text{String: *req.Updates.AssigneeType, Valid: true}
			} else {
				params.AssigneeType = pgtype.Text{Valid: false}
			}
		}
		if _, ok := rawUpdates["assignee_id"]; ok {
			if req.Updates.AssigneeID != nil {
				assigneeUUID, err := util.ParseUUID(*req.Updates.AssigneeID)
				if err != nil {
					continue
				}
				params.AssigneeID = assigneeUUID
			} else {
				params.AssigneeID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["start_date"]; ok {
			if req.Updates.StartDate != nil && *req.Updates.StartDate != "" {
				d, err := util.ParseCalendarDate(*req.Updates.StartDate)
				if err != nil {
					continue
				}
				params.StartDate = d
			} else {
				params.StartDate = pgtype.Date{Valid: false}
			}
		}
		if _, ok := rawUpdates["due_date"]; ok {
			if req.Updates.DueDate != nil && *req.Updates.DueDate != "" {
				d, err := util.ParseCalendarDate(*req.Updates.DueDate)
				if err != nil {
					continue
				}
				params.DueDate = d
			} else {
				params.DueDate = pgtype.Date{Valid: false}
			}
		}

		if _, ok := rawUpdates["parent_issue_id"]; ok {
			if req.Updates.ParentIssueID != nil {
				newParentID, err := util.ParseUUID(*req.Updates.ParentIssueID)
				if err != nil {
					continue
				}
				params.ParentIssueID = newParentID
			} else {
				params.ParentIssueID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["project_id"]; ok {
			if req.Updates.ProjectID != nil {
				projectUUID, err := util.ParseUUID(*req.Updates.ProjectID)
				if err != nil {
					continue
				}
				params.ProjectID = projectUUID
			} else {
				params.ProjectID = pgtype.UUID{Valid: false}
			}
		}

		// Validate the resulting assignee pair when this batch update touches
		// either assignee field. Skip the issue silently on failure.
		_, batchTouchedType := rawUpdates["assignee_type"]
		_, batchTouchedID := rawUpdates["assignee_id"]
		if batchTouchedType || batchTouchedID {
			if status, _ := h.validateAssigneePair(r.Context(), r, workspaceID, params.AssigneeType, params.AssigneeID); status != 0 {
				continue
			}
		}

		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		taskID := pgtype.UUID{}
		if taskIDStr := r.Header.Get("X-Task-ID"); taskIDStr != "" {
			if parsed, err := util.ParseUUID(taskIDStr); err == nil {
				taskID = parsed
			}
		}
		opts := service.IssueUpdateOpts{
			ActorType: actorType,
			ActorID:   actorID,
			TaskID:    taskID,
		}

		result, err := h.IssueService.Update(r.Context(), prevIssue, params, nil, opts)
		if err != nil {
			slog.Warn("batch update issue failed", "issue_id", issueID, "error", err)
			continue
		}

		issue := result.Issue
		ch := result.Changes

		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)

		h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
			"issue":            resp,
			"assignee_changed": ch.AssigneeChanged,
			"status_changed":   ch.StatusChanged,
			"priority_changed": ch.PriorityChanged,
		})

		h.IssueService.ReconcileAfterUpdate(r.Context(), result, opts)

		if ch.StatusChanged {
			h.notifyParentOfChildDone(r.Context(), prevIssue, issue, actorType, actorID)
		}

		updated++
	}

	slog.Info("batch update issues", append(logger.RequestAttrs(r), "count", updated)...)
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

type BatchDeleteIssuesRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

func (h *Handler) BatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	var req BatchDeleteIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	deleted := 0
	for _, issueID := range req.IssueIDs {
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			continue
		}
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			continue
		}

		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
		h.Queries.FailAutopilotRunsByIssue(r.Context(), issue.ID)

		// Collect attachment URLs before CASCADE delete to clean up S3 objects.
		attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

		if err := h.Queries.DeleteIssue(r.Context(), db.DeleteIssueParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
		}); err != nil {
			slog.Warn("batch delete issue failed", "issue_id", issueID, "error", err)
			continue
		}

		h.deleteS3Objects(r.Context(), attachmentURLs)

		// Always emit the resolved UUID — frontend caches key by UUID.
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID)})
		deleted++
	}

	slog.Info("batch delete issues", append(logger.RequestAttrs(r), "count", deleted)...)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
