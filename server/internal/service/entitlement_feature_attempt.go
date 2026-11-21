package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FeatureAttemptOutcome is what happened to one attempt to add to a paid
// feature that was not off.
type FeatureAttemptOutcome string

const (
	// FeatureAttemptObserved was admitted under a configured observe.
	FeatureAttemptObserved FeatureAttemptOutcome = "observed"
	// FeatureAttemptDegraded was admitted only because a stale cached enforce
	// had been downgraded to observe during a Cloud outage.
	FeatureAttemptDegraded FeatureAttemptOutcome = "degraded"
	// FeatureAttemptEnforced was rejected.
	FeatureAttemptEnforced FeatureAttemptOutcome = "enforced"
)

// FeatureAttemptOutcomeFor maps a decision to the outcome of an attempt made
// under it. ok=false for off: an unrestricted feature records nothing.
func FeatureAttemptOutcomeFor(decision entitlement.FeatureDecision) (FeatureAttemptOutcome, bool) {
	switch decision.Action {
	case entitlement.ActionEnforce:
		return FeatureAttemptEnforced, true
	case entitlement.ActionObserve:
		if decision.Downgraded {
			return FeatureAttemptDegraded, true
		}
		return FeatureAttemptObserved, true
	default:
		return "", false
	}
}

// FeatureAttempt is one attempt to add to a paid feature's configuration.
type FeatureAttempt struct {
	WorkspaceID pgtype.UUID
	Feature     entitlement.FeatureName
	// Operation names what was attempted, e.g. "create" or "add_members".
	Operation string
	Decision  entitlement.FeatureDecision
}

// FeatureAttemptDay is one workspace's attempts on one feature in one UTC day.
type FeatureAttemptDay struct {
	WorkspaceID             uuid.UUID
	Feature                 entitlement.FeatureName
	Day                     time.Time
	ObservedCount           int64
	DegradedCount           int64
	EnforcedCount           int64
	LastOperation           string
	LastAction              entitlement.Action
	LastReason              entitlement.Reason
	LastPolicyRevision      int64
	LastSubscriptionVersion int64
	FirstAttemptAt          time.Time
	LastAttemptAt           time.Time
}

// EntitlementFeatureAttempts records and reads attempts to add to restricted
// paid features. Only features that are observing or enforcing reach it, so a
// self-hosted deployment never touches the table.
type EntitlementFeatureAttempts struct {
	Queries *db.Queries
	// RecordTimeout bounds one Record; zero means defaultFeatureAttemptRecordTimeout.
	RecordTimeout time.Duration
}

// Recording runs before the caller writes its response, so it gets its own
// short deadline: a contended row lock or a slow database drops the record
// instead of holding a request whose outcome is already decided.
const defaultFeatureAttemptRecordTimeout = time.Second

// Record stores one attempt. It is best-effort: callers record an admitted
// attempt after their write commits and a rejected one after authorization,
// and a failure here never changes the operation's result. It is detached from
// the caller's cancellation, so a client that disconnects after a committed
// write still leaves a record, and bounded by RecordTimeout, so it can never
// hold the response.
func (a EntitlementFeatureAttempts) Record(ctx context.Context, attempt FeatureAttempt) {
	outcome, ok := FeatureAttemptOutcomeFor(attempt.Decision)
	if !ok || a.Queries == nil {
		return
	}
	timeout := a.RecordTimeout
	if timeout <= 0 {
		timeout = defaultFeatureAttemptRecordTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	_, err := a.Queries.RecordEntitlementFeatureAttempt(ctx, db.RecordEntitlementFeatureAttemptParams{
		WorkspaceID:         attempt.WorkspaceID,
		Feature:             string(attempt.Feature),
		Outcome:             string(outcome),
		Operation:           attempt.Operation,
		Action:              string(attempt.Decision.Action),
		Reason:              string(attempt.Decision.Reason),
		PolicyRevision:      attempt.Decision.PolicyRevision,
		SubscriptionVersion: attempt.Decision.SubscriptionVersion,
	})
	if err != nil {
		slog.Warn("entitlement feature attempt was not recorded; the operation's result is unchanged",
			"workspace_id", util.UUIDToString(attempt.WorkspaceID),
			"feature", string(attempt.Feature),
			"outcome", string(outcome),
			"error", err,
		)
	}
}

// ForWorkspace lists every day on which a workspace tried to add to a
// restricted feature, newest first.
func (a EntitlementFeatureAttempts) ForWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]FeatureAttemptDay, error) {
	rows, err := a.Queries.ListWorkspaceEntitlementFeatureAttempts(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace feature attempts: %w", err)
	}
	return featureAttemptDaysFromRows(rows), nil
}

// Since lists, per workspace and day, the attempts on one feature from a UTC
// day on.
func (a EntitlementFeatureAttempts) Since(ctx context.Context, feature entitlement.FeatureName, day time.Time) ([]FeatureAttemptDay, error) {
	rows, err := a.Queries.ListEntitlementFeatureAttemptsSince(ctx, db.ListEntitlementFeatureAttemptsSinceParams{
		Feature:  string(feature),
		SinceDay: pgtype.Date{Time: day.UTC(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list feature attempts since day: %w", err)
	}
	return featureAttemptDaysFromRows(rows), nil
}

func featureAttemptDaysFromRows(rows []db.EntitlementFeatureAttempt) []FeatureAttemptDay {
	out := make([]FeatureAttemptDay, 0, len(rows))
	for _, row := range rows {
		out = append(out, FeatureAttemptDay{
			WorkspaceID:             uuid.UUID(row.WorkspaceID.Bytes),
			Feature:                 entitlement.FeatureName(row.Feature),
			Day:                     row.Day.Time.UTC(),
			ObservedCount:           row.ObservedCount,
			DegradedCount:           row.DegradedCount,
			EnforcedCount:           row.EnforcedCount,
			LastOperation:           row.LastOperation,
			LastAction:              entitlement.Action(row.LastAction),
			LastReason:              entitlement.Reason(row.LastReason),
			LastPolicyRevision:      row.LastPolicyRevision,
			LastSubscriptionVersion: row.LastSubscriptionVersion,
			FirstAttemptAt:          row.FirstAttemptAt.Time.UTC(),
			LastAttemptAt:           row.LastAttemptAt.Time.UTC(),
		})
	}
	return out
}
