package inboxv2

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MaxLazyMigration is the point past which a user's history stops being
// something a page load can absorb.
//
// Above it EnsureGroups returns not-ready instead of doing the work: the
// request falls back to v1, which is fully correct, and the migration happens
// on the background pass. A user with a hundred thousand notifications is rare
// and would otherwise pay for all of them at once, in the foreground, on the
// request that was supposed to show them their inbox.
const MaxLazyMigration = 50_000

// migrationLocks serialises EnsureGroups per user within a process.
//
// Not a correctness mechanism — the group identity index and the row locks are
// that, and a second process racing the first is safe. This only stops one
// user's five open tabs from each starting the same migration on every request
// and turning a one-off cost into a recurring one.
var migrationLocks sync.Map

// EnsureGroups folds a user's unclaimed inbox history into groups.
//
// User-level rather than workspace-level on purpose: the cross-workspace unread
// summary reads every workspace the user belongs to, so a per-workspace barrier
// would leave that endpoint reporting from a half-migrated world. One user, one
// barrier, all their workspaces at once.
//
// Returns ready=false when the caller should fall back to v1 for this request.
// That happens for an oversized history and for any failure: failing CLOSED
// matters more than usual here, because a half-populated group set does not
// read as broken — it reads as an inbox that has quietly lost some of its
// contents, and the fallback path is completely correct.
func (s *Service) EnsureGroups(ctx context.Context, userID pgtype.UUID, now time.Time) (bool, error) {
	gate, _ := migrationLocks.LoadOrStore(string(userID.Bytes[:]), &sync.Mutex{})
	mu := gate.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	pending, err := s.q.CountUnclaimedInboxItems(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("inboxv2: count unclaimed: %w", err)
	}
	if pending == 0 {
		return true, nil
	}
	if pending > MaxLazyMigration {
		slog.Info("inboxv2: history too large for lazy migration, falling back to v1",
			"user_id", userID, "pending", pending, "bound", MaxLazyMigration)
		return false, nil
	}

	sources, err := s.q.ListUnclaimedInboxSources(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("inboxv2: list unclaimed sources: %w", err)
	}
	for _, src := range sources {
		if err := s.migrateSource(ctx, userID, src, now); err != nil {
			// Fail closed: the caller falls back to v1 rather than rendering a
			// partially built group set as if it were the whole inbox.
			return false, err
		}
	}
	return true, nil
}

// migrateSource builds one group, in its own transaction.
//
// Per-source rather than one transaction for the whole user: a user with
// thousands of sources would otherwise hold locks on all of them at once, and a
// failure at the end would throw away every group already built. Each source is
// independently idempotent, so a retry resumes rather than restarts.
func (s *Service) migrateSource(ctx context.Context, userID pgtype.UUID, src db.ListUnclaimedInboxSourcesRow, now time.Time) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	group, err := q.AcquireInboxGroup(ctx, db.AcquireInboxGroupParams{
		WorkspaceID: src.WorkspaceID,
		RecipientID: userID,
		SourceKind:  src.SourceKind,
		SourceID:    src.SourceID,
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("inboxv2: acquire group: %w", err)
	}
	if err := s.claimSourceHistory(ctx, q,
		src.WorkspaceID, userID, SourceKind(src.SourceKind), src.SourceID, group, now); err != nil {
		return err
	}
	if _, err := q.RefreshInboxItemMirror(ctx, group.ID); err != nil {
		return fmt.Errorf("inboxv2: refresh mirror: %w", err)
	}
	return tx.Commit(ctx)
}

// PrewarmGroups migrates a bounded slice of users in the background.
//
// Optional by design: the lazy path covers correctness on its own, and this
// only moves the cost off the first request. It exists so the oversized-history
// case has somewhere to land.
func (s *Service) PrewarmGroups(ctx context.Context, batch int32, now time.Time) (int, error) {
	users, err := s.q.ListRecipientsWithUnclaimedInboxItems(ctx, batch)
	if err != nil {
		return 0, fmt.Errorf("inboxv2: list recipients: %w", err)
	}
	done := 0
	for _, u := range users {
		sources, err := s.q.ListUnclaimedInboxSources(ctx, u)
		if err != nil {
			return done, fmt.Errorf("inboxv2: list unclaimed sources: %w", err)
		}
		for _, src := range sources {
			if err := s.migrateSource(ctx, u, src, now); err != nil {
				return done, err
			}
		}
		done++
	}
	return done, nil
}
