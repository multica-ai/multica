package inboxv2

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regressions for the four blockers found in review of the first write-path
// commit. Each one reproduces the concrete sequence that broke, not just the
// property in the abstract.

// insertLegacyRow writes the shape a pre-cutover delivery left behind: no group,
// no sequence, no delivery key.
func (f *fixture) insertLegacyRow(t *testing.T, issueID pgtype.UUID, createdAt time.Time, typ string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, read, archived, created_at)
VALUES ($1, 'member', $2, $3, 'info', $4, 'legacy history', true, false, $5)
RETURNING id
`, f.ws, f.user, typ, issueID, createdAt).Scan(&id); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	return id
}

// Blocker 1. The gate opens, a new notification lands on an empty group and
// takes event_seq 1, and only afterwards does this person's history get
// claimed. Numbering the history from 1 again collides on
// inbox_item_group_seq_uidx; numbering it above the new row would put an older
// notification at a higher sequence than a newer one, so v1 (created_at) and v2
// (event_seq) would elect different representative rows.
func TestHistoryClaimedAfterANewDeliveryKeepsSequenceAndOrderConsistent(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	// Two historical rows, older than anything the gate will produce.
	f.insertLegacyRow(t, f.issue, f.now.Add(-2*time.Hour), "new_comment")
	f.insertLegacyRow(t, f.issue, f.now.Add(-1*time.Hour), "new_comment")

	// The new delivery. It must claim the history before taking its own number.
	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatalf("delivery: %v", err)
	}
	if res.Item.EventSeq.Int64 != 3 {
		t.Fatalf("new delivery took seq %d, want 3 behind two claimed rows", res.Item.EventSeq.Int64)
	}

	// created_at order and event_seq order must agree.
	rows, err := f.pool.Query(ctx, `
