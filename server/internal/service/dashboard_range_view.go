package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DashboardRangeOutcome is how a dashboard read related to the workspace's
// history limit (entitlement.GateDashboardHistoryDays).
type DashboardRangeOutcome string

const (
	// DashboardRangeWithin read no further back than the limit, or had none.
	DashboardRangeWithin DashboardRangeOutcome = "within"
	// DashboardRangeObserved read past the limit under a configured observe.
	DashboardRangeObserved DashboardRangeOutcome = "observed"
	// DashboardRangeDegraded read past the limit only because a stale cached
	// enforce had been downgraded to observe during a Cloud outage.
	DashboardRangeDegraded DashboardRangeOutcome = "degraded"
	// DashboardRangeTruncated asked past the limit and was cut to it.
	DashboardRangeTruncated DashboardRangeOutcome = "truncated"
)

// DashboardRangeOutcomeFor classifies a read of requestedDays under decision.
func DashboardRangeOutcomeFor(decision entitlement.Decision, requestedDays int) DashboardRangeOutcome {
	limit := decision.Gate.Limit
	if limit == nil || requestedDays <= *limit {
		return DashboardRangeWithin
	}
	switch decision.Gate.Action {
	case entitlement.ActionEnforce:
		return DashboardRangeTruncated
	case entitlement.ActionObserve:
		if decision.Downgraded {
			return DashboardRangeDegraded
		}
		return DashboardRangeObserved
	default:
		return DashboardRangeWithin
	}
}

// DashboardRangeView is one dashboard read: the range the viewer asked for,
// the range the response covered, and the limit decision it was served under.
type DashboardRangeView struct {
	WorkspaceID   pgtype.UUID
	RequestedDays int
	ReturnedDays  int
	Decision      entitlement.Decision
}

// DashboardRangeViews records dashboard reads per workspace, UTC day and
// range, so the reach of the history limit can be measured per workspace.
type DashboardRangeViews struct {
	Queries *db.Queries
	// RecordTimeout bounds one Record; zero means defaultDashboardRangeViewRecordTimeout.
	RecordTimeout time.Duration
}

const defaultDashboardRangeViewRecordTimeout = 500 * time.Millisecond

// Record stores one read. It is best-effort and never part of the response:
// callers run it off the request path, it is detached from the request's
// cancellation, and a failure or timeout only drops the record.
func (v DashboardRangeViews) Record(ctx context.Context, view DashboardRangeView) {
	if v.Queries == nil || view.RequestedDays <= 0 || view.ReturnedDays <= 0 {
		return
	}
	timeout := v.RecordTimeout
	if timeout <= 0 {
		timeout = defaultDashboardRangeViewRecordTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	outcome := DashboardRangeOutcomeFor(view.Decision, view.RequestedDays)
	var limit pgtype.Int4
	if view.Decision.Gate.Action != entitlement.ActionOff && view.Decision.Gate.Limit != nil {
		limit = pgtype.Int4{Int32: int32(*view.Decision.Gate.Limit), Valid: true}
	}
	err := v.Queries.RecordDashboardRangeView(ctx, db.RecordDashboardRangeViewParams{
		WorkspaceID:   view.WorkspaceID,
		RequestedDays: int32(view.RequestedDays),
		ReturnedDays:  int32(view.ReturnedDays),
		Outcome:       string(outcome),
		LimitDays:     limit,
		Action:        string(view.Decision.Gate.Action),
		Reason:        string(view.Decision.Reason),
	})
	if err != nil {
		slog.Warn("dashboard range view was not recorded; the response is unchanged",
			"workspace_id", util.UUIDToString(view.WorkspaceID),
			"outcome", string(outcome),
			"error", err,
		)
	}
}
