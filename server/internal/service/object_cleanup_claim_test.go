package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestObjectCleanupClaimsOneRowWithStaleStatistics(t *testing.T) {
	cases := []struct {
		table  string
		seed   string
		states []string
		claim  func(context.Context, *db.Queries, pgtype.UUID) (string, error)
	}{
		{
			table: "channel_media_pending_object",
			seed: `INSERT INTO channel_media_pending_object
				(storage_key, workspace_id, chat_message_id, storage_url, state, created_at, next_attempt_at)
				SELECT 'key-' || n, gen_random_uuid(), gen_random_uuid(), 'https://objects.test/' || n,
				       $3, now() - interval '2 hours', now() - interval '2 hours' + n * interval '1 second'
				FROM generate_series($1::int, $2::int) n`,
			states: []string{"pending", "deleting", "tombstoned"},
			claim: func(ctx context.Context, queries *db.Queries, lease pgtype.UUID) (string, error) {
				row, err := queries.ClaimNextChannelMediaPendingObjectForReconcile(ctx, db.ClaimNextChannelMediaPendingObjectForReconcileParams{
					LeaseToken: lease, Lease: pgInterval(channelMediaReconcileLease),
					SettleDelay: pgInterval(ChannelMediaReconcileSettleDelay),
				})
				return row.StorageKey, err
			},
		},
		{
			table: "issue_source_context_object_intent",
			seed: `INSERT INTO issue_source_context_object_intent
				(storage_key, workspace_id, source_context_id, attachment_id, object_url, state, created_at, next_attempt_at)
				SELECT 'key-' || n, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'https://objects.test/' || n,
				       $3, now() - interval '2 hours', now() - interval '2 hours' + n * interval '1 second'
				FROM generate_series($1::int, $2::int) n`,
			states: []string{"pending", "deleting"},
			claim: func(ctx context.Context, queries *db.Queries, lease pgtype.UUID) (string, error) {
				row, err := queries.ClaimSourceContextObjectIntentForCleanup(ctx, lease)
				return row.StorageKey, err
			},
		},
	}
	for _, testCase := range cases {
		for _, state := range testCase.states {
			t.Run(testCase.table+"/"+state, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				tx, err := sharedTestPool(t).Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				// Copy the real schema so statistics and claims stay local to this test.
				table := pgx.Identifier{testCase.table}.Sanitize()
				if _, err := tx.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL) ON COMMIT DROP"); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, testCase.seed, 1, 1, state); err != nil {
					t.Fatal(err)
				}
				// Add due rows after ANALYZE without forcing a particular query plan.
				if _, err := tx.Exec(ctx, "ANALYZE "+table); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, testCase.seed, 2, 3, state); err != nil {
					t.Fatal(err)
				}
				queries := db.New(tx)
				for index := 1; index <= 3; index++ {
					key, err := testCase.claim(ctx, queries, dbid.NewV7())
					if err != nil || key != fmt.Sprintf("key-%d", index) {
						t.Fatalf("claim %d returned %q, err=%v", index, key, err)
					}
					var claimed int
					if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE lease_token IS NOT NULL").Scan(&claimed); err != nil {
						t.Fatal(err)
					}
					if claimed != index {
						t.Fatalf("after %d claims, %d rows hold leases; want %d", index, claimed, index)
					}
				}
				if _, err := testCase.claim(ctx, queries, dbid.NewV7()); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("claim while all leases are active: %v, want no rows", err)
				}
			})
		}
	}
}
