package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// processChildDoneEvents runs the child-done system rule for the transitions
// recorded on these parents. The status write records each transition in its
// own transaction (migration 556); this claims them, evaluates the stage
// barrier against the current sibling state, and marks them processed. A
// failure leaves the claim to expire so SweepChildDone retries it.
func (h *Handler) processChildDoneEvents(ctx context.Context, parentIDs ...pgtype.UUID) error {
	var errs []error
	seen := map[pgtype.UUID]bool{}
	for _, parentID := range parentIDs {
		if !parentID.Valid || seen[parentID] {
			continue
		}
		seen[parentID] = true
		if err := h.processParentChildDone(ctx, parentID); err != nil {
			slog.Warn("child done: processing failed; will retry", "error", err, "parent_id", uuidToString(parentID))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *Handler) processParentChildDone(ctx context.Context, parentID pgtype.UUID) error {
	events, err := h.Queries.ClaimChildDoneEvents(ctx, parentID)
	if err != nil || len(events) == 0 {
		return err
	}
	ids := make([]pgtype.UUID, 0, len(events))
	completed := make([]db.Issue, 0, len(events))
	children := map[pgtype.UUID]bool{}
	for _, e := range events {
		ids = append(ids, e.ID)
		if children[e.ChildID] {
			continue
		}
		children[e.ChildID] = true
		child, err := h.Queries.GetIssue(ctx, e.ChildID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load child: %w", err)
		}
		// A child moved to another parent no longer closes this one's stage.
		if child.ParentIssueID != parentID {
			continue
		}
		completed = append(completed, child)
	}
	if err = h.notifyParentsOfBatchChildDone(ctx, completed); err != nil {
		return err
	}
	return h.Queries.FinishChildDoneEvents(ctx, ids)
}

// SweepChildDone retries transitions that were recorded but not processed,
// and drops processed rows after a week.
func (h *Handler) SweepChildDone(ctx context.Context) error {
	parents, err := h.Queries.ListStaleChildDoneParents(ctx)
	if err != nil {
		return err
	}
	err = h.processChildDoneEvents(ctx, parents...)
	cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, cleanupErr := h.Queries.DeleteProcessedChildDoneEvents(cleanupCtx, pgtype.Timestamptz{Time: time.Now().Add(-7 * 24 * time.Hour), Valid: true}); cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("expire child-done events: %w", cleanupErr))
	}
	return err
}
