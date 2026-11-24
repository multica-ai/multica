package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

// Monthly app call limit (MUL-10047). The limit itself lives in
// service.AppToolCallQuota and is checked by the Connected Apps gateway right
// before a call reaches Composio; this file serves the Billing read.

func (h *Handler) appToolCallQuota() service.AppToolCallQuota {
	return service.AppToolCallQuota{Queries: h.Queries, Entitlements: h.Entitlements}
}

// AppCallUsageResponse is the monthly app call limit as clients render it.
// action is off when no limit applies, including self-hosted deployments.
// While a limit applies used, limit, period_start and reset_at are present;
// reached only under enforce. members is the period's calls per person, for
// owners and admins only; a null user_id is runs no person started.
type AppCallUsageResponse struct {
	Action      string                    `json:"action"`
	Used        *int64                    `json:"used,omitempty"`
	Limit       *int64                    `json:"limit,omitempty"`
	Reached     *bool                     `json:"reached,omitempty"`
	PeriodStart *string                   `json:"period_start,omitempty"`
	ResetAt     *string                   `json:"reset_at,omitempty"`
	Members     []AppCallMemberUsageEntry `json:"members,omitempty"`
}

type AppCallMemberUsageEntry struct {
	UserID *string `json:"user_id"`
	Calls  int64   `json:"calls"`
}

// GetAppCallUsage — GET /api/workspaces/{id}/app-call-usage
//
// Member-readable: an agent's refusal names the limit, and members read why.
// The per-person breakdown is for owners and admins.
func (h *Handler) GetAppCallUsage(w http.ResponseWriter, r *http.Request) {
	wsID := workspaceIDFromURL(r, "id")
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	quota := h.appToolCallQuota()
	usage, err := quota.Usage(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app call usage")
		return
	}
	resp := AppCallUsageResponse{Action: string(usage.Action), Used: usage.Used, Limit: usage.Limit, Reached: usage.Reached}
	if usage.PeriodStart != nil {
		v := usage.PeriodStart.UTC().Format(time.RFC3339)
		resp.PeriodStart = &v
	}
	if usage.ResetAt != nil {
		v := usage.ResetAt.UTC().Format(time.RFC3339)
		resp.ResetAt = &v
	}
	if usage.PeriodStart != nil && (member.Role == "owner" || member.Role == "admin") {
		members, err := quota.MemberUsage(r.Context(), workspaceID, *usage.PeriodStart)
		if err != nil {
			// The breakdown is secondary; the total still renders.
			slog.WarnContext(r.Context(), "app call usage by member failed", "workspace_id", wsID, "error", err)
		}
		resp.Members = make([]AppCallMemberUsageEntry, 0, len(members))
		for _, m := range members {
			entry := AppCallMemberUsageEntry{Calls: m.Calls}
			if m.UserID.Valid {
				id := util.UUIDToString(m.UserID)
				entry.UserID = &id
			}
			resp.Members = append(resp.Members, entry)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
