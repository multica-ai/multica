package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	childDoneWorkerPollInterval = time.Second
	childDoneWorkerConcurrency  = 2
	childDoneRetryMaxDelay      = time.Minute
)

// ChildDoneDispatchWorker drains system-comment outbox rows. Postgres leases
// make the queue restart-safe and distribute work across server replicas; the
// in-memory notification only lowers local latency.
type ChildDoneDispatchWorker struct {
	h      *Handler
	notify chan struct{}
	done   chan struct{}
}

func NewChildDoneDispatchWorker(h *Handler) *ChildDoneDispatchWorker {
	return &ChildDoneDispatchWorker{
		h:      h,
		notify: make(chan struct{}, childDoneWorkerConcurrency),
		done:   make(chan struct{}),
	}
}

func (w *ChildDoneDispatchWorker) Notify() {
	if w == nil {
		return
	}
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

func (w *ChildDoneDispatchWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	defer close(w.done)
	if w.h == nil || w.h.Queries == nil {
		return
	}
	var workers sync.WaitGroup
	workers.Add(childDoneWorkerConcurrency)
	for range childDoneWorkerConcurrency {
		go func() {
			defer workers.Done()
			w.runLoop(ctx)
		}()
	}
	workers.Wait()
}

func (w *ChildDoneDispatchWorker) runLoop(ctx context.Context) {
	ticker := time.NewTicker(childDoneWorkerPollInterval)
	defer ticker.Stop()
	for {
		transitionWorked, err := w.ProcessNextTransitionGroup(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("child done worker: process transition", "error", err)
		}
		dispatchWorked, err := w.ProcessNext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("child done worker: process dispatch", "error", err)
		}
		if transitionWorked || dispatchWorked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-w.notify:
		case <-ticker.C:
		}
	}
}

func (w *ChildDoneDispatchWorker) ProcessNextTransitionGroup(ctx context.Context) (bool, error) {
	transitions, err := w.h.Queries.ClaimNextChildDoneTransitionGroup(ctx)
	if err != nil {
		return false, fmt.Errorf("claim next child-done transition group: %w", err)
	}
	if len(transitions) == 0 {
		return false, nil
	}
	return true, w.processClaimedTransitions(ctx, transitions)
}

func (w *ChildDoneDispatchWorker) ProcessTransitionGroup(ctx context.Context, groupID pgtype.UUID) (bool, error) {
	transitions, err := w.h.Queries.ClaimChildDoneTransitionGroup(ctx, groupID)
	if err != nil {
		return false, fmt.Errorf("claim child-done transition group: %w", err)
	}
	if len(transitions) == 0 {
		return false, nil
	}
	return true, w.processClaimedTransitions(ctx, transitions)
}

func (w *ChildDoneDispatchWorker) processClaimedTransitions(ctx context.Context, transitions []db.ChildDoneTransition) error {
	groupID := transitions[0].GroupID
	leaseToken := transitions[0].LeaseToken
	completed := make([]db.Issue, 0, len(transitions))
	for _, transition := range transitions {
		issue, err := w.h.Queries.GetIssue(ctx, transition.ChildIssueID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return w.retryTransitions(ctx, transitions, fmt.Errorf("load transitioned child: %w", err))
		}
		// Routing and barrier identity belong to the committed transition, not
		// to a later edit that happened before a recovering worker caught up.
		issue.ParentIssueID = transition.ParentIssueID
		issue.Status = transition.TerminalStatus
		issue.Stage = transition.Stage
		issue.UpdatedAt = transition.TransitionAt
		completed = append(completed, issue)
	}
	sort.Slice(completed, func(i, j int) bool {
		if completed[i].Stage.Valid != completed[j].Stage.Valid {
			return !completed[i].Stage.Valid
		}
		if completed[i].Stage.Int32 != completed[j].Stage.Int32 {
			return completed[i].Stage.Int32 < completed[j].Stage.Int32
		}
		if !completed[i].UpdatedAt.Time.Equal(completed[j].UpdatedAt.Time) {
			return completed[i].UpdatedAt.Time.Before(completed[j].UpdatedAt.Time)
		}
		return uuidToString(completed[i].ID) < uuidToString(completed[j].ID)
	})
	if err := w.h.notifyParentsOfBatchChildDone(ctx, completed); err != nil {
		return w.retryTransitions(ctx, transitions, err)
	}
	_, err := w.h.Queries.CompleteClaimedChildDoneTransitionGroup(ctx, db.CompleteClaimedChildDoneTransitionGroupParams{
		GroupID:    groupID,
		LeaseToken: leaseToken,
	})
	if err != nil {
		return fmt.Errorf("complete child-done transition group: %w", err)
	}
	slog.Info("child done worker: transition group completed",
		"group_id", uuidToString(groupID),
		"transitions", len(transitions))
	return nil
}

