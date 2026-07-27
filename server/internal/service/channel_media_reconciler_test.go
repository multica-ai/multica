package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeObjectDeleter struct {
	mu       sync.Mutex
	deleted  []string
	err      error
	onDelete func(key string)
}

func (f *fakeObjectDeleter) DeleteObject(_ context.Context, key string) error {
	f.mu.Lock()
	hook := f.onDelete
	f.mu.Unlock()
	if hook != nil {
		hook(key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeObjectDeleter) deletedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

type reconcilerFixture struct {
	pool        *pgxpool.Pool
	workspaceID string
	sessionID   string
	messageID   string
}

func seedReconcilerFixture(t *testing.T, pool *pgxpool.Pool) reconcilerFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	f := reconcilerFixture{pool: pool}

	var userID, runtimeID, agentID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reconciler test", fmt.Sprintf("media-reconciler-%d@multica.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description) VALUES ($1, $2, '') RETURNING id`,
		"Reconciler test", fmt.Sprintf("media-reconciler-%d", suffix)).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, f.workspaceID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, owner_id)
		VALUES ($1, $2, 'local', 'multica_daemon', $3) RETURNING id`,
		f.workspaceID, fmt.Sprintf("media-reconciler-rt-%d", suffix), userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4) RETURNING id`,
		f.workspaceID, fmt.Sprintf("media-reconciler-agent-%d", suffix), runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'reconciler test') RETURNING id`,
		f.workspaceID, agentID, userID).Scan(&f.sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_ingested)
		VALUES ($1, 'user', '[Image]', TRUE) RETURNING id`, f.sessionID).Scan(&f.messageID); err != nil {
		t.Fatalf("create chat message: %v", err)
	}
	return f
}

// seedLedgerRow inserts a ledger row with a controllable age and state.
func (f reconcilerFixture) seedLedgerRow(t *testing.T, key, url, state string, age time.Duration) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO channel_media_pending_object (storage_key, workspace_id, chat_message_id, storage_url, state, created_at, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, now() - $6::interval, now() - $6::interval)
	`, key, f.workspaceID, f.messageID, url, state, age.String()); err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}
}

func (f reconcilerFixture) rowState(t *testing.T, key string) (state string, attempt int, exists bool) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(), `
		SELECT state, attempt FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&state, &attempt)
	if err != nil {
		return "", 0, false
	}
	return state, attempt, true
}

func (f reconcilerFixture) bindAttachment(t *testing.T, url string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO attachment (workspace_id, chat_session_id, chat_message_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		SELECT $1, $2, $3, 'member', creator_id, 'img.png', $4, 'image/png', 1
		FROM chat_session WHERE id = $2
	`, f.workspaceID, f.sessionID, f.messageID, url); err != nil {
		t.Fatalf("bind attachment: %v", err)
	}
}

