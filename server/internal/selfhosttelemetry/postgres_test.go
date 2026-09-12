package selfhosttelemetry

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func telemetryTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres telemetry test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("selfhost_telemetry_%d_%d", time.Now().UnixNano(), rand.Uint32())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Logf("drop telemetry test schema: %v", err)
		}
		admin.Close()
	})
	return pool
}

func TestPostgresStoreIdentityAndLeaderLock(t *testing.T) {
	pool := telemetryTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE instance_telemetry_state (
			singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
			instance_id uuid NOT NULL DEFAULT gen_random_uuid(),
			last_successful_day date,
			pending_day date,
			pending_body bytea,
			next_attempt_at timestamptz,
			attempt_count integer NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now()
		);
		INSERT INTO instance_telemetry_state (singleton) VALUES (true)`); err != nil {
		t.Fatal(err)
	}

	firstStore := newPostgresStore(pool)
	first, leader, err := firstStore.TryLeader(ctx)
	if err != nil || !leader {
		t.Fatalf("first leader = %v, err = %v", leader, err)
	}
	firstState, err := first.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	secondStore := newPostgresStore(pool)
	contender, leader, err := secondStore.TryLeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if leader || contender != nil {
		t.Fatal("second replica acquired the telemetry lock while the first held it")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, leader, err := secondStore.TryLeader(ctx)
	if err != nil || !leader {
		t.Fatalf("second leader after release = %v, err = %v", leader, err)
	}
	secondState, err := second.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if firstState.InstanceID != secondState.InstanceID {
		t.Fatalf("instance identity changed across replica/restart: %s != %s", firstState.InstanceID, secondState.InstanceID)
	}
	if _, err := second.Connection().Exec(ctx, "DELETE FROM instance_telemetry_state"); err != nil {
		t.Fatal(err)
	}
	recreated, err := second.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recreated.InstanceID == firstState.InstanceID {
		t.Fatal("explicit state deletion did not create a new deployment identity")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTelemetryMigrationsUpAndDown(t *testing.T) {
	pool := telemetryTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE agent_runtime (daemon_id text, last_seen_at timestamptz);
		CREATE TABLE agent_task_queue (started_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	up := []string{
		"467_instance_telemetry_state.up.sql",
		"468_instance_telemetry_state_singleton_index.up.sql",
		"469_instance_telemetry_state_primary_key.up.sql",
		"471_agent_task_queue_telemetry_started_index.up.sql",
	}
	for _, name := range up {
		applyTelemetryMigration(t, pool, name)
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM instance_telemetry_state").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("singleton rows = %d, want 1", rows)
	}
	var originalInstanceID string
	if err := pool.QueryRow(ctx, "SELECT instance_id::text FROM instance_telemetry_state").Scan(&originalInstanceID); err != nil {
		t.Fatal(err)
	}
	var hasPrimaryKey bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'instance_telemetry_state'::regclass AND contype = 'p'
		)`).Scan(&hasPrimaryKey); err != nil {
		t.Fatal(err)
	}
	if !hasPrimaryKey {
		t.Fatal("telemetry state primary key was not attached")
	}

	down := []string{
		"471_agent_task_queue_telemetry_started_index.down.sql",
		"469_instance_telemetry_state_primary_key.down.sql",
		"468_instance_telemetry_state_singleton_index.down.sql",
		"467_instance_telemetry_state.down.sql",
	}
	for _, name := range down {
		applyTelemetryMigration(t, pool, name)
	}
	var stateTable *string
	if err := pool.QueryRow(ctx, "SELECT to_regclass('instance_telemetry_state')::text").Scan(&stateTable); err != nil {
		t.Fatal(err)
	}
	if stateTable != nil {
		t.Fatalf("state table remains after down migrations: %s", *stateTable)
	}

	for _, name := range up {
		applyTelemetryMigration(t, pool, name)
	}
	var rebuiltInstanceID string
	if err := pool.QueryRow(ctx, "SELECT instance_id::text FROM instance_telemetry_state").Scan(&rebuiltInstanceID); err != nil {
		t.Fatal(err)
	}
	if rebuiltInstanceID == originalInstanceID {
		t.Fatalf("database rebuild reused deployment identity %s", originalInstanceID)
	}
}

type concurrentCollector struct {
	calls atomic.Int32
}

func (c *concurrentCollector) Collect(context.Context, *pgx.Conn, time.Time) (Counts, error) {
	c.calls.Add(1)
	return Counts{}, nil
}

type gatedSender struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (s *gatedSender) Send(context.Context, []byte) deliveryDisposition {
	if s.calls.Add(1) == 1 {
		close(s.started)
	}
	<-s.release
	return deliverySuccess
}

func (*gatedSender) Close() {}

