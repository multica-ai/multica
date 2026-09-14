package lark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DB-backed coverage for the lark_install_session projection (MUL-7340).
// The fake store in registration_service_test.go cannot prove any of this:
// workspace scoping, the first-writer-wins guard and the sweep predicates
// all live in SQL, and it was exactly a store-shaped assumption that
// produced the original bug.
//
// Without a database the suite exits green rather than red — the same
// contract every other DB-backed package in this tree follows, so a
// laptop without Postgres still runs the rest of the package.

var storeTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping lark DB tests: could not connect: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping lark DB tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	storeTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// newStoreBackedService builds the slice of RegistrationService the status
// path actually uses — config, the durable store, and the goroutine map.
// Each call returns an INDEPENDENT instance with its own empty map, which
// is how these tests stand in for a second backend replica.
func newStoreBackedService(t *testing.T, now func() time.Time) *RegistrationService {
	t.Helper()
	if storeTestPool == nil {
		t.Skip("no database")
	}
	return &RegistrationService{
		cfg:          RegistrationServiceConfig{Now: now}.withDefaults(),
		sessionStore: db.New(storeTestPool),
		sessions:     make(map[string]*registrationSession),
	}
}

func seedStoreSession(t *testing.T, s *RegistrationService, id string, ws pgtype.UUID, expiresAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.sessionStore.CreateLarkInstallSession(ctx, db.CreateLarkInstallSessionParams{
		ID:          id,
		WorkspaceID: ws,
		AgentID:     ws,
		InitiatorID: ws,
		ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		t.Fatalf("seed session %q: %v", id, err)
	}
	t.Cleanup(func() {
		_, _ = storeTestPool.Exec(context.Background(), `DELETE FROM lark_install_session WHERE id = $1`, id)
	})
}

func storeTestUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// TestInstallSessionIsReadableFromAnotherProcess is the regression. Instance
// A serves begin; instance B — a different replica, holding an empty session
// map — serves the browser's status poll. Before the row existed, B answered
// 404 and the dialog rendered "安装会话已失效或丢失，请重新扫码" ~5s in.
func TestInstallSessionIsReadableFromAnotherProcess(t *testing.T) {
	now := time.Now()
	instanceA := newStoreBackedService(t, func() time.Time { return now })
	instanceB := newStoreBackedService(t, func() time.Time { return now })
	ctx := context.Background()
	ws := storeTestUUID(t, "11111111-1111-1111-1111-111111111111")

	seedStoreSession(t, instanceA, "cross-process-1", ws, now.Add(time.Hour))

	instanceB.mu.Lock()
	mapped := len(instanceB.sessions)
	instanceB.mu.Unlock()
	if mapped != 0 {
		t.Fatalf("precondition: instance B must hold no in-process session, got %d", mapped)
	}

	state, err := instanceB.GetSession(ctx, ws, "cross-process-1")
	if err != nil {
		t.Fatalf("status read on the instance that did not serve begin: %v", err)
	}
	if state.Status != RegistrationStatusPending {
		t.Errorf("Status: got %q want pending", state.Status)
	}
	if state.InitiatorID != ws {
		t.Errorf("InitiatorID must survive the round-trip; got %v", state.InitiatorID)
	}
}

// TestInstallSessionReadIsWorkspaceScoped pins the SQL WHERE clause: a
// valid session id read from the wrong workspace is indistinguishable from
// one that never existed.
func TestInstallSessionReadIsWorkspaceScoped(t *testing.T) {
	now := time.Now()
	s := newStoreBackedService(t, func() time.Time { return now })
	ctx := context.Background()
	ws := storeTestUUID(t, "11111111-1111-1111-1111-111111111111")
	otherWs := storeTestUUID(t, "22222222-2222-2222-2222-222222222222")

	seedStoreSession(t, s, "scoped-1", ws, now.Add(time.Hour))

	if _, err := s.GetSession(ctx, otherWs, "scoped-1"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("cross-workspace read: want ErrRegistrationSessionNotFound, got %v", err)
	}
	if _, err := s.GetSession(ctx, ws, "scoped-1"); err != nil {
		t.Errorf("same-workspace read: %v", err)
	}
}

// TestInstallSessionTerminalWriteIsFirstWriterWins pins the
// `status = 'pending'` predicate on both terminal UPDATEs. The expiry
// deadline and a poll result can fire concurrently; the user must keep the
// outcome they were already shown, and the loser must report no rows rather
// than an error.
func TestInstallSessionTerminalWriteIsFirstWriterWins(t *testing.T) {
	now := time.Now()
	s := newStoreBackedService(t, func() time.Time { return now })
	ctx := context.Background()
	ws := storeTestUUID(t, "11111111-1111-1111-1111-111111111111")

	seedStoreSession(t, s, "race-1", ws, now.Add(time.Hour))
	sess := &registrationSession{id: "race-1", workspaceID: ws}

	s.markError(sess, RegistrationReasonAccessDenied, "user denied")
	s.markError(sess, RegistrationReasonExpired, "qr expired")
	s.markSuccess(sess, storeTestUUID(t, "33333333-3333-3333-3333-333333333333"))

	state, err := s.GetSession(ctx, ws, "race-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if state.Status != RegistrationStatusError {
		t.Fatalf("Status: got %q want error — a late success must not overwrite a recorded failure", state.Status)
	}
	if state.ErrorReason != RegistrationReasonAccessDenied {
		t.Errorf("ErrorReason: got %q want %q", state.ErrorReason, RegistrationReasonAccessDenied)
	}

	// The loser's own return value must be pgx.ErrNoRows, which markError
	// treats as an expected outcome rather than logging it as a failure.
	if _, err := s.sessionStore.MarkLarkInstallSessionError(ctx, db.MarkLarkInstallSessionErrorParams{
		ID:          "race-1",
		WorkspaceID: ws,
		ErrorReason: RegistrationReasonExpired,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("second terminal write: want pgx.ErrNoRows, got %v", err)
	}
}

// TestInstallSessionSweepKeepsLiveRows pins the sweep predicates against
// real SQL: it must drop terminal rows past gc_after and pending rows whose
// QR already died, while leaving a live pending row alone. A sweep that took
// live rows would reintroduce the 404 this table exists to remove.
func TestInstallSessionSweepKeepsLiveRows(t *testing.T) {
	now := time.Now()
	s := newStoreBackedService(t, func() time.Time { return now })
	ctx := context.Background()
	ws := storeTestUUID(t, "11111111-1111-1111-1111-111111111111")

	seedStoreSession(t, s, "sweep-live", ws, now.Add(time.Hour))
	seedStoreSession(t, s, "sweep-dead-pending", ws, now.Add(-time.Hour))
	seedStoreSession(t, s, "sweep-terminal", ws, now.Add(time.Hour))

	// Terminal, with gc_after already in the past.
	if _, err := s.sessionStore.MarkLarkInstallSessionError(ctx, db.MarkLarkInstallSessionErrorParams{
		ID:          "sweep-terminal",
		WorkspaceID: ws,
		ErrorReason: RegistrationReasonAccessDenied,
		GcAfter:     pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("mark terminal: %v", err)
	}

	s.sweepSessions(ctx)

	if _, err := s.GetSession(ctx, ws, "sweep-live"); err != nil {
		t.Errorf("live pending row must survive the sweep, got %v", err)
	}
	if _, err := s.GetSession(ctx, ws, "sweep-dead-pending"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("pending row past expires_at should be swept, got %v", err)
	}
	if _, err := s.GetSession(ctx, ws, "sweep-terminal"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("terminal row past gc_after should be swept, got %v", err)
	}
}