// The reconciler's three terminal states: a referenced object is kept (row
// cleared), an unreferenced settled object is deleted (row cleared), and a
// row younger than the settle delay is untouched.
func TestChannelMediaReconciler_SettlesThreeStates(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/referenced", "https://cdn.test/referenced", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	f.bindAttachment(t, "https://cdn.test/referenced")
	f.seedLedgerRow(t, "ws/lark/orphan", "https://cdn.test/orphan", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	f.seedLedgerRow(t, "ws/lark/young", "https://cdn.test/young", "pending", time.Minute)

	rec.RunOnce(context.Background())

	if _, _, exists := f.rowState(t, "ws/lark/referenced"); exists {
		t.Fatal("referenced row must be cleared")
	}
	// The deleted orphan is kept as a tombstone (scheduled re-delete) rather
	// than cleared, so an abandoned PUT materializing later is still reclaimed.
	if state, _, exists := f.rowState(t, "ws/lark/orphan"); !exists || state != "tombstoned" {
		t.Fatalf("orphan row = (%q, %v), want a 'tombstoned' row after its delete", state, exists)
	}
	if state, _, exists := f.rowState(t, "ws/lark/young"); !exists || state != "pending" {
		t.Fatalf("young row = (%q, %v), want untouched 'pending'", state, exists)
	}
	deleted := deleter.deletedKeys()
	if len(deleted) != 1 || deleted[0] != "ws/lark/orphan" {
		t.Fatalf("deleted keys = %v, want only the orphan", deleted)
	}
}

// A worker that claimed rows and crashed: the rows sit in 'deleting' with an
// expired lease and must be reclaimable by the next sweep.
func TestChannelMediaReconciler_ReclaimsExpiredLease(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/crashed", "https://cdn.test/crashed", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object
		SET state = 'deleting', lease_token = $2, lease_expires_at = now() - interval '1 minute'
		WHERE storage_key = $1
	`, "ws/lark/crashed", util.MustParseUUID("99999999-9999-4999-8999-999999999999")); err != nil {
		t.Fatalf("simulate crashed claim: %v", err)
	}

	rec.RunOnce(context.Background())

	if state, _, exists := f.rowState(t, "ws/lark/crashed"); !exists || state != "tombstoned" {
		t.Fatalf("expired-lease row = (%q, %v), want reclaimed and settled to 'tombstoned'", state, exists)
	}
	if deleted := deleter.deletedKeys(); len(deleted) != 1 || deleted[0] != "ws/lark/crashed" {
		t.Fatalf("deleted keys = %v, want the reclaimed key", deleted)
	}
}

// A failed object-storage DELETE keeps the row in 'deleting' (bind must never
// attach it), releases the lease, and backs off; a later sweep after
// next_attempt_at retries and settles it.
func TestChannelMediaReconciler_DeleteFailureBacksOffThenRetries(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{err: errors.New("storage unavailable")}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/flaky", "https://cdn.test/flaky", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	rec.RunOnce(context.Background())

	state, attempt, exists := f.rowState(t, "ws/lark/flaky")
	if !exists || state != "deleting" || attempt != 1 {
		t.Fatalf("row after failed delete = (%q, attempt=%d, %v), want ('deleting', 1, true)", state, attempt, exists)
	}
	var due bool
	if err := pool.QueryRow(context.Background(), `
		SELECT next_attempt_at > now() FROM channel_media_pending_object WHERE storage_key = $1
	`, "ws/lark/flaky").Scan(&due); err != nil || !due {
		t.Fatalf("failed delete must back off next_attempt_at (future=%v, err=%v)", due, err)
	}

	// Immediately re-running must NOT retry (backoff in effect).
	rec.RunOnce(context.Background())
	if _, attempt2, _ := f.rowState(t, "ws/lark/flaky"); attempt2 != 1 {
		t.Fatalf("backoff violated: attempt = %d, want still 1", attempt2)
	}

	// Force the backoff to expire; the retry (with a healthy store) settles.
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET next_attempt_at = now() WHERE storage_key = $1
	`, "ws/lark/flaky"); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}
	deleter.mu.Lock()
	deleter.err = nil
	deleter.mu.Unlock()
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, "ws/lark/flaky"); !exists || state != "tombstoned" {
		t.Fatalf("retried delete row = (%q, %v), want settled to 'tombstoned'", state, exists)
	}
}

// Bind-wins interleaving at the DB level: a 'pending' row is NOT claimable by
// the reconciler once the bind transaction deleted it; and the reconciler's
// claim query never touches rows in other workspaces' scope implicitly — the
// claim is keyed off due-time and state only, but every settle decision joins
// through the row's own workspace_id.
func TestChannelMediaReconciler_LeavesFreshPendingToBind(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	// The row a live bind is about to claim: younger than settle.
	f.seedLedgerRow(t, "ws/lark/inflight", "https://cdn.test/inflight", "pending", time.Second)
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, "ws/lark/inflight"); !exists || state != "pending" {
		t.Fatalf("in-flight row = (%q, %v), want untouched so the bind can claim it", state, exists)
	}
	// The bind claims it exactly like BindMediaRefs does (state-guarded).
	tag, err := pool.Exec(context.Background(), `
		DELETE FROM channel_media_pending_object WHERE storage_key = $1 AND state = 'pending'
	`, "ws/lark/inflight")
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("bind-side claim failed: rows=%d err=%v", tag.RowsAffected(), err)
	}
	if len(deleter.deletedKeys()) != 0 {
		t.Fatalf("nothing should have been deleted: %v", deleter.deletedKeys())
	}
}

