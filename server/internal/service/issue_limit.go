package service

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueLimitReachedError carries Cloud's effective limit and policy revision
// to transport adapters. It never includes a plan name or subscription state.
type IssueLimitReachedError struct {
	Limit          int64
	PolicyRevision int64
}

func (e *IssueLimitReachedError) Error() string {
	return fmt.Sprintf("workspace issue limit reached: limit %d", e.Limit)
}

// IssueCountPolicy is the validated mechanical instruction received from
// Cloud. Resolve it before opening a database transaction so a network call
// never holds product-database locks.
type IssueCountPolicy struct {
	Action         entitlement.Action
	Limit          int64
	PolicyRevision int64
}

func ResolveIssueCountPolicy(ctx context.Context, provider entitlement.Provider, workspaceID pgtype.UUID) IssueCountPolicy {
	if provider == nil || !workspaceID.Valid {
		return IssueCountPolicy{Action: entitlement.ActionOff}
	}
	decision := provider.Gate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateIssueCount)
	policy := IssueCountPolicy{
		Action:         decision.Gate.Action,
		PolicyRevision: decision.PolicyRevision,
	}
	if decision.Gate.Limit != nil {
		policy.Limit = int64(*decision.Gate.Limit)
	}
	switch policy.Action {
	case entitlement.ActionOff:
		policy.Limit = 0
	case entitlement.ActionEnforce:
		if policy.Limit > 0 {
			return policy
		}
		policy.Action = entitlement.ActionOff
		policy.Limit = 0
	default:
		policy.Action = entitlement.ActionOff
		policy.Limit = 0
	}
	return policy
}

// CheckIssueCreateCapacity performs a read-only admission preflight for flows
// that do work before the issue transaction starts, such as agent quick create.
// It is deliberately not a reservation: callers must still use
// AllocateIssueNumber in the final create transaction to serialize concurrent
// admissions against the workspace row.
func CheckIssueCreateCapacity(ctx context.Context, q *db.Queries, provider entitlement.Provider, workspaceID pgtype.UUID) error {
	policy := ResolveIssueCountPolicy(ctx, provider, workspaceID)
	if policy.Action != entitlement.ActionEnforce {
		return nil
	}
	used, err := CountIssueUsage(ctx, q, workspaceID, policy)
	if err != nil {
		return err
	}
	if used >= policy.Limit {
		return &IssueLimitReachedError{Limit: policy.Limit, PolicyRevision: policy.PolicyRevision}
	}
	return nil
}

// AllocateIssueNumber serializes creates on the workspace row, then checks the
// current number of issue rows inside the same transaction. The caller must
// roll the transaction back on IssueLimitReachedError; that also rolls back
// the counter increment. Deleting an issue frees capacity because
// issue_counter is deliberately not used as quota usage.
func AllocateIssueNumber(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, policy IssueCountPolicy) (int32, error) {
	number, err := q.IncrementIssueCounter(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	if policy.Action != entitlement.ActionEnforce {
		return number, nil
	}
	used, err := q.CountIssuesUpTo(ctx, db.CountIssuesUpToParams{
		WorkspaceID: workspaceID,
		Limit:       policy.Limit,
	})
	if err != nil {
		return 0, err
	}
	if used >= policy.Limit {
		return 0, &IssueLimitReachedError{Limit: policy.Limit, PolicyRevision: policy.PolicyRevision}
	}
	return number, nil
}

// CountIssueUsage performs a bounded read for a limited policy. The returned
// value is exact while below the limit and capped at the limit once the
// workspace is full.
func CountIssueUsage(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, policy IssueCountPolicy) (int64, error) {
	if policy.Action != entitlement.ActionEnforce {
		return 0, nil
	}
	sampleLimit := policy.Limit
	if sampleLimit < math.MaxInt64 {
		sampleLimit++
	}
	sampled, err := q.CountIssuesUpTo(ctx, db.CountIssuesUpToParams{
		WorkspaceID: workspaceID,
		Limit:       sampleLimit,
	})
	if err != nil {
		return 0, err
	}
	if sampled > policy.Limit {
		return policy.Limit, nil
	}
	return sampled, nil
}
