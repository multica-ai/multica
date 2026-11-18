//go:build integration

package db_test

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Only the explicitly designated disposable database is allowed. Each test
// applies just this migration in its own schema, including rollback coverage.
func enrollmentPersistenceDB(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	const disposableURL = "postgres://multica@127.0.0.1:25432/multica?sslmode=disable"
	if os.Getenv("DATABASE_URL") != disposableURL {
		t.Fatal("set DATABASE_URL to the designated disposable database: " + disposableURL)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, disposableURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("required disposable PostgreSQL: %v", err)
	}
	schema := "kagent_enrollment_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("clean test schema: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(disposableURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	up, err := os.ReadFile("../../../migrations/637_kagent_enrollment.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply enrollment migration: %v", err)
	}
	return pool, db.New(pool)
}

func enrollmentUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

func enrollmentNoRows(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected rejected CAS/scope (ErrNoRows), got %v", err)
	}
}

func TestKagentEnrollmentPersistenceLifecycle(t *testing.T) {
	_, q := enrollmentPersistenceDB(t)
	ctx := t.Context()
	key := db.UpsertKagentEnrollmentParams{AgentID: enrollmentUUID(), WorkspaceID: enrollmentUUID(), RuntimeID: enrollmentUUID()}
	row, err := q.UpsertKagentEnrollment(ctx, key)
	if err != nil || row.Revision != 1 || row.Generation != 1 || row.Phase != "pending" || row.DesiredState != "present" || row.AppliedGeneration.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		t.Fatalf("initial enrollment: %+v, %v", row, err)
	}
	report := db.ReportKagentEnrollmentStatusParams{AgentID: key.AgentID, WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, ExpectedRevision: 1, ExpectedGeneration: 1, ExpectedPhase: "pending", Phase: "ready"}
	row, err = q.ReportKagentEnrollmentStatus(ctx, report)
	if err != nil || row.Revision != 2 || row.Phase != "ready" || !row.AppliedGeneration.Valid || row.AppliedGeneration.Int64 != 1 {
		t.Fatalf("ready report: %+v, %v", row, err)
	}
	row, err = q.UpsertKagentEnrollment(ctx, key)
	if err != nil || row.Revision != 2 || row.Phase != "ready" || row.Generation != 1 {
		t.Fatalf("upsert must preserve progress: %+v, %v", row, err)
	}
	request := db.RequestKagentEnrollmentDesiredStateParams{AgentID: key.AgentID, WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, ExpectedGeneration: 1, DesiredState: "present"}
	row, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	if err != nil || row.Revision != 3 || row.Generation != 2 || row.Phase != "pending" || row.AppliedGeneration.Int64 != 1 {
		t.Fatalf("new configuration generation: %+v, %v", row, err)
	}
	_, err = q.ReportKagentEnrollmentStatus(ctx, report)
	enrollmentNoRows(t, err) // old ready report must not apply to generation 2
	_, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	enrollmentNoRows(t, err)
	request.ExpectedGeneration, request.DesiredState = 2, "absent"
	row, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	if err != nil || row.Revision != 4 || row.Generation != 3 || row.Phase != "draining" || row.DesiredState != "absent" {
		t.Fatalf("request removal: %+v, %v", row, err)
	}
	request.ExpectedGeneration, request.DesiredState = 3, "present"
	_, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	enrollmentNoRows(t, err) // restore cannot race drain
	_, err = q.UpsertKagentEnrollment(ctx, key)
	enrollmentNoRows(t, err)
	report.ExpectedRevision = 4
	report.ExpectedGeneration, report.ExpectedPhase, report.Phase = 3, "draining", "deleted"
	report.RemoteDeletionVerified = true
	_, err = q.ReportKagentEnrollmentStatus(ctx, report)
	enrollmentNoRows(t, err) // cannot skip deleting even with verification
	begin := db.BeginKagentEnrollmentDeleteParams{AgentID: key.AgentID, WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, ExpectedRevision: 4, ExpectedGeneration: 3}
	_, err = q.BeginKagentEnrollmentDelete(ctx, begin)
	enrollmentNoRows(t, err) // an empty local DB is not proof of remote quiescence
	begin.RemoteQuiescenceVerified = true
	row, err = q.BeginKagentEnrollmentDelete(ctx, begin)
	if err != nil || row.Revision != 5 || row.Phase != "deleting" {
		t.Fatalf("begin verified deletion: %+v, %v", row, err)
	}
	_, err = q.BeginKagentEnrollmentDelete(ctx, begin)
	enrollmentNoRows(t, err)
	_, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	enrollmentNoRows(t, err) // restore cannot race remote deletion
	report.ExpectedRevision = 5
	report.ExpectedPhase, report.RemoteDeletionVerified = "deleting", false
	_, err = q.ReportKagentEnrollmentStatus(ctx, report)
	enrollmentNoRows(t, err)
	report.RemoteDeletionVerified = true
	row, err = q.ReportKagentEnrollmentStatus(ctx, report)
	if err != nil || row.Revision != 6 || row.Phase != "deleted" || row.DesiredState != "absent" || row.AppliedGeneration.Int64 != 3 {
		t.Fatalf("verified tombstone: %+v, %v", row, err)
	}
	_, err = q.UpsertKagentEnrollment(ctx, key)
	enrollmentNoRows(t, err)
	request.DesiredState = "absent"
	_, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	enrollmentNoRows(t, err) // absent remains deleted
	rows, err := q.ListKagentEnrollments(ctx, db.ListKagentEnrollmentsParams{WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID})
	if err != nil || len(rows) != 1 || rows[0].AgentID != key.AgentID || rows[0].Phase != "deleted" {
		t.Fatalf("retained tombstone: %+v, %v", rows, err)
	}
	request.DesiredState = "present"
	row, err = q.RequestKagentEnrollmentDesiredState(ctx, request)
	if err != nil || row.Revision != 7 || row.Generation != 4 || row.Phase != "pending" || row.DesiredState != "present" {
		t.Fatalf("explicit restore after deletion: %+v, %v", row, err)
	}
	_, err = q.ReportKagentEnrollmentStatus(ctx, report)
	enrollmentNoRows(t, err) // old deletion report cannot delete restored enrollment
	row, err = q.GetKagentEnrollment(ctx, db.GetKagentEnrollmentParams{WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, AgentID: key.AgentID})
	if err != nil || row.Revision != 7 || row.Generation != 4 || row.Phase != "pending" {
		t.Fatalf("restored intent preserved: %+v, %v", row, err)
	}
}

