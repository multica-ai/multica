package outbox

// claim_order_db_test.go — the order rows come off the queue for one chat.
//
// Both messages in these tests arrive either way, which is what makes the bug
// they cover so quiet: nothing retries, nothing alerts, no operator sees a
// failure. The damage is a misread thread. In a group where two people asked at
// once, each reads the other's answer as the response to their own question.
//
// DB-backed because the guarantee IS the claim query's ORDER BY and the UPDATE
// that postpones a target. A fake queries layer would assert the fake's own
// ordering, and the change under test would read as changing nothing. They skip
// when no migrated database is reachable (CI has one).

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func orderTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	var present bool
	if err := pool.QueryRow(ctx,
		"SELECT to_regclass('public.channel_outbound_queue') IS NOT NULL").Scan(&present); err != nil || !present {
		pool.Close()
		t.Skip("channel_outbound_queue not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

// newOrderFixture seeds one active installation and returns it with a clean
// queue. Each test gets its own installation id so they cannot see each other's
// rows even when run together.
//
// The pool is returned alongside the generated queries because the timestamp
// surgery these tests need — backdating a row, tying two created_at values — is
// test-only and belongs here, not as extra queries in the shipped SQL.
func newOrderFixture(t *testing.T) (context.Context, *db.Queries, *pgxpool.Pool, pgtype.UUID) {
	t.Helper()
	pool := orderTestDB(t)
	ctx := context.Background()

	var installationID pgtype.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES (gen_random_uuid(), gen_random_uuid(), 'wecom',
        jsonb_build_object('bot_id', 'order_test'), gen_random_uuid(), 'active')
RETURNING id`).Scan(&installationID); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_outbound_queue WHERE installation_id = $1`, installationID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
	return ctx, db.New(pool), pool, installationID
}

// enqueueAt inserts one row and backdates it, so a test can lay out a backlog
// without sleeping.
func enqueueAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, installationID pgtype.UUID,
	sourceID, targetChatID string, at time.Time) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO channel_outbound_queue
    (installation_id, workspace_id, channel_type, source_kind, source_id,
     target_chat_id, target_chat_type, msg_type, created_at, next_attempt_at)
VALUES ($1, gen_random_uuid(), 'wecom', 'chat_done', $2, $3, 2, 'markdown', $4, $4)
RETURNING id`, installationID, sourceID, targetChatID, at).Scan(&id); err != nil {
		t.Fatalf("enqueue %s: %v", sourceID, err)
	}
	return id
}

func claimOne(t *testing.T, ctx context.Context, q *db.Queries, installationID pgtype.UUID) (db.ChannelOutboundQueue, bool) {
	t.Helper()
	row, err := q.ClaimChannelOutbound(ctx, db.ClaimChannelOutboundParams{
		InstallationID: installationID,
		LeaseExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(30 * time.Second), Valid: true},
	})
	if err != nil {
		return db.ChannelOutboundQueue{}, false
	}
	return row, true
}

func completeClaim(t *testing.T, ctx context.Context, q *db.Queries, row db.ChannelOutboundQueue) {
	t.Helper()
	if _, err := q.CompleteClaimedChannelOutbound(ctx, db.CompleteClaimedChannelOutboundParams{
		ID:         row.ID,
		LeaseToken: row.LeaseToken,
	}); err != nil {
		t.Fatalf("complete claim: %v", err)
	}
}

func TestClaimOrder_ARowEnqueuedAfterRetryCannotOvertake(t *testing.T) {
	ctx, q, pool, installationID := newOrderFixture(t)
	base := time.Now().Add(-10 * time.Minute)

	older := enqueueAt(t, ctx, pool, installationID, "task-A", "GROUP_1", base)
	first, ok := claimOne(t, ctx, q, installationID)
	if !ok || first.ID != older {
		t.Fatal("expected to claim the older row first")
	}
	if _, err := q.RetryClaimedChannelOutbound(ctx, db.RetryClaimedChannelOutboundParams{
		ID:            first.ID,
		LeaseToken:    first.LeaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
		LastError:     pgtype.Text{String: "broken pipe", Valid: true},
	}); err != nil {
		t.Fatalf("retry: %v", err)
	}

	newer := enqueueAt(t, ctx, pool, installationID, "task-B", "GROUP_1", base.Add(time.Minute))
	if next, ok := claimOne(t, ctx, q, installationID); ok {
		t.Fatalf("claimed %s while older row backs off; newer row is %s",
			util.UUIDToString(next.ID), util.UUIDToString(newer))
	}

	if _, err := pool.Exec(ctx,
		`UPDATE channel_outbound_queue SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, older); err != nil {
		t.Fatalf("make older row due: %v", err)
	}
	first, ok = claimOne(t, ctx, q, installationID)
	if !ok || first.ID != older {
		t.Fatal("expected the older row after its backoff")
	}
	completeClaim(t, ctx, q, first)

	next, ok := claimOne(t, ctx, q, installationID)
	if !ok || next.ID != newer {
		t.Fatal("newer row did not become claimable after the predecessor settled")
	}
}

