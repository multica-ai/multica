package inboxv2

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regressions for the five blockers the pre-launch review found in the write
// path. Each reproduces the concrete sequence, not the property in the abstract.

// Blocker 1. Historical rows carry only `archived` and cannot say whether the
// user archived the issue or the system retired a stale task_failed. The frozen
// rule reads the SET: every row archived means the SOURCE was archived, so the
// group inherits it and no row is individually dismissed.
func TestFullyArchivedHistoryBecomesAnArchivedGroupNotDismissals(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	a := f.insertLegacyRow(t, f.issue, f.now.Add(-2*time.Hour), "new_comment")
	b := f.insertLegacyRow(t, f.issue, f.now.Add(-time.Hour), "new_comment")
	f.archiveRows(t, a, b)

	group := f.migrate(t)
	if !group.ArchivedAt.Valid {
		t.Fatal("a source whose every row was archived must migrate to an archived group")
	}
	for _, id := range []pgtype.UUID{a, b} {
		if f.dismissedAt(t, id).Valid {
			t.Fatal("a wholly-archived source is a group archive, not per-row dismissals")
		}
	}

	// And the point of the distinction: unarchiving restores all of it.
	if _, err := f.w.UnarchiveGroup(ctx, group, f.now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []pgtype.UUID{a, b} {
		if f.archived(t, id) {
			t.Fatal("unarchiving the group must restore every row it archived")
		}
	}
}

// The other half of the same rule: a partially archived source could only have
// been produced by dismissal, so the archived rows become dismissals and
// unarchiving must NOT bring them back.
func TestPartiallyArchivedHistoryBecomesDismissals(t *testing.T) {
	f := newFixture(t, true)

	stale := f.insertLegacyRow(t, f.issue, f.now.Add(-2*time.Hour), "task_failed")
	live := f.insertLegacyRow(t, f.issue, f.now.Add(-time.Hour), "new_comment")
	f.archiveRows(t, stale)

	group := f.migrate(t)
	if group.ArchivedAt.Valid {
		t.Fatal("a source with a live row must not migrate to an archived group")
	}
	if !f.dismissedAt(t, stale).Valid {
		t.Fatal("the archived row of a partially archived source is a dismissal")
	}
	if f.dismissedAt(t, live).Valid {
		t.Fatal("the live row must not be dismissed")
	}
	if group.LatestEventID != live {
		t.Fatal("the representative must be the surviving row, not the dismissed one")
	}

	// The resurrection this whole column exists to prevent.
	if _, err := f.w.ArchiveGroup(context.Background(), group, f.now); err != nil {
		t.Fatal(err)
	}
	reloaded := f.group(t, group.ID)
	if _, err := f.w.UnarchiveGroup(context.Background(), reloaded, f.now); err != nil {
		t.Fatal(err)
	}
	if !f.archived(t, stale) {
		t.Fatal("un-archiving the group resurrected a dismissed task_failed row")
	}
	if f.archived(t, live) {
		t.Fatal("un-archiving must restore the row that was never dismissed")
	}
}

// Blocker 2. The real dismissal path — an issue reaching a terminal status —
// must repair the groups it touches in the same transaction. Before, the rows
// moved and the group went on pointing at the row the user was just shown the
// back of.
func TestDismissIssueTypeRepairsTheGroupInTheSameCall(t *testing.T) {
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

	// No manual recompute anywhere: this is the whole production path.
	rows, err := f.w.DismissIssueType(ctx, f.ws, f.issue, "task_failed", f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the dismissal must report the recipients whose inbox changed")
	}

	group := f.group(t, failedRes.Group.ID)
	if group.LatestEventID != first.Item.ID {
		t.Fatal("the group still points at the dismissed row — the dismissal did not repair it")
	}
	if group.LatestSeq != first.Item.EventSeq.Int64 {
		t.Fatalf("latest_seq = %d, want the survivor's %d", group.LatestSeq, first.Item.EventSeq.Int64)
	}
	// And the mirror followed, so v1 clients see it too.
	if !f.archived(t, failedRes.Item.ID) {
		t.Fatal("the dismissed row must read as archived to v1")
	}
	if f.archived(t, first.Item.ID) {
		t.Fatal("the surviving row must stay active")
	}
}

// Blocker 3. The lazy migration is user-level, so a workspace the user has left
// must not contribute to it — not to the source list, not to the budget, and
// not to a claim.
func TestLazyMigrationIgnoresWorkspacesTheUserHasLeft(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	// A row in a workspace this user is NOT a member of. Leaving does not
	// reliably delete these, so they are the shape real stale data takes.
	stale := f.insertForeignWorkspaceRow(t)

	sources, err := f.q.ListUnclaimedInboxSources(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s.WorkspaceID == stale {
			t.Fatal("the source scan reached a workspace the user has left")
		}
	}
	count, err := f.q.CountUnclaimedInboxItems(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale rows in a left workspace counted %d against the migration budget", count)
	}

	ready, err := f.w.EnsureGroups(ctx, f.user, f.now)
	if err != nil || !ready {
		t.Fatalf("EnsureGroups(ready=%v, err=%v): stale history must not block a live user", ready, err)
	}
	var groups int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM inbox_group WHERE workspace_id = $1`, stale).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if groups != 0 {
		t.Fatal("the migration built groups in a workspace the user has left")
	}
}

// Blocker 4. A first delivery to a source with a large history must not drag
// all of it through the notification transaction. The history is deferred, and
// reconcile finishes it — numbering it BELOW the delivery that went first, so
// event_seq still agrees with created_at about which row is the head.
func TestOversizedHistoryIsDeferredThenClaimedBelowTheDelivery(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	for i := 0; i < MaxInlineClaim+5; i++ {
		f.insertLegacyRow(t, f.issue, f.now.Add(-time.Duration(MaxInlineClaim+5-i)*time.Minute), "new_comment")
	}

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.EventSeq.Int64 != 1 {
		t.Fatalf("deferred claim: delivery took seq %d, want 1 on an empty group", res.Item.EventSeq.Int64)
	}
	if f.unclaimedCount(t) != MaxInlineClaim+5 {
		t.Fatal("the oversized history must be left for reconcile, not claimed inline")
	}

	if _, err := f.w.Reconcile(ctx, f.ws, 500, f.now); err != nil {
		t.Fatal(err)
	}
	if got := f.unclaimedCount(t); got != 0 {
		t.Fatalf("%d rows still unclaimed after reconcile", got)
	}

	// The invariant that actually matters: the highest sequence is the newest
	// row, so v1 (created_at) and v2 (event_seq) elect the same representative.
	group := f.group(t, res.Group.ID)
	if group.LatestEventID != res.Item.ID {
		t.Fatal("reconcile moved the representative off the newest event")
	}
	var maxSeqID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
SELECT id FROM inbox_item WHERE group_id = $1 ORDER BY event_seq DESC LIMIT 1`,
		group.ID).Scan(&maxSeqID); err != nil {
		t.Fatal(err)
	}
	var newestID pgtype.UUID
	if err := f.pool.QueryRow(ctx, `
SELECT id FROM inbox_item WHERE group_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`,
		group.ID).Scan(&newestID); err != nil {
		t.Fatal(err)
	}
	if maxSeqID != newestID {
		t.Fatal("event_seq and created_at disagree about the head — v1 and v2 would render different rows")
	}
}

