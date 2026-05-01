package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type WorklogResponse struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	WorkspaceID     string  `json:"workspace_id"`
	AuthorType      string  `json:"author_type"`
	AuthorID        string  `json:"author_id"`
	DurationMinutes int32   `json:"duration_minutes"`
	Description     *string `json:"description"`
	Type            string  `json:"type"`
	LoggedAt        string  `json:"logged_at"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type CreateWorklogRequest struct {
	DurationMinutes int32   `json:"duration_minutes"`
	Description     *string `json:"description"`
}

type UpdateWorklogRequest struct {
	DurationMinutes *int32  `json:"duration_minutes"`
	Description     *string `json:"description"`
}

func buildWorklogResponse(id, issueID, workspaceID, authorID string, authorType string, durationMinutes int32, description *string, worklogType string, loggedAt, createdAt, updatedAt string) WorklogResponse {
	return WorklogResponse{
		ID:              id,
		IssueID:         issueID,
		WorkspaceID:     workspaceID,
		AuthorType:      authorType,
		AuthorID:        authorID,
		DurationMinutes: durationMinutes,
		Description:     description,
		Type:            worklogType,
		LoggedAt:        loggedAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}

func worklogToResponse(worklog db.Worklog, issueID string) WorklogResponse {
	return buildWorklogResponse(
		uuidToString(worklog.ID),
		issueID,
		uuidToString(worklog.WorkspaceID),
		uuidToString(worklog.AuthorID),
		worklog.AuthorType,
		worklog.DurationMinutes,
		textToPtr(worklog.Description),
		worklog.Type,
		timestampToString(worklog.LoggedAt),
		timestampToString(worklog.CreatedAt),
		timestampToString(worklog.UpdatedAt),
	)
}

func worklogRowToResponse(worklog db.ListWorklogsByIssueRow) WorklogResponse {
	return buildWorklogResponse(
		uuidToString(worklog.ID),
		uuidToString(worklog.IssueID),
		uuidToString(worklog.WorkspaceID),
		uuidToString(worklog.AuthorID),
		worklog.AuthorType,
		worklog.DurationMinutes,
		textToPtr(worklog.Description),
		worklog.Type,
		timestampToString(worklog.LoggedAt),
		timestampToString(worklog.CreatedAt),
		timestampToString(worklog.UpdatedAt),
	)
}

func worklogDetailToResponse(worklog db.GetWorklogByIDRow) WorklogResponse {
	return buildWorklogResponse(
		uuidToString(worklog.ID),
		uuidToString(worklog.IssueID),
		uuidToString(worklog.WorkspaceID),
		uuidToString(worklog.AuthorID),
		worklog.AuthorType,
		worklog.DurationMinutes,
		textToPtr(worklog.Description),
		worklog.Type,
		timestampToString(worklog.LoggedAt),
		timestampToString(worklog.CreatedAt),
		timestampToString(worklog.UpdatedAt),
	)
}

func (h *Handler) ListWorklogs(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	worklogs, err := h.Queries.ListWorklogsByIssue(r.Context(), db.ListWorklogsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list worklogs")
		return
	}

	response := make([]WorklogResponse, len(worklogs))
	for index, worklog := range worklogs {
		response[index] = worklogRowToResponse(worklog)
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) CreateWorklog(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var request CreateWorklogRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.DurationMinutes <= 0 {
		writeError(w, http.StatusBadRequest, "duration_minutes must be greater than 0")
		return
	}

	authorType, authorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create worklog")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	worklog, err := qtx.CreateWorklog(r.Context(), db.CreateWorklogParams{
		WorkspaceID:     issue.WorkspaceID,
		AuthorType:      authorType,
		AuthorID:        parseUUID(authorID),
		DurationMinutes: request.DurationMinutes,
		Description:     ptrToText(request.Description),
	})
	if err != nil {
		slog.Warn("create worklog failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to create worklog")
		return
	}

	_, err = qtx.CreateWorklogIssue(r.Context(), db.CreateWorklogIssueParams{
		WorklogID:   worklog.ID,
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("link worklog to issue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "worklog_id", uuidToString(worklog.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to create worklog")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create worklog")
		return
	}

	response := worklogToResponse(worklog, uuidToString(issue.ID))
	h.publish(protocol.EventWorklogCreated, uuidToString(issue.WorkspaceID), authorType, authorID, map[string]any{"worklog": response})
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) UpdateWorklog(w http.ResponseWriter, r *http.Request) {
	worklogID := chi.URLParam(r, "id")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	existing, err := h.Queries.GetWorklogByID(r.Context(), db.GetWorklogByIDParams{
		ID:          parseUUID(worklogID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "worklog not found")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := existing.AuthorType == authorType && uuidToString(existing.AuthorID) == authorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only worklog author or admin can edit")
		return
	}

	var request UpdateWorklogRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.DurationMinutes == nil && request.Description == nil {
		writeError(w, http.StatusBadRequest, "at least one field must be provided")
		return
	}

	durationMinutes := existing.DurationMinutes
	if request.DurationMinutes != nil {
		if *request.DurationMinutes <= 0 {
			writeError(w, http.StatusBadRequest, "duration_minutes must be greater than 0")
			return
		}
		durationMinutes = *request.DurationMinutes
	}

	description := existing.Description
	if request.Description != nil {
		description = strToText(*request.Description)
	}

	worklog, err := h.Queries.UpdateWorklog(r.Context(), db.UpdateWorklogParams{
		ID:              parseUUID(worklogID),
		DurationMinutes: durationMinutes,
		Description:     description,
		WorkspaceID:     parseUUID(workspaceID),
	})
	if err != nil {
		slog.Warn("update worklog failed", append(logger.RequestAttrs(r), "error", err, "worklog_id", worklogID)...)
		writeError(w, http.StatusInternalServerError, "failed to update worklog")
		return
	}

	response := worklogToResponse(worklog, uuidToString(existing.IssueID))
	h.publish(protocol.EventWorklogUpdated, workspaceID, authorType, authorID, map[string]any{"worklog": response})
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) DeleteWorklog(w http.ResponseWriter, r *http.Request) {
	worklogID := chi.URLParam(r, "id")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	existing, err := h.Queries.GetWorklogByID(r.Context(), db.GetWorklogByIDParams{
		ID:          parseUUID(worklogID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "worklog not found")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := existing.AuthorType == authorType && uuidToString(existing.AuthorID) == authorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only worklog author or admin can delete")
		return
	}

	if err := h.Queries.DeleteWorklog(r.Context(), db.DeleteWorklogParams{
		ID:          parseUUID(worklogID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		slog.Warn("delete worklog failed", append(logger.RequestAttrs(r), "error", err, "worklog_id", worklogID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete worklog")
		return
	}

	h.publish(protocol.EventWorklogDeleted, workspaceID, authorType, authorID, map[string]any{
		"worklog_id": worklogID,
		"issue_id":   uuidToString(existing.IssueID),
	})
	w.WriteHeader(http.StatusNoContent)
}