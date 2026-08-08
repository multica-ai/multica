package inboxv2

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The write path is enforced by Postgres — the group row lock that serialises
// sequence allocation, the partial unique index that arbitrates duplicate
// deliveries, the mirror invariant expressed as one UPDATE. A mock would only
// assert that the Go code calls the queries it calls.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("skipping DB test: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping DB test: database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type fixture struct {
	w     *Writer
	q     *db.Queries
	pool  *pgxpool.Pool
	ws    pgtype.UUID
	user  pgtype.UUID
	issue pgtype.UUID
	now   time.Time
}

func newFixture(t *testing.T, gateOpen bool) *fixture {
	t.Helper()
	pool := testPool(t)
	q := db.New(pool)
	ctx := context.Background()

	f := &fixture{
		w: NewWriter(q, pool), q: q, pool: pool,
		ws:    uuidVal(uuid.New()),
		user:  uuidVal(uuid.New()),
		issue: uuidVal(uuid.New()),
		now:   time.Now().UTC().Truncate(time.Microsecond),
	}

	// inbox_item predates the no-FK convention and still references workspace
	// and issue, so both have to exist for a delivery to insert.
	slug := "inboxv2-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace (id, name, slug) VALUES ($1, 'inbox v2 write', $2)`,
		f.ws, slug); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	// member is a real boundary now: the lazy migration and the claim both join
	// it, so a recipient who is not a member of the workspace has no history to
	// migrate. Seeding user + member is part of representing a real recipient.
	if _, err := pool.Exec(ctx,
		`INSERT INTO "user" (id, email, name) VALUES ($1, $2, 'inbox v2 tester')`,
		f.user, "inboxv2-"+uuid.NewString()[:8]+"@example.test"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		f.ws, f.user); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	creator := uuidVal(uuid.New())
	if _, err := pool.Exec(ctx, `
INSERT INTO issue (id, workspace_id, title, status, priority, creator_type, creator_id, position, number)
VALUES ($1, $2, 'inbox v2 write path', 'todo', 'medium', 'member', $3, 0, 1)
`, f.issue, f.ws, creator); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	// The gate is global, so restore whatever it was rather than assuming off.
	prev, err := q.GetInboxV2WriteEnabled(ctx)
	if err != nil {
		t.Fatalf("read gate: %v", err)
	}
	if err := q.SetInboxV2WriteEnabled(ctx, gateOpen); err != nil {
		t.Fatalf("set gate: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = q.SetInboxV2WriteEnabled(bg, prev)
		_, _ = pool.Exec(bg, `DELETE FROM inbox_item WHERE workspace_id = $1`, f.ws)
		_, _ = pool.Exec(bg, `DELETE FROM inbox_group WHERE workspace_id = $1`, f.ws)
		_, _ = pool.Exec(bg, `DELETE FROM issue WHERE workspace_id = $1`, f.ws)
		_, _ = pool.Exec(bg, `DELETE FROM member WHERE workspace_id = $1`, f.ws)
		_, _ = pool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, f.ws)
		_, _ = pool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, f.user)
	})
	return f
}

func uuidVal(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }

func (f *fixture) delivery(key string) Delivery {
	return Delivery{
		WorkspaceID: f.ws,
		RecipientID: f.user,
		SourceKind:  SourceIssue,
		SourceID:    f.issue,
		Type:        "new_comment",
		Severity:    "info",
		IssueID:     f.issue,
		Title:       "write path test",
		ActorType:   pgtype.Text{String: "member", Valid: true},
		Details:     []byte(`{}`),
		TargetKind:  pgtype.Text{String: "comment", Valid: true},
		TargetID:    uuidVal(uuid.New()),
		DeliveryKey: pgtype.Text{String: key, Valid: true},
	}
}

func (f *fixture) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), q, f.ws).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// --- the gate ---------------------------------------------------------------

