package inboxv2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Service is the type the rest of the server holds. Writer is the same thing
// under its original name, kept so the delivery path reads as a writer at its
// call sites.
type Service = Writer

// NewService builds the inbox v2 service.
func NewService(q *db.Queries, tx TxStarter) *Service { return NewWriter(q, tx) }

// ErrNoGroup means the legacy row a v1 endpoint addressed has not been folded
// into a group yet and could not be folded now.
//
// The adapters treat it as "do the v1 write and stop": with the gate closed
// that is the whole behaviour, and with it open it means the row belongs to a
// source whose history is being migrated by reconcile instead. Either way the
// legacy row is still authoritative, so the user's action is not lost.
var ErrNoGroup = errors.New("inboxv2: no group for item")

// sourceOf derives a row's group identity.
//
// An issue-bearing row groups with everything else about that issue. A row with
// no issue is its own source, keyed on its own id — an autopilot pausing and a
// quick create that failed have nothing to do with each other and must not
// share a read cursor.
func sourceOf(item db.InboxItem) (SourceKind, pgtype.UUID) {
	if item.IssueID.Valid {
		return SourceIssue, item.IssueID
	}
	return SourceStandalone, item.ID
}

// groupOp is one group state transition, run under the group's row lock.
type groupOp func(ctx context.Context, q *db.Queries, group db.InboxGroup) (db.InboxGroup, error)

// applyToItemGroup is the shape every single-item v1 adapter has: resolve the
// row to its group, take the group's lock, apply the transition, and push the
// result back onto the legacy booleans — all in one transaction.
//
// The mirror refresh is not optional and not deferrable. Group state that has
// not reached inbox_item is state the v1 clients cannot see, and the whole
// reason this adapter layer is permanent rather than transitional is that the
// mobile client stays on v1 indefinitely.
func (s *Service) applyToItemGroup(
	ctx context.Context,
	workspaceID, recipientID, itemID pgtype.UUID,
	now time.Time,
	op groupOp,
) (db.InboxGroup, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	group, err := s.resolveGroupForItem(ctx, q, workspaceID, recipientID, itemID, now)
	if err != nil {
		return db.InboxGroup{}, err
	}

	group, err = op(ctx, q, group)
	if err != nil {
		return db.InboxGroup{}, err
	}
	if _, err := q.RefreshInboxItemMirror(ctx, group.ID); err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: refresh mirror: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: commit: %w", err)
	}
	return group, nil
}

// resolveGroupForItem finds the row's group, creating and populating it if this
// is the first time anyone has touched that source.
//
// Creating it here rather than refusing is what makes the adapters work during
// the migration window: a user can archive an issue whose history no v2 request
// has ever looked at, and the archive has to land on a group or it will be
// invisible to v2 the moment the read gate opens.
func (s *Service) resolveGroupForItem(
	ctx context.Context,
	q *db.Queries,
	workspaceID, recipientID, itemID pgtype.UUID,
	now time.Time,
) (db.InboxGroup, error) {
	item, err := q.GetInboxItem(ctx, itemID)
	if err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: load item: %w", err)
	}
	// The row is proof of nothing on its own; ownership is checked against the
	// caller the request authenticated.
	if item.WorkspaceID != workspaceID || item.RecipientID != recipientID || item.RecipientType != "member" {
		return db.InboxGroup{}, ErrNoGroup
	}

	kind, sourceID := sourceOf(item)
	group, err := q.AcquireInboxGroup(ctx, db.AcquireInboxGroupParams{
		WorkspaceID: workspaceID,
		RecipientID: recipientID,
		SourceKind:  string(kind),
		SourceID:    sourceID,
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: acquire group: %w", err)
	}

	if err := s.claimSourceHistory(ctx, q, workspaceID, recipientID, kind, sourceID, group, now); err != nil {
		return db.InboxGroup{}, err
	}
	// Re-read: claiming rewrites the pointers the op is about to build on.
	group, err = q.GetInboxGroupForRecipient(ctx, db.GetInboxGroupForRecipientParams{
		ID: group.ID, WorkspaceID: workspaceID, RecipientID: recipientID,
	})
	if err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: reload group: %w", err)
	}
	if !group.LatestEventID.Valid {
		// Nothing to represent the group — every row dismissed, or the history is
		// still deferred. There is no state for a v1 write to land on.
		return db.InboxGroup{}, ErrNoGroup
	}
	return group, nil
}