// Blocker 5. A delivery key is mandatory once the gate is open. Idempotency
// that a producer can silently omit is idempotency that fails only in
// production, after a retry.
func TestDeliveryWithoutAKeyIsRejectedWhenTheGateIsOpen(t *testing.T) {
	f := newFixture(t, true)
	d := f.delivery("")
	d.DeliveryKey = pgtype.Text{}
	if _, err := f.w.Deliver(context.Background(), d, f.now); err == nil {
		t.Fatal("a keyless delivery must be rejected once the gate is open")
	} else if !strings.Contains(err.Error(), "delivery key required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Two recipients of the same event each get their own notification. With a
// globally-unique key the second one is silently dropped, which reads as a
// missing notification rather than as an error anywhere.
func TestTheSameEventReachesEveryRecipient(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	other := f.addMember(t)

	// The composition producers actually use, for one shared underlying event.
	first := f.delivery("")
	first.DeliveryKey = DeliveryKey(uuidStr(f.ws), uuidStr(f.user), "new_comment", uuidStr(f.issue), "comment-1")
	second := f.delivery("")
	second.RecipientID = other
	second.DeliveryKey = DeliveryKey(uuidStr(f.ws), uuidStr(other), "new_comment", uuidStr(f.issue), "comment-1")

	a, err := f.w.Deliver(ctx, first, f.now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.w.Deliver(ctx, second, f.now)
	if err != nil {
		t.Fatalf("the second recipient's copy was rejected: %v", err)
	}
	if b.Deduplicated {
		t.Fatal("a different recipient's copy was treated as a duplicate")
	}
	if a.Group.ID == b.Group.ID {
		t.Fatal("two people must not share one group")
	}
}

// DeliveryKey must be a function of the recipient, so no producer can build a
// key that addresses someone else's notification.
func TestDeliveryKeyVariesWithEveryIdentityComponent(t *testing.T) {
	base := DeliveryKey("ws", "user", "new_comment", "issue", "anchor")
	for _, v := range []struct {
		name string
		key  pgtype.Text
	}{
		{"workspace", DeliveryKey("ws2", "user", "new_comment", "issue", "anchor")},
		{"recipient", DeliveryKey("ws", "user2", "new_comment", "issue", "anchor")},
		{"type", DeliveryKey("ws", "user", "mentioned", "issue", "anchor")},
		{"anchor", DeliveryKey("ws", "user", "new_comment", "issue", "anchor2")},
	} {
		if v.key.String == base.String {
			t.Errorf("delivery key does not depend on %s", v.name)
		}
	}
	// And separator injection cannot make two different identities collide.
	if DeliveryKey("a", "b:c", "t").String == DeliveryKey("a:b", "c", "t").String {
		t.Error("delivery key parts can be re-split into a different identity")
	}
	if !strings.HasPrefix(base.String, "v1:") {
		t.Error("delivery keys must carry their composition version")
	}
}

// --- fixture helpers -------------------------------------------------------

func uuidStr(u pgtype.UUID) string { return uuid.UUID(u.Bytes).String() }

func (f *fixture) migrate(t *testing.T) db.InboxGroup {
	t.Helper()
	ready, err := f.w.EnsureGroups(context.Background(), f.user, f.now)
	if err != nil || !ready {
		t.Fatalf("EnsureGroups(ready=%v, err=%v)", ready, err)
	}
	group, err := f.q.FindInboxGroupBySource(context.Background(), db.FindInboxGroupBySourceParams{
		WorkspaceID: f.ws, RecipientID: f.user,
		SourceKind: string(SourceIssue), SourceID: f.issue,
	})
	if err != nil {
		t.Fatalf("group was not created: %v", err)
	}
	return group
}

func (f *fixture) group(t *testing.T, id pgtype.UUID) db.InboxGroup {
	t.Helper()
	g, err := f.q.GetInboxGroupForRecipient(context.Background(), db.GetInboxGroupForRecipientParams{
		ID: id, WorkspaceID: f.ws, RecipientID: f.user,
	})
	if err != nil {
		t.Fatalf("reload group: %v", err)
	}
	return g
}

func (f *fixture) archiveRows(t *testing.T, ids ...pgtype.UUID) {
	t.Helper()
	for _, id := range ids {
		if _, err := f.pool.Exec(context.Background(),
			`UPDATE inbox_item SET archived = true WHERE id = $1`, id); err != nil {
			t.Fatalf("archive row: %v", err)
		}
	}
}

func (f *fixture) dismissedAt(t *testing.T, id pgtype.UUID) pgtype.Timestamptz {
	t.Helper()
	var at pgtype.Timestamptz
	if err := f.pool.QueryRow(context.Background(),
		`SELECT dismissed_at FROM inbox_item WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatalf("read dismissed_at: %v", err)
	}
	return at
}

func (f *fixture) archived(t *testing.T, id pgtype.UUID) bool {
	t.Helper()
	var archived bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT archived FROM inbox_item WHERE id = $1`, id).Scan(&archived); err != nil {
		t.Fatalf("read archived: %v", err)
	}
	return archived
}

func (f *fixture) unclaimedCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND group_id IS NULL`, f.ws).Scan(&n); err != nil {
		t.Fatalf("count unclaimed: %v", err)
	}
	return n
}

// addMember creates a second member of the same workspace.
func (f *fixture) addMember(t *testing.T) pgtype.UUID {
	t.Helper()
	id := uuidVal(uuid.New())
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO "user" (id, email, name) VALUES ($1, $2, 'second member')`,
		id, "inboxv2-"+uuid.NewString()[:8]+"@example.test"); err != nil {
		t.Fatalf("seed second user: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		f.ws, id); err != nil {
		t.Fatalf("seed second member: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = f.pool.Exec(bg, `DELETE FROM inbox_item WHERE recipient_id = $1`, id)
		_, _ = f.pool.Exec(bg, `DELETE FROM inbox_group WHERE recipient_id = $1`, id)
		_, _ = f.pool.Exec(bg, `DELETE FROM member WHERE user_id = $1`, id)
		_, _ = f.pool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, id)
	})
	return id
}

// insertForeignWorkspaceRow leaves this user an inbox row in a workspace they
// are NOT a member of — the shape stale history takes after someone leaves.
func (f *fixture) insertForeignWorkspaceRow(t *testing.T) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	ws := uuidVal(uuid.New())
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO workspace (id, name, slug) VALUES ($1, 'left behind', $2)`,
		ws, "left-"+uuid.NewString()[:8]); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, title, read, archived, created_at)
VALUES ($1, 'member', $2, 'new_comment', 'info', 'stale', true, false, $3)`,
		ws, f.user, f.now.Add(-72*time.Hour)); err != nil {
		t.Fatalf("seed foreign row: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = f.pool.Exec(bg, `DELETE FROM inbox_item WHERE workspace_id = $1`, ws)
		_, _ = f.pool.Exec(bg, `DELETE FROM inbox_group WHERE workspace_id = $1`, ws)
		_, _ = f.pool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, ws)
	})
	return ws
}

