package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type PlanResponse struct {
	ID              string             `json:"id"`
	WorkspaceID     string             `json:"workspace_id"`
	UserID          string             `json:"user_id"`
	PlanDate        string             `json:"plan_date"`
	Status          string             `json:"status"`
	EnergyLevel     *int32             `json:"energy_level"`
	EnergyNote      *string            `json:"energy_note"`
	RecoveryNeed    bool               `json:"recovery_need"`
	CapacityMinutes *int32             `json:"capacity_minutes"`
	CapacityNote    *string            `json:"capacity_note"`
	Items           []PlanItemResponse `json:"items"`
	CreatedAt       string             `json:"created_at"`
	UpdatedAt       string             `json:"updated_at"`
}

type PlanItemResponse struct {
	ID                   string  `json:"id"`
	WorkspaceID          string  `json:"workspace_id"`
	UserID               string  `json:"user_id"`
	PlanID               string  `json:"plan_id"`
	IssueID              *string `json:"issue_id"`
	IssueTypeID          *string `json:"issue_type_id"`
	SuggestedIssueTypeID *string `json:"suggested_issue_type_id"`
	TitleSnapshot        string  `json:"title_snapshot"`
	Note                 string  `json:"note"`
	Position             int32   `json:"position"`
	EstimatedMinutes     *int32  `json:"estimated_minutes"`
	ActualSeconds        int64   `json:"actual_seconds"`
	Status               string  `json:"status"`
	StatusReason         *string `json:"status_reason"`
	Source               string  `json:"source"`
	CompletedAt          *string `json:"completed_at"`
	SkippedAt            *string `json:"skipped_at"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type upsertPlanRequest struct {
	Date            string  `json:"date"`
	EnergyLevel     *int32  `json:"energy_level"`
	EnergyNote      *string `json:"energy_note"`
	RecoveryNeed    bool    `json:"recovery_need"`
	CapacityMinutes *int32  `json:"capacity_minutes"`
	CapacityNote    *string `json:"capacity_note"`
}

type createPlanItemRequest struct {
	IssueID              *string `json:"issue_id"`
	SuggestedIssueTypeID *string `json:"suggested_issue_type_id"`
	Title                string  `json:"title"`
	Note                 *string `json:"note"`
	EstimatedMinutes     *int32  `json:"estimated_minutes"`
}

type updatePlanItemRequest struct {
	Title                *string `json:"title"`
	Note                 *string `json:"note"`
	EstimatedMinutes     *int32  `json:"estimated_minutes"`
	Status               *string `json:"status"`
	StatusReason         *string `json:"status_reason"`
	SuggestedIssueTypeID *string `json:"suggested_issue_type_id"`
}

func planDateFromString(value string) (time.Time, error) {
	now := time.Now().UTC()
	switch value {
	case "", "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	case "tomorrow":
		tomorrow := now.Add(24 * time.Hour)
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC), nil
	default:
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			return time.Time{}, err
		}
		return parsed, nil
	}
}

func int32ToPg(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func planItemStatusValid(status string) bool {
	switch status {
	case "planned", "in_progress", "progressed", "done", "skipped":
		return true
	default:
		return false
	}
}

func planItemToResponse(item db.PlanItem, actualSeconds int64, issueTypeID *string) PlanItemResponse {
	return PlanItemResponse{
		ID:                   uuidToString(item.ID),
		WorkspaceID:          uuidToString(item.WorkspaceID),
		UserID:               uuidToString(item.UserID),
		PlanID:               uuidToString(item.PlanID),
		IssueID:              uuidToPtr(item.IssueID),
		IssueTypeID:          issueTypeID,
		SuggestedIssueTypeID: uuidToPtr(item.SuggestedIssueTypeID),
		TitleSnapshot:        item.TitleSnapshot,
		Note:                 item.Note,
		Position:             item.Position,
		EstimatedMinutes:     int4ToPtr(item.EstimatedMinutes),
		ActualSeconds:        actualSeconds,
		Status:               item.Status,
		StatusReason:         textToPtr(item.StatusReason),
		Source:               item.Source,
		CompletedAt:          timestampToPtr(item.CompletedAt),
		SkippedAt:            timestampToPtr(item.SkippedAt),
		CreatedAt:            timestampToString(item.CreatedAt),
		UpdatedAt:            timestampToString(item.UpdatedAt),
	}
}

func planItemIssueTypeID(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, item db.PlanItem) (*string, error) {
	issueTypeID := uuidToPtr(item.SuggestedIssueTypeID)
	if !item.IssueID.Valid {
		return issueTypeID, nil
	}
	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          item.IssueID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}
	return uuidToPtr(issue.IssueTypeID), nil
}

func (h *Handler) buildPlanResponse(ctx context.Context, queries *db.Queries, plan db.DailyPlan) (PlanResponse, error) {
	items, err := queries.ListPlanItems(ctx, db.ListPlanItemsParams{
		WorkspaceID: plan.WorkspaceID,
		PlanID:      plan.ID,
		UserID:      plan.UserID,
	})
	if err != nil {
		return PlanResponse{}, err
	}
	respItems := make([]PlanItemResponse, len(items))
	for i, item := range items {
		entries, err := queries.ListTimeEntriesByPlanItem(ctx, db.ListTimeEntriesByPlanItemParams{
			WorkspaceID: plan.WorkspaceID,
			PlanItemID:  item.ID,
		})
		if err != nil {
			return PlanResponse{}, err
		}
		var actualSeconds int64
		for _, entry := range entries {
			if entry.StopTime.Valid {
				actualSeconds += entry.DurationSeconds
			}
		}
		issueTypeID, err := planItemIssueTypeID(ctx, queries, plan.WorkspaceID, item)
		if err != nil {
			return PlanResponse{}, err
		}
		respItems[i] = planItemToResponse(item, actualSeconds, issueTypeID)
	}
	planDate := ""
	if plan.PlanDate.Valid {
		planDate = plan.PlanDate.Time.Format(time.DateOnly)
	}
	return PlanResponse{
		ID:              uuidToString(plan.ID),
		WorkspaceID:     uuidToString(plan.WorkspaceID),
		UserID:          uuidToString(plan.UserID),
		PlanDate:        planDate,
		Status:          plan.Status,
		EnergyLevel:     int4ToPtr(plan.EnergyLevel),
		EnergyNote:      textToPtr(plan.EnergyNote),
		RecoveryNeed:    plan.RecoveryNeed,
		CapacityMinutes: int4ToPtr(plan.CapacityMinutes),
		CapacityNote:    textToPtr(plan.CapacityNote),
		Items:           respItems,
		CreatedAt:       timestampToString(plan.CreatedAt),
		UpdatedAt:       timestampToString(plan.UpdatedAt),
	}, nil
}

func (h *Handler) ensurePlanForDate(ctx context.Context, workspaceID, userID pgtype.UUID, date time.Time) (db.DailyPlan, error) {
	planDate := pgtype.Date{Time: date, Valid: true}
	plan, err := h.Queries.GetPlanByDate(ctx, db.GetPlanByDateParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		PlanDate:    planDate,
	})
	if err == nil {
		return plan, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.DailyPlan{}, err
	}
	return h.Queries.UpsertPlanForDate(ctx, db.UpsertPlanForDateParams{
		WorkspaceID:  workspaceID,
		UserID:       userID,
		PlanDate:     planDate,
		DraftContent: "",
		TopIssueIds:  []pgtype.UUID{},
		GeneratedBy:  "manual",
		RecoveryNeed: false,
	})
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}
	date, err := planDateFromString(r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date")
		return
	}
	plan, err := h.ensurePlanForDate(r.Context(), workspaceID, userID, date)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get plan")
		return
	}
	resp, err := h.buildPlanResponse(r.Context(), h.Queries, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get plan")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpsertPlan(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}
	var req upsertPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	date, err := planDateFromString(req.Date)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date")
		return
	}
	plan, err := h.Queries.UpsertPlanForDate(r.Context(), db.UpsertPlanForDateParams{
		WorkspaceID:     workspaceID,
		UserID:          userID,
		PlanDate:        pgtype.Date{Time: date, Valid: true},
		DraftContent:    "",
		TopIssueIds:     []pgtype.UUID{},
		GeneratedBy:     "manual",
		EnergyLevel:     int32ToPg(req.EnergyLevel),
		EnergyNote:      ptrToText(req.EnergyNote),
		RecoveryNeed:    req.RecoveryNeed,
		CapacityMinutes: int32ToPg(req.CapacityMinutes),
		CapacityNote:    ptrToText(req.CapacityNote),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save plan")
		return
	}
	resp, err := h.buildPlanResponse(r.Context(), h.Queries, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save plan")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreatePlanItem(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}
	var req createPlanItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	planID := parseUUID(chi.URLParam(r, "id"))
	items, err := h.Queries.ListPlanItems(r.Context(), db.ListPlanItemsParams{
		WorkspaceID: workspaceID,
		PlanID:      planID,
		UserID:      userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plan item")
		return
	}
	issueID := parseOptionalUUID(req.IssueID)
	suggestedIssueTypeID := parseOptionalUUID(req.SuggestedIssueTypeID)
	title := req.Title
	if issueID.Valid {
		if existing, err := h.Queries.GetPlanItemByIssue(r.Context(), db.GetPlanItemByIssueParams{
			WorkspaceID: workspaceID,
			PlanID:      planID,
			UserID:      userID,
			IssueID:     issueID,
		}); err == nil {
			issueTypeID, err := planItemIssueTypeID(r.Context(), h.Queries, workspaceID, existing)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create plan item")
				return
			}
			writeJSON(w, http.StatusOK, planItemToResponse(existing, 0, issueTypeID))
			return
		}
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "issue not found")
			return
		}
		if title == "" {
			title = issue.Title
		}
	}
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item, err := h.Queries.CreatePlanItem(r.Context(), db.CreatePlanItemParams{
		WorkspaceID:          workspaceID,
		UserID:               userID,
		PlanID:               planID,
		IssueID:              issueID,
		SuggestedIssueTypeID: suggestedIssueTypeID,
		TitleSnapshot:        title,
		Note:                 ptrToText(req.Note),
		Position:             int32(len(items)+1) * 10,
		EstimatedMinutes:     int32ToPg(req.EstimatedMinutes),
		Status:               "planned",
		Source:               "manual",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plan item")
		return
	}
	issueTypeID, err := planItemIssueTypeID(r.Context(), h.Queries, workspaceID, item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plan item")
		return
	}
	writeJSON(w, http.StatusCreated, planItemToResponse(item, 0, issueTypeID))
}

func (h *Handler) UpdatePlanItem(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}
	var req updatePlanItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != nil && !planItemStatusValid(*req.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	item, err := h.Queries.UpdatePlanItem(r.Context(), db.UpdatePlanItemParams{
		ID:                   parseUUID(chi.URLParam(r, "id")),
		WorkspaceID:          workspaceID,
		UserID:               userID,
		TitleSnapshot:        ptrToText(req.Title),
		Note:                 ptrToText(req.Note),
		EstimatedMinutes:     int32ToPg(req.EstimatedMinutes),
		Status:               ptrToText(req.Status),
		StatusReason:         ptrToText(req.StatusReason),
		SuggestedIssueTypeID: parseOptionalUUID(req.SuggestedIssueTypeID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "plan item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update plan item")
		return
	}
	issueTypeID, err := planItemIssueTypeID(r.Context(), h.Queries, workspaceID, item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update plan item")
		return
	}
	writeJSON(w, http.StatusOK, planItemToResponse(item, 0, issueTypeID))
}

func (h *Handler) DeletePlanItem(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeletePlanItem(r.Context(), db.DeletePlanItemParams{
		ID:          parseUUID(chi.URLParam(r, "id")),
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete plan item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListPlanCandidates(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireUserAndWorkspace(w, r)
	if !ok {
		return
	}
	date, err := planDateFromString(r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date")
		return
	}
	plan, err := h.ensurePlanForDate(r.Context(), workspaceID, userID, date)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list candidates")
		return
	}
	candidates, err := h.Queries.ListPlanCandidates(r.Context(), db.ListPlanCandidatesParams{
		WorkspaceID: workspaceID,
		IssueTypeID: parseOptionalUUID(func() *string {
			if v := r.URL.Query().Get("issue_type_id"); v != "" {
				return &v
			}
			return nil
		}()),
		PlanID:     plan.ID,
		LimitCount: parseInt32Query(r, "limit", 50),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list candidates")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), workspaceID)
	resp := make([]IssueResponse, len(candidates))
	for i, issue := range candidates {
		resp[i] = issueToResponse(issue, prefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": resp})
}

func (h *Handler) StartPlanItemFocus(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	var req focusStartRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	req.PlanItemID = &itemID
	body, _ := json.Marshal(req)
	r.ContentLength = int64(len(body))
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.StartFocus(w, r)
}