// Closed is the deploy state, and closed must mean the pre-v2 write byte for
// byte: the legacy row lands, no group exists, and the new columns stay NULL.
// That is what lets this code ship long before the switch is touched.
func TestGateClosedWritesLegacyRowOnly(t *testing.T) {
	f := newFixture(t, false)
	res, err := f.w.Deliver(context.Background(), f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if res.GateOpen {
		t.Fatal("the gate is closed")
	}
	if res.Item.GroupID.Valid || res.Item.EventSeq.Valid || res.Item.DeliveryKey.Valid {
		t.Fatal("a closed gate must leave the v2 columns NULL")
	}
	if got := f.count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id=$1`); got != 1 {
		t.Fatalf("legacy rows = %d, want 1", got)
	}
	if got := f.count(t, `SELECT count(*) FROM inbox_group WHERE workspace_id=$1`); got != 0 {
		t.Fatalf("groups = %d, want 0", got)
	}
}

// --- sequence allocation ----------------------------------------------------

func TestGateOpenAllocatesGaplessSequence(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
		if err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
		if res.Item.EventSeq.Int64 != int64(i) {
			t.Fatalf("delivery %d got seq %d", i, res.Item.EventSeq.Int64)
		}
		if res.Group.LatestSeq != int64(i) {
			t.Fatalf("delivery %d left latest_seq %d", i, res.Group.LatestSeq)
		}
	}
	if got := f.count(t, `SELECT count(*) FROM inbox_group WHERE workspace_id=$1`); got != 1 {
		t.Fatalf("five deliveries about one issue produced %d groups, want 1", got)
	}
}

// created_at must be strictly increasing within a group even when the clock is
// not: the legacy endpoints sort by it, so a tie or a backwards step would make
// v1 and v2 disagree about which row represents the group.
func TestCreatedAtIsMonotonicWithinAGroup(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	frozen := f.now
	var last time.Time
	for i := 0; i < 4; i++ {
		// Same instant every time, and then a clock that has gone backwards.
		at := frozen
		if i == 3 {
			at = frozen.Add(-time.Hour)
		}
		res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), at)
		if err != nil {
			t.Fatal(err)
		}
		got := res.Item.CreatedAt.Time
		if !got.After(last) && i > 0 {
			t.Fatalf("created_at went backwards or tied: %v then %v", last, got)
		}
		last = got
	}

	// The database order must agree with the sequence order.
	rows, err := f.pool.Query(ctx, `
SELECT event_seq FROM inbox_item WHERE workspace_id=$1 ORDER BY created_at, id`, f.ws)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, s)
	}
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Fatalf("created_at order disagrees with event_seq: %v", seqs)
		}
	}
}

// --- idempotency ------------------------------------------------------------

func TestRepeatedDeliveryIsDeduplicated(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	d := f.delivery("v1:" + uuid.NewString())

	first, err := f.w.Deliver(ctx, d, f.now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.w.Deliver(ctx, d, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Deduplicated {
		t.Fatal("a retry must be reported as deduplicated so the caller suppresses its websocket event")
	}
	if second.Item.ID != first.Item.ID {
		t.Fatal("a retry must return the original row")
	}
	if second.Group.LatestSeq != 1 {
		t.Fatalf("a retry advanced latest_seq to %d", second.Group.LatestSeq)
	}
	if got := f.count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id=$1`); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

// Two transactions can both miss the probe. The partial unique index decides;
// the loser rolls back whole and re-reads the winner.
func TestConcurrentSameDeliveryKeyProducesOneRow(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	d := f.delivery("v1:" + uuid.NewString())

	const racers = 8
	var wg sync.WaitGroup
	results := make([]Result, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.w.Deliver(ctx, d, f.now)
		}(i)
	}
	close(start)
	wg.Wait()

	fresh := 0
	var id pgtype.UUID
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if !results[i].Deduplicated {
			fresh++
		}
		if !id.Valid {
			id = results[i].Item.ID
		} else if results[i].Item.ID != id {
			t.Fatal("racers observed different rows for one delivery key")
		}
	}
	if fresh != 1 {
		t.Fatalf("exactly one racer may create the row, got %d", fresh)
	}
	if got := f.count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id=$1`); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

// Concurrent distinct deliveries into one group serialise on the group row
// lock, producing a dense sequence rather than duplicates or holes.
func TestConcurrentDistinctDeliveriesSerialise(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	seqs := make([]int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := f.delivery("v1:" + uuid.NewString())
			<-start
			res, err := f.w.Deliver(ctx, d, f.now)
			errs[i] = err
			if err == nil {
				seqs[i] = res.Item.EventSeq.Int64
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Fatalf("sequence is not dense: %v", seqs)
		}
	}
}

// --- the mirror invariant ---------------------------------------------------

// v1 clients read `read` and `archived`, and they fold a group down to one row.
// So only the representative row goes unread: marking the whole history unread
// would make the old raw-row count report a group as N unread items.
func TestMirrorMarksOnlyTheRepresentativeRowUnread(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := f.pool.Query(ctx, `
SELECT event_seq, read, archived FROM inbox_item WHERE workspace_id=$1 ORDER BY event_seq`, f.ws)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		seq            int64
		read, archived bool
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.seq, &r.read, &r.archived); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3", len(got))
	}
	for _, r := range got {
		wantRead := r.seq != 3 // only the newest is the representative
		if r.read != wantRead {
			t.Errorf("seq %d: read = %v, want %v", r.seq, r.read, wantRead)
		}
		if r.archived {
			t.Errorf("seq %d: archived without an archived group", r.seq)
		}
	}
}

// Archiving the group must show up on every row, because v1 clients filter on
// the row's own boolean.
func TestMirrorReflectsGroupArchive(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
UPDATE inbox_group SET archived_at = now(), read_through_seq = latest_seq WHERE id = $1`,
		res.Group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.RefreshInboxItemMirror(ctx, res.Group.ID); err != nil {
		t.Fatal(err)
	}

	var archived, read bool
	if err := f.pool.QueryRow(ctx,
		`SELECT archived, read FROM inbox_item WHERE id = $1`, res.Item.ID).Scan(&archived, &read); err != nil {
		t.Fatal(err)
	}
	if !archived {
		t.Fatal("archiving the group must mirror onto its rows")
	}
	if !read {
		t.Fatal("an archived group is handled, so its rows must read as read")
	}
}

