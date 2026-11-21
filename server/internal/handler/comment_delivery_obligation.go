package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type commentDeliverySource struct {
	Comment          db.Comment
	ActorType        string
	ActorID          string
	OriginatorUserID string
}

func (h *Handler) releaseCommentDeliveryObligation(ctx context.Context, obligation db.CommentDeliveryObligation) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := h.Queries.ReleaseCommentDeliveryObligation(ctx, db.ReleaseCommentDeliveryObligationParams{
		WorkspaceID: obligation.WorkspaceID, CommentID: obligation.CommentID,
		AgentID: obligation.AgentID, CommentRevision: obligation.CommentRevision, LeaseUntil: obligation.LeaseUntil,
	}); err != nil {
		slog.Warn("release comment input lease failed", "comment_id", uuidToString(obligation.CommentID), "error", err)
	}
}

// DrainCommentDeliveryObligations retries accepted input independently of the
// task that originally blocked it. Planned input remains pending until a receipt
// or an explicit cancellation settles it.
func (h *Handler) DrainCommentDeliveryObligations(ctx context.Context, maxPerTick int32) error {
	if maxPerTick <= 0 {
		return nil
	}
	if maxPerTick > 50 {
		maxPerTick = 50
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	pending, err := h.Queries.ClaimCommentDeliveryObligations(ctx, maxPerTick)
	if err != nil {
		return fmt.Errorf("claim comment obligations: %w", err)
	}
	var failures []error
	for _, obligation := range pending {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		attempt, stop := context.WithTimeout(ctx, 5*time.Second)
		err := h.drainCommentDeliveryObligation(attempt, obligation)
		stop()
		if err != nil {
			failures = append(failures, fmt.Errorf("replay comment %s for agent %s: %w", uuidToString(obligation.CommentID), uuidToString(obligation.AgentID), err))
		}
	}
	return errors.Join(failures...)
}

func (h *Handler) drainCommentDeliveryObligation(ctx context.Context, obligation db.CommentDeliveryObligation) error {
	defer h.releaseCommentDeliveryObligation(ctx, obligation)
	settle := func() error {
		return h.Queries.DeleteCommentDeliveryObligation(ctx, db.DeleteCommentDeliveryObligationParams{
			WorkspaceID: obligation.WorkspaceID, CommentID: obligation.CommentID,
			AgentID: obligation.AgentID, CommentRevision: obligation.CommentRevision,
		})
	}
	comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: obligation.CommentID, WorkspaceID: obligation.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return settle()
	}
	if err != nil {
		return err
	}
	if !commentDeliveryInputMatches(obligation, comment) {
		return settle()
	}
	issue, err := h.Queries.GetIssue(ctx, obligation.IssueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return settle()
	}
	if err != nil {
		return err
	}
	if issue.WorkspaceID != obligation.WorkspaceID {
		return settle()
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID: obligation.AgentID, WorkspaceID: obligation.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return settle()
	}
	if err != nil {
		return err
	}
	if agent.ArchivedAt.Valid {
		return settle()
	}
	coverage, err := h.Queries.CommentDeliveryObligationCoverage(ctx, db.CommentDeliveryObligationCoverageParams{
		WorkspaceID: obligation.WorkspaceID, CommentID: obligation.CommentID, AgentID: obligation.AgentID,
	})
	if err != nil {
		return err
	}
	if coverage.Settled.Valid && coverage.Settled.Bool {
		return settle()
	}
	if coverage.Planned {
		return nil
	}
	// Coverage and routing reads can wait while the author withdraws or edits
	// the input. Revalidate the accepted version before resolving its recipient.
	comment, err = h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: obligation.CommentID, WorkspaceID: obligation.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return settle()
	}
	if err != nil {
		return err
	}
	if !commentDeliveryInputMatches(obligation, comment) {
		return settle()
	}
	var parent *db.Comment
	if comment.ParentID.Valid {
		row, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID: comment.ParentID, WorkspaceID: obligation.WorkspaceID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			parent = &row
		}
	}
	// Request routing deliberately fails closed on read errors. A durable
	// replay must also distinguish those errors from a permanently lost route.
	reader, reads := h.commentDeliveryReader()
	triggers, _ := reader.computeCommentAgentTriggers(ctx, issue, comment.Content, parent, obligation.ActorType, uuidToString(obligation.ActorID), commentTriggerComputeOptions{
		ExcludeTriggerCommentID: comment.ID, AuthoringTaskID: obligation.AuthoringTaskID, OriginatorUserID: uuidToString(obligation.OriginatorUserID),
	})
	if reads.err != nil {
		return reads.err
	}
	for _, trigger := range triggers {
		if trigger.Agent.ID == obligation.AgentID {
			h.dispatchClaimedCommentDelivery(ctx, obligation, issue, trigger)
			return nil
		}
	}
	return settle()
}

// Capture errors even when an existing routing/permission helper returns only
// a boolean. Keep this observer local to the replay's read phase.
type commentDeliveryReads struct {
	dbExecutor
	err error
}