func TestClaimOrder_ARowEnqueuedAfterDeferralCannotOvertake(t *testing.T) {
	ctx, q, pool, installationID := newOrderFixture(t)
	base := time.Now().Add(-10 * time.Minute)

	older := enqueueAt(t, ctx, pool, installationID, "task-A", "GROUP_1", base)
	first, ok := claimOne(t, ctx, q, installationID)
	if !ok || first.ID != older {
		t.Fatal("expected to claim the older row first")
	}
	if _, err := q.DeferClaimedChannelOutbound(ctx, db.DeferClaimedChannelOutboundParams{
		ID:            first.ID,
		LeaseToken:    first.LeaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("defer: %v", err)
	}

	newer := enqueueAt(t, ctx, pool, installationID, "task-B", "GROUP_1", base.Add(time.Minute))
	if next, ok := claimOne(t, ctx, q, installationID); ok {
		t.Fatalf("claimed %s while older row is deferred; newer row is %s",
			util.UUIDToString(next.ID), util.UUIDToString(newer))
	}

	if _, err := pool.Exec(ctx,
		`UPDATE channel_outbound_queue SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, older); err != nil {
		t.Fatalf("make older row due: %v", err)
	}
	first, ok = claimOne(t, ctx, q, installationID)
	if !ok || first.ID != older {
		t.Fatal("expected the older row after its deferral")
	}
	completeClaim(t, ctx, q, first)

	next, ok := claimOne(t, ctx, q, installationID)
	if !ok || next.ID != newer {
		t.Fatal("newer row did not become claimable after the predecessor settled")
	}
}

// A transient failure on the older reply must not let the newer one overtake it.
//
// The reconnect drain is the deterministic case: the older row is pushed to
// now+backoff, the newer one still carries its enqueue time, and the claim's
// (next_attempt_at, created_at) order therefore hands out the NEWER row first.
func TestClaimOrder_ARetryHoldsBackLaterRepliesToTheSameChat(t *testing.T) {
	ctx, q, pool, installationID := newOrderFixture(t)
	base := time.Now().Add(-10 * time.Minute)

	older := enqueueAt(t, ctx, pool, installationID, "task-A", "GROUP_1", base)
	newer := enqueueAt(t, ctx, pool, installationID, "task-B", "GROUP_1", base.Add(time.Minute))

	first, ok := claimOne(t, ctx, q, installationID)
	if !ok {
		t.Fatal("nothing claimable")
	}
	if first.ID != older {
		t.Fatalf("claimed the newer row first; the backlog is not in order to begin with")
	}

	// The send failed transiently — a dropped socket mid-drain.
	if _, err := q.RetryClaimedChannelOutbound(ctx, db.RetryClaimedChannelOutboundParams{
		ID:            first.ID,
		LeaseToken:    first.LeaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
		LastError:     pgtype.Text{String: "broken pipe", Valid: true},
	}); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if next, ok := claimOne(t, ctx, q, installationID); ok {
		if next.ID == newer {
			t.Error("the newer reply was handed out while the older one waits out its backoff: " +
				"both land, in an order that reads as deliberate, and nothing reports a problem")
		} else {
			t.Errorf("claimed an unexpected row %s", util.UUIDToString(next.ID))
		}
	}
}

// The guarantee is per conversation. Holding an unrelated chat behind this
// failure would turn one bad socket into queue-wide latency, and two rooms have
// never needed to be ordered against each other.
func TestClaimOrder_ARetryLeavesOtherChatsAlone(t *testing.T) {
	ctx, q, pool, installationID := newOrderFixture(t)
	base := time.Now().Add(-10 * time.Minute)

	older := enqueueAt(t, ctx, pool, installationID, "task-A", "GROUP_1", base)
	otherChat := enqueueAt(t, ctx, pool, installationID, "task-C", "GROUP_2", base.Add(time.Minute))

	first, ok := claimOne(t, ctx, q, installationID)
	if !ok || first.ID != older {
		t.Fatalf("expected to claim the oldest row first")
	}
	if _, err := q.RetryClaimedChannelOutbound(ctx, db.RetryClaimedChannelOutboundParams{
		ID:            first.ID,
		LeaseToken:    first.LeaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
		LastError:     pgtype.Text{String: "broken pipe", Valid: true},
	}); err != nil {
		t.Fatalf("retry: %v", err)
	}

	next, ok := claimOne(t, ctx, q, installationID)
	if !ok {
		t.Fatal("a different chat's reply was held behind an unrelated failure")
	}
	if next.ID != otherChat {
		t.Errorf("claimed %s, want the other chat's row", util.UUIDToString(next.ID))
	}
}

// The rate gate postpones a row without spending an attempt, and that is the
// other way a row gets moved into the future. The windows are sliding counts, so
// a later row claimed moments after this one can find an attempt has aged out of
// the minute window, be admitted, and answer ahead of the message it was queued
// behind. Same visible defect, different mover.
func TestClaimOrder_ADeferralHoldsBackLaterRepliesToTheSameChat(t *testing.T) {
	ctx, q, pool, installationID := newOrderFixture(t)
	base := time.Now().Add(-10 * time.Minute)

	older := enqueueAt(t, ctx, pool, installationID, "task-A", "GROUP_1", base)
	newer := enqueueAt(t, ctx, pool, installationID, "task-B", "GROUP_1", base.Add(time.Minute))

	first, ok := claimOne(t, ctx, q, installationID)
	if !ok || first.ID != older {
		t.Fatalf("expected to claim the oldest row first")
	}
	// Over quota: deferred by a full window, no attempt spent.
	if _, err := q.DeferClaimedChannelOutbound(ctx, db.DeferClaimedChannelOutboundParams{
		ID:            first.ID,
		LeaseToken:    first.LeaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("defer: %v", err)
	}

	if next, ok := claimOne(t, ctx, q, installationID); ok {
		if next.ID == newer {
			t.Error("the newer reply was handed out while the older one waits out a rate-window deferral")
		} else {
			t.Errorf("claimed an unexpected row %s", util.UUIDToString(next.ID))
		}
	}
}

// Rows inserted by one statement share created_at to the microsecond, and the
// claim ordered by (next_attempt_at, created_at) with no third key. For tied
// rows the order was then whatever the plan produced — and an UPDATE moves a
// row's tuple, so touching the older row is enough to put the newer one first.
//
// Invisible while every row is an independent message. Not invisible the moment
// one answer is several rows, because "part 2 of 3" before "part 1 of 3" is a
// defect a reader sees.
func TestClaimOrder_TiedCreatedAtIsBrokenByInsertionOrder(t *testing.T) {
	ctx, _, pool, installationID := newOrderFixture(t)
	at := time.Now().Add(-time.Minute)

	// Enough rows that an unstable sort over tied keys has somewhere to go. One
	// answer split for the wire is exactly this: N rows, one created_at.
	const pieces = 20
	want := make([]pgtype.UUID, 0, pieces)
	for i := range pieces {
		want = append(want, enqueueAt(t, ctx, pool, installationID,
			fmt.Sprintf("part-%02d", i), "GROUP_1", at))
	}
	// Tie every created_at to the first row's, as one INSERT ... VALUES (..),(..)
	// would: now() is the transaction timestamp, so every row a single statement
	// writes shares it to the microsecond.
	if _, err := pool.Exec(ctx, `
UPDATE channel_outbound_queue
SET created_at = (SELECT created_at FROM channel_outbound_queue WHERE id = $2)
WHERE installation_id = $1`, installationID, want[0]); err != nil {
		t.Fatalf("tie created_at: %v", err)
	}
	// Rewrite half the tuples so physical order no longer matches insertion
	// order. Nothing exotic — a lease or a deferral already rewrites a row.
	for i := 0; i < pieces; i += 2 {
		if _, err := pool.Exec(ctx,
			`UPDATE channel_outbound_queue SET updated_at = now() WHERE id = $1`, want[i]); err != nil {
			t.Fatalf("touch: %v", err)
		}
	}

	got := make([]pgtype.UUID, 0, pieces)
	// Pin a plan that cannot inherit its ordering from the claim index, so the
	// ORDER BY is what has to decide. This is the whole point: the index returns
	// tied rows in insertion order today, which is exactly why the defect is
	// invisible in production — correct order is a property of the plan that
	// happens to be chosen, not of the query. A guarantee has to survive the
	// planner changing its mind.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	for _, off := range []string{
		"SET enable_indexscan = off",
		"SET enable_bitmapscan = off",
		"SET enable_indexonlyscan = off",
	} {
		if _, err := conn.Exec(ctx, off); err != nil {
			t.Fatalf("%s: %v", off, err)
		}
	}
	pinned := db.New(conn)

	for range pieces {
		row, ok := claimOne(t, ctx, pinned, installationID)
		if !ok {
			break
		}
		got = append(got, row.ID)
		completeClaim(t, ctx, pinned, row)
	}
	if len(got) != pieces {
		t.Fatalf("claimed %d rows, want %d", len(got), pieces)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("piece %d of the answer came out at position %d: with created_at tied and no "+
				"third sort key, the order of a split answer's pieces is whatever the plan produces",
				indexOf(want, got[i]), i)
		}
	}
}

func indexOf(ids []pgtype.UUID, id pgtype.UUID) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}