func TestChannelMediaReconciler_SettleInvariantDwarfsPipelineBudgets(t *testing.T) {
	// The settle delay is an operational buffer with NO correctness weight —
	// correctness comes from the 'deleting' state flip. This invariant only
	// guarantees the reconciler is never doing wasted work while a healthy
	// pipeline is still running: it must dwarf every inline budget.
	const maxPipelineBudget = 45 * time.Second // engine media budget / lark download cap (see cmd/server invariant test for the cross-package assertion)
	if ChannelMediaReconcileSettleDelay < 10*maxPipelineBudget {
		t.Fatalf("settle %v must be >= 10x the largest pipeline budget %v", ChannelMediaReconcileSettleDelay, maxPipelineBudget)
	}
	if channelMediaReconcileLease <= 0 || channelMediaReconcileLease >= ChannelMediaReconcileSettleDelay {
		t.Fatalf("lease %v must be positive and well under settle %v", channelMediaReconcileLease, ChannelMediaReconcileSettleDelay)
	}
	// The lease is heartbeated per row, so it only ever needs to cover ONE
	// row's worst case (a delete at its full timeout plus DB round-trips) —
	// never the whole sequential batch. 2x margin keeps renewal comfortably
	// ahead of expiry.
	if channelMediaReconcileLease < 2*channelMediaReconcileDeleteTimeout {
		t.Fatalf("lease %v must be >= 2x the per-delete timeout %v", channelMediaReconcileLease, channelMediaReconcileDeleteTimeout)
	}
}

// Storage initialization can fail at boot (no S3, unwritable local dir) while
// ledger rows pre-exist from an earlier boot where storage worked. A nil
// deleter must never panic the worker — and must not claim rows either, or
// they would sit stranded in 'deleting' until lease expiry.
func TestChannelMediaReconciler_NilStorageSkipsSweepWithoutClaiming(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: nil}

	f.seedLedgerRow(t, "ws/lark/nil-storage", "https://cdn.test/nil-storage", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	rec.RunOnce(context.Background())

	state, attempt, exists := f.rowState(t, "ws/lark/nil-storage")
	if !exists || state != "pending" || attempt != 0 {
		t.Fatalf("row = (%q, attempt=%d, %v), want untouched 'pending' when storage is missing", state, attempt, exists)
	}
}

// Tenancy is enforced on every settle operation, not derived from the key
// string: release and delete with the right lease but the wrong workspace
// must touch nothing.
func TestChannelMediaReconciler_WrongWorkspaceCannotReleaseOrDelete(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	q := db.New(pool)

	key := "ws/lark/tenancy"
	f.seedLedgerRow(t, key, "https://cdn.test/tenancy", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	lease := util.MustParseUUID("88888888-8888-4888-8888-888888888888")
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET state = 'deleting', lease_token = $2 WHERE storage_key = $1
	`, key, lease); err != nil {
		t.Fatalf("claim row: %v", err)
	}
	otherWorkspace := util.MustParseUUID("77777777-7777-4777-8777-777777777777")

	if err := q.ReleaseChannelMediaPendingObject(context.Background(), db.ReleaseChannelMediaPendingObjectParams{
		StorageKey:    key,
		WorkspaceID:   otherWorkspace,
		LeaseToken:    lease,
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		LastError:     pgtype.Text{String: "cross-tenant", Valid: true},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	n, err := q.DeleteChannelMediaPendingObject(context.Background(), db.DeleteChannelMediaPendingObjectParams{
		StorageKey:  key,
		WorkspaceID: otherWorkspace,
		LeaseToken:  lease,
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 0 {
		t.Fatalf("cross-workspace delete removed %d rows, want 0", n)
	}

	var state string
	var leaseStillHeld bool
	if err := pool.QueryRow(context.Background(), `
		SELECT state, lease_token IS NOT NULL FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&state, &leaseStillHeld); err != nil {
		t.Fatalf("load row: %v", err)
	}
	if state != "deleting" || !leaseStillHeld {
		t.Fatalf("row = (state=%q, lease_held=%v), want untouched ('deleting', true)", state, leaseStillHeld)
	}
}

