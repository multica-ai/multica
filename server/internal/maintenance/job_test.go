package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func fixture(t *testing.T) (*pgxpool.Pool, *Service) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required; run through make env-exec")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "maintenance_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",pg_catalog"
	config.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE schema_migrations(version text)")
	for _, name := range []string{"332_issue_status", "333_issue_status_pkey_index", "478_issue_status_category_expand", "485_maintenance_job", "486_maintenance_job_id_index", "487_maintenance_job_idempotency_index", "488_maintenance_job_active_index"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", name+".up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		exec(string(body))
	}
	exec("INSERT INTO schema_migrations VALUES ('478_issue_status_category_expand')")
	return pool, NewService(pool, StatusCategory{})
}
func statusID(i int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", i) }
func seed(t *testing.T, pool *pgxpool.Pool, mode string) {
	t.Helper()
	dbfx := testutil.New(pool, uuid.NewString(), uuid.NewString())
	keys := []string{"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled"}
	for i, key := range keys {
		category := key
		if mode == "new" || (mode == "mixed" && i%2 == 0) {
			category = categoryMapping[key]
		}
		dbfx.Insert(t, "issue_status", testutil.Cols{"id": statusID(i + 1), "workspace_id": dbfx.WorkspaceID,
			"key": key, "name": key, "category": category, "color": "#123456", "is_system": true})
		dbfx.Insert(t, "issue_status", testutil.Cols{"id": statusID(i + 8), "workspace_id": uuid.NewString(),
			"key": "custom_" + key, "name": key, "category": category, "color": "#123456", "archived_at": testutil.Raw("now()")})
	}
}
func request(dry bool) CreateRequest {
	return CreateRequest{Type: StatusCategoryType, Version: 1, Scope: "database", IdempotencyKey: uuid.NewString(),
		DryRun: &dry, Options: Options{BatchSize: 3, DelayMS: 1, LockTimeoutMS: 50, StatementTimeoutMS: 1000},
		Parameters: asJSON(statusParameters{WritersUpgraded: !dry})}
}
func create(t *testing.T, s *Service, dry bool) Job {
	t.Helper()
	j, err := s.Create(context.Background(), request(dry))
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func step(t *testing.T, s *Service, j Job) Job {
	t.Helper()
	for i := 0; i < 5; i++ {
		next, err := s.Mutate(context.Background(), j.ID, j.Revision, "advance")
		if errors.Is(err, ErrThrottled) {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("advance: %v; job=%+v", err, next)
		}
		return next
	}
	t.Fatal("throttled beyond bounded retry")
	return Job{}
}
func finish(t *testing.T, s *Service, j Job) Job {
	t.Helper()
	for i := 0; i < 30; i++ {
		if j.Status == "completed" {
			return j
		}
		j = step(t, s, j)
	}
	t.Fatal("job failed to terminate")
	return Job{}
}
func snapshot(t *testing.T, pool *pgxpool.Pool, excludeCategory bool) string {
	t.Helper()
	var s string
	expr := "to_jsonb(s)"
	if excludeCategory {
		expr += "-'category'"
	}
	if err := pool.QueryRow(context.Background(), "SELECT coalesce(jsonb_agg("+expr+" ORDER BY id),'[]')::text FROM issue_status s").Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestCategoryBackfillOldMixedNewAndDryRun(t *testing.T) {
	for _, mode := range []string{"old", "mixed", "new"} {
		t.Run(mode, func(t *testing.T) {
			pool, s := fixture(t)
			seed(t, pool, mode)
			before := snapshot(t, pool, false)
			identity := snapshot(t, pool, true)
			dry := finish(t, s, create(t, s, true))
			if snapshot(t, pool, false) != before {
				t.Fatal("dry-run changed catalog")
			}
			var report statusProgress
			if err := decode(dry.Progress, &report); err != nil {
				t.Fatal(err)
			}
			if report.Scanned != 14 || report.System != 7 || report.Archived != 7 || report.Updated != 0 {
				t.Fatalf("dry report: %+v", report)
			}
			j := create(t, s, false)
			for j.Status != "completed" {
				var p statusProgress
				_ = decode(j.Progress, &p)
				next := step(t, s, j)
				var np statusProgress
				_ = decode(next.Progress, &np)
				if np.Scanned-p.Scanned > 3 || np.Updated-p.Updated > 3 || np.Verified-p.Verified > 3 {
					t.Fatal("unbounded batch")
				}
				// A different service instance represents another replica/restarted container.
				s = NewService(pool, StatusCategory{})
				j = next
			}
			if snapshot(t, pool, true) != identity {
				t.Fatal("identity, archive, timestamps or business fields changed")
			}
			var remaining int
			if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM issue_status WHERE category != issue_status_category(category)").Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("remaining=%d: %v", remaining, err)
			}
			var p statusProgress
			_ = decode(j.Progress, &p)
			if p.Verified != 14 || p.Updated != report.WouldUpdate {
				t.Fatalf("progress=%+v dry=%+v", p, report)
			}
			again := finish(t, s, create(t, s, false))
			_ = decode(again.Progress, &p)
			if p.Updated != 0 {
				t.Fatal("second pass rewrote canonical rows")
			}
		})
	}
}
func TestJobIdempotencyVersionPauseAndDatabaseThrottle(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	req := request(false)
	req.Options.DelayMS = 60000
	j, err := s.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.Create(ctx, req)
	if err != nil || same.ID != j.ID {
		t.Fatalf("create retry: %+v %v", same, err)
	}
	different := req
	different.Options.BatchSize++
	if _, err = s.Create(ctx, different); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed duplicate request: %v", err)
	}
	other := request(false)
	if _, err = s.Create(ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active scope: %v", err)
	}
	next := step(t, s, j)
	replay, err := NewService(pool, StatusCategory{}).Mutate(ctx, j.ID, j.Revision, "advance")
	if !errors.Is(err, ErrConflict) || replay.Revision != next.Revision {
		t.Fatalf("lost-response replay advanced: %+v %v", replay, err)
	}
	if _, err = s.Mutate(ctx, j.ID, next.Revision, "advance"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("cross-replica delay: %v", err)
	}
	paused, err := s.Mutate(ctx, j.ID, next.Revision, "pause")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, paused.Revision, "advance"); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused advanced: %v", err)
	}
	resumed, err := s.Mutate(ctx, j.ID, paused.Revision, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, resumed.Revision, "advance"); !errors.Is(err, ErrThrottled) {
		t.Fatal("resume bypassed delay")
	}
	cancelled, err := s.Mutate(ctx, j.ID, resumed.Revision, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, cancelled.Revision, "resume"); !errors.Is(err, ErrConflict) {
		t.Fatal("terminal history reopened")
	}
	if _, err = s.Create(ctx, other); err != nil {
		t.Fatal("new run after cancellation:", err)
	}
}
func TestLockTimeoutRollsBackDataAndCheckpoint(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	j := create(t, s, false)
	ctx := context.Background()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, "SELECT id FROM issue_status WHERE id=$1::uuid FOR UPDATE", statusID(2)); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, pool, false)
	start := time.Now()
	failed, err := s.Mutate(ctx, j.ID, j.Revision, "advance")
	if err == nil || failed.Status != "paused" || failed.LastError == "" {
		t.Fatalf("expected paused timeout: %+v %v", failed, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	if string(failed.Checkpoint) != "{}" || string(failed.Progress) != "{}" || snapshot(t, pool, false) != before {
		t.Fatal("partial data/checkpoint committed")
	}
	t.Logf("row-lock contention bounded to %s; partial updates rolled back", time.Since(start))
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Mutate(ctx, j.ID, failed.Revision, "resume")
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, resumed)
}