// claimSourceHistory folds one source's unclaimed rows into its group and
// derives the group's initial state from them.
//
// Shared by the delivery path's inline claim, the adapters and reconcile, so
// there is exactly one translation from "what v1 rows say" to "what the group
// says". Three of them would be three chances to disagree.
func (s *Service) claimSourceHistory(
	ctx context.Context,
	q *db.Queries,
	workspaceID, recipientID pgtype.UUID,
	kind SourceKind,
	sourceID pgtype.UUID,
	group db.InboxGroup,
	now time.Time,
) error {
	ts := pgtype.Timestamptz{Time: now, Valid: true}
	claimed, err := q.ClaimInboxItemsForSource(ctx, db.ClaimInboxItemsForSourceParams{
		WorkspaceID: workspaceID,
		RecipientID: recipientID,
		SourceKind:  string(kind),
		SourceID:    sourceID,
		GroupID:     group.ID,
		Now:         ts,
	})
	if err != nil {
		return fmt.Errorf("inboxv2: claim history: %w", err)
	}
	if claimed == 0 {
		return nil
	}
	if _, err := q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
		ID: group.ID, Now: ts,
	}); err != nil {
		return fmt.Errorf("inboxv2: recompute after claim: %w", err)
	}
	if _, err := q.SeedInboxGroupCursorForClaimedHistory(ctx, db.SeedInboxGroupCursorForClaimedHistoryParams{
		ID: group.ID, Now: ts,
	}); err != nil {
		return fmt.Errorf("inboxv2: seed cursor: %w", err)
	}
	if _, err := q.SeedInboxGroupArchivedFromHistory(ctx, db.SeedInboxGroupArchivedFromHistoryParams{
		ID: group.ID, Now: ts,
	}); err != nil {
		return fmt.Errorf("inboxv2: seed archived: %w", err)
	}
	return nil
}

// MarkItemRead is the v1 POST /api/inbox/{id}/read adapter.
//
// v1 marks one row read; the group reads through that row's sequence. Those
// mean the same thing because v1's own invariant is that at most the
// representative row is unread — so the row the user just read IS the group's
// unread frontier.
func (s *Service) MarkItemRead(ctx context.Context, workspaceID, recipientID, itemID pgtype.UUID, now time.Time) (db.InboxGroup, error) {
	return s.applyToItemGroup(ctx, workspaceID, recipientID, itemID, now,
		func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
			// observed_state_version is the group's own current version: a v1 client
			// cannot report one, and reading through the head is unambiguous enough
			// not to need the race protection the v2 endpoint gets.
			return q.MarkInboxGroupReadThrough(ctx, db.MarkInboxGroupReadThroughParams{
				ID:                   g.ID,
				ObservedSeq:          g.LatestSeq,
				ObservedStateVersion: g.StateVersion,
				Now:                  pgtype.Timestamptz{Time: now, Valid: true},
			})
		})
}

// MarkItemUnread is the v1 POST /api/inbox/{id}/unread adapter.
func (s *Service) MarkItemUnread(ctx context.Context, workspaceID, recipientID, itemID pgtype.UUID, now time.Time) (db.InboxGroup, error) {
	return s.applyToItemGroup(ctx, workspaceID, recipientID, itemID, now,
		func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
			return q.MarkInboxGroupUnread(ctx, db.MarkInboxGroupUnreadParams{
				ID:  g.ID,
				Now: pgtype.Timestamptz{Time: now, Valid: true},
			})
		})
}

// ArchiveItem is the v1 POST /api/inbox/{id}/archive adapter. v1 archiving is
// already issue-level — archiving one row archives every sibling — so it maps
// onto the group without any change in scope.
func (s *Service) ArchiveItem(ctx context.Context, workspaceID, recipientID, itemID pgtype.UUID, now time.Time) (db.InboxGroup, error) {
	return s.applyToItemGroup(ctx, workspaceID, recipientID, itemID, now,
		func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
			return q.ArchiveInboxGroup(ctx, db.ArchiveInboxGroupParams{
				ID:  g.ID,
				Now: pgtype.Timestamptz{Time: now, Valid: true},
			})
		})
}

