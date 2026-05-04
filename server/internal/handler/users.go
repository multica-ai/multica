package handler

// users.go: admin-only endpoints powering the new Users page.
// Co-locates the per-member enforcement-toggle PATCH endpoints
// (scope + budget) and the usage roll-up the page renders alongside
// each row.

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

// requireWorkspaceAdmin enforces that the caller's member row has
// 'owner' or 'admin' role. The toggles control security and spend
// limits, so a regular member must not be able to flip them.
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

type updateMemberToggleRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

// PatchMemberScopeEnforcement is admin-only. It flips the JEH-324 scope
// toggle and writes an audit row to budget_change_log so the change is
// visible alongside other workspace-spend changes.
func (h *Handler) PatchMemberScopeEnforcement(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}

	memberID := parseUUID(chi.URLParam(r, "memberId"))
	target, err := h.Queries.GetMember(r.Context(), memberID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup member: "+err.Error())
		return
	}
	if uuidToString(target.WorkspaceID) != uuidToString(caller.WorkspaceID) {
		writeError(w, http.StatusForbidden, "member outside workspace")
		return
	}

	var body updateMemberToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if target.ScopeEnforcementEnabled == body.Enabled {
		writeJSON(w, http.StatusOK, memberToToggleResponse(target))
		return
	}

	updated, err := h.Queries.UpdateMemberScopeEnforcement(r.Context(), db.UpdateMemberScopeEnforcementParams{
		ID:                      memberID,
		ScopeEnforcementEnabled: body.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update toggle: "+err.Error())
		return
	}

	logToggleChange(r, h, caller, updated, "member_scope_toggle", target.ScopeEnforcementEnabled, updated.ScopeEnforcementEnabled, body.Reason)

	slog.Info("member scope enforcement toggled",
		"member_id", uuidToString(updated.ID),
		"enabled", updated.ScopeEnforcementEnabled,
		"by", uuidToString(caller.UserID),
	)
	writeJSON(w, http.StatusOK, memberToToggleResponse(updated))
}

// PatchMemberBudgetEnforcement is the per-user budget-cap toggle.
// Workspace and per-agent caps still apply when this is off; only the
// user-axis check in CheckPreClaim is bypassed.
func (h *Handler) PatchMemberBudgetEnforcement(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}

	memberID := parseUUID(chi.URLParam(r, "memberId"))
	target, err := h.Queries.GetMember(r.Context(), memberID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup member: "+err.Error())
		return
	}
	if uuidToString(target.WorkspaceID) != uuidToString(caller.WorkspaceID) {
		writeError(w, http.StatusForbidden, "member outside workspace")
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
		ID:                       memberID,
		BudgetEnforcementEnabled: body.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update toggle: "+err.Error())
		return
	}

	logToggleChange(r, h, caller, updated, "member_budget_toggle", target.BudgetEnforcementEnabled, updated.BudgetEnforcementEnabled, body.Reason)

	slog.Info("member budget enforcement toggled",
		"member_id", uuidToString(updated.ID),
		"enabled", updated.BudgetEnforcementEnabled,
		"by", uuidToString(caller.UserID),
	)
	writeJSON(w, http.StatusOK, memberToToggleResponse(updated))
}

type memberUsageResponse struct {
	UserID         string `json:"user_id"`
	DailyCents     int64  `json:"daily_cents"`
	MonthlyCents   int64  `json:"monthly_cents"`
	DailyWindow    string `json:"daily_window"`
	MonthlyWindow  string `json:"monthly_window"`
}

// GetMemberUsage returns today's and the current month's spend for a
// single member. Used by the Users page so each row can show the
// real-time number alongside its caps.
func (h *Handler) GetMemberUsage(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}

	memberID := parseUUID(chi.URLParam(r, "memberId"))
	target, err := h.Queries.GetMember(r.Context(), memberID)
	if err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if uuidToString(target.WorkspaceID) != uuidToString(caller.WorkspaceID) {
		writeError(w, http.StatusForbidden, "member outside workspace")
		return
	}

	now := time.Now().UTC()
	dayStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
	monthStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}

	daily := lookupSpend(r, h, target.WorkspaceID, "user", target.UserID, "day", dayStart)
	monthly := lookupSpend(r, h, target.WorkspaceID, "user", target.UserID, "month", monthStart)

	writeJSON(w, http.StatusOK, memberUsageResponse{
		UserID:        uuidToString(target.UserID),
		DailyCents:    daily,
		MonthlyCents:  monthly,
		DailyWindow:   dayStart.Time.Format("2006-01-02"),
		MonthlyWindow: monthStart.Time.Format("2006-01-02"),
	})
}

func lookupSpend(r *http.Request, h *Handler, workspaceID pgtype.UUID, scopeType string, scopeID pgtype.UUID, windowType string, windowStart pgtype.Date) int64 {
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

func logToggleChange(r *http.Request, h *Handler, caller, updated db.Member, scope string, oldEnabled, newEnabled bool, reason string) {
	if err := h.Queries.InsertBudgetChangeLog(r.Context(), db.InsertBudgetChangeLogParams{
		WorkspaceID:   caller.WorkspaceID,
		ChangedBy:     caller.UserID,
		ScopeType:     scope,
		ScopeID:       updated.ID,
		Field:         "enabled",
		OldValueCents: pgtype.Int8{Int64: boolToInt64(oldEnabled), Valid: true},
		NewValueCents: pgtype.Int8{Int64: boolToInt64(newEnabled), Valid: true},
		Reason:        pgtype.Text{String: reason, Valid: reason != ""},
	}); err != nil {
		slog.Warn("audit log write failed", "scope", scope, "member_id", uuidToString(updated.ID), "error", err)
	}
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func memberToToggleResponse(m db.Member) MemberWithUserResponse {
	return MemberWithUserResponse{
		ID:                       uuidToString(m.ID),
		WorkspaceID:              uuidToString(m.WorkspaceID),
		UserID:                   uuidToString(m.UserID),
		Role:                     m.Role,
		CreatedAt:                timestampToString(m.CreatedAt),
		ScopeEnforcementEnabled:  m.ScopeEnforcementEnabled,
		BudgetEnforcementEnabled: m.BudgetEnforcementEnabled,
	}
}
