package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

type PlanResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	CreatorID   string  `json:"creator_id"`
	Title       string  `json:"title"`
	Content     *string `json:"content"`
	Status      string  `json:"status"`
	WorkflowID  *string `json:"workflow_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func planToResponse(p service.PlanOutput) PlanResponse {
	return PlanResponse{
		ID:          p.ID,
		WorkspaceID: p.WorkspaceID,
		CreatorID:   p.CreatorID,
		Title:       p.Title,
		Content:     p.Content,
		Status:      p.Status,
		WorkflowID:  p.WorkflowID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "not a workspace member")
		return
	}

	var body struct {
		Title   string  `json:"title"`
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	plan, err := h.PlanSvc.Create(r.Context(), wsID, util.UUIDToString(member.ID), body.Title, body.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, planToResponse(plan))
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	planIDStr := chi.URLParam(r, "planId")
	planID, ok := parseUUIDOrBadRequest(w, planIDStr, "planId")
	if !ok {
		return
	}
	plan, err := h.PlanSvc.Get(r.Context(), planID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, planToResponse(plan))
}

func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	wsID := ctxWorkspaceID(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	plans, err := h.PlanSvc.List(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := make([]PlanResponse, len(plans))
	for i, p := range plans {
		resp[i] = planToResponse(p)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	planIDStr := chi.URLParam(r, "planId")
	planID, ok := parseUUIDOrBadRequest(w, planIDStr, "planId")
	if !ok {
		return
	}
	var body struct {
		Title   *string `json:"title"`
		Content *string `json:"content"`
		Status  *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	plan, err := h.PlanSvc.Update(r.Context(), planID, body.Title, body.Content, body.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, planToResponse(plan))
}
