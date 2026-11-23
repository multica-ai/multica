package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/service"
)

// Dashboard history limit (MUL-9490).
//
// Cloud sends dashboard_history_days per workspace: how many calendar days
// back the dashboard may read. Only enforce limits anything. Self-hosted
// (h.Entitlements nil), off and observe read as far back as the product
// allows, so an observed rollout changes nothing users can see.
//
// Under enforce every dashboard endpoint is cut to the most recent N calendar
// days in the viewing time zone, whatever `days` asked for: the extra day of
// headroom the date-bucketed series normally carry, and the days a weekly
// chart fetches to fill its leftmost week, are cut too, so no response holds
// data from before the plan's range. The response carries the limit in
// HeaderDashboardHistoryLimit so a page open across a plan change notices it.

// HeaderDashboardHistoryLimit carries the enforced limit, in days, on every
// dashboard response while one applies. Absent means no limit.
const HeaderDashboardHistoryLimit = "X-Dashboard-History-Limit-Days"

// dashboardMaxDaysParam is the largest `days` the dashboard endpoints accept:
// 53 weeks, because a 365-day weekly chart rounds its fetch up to whole weeks
// (371 days). It bounds the request, not what a plan may read.
const dashboardMaxDaysParam = 371

// dashboardHistoryDecision is the workspace's dashboard_history_days decision.
// ok=false when no Cloud is connected; the dashboard is then unlimited and
// reads are not recorded.
func (h *Handler) dashboardHistoryDecision(ctx context.Context, workspaceID string) (entitlement.Decision, bool) {
	if h.Entitlements == nil {
		return entitlement.Decision{}, false
	}
	id, err := uuid.Parse(workspaceID)
	if err != nil {
		return entitlement.Decision{}, false
	}
	return h.Entitlements.Gate(ctx, id, entitlement.GateDashboardHistoryDays), true
}

// enforcedDashboardHistoryDays is the limit a decision enforces, or 0.
func enforcedDashboardHistoryDays(decision entitlement.Decision) int {
	if decision.Gate.Action != entitlement.ActionEnforce || decision.Gate.Limit == nil || *decision.Gate.Limit < 1 {
		return 0
	}
	return *decision.Gate.Limit
}

// dashboardWindow is the resolved `days` window of one dashboard request.
type dashboardWindow struct {
	since pgtype.Timestamptz
	// requestedDays is the parsed `days`.
	requestedDays int
	// truncated is set when the enforced limit moved the cutoff forward.
	truncated bool
	// limitDays is the enforced limit, 0 when none applies.
	limitDays int
	decision  entitlement.Decision
	// recordable is set when Cloud is connected, so the read may be recorded.
	recordable bool
}

// dashboardCutoff resolves the `days` cutoff of a dashboard request in tz and
// applies the workspace's history limit to it. trimDays is 0 for the
// date-bucketed series (N+1 days of headroom) and 1 for the per-agent rollups
// (exactly N). It sets HeaderDashboardHistoryLimit when a limit is enforced,
// so call it before writing the response.
func (h *Handler) dashboardCutoff(w http.ResponseWriter, r *http.Request, workspaceID, tz string, trimDays int) dashboardWindow {
	days := parseDaysParam(r, 30, dashboardMaxDaysParam)
	loc := loadViewingLocation(tz)
	now := dayWindowNow()
	since := sinceFromDays(now, days-trimDays, loc)
	window := dashboardWindow{requestedDays: days}

	if decision, ok := h.dashboardHistoryDecision(r.Context(), workspaceID); ok {
		window.decision = decision
		window.recordable = true
		if limit := enforcedDashboardHistoryDays(decision); limit > 0 {
			window.limitDays = limit
			// The first of the N most recent calendar days, today included.
			if floor := sinceFromDays(now, limit-1, loc); since.Before(floor) {
				since = floor
				window.truncated = true
			}
			w.Header().Set(HeaderDashboardHistoryLimit, strconv.Itoa(limit))
		}
	}
	window.since = pgtype.Timestamptz{Time: since, Valid: true}
	return window
}

// returnedDays is how many calendar days an exact-N window covered.
func (d dashboardWindow) returnedDays() int {
	if d.limitDays > 0 && d.requestedDays > d.limitDays {
		return d.limitDays
	}
	return d.requestedDays
}

// recordDashboardRangeView records a successful exact-N dashboard read off the
// request path. It never delays or changes the response.
func (h *Handler) recordDashboardRangeView(ctx context.Context, workspaceID pgtype.UUID, window dashboardWindow) {
	if !window.recordable || h.Queries == nil {
		return
	}
	view := service.DashboardRangeView{
		WorkspaceID:   workspaceID,
		RequestedDays: window.requestedDays,
		ReturnedDays:  window.returnedDays(),
		Decision:      window.decision,
	}
	views := service.DashboardRangeViews{Queries: h.Queries}
	detached := context.WithoutCancel(ctx)
	go views.Record(detached, view)
}

// dashboardHistoryLimitForClient is the limit GET /features reports: the
// enforced day count, or nil when nothing is enforced.
func (h *Handler) dashboardHistoryLimitForClient(ctx context.Context, workspaceID string) *int {
	decision, ok := h.dashboardHistoryDecision(ctx, workspaceID)
	if !ok {
		return nil
	}
	if limit := enforcedDashboardHistoryDays(decision); limit > 0 {
		return &limit
	}
	return nil
}
