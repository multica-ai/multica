package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeObjectDeleter struct {
	mu      sync.Mutex
	deleted []string
	err     error
}

func (f *fakeObjectDeleter) DeleteObject(_ context.Context, key string) error {
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
	if _, _, exists := f.rowState(t, "ws/lark/orphan"); exists {
		t.Fatal("orphan row must be cleared after its object is deleted")
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

	if _, _, exists := f.rowState(t, "ws/lark/crashed"); exists {
		t.Fatal("expired-lease row must be reclaimed and settled")
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
	if _, _, exists := f.rowState(t, "ws/lark/flaky"); exists {
		t.Fatal("retried delete must settle the row")
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
}