// UnarchiveItem is the v1 POST /api/inbox/{id}/unarchive adapter.
func (s *Service) UnarchiveItem(ctx context.Context, workspaceID, recipientID, itemID pgtype.UUID, now time.Time) (db.InboxGroup, error) {
	return s.applyToItemGroup(ctx, workspaceID, recipientID, itemID, now,
		func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
			return q.UnarchiveInboxGroup(ctx, db.UnarchiveInboxGroupParams{
				ID:  g.ID,
				Now: pgtype.Timestamptz{Time: now, Valid: true},
			})
		})
}

// BatchOp names one of the four v1 bulk endpoints.
type BatchOp string

const (
	BatchMarkAllRead     BatchOp = "mark_all_read"
	BatchArchiveAll      BatchOp = "archive_all"
	BatchArchiveRead     BatchOp = "archive_read"
	BatchArchiveComplete BatchOp = "archive_completed"
)

// ApplyBatch is the adapter for the four bulk v1 endpoints.
//
// One statement over the recipient's groups plus one bulk mirror refresh,
// rather than a loop of single-group operations: "archive all" on a busy
// workspace is precisely where a per-group loop turns one request into hundreds
// of round trips inside a transaction.
//
// Returns the number of GROUPS affected. That is deliberately not the number of
// rows v1 returned — the count v1 reported was rows, which is the number the
// clients then had to fold back down to groups for display and got wrong. The
// handler keeps reporting the legacy number to legacy clients.
func (s *Service) ApplyBatch(ctx context.Context, workspaceID, recipientID pgtype.UUID, op BatchOp, now time.Time) ([]db.InboxGroup, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	ts := pgtype.Timestamptz{Time: now, Valid: true}
	var groups []db.InboxGroup
	switch op {
	case BatchMarkAllRead:
		groups, err = q.MarkAllInboxGroupsRead(ctx, db.MarkAllInboxGroupsReadParams{
			WorkspaceID: workspaceID, RecipientID: recipientID, Now: ts,
		})
	case BatchArchiveAll:
		groups, err = q.ArchiveAllInboxGroups(ctx, db.ArchiveAllInboxGroupsParams{
			WorkspaceID: workspaceID, RecipientID: recipientID, Now: ts,
		})
	case BatchArchiveRead:
		groups, err = q.ArchiveReadInboxGroups(ctx, db.ArchiveReadInboxGroupsParams{
			WorkspaceID: workspaceID, RecipientID: recipientID, Now: ts,
		})
	case BatchArchiveComplete:
		groups, err = q.ArchiveCompletedInboxGroups(ctx, db.ArchiveCompletedInboxGroupsParams{
			WorkspaceID: workspaceID, RecipientID: recipientID, Now: ts,
		})
	default:
		return nil, fmt.Errorf("inboxv2: unknown batch op %q", op)
	}
	if err != nil {
		return nil, fmt.Errorf("inboxv2: batch %s: %w", op, err)
	}

	if len(groups) > 0 {
		if _, err := q.RefreshInboxItemMirrorForRecipient(ctx, db.RefreshInboxItemMirrorForRecipientParams{
			WorkspaceID: workspaceID, RecipientID: recipientID,
		}); err != nil {
			return nil, fmt.Errorf("inboxv2: refresh mirror: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("inboxv2: commit: %w", err)
	}
	return groups, nil
}

// DismissIssueType retires an issue's stale notifications of one type and
// brings every group they belonged to back in line, in one transaction.
//
// This is the path an issue reaching a terminal status takes. Splitting it —
// stamping the rows in one place and fixing the groups somewhere else, or not
// at all — leaves each affected group pointing at a row the user was just shown
// the back of. That was invisible while the gate was closed and wrong the
// moment it opened, which is exactly the class of bug a permanently-closed
// switch hides.
//
// Returns the recipients whose visible inbox actually changed, for the caller's
// websocket fan-out.
func (s *Service) DismissIssueType(
	ctx context.Context,
	workspaceID, issueID pgtype.UUID,
	notificationType string,
	now time.Time,
) ([]db.ArchiveInboxByIssueAndTypeRow, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	rows, err := q.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		Type:        notificationType,
	})
	if err != nil {
		return nil, fmt.Errorf("inboxv2: dismiss: %w", err)
	}

	// Deduplicate: every returned row carries the same array of touched groups.
	seen := map[string]bool{}
	ts := pgtype.Timestamptz{Time: now, Valid: true}
	for _, row := range rows {
		for _, gid := range row.TouchedGroupIds {
			key := string(gid.Bytes[:])
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, err := q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
				ID: gid, Now: ts,
			}); err != nil {
				return nil, fmt.Errorf("inboxv2: recompute after dismiss: %w", err)
			}
			if _, err := q.RefreshInboxItemMirror(ctx, gid); err != nil {
				return nil, fmt.Errorf("inboxv2: refresh mirror after dismiss: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("inboxv2: commit: %w", err)
	}
	return rows, nil
}

// GroupFor returns the group a legacy row belongs to, or ErrNoGroup.
func (s *Service) GroupFor(ctx context.Context, workspaceID, recipientID, itemID pgtype.UUID) (db.InboxGroup, error) {
	group, err := s.q.FindInboxGroupForItem(ctx, db.FindInboxGroupForItemParams{
		ItemID: itemID, WorkspaceID: workspaceID, RecipientID: recipientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.InboxGroup{}, ErrNoGroup
	}
	return group, err
}

// applyToGroup is the v2 counterpart of applyToItemGroup: the caller already
// resolved and authorised the group, so this only locks it, applies the
// transition and refreshes the mirror.
//
// The mirror refresh is what keeps mobile honest. A v2 client archives a group;
// the phone, still on v1, reads inbox_item — and would go on showing the
// archived notification forever if the group's state stopped at inbox_group.
func (s *Service) applyToGroup(ctx context.Context, group db.InboxGroup, op groupOp) (db.InboxGroup, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	locked, err := q.LockInboxGroup(ctx, db.LockInboxGroupParams{
		ID: group.ID, WorkspaceID: group.WorkspaceID, RecipientID: group.RecipientID,
	})
	if err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: lock group: %w", err)
	}
	updated, err := op(ctx, q, locked)
	if err != nil {
		return db.InboxGroup{}, err
	}
	if _, err := q.RefreshInboxItemMirror(ctx, updated.ID); err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: refresh mirror: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.InboxGroup{}, fmt.Errorf("inboxv2: commit: %w", err)
	}
	return updated, nil
}