// Lifecycle. inbox_group carries no foreign keys (repo rule), so every parent's
// disappearance has to remove its groups explicitly — and remove only its own.
func TestLifecycleCleanupDeletesCleanlyAndDoesNotOverreach(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	other := f.addMember(t)

	mine, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	theirs := f.delivery("")
	theirs.RecipientID = other
	theirs.DeliveryKey = DeliveryKey(uuidStr(f.ws), uuidStr(other), "new_comment", uuidStr(f.issue), "x")
	theirsRes, err := f.w.Deliver(ctx, theirs, f.now)
	if err != nil {
		t.Fatal(err)
	}

	// A member leaving takes their groups and nobody else's.
	if err := f.w.PurgeMember(ctx, f.ws, other); err != nil {
		t.Fatal(err)
	}
	if f.groupExists(t, theirsRes.Group.ID) {
		t.Fatal("the departed member's group survived")
	}
	if !f.groupExists(t, mine.Group.ID) {
		t.Fatal("purging one member removed another member's group")
	}

	// Deleting the issue takes every group about it.
	if err := f.w.PurgeIssue(ctx, f.ws, f.issue); err != nil {
		t.Fatal(err)
	}
	if f.groupExists(t, mine.Group.ID) {
		t.Fatal("a group for a deleted issue survived")
	}
}

// Orphan groups — every event deleted out from under them — render as nothing
// while still occupying a page of results. Nothing else removes them.
func TestReconcileRemovesOrphanGroups(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM inbox_item WHERE group_id = $1`, res.Group.ID); err != nil {
		t.Fatal(err)
	}
	report, err := f.w.Reconcile(ctx, f.ws, 100, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if report.OrphansRemoved == 0 {
		t.Fatal("reconcile left an orphan group behind")
	}
	if f.groupExists(t, res.Group.ID) {
		t.Fatal("the orphan group survived reconcile")
	}
}

func (f *fixture) groupExists(t *testing.T, id pgtype.UUID) bool {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_group WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count group: %v", err)
	}
	return n > 0
}
