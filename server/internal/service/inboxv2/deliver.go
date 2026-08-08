// Package inboxv2 owns the group/event model behind the Inbox.
//
// The product renders "one row per issue" but the database only ever had "one
// row per event", so the row a user marks read, archives or snoozes has never
// existed as an entity — each of the three clients folded events into it for
// itself. inbox_group is that entity; inbox_item keeps its role as the event
// table and gains the columns that tie the two together.
package inboxv2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter matches the interface the other services in this tree use.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SourceKind identifies what a group is about.
type SourceKind string

const (
	// SourceIssue folds everything about one issue into one row per person.
	SourceIssue SourceKind = "issue"
	// SourceStandalone is for notifications with no durable parent — an
	// autopilot pausing, a quick create that failed before an issue existed.
	SourceStandalone SourceKind = "standalone"
)

// uniqueViolation is the SQLSTATE for a unique constraint violation.
const uniqueViolation = "23505"

// MaxInlineClaim bounds how much history one delivery will migrate inside the
// notification transaction.
//
// A first delivery to an issue that has been running for months would otherwise
// drag every notification that issue ever produced for this person through an
// UPDATE — and then through the mirror refresh again — while a user waits for a
// comment to post. Above the bound the delivery takes its own sequence and
// leaves the history to reconcile, which numbers it downward from the group's
// floor and therefore cannot collide with anything already there.
//
// The group is correct about its newest event either way. What the delivery
// defers is only the older rows, which no v2 surface renders anyway: the list
// shows one representative per group.
const MaxInlineClaim = 200

// Delivery is one notification for one member, in the shape the write path
// needs. Producers already hold every field; the legacy write threw most of the
// structure away into a JSON blob.
type Delivery struct {
	WorkspaceID pgtype.UUID
	RecipientID pgtype.UUID

	SourceKind SourceKind
	SourceID   pgtype.UUID

	Type     string
	Severity string
	IssueID  pgtype.UUID
	Title    string
	Body     pgtype.Text

	ActorType pgtype.Text
	ActorID   pgtype.UUID
	Details   []byte

	TargetKind pgtype.Text
	TargetID   pgtype.UUID

	// DeliveryKey is derived by the producer from the originating entity, never
	// from a row id, so a retry recomputes it and collides instead of creating a
	// second notification.
	DeliveryKey pgtype.Text
}

// Result reports what a delivery did.
//
// Deduplicated means the delivery had already been recorded: nothing advanced,
// and the caller must not emit a websocket event — re-emitting would make a
// retry look like a second notification to every connected client.
//
// GateOpen reports whether the v2 half ran at all. With the gate closed the
// legacy row is still written and Group is zero.
type Result struct {
	Item         db.InboxItem
	Group        db.InboxGroup
	Deduplicated bool
	GateOpen     bool
}

// Writer performs the delivery write path.
type Writer struct {
	q  *db.Queries
	tx TxStarter
}

func NewWriter(q *db.Queries, tx TxStarter) *Writer {
	return &Writer{q: q, tx: tx}
}

// Deliver writes one notification.
//
// Everything happens in one transaction, and the order inside it is the
// contract:
//
//  1. Read the cutover row. Reading it HERE, rather than from a cached flag, is
//     what makes the switch atomic with respect to every delivery: with a
//     per-process flag there is a window where some instances write groups and
//     some do not, and the rows written in that window are precisely the ones
//     reconcile would have to find afterwards.
//  2. Gate closed → write the legacy row exactly as before and stop. This is
//     the pre-v2 behaviour byte for byte, which is what lets the code ship long
//     before the switch is touched.
//  3. Probe by delivery key. A hit returns the existing row without advancing
//     anything.
//  4. Acquire (create or lock) the group. This lock is the fixed point of the
//     lock order every write path in this package shares: group first, then its
//     items.
//  5. Allocate event_seq = latest_seq+1 and a created_at that is monotonic
//     within the group, insert the item, advance the group, refresh the mirror.
//
// Allocating the sequence inside the lock is what makes it gapless, and writing
// it in the same transaction as the insert is what makes a failure roll the
// number back too — by transaction atomicity rather than by convention.
func (w *Writer) Deliver(ctx context.Context, d Delivery, now time.Time) (Result, error) {
	if d.SourceKind == "" {
		return Result{}, errors.New("inboxv2: source kind required")
	}
	if !d.SourceID.Valid {
		return Result{}, errors.New("inboxv2: source id required")
	}

	res, err := w.deliverOnce(ctx, d, now)
	if err == nil {
		return res, nil
	}

	// Two transactions can both miss the delivery-key probe; the unique index
	// decides. The loser rolls back whole and re-reads the winner's row, so both
	// callers describe the same single notification.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return w.loadExisting(ctx, d)
	}
	return Result{}, err
}

