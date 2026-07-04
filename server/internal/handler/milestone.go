package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func dateToString(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

func parseDateParam(w http.ResponseWriter, s *string, paramName string) (pgtype.Date, bool) {
	if s == nil || *s == "" {
		return pgtype.Date{Valid: false}, true
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date format for " + paramName)
		return pgtype.Date{Valid: false}, false
	}
	return pgtype.Date{Time: t, Valid: true}, true
}

type MilestoneResponse struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
	Status      string  `json:"status"`
	SortOrder   int32   `json:"sort_order"`
	CreatedBy   string  `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func milestoneToResponse(m db.Milestone) MilestoneResponse {
	return MilestoneResponse{
		ID:          uuidToString(m.ID),
		ProjectID:   uuidToString(m.ProjectID),
		Title:       m.Title,
		Description: textToPtr(m.Description),
		StartDate:   dateToString(m.StartDate),
		DueDate:     dateToString(m.DueDate),
		Status:      m.Status,
		SortOrder:   m.SortOrder,
		CreatedBy:   uuidToString(m.CreatedBy),
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func (h *Handler) ListMilestones(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}

	milestones, err := h.Queries.ListMilestonesByProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list milestones")
		return
	}

	resp := make([]MilestoneResponse, len(milestones))
	for i, m := range milestones {
		resp[i] = milestoneToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	
	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
		StartDate   *string `json:"start_date"`
		DueDate     *string `json:"due_date"`
		Status      string  `json:"status"`
		SortOrder   int32   `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Status == "" {
		req.Status = "open"
	}
	
	startDate, ok := parseDateParam(w, req.StartDate, "start_date")
	if !ok {
		return
	}
	dueDate, ok := parseDateParam(w, req.DueDate, "due_date")
	if !ok {
		return
	}

	m, err := h.Queries.CreateMilestone(r.Context(), db.CreateMilestoneParams{
		ProjectID:   projectID,
		Title:       req.Title,
		Description: ptrToText(req.Description),
		StartDate:   startDate,
		DueDate:     dueDate,
		Status:      req.Status,
		SortOrder:   req.SortOrder,
		CreatedBy:   requester.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create milestone")
		return
	}

	resp := milestoneToResponse(m)
	writeJSON(w, http.StatusCreated, resp)
	
	h.publish(protocol.EventMilestoneCreated, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"milestone": resp,
	})
}

func (h *Handler) GetMilestone(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	milestoneIdStr := chi.URLParam(r, "milestoneId")
	milestoneUUID, ok := parseUUIDOrBadRequest(w, milestoneIdStr, "milestone id")
	if !ok {
		return
	}

	m, err := h.Queries.GetMilestone(r.Context(), milestoneUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}

	writeJSON(w, http.StatusOK, milestoneToResponse(m))
}

func (h *Handler) UpdateMilestone(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	milestoneIdStr := chi.URLParam(r, "milestoneId")
	milestoneUUID, ok := parseUUIDOrBadRequest(w, milestoneIdStr, "milestone id")
	if !ok {
		return
	}

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		StartDate   *string `json:"start_date"`
		DueDate     *string `json:"due_date"`
		Status      *string `json:"status"`
		SortOrder   *int32  `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	
	startDate, ok := parseDateParam(w, req.StartDate, "start_date")
	if !ok {
		return
	}
	dueDate, ok := parseDateParam(w, req.DueDate, "due_date")
	if !ok {
		return
	}

	params := db.UpdateMilestoneParams{
		ID:          milestoneUUID,
		StartDate:   startDate,
		DueDate:     dueDate,
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.SortOrder != nil {
		params.SortOrder = pgtype.Int4{Int32: *req.SortOrder, Valid: true}
	}
	
	// Check if date fields were passed in request body if nil
	// Or we can let COALESCE handle nil dates in SQL but SQL COALESCE won't know if we explicitly passed nil for StartDate vs didn't pass it. 
	// Wait! The sqlc for updateMilestone uses COALESCE. So if we pass invalid (false), it ignores it. But what if we WANT to clear the date? 
	// The problem is COALESCE($3, start_date) means if $3 is NULL, keep old value.
	// Since the DB schema might allow NULL for dates (and frontend might want to clear it), this is standard. 
	// The problem is, how do we distinguish "not provided" from "clear date"? 
	// Let's assume standard update where if not provided, we just rely on the DB's COALESCE, meaning we can't clear dates unless we rewrite the SQL. For now we will just use the current SQL and pass false for missing fields. 
	// The frontend usually sends the whole object, but if it sends a partial, missing means false.
	// In our parseDateParam, if string is nil or "", Valid=false. So it won't update the date. 

	m, err := h.Queries.UpdateMilestone(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update milestone")
		return
	}

	resp := milestoneToResponse(m)
	writeJSON(w, http.StatusOK, resp)
	
	h.publish(protocol.EventMilestoneUpdated, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"milestone": resp,
	})
}

func (h *Handler) DeleteMilestone(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	milestoneIdStr := chi.URLParam(r, "milestoneId")
	milestoneUUID, ok := parseUUIDOrBadRequest(w, milestoneIdStr, "milestone id")
	if !ok {
		return
	}

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	err := h.Queries.DeleteMilestone(r.Context(), milestoneUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete milestone")
		return
	}

	w.WriteHeader(http.StatusNoContent)
	
	h.publish(protocol.EventMilestoneDeleted, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"milestone_id": milestoneIdStr,
	})
}
