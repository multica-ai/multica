package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const defaultIssueLabelColor = "#6b7280"

var errInvalidLabelName = errors.New("invalid label name")

type CreateLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type AddIssueLabelRequest struct {
	LabelID *string `json:"label_id"`
	Name    *string `json:"name"`
	Color   *string `json:"color"`
}

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	labels, err := h.Queries.ListLabelsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}

	resp := make([]IssueLabelResponse, len(labels))
	for index, label := range labels {
		resp[index] = issueLabelToResponse(label)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"labels": resp,
		"total":  len(resp),
	})
}

func normalizeLabelColor(value string) string {
	color := strings.TrimSpace(value)
	if color == "" {
		return defaultIssueLabelColor
	}
	return color
}

func (h *Handler) findOrCreateLabel(ctx context.Context, workspaceID string, name string, color string) (db.IssueLabel, int, error) {
	labelName := strings.TrimSpace(name)
	if labelName == "" {
		return db.IssueLabel{}, 0, errInvalidLabelName
	}

	existingLabels, err := h.Queries.GetLabelByNameInWorkspace(ctx, db.GetLabelByNameInWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		Lower:       labelName,
	})
	if err != nil {
		return db.IssueLabel{}, 0, err
	}
	if len(existingLabels) > 0 {
		return existingLabels[0], http.StatusOK, nil
	}

	label, err := h.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        labelName,
		Color:       normalizeLabelColor(color),
	})
	if err != nil {
		return db.IssueLabel{}, 0, err
	}

	return label, http.StatusCreated, nil
}

type UpdateLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *Handler) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	labelID := chi.URLParam(r, "id")

	var req UpdateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "label name is required")
		return
	}

	label, err := h.Queries.UpdateLabel(r.Context(), db.UpdateLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
		Name:        name,
		Color:       normalizeLabelColor(req.Color),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update label")
		return
	}

	writeJSON(w, http.StatusOK, issueLabelToResponse(label))
}

func (h *Handler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	labelID := chi.URLParam(r, "id")

	if err := h.Queries.DeleteLabel(r.Context(), db.DeleteLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete label")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	var req CreateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	label, status, err := h.findOrCreateLabel(r.Context(), workspaceID, req.Name, req.Color)
	if err != nil {
		if err == errInvalidLabelName {
			writeError(w, http.StatusBadRequest, "label name is required")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create label")
		return
	}

	writeJSON(w, status, issueLabelToResponse(label))
}

func (h *Handler) AddIssueLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	var req AddIssueLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var (
		label db.IssueLabel
		err   error
	)
	if req.LabelID != nil && strings.TrimSpace(*req.LabelID) != "" {
		label, err = h.Queries.GetLabelInWorkspace(r.Context(), db.GetLabelInWorkspaceParams{
			ID:          parseUUID(*req.LabelID),
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "label not found")
			return
		}
	} else {
		labelName := ""
		if req.Name != nil {
			labelName = *req.Name
		}
		labelColor := ""
		if req.Color != nil {
			labelColor = *req.Color
		}

		label, _, err = h.findOrCreateLabel(r.Context(), uuidToString(issue.WorkspaceID), labelName, labelColor)
		if err != nil {
			if err == errInvalidLabelName {
				writeError(w, http.StatusBadRequest, "label name is required")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to create label")
			return
		}
	}

	if err := h.Queries.AddIssueLabel(r.Context(), db.AddIssueLabelParams{
		IssueID: issue.ID,
		LabelID: label.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add label")
		return
	}

	updatedIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	resp, err := h.buildIssueDetailResponse(r.Context(), updatedIssue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue": resp,
	})

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RemoveIssueLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	labelID := chi.URLParam(r, "labelId")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	if err := h.Queries.RemoveIssueLabel(r.Context(), db.RemoveIssueLabelParams{
		IssueID: issue.ID,
		LabelID: parseUUID(labelID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove label")
		return
	}

	updatedIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	resp, err := h.buildIssueDetailResponse(r.Context(), updatedIssue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue": resp,
	})

	writeJSON(w, http.StatusOK, resp)
}