func TestKagentEnrollmentPersistenceScopeAndTransitions(t *testing.T) {
	_, q := enrollmentPersistenceDB(t)
	ctx := t.Context()
	key := db.UpsertKagentEnrollmentParams{AgentID: enrollmentUUID(), WorkspaceID: enrollmentUUID(), RuntimeID: enrollmentUUID()}
	if _, err := q.UpsertKagentEnrollment(ctx, key); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"workspace", "runtime", "agent"} {
		t.Run(field, func(t *testing.T) {
			wrong := key
			switch field {
			case "workspace":
				wrong.WorkspaceID = enrollmentUUID()
			case "runtime":
				wrong.RuntimeID = enrollmentUUID()
			case "agent":
				wrong.AgentID = enrollmentUUID()
			}
			_, err := q.GetKagentEnrollment(ctx, db.GetKagentEnrollmentParams{WorkspaceID: wrong.WorkspaceID, RuntimeID: wrong.RuntimeID, AgentID: wrong.AgentID})
			enrollmentNoRows(t, err)
			_, err = q.RequestKagentEnrollmentDesiredState(ctx, db.RequestKagentEnrollmentDesiredStateParams{WorkspaceID: wrong.WorkspaceID, RuntimeID: wrong.RuntimeID, AgentID: wrong.AgentID, ExpectedGeneration: 1, DesiredState: "absent"})
			enrollmentNoRows(t, err)
			_, err = q.ReportKagentEnrollmentStatus(ctx, db.ReportKagentEnrollmentStatusParams{WorkspaceID: wrong.WorkspaceID, RuntimeID: wrong.RuntimeID, AgentID: wrong.AgentID, ExpectedRevision: 1, ExpectedGeneration: 1, ExpectedPhase: "pending", Phase: "ready"})
			enrollmentNoRows(t, err)
			if field != "agent" {
				_, err = q.UpsertKagentEnrollment(ctx, wrong)
				enrollmentNoRows(t, err)
				rows, err := q.ListKagentEnrollments(ctx, db.ListKagentEnrollmentsParams{WorkspaceID: wrong.WorkspaceID, RuntimeID: wrong.RuntimeID})
				if err != nil || len(rows) != 0 {
					t.Fatalf("scope leak: %+v, %v", rows, err)
				}
			}
		})
	}
	report := db.ReportKagentEnrollmentStatusParams{WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, AgentID: key.AgentID, ExpectedRevision: 1, ExpectedGeneration: 1, ExpectedPhase: "ready", Phase: "blocked", LastError: "remote controller unavailable"}
	_, err := q.ReportKagentEnrollmentStatus(ctx, report)
	enrollmentNoRows(t, err) // wrong expected phase
	report.ExpectedPhase = "pending"
	row, err := q.ReportKagentEnrollmentStatus(ctx, report)
	if err != nil || row.Phase != "blocked" || row.LastError != report.LastError || row.AppliedGeneration.Valid {
		t.Fatalf("blocked diagnostic: %+v, %v", row, err)
	}
	report.ExpectedRevision = row.Revision
	report.ExpectedPhase, report.Phase = "blocked", "ready"
	_, err = q.ReportKagentEnrollmentStatus(ctx, report)
	enrollmentNoRows(t, err) // retry must start from pending
	report.Phase, report.LastError = "pending", ""
	if _, err := q.ReportKagentEnrollmentStatus(ctx, report); err != nil {
		t.Fatal(err)
	}
	if _, err := q.RequestKagentEnrollmentDesiredState(ctx, db.RequestKagentEnrollmentDesiredStateParams{WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, AgentID: key.AgentID, ExpectedGeneration: 1, DesiredState: "absent"}); err != nil {
		t.Fatal(err)
	}
	begin := db.BeginKagentEnrollmentDeleteParams{WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, AgentID: key.AgentID, ExpectedRevision: 4, ExpectedGeneration: 2, RemoteQuiescenceVerified: true}
	for _, field := range []string{"workspace", "runtime", "agent", "generation"} {
		wrong := begin
		switch field {
		case "workspace":
			wrong.WorkspaceID = enrollmentUUID()
		case "runtime":
			wrong.RuntimeID = enrollmentUUID()
		case "agent":
			wrong.AgentID = enrollmentUUID()
		case "generation":
			wrong.ExpectedGeneration = 1
		}
		_, err := q.BeginKagentEnrollmentDelete(ctx, wrong)
		enrollmentNoRows(t, err)
	}
	report.ExpectedRevision = 4
	report.ExpectedGeneration, report.ExpectedPhase, report.Phase = 2, "draining", "blocked"
	if _, err := q.ReportKagentEnrollmentStatus(ctx, report); err != nil {
		t.Fatal(err)
	}
	_, err = q.BeginKagentEnrollmentDelete(ctx, begin)
	enrollmentNoRows(t, err)
	report.ExpectedRevision = 5
	report.ExpectedPhase, report.Phase = "blocked", "draining"
	if _, err := q.ReportKagentEnrollmentStatus(ctx, report); err != nil {
		t.Fatal(err)
	}
	_, err = q.BeginKagentEnrollmentDelete(ctx, begin)
	enrollmentNoRows(t, err) // draining phase has returned, but old delete approval is stale
	begin.ExpectedRevision = 6
	if _, err := q.BeginKagentEnrollmentDelete(ctx, begin); err != nil {
		t.Fatal(err)
	}
}

