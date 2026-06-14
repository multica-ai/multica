package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DailyReviewHandler exposes HTTP endpoints for daily review operations.
type DailyReviewHandler struct {
	reviewSvc *service.ReviewService
}

// NewDailyReviewHandler creates a DailyReviewHandler with the given service.
func NewDailyReviewHandler(reviewSvc *service.ReviewService) *DailyReviewHandler {
	return &DailyReviewHandler{reviewSvc: reviewSvc}
}

// DailyReviewResponse is the JSON shape returned to clients.
type DailyReviewResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	UserID       string  `json:"user_id"`
	ReviewDate   string  `json:"review_date"` // YYYY-MM-DD
	DraftContent string  `json:"draft_content"`
	Status       string  `json:"status"`
	ConfirmedAt  *string `json:"confirmed_at"`
	GeneratedBy  string  `json:"generated_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	EnergyLevel  *int32  `json:"energy_level"`
	EnergyNote   *string `json:"energy_note"`
	RecoveryNeed *bool   `json:"recovery_need"`
}

type ConfirmDailyReviewRequest struct {
	EnergyLevel  *int32  `json:"energy_level"`
	EnergyNote   *string `json:"energy_note"`
	RecoveryNeed *bool   `json:"recovery_need"`
}

func dailyReviewToResponse(r db.DailyReview) DailyReviewResponse {
	reviewDate := ""
	if r.ReviewDate.Valid {
		reviewDate = r.ReviewDate.Time.Format("2006-01-02")
	}
	return DailyReviewResponse{
		ID:           util.UUIDToString(r.ID),
		WorkspaceID:  util.UUIDToString(r.WorkspaceID),
		UserID:       util.UUIDToString(r.UserID),
		ReviewDate:   reviewDate,
		DraftContent: r.DraftContent,
		Status:       r.Status,
		ConfirmedAt:  util.TimestampToPtr(r.ConfirmedAt),
		GeneratedBy:  r.GeneratedBy,
		CreatedAt:    util.TimestampToString(r.CreatedAt),
		UpdatedAt:    util.TimestampToString(r.UpdatedAt),
		EnergyLevel:  int4ToPtr(r.EnergyLevel),
		EnergyNote:   textToPtr(r.EnergyNote),
		RecoveryNeed: boolToPtr(r.RecoveryNeed),
	}
}

func boolToPtr(value pgtype.Bool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

// GenerateReview handles POST /api/daily-reviews/generate.
// Generates (or regenerates) today's review draft for the authenticated user.
func (h *DailyReviewHandler) GenerateReview(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	review, err := h.reviewSvc.GenerateReviewDraft(r.Context(), workspaceID, userID, time.Now().UTC(), "manual")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate review: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, dailyReviewToResponse(review))
}

// GetTodayReview handles GET /api/daily-reviews/today.
// Returns today's review draft, or 204 No Content if none exists.
func (h *DailyReviewHandler) GetTodayReview(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	review, err := h.reviewSvc.GetTodayReview(r.Context(), workspaceID, userID)
	if err != nil {
		if isNotFound(err) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get review")
		return
	}

	writeJSON(w, http.StatusOK, dailyReviewToResponse(review))
}

// ListReviews handles GET /api/daily-reviews?limit=N.
// Returns the most recent reviews for the authenticated user (default limit: 30).
func (h *DailyReviewHandler) ListReviews(w http.ResponseWriter, r *http.Request) {
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

	reviews, err := h.reviewSvc.ListReviews(r.Context(), workspaceID, userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reviews")
		return
	}

	resp := make([]DailyReviewResponse, len(reviews))
	for i, rev := range reviews {
		resp[i] = dailyReviewToResponse(rev)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ConfirmReview handles POST /api/daily-reviews/{id}/confirm.
// Marks the specified review draft as confirmed by the user.
func (h *DailyReviewHandler) ConfirmReview(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "id")
	var reviewID pgtype.UUID
	if err := reviewID.Scan(idStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid review id")
		return
	}

	var req ConfirmDailyReviewRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if req.EnergyLevel != nil && (*req.EnergyLevel < 1 || *req.EnergyLevel > 5) {
		writeError(w, http.StatusBadRequest, "invalid energy_level")
		return
	}

	review, err := h.reviewSvc.ConfirmReview(r.Context(), workspaceID, reviewID, service.ReviewEnergyInput{
		EnergyLevel:  req.EnergyLevel,
		EnergyNote:   req.EnergyNote,
		RecoveryNeed: req.RecoveryNeed,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "review not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to confirm review")
		return
	}

	writeJSON(w, http.StatusOK, dailyReviewToResponse(review))
}

// requireUserAndWorkspace extracts userID and workspaceID from the request context/headers.
// Writes an error and returns false if either is missing.
func requireUserAndWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	workspaceIDStr := resolveWorkspaceID(r)
	if workspaceIDStr == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	var userID pgtype.UUID
	if err := userID.Scan(userIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	var workspaceID pgtype.UUID
	if err := workspaceID.Scan(workspaceIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	return userID, workspaceID, true
}

// discardBody ensures json decode does not panic for empty bodies.
func discardBody(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(v)
}