SELECT event_seq FROM inbox_item WHERE workspace_id = $1 ORDER BY created_at, id`, f.ws)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var s pgtype.Int8
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if !s.Valid {
			t.Fatal("a row was left unclaimed")
		}
		seqs = append(seqs, s.Int64)
	}
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Fatalf("event_seq disagrees with created_at order: %v", seqs)
		}
	}

	// Already-seen history must not resurface as unread: the cursor parks one
	// below the head, so exactly the new event is unread.
	if !IsUnread(res.Group) {
		t.Fatal("the group must be unread for the event that just arrived")
	}
	if res.Group.ReadThroughSeq != 2 {
		t.Fatalf("cursor = %d, want 2 — claimed history is history the user already saw",
			res.Group.ReadThroughSeq)
	}
}

// A second delivery after the history is claimed must simply continue the
// sequence, not re-claim anything.
func TestClaimIsIdempotentAcrossDeliveries(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	f.insertLegacyRow(t, f.issue, f.now.Add(-time.Hour), "new_comment")

	first, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Item.EventSeq.Int64 != 2 || second.Item.EventSeq.Int64 != 3 {
		t.Fatalf("sequence = %d, %d; want 2, 3", first.Item.EventSeq.Int64, second.Item.EventSeq.Int64)
	}
	if got := f.count(t, `SELECT count(*) FROM inbox_group WHERE workspace_id=$1`); got != 1 {
		t.Fatalf("groups = %d, want 1", got)
	}
}

// Blocker 2. Two issue-less historical rows are two unrelated notifications —
// an autopilot pause and a failed quick create have nothing to do with each
// other. Folding them by issue_id IS NULL would give them one read cursor and
// one archive state.
func TestStandaloneHistoryRowsBecomeSeparateGroups(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	a := f.insertLegacyRow(t, pgtype.UUID{}, f.now.Add(-2*time.Hour), "autopilot_paused")
	b := f.insertLegacyRow(t, pgtype.UUID{}, f.now.Add(-time.Hour), "quick_create_failed")

	sources, err := f.q.ListUnclaimedInboxSources(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("two issue-less rows produced %d sources, want 2", len(sources))
	}
	seen := map[string]bool{}
	for _, s := range sources {
		if s.SourceKind != string(SourceStandalone) {
			t.Fatalf("source_kind = %q, want standalone", s.SourceKind)
		}
		seen[uuid.UUID(s.SourceID.Bytes).String()] = true
	}
	if !seen[uuid.UUID(a.Bytes).String()] || !seen[uuid.UUID(b.Bytes).String()] {
		t.Fatal("each standalone row must be its own source, keyed on its own id")
	}

	// Claiming one must not sweep up the other.
	group, err := f.q.AcquireInboxGroup(ctx, db.AcquireInboxGroupParams{
		WorkspaceID: f.ws, RecipientID: f.user,
		SourceKind: string(SourceStandalone), SourceID: a,
		Now: pgtype.Timestamptz{Time: f.now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.q.ClaimInboxItemsForSource(ctx, db.ClaimInboxItemsForSourceParams{
		WorkspaceID: f.ws, RecipientID: f.user,
		SourceKind: string(SourceStandalone), SourceID: a,
		GroupID: group.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed != 1 {
		t.Fatalf("claiming one standalone source took %d rows, want 1", claimed)
	}
	var stillUnclaimed bool
	if err := f.pool.QueryRow(ctx,
		`SELECT group_id IS NULL FROM inbox_item WHERE id = $1`, b).Scan(&stillUnclaimed); err != nil {
		t.Fatal(err)
	}
	if !stillUnclaimed {
		t.Fatal("claiming one standalone source swept up an unrelated one")
	}
}

// Blocker 3. Event-level dismissal is a different fact from group archive. The
// real path is ArchiveInboxByIssueAndType — an issue reaching a terminal status
// retires its stale task_failed rows — and the mirror must not put them back.
func TestDismissedRowSurvivesTheMirrorAndIsNotElectedRepresentative(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	// A delivery, then a task_failed delivery on the same issue.
	if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now); err != nil {
		t.Fatal(err)
	}
	failed := f.delivery("v1:" + uuid.NewString())
	failed.Type = "task_failed"
	failedRes, err := f.w.Deliver(ctx, failed, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if failedRes.Item.EventSeq.Int64 != 2 {
		t.Fatalf("task_failed seq = %d, want 2", failedRes.Item.EventSeq.Int64)
	}

	// The real dismissal path, not a DELETE.
	if _, err := f.q.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: f.ws, IssueID: f.issue, Type: "task_failed",
	}); err != nil {
		t.Fatal(err)
	}

	// The representative must fall back to the surviving row.
	group, err := f.q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
		ID: failedRes.Group.ID, Now: pgtype.Timestamptz{Time: f.now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.LatestSeq != 1 {
		t.Fatalf("latest_seq = %d, want the dismissed row to be skipped", group.LatestSeq)
	}

	// And the mirror must not resurrect it.
	if _, err := f.q.RefreshInboxItemMirror(ctx, group.ID); err != nil {
		t.Fatal(err)
	}
	var archived bool
	var dismissedAt pgtype.Timestamptz
	if err := f.pool.QueryRow(ctx,
		`SELECT archived, dismissed_at FROM inbox_item WHERE id = $1`,
		failedRes.Item.ID).Scan(&archived, &dismissedAt); err != nil {
		t.Fatal(err)
	}
	if !dismissedAt.Valid {
		t.Fatal("the dismissal path must record dismissed_at")
	}
	if !archived {
		t.Fatal("the mirror resurrected a dismissed row — this is the feature the refactor must preserve")
	}

	// A further delivery must still leave it dismissed.
	if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx,
		`SELECT archived FROM inbox_item WHERE id = $1`, failedRes.Item.ID).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if !archived {
		t.Fatal("a later delivery resurrected the dismissed row")
	}
}

// Blocker 4. The gate read must be an activation boundary: a delivery that read
// `off` must finish before activation commits, so no delivery can straddle the
// flip and leave an unclaimed row behind a completed reconcile pass.
func TestActivationWaitsForInFlightDeliveries(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	// A delivery in flight, holding the share lock on the cutover row.
	enabled, err := f.q.WithTx(tx).GetInboxV2WriteEnabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("fixture should have left the gate closed")
	}

	// Activation must not be able to commit while that read is held.
	done := make(chan error, 1)
	go func() {
		done <- f.q.SetInboxV2WriteEnabled(context.Background(), true)
	}()

	select {
	case err := <-done:
		t.Fatalf("activation completed while a delivery held the gate read: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Correct: it is blocked behind the share lock.
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("activation failed after the delivery finished: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("activation never completed after the delivery committed")
	}
}

// Round 2, blocker 1. All three representative pointers must come from the same
// surviving row. Filtering dismissed rows out of the sequence but not out of the
// id left latest_seq on the survivor while latest_event_id still addressed the
// dismissed one.
func TestRepresentativePointersAllComeFromTheSurvivor(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	first, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	failed := f.delivery("v1:" + uuid.NewString())
	failed.Type = "task_failed"
	failedRes, err := f.w.Deliver(ctx, failed, f.now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.q.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: f.ws, IssueID: f.issue, Type: "task_failed",
	}); err != nil {
		t.Fatal(err)
	}
	group, err := f.q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
		ID: failedRes.Group.ID, Now: pgtype.Timestamptz{Time: f.now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	if group.LatestSeq != first.Item.EventSeq.Int64 {
		t.Fatalf("latest_seq = %d, want the survivor's %d", group.LatestSeq, first.Item.EventSeq.Int64)
	}
	if group.LatestEventID != first.Item.ID {
		t.Fatal("latest_event_id still addresses the dismissed row — the pointers disagree")
	}
	if !group.LatestEventAt.Time.Equal(first.Item.CreatedAt.Time) {
		t.Fatal("latest_event_at came from a different row than latest_event_id")
	}
}

// Every row dismissed is a real state, not an impossible one. The group must
// read as empty rather than keeping a pointer to something the user was
// deliberately shown the back of.
func TestRepresentativeWithNoSurvivorIsEmpty(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	only := f.delivery("v1:" + uuid.NewString())
	only.Type = "task_failed"
	res, err := f.w.Deliver(ctx, only, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: f.ws, IssueID: f.issue, Type: "task_failed",
	}); err != nil {
		t.Fatal(err)
	}
	group, err := f.q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
		ID: res.Group.ID, Now: pgtype.Timestamptz{Time: f.now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.LatestSeq != 0 {
		t.Fatalf("latest_seq = %d, want 0 with every row dismissed", group.LatestSeq)
	}
	if group.LatestEventID.Valid {
		t.Fatal("latest_event_id must be NULL when nothing survives")
	}
	if IsUnread(group) {
		t.Fatal("a group whose every row is dismissed must not read as unread")
	}
}

// Round 2, blocker 2. An issue reaching a terminal status while its group is
// already archived must still stamp the dismissal. Otherwise un-archiving later
// brings the stale task_failed row back.
func TestDismissalStampsEvenWhenTheGroupIsAlreadyArchived(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now); err != nil {
		t.Fatal(err)
	}
	failed := f.delivery("v1:" + uuid.NewString())
	failed.Type = "task_failed"
	failedRes, err := f.w.Deliver(ctx, failed, f.now)
	if err != nil {
		t.Fatal(err)
	}

	// The user archives the whole group first; the mirror sets every row
	// archived = true.
	if _, err := f.pool.Exec(ctx, `