type wrappedProcessor struct {
	StatusCategory
	hook func(context.Context, pgx.Tx, Job) error
}

func (p wrappedProcessor) Step(ctx context.Context, tx pgx.Tx, j Job) (json.RawMessage, json.RawMessage, json.RawMessage, bool, error) {
	cp, progress, result, done, err := p.StatusCategory.Step(ctx, tx, j)
	if err == nil {
		err = p.hook(ctx, tx, j)
	}
	return cp, progress, result, done, err
}
func TestConcurrentReplicaAndConnectionLoss(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	j := create(t, s, false)
	before := snapshot(t, pool, false)
	entered := make(chan struct{})
	release := make(chan struct{})
	crashing := NewService(pool, wrappedProcessor{hook: func(ctx context.Context, tx pgx.Tx, _ Job) error {
		close(entered)
		<-release
		// Terminate only this test's own batch connection, after data UPDATE and
		// before checkpoint/commit, emulating container/connection loss.
		var pid int
		if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		_, err := pool.Exec(ctx, "SELECT pg_terminate_backend($1)", pid)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "SELECT 1")
		return err
	}})
	result := make(chan error, 1)
	go func() { _, err := crashing.Mutate(context.Background(), j.ID, j.Revision, "advance"); result <- err }()
	<-entered
	_, err := NewService(pool, StatusCategory{}).Mutate(context.Background(), j.ID, j.Revision, "advance")
	if !errors.Is(err, ErrBusy) {
		close(release)
		<-result
		t.Fatalf("concurrent replica: %v", err)
	}
	// Normal writes outside the batch remain available, with no ACCESS EXCLUSIVE lock.
	var exclusive int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks WHERE relation='issue_status'::regclass AND mode='AccessExclusiveLock'`).Scan(&exclusive); err != nil || exclusive != 0 {
		t.Errorf("exclusive=%d: %v", exclusive, err)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE issue_status SET name='business write' WHERE id=$1::uuid", statusID(14)); err != nil {
		t.Error(err)
	}
	close(release)
	if err := <-result; err == nil {
		t.Fatal("connection loss returned success")
	}
	current, err := s.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 0 || string(current.Checkpoint) != "{}" {
		t.Fatal("uncommitted checkpoint survived crash")
	}
	var category string
	if err := pool.QueryRow(context.Background(), "SELECT category FROM issue_status WHERE id=$1::uuid", statusID(1)).Scan(&category); err != nil || category != "backlog" {
		t.Fatalf("uncommitted update survived: %s %v; before=%s", category, err, before)
	}
	finish(t, s, current)
}
func TestValidationDetectsResidualBehindCursorAndBadPrerequisites(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	req := request(false)
	req.Parameters = asJSON(statusParameters{})
	if _, err := s.Create(ctx, req); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing writer attestation accepted")
	}
	j := create(t, s, false)
	for {
		var cp statusCheckpoint
		_ = decode(j.Checkpoint, &cp)
		if cp.Phase == "verify" {
			break
		}
		j = step(t, s, j)
	}
	// A legacy writer touches a row behind the completed scan.
	if _, err := pool.Exec(ctx, "UPDATE issue_status SET category='backlog' WHERE id=$1::uuid", statusID(1)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	failed, err := s.Mutate(ctx, j.ID, j.Revision, "advance")
	if err == nil || failed.Status != "paused" || !strings.Contains(failed.LastError, "legacy category remains") {
		t.Fatalf("false completion: %+v %v", failed, err)
	}
	// A different handler version cannot reinterpret stored checkpoint data.
	if _, err := pool.Exec(ctx, "UPDATE maintenance_job SET job_version=2 WHERE id=$1::uuid", j.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Mutate(ctx, j.ID, failed.Revision, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(ctx, j.ID, resumed.Revision, "advance"); !errors.Is(err, ErrConflict) {
		t.Fatalf("unsupported checkpoint version: %v", err)
	}
}
func TestPreflightChecksActualSchemaAndLedger(t *testing.T) {
	pool, s := fixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, request(true)); err == nil {
		t.Fatal("missing 478 accepted")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations VALUES ('478_issue_status_category_expand')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE issue_status DROP CONSTRAINT issue_status_category_check;
 ALTER TABLE issue_status ADD CONSTRAINT issue_status_category_check CHECK(category IN ('backlog','todo','in_progress','in_review','blocked','done','cancelled'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, request(true)); err == nil {
		t.Fatal("ledger hid incompatible CHECK")
	}
}

func TestConcurrentBusinessWriteWinsOverObservedLegacyValue(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	req := request(false)
	req.Options.BatchSize = 10
	req.Options.LockTimeoutMS = 1000
	req.Options.StatementTimeoutMS = 3000
	j, err := s.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(ctx)
	if _, err = writer.Exec(ctx, "UPDATE issue_status SET category='done',name='concurrent edit' WHERE id=$1::uuid", statusID(8)); err != nil {
		t.Fatal(err)
	}
	outcome := make(chan error, 1)
	go func() { _, err := s.Mutate(ctx, j.ID, j.Revision, "advance"); outcome <- err }()
	// Wait only for this isolated schema's backfill UPDATE to block behind writer.
	deadline := time.Now().Add(2 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
  AND wait_event_type='Lock' AND query LIKE 'UPDATE issue_status SET category=issue_status_category%'`).Scan(&count); err != nil {
			break
		}
		if count > 0 {
			waiting = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-outcome; err != nil {
		t.Fatal(err)
	}
	if !waiting {
		t.Fatal("did not exercise concurrent recheck after lock wait")
	}
	var category, name string
	if err = pool.QueryRow(ctx, "SELECT category,name FROM issue_status WHERE id=$1::uuid", statusID(8)).Scan(&category, &name); err != nil {
		t.Fatal(err)
	}
	if category != "done" || name != "concurrent edit" {
		t.Fatalf("overwrote business write: %s %s", category, name)
	}
}
func TestUnknownStoredDataCannotComplete(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	// Simulate pre-existing invalid data under NOT VALID compatibility CHECKs.
	if _, err := pool.Exec(ctx, `ALTER TABLE issue_status DROP CONSTRAINT issue_status_category_check;
 UPDATE issue_status SET category='unexpected' WHERE id='00000000-0000-0000-0000-000000000008'`); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../migrations/478_issue_status_category_expand.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	j := create(t, s, true)
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Millisecond)
		next, err := s.Mutate(ctx, j.ID, j.Revision, "advance")
		if err != nil {
			if next.Status != "paused" || !strings.Contains(next.LastError, "unknown category") {
				t.Fatalf("unexpected error: %+v %v", next, err)
			}
			return
		}
		j = next
	}
	t.Fatal("invalid data accepted")
}
func TestBoundedKeysetUsesExistingIndex(t *testing.T) {
	pool, _ := fixture(t)
	ctx := context.Background()
	// Bulk fixture is deliberately large enough for the planner to prefer the
	// existing ID index, without forcing planner switches.
	if _, err := pool.Exec(ctx, `INSERT INTO issue_status(id,workspace_id,key,name,category,color)
 SELECT md5(n::text)::uuid,'00000000-0000-0000-0000-000000000001'::uuid,
 'status_'||n,'Status '||n,'unstarted','#123456' FROM generate_series(1,10000) n;
 ANALYZE issue_status`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := pool.QueryRow(ctx, `EXPLAIN (FORMAT JSON) SELECT id::text,key,category,is_system,archived_at IS NOT NULL
 FROM issue_status WHERE id>'80000000-0000-0000-0000-000000000000'::uuid ORDER BY issue_status.id LIMIT 100`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "Index Scan") || !strings.Contains(plan, "issue_status_pkey_uidx") || strings.Contains(plan, "Seq Scan") {
		t.Fatalf("keyset page did not use bounded index access: %s", plan)
	}
	t.Log("10,000-row fixture: Limit -> Index Scan using issue_status_pkey_uidx, no sequential scan")
}

type counterProcessor struct{}

func (counterProcessor) Type() string { return "example_repair" }
func (counterProcessor) Version() int { return 2 }
func (counterProcessor) Validate(scope string, dry bool, p json.RawMessage) (json.RawMessage, error) {
	if scope != "workspace:example" {
		return nil, ErrInvalid
	}
	var params struct {
		Label string `json:"label"`
	}
	if err := decode(p, &params); err != nil {
		return nil, err
	}
	return asJSON(params), nil
}
func (counterProcessor) Preflight(context.Context, pgx.Tx) error { return nil }
func (counterProcessor) Step(ctx context.Context, tx pgx.Tx, j Job) (json.RawMessage, json.RawMessage, json.RawMessage, bool, error) {
	return asJSON(map[string]any{"timestamp": "2026-09-15T00:00:00Z", "sequence": 4}),
		asJSON(map[string]int{"repaired": 4}), asJSON(map[string]bool{"verified": true}), true, nil
}
func TestSharedTableSupportsDifferentProcessorAndCursor(t *testing.T) {
	pool, s := fixture(t)
	category := create(t, s, true)
	service := NewService(pool, StatusCategory{}, counterProcessor{})
	j, err := service.Create(context.Background(), CreateRequest{Type: "example_repair", Version: 2, Scope: "workspace:example",
		IdempotencyKey: uuid.NewString(), Parameters: asJSON(map[string]string{"label": "example"})})
	if err != nil {
		t.Fatal(err)
	}
	j = step(t, service, j)
	if j.Status != "completed" || !strings.Contains(string(j.Checkpoint), "timestamp") {
		t.Fatalf("generic processor failed: %+v", j)
	}
	unchanged, err := s.Get(context.Background(), category.ID)
	if err != nil || unchanged.Revision != 0 {
		t.Fatal("other job was modified")
	}
}

func TestContractPreservesAuditAndRejectsCategoryProcessor(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	ctx := context.Background()
	completed := finish(t, s, create(t, s, false))
	pending := create(t, s, false)
	for _, name := range []string{"491_issue_status_category_backfill", "492_issue_status_category_contract", "493_issue_status_category_validate", "494_issue_status_category_read_contract"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", name+".up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	audit, err := s.Get(ctx, completed.ID)
	if err != nil || string(audit.Result) != string(completed.Result) || audit.Status != "completed" || audit.Revision != completed.Revision {
		t.Fatalf("audit changed: %+v %v", audit, err)
	}
	if _, err := s.Create(ctx, request(true)); err == nil {
		t.Fatal("contracted schema accepted v1 category job")
	}
	failed, err := s.Mutate(ctx, pending.ID, pending.Revision, "advance")
	if err == nil || failed.Status != "paused" {
		t.Fatalf("pre-contract pending job did not fail closed: %+v %v", failed, err)
	}
}
