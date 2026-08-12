package domainevent

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueCreatedFromRow builds an issue.created event from a freshly-created issue
// row, stamping its birth assignee/origin into the payload. Shared by every
// issue producer (HTTP create, autopilot dispatch, onboarding) so the event
// payload stays identical regardless of which path created the issue.
func IssueCreatedFromRow(issue db.Issue) Event {
	return IssueCreated(issue.WorkspaceID, issue.ID,
		ActorFrom(issue.CreatorType, issue.CreatorID),
		IssueCreatedPayload{
			Status:        issue.Status,
			Title:         issue.Title,
			Priority:      issue.Priority,
			ParentIssueID: util.UUIDToString(issue.ParentIssueID),
			AssigneeType:  issue.AssigneeType.String,
			AssigneeID:    util.UUIDToString(issue.AssigneeID),
			OriginType:    issue.OriginType.String,
		})
}

// Creator is the single DB method Write needs. *db.Queries satisfies it, so a
// caller passes the tx-bound handle it already holds (base.WithTx(tx)). Keeping
// it an interface (not *db.Queries) lets tests substitute a fake.
type Creator interface {
	CreateDomainEvent(ctx context.Context, arg db.CreateDomainEventParams) (db.DomainEvent, error)
	// LockWorkspaceForAutomationWrite is the writer half of the workspace
	// delete/create protocol (MUL-4332). domain_event has no FK to workspace, so
	// this explicit FOR KEY SHARE is what keeps an event from committing into a
	// workspace that is being torn down.
	LockWorkspaceForAutomationWrite(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
}

// ErrWorkspaceGone reports that the event's workspace no longer exists — it was
// deleted, or a delete is committing now. The caller's transaction must abort
// rather than write an orphaned event.
var ErrWorkspaceGone = errors.New("domainevent: workspace is gone")

// txBeginner is the subset of *pgxpool.Pool that WriteInTx needs.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Write validates evt and inserts it as a pending outbox row using q, which MUST
// be bound to the caller's transaction (Queries.WithTx) so the event commits
// atomically with the domain fact. It returns the persisted row (seq/created_at
// populated by the DB).
//
// For a root event (CorrelationID unset — every v1 domain write) Write assigns a
// fresh id and sets correlation_id = id, hop_count = 0.
func Write(ctx context.Context, q Creator, evt Event) (db.DomainEvent, error) {
	if err := evt.validate(); err != nil {
		return db.DomainEvent{}, err
	}

	// Join the workspace teardown protocol before inserting. This covers DIRECT
	// callers, which manage their own transaction; WriteInTx additionally takes the
	// same lock before its callback so the workspace is that transaction's FIRST lock
	// (re-taking FOR KEY SHARE inside one transaction is a no-op). A direct caller
	// that locks its domain fact first must take LockWorkspaceForAutomationWrite
	// itself, ahead of that lock — this guard alone does not fix lock ORDER, only
	// mutual exclusion.
	if _, err := q.LockWorkspaceForAutomationWrite(ctx, evt.WorkspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.DomainEvent{}, fmt.Errorf("%w: %s", ErrWorkspaceGone, evt.Type)
		}
		return db.DomainEvent{}, fmt.Errorf("domainevent: lock workspace for %s: %w", evt.Type, err)
	}

	id := pgUUID(uuid.New())
	correlation := evt.CorrelationID
	if !correlation.Valid {
		// Root event: it is its own correlation head.
		correlation = id
	}

	row, err := q.CreateDomainEvent(ctx, db.CreateDomainEventParams{
		ID:                   id,
		WorkspaceID:          evt.WorkspaceID,
		Type:                 evt.Type,
		SchemaVersion:        evt.SchemaVersion,
		SubjectType:          evt.SubjectType,
		SubjectID:            evt.SubjectID,
		ActorType:            evt.ActorType,
		ActorID:              evt.ActorID,
		Payload:              evt.Payload,
		CorrelationID:        correlation,
		CausationExecutionID: evt.CausationExecutionID,
		CausationActionIndex: evt.CausationActionIndex,
		HopCount:             evt.HopCount,
	})
	if err != nil {
		return db.DomainEvent{}, fmt.Errorf("domainevent: insert %s: %w", evt.Type, err)
	}
	return row, nil
}

// WriteInTx wraps a domain write that is otherwise a bare autocommit statement.
// It opens a transaction, hands fn a tx-bound *db.Queries to perform the write,
// then persists the events fn returns — all in one commit, so the fact and its
// events land atomically or not at all.
//
//	err := domainevent.WriteInTx(ctx, pool, h.Queries, func(qtx *db.Queries) ([]domainevent.Event, error) {
//	    row, err := qtx.UpdateIssueStatus(ctx, params)
//	    if err != nil { return nil, err }
//	    return []domainevent.Event{domainevent.IssueStatusChanged(ws, row.ID, actor, ...)}, nil
//	})
//
// base is the pool-bound *db.Queries; WriteInTx rebinds it to the new tx. If fn
// returns no events (e.g. the write turned out to be a no-op), the transaction
// still commits the fn's own writes.
func WriteInTx(ctx context.Context, tb txBeginner, base *db.Queries, workspaceID pgtype.UUID, fn func(qtx *db.Queries) ([]Event, error)) error {
	tx, err := tb.Begin(ctx)
	if err != nil {
		return fmt.Errorf("domainevent: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := base.WithTx(tx)

	// WORKSPACE FIRST — before the callback, not after it. The callback locks the
	// domain fact (e.g. LockIssueRowForUpdate), and DeleteWorkspace locks workspace
	// then deletes that fact; taking the workspace lock only in Write (after the
	// callback returned) left the real order as fact -> workspace, the reverse of the
	// delete's, which deadlocks under a concurrent teardown (MUL-4332 review:
	// production lock order). Locking here makes workspace the transaction's first
	// lock for every producer that funnels through this helper.
	if _, err := qtx.LockWorkspaceForAutomationWrite(ctx, workspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrWorkspaceGone, "write in tx")
		}
		return fmt.Errorf("domainevent: lock workspace: %w", err)
	}

	events, err := fn(qtx)
	if err != nil {
		return err
	}
	for _, evt := range events {
		if _, err := Write(ctx, qtx, evt); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("domainevent: commit tx: %w", err)
	}
	return nil
}

func pgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}
