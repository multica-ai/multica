package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultTimeEntryLabelColor = "#6b7280"

var errInvalidTimeEntryLabelName = errors.New("invalid time entry label name")

type timeEntryLabelValidationError struct {
	message string
}

func (e *timeEntryLabelValidationError) Error() string {
	return e.message
}

func newTimeEntryLabelValidationError(message string) error {
	return &timeEntryLabelValidationError{message: message}
}

func timeEntryLabelMutationErrorResponse(err error) (int, string) {
	var validationErr *timeEntryLabelValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest, validationErr.message
	}
	return http.StatusInternalServerError, "failed to update time entry labels"
}

// CreateTimeEntryLabelRequest defines payload for creating a workspace time-entry label.
type CreateTimeEntryLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// UpdateTimeEntryLabelRequest defines payload for updating a workspace time-entry label.
type UpdateTimeEntryLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// AddTimeEntryLabelRequest defines payload for attaching a label to one time entry.
type AddTimeEntryLabelRequest struct {
	LabelID *string `json:"label_id"`
	Name    *string `json:"name"`
	Color   *string `json:"color"`
}

// SetTimeEntryLabelsRequest defines payload for replacing all labels on one time entry.
type SetTimeEntryLabelsRequest struct {
	LabelIDs []string `json:"label_ids"`
}

// normalizeTimeEntryLabelColor ensures every label has a color.
func normalizeTimeEntryLabelColor(value string) string {
	color := strings.TrimSpace(value)
	if color == "" {
		return defaultTimeEntryLabelColor
	}
	return color
}

// findOrCreateTimeEntryLabel returns an existing workspace label by name, or creates it.
func (h *Handler) findOrCreateTimeEntryLabel(ctx context.Context, workspaceID string, name string, color string) (db.TimeEntryLabel, int, error) {
	labelName := strings.TrimSpace(name)
	if labelName == "" {
		return db.TimeEntryLabel{}, 0, errInvalidTimeEntryLabelName
	}

	existingLabels, err := h.Queries.GetTimeEntryLabelByNameInWorkspace(ctx, db.GetTimeEntryLabelByNameInWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		Lower:       labelName,
	})
	if err != nil {
		return db.TimeEntryLabel{}, 0, err
	}
	if len(existingLabels) > 0 {
		return existingLabels[0], http.StatusOK, nil
	}

	label, err := h.Queries.CreateTimeEntryLabel(ctx, db.CreateTimeEntryLabelParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        labelName,
		Color:       normalizeTimeEntryLabelColor(color),
	})
	if err != nil {
		return db.TimeEntryLabel{}, 0, err
	}

	return label, http.StatusCreated, nil
}

// loadTimeEntryForUser returns one time entry and enforces that it belongs to the requesting user.
func (h *Handler) loadTimeEntryForUser(ctx context.Context, workspaceID string, userID string, entryID string) (db.TimeEntry, error) {
	entry, err := h.Queries.GetTimeEntryByID(ctx, db.GetTimeEntryByIDParams{
		ID:          parseUUID(entryID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return db.TimeEntry{}, err
	}
	if uuidToString(entry.UserID) != userID {
		return db.TimeEntry{}, pgx.ErrNoRows
	}
	return entry, nil
}

// replaceTimeEntryLabelsWithQueries replaces all labels on one time entry using the provided queries handle.
func (h *Handler) replaceTimeEntryLabelsWithQueries(ctx context.Context, queries *db.Queries, entry db.TimeEntry, labelIDs []string) error {
	if err := queries.ClearTimeEntryLabels(ctx, entry.ID); err != nil {
		return fmt.Errorf("clear time entry labels: %w", err)
	}

	seen := make(map[string]struct{}, len(labelIDs))
	for _, rawID := range labelIDs {
		trimmedID := strings.TrimSpace(rawID)
		if trimmedID == "" {
			continue
		}
		if _, ok := seen[trimmedID]; ok {
			continue
		}
		seen[trimmedID] = struct{}{}

		labelUUID := parseUUID(trimmedID)
		if !labelUUID.Valid {
			return newTimeEntryLabelValidationError(fmt.Sprintf("invalid label_id: %s", trimmedID))
		}

		if _, err := queries.GetTimeEntryLabelInWorkspace(ctx, db.GetTimeEntryLabelInWorkspaceParams{
			ID:          labelUUID,
			WorkspaceID: entry.WorkspaceID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return newTimeEntryLabelValidationError(fmt.Sprintf("label not found: %s", trimmedID))
			}
			return fmt.Errorf("lookup time entry label %s: %w", trimmedID, err)
		}

		if err := queries.AddTimeEntryLabel(ctx, db.AddTimeEntryLabelParams{
			TimeEntryID: entry.ID,
			LabelID:     labelUUID,
		}); err != nil {
			return fmt.Errorf("add time entry label %s: %w", trimmedID, err)
		}
	}

	return nil
}

// replaceTimeEntryLabels replaces all labels on one time entry atomically.
func (h *Handler) replaceTimeEntryLabels(ctx context.Context, entry db.TimeEntry, labelIDs []string) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin label update transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	if err := h.replaceTimeEntryLabelsWithQueries(ctx, qtx, entry, labelIDs); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit label update transaction: %w", err)
	}
	return nil
}

