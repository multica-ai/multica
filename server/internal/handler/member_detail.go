package handler

// CEREBRO-PATCH(member-detail-handler): cerebro modification of upstream file

// member_detail.go: admin endpoints behind the click-into-member detail
// page reachable from Settings → Members. Each handler is a small,
// self-contained PATCH/GET that defaults to the safe behavior on any
// lookup error so a misrouted call can never silently change state.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) requireWorkspaceAdmin(w http.ResponseWriter, r *http.Request) (db.Member, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no member in context")
		return db.Member{}, false
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "admin access required")
		return db.Member{}, false
	}
	return member, true
}

func (h *Handler) loadTargetMember(w http.ResponseWriter, r *http.Request, caller db.Member) (db.Member, bool) {
	memberID := parseUUID(chi.URLParam(r, "memberId"))
	target, err := h.Queries.GetMember(r.Context(), memberID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "member not found")
		} else {
			writeError(w, http.StatusInternalServerError, "lookup member: "+err.Error())
		}
		return db.Member{}, false
	}
	if uuidToString(target.WorkspaceID) != uuidToString(caller.WorkspaceID) {
		writeError(w, http.StatusForbidden, "member outside workspace")
		return db.Member{}, false
	}
	return target, true
}

type updateMemberToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// PatchMemberBudgetEnforcement flips the JEH-327 budget-cap toggle.
// Owner/admin only.
func (h *Handler) PatchMemberBudgetEnforcement(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	target, ok := h.loadTargetMember(w, r, caller)
	if !ok {
		return
	}

	var body updateMemberToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if target.BudgetEnforcementEnabled == body.Enabled {
		writeJSON(w, http.StatusOK, memberToToggleResponse(target))
		return
	}

	updated, err := h.Queries.UpdateMemberBudgetEnforcement(r.Context(), db.UpdateMemberBudgetEnforcementParams{
		ID:                       target.ID,
		BudgetEnforcementEnabled: body.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update toggle: "+err.Error())
		return
	}

	slog.Info("member budget enforcement toggled",
		"member_id", uuidToString(updated.ID),
		"enabled", updated.BudgetEnforcementEnabled,
		"by", uuidToString(caller.UserID),
	)
	writeJSON(w, http.StatusOK, memberToToggleResponse(updated))
}

type memberUsageResponse struct {
	UserID        string `json:"user_id"`
	DailyCents    int64  `json:"daily_cents"`
	MonthlyCents  int64  `json:"monthly_cents"`
	DailyWindow   string `json:"daily_window"`
	MonthlyWindow string `json:"monthly_window"`
}

// GetMemberUsage returns today's and current month's spend for a
// single member. Drives the spend column on the member detail page.
func (h *Handler) GetMemberUsage(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	target, ok := h.loadTargetMember(w, r, caller)
	if !ok {
		return
	}

	now := time.Now().UTC()
	dayStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
	monthStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}

	daily := h.lookupSpend(r, target.WorkspaceID, "user", target.UserID, "day", dayStart)
	monthly := h.lookupSpend(r, target.WorkspaceID, "user", target.UserID, "month", monthStart)

	writeJSON(w, http.StatusOK, memberUsageResponse{
		UserID:        uuidToString(target.UserID),
		DailyCents:    daily,
		MonthlyCents:  monthly,
		DailyWindow:   dayStart.Time.Format("2006-01-02"),
		MonthlyWindow: monthStart.Time.Format("2006-01-02"),
	})
}

func (h *Handler) lookupSpend(r *http.Request, workspaceID pgtype.UUID, scopeType string, scopeID pgtype.UUID, windowType string, windowStart pgtype.Date) int64 {
	state, err := h.Queries.GetBudgetState(r.Context(), db.GetBudgetStateParams{
		WorkspaceID: workspaceID,
		ScopeType:   scopeType,
		ScopeID:     scopeID,
		WindowType:  windowType,
		WindowStart: windowStart,
	})
	if err != nil {
		return 0
	}
	return state.CentsSpent
}

func memberToToggleResponse(m db.Member) MemberWithUserResponse {
	return MemberWithUserResponse{
		ID:                       uuidToString(m.ID),
		WorkspaceID:              uuidToString(m.WorkspaceID),
		UserID:                   uuidToString(m.UserID),
		Role:                     m.Role,
		CreatedAt:                timestampToString(m.CreatedAt),
		BudgetEnforcementEnabled: m.BudgetEnforcementEnabled,
	}
}