func TestKagentEnrollmentPersistenceConcurrentCAS(t *testing.T) {
	_, q := enrollmentPersistenceDB(t)
	ctx := t.Context()
	key := db.UpsertKagentEnrollmentParams{AgentID: enrollmentUUID(), WorkspaceID: enrollmentUUID(), RuntimeID: enrollmentUUID()}
	if _, err := q.UpsertKagentEnrollment(ctx, key); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			_, err := q.RequestKagentEnrollmentDesiredState(ctx, db.RequestKagentEnrollmentDesiredStateParams{WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, AgentID: key.AgentID, ExpectedGeneration: 1, DesiredState: "absent"})
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, pgx.ErrNoRows):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("CAS winners=%d rejected=%d", successes, rejected)
	}
}

func TestKagentEnrollmentPersistenceConstraintsAndRollback(t *testing.T) {
	pool, _ := enrollmentPersistenceDB(t)
	ctx := t.Context()
	fx := dbfx.New(pool, uuid.NewString(), "")
	validID := uuid.NewString()
	fx.InsertNoID(t, "kagent_enrollment", dbfx.Cols{"agent_id": validID, "workspace_id": fx.WorkspaceID, "runtime_id": uuid.NewString()}, "agent_id = $1", validID)
	for _, tc := range []struct {
		name, column string
		value        any
	}{
		{"zero revision", "revision", int64(0)},
		{"negative revision", "revision", int64(-1)},
		{"zero generation", "generation", int64(0)},
		{"negative generation", "generation", int64(-1)},
		{"unknown desired state", "desired_state", "forgotten"},
		{"unknown phase", "phase", "finished"},
		{"absent pending", "desired_state", "absent"},
		{"present deleted", "phase", "deleted"},
		{"unapplied ready", "phase", "ready"},
		{"future applied generation", "applied_generation", int64(2)},
		{"zero applied generation", "applied_generation", int64(0)},
		{"bounded diagnostic", "last_error", strings.Repeat("x", 2049)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, "UPDATE kagent_enrollment SET "+pgx.Identifier{tc.column}.Sanitize()+" = $1 WHERE agent_id = $2", tc.value, validID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("expected check violation, got %v", err)
			}
		})
	}
	down, err := os.ReadFile("../../../migrations/637_kagent_enrollment.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("migration rollback: %v", err)
	}
	var missing bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('kagent_enrollment') IS NULL").Scan(&missing); err != nil || !missing {
		t.Fatalf("rollback left table: missing=%v, %v", missing, err)
	}
}