// ListTimeEntryLabels handles GET /api/time-entry-labels.
func (h *Handler) ListTimeEntryLabels(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	labels, err := h.Queries.ListTimeEntryLabelsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list time entry labels")
		return
	}

	resp := make([]TimeEntryLabelResponse, len(labels))
	for index, label := range labels {
		resp[index] = timeEntryLabelToResponse(label)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"labels": resp,
		"total":  len(resp),
	})
}

// CreateTimeEntryLabel handles POST /api/time-entry-labels.
func (h *Handler) CreateTimeEntryLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	var req CreateTimeEntryLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	label, status, err := h.findOrCreateTimeEntryLabel(r.Context(), workspaceID, req.Name, req.Color)
	if err != nil {
		if err == errInvalidTimeEntryLabelName {
			writeError(w, http.StatusBadRequest, "label name is required")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create time entry label")
		return
	}

	writeJSON(w, status, timeEntryLabelToResponse(label))
}

// UpdateTimeEntryLabel handles PATCH /api/time-entry-labels/:id.
func (h *Handler) UpdateTimeEntryLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	labelID := chi.URLParam(r, "id")

	var req UpdateTimeEntryLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "label name is required")
		return
	}

	label, err := h.Queries.UpdateTimeEntryLabel(r.Context(), db.UpdateTimeEntryLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
		Name:        name,
		Color:       normalizeTimeEntryLabelColor(req.Color),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update time entry label")
		return
	}

	writeJSON(w, http.StatusOK, timeEntryLabelToResponse(label))
}

// DeleteTimeEntryLabel handles DELETE /api/time-entry-labels/:id.
func (h *Handler) DeleteTimeEntryLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	labelID := chi.URLParam(r, "id")

	if err := h.Queries.DeleteTimeEntryLabel(r.Context(), db.DeleteTimeEntryLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete time entry label")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddLabelToTimeEntry handles POST /api/time-entries/:entry_id/labels.
func (h *Handler) AddLabelToTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "entry_id")

	entry, err := h.loadTimeEntryForUser(r.Context(), workspaceID, userID, entryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load time entry")
		return
	}

	var req AddTimeEntryLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var label db.TimeEntryLabel
	if req.LabelID != nil && strings.TrimSpace(*req.LabelID) != "" {
		label, err = h.Queries.GetTimeEntryLabelInWorkspace(r.Context(), db.GetTimeEntryLabelInWorkspaceParams{
			ID:          parseUUID(strings.TrimSpace(*req.LabelID)),
			WorkspaceID: entry.WorkspaceID,
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

		label, _, err = h.findOrCreateTimeEntryLabel(r.Context(), workspaceID, labelName, labelColor)
		if err != nil {
			if err == errInvalidTimeEntryLabelName {
				writeError(w, http.StatusBadRequest, "label name is required")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to create time entry label")
			return
		}
	}

	if err := h.Queries.AddTimeEntryLabel(r.Context(), db.AddTimeEntryLabelParams{
		TimeEntryID: entry.ID,
		LabelID:     label.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add label")
		return
	}

	updatedEntry, err := h.Queries.GetTimeEntryByID(r.Context(), db.GetTimeEntryByIDParams{ID: entry.ID, WorkspaceID: entry.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated time entry")
		return
	}
	resp, err := h.buildTimeEntryResponse(r.Context(), updatedEntry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated time entry")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// RemoveLabelFromTimeEntry handles DELETE /api/time-entries/:entry_id/labels/:labelId.
func (h *Handler) RemoveLabelFromTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "entry_id")
	labelID := chi.URLParam(r, "labelId")

	entry, err := h.loadTimeEntryForUser(r.Context(), workspaceID, userID, entryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load time entry")
		return
	}

	if err := h.Queries.RemoveTimeEntryLabel(r.Context(), db.RemoveTimeEntryLabelParams{
		TimeEntryID: entry.ID,
		LabelID:     parseUUID(labelID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove label")
		return
	}

	updatedEntry, err := h.Queries.GetTimeEntryByID(r.Context(), db.GetTimeEntryByIDParams{ID: entry.ID, WorkspaceID: entry.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated time entry")
		return
	}
	resp, err := h.buildTimeEntryResponse(r.Context(), updatedEntry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated time entry")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// SetTimeEntryLabels handles PUT /api/time-entries/:entry_id/labels.
func (h *Handler) SetTimeEntryLabels(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "entry_id")

	entry, err := h.loadTimeEntryForUser(r.Context(), workspaceID, userID, entryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load time entry")
		return
	}

	var req SetTimeEntryLabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.replaceTimeEntryLabels(r.Context(), entry, req.LabelIDs); err != nil {
		statusCode, message := timeEntryLabelMutationErrorResponse(err)
		if statusCode >= http.StatusInternalServerError {
			slog.Warn("set time entry labels failed", append(logger.RequestAttrs(r), "error", err, "entry_id", entryID)...)
		}
		writeError(w, statusCode, message)
		return
	}

	updatedEntry, err := h.Queries.GetTimeEntryByID(r.Context(), db.GetTimeEntryByIDParams{ID: entry.ID, WorkspaceID: entry.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated time entry")
		return
	}
	resp, err := h.buildTimeEntryResponse(r.Context(), updatedEntry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated time entry")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
