package inboxv2

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The group and the legacy rows must describe the same inbox at every step.
//
// This is the property the whole refactor rests on: v1 clients keep reading
// inbox_item and must never see something the group disagrees with, because
// mobile stays on v1 indefinitely and web can be switched back at any moment.

// v1View is what a v1 client computes from the raw rows: fold by issue, newest
// row wins, unread if that row is unread.
type v1View struct {
	unread   bool
	archived bool
	repID    pgtype.UUID
}

func (f *fixture) v1View(t *testing.T) v1View {
	t.Helper()
	var v v1View
	err := f.pool.QueryRow(context.Background(), `
SELECT read = false, archived, id
FROM inbox_item
WHERE workspace_id = $1 AND recipient_id = $2 AND issue_id = $3
ORDER BY created_at DESC, id DESC
LIMIT 1`, f.ws, f.user, f.issue).Scan(&v.unread, &v.archived, &v.repID)
	if err != nil {
		t.Fatalf("v1 view: %v", err)
	}
	return v
}

func (f *fixture) assertAgree(t *testing.T, step string, groupID pgtype.UUID) {
	t.Helper()
	g := f.group(t, groupID)
	v1 := f.v1View(t)

	if IsUnread(g) != v1.unread {
		t.Fatalf("%s: group unread=%v but v1 renders unread=%v", step, IsUnread(g), v1.unread)
	}
	if g.ArchivedAt.Valid != v1.archived {
		t.Fatalf("%s: group archived=%v but v1 renders archived=%v", step, g.ArchivedAt.Valid, v1.archived)
	}
	if g.LatestEventID != v1.repID {
		t.Fatalf("%s: group and v1 elect different representative rows", step)
	}
}

// The full lifecycle the spec calls out: consecutive new events, a late read, an
// archive, and a new event waking the archived group back up.
func TestGroupAndLegacyRowsAgreeThroughTheWholeLifecycle(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	first, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	gid := first.Group.ID
	f.assertAgree(t, "after the first delivery", gid)

	// Consecutive new events.
	if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	f.assertAgree(t, "after three deliveries", gid)

	// A read arriving LATE — it reports the first event, long after the third
	// landed. It must not mark the newer events read.
	g := f.group(t, gid)
	if _, err := f.w.MarkGroupRead(ctx, g, first.Item.EventSeq.Int64, g.StateVersion, f.now); err != nil {
		t.Fatal(err)
	}
	if !IsUnread(f.group(t, gid)) {
		t.Fatal("a read for an older event marked newer ones read")
	}
	f.assertAgree(t, "after a late read", gid)

	// A current read clears it.
	g = f.group(t, gid)
	if _, err := f.w.MarkGroupRead(ctx, g, second.Item.EventSeq.Int64, g.StateVersion, f.now); err != nil {
		t.Fatal(err)
	}
	if IsUnread(f.group(t, gid)) {
		t.Fatal("a read for the head left the group unread")
	}
	f.assertAgree(t, "after a current read", gid)

	// Archive, then a new event wakes it.
	g = f.group(t, gid)
	if _, err := f.w.ArchiveGroup(ctx, g, f.now); err != nil {
		t.Fatal(err)
	}
	f.assertAgree(t, "after archive", gid)

	if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	woken := f.group(t, gid)
	if woken.ArchivedAt.Valid {
		t.Fatal("a new event must pull an archived group back into the inbox")
	}
	if !IsUnread(woken) {
		t.Fatal("the event that woke the group must be unread")
	}
	f.assertAgree(t, "after a new event woke the archived group", gid)
}

// Manual unread is the user's decision and outranks an automatic read that was
// issued before it and arrives after it. That distinction needs state_version:
// both requests carry the same observed_seq.
func TestManualUnreadSurvivesAStaleAutomaticRead(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	g := f.group(t, res.Group.ID)
	staleVersion := g.StateVersion

	// The user reads it, then deliberately marks it unread.
	if _, err := f.w.MarkGroupRead(ctx, g, g.LatestSeq, g.StateVersion, f.now); err != nil {
		t.Fatal(err)
	}
	g = f.group(t, res.Group.ID)
	if _, err := f.w.MarkGroupUnread(ctx, g, f.now); err != nil {
		t.Fatal(err)
	}
	if !IsUnread(f.group(t, res.Group.ID)) {
		t.Fatal("mark unread did not take")
	}

	// A read from a tab that had not seen the unread yet: same seq, stale
	// version. It must not undo the user.
	g = f.group(t, res.Group.ID)
	if _, err := f.w.MarkGroupRead(ctx, g, g.LatestSeq, staleVersion, f.now); err != nil {
		t.Fatal(err)
	}
	if !IsUnread(f.group(t, res.Group.ID)) {
		t.Fatal("a stale automatic read silently undid the user's mark-unread")
	}
	f.assertAgree(t, "after a stale read raced manual unread", res.Group.ID)
}