func TestKagentEnrollmentPersistenceRevisionOverflow(t *testing.T) {
	pool, q := enrollmentPersistenceDB(t)
	key := db.UpsertKagentEnrollmentParams{AgentID: enrollmentUUID(), WorkspaceID: enrollmentUUID(), RuntimeID: enrollmentUUID()}
	if _, err := q.UpsertKagentEnrollment(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE kagent_enrollment SET revision=$1 WHERE agent_id=$2", int64(math.MaxInt64), key.AgentID); err != nil {
		t.Fatal(err)
	}
	_, err := q.ReportKagentEnrollmentStatus(t.Context(), db.ReportKagentEnrollmentStatusParams{
		AgentID: key.AgentID, WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID,
		ExpectedRevision: math.MaxInt64, ExpectedGeneration: 1, ExpectedPhase: "pending", Phase: "ready",
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22003" {
		t.Fatalf("expected explicit bigint overflow, got %v", err)
	}
	_, err = q.RequestKagentEnrollmentDesiredState(t.Context(), db.RequestKagentEnrollmentDesiredStateParams{
		AgentID: key.AgentID, WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID, ExpectedGeneration: 1, DesiredState: "absent",
	})
	if !errors.As(err, &pgErr) || pgErr.Code != "22003" {
		t.Fatalf("expected desired-state revision overflow, got %v", err)
	}
	row, err := q.GetKagentEnrollment(t.Context(), db.GetKagentEnrollmentParams{AgentID: key.AgentID, WorkspaceID: key.WorkspaceID, RuntimeID: key.RuntimeID})
	if err != nil || row.Revision != math.MaxInt64 || row.Generation != 1 || row.Phase != "pending" || row.AppliedGeneration.Valid {
		t.Fatalf("overflow partially persisted: %+v %v", row, err)
	}
}