UPDATE inbox_group SET archived_at = now(), read_through_seq = latest_seq WHERE id = $1`,
		failedRes.Group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.RefreshInboxItemMirror(ctx, failedRes.Group.ID); err != nil {
		t.Fatal(err)
	}

	// Now the issue completes.
	if _, err := f.q.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: f.ws, IssueID: f.issue, Type: "task_failed",
	}); err != nil {
		t.Fatal(err)
	}
	var dismissedAt pgtype.Timestamptz
	if err := f.pool.QueryRow(ctx,
		`SELECT dismissed_at FROM inbox_item WHERE id = $1`, failedRes.Item.ID).Scan(&dismissedAt); err != nil {
		t.Fatal(err)
	}
	if !dismissedAt.Valid {
		t.Fatal("an already-archived row must still be stamped as dismissed")
	}

	// Un-archive, refresh: the stale failure must stay gone.
	if _, err := f.pool.Exec(ctx,
		`UPDATE inbox_group SET archived_at = NULL WHERE id = $1`, failedRes.Group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.RefreshInboxItemMirror(ctx, failedRes.Group.ID); err != nil {
		t.Fatal(err)
	}
	var archived bool
	if err := f.pool.QueryRow(ctx,
		`SELECT archived FROM inbox_item WHERE id = $1`, failedRes.Item.ID).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if !archived {
		t.Fatal("un-archiving the group resurrected a dismissed task_failed row")
	}
}

// Round 2, blocker 3. A gap row claimed after a dismissal must take the group's
// high-water mark, not latest_seq — the dismissed row still occupies its number.
func TestClaimAfterDismissalUsesTheHighWaterMark(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now); err != nil {
		t.Fatal(err)
	}
	failed := f.delivery("v1:" + uuid.NewString())
	failed.Type = "task_failed"
	failedRes, err := f.w.Deliver(ctx, failed, f.now) // seq 2
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: f.ws, IssueID: f.issue, Type: "task_failed",
	}); err != nil {
		t.Fatal(err)
	}
	group, err := f.q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
		ID: failedRes.Group.ID, Now: pgtype.Timestamptz{Time: f.now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.LatestSeq != 1 {
		t.Fatalf("setup: latest_seq = %d, want 1", group.LatestSeq)
	}

	// A gap row, the shape a rollback window leaves behind.
	gap := f.insertLegacyRow(t, f.issue, f.now.Add(time.Minute), "new_comment")

	claimed, err := f.q.ClaimInboxItemsForSource(ctx, db.ClaimInboxItemsForSourceParams{
		WorkspaceID: f.ws, RecipientID: f.user,
		SourceKind: string(SourceIssue), SourceID: f.issue,
		GroupID: group.ID,
	})
	if err != nil {
		t.Fatalf("claiming a gap row after a dismissal collided: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("claimed %d rows, want 1", claimed)
	}
	var seq pgtype.Int8
	if err := f.pool.QueryRow(ctx,
		`SELECT event_seq FROM inbox_item WHERE id = $1`, gap).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	if seq.Int64 != 3 {
		t.Fatalf("gap row took seq %d, want 3 — 2 is still occupied by the dismissed row", seq.Int64)
	}
}
