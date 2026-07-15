package domainevent

import (
	"context"
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
}

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
func WriteInTx(ctx context.Context, tb txBeginner, base *db.Queries, fn func(qtx *db.Queries) ([]Event, error)) error {
	tx, err := tb.Begin(ctx)
	if err != nil {
		return fmt.Errorf("domainevent: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := base.WithTx(tx)
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