// blockingDeleter hangs until its context ends — the black-holed-connection
// shape. The per-delete timeout must bound it so one stalled DELETE cannot
// wedge the sequential sweep, and the row must go to backoff.
type blockingDeleter struct{}

func (blockingDeleter) DeleteObject(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestChannelMediaReconciler_StalledDeleteIsBoundedAndBacksOff(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: blockingDeleter{}, deleteTimeout: 50 * time.Millisecond}

	f.seedLedgerRow(t, "ws/lark/stalled", "https://cdn.test/stalled", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	start := time.Now()
	rec.RunOnce(context.Background())
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stalled delete wedged the sweep for %v", elapsed)
	}
	state, attempt, exists := f.rowState(t, "ws/lark/stalled")
	if !exists || state != "deleting" || attempt != 1 {
		t.Fatalf("row = (%q, attempt=%d, %v), want released 'deleting' with backoff", state, attempt, exists)
	}
}

// The batch shares one claim but the lease is heartbeated per row: a row
// reclaimed by another replica mid-batch (its lease expired while earlier
// deletes ran long) must be skipped — no duplicate delete, no clobbered
// attempt/backoff on the new owner's row.
func TestChannelMediaReconciler_SkipsRowReclaimedMidBatch(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	foreign := util.MustParseUUID("66666666-6666-4666-8666-666666666666")
	deleter := &fakeObjectDeleter{}
	// While row 1's delete runs, another replica reclaims row 2 (its shared
	// lease "expired"): simulated by swapping in a foreign lease token.
	deleter.onDelete = func(key string) {
		if key != "ws/lark/first" {
			return
		}
		if _, err := pool.Exec(context.Background(), `
			UPDATE channel_media_pending_object
			SET lease_token = $2, attempt = attempt + 1
			WHERE storage_key = $1
		`, "ws/lark/second", foreign); err != nil {
			t.Errorf("simulate reclaim: %v", err)
		}
	}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	// Ordered by next_attempt_at: "first" is older, processed first.
	f.seedLedgerRow(t, "ws/lark/first", "https://cdn.test/first", "pending", ChannelMediaReconcileSettleDelay+2*time.Minute)
	f.seedLedgerRow(t, "ws/lark/second", "https://cdn.test/second", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())

	if deleted := deleter.deletedKeys(); len(deleted) != 1 || deleted[0] != "ws/lark/first" {
		t.Fatalf("deleted keys = %v, want only the first row (second was reclaimed)", deleted)
	}
	state, attempt, exists := f.rowState(t, "ws/lark/second")
	if !exists || state != "deleting" || attempt != 2 {
		t.Fatalf("reclaimed row = (%q, attempt=%d, %v), want untouched under the new owner ('deleting', 2, true)", state, attempt, exists)
	}
	var token pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT lease_token FROM channel_media_pending_object WHERE storage_key = $1
	`, "ws/lark/second").Scan(&token); err != nil {
		t.Fatalf("load lease: %v", err)
	}
	if token != foreign {
		t.Fatalf("lease token = %v, want the new owner's %v", token, foreign)
	}
}

// reappearingDeleter models an object store where a PUT the client had already
// abandoned materializes the object AFTER the reconciler's DELETE completed —
// the interleaving no DELETE can be ordered against. present tracks whether
// the object currently exists.
type reappearingDeleter struct {
	mu           sync.Mutex
	present      bool
	deleteCalls  int
	lateMaterial func(call int) bool
}

func (d *reappearingDeleter) DeleteObject(_ context.Context, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleteCalls++
	d.present = false
	// The abandoned PUT lands right after this delete returned.
	if d.lateMaterial != nil && d.lateMaterial(d.deleteCalls) {
		d.present = true
	}
	return nil
}

func (d *reappearingDeleter) state(t *testing.T) (present bool, calls int) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.present, d.deleteCalls
}

// The blocking gap the reviewer identified: DELETE completes, the ledger row
// would be cleared, and only then does the abandoned PUT materialize the
// object — leaving an orphan nothing could reclaim. The tombstone schedule
// closes it: the row survives the delete and each due pass re-deletes, so a
// late materialization is reclaimed by a later pass and no object survives.
func TestChannelMediaReconciler_LatePutAfterDeleteIsReclaimedByTombstone(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	// The object reappears exactly once, right after the first delete.
	deleter := &reappearingDeleter{present: true, lateMaterial: func(call int) bool { return call == 1 }}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	key := "ws/lark/late-put"
	f.seedLedgerRow(t, key, "https://cdn.test/late-put", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	// Pass 1: deletes the orphan; the abandoned PUT lands immediately after.
	rec.RunOnce(context.Background())
	present, calls := deleter.state(t)
	if calls != 1 || !present {
		t.Fatalf("after first pass: calls=%d present=%v, want 1/true (late PUT landed)", calls, present)
	}
	state, _, exists := f.rowState(t, key)
	if !exists || state != "tombstoned" {
		t.Fatalf("row = (%q, %v), want a surviving 'tombstoned' row to catch the late object", state, exists)
	}

	// Walk the whole re-delete schedule; each pass must re-delete.
	for pass := 2; pass <= len(channelMediaTombstoneRedelete); pass++ {
		if _, err := pool.Exec(context.Background(), `
			UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
		`, key); err != nil {
			t.Fatalf("make tombstone due: %v", err)
		}
		rec.RunOnce(context.Background())
		if _, calls := deleter.state(t); calls != pass {
			t.Fatalf("pass %d: delete calls = %d, want %d", pass, calls, pass)
		}
	}

	// The late object is gone and the schedule is exhausted, so the row clears.
	if present, _ := deleter.state(t); present {
		t.Fatal("late-materialized object survived the tombstone schedule")
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
	`, key); err != nil {
		t.Fatalf("make tombstone due: %v", err)
	}
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, key); exists {
		t.Fatalf("row = %q, want cleared once the re-delete schedule is exhausted", state)
	}
}

// A tombstone whose object stayed gone still walks its schedule and then
// clears, and the object-deleted metric counts the object once (not once per
// re-delete pass).
func TestChannelMediaReconciler_TombstoneSchedulesThenClears(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	key := "ws/lark/tombstone-walk"
	f.seedLedgerRow(t, key, "https://cdn.test/tombstone-walk", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	for pass := 1; pass <= len(channelMediaTombstoneRedelete); pass++ {
		rec.RunOnce(context.Background())
		state, _, exists := f.rowState(t, key)
		if !exists || state != "tombstoned" {
			t.Fatalf("pass %d: row = (%q, %v), want 'tombstoned'", pass, state, exists)
		}
		if _, err := pool.Exec(context.Background(), `
			UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
		`, key); err != nil {
			t.Fatalf("make tombstone due: %v", err)
		}
	}
	rec.RunOnce(context.Background())
	if _, _, exists := f.rowState(t, key); exists {
		t.Fatal("row must clear after the schedule is exhausted")
	}
	if got := len(deleter.deletedKeys()); got != len(channelMediaTombstoneRedelete)+1 {
		t.Fatalf("delete calls = %d, want one per pass plus the final one", got)
	}
}