func (w *ChildDoneDispatchWorker) retryTransitions(ctx context.Context, transitions []db.ChildDoneTransition, cause error) error {
	attempt := transitions[0].Attempts + 1
	delay := time.Second << min(attempt-1, 6)
	if delay > childDoneRetryMaxDelay {
		delay = childDoneRetryMaxDelay
	}
	_, err := w.h.Queries.RetryClaimedChildDoneTransitionGroup(ctx, db.RetryClaimedChildDoneTransitionGroupParams{
		AvailableAt: pgtype.Timestamptz{Time: time.Now().Add(delay), Valid: true},
		Error:       pgtype.Text{String: cause.Error(), Valid: true},
		GroupID:     transitions[0].GroupID,
		LeaseToken:  transitions[0].LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("reschedule child-done transition group after %v: %w", cause, err)
	}
	slog.Warn("child done worker: transition group rescheduled",
		"error", cause,
		"group_id", uuidToString(transitions[0].GroupID),
		"attempt", attempt,
		"retry_at", time.Now().Add(delay))
	return cause
}

func (w *ChildDoneDispatchWorker) WaitWithTimeout(timeout time.Duration) bool {
	if w == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return true
	case <-timer.C:
		return false
	}
}

func (w *ChildDoneDispatchWorker) ProcessNext(ctx context.Context) (bool, error) {
	dispatch, err := w.h.Queries.ClaimNextChildDoneDispatch(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim next child-done dispatch: %w", err)
	}
	return true, w.processClaimed(ctx, dispatch)
}

func (w *ChildDoneDispatchWorker) ProcessID(ctx context.Context, id pgtype.UUID) (bool, error) {
	dispatch, err := w.h.Queries.ClaimChildDoneDispatchByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim child-done dispatch: %w", err)
	}
	return true, w.processClaimed(ctx, dispatch)
}

func (w *ChildDoneDispatchWorker) processClaimed(ctx context.Context, dispatch db.Comment) error {
	slog.Debug("child done worker: dispatch claimed",
		"comment_id", uuidToString(dispatch.ID),
		"parent_id", uuidToString(dispatch.IssueID),
		"attempt", dispatch.ChildDoneDispatchAttempts+1)
	parent, err := w.h.Queries.GetIssue(ctx, dispatch.IssueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return w.complete(ctx, dispatch, "skipped")
		}
		return w.retry(ctx, dispatch, fmt.Errorf("load parent: %w", err))
	}
	if parent.Status == "done" || parent.Status == "cancelled" || parent.Status == "backlog" {
		return w.complete(ctx, dispatch, "skipped")
	}

	target := childDoneDispatchTarget{Issue: parent}
	if parent.AssigneeType.Valid && parent.AssigneeID.Valid {
		if parent.AssigneeType.String == "member" {
			return w.complete(ctx, dispatch, "skipped")
		}
	} else {
		if !dispatch.ChildDoneOriginTaskID.Valid {
			updated, retargetErr := w.retarget(ctx, dispatch, target)
			if retargetErr != nil {
				return w.retry(ctx, dispatch, retargetErr)
			}
			return w.complete(ctx, updated, "skipped")
		}
		if !dispatch.ChildDoneTargetType.Valid || !dispatch.ChildDoneTargetID.Valid {
			return w.complete(ctx, dispatch, "skipped")
		}
		target.Issue.AssigneeType = dispatch.ChildDoneTargetType
		target.Issue.AssigneeID = dispatch.ChildDoneTargetID
		origin, loadErr := w.h.Queries.GetAgentTask(ctx, dispatch.ChildDoneOriginTaskID)
		if loadErr != nil {
			if errors.Is(loadErr, pgx.ErrNoRows) {
				return w.complete(ctx, dispatch, "skipped")
			}
			return w.retry(ctx, dispatch, fmt.Errorf("load origin task: %w", loadErr))
		}
		target.OriginTask = &origin
	}

	updated, err := w.retarget(ctx, dispatch, target)
	if err != nil {
		return w.retry(ctx, dispatch, err)
	}
	dispatch = updated

	if err := w.h.dispatchParentAssigneeTrigger(ctx, target, dispatch); err != nil {
		if errors.Is(err, errChildDoneDispatchSkipped) {
			return w.complete(ctx, dispatch, "skipped")
		}
		if errors.Is(err, service.ErrIssueAssignedForTask) {
			return w.retryAfter(ctx, dispatch, err, time.Second)
		}
		return w.retry(ctx, dispatch, err)
	}
	return w.complete(ctx, dispatch, "dispatched")
}

