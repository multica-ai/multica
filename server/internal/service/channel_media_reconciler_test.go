package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/multica-ai/multica/server/internal/metrics"
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

// deletedOwn returns only this test's deletions — every fixture key lives
// under "ws/lark/". The sweep walks the whole table and the test database is
// shared by every package `go test ./...` runs in parallel, so a due row
// seeded by a wecom or handler test is legitimately deleted by THIS test's
// RunOnce. Asserting on the global list made these tests flake under full-suite
// load (reproduced: four workspaces/…/wecom/… keys inside deletedKeys), while
// standalone runs — nothing else writing the table — always passed. Order is
// preserved, so per-key sequencing assertions keep their strength; only the
// claim "and nothing else happened in the world" is dropped, which was never
// this suite's to make.
func (f *fakeObjectDeleter) deletedOwn() []string {
	all := f.deletedKeys()
	own := make([]string, 0, len(all))
	for _, k := range all {
		if strings.HasPrefix(k, "ws/lark/") {
			own = append(own, k)
		}
	}
	return own
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

func (f reconcilerFixture) tombstonePass(t *testing.T, key string) int {
	t.Helper()
	var pass int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT tombstone_pass FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&pass); err != nil {
		t.Fatalf("read tombstone_pass: %v", err)
	}
	return pass
}

