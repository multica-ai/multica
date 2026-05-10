package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DailyPlanHandler exposes HTTP endpoints for daily plan operations.
type DailyPlanHandler struct {
	planSvc *service.DailyPlanService
}

// NewDailyPlanHandler creates a DailyPlanHandler with the given service.
func NewDailyPlanHandler(planSvc *service.DailyPlanService) *DailyPlanHandler {
	return &DailyPlanHandler{planSvc: planSvc}
}

// DailyPlanResponse is the JSON shape returned to clients.
type DailyPlanResponse struct {
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspace_id"`
	UserID       string   `json:"user_id"`
	PlanDate     string   `json:"plan_date"` // YYYY-MM-DD
	DraftContent string   `json:"draft_content"`
	TopIssueIDs  []string `json:"top_issue_ids"`
	Status       string   `json:"status"`
	ConfirmedAt  *string  `json:"confirmed_at"`
	GeneratedBy  string   `json:"generated_by"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

func dailyPlanToResponse(p db.DailyPlan) DailyPlanResponse {
	planDate := ""
	if p.PlanDate.Valid {
		planDate = p.PlanDate.Time.Format("2006-01-02")
	}

	topIDs := make([]string, 0, len(p.TopIssueIds))
	for _, u := range p.TopIssueIds {
		if u.Valid {
			topIDs = append(topIDs, util.UUIDToString(u))
		}
	}

	return DailyPlanResponse{
		ID:           util.UUIDToString(p.ID),
		WorkspaceID:  util.UUIDToString(p.WorkspaceID),
		UserID:       util.UUIDToString(p.UserID),
		PlanDate:     planDate,
		DraftContent: p.DraftContent,
		TopIssueIDs:  topIDs,
		Status:       p.Status,
		ConfirmedAt:  util.TimestampToPtr(p.ConfirmedAt),
		GeneratedBy:  p.GeneratedBy,
		CreatedAt:    util.TimestampToString(p.CreatedAt),
		UpdatedAt:    util.TimestampToString(p.UpdatedAt),
	}
}

// GeneratePlan handles POST /api/daily-plans/generate.
// Generates (or regenerates) tomorrow's plan draft for the authenticated user.
// Optionally accepts a JSON body with { "plan_date": "YYYY-MM-DD" }.
func (h *DailyPlanHandler) GeneratePlan(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	// Default to tomorrow; allow override via JSON body.
	planDate := time.Now().UTC().Add(24 * time.Hour)
	var req struct {
		PlanDate string `json:"plan_date"`
	}
	_ = discardBody(r, &req)
	if req.PlanDate != "" {
		if t, err := time.Parse("2006-01-02", req.PlanDate); err == nil {
			planDate = t
		}
	}

	plan, err := h.planSvc.GeneratePlanDraft(r.Context(), workspaceID, userID, planDate, "manual")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate plan: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, dailyPlanToResponse(plan))
}

// GetTomorrowPlan handles GET /api/daily-plans/tomorrow.
// Returns tomorrow's plan draft, or 204 No Content if none exists.
func (h *DailyPlanHandler) GetTomorrowPlan(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	plan, err := h.planSvc.GetTomorrowPlan(r.Context(), workspaceID, userID)
	if err != nil {
		if isNotFound(err) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get plan")
		return
	}

	writeJSON(w, http.StatusOK, dailyPlanToResponse(plan))
}

// ListPlans handles GET /api/daily-plans?limit=N.
// Returns the most recent plans for the authenticated user (default limit: 30).
func (h *DailyPlanHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	limit := int32(30)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = int32(v)
		}
	}

	plans, err := h.planSvc.ListPlans(r.Context(), workspaceID, userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list plans")
		return
	}

	resp := make([]DailyPlanResponse, len(plans))
	for i, p := range plans {
		resp[i] = dailyPlanToResponse(p)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ConfirmPlan handles POST /api/daily-plans/{id}/confirm.
// Marks the specified plan draft as confirmed by the user.
func (h *DailyPlanHandler) ConfirmPlan(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "id")
	var planID pgtype.UUID
	if err := planID.Scan(idStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}

	plan, err := h.planSvc.ConfirmPlan(r.Context(), workspaceID, planID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to confirm plan")
		return
	}

	writeJSON(w, http.StatusOK, dailyPlanToResponse(plan))
}
