package lark

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func legacyReceiptFixture(t *testing.T) (*pgxpool.Pool, *testutil.Fixture) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	return pool, testutil.New(pool, uuid.NewString(), uuid.NewString())
}

func receiptInstallation(t *testing.T, fx *testutil.Fixture, status, channel string, installed time.Time, config map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return fx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": fx.WorkspaceID, "agent_id": uuid.NewString(), "installer_user_id": fx.UserID, "channel_type": channel, "status": status, "installed_at": installed, "config": raw})
}

func receiptHistory(t *testing.T, fx *testutil.Fixture, id, channel string, at time.Time) {
	t.Helper()
	fx.Insert(t, "channel_chat_session_binding", testutil.Cols{"installation_id": id, "chat_session_id": uuid.NewString(), "channel_type": channel, "channel_chat_id": uuid.NewString(), "chat_type": "p2p", "created_at": at})
}

func receiptConfig(t *testing.T, pool *pgxpool.Pool, id string) map[string]any {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(context.Background(), "SELECT config FROM channel_installation WHERE id=$1", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestLegacyReceiptBackfillEvidenceAndIdempotence(t *testing.T) {
	pool, fx := legacyReceiptFixture(t)
	q := db.New(pool)
	installed := time.Date(2026, 10, 30, 0, 0, 0, 123456000, time.UTC)
	for _, tc := range []struct {
		name, status, channel, generation, receipt string
		history                                    []time.Time
		historyChannel                             string
		verified                                   bool
	}{
		{name: "qualifying history", history: []time.Time{installed.Add(time.Minute), installed.Add(2 * time.Minute)}, verified: true},
		{name: "exact install boundary", history: []time.Time{installed}, verified: true},
		{name: "no history"},
		{name: "history predates rebind", history: []time.Time{installed.Add(-time.Microsecond)}},
		{name: "wrong channel history", history: []time.Time{installed}, historyChannel: "slack"},
		{name: "revoked", status: "revoked", history: []time.Time{installed}},
		{name: "other channel", channel: "slack", history: []time.Time{installed}},
		{name: "new binding", generation: uuid.NewString(), history: []time.Time{installed}},
		{name: "existing receipt", receipt: "2026-10-30T00:00:00Z", history: []time.Time{installed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, channel := tc.status, tc.channel
			if status == "" {
				status = "active"
			}
			if channel == "" {
				channel = "feishu"
			}
			config := map[string]any{"app_id": "cli_" + uuid.NewString(), "region": "feishu", "unrelated": map[string]any{"preserve": true}}
			if tc.generation != "" {
				config["binding_generation"] = tc.generation
			}
			if tc.receipt != "" {
				config["first_message_received_at"] = tc.receipt
			}
			id := receiptInstallation(t, fx, status, channel, installed, config)
			historyChannel := tc.historyChannel
			if historyChannel == "" {
				historyChannel = channel
			}
			for _, at := range tc.history {
				receiptHistory(t, fx, id, historyChannel, at)
			}
			// History for a different installation is never evidence for this one.
			receiptHistory(t, fx, uuid.NewString(), channel, installed)
			n, err := q.BackfillLegacyFeishuInboundReceipts(context.Background(), 100)
			if err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if tc.verified {
				want = 1
			}
			if n != want {
				t.Fatalf("updated=%d want %d", n, want)
			}
			got := receiptConfig(t, pool, id)
			if tc.verified {
				at, err := time.Parse(time.RFC3339Nano, got["first_message_received_at"].(string))
				if err != nil {
					t.Fatal(err)
				}
				if !at.Equal(tc.history[0]) {
					t.Fatalf("receipt=%s want historical %s", at, tc.history[0])
				}
				delete(got, "first_message_received_at")
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(config)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("config changed: %s want %s", gotJSON, wantJSON)
			}
			before := receiptConfig(t, pool, id)
			n, err = q.BackfillLegacyFeishuInboundReceipts(context.Background(), 100)
			if err != nil || n != 0 {
				t.Fatalf("rerun=%d err=%v", n, err)
			}
			after := receiptConfig(t, pool, id)
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if string(beforeJSON) != string(afterJSON) {
				t.Fatal("rerun overwrote evidence")
			}
		})
	}
}

func TestLegacyReceiptBackfillBatchLimit(t *testing.T) {
	pool, fx := legacyReceiptFixture(t)
	installed := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		id := receiptInstallation(t, fx, "active", "feishu", installed, map[string]any{"app_id": "cli_" + uuid.NewString()})
		receiptHistory(t, fx, id, "feishu", installed)
	}
	q := db.New(pool)
	for _, want := range []int64{2, 2, 1, 0} {
		n, err := q.BackfillLegacyFeishuInboundReceipts(context.Background(), 2)
		if err != nil || n != want {
			t.Fatalf("batch updated=%d want %d err=%v", n, want, err)
		}
	}
}

func TestLegacyReceiptBackfillConcurrentWriters(t *testing.T) {
	pool, fx := legacyReceiptFixture(t)
	installed := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	liveReceipt := installed.Add(time.Hour).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name, update string
		want         int64
	}{
		{"rebind", `config=config || '{"binding_generation":"new"}'::jsonb, installed_at=installed_at+interval '1 second'`, 0},
		{"legacy reinstall", `installed_at=installed_at+interval '1 second'`, 0},
		{"revocation", `status='revoked'`, 0},
		{"live receipt", `config=jsonb_set(config,'{first_message_received_at}','"` + liveReceipt + `"'::jsonb)`, 0},
		{"metadata", `config=jsonb_set(config,'{bot_union_id}','"on_keep"'::jsonb)`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := receiptInstallation(t, fx, "active", "feishu", installed, map[string]any{"app_id": "cli_" + uuid.NewString()})
			receiptHistory(t, fx, id, "feishu", installed)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			worker, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer worker.Release()
			pid := worker.Conn().PgConn().PID()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			// The rollout statement sees old eligibility, then waits on this uncommitted
			// writer. Rechecking after the lock is granted must use the committed state.
			if _, err := tx.Exec(ctx, "UPDATE channel_installation SET "+tc.update+" WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			type result struct {
				n   int64
				err error
			}
			done := make(chan result, 1)
			go func() { n, err := db.New(worker).BackfillLegacyFeishuInboundReceipts(ctx, 100); done <- result{n, err} }()
			blocked := false
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
				if err := pool.QueryRow(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !blocked {
				_ = tx.Rollback(ctx)
				<-done
				t.Fatal("backfill did not reach the fixture row lock")
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			got := <-done
			if got.err != nil || got.n != tc.want {
				t.Fatalf("updated=%d want %d err=%v", got.n, tc.want, got.err)
			}
			config := receiptConfig(t, pool, id)
			switch tc.name {
			case "metadata":
				if config["bot_union_id"] != "on_keep" || config["first_message_received_at"] == nil {
					t.Fatal("lost concurrent metadata or receipt")
				}
			case "live receipt":
				if config["first_message_received_at"] != liveReceipt {
					t.Fatal("overwrote live receipt")
				}
			default:
				if config["first_message_received_at"] != nil {
					t.Fatal("old history verified changed binding")
				}
			}
		})
	}
}

type legacyBackfillFunc func(context.Context, int32) (int64, error)

func (f legacyBackfillFunc) BackfillLegacyFeishuInboundReceipts(ctx context.Context, batch int32) (int64, error) {
	return f(ctx, batch)
}

func TestLegacyReceiptRolloutBounds(t *testing.T) {
	calls := 0
	store := legacyBackfillFunc(func(_ context.Context, batch int32) (int64, error) {
		calls++
		if batch != legacyReceiptBatchSize {
			t.Fatalf("unbounded batch %d", batch)
		}
		return int64(batch), nil
	})
	rows, err := backfillLegacyInboundReceipts(context.Background(), store)
	if err != nil || calls != legacyReceiptMaxBatches || rows != legacyReceiptBatchSize*legacyReceiptMaxBatches {
		t.Fatalf("calls=%d rows=%d err=%v", calls, rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = backfillLegacyInboundReceipts(ctx, store)
	if err != context.Canceled || calls != legacyReceiptMaxBatches {
		t.Fatal("cancellation did not stop rollout")
	}
	calls = 0
	failure := context.DeadlineExceeded
	store = legacyBackfillFunc(func(context.Context, int32) (int64, error) { calls++; return 0, failure })
	_, err = backfillLegacyInboundReceipts(context.Background(), store)
	if err != failure || calls != 1 {
		t.Fatal("retried failed batch without a bound")
	}
}