func (w *Writer) deliverOnce(ctx context.Context, d Delivery, now time.Time) (Result, error) {
	tx, err := w.tx.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := w.q.WithTx(tx)

	enabled, err := q.GetInboxV2WriteEnabled(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return Result{}, fmt.Errorf("inboxv2: read cutover: %w", err)
		}
		// No cutover row at all (the migration was rolled back) reads as off,
		// which is the safe direction.
		enabled = false
	}

	if !enabled {
		item, err := q.CreateInboxItem(ctx, legacyParams(d))
		if err != nil {
			return Result{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("inboxv2: commit: %w", err)
		}
		return Result{Item: item}, nil
	}

	// A delivery key is mandatory once the gate is open. Idempotency that is
	// optional is idempotency that a producer can silently omit — and the failure
	// only shows up as duplicate notifications after a retry in production, on the
	// one path nobody re-tests. Requiring it here makes the omission a startup-time
	// error in tests instead.
	if !d.DeliveryKey.Valid || d.DeliveryKey.String == "" {
		return Result{}, errors.New("inboxv2: delivery key required once the write gate is open")
	}

	{
		existing, err := q.FindInboxItemByDeliveryKey(ctx, db.FindInboxItemByDeliveryKeyParams{
			WorkspaceID: d.WorkspaceID,
			RecipientID: d.RecipientID,
			DeliveryKey: d.DeliveryKey,
		})
		if err == nil {
			group, gerr := q.GetInboxGroupForRecipient(ctx, db.GetInboxGroupForRecipientParams{
				ID:          existing.GroupID,
				WorkspaceID: d.WorkspaceID,
				RecipientID: d.RecipientID,
			})
			if gerr != nil {
				return Result{}, fmt.Errorf("inboxv2: load group for duplicate: %w", gerr)
			}
			if err := tx.Commit(ctx); err != nil {
				return Result{}, fmt.Errorf("inboxv2: commit: %w", err)
			}
			return Result{Item: existing, Group: group, Deduplicated: true, GateOpen: true}, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return Result{}, fmt.Errorf("inboxv2: delivery key probe: %w", err)
		}
	}

	group, err := q.AcquireInboxGroup(ctx, db.AcquireInboxGroupParams{
		WorkspaceID: d.WorkspaceID,
		RecipientID: d.RecipientID,
		SourceKind:  string(d.SourceKind),
		SourceID:    d.SourceID,
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: acquire group: %w", err)
	}

	// Claim any unclaimed history for this source BEFORE taking a sequence
	// number. The group lock is already held, so nothing can interleave.
	//
	// Order is the reason this happens here rather than in a background pass:
	// history is older than the event being delivered, and the legacy endpoints
	// sort by created_at. Claiming first means the history numbers 1..M in
	// created_at order and this delivery takes M+1, so the sequence and the
	// timestamp agree about which row represents the group. Claiming afterwards
	// would either collide on inbox_item_group_seq_uidx or hand an older row a
	// higher number than a newer one.
	pending, err := q.CountUnclaimedInboxItemsForSource(ctx, db.CountUnclaimedInboxItemsForSourceParams{
		WorkspaceID: d.WorkspaceID,
		RecipientID: d.RecipientID,
		SourceKind:  string(d.SourceKind),
		SourceID:    d.SourceID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: count unclaimed: %w", err)
	}

	var claimed int64
	if pending > MaxInlineClaim {
		// Deferred, not skipped: reconcile numbers this history below the group's
		// floor, so the ordering it lands in is the same one it would have had
		// inline. Logged because a source over the bound is a real signal about the
		// shape of the data, not a routine event.
		slog.Info("inboxv2: deferring oversized history claim to reconcile",
			"workspace_id", d.WorkspaceID, "recipient_id", d.RecipientID,
			"source_kind", d.SourceKind, "pending", pending, "bound", MaxInlineClaim)
	} else if pending > 0 {
		claimed, err = q.ClaimInboxItemsForSource(ctx, db.ClaimInboxItemsForSourceParams{
			WorkspaceID: d.WorkspaceID,
			RecipientID: d.RecipientID,
			SourceKind:  string(d.SourceKind),
			SourceID:    d.SourceID,
			GroupID:     group.ID,
			Now:         pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return Result{}, fmt.Errorf("inboxv2: claim history: %w", err)
		}
	}
	if claimed > 0 {
		// The claimed rows advanced the group's head; recompute from the rows
		// themselves rather than assuming, since dismissal may have retired
		// some of what was just claimed.
		group, err = q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
			ID:  group.ID,
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return Result{}, fmt.Errorf("inboxv2: recompute after claim: %w", err)
		}
		// The claimed history is history the user has already been shown through
		// v1, so the cursor parks one below the head rather than at zero, which
		// would re-announce years of notifications as unread.
		group, err = q.SeedInboxGroupCursorForClaimedHistory(ctx, db.SeedInboxGroupCursorForClaimedHistoryParams{
			ID:  group.ID,
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return Result{}, fmt.Errorf("inboxv2: seed cursor: %w", err)
		}
		// A source whose every historical row was archived was archived as a whole
		// under v1, so the group inherits that rather than the rows carrying it as
		// individual dismissals. The delivery below then unarchives the group,
		// which is the correct "archive is not unsubscribe" behaviour — but the
		// group has to pass THROUGH the archived state for a later unarchive to
		// restore the right thing.
		if _, err := q.SeedInboxGroupArchivedFromHistory(ctx, db.SeedInboxGroupArchivedFromHistoryParams{
			ID:  group.ID,
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			return Result{}, fmt.Errorf("inboxv2: seed archived: %w", err)
		}
	}

	// Allocate from the group's high-water mark, not from latest_seq. Those
	// differ after a dismissal retires the representative: latest_seq moves down
	// to the surviving head while the dismissed row keeps its number, so
	// latest_seq+1 would collide with a row that is still there.
	seq, err := q.NextInboxItemSeqForGroup(ctx, group.ID)
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: next sequence: %w", err)
	}
	createdAt := monotonicCreatedAt(now, group.LatestEventAt)

	item, err := q.InsertInboxItemForGroup(ctx, db.InsertInboxItemForGroupParams{
		WorkspaceID: d.WorkspaceID,
		RecipientID: d.RecipientID,
		Type:        d.Type,
		Severity:    d.Severity,
		IssueID:     d.IssueID,
		Title:       d.Title,
		Body:        d.Body,
		ActorType:   d.ActorType,
		ActorID:     d.ActorID,
		Details:     d.Details,
		GroupID:     group.ID,
		EventSeq:    pgtype.Int8{Int64: seq, Valid: true},
		TargetKind:  d.TargetKind,
		TargetID:    d.TargetID,
		DeliveryKey: d.DeliveryKey,
		CreatedAt:   pgtype.Timestamptz{Time: createdAt, Valid: true},
	})
	if err != nil {
		return Result{}, err
	}

	group, err = q.AdvanceInboxGroupForItem(ctx, db.AdvanceInboxGroupForItemParams{
		EventSeq:  seq,
		EventID:   item.ID,
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
		ID:        group.ID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: advance group: %w", err)
	}

	if _, err := q.RefreshInboxItemMirror(ctx, group.ID); err != nil {
		return Result{}, fmt.Errorf("inboxv2: refresh mirror: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("inboxv2: commit: %w", err)
	}
	// Re-read the inserted row so the caller sees the mirror the refresh just
	// applied rather than the pre-refresh booleans RETURNING gave back.
	item.Read = false
	item.Archived = false
	return Result{Item: item, Group: group, GateOpen: true}, nil
}

// monotonicCreatedAt keeps created_at strictly increasing within a group.
//
// The legacy endpoints order by created_at, so if two deliveries land in the
// same millisecond — or a node's clock is behind the last one — the v1 view and
// the v2 sequence would disagree about which row represents the group. One
// microsecond past the previous event is enough, and matches the column's
// resolution.
func monotonicCreatedAt(now time.Time, previous pgtype.Timestamptz) time.Time {
	if previous.Valid && !now.After(previous.Time) {
		return previous.Time.Add(time.Microsecond)
	}
	return now
}

func (w *Writer) loadExisting(ctx context.Context, d Delivery) (Result, error) {
	item, err := w.q.FindInboxItemByDeliveryKey(ctx, db.FindInboxItemByDeliveryKeyParams{
		WorkspaceID: d.WorkspaceID,
		RecipientID: d.RecipientID,
		DeliveryKey: d.DeliveryKey,
	})
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: reload after conflict: %w", err)
	}
	group, err := w.q.GetInboxGroupForRecipient(ctx, db.GetInboxGroupForRecipientParams{
		ID:          item.GroupID,
		WorkspaceID: d.WorkspaceID,
		RecipientID: d.RecipientID,
	})
	if err != nil {
		return Result{}, fmt.Errorf("inboxv2: reload group after conflict: %w", err)
	}
	return Result{Item: item, Group: group, Deduplicated: true, GateOpen: true}, nil
}

// legacyParams is the pre-v2 write, unchanged. Keeping it here rather than at
// the call sites means a producer's code path is identical either way and the
// gate alone decides which shape the row takes.
func legacyParams(d Delivery) db.CreateInboxItemParams {
	return db.CreateInboxItemParams{
		WorkspaceID:   d.WorkspaceID,
		RecipientType: "member",
		RecipientID:   d.RecipientID,
		Type:          d.Type,
		Severity:      d.Severity,
		IssueID:       d.IssueID,
		Title:         d.Title,
		Body:          d.Body,
		ActorType:     d.ActorType,
		ActorID:       d.ActorID,
		Details:       d.Details,
	}
}

// IsUnread is the single derived unread rule. Every count, badge and mirror
// refresh goes through the same expression so the three clients cannot drift
// into separate definitions the way they did with the old boolean.
func IsUnread(g db.InboxGroup) bool {
	return g.ManualUnread || g.ReadThroughSeq < g.LatestSeq
}