// MarkGroupRead advances the cursor to what the client reports having seen.
func (s *Service) MarkGroupRead(ctx context.Context, group db.InboxGroup, observedSeq, observedVersion int64, now time.Time) (db.InboxGroup, error) {
	return s.applyToGroup(ctx, group, func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
		return q.MarkInboxGroupReadThrough(ctx, db.MarkInboxGroupReadThroughParams{
			ID:                   g.ID,
			ObservedSeq:          observedSeq,
			ObservedStateVersion: observedVersion,
			Now:                  pgtype.Timestamptz{Time: now, Valid: true},
		})
	})
}

// MarkGroupUnread records explicit user intent.
func (s *Service) MarkGroupUnread(ctx context.Context, group db.InboxGroup, now time.Time) (db.InboxGroup, error) {
	return s.applyToGroup(ctx, group, func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
		return q.MarkInboxGroupUnread(ctx, db.MarkInboxGroupUnreadParams{
			ID: g.ID, Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
	})
}

// ArchiveGroup marks a group handled.
func (s *Service) ArchiveGroup(ctx context.Context, group db.InboxGroup, now time.Time) (db.InboxGroup, error) {
	return s.applyToGroup(ctx, group, func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
		return q.ArchiveInboxGroup(ctx, db.ArchiveInboxGroupParams{
			ID: g.ID, Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
	})
}

// UnarchiveGroup restores an archived group.
func (s *Service) UnarchiveGroup(ctx context.Context, group db.InboxGroup, now time.Time) (db.InboxGroup, error) {
	return s.applyToGroup(ctx, group, func(ctx context.Context, q *db.Queries, g db.InboxGroup) (db.InboxGroup, error) {
		return q.UnarchiveInboxGroup(ctx, db.UnarchiveInboxGroupParams{
			ID: g.ID, Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
	})
}