// The v1 adapters: a legacy endpoint's write has to move the group too, or the
// two surfaces diverge for anyone whose other client is on v2.
func TestV1AdaptersMoveTheGroup(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	item := res.Item

	if _, err := f.w.MarkItemRead(ctx, f.ws, f.user, item.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if IsUnread(f.group(t, res.Group.ID)) {
		t.Fatal("v1 mark-read did not clear the group's unread")
	}

	if _, err := f.w.MarkItemUnread(ctx, f.ws, f.user, item.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if !IsUnread(f.group(t, res.Group.ID)) {
		t.Fatal("v1 mark-unread did not reach the group")
	}

	if _, err := f.w.ArchiveItem(ctx, f.ws, f.user, item.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if !f.group(t, res.Group.ID).ArchivedAt.Valid {
		t.Fatal("v1 archive did not reach the group")
	}
	f.assertAgree(t, "after v1 archive", res.Group.ID)

	if _, err := f.w.UnarchiveItem(ctx, f.ws, f.user, item.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if f.group(t, res.Group.ID).ArchivedAt.Valid {
		t.Fatal("v1 unarchive did not reach the group")
	}
	f.assertAgree(t, "after v1 unarchive", res.Group.ID)
}

// A v1 write that bypasses the adapters — the rollback window — moves the rows
// and leaves the group behind. Reconcile repairs it, with the ROWS winning,
// because the rows are what the user actually saw and acted on.
func TestReconcileRepairsDriftWithRowsWinning(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if !IsUnread(f.group(t, res.Group.ID)) {
		t.Fatal("setup: the delivery should be unread")
	}

	// The raw v1 write, exactly as an old client with the adapters off makes it.
	if _, err := f.pool.Exec(ctx,
		`UPDATE inbox_item SET read = true, archived = true WHERE group_id = $1`, res.Group.ID); err != nil {
		t.Fatal(err)
	}

	report, err := f.w.Reconcile(ctx, f.ws, 100, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if report.GroupsRepaired == 0 {
		t.Fatal("reconcile did not notice the drift")
	}
	g := f.group(t, res.Group.ID)
	if IsUnread(g) {
		t.Fatal("rows said read; the group must follow")
	}
	if !g.ArchivedAt.Valid {
		t.Fatal("rows said archived; the group must follow")
	}
	f.assertAgree(t, "after reconcile", res.Group.ID)

	// Idempotent: a second pass over a healthy inbox finds nothing.
	again, err := f.w.Reconcile(ctx, f.ws, 100, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if again.GroupsRepaired != 0 {
		t.Fatalf("a second pass repaired %d groups; reconcile is not idempotent", again.GroupsRepaired)
	}
}

// Cross-tenant isolation on every group-addressed operation.
func TestGroupReadsAreScopedToTheirOwner(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	other := f.addMember(t)

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.GetInboxGroupForRecipient(ctx, db.GetInboxGroupForRecipientParams{
		ID: res.Group.ID, WorkspaceID: f.ws, RecipientID: other,
	}); err == nil {
		t.Fatal("another member read a group that is not theirs")
	}
	if _, err := f.w.MarkItemRead(ctx, f.ws, other, res.Item.ID, f.now); err == nil {
		t.Fatal("another member wrote to a row that is not theirs")
	}
}

// The mobile contract. apps/mobile parses details as
// z.record(z.string(), z.string()) through parseWithFallback, which validates
// the whole response at once — so one non-string value anywhere blanks the
// user's ENTIRE inbox, not just the offending row.
func TestDetailsValuesMustAllBeStrings(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	d := f.delivery("v1:" + uuid.NewString())
	d.Details = mustJSON(t, StringDetails(map[string]string{
		"failed_runs": "3",
		"total_runs":  "5",
		"reason":      "auto_paused_high_failure_rate",
	}))
	res, err := f.w.Deliver(ctx, d, f.now)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(res.Item.Details, &raw); err != nil {
		t.Fatalf("details is not an object: %v", err)
	}
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			t.Fatalf("details[%q] is %T, not a string — this blanks the whole mobile inbox", k, v)
		}
	}
}

// The negative half: a number in details is exactly what mobile cannot take.
// StringDetails exists so producers cannot express one by accident.
func TestStringDetailsCannotCarryANonString(t *testing.T) {
	got := StringDetails(map[string]string{"failed_runs": "3"})
	encoded := mustJSON(t, got)
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["failed_runs"].(string); !ok {
		t.Fatal("StringDetails must serialise every value as a JSON string")
	}
	if StringDetails(nil) == nil {
		t.Fatal("nil details must serialise as {} rather than null: mobile parses an object")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