func (r *commentDeliveryReads) record(err error) {
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && r.err == nil {
		r.err = err
	}
}

func (r *commentDeliveryReads) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return commentDeliveryReadRow{Row: r.dbExecutor.QueryRow(ctx, sql, args...), reads: r}
}

type commentDeliveryReadRow struct {
	pgx.Row
	reads *commentDeliveryReads
}

func (r commentDeliveryReadRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.reads.record(err)
	return err
}

func (r *commentDeliveryReads) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := r.dbExecutor.Query(ctx, sql, args...)
	r.record(err)
	if err != nil {
		return nil, err
	}
	return commentDeliveryReadRows{Rows: rows, reads: r}, nil
}

type commentDeliveryReadRows struct {
	pgx.Rows
	reads *commentDeliveryReads
}

func (r commentDeliveryReadRows) Scan(dest ...any) error {
	err := r.Rows.Scan(dest...)
	r.reads.record(err)
	return err
}

func (r commentDeliveryReadRows) Err() error {
	err := r.Rows.Err()
	r.reads.record(err)
	return err
}

func (h *Handler) commentDeliveryReader() (*Handler, *commentDeliveryReads) {
	reads := &commentDeliveryReads{dbExecutor: h.DB}
	q := db.New(reads)
	return &Handler{Queries: q, TaskService: &service.TaskService{Queries: q}, Metrics: h.Metrics}, reads
}

func commentDeliveryInputMatches(obligation db.CommentDeliveryObligation, comment db.Comment) bool {
	return comment.IssueID == obligation.IssueID && comment.Revision >= obligation.CommentRevision && comment.Content == obligation.CommentContent &&
		!comment.DeletedAt.Valid && !isNoteComment(comment.Content) && !slices.Contains(comment.SuppressedAgentIds, obligation.AgentID)
}

func (h *Handler) commentDeliveryActorAllowed(ctx context.Context, obligation db.CommentDeliveryObligation, target db.Agent) (bool, error) {
	effectiveUser := obligation.OriginatorUserID
	switch obligation.ActorType {
	case "member":
		effectiveUser = obligation.ActorID
	case "agent":
		actor, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: obligation.ActorID, WorkspaceID: obligation.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && actor.ArchivedAt.Valid) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if obligation.AuthoringTaskID.Valid {
			parent, err := h.Queries.GetAgentTask(ctx, obligation.AuthoringTaskID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if parent.AgentID != obligation.ActorID || parent.OriginatorUserID != obligation.OriginatorUserID {
				return false, nil
			}
		}
	case "system":
	default:
		return false, nil
	}
	if effectiveUser.Valid {
		if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: effectiveUser, WorkspaceID: obligation.WorkspaceID}); errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	}
	return h.canInvokeAgent(ctx, target, obligation.ActorType, uuidToString(obligation.ActorID), uuidToString(obligation.OriginatorUserID), uuidToString(obligation.WorkspaceID)), nil
}

func (h *Handler) dispatchClaimedCommentDelivery(ctx context.Context, obligation db.CommentDeliveryObligation, issue db.Issue, trigger commentAgentTrigger) (DispatchStatus, DispatchReasonCode) {
	current, err := h.Queries.GetCommentDeliveryObligation(ctx, db.GetCommentDeliveryObligationParams{
		WorkspaceID: obligation.WorkspaceID, CommentID: obligation.CommentID, AgentID: obligation.AgentID,
	})
	if err != nil || current.CommentRevision != obligation.CommentRevision || !current.LeaseUntil.Time.Equal(obligation.LeaseUntil.Time) {
		// Cancellation removes the record; a new admission can replace its
		// revision or claim. Neither permits this older attempt to dispatch.
		return DispatchBlocked, ReasonInternalError
	}
	target, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: obligation.AgentID, WorkspaceID: obligation.WorkspaceID})
	if err != nil || target.ArchivedAt.Valid {
		return DispatchDeferred, ReasonDeferred
	}
	reader, reads := h.commentDeliveryReader()
	allowed, err := reader.commentDeliveryActorAllowed(ctx, obligation, target)
	if err != nil || reads.err != nil {
		return DispatchDeferred, ReasonDeferred
	}
	status, reason := DispatchBlocked, ReasonInvocationNotAllowed
	if allowed {
		trigger.Agent = target
		status, reason = h.resolveCommentTriggerEnqueueNow(ctx, issue, trigger, obligation)
	}
	if status != DispatchBlocked {
		return status, reason
	}
	if reason == ReasonInternalError {
		// The accepted record remains available for retry after transient failure.
		return DispatchDeferred, ReasonDeferred
	}
	if err := h.Queries.DeleteCommentDeliveryObligation(ctx, db.DeleteCommentDeliveryObligationParams{
		WorkspaceID: obligation.WorkspaceID, CommentID: obligation.CommentID,
		AgentID: obligation.AgentID, CommentRevision: obligation.CommentRevision,
	}); err != nil {
		return DispatchBlocked, ReasonInternalError
	}
	return status, reason
}
