package metrics

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestChannelOutboundQueueSamplerQueryLive executes the outbound-backlog
// sampler query against a real Postgres. Unit tests for the sampler swap
// refreshFn out for a fake, so nothing else proves the SQL is valid against
// the actual schema — column renames and type mismatches would otherwise only
// surface as a scrape-time error counter in production.
//
// Skips cleanly when no DATABASE_URL is set, mirroring the statement_timeout
// integration test in this package.
func TestChannelOutboundQueueSamplerQueryLive(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres sampler query test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("could not ping %s: %v", dbURL, err)
	}

	// channel_outbound_queue has no foreign keys (repo rule), so a standalone
	// row with synthetic ids is a valid fixture.
	queueID := uuid.New()
	_, err = pool.Exec(ctx, `
INSERT INTO channel_outbound_queue (
  id, installation_id, workspace_id, channel_type, source_kind, source_id,
  target_chat_id, target_chat_type, msg_type, status, created_at
) VALUES ($1, $2, $3, 'wecom', 'chat_done', $4, 'user1', 1, 'markdown', 'queued', now() - interval '42 seconds')
`, queueID, uuid.New(), uuid.New(), queueID.String())
	if err != nil {
		t.Fatalf("insert fixture row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_outbound_queue WHERE id = $1`, queueID)
	})

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{Pool: pool})
	if c == nil {
		t.Fatal("NewBusinessSamplerCollector returned nil")
	}
	snap := newSamplerSnapshot(time.Now())
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if err := c.queryChannelOutboundQueue(ctx, tx, snap); err != nil {
		t.Fatalf("queryChannelOutboundQueue: %v", err)
	}
	if got := snap.channelOutboundDepth["wecom"]; got < 1 {
		t.Fatalf("wecom queue depth = %v, want at least 1", got)
	}
	if got := snap.channelOutboundOldest["wecom"]; got < 40 {
		t.Fatalf("wecom oldest queued age = %v seconds, want at least 40", got)
	}
}
