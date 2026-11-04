package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PolicyReviewPeriod is the period of the weekly review (spec 4.4).
const PolicyReviewPeriod = 7 * 24 * time.Hour

// policyReviewDetailKey tags the review_published event that carries the policy
// section of a weekly review, so the event can be found again.
const policyReviewDetailKey = "policy_reversals"

// RecordWeeklyPolicyReview writes the policy section of the weekly review for the
// week ending at now: every auto-approval reversed on review in that week, with
// the policy that approved it, as one review_published governance event. A week
// with no reversal writes nothing, and a week already recorded is not recorded
// again. It returns how many reversals the section lists.
func (s *ApprovalPolicyService) RecordWeeklyPolicyReview(ctx context.Context, ws pgtype.UUID, now time.Time) (int, error) {
	periodEnd := now.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	recorded, err := s.Queries.CountPolicyReviewEvents(ctx, db.CountPolicyReviewEventsParams{WorkspaceID: ws, PeriodEnd: periodEnd})
	if err != nil {
		return 0, fmt.Errorf("count weekly policy reviews: %w", err)
	}
	if recorded > 0 {
		return 0, nil
	}
	reversed, err := s.ListReversedAutoApprovals(ctx, ws, now.Add(-PolicyReviewPeriod))
	if err != nil {
		return 0, fmt.Errorf("list reversed auto-approvals: %w", err)
	}
	if len(reversed) == 0 {
		return 0, nil
	}
	items := make([]map[string]any, 0, len(reversed))
	for _, r := range reversed {
		items = append(items, map[string]any{
			"proposal_id":   util.UUIDToString(r.ProposalID),
			"policy_id":     util.UUIDToString(r.PolicyID),
			"title":         r.Title,
			"reversal_note": r.ReversalNote,
		})
	}
	err = NewGovernanceLog().Record(ctx, s.Queries, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindReviewPublished,
		ActorType:   GovernanceActorSystem,
		SubjectType: "workspace",
		SubjectID:   ws,
		Summary: fmt.Sprintf("Weekly review: %d auto-approval(s) were reversed on review this week. Their policies are flagged on the Autonomy and Policies lists.",
			len(reversed)),
		Details: map[string]any{
			"review":     policyReviewDetailKey,
			"period_end": periodEnd,
			"reversals":  items,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("record weekly policy review: %w", err)
	}
	return len(reversed), nil
}

// RunWeeklyPolicyReview is the work of the weekly policy job: the policy section
// of every workspace's weekly review. A failure in one workspace does not stop
// the others; the error joins what failed.
func (s *ApprovalPolicyService) RunWeeklyPolicyReview(ctx context.Context, now time.Time) (int, error) {
	wsIDs, err := s.Queries.ListAllWorkspaceIDsForGovernance(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	recorded := 0
	var errs []error
	for _, ws := range wsIDs {
		n, err := s.RecordWeeklyPolicyReview(ctx, ws, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("weekly policy review for workspace %s: %w", util.UUIDToString(ws), err))
			continue
		}
		recorded += n
	}
	return recorded, errors.Join(errs...)
}