func TestTwoPostgresBackedWorkersProduceOnePendingAndDelivery(t *testing.T) {
	pool := telemetryTestPool(t)
	ctx := context.Background()
	for _, name := range []string{
		"467_instance_telemetry_state.up.sql",
		"468_instance_telemetry_state_singleton_index.up.sql",
		"469_instance_telemetry_state_primary_key.up.sql",
	} {
		applyTelemetryMigration(t, pool, name)
	}

	identitySession, leader, err := newPostgresStore(pool).TryLeader(ctx)
	if err != nil || !leader {
		t.Fatalf("load identity leader = %v, err = %v", leader, err)
	}
	state, err := identitySession.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := identitySession.Close(); err != nil {
		t.Fatal(err)
	}

	collector := &concurrentCollector{}
	sender := &gatedSender{started: make(chan struct{}), release: make(chan struct{})}
	now := scheduledAt(state.InstanceID, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)).Add(time.Minute)
	newWorker := func() *Worker {
		return &Worker{
			store: newPostgresStore(pool), collector: collector, sender: sender,
			serverVersion: "v1.2.3", logger: slog.Default(), now: time.Now,
		}
	}

	firstDone := make(chan error, 1)
	go func() {
		_, runErr := newWorker().runOnce(ctx, now)
		firstDone <- runErr
	}()
	select {
	case <-sender.started:
	case <-time.After(time.Second):
		close(sender.release)
		t.Fatal("first worker did not reach delivery")
	}
	if _, err := newWorker().runOnce(ctx, now); err != nil {
		close(sender.release)
		t.Fatal(err)
	}
	close(sender.release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first worker did not finish")
	}
	if got := collector.calls.Load(); got != 1 {
		t.Fatalf("collection calls = %d, want 1", got)
	}
	if got := sender.calls.Load(); got != 1 {
		t.Fatalf("delivery calls = %d, want 1", got)
	}
}

func applyTelemetryMigration(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	body, err := os.ReadFile("../../migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(body)); err != nil {
		t.Fatalf("apply %s: %v", name, err)
	}
}

func TestSQLCollectorUsesOneBoundaryAndWindowIndexes(t *testing.T) {
	pool := telemetryTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE workspace (id uuid PRIMARY KEY);
		CREATE TABLE member (user_id uuid NOT NULL);
		CREATE TABLE agent (archived_at timestamptz);
		CREATE TABLE agent_runtime (daemon_id text, last_seen_at timestamptz);
		CREATE TABLE agent_task_queue (status text, started_at timestamptz, completed_at timestamptz);
		CREATE INDEX idx_agent_task_queue_telemetry_started
			ON agent_task_queue (started_at) WHERE started_at IS NOT NULL;
		CREATE INDEX idx_agent_task_queue_terminal_completed_at_v2
			ON agent_task_queue (completed_at) WHERE status IN ('completed', 'failed', 'cancelled')`); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	from := at.Add(-24 * time.Hour)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace VALUES (gen_random_uuid()), (gen_random_uuid()), (gen_random_uuid());
		INSERT INTO member VALUES
			('10000000-0000-0000-0000-000000000001'),
			('10000000-0000-0000-0000-000000000001'),
			('10000000-0000-0000-0000-000000000002')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent VALUES (NULL), (NULL), ($1)`, at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_runtime VALUES
			('daemon-a', $1::timestamptz), ('daemon-a', $1::timestamptz + interval '1 hour'),
			('daemon-b', $2::timestamptz - interval '1 second'), ('daemon-old', $1::timestamptz - interval '1 second'),
			(NULL, $1::timestamptz + interval '2 hours')`, from, at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue VALUES
			('completed', $1::timestamptz, $1::timestamptz),
			('failed', $1::timestamptz + interval '1 hour', $1::timestamptz + interval '1 hour'),
			('cancelled', $2::timestamptz - interval '1 second', $2::timestamptz - interval '1 second'),
			('completed', $2::timestamptz, $2::timestamptz),
			('running', NULL, NULL)`, from, at); err != nil {
		t.Fatal(err)
	}
	// Keep almost all rows outside the reporting window so this exercises the
	// production shape: lifetime tables are large while one UTC day's slice is
	// small. The runtime query deliberately accepts a sequential scan because a
	// last_seen_at index would be rewritten by every online-runtime heartbeat.
	// The planner should choose both task time-window indexes naturally, without
	// the test disabling sequential scans.
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_runtime (daemon_id, last_seen_at)
		SELECT 'historical-daemon-' || n, $1::timestamptz - interval '30 days'
		  FROM generate_series(1, 50000) AS n`, from); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (status, started_at, completed_at)
		SELECT 'completed', $1::timestamptz - interval '30 days', $1::timestamptz - interval '30 days'
		  FROM generate_series(1, 50000)`, from); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE agent_runtime"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE agent_task_queue"); err != nil {
		t.Fatal(err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	counts, err := (SQLCollector{}).Collect(ctx, conn.Conn(), at)
	if err != nil {
		t.Fatal(err)
	}
	want := (Counts{
		Workspaces: 3, HumanMembers: 2, Agents: 2, ActiveDaemons: 2,
		TasksStarted: 3, TasksCompleted: 1, TasksFailed: 1, TasksCancelled: 1,
	})
	if counts != want {
		t.Fatalf("counts = %#v, want %#v", counts, want)
	}

	var timeout string
	if err := conn.QueryRow(ctx, "SHOW statement_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != "0" {
		t.Fatalf("statement_timeout leaked onto pooled connection: %q", timeout)
	}

	rows, err := conn.Query(ctx, "EXPLAIN (ANALYZE, COSTS OFF, FORMAT TEXT) "+collectionSQL, from, at)
	if err != nil {
		t.Fatal(err)
	}
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(planLines, "\n")
	for _, index := range []string{
		"idx_agent_task_queue_telemetry_started",
		"idx_agent_task_queue_terminal_completed_at_v2",
	} {
		if !strings.Contains(plan, index) {
			t.Errorf("EXPLAIN ANALYZE did not use %s:\n%s", index, plan)
		}
	}
}