// The IS DISTINCT FROM guard is what keeps a delivery from rewriting the whole
// group. Without it a busy issue turns one insert into an update of its entire
// history, and every one of those is a row version to vacuum.
func TestMirrorRefreshOnlyTouchesRowsThatChange(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if _, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now); err != nil {
			t.Fatal(err)
		}
	}
	res, err := f.q.ListUnclaimedInboxSources(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("delivered rows must already be claimed, got %d unclaimed sources", len(res))
	}

	var groupID pgtype.UUID
	if err := f.pool.QueryRow(ctx,
		`SELECT id FROM inbox_group WHERE workspace_id = $1`, f.ws).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	// State is already consistent, so a second refresh must touch nothing.
	touched, err := f.q.RefreshInboxItemMirror(ctx, groupID)
	if err != nil {
		t.Fatal(err)
	}
	if touched != 0 {
		t.Fatalf("a no-op refresh rewrote %d rows", touched)
	}
}

// --- representative recomputation ------------------------------------------

// Event-level dismissal still exists: an issue completing retires its stale
// task_failed row. If that row was the representative, latest_* has to fall
// BACK to the newest survivor and the cursor must not be left above the head.
func TestRepresentativeRecomputesDownwardAfterDismissal(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()

	var last Result
	for i := 0; i < 3; i++ {
		res, err := f.w.Deliver(ctx, f.delivery("v1:"+uuid.NewString()), f.now)
		if err != nil {
			t.Fatal(err)
		}
		last = res
	}
	// The user had read everything.
	if _, err := f.pool.Exec(ctx,
		`UPDATE inbox_group SET read_through_seq = latest_seq WHERE id = $1`, last.Group.ID); err != nil {
		t.Fatal(err)
	}

	// Dismiss the representative row.
	if _, err := f.pool.Exec(ctx, `DELETE FROM inbox_item WHERE id = $1`, last.Item.ID); err != nil {
		t.Fatal(err)
	}

	group, err := f.q.RecomputeInboxGroupRepresentative(ctx, db.RecomputeInboxGroupRepresentativeParams{
		ID:  last.Group.ID,
		Now: pgtype.Timestamptz{Time: f.now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.LatestSeq != 2 {
		t.Fatalf("latest_seq = %d, want it to fall back to 2", group.LatestSeq)
	}
	if group.ReadThroughSeq > group.LatestSeq {
		t.Fatalf("cursor %d is above the new head %d — the group would read as permanently read",
			group.ReadThroughSeq, group.LatestSeq)
	}
	if IsUnread(group) {
		t.Fatal("removing an already-read event must not resurrect the group as unread")
	}
}