// makeDue moves a row's next_attempt_at into the past so the next sweep claims
// it, without waiting out the real schedule.
func (f reconcilerFixture) makeDue(t *testing.T, key string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
	`, key); err != nil {
		t.Fatalf("make row due: %v", err)
	}
}

// deadlines reads the row's lease/retry deadlines together with the DATABASE's
// own now(), so a test can assert which clock produced them.
func (f reconcilerFixture) deadlines(t *testing.T, key string) (lease, next, dbNow time.Time) {
	t.Helper()
	var l, n pgtype.Timestamptz
	if err := f.pool.QueryRow(context.Background(), `
		SELECT lease_expires_at, next_attempt_at, now() FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&l, &n, &dbNow); err != nil {
		t.Fatalf("read deadlines: %v", err)
	}
	return l.Time, n.Time, dbNow
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
	deleted := deleter.deletedOwn()
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
	if deleted := deleter.deletedOwn(); len(deleted) != 1 || deleted[0] != "ws/lark/crashed" {
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
	if own := deleter.deletedOwn(); len(own) != 0 {
		t.Fatalf("nothing of this test's should have been deleted: %v", own)
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
		StorageKey:  key,
		WorkspaceID: otherWorkspace,
		LeaseToken:  lease,
		Backoff:     pgInterval(time.Hour),
		LastError:   pgtype.Text{String: "cross-tenant", Valid: true},
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

// Shutdown cancels the sweep mid-loop. That is how a sweep normally ends, so
// it must settle nothing and stay silent rather than page someone with a
// claim-failure warning for its own cancellation.
func TestChannelMediaReconciler_CancelledSweepIsQuiet(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	key := "ws/lark/cancelled"
	f.seedLedgerRow(t, key, "https://cdn.test/cancelled", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	var logs bytes.Buffer
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{
		Queries: db.New(pool),
		Storage: deleter,
		// Warn and above only: shutdown must not page anyone. Debug/info
		// tracing a future RunOnce may add is not what this test guards.
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec.RunOnce(ctx)

	if logs.Len() != 0 {
		t.Fatalf("cancelled sweep logged %q, want no warnings", logs.String())
	}
	if deleted := deleter.deletedKeys(); len(deleted) != 0 {
		t.Fatalf("cancelled sweep deleted %v, want nothing", deleted)
	}
	if state, attempt, exists := f.rowState(t, key); !exists || state != "pending" || attempt != 0 {
		t.Fatalf("row = (%q, attempt=%d, %v), want an untouched ('pending', 0)", state, attempt, exists)
	}
}

// Cancellation that lands INSIDE a settle — the common case, since a sweep
// spends its time in object-storage deletes — must be as quiet as one at the
// claim boundary. The row keeps its lease and is reclaimed after expiry, so
// there is nothing to record: writing the backoff would fail on the same
// cancelled context and log twice per in-flight row on every shutdown.
func TestChannelMediaReconciler_CancelledSettleIsQuiet(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	key := "ws/lark/cancelled-mid-settle"
	f.seedLedgerRow(t, key, "https://cdn.test/cancelled-mid-settle", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	deleter := &fakeObjectDeleter{err: context.Canceled}
	deleter.onDelete = func(string) { cancel() }
	var logs bytes.Buffer
	rec := &ChannelMediaReconciler{
		Queries: db.New(pool),
		Storage: deleter,
		Logger:  slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	defer cancel()

	rec.RunOnce(ctx)

	if logs.Len() != 0 {
		t.Fatalf("sweep cancelled during a delete logged %q, want no warnings", logs.String())
	}
	// The claim stands: the row waits for its lease to expire, exactly like a
	// worker that crashed mid-delete.
	if state, attempt, exists := f.rowState(t, key); !exists || state != "deleting" || attempt != 1 {
		t.Fatalf("row = (%q, attempt=%d, %v), want a still-claimed ('deleting', 1)", state, attempt, exists)
	}
}

// Rows are claimed one at a time, immediately before their own work: while
// the first row's delete runs, the next row must still be untouched
// ('pending', attempt 0). Claiming the whole batch up front made that row hold
// a lease through work it was not part of — it could expire and be reclaimed
// before its first DELETE was ever tried, so attempt/backoff counted attempts
// that never happened.
func TestChannelMediaReconciler_TailRowIsUnclaimedUntilItsTurn(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	var tailState string
	var tailAttempt int
	deleter.onDelete = func(key string) {
		if key != "ws/lark/first" {
			return
		}
		tailState, tailAttempt, _ = f.rowState(t, "ws/lark/second")
	}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	// Ordered by next_attempt_at: "first" is older, so it is claimed first.
	f.seedLedgerRow(t, "ws/lark/first", "https://cdn.test/first", "pending", ChannelMediaReconcileSettleDelay+2*time.Minute)
	f.seedLedgerRow(t, "ws/lark/second", "https://cdn.test/second", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())

	if tailState != "pending" || tailAttempt != 0 {
		t.Fatalf("tail row during the first delete = (%q, attempt=%d), want an unclaimed ('pending', 0)", tailState, tailAttempt)
	}
	// Both rows are still settled by the same sweep, just in turn.
	if deleted := deleter.deletedOwn(); len(deleted) != 2 {
		t.Fatalf("deleted keys = %v, want both rows settled in one sweep", deleted)
	}
	for _, key := range []string{"ws/lark/first", "ws/lark/second"} {
		state, attempt, exists := f.rowState(t, key)
		if !exists || state != "tombstoned" || attempt != 1 {
			t.Fatalf("row %s = (%q, attempt=%d, %v), want ('tombstoned', 1, true) — one claim, one delete", key, state, attempt, exists)
		}
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
	if got := len(deleter.deletedOwn()); got != len(channelMediaTombstoneRedelete)+1 {
		t.Fatalf("delete calls = %d, want one per pass plus the final one", got)
	}
}

// A tombstone re-delete targets the same key an attachment would read, so the
// reference check runs on every pass. Finding a reference here is an invariant
// violation (per-message keys plus the bind state guard make it unreachable),
// and the answer to a broken invariant is to keep the object and raise an
// anomaly — deleting it would manufacture the dangling attachment the whole
// ledger exists to prevent.
func TestChannelMediaReconciler_TombstoneKeepsReferencedObject(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	m := metrics.NewChannelMediaReconcilerMetrics()
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter, Metrics: m}
	key := "ws/lark/tombstone-vs-attachment"
	url := "https://cdn.test/tombstone-vs-attachment"
	f.seedLedgerRow(t, key, url, "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, key); !exists || state != "tombstoned" {
		t.Fatalf("row = (%q, %v), want 'tombstoned'", state, exists)
	}

	// An attachment now reads this object — a state that should be impossible.
	f.bindAttachment(t, url)
	f.makeDue(t, key)
	rec.RunOnce(context.Background())

	if got := len(deleter.deletedOwn()); got != 1 {
		t.Fatalf("delete calls = %d, want the referenced object left alone (only the first delete)", got)
	}
	if state, _, exists := f.rowState(t, key); exists {
		t.Fatalf("row = %q, want it cleared once the object is known to be referenced", state)
	}
	if got := testutil.ToFloat64(m.TombstoneReferenced); got != 1 {
		t.Fatalf("tombstone-referenced anomaly counter = %v, want 1", got)
	}
}

// Every deadline the reconciler persists is computed by Postgres from its own
// now(), not by the process: the queries take durations, so a replica whose
// clock drifted cannot settle a row whose upload is still in flight, hand out
// a lease that is born expired, or compress the tombstone schedule. This pins
// that the persisted timestamps track the DATABASE clock.
func TestChannelMediaReconciler_DeadlinesComeFromTheDatabaseClock(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	key := "ws/lark/db-clock"
	f.seedLedgerRow(t, key, "https://cdn.test/db-clock", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	// Read the lease while the settle is in flight — it is cleared on release.
	var leaseAhead time.Duration
	deleter := &fakeObjectDeleter{err: errors.New("storage down")}
	deleter.onDelete = func(string) {
		lease, _, dbNow := f.deadlines(t, key)
		leaseAhead = lease.Sub(dbNow)
	}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	rec.RunOnce(context.Background())

	if leaseAhead <= 0 || leaseAhead > channelMediaReconcileLease {
		t.Fatalf("lease expires %v after the database's now(), want (0, %v]", leaseAhead, channelMediaReconcileLease)
	}
	// The failed delete backed the row off by the base backoff, from now().
	_, next, dbNow := f.deadlines(t, key)
	if ahead := next.Sub(dbNow); ahead <= 0 || ahead > channelMediaReconcileBackoffBase {
		t.Fatalf("backoff lands %v after the database's now(), want (0, %v]", ahead, channelMediaReconcileBackoffBase)
	}

	// And so does the tombstone's first re-delete pass.
	deleter.err = nil
	deleter.onDelete = nil
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	_, next, dbNow = f.deadlines(t, key)
	if ahead := next.Sub(dbNow); ahead <= 0 || ahead > channelMediaTombstoneRedelete[0] {
		t.Fatalf("re-delete lands %v after the database's now(), want (0, %v]", ahead, channelMediaTombstoneRedelete[0])
	}
}

// A failed re-delete must not restart the tombstone schedule. The failure
// writes last_error, so a schedule position carried there would be erased and
// the walk would begin again — with a store failing intermittently the row
// would never reach the end of the schedule and never be dropped. The position
// lives in its own column, so a failure only costs one backed-off retry.
func TestChannelMediaReconciler_TombstonePassSurvivesDeleteFailure(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	key := "ws/lark/tombstone-pass-survives"
	f.seedLedgerRow(t, key, "https://cdn.test/tombstone-pass-survives", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	// First settle deletes the object and starts the schedule at pass 0.
	rec.RunOnce(context.Background())
	if got := f.tombstonePass(t, key); got != 0 {
		t.Fatalf("tombstone_pass = %d after the first delete, want 0", got)
	}

	// One re-delete pass lands, advancing the schedule.
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	if got := f.tombstonePass(t, key); got != 1 {
		t.Fatalf("tombstone_pass = %d after one re-delete, want 1", got)
	}

	// The next re-delete fails: the row backs off, keeps its position, and
	// records the failure in last_error.
	deleter.err = errors.New("storage unavailable")
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, key); !exists || state != "tombstoned" {
		t.Fatalf("row = (%q, %v), want the failed re-delete to leave a tombstone", state, exists)
	}
	if got := f.tombstonePass(t, key); got != 1 {
		t.Fatalf("tombstone_pass = %d after a failed re-delete, want it held at 1", got)
	}

	// Recovery resumes the walk instead of restarting it.
	deleter.err = nil
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	if got := f.tombstonePass(t, key); got != 2 {
		t.Fatalf("tombstone_pass = %d after recovery, want the schedule to resume at 2", got)
	}

	// And the remaining passes still terminate the row.
	for range channelMediaTombstoneRedelete[2:] {
		f.makeDue(t, key)
		rec.RunOnce(context.Background())
	}
	if state, _, exists := f.rowState(t, key); exists {
		t.Fatalf("row = %q, want it dropped once the schedule is exhausted", state)
	}
}