func (w *ChildDoneDispatchWorker) retarget(ctx context.Context, dispatch db.Comment, target childDoneDispatchTarget) (db.Comment, error) {
	targetType := target.Issue.AssigneeType
	targetID := target.Issue.AssigneeID
	var originTaskID pgtype.UUID
	if target.OriginTask != nil {
		originTaskID = target.OriginTask.ID
	}
	content := w.h.buildParentAssigneeMention(ctx, target.Issue) + stripChildDoneRoutingMention(dispatch.Content)
	if content == dispatch.Content &&
		targetType == dispatch.ChildDoneTargetType &&
		targetID == dispatch.ChildDoneTargetID &&
		originTaskID == dispatch.ChildDoneOriginTaskID {
		return dispatch, nil
	}
	updated, err := w.h.Queries.RetargetClaimedChildDoneDispatch(ctx, db.RetargetClaimedChildDoneDispatchParams{
		Content:               content,
		ChildDoneTargetType:   targetType,
		ChildDoneTargetID:     targetID,
		ChildDoneOriginTaskID: originTaskID,
		ID:                    dispatch.ID,
		ChildDoneLeaseToken:   dispatch.ChildDoneLeaseToken,
	})
	if err != nil {
		return db.Comment{}, fmt.Errorf("retarget dispatch: %w", err)
	}
	w.h.publish(protocol.EventCommentUpdated, uuidToString(updated.WorkspaceID), "system", "", map[string]any{
		"comment": commentToResponse(updated, nil, nil),
	})
	return updated, nil
}

func stripChildDoneRoutingMention(content string) string {
	if !strings.HasPrefix(content, "[") {
		return content
	}
	end := strings.Index(content, ") ")
	if end < 0 || !strings.Contains(content[:end], "](mention://") {
		return content
	}
	return content[end+2:]
}

func (w *ChildDoneDispatchWorker) complete(ctx context.Context, dispatch db.Comment, status string) error {
	_, err := w.h.Queries.CompleteClaimedChildDoneDispatch(ctx, db.CompleteClaimedChildDoneDispatchParams{
		ChildDoneDispatchStatus: pgtype.Text{String: status, Valid: true},
		ID:                      dispatch.ID,
		ChildDoneLeaseToken:     dispatch.ChildDoneLeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err == nil {
		slog.Info("child done worker: dispatch completed",
			"comment_id", uuidToString(dispatch.ID),
			"parent_id", uuidToString(dispatch.IssueID),
			"status", status,
			"attempt", dispatch.ChildDoneDispatchAttempts+1)
	}
	return err
}

func (w *ChildDoneDispatchWorker) retry(ctx context.Context, dispatch db.Comment, cause error) error {
	attempt := dispatch.ChildDoneDispatchAttempts + 1
	delay := time.Second << min(attempt-1, 6)
	if delay > childDoneRetryMaxDelay {
		delay = childDoneRetryMaxDelay
	}
	return w.retryAfter(ctx, dispatch, cause, delay)
}

func (w *ChildDoneDispatchWorker) retryAfter(ctx context.Context, dispatch db.Comment, cause error, delay time.Duration) error {
	_, err := w.h.Queries.RetryClaimedChildDoneDispatch(ctx, db.RetryClaimedChildDoneDispatchParams{
		AvailableAt:            pgtype.Timestamptz{Time: time.Now().Add(delay), Valid: true},
		ChildDoneDispatchError: pgtype.Text{String: cause.Error(), Valid: true},
		ID:                     dispatch.ID,
		ChildDoneLeaseToken:    dispatch.ChildDoneLeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reschedule child-done dispatch after %v: %w", cause, err)
	}
	slog.Warn("child done worker: dispatch rescheduled",
		"error", cause,
		"comment_id", uuidToString(dispatch.ID),
		"parent_id", uuidToString(dispatch.IssueID),
		"attempt", dispatch.ChildDoneDispatchAttempts+1,
		"retry_at", time.Now().Add(delay))
	return cause
}
