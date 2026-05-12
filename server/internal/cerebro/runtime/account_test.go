package runtime

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

	"github.com/multica-ai/multica/server/internal/cerebro/account"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
)

// JEH-997 integration tests for AccountService.RecordAccount. They require a
// reachable postgres (same DATABASE_URL convention as the account service's
// service_test.go); skip silently when unavailable so unit-only CI runs do
// not break. The tests own a dedicated workspace + runtime row so they do
// not interfere with the per-package fixture in account/service_test.go.

const (
	runtimeAccountTestEmail    = "runtime-account-test@multica.ai"
	runtimeAccountTestName     = "Runtime Account Test"
	runtimeAccountTestWSSlug   = "rt-account-tests"
	runtimeAccountTestRuntimeN = "Runtime Account Probe"
)

var (
	runtimeAccountTestPool      *pgxpool.Pool
	runtimeAccountTestUserID    pgtype.UUID
	runtimeAccountTestWSID      pgtype.UUID
	runtimeAccountTestRuntimeID pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping cerebro runtime account integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping cerebro runtime account integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	if err := cleanupRuntimeAccountFixture(ctx, pool); err != nil {
		fmt.Printf("clean fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	var userID, wsID, runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, runtimeAccountTestName, runtimeAccountTestEmail).Scan(&userID); err != nil {
		fmt.Printf("create user: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "Runtime Account Tests", runtimeAccountTestWSSlug, "Temp workspace for runtime account tests", "RAT").Scan(&wsID); err != nil {
		fmt.Printf("create workspace: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1, $2, $3, 'local', 'claude', 'online', '', $4)
		RETURNING id
	`, wsID, "rt-account-test-host", runtimeAccountTestRuntimeN, userID).Scan(&runtimeID); err != nil {
		fmt.Printf("create runtime: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	runtimeAccountTestPool = pool
	runtimeAccountTestUserID = userID
	runtimeAccountTestWSID = wsID
	runtimeAccountTestRuntimeID = runtimeID

	code := m.Run()

	if err := cleanupRuntimeAccountFixture(context.Background(), pool); err != nil {
		fmt.Printf("clean fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func cleanupRuntimeAccountFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, runtimeAccountTestWSSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, runtimeAccountTestEmail); err != nil {
		return err
	}
	return nil
}

func newRuntimeAccountService(t *testing.T) (*AccountService, *pgxpool.Pool) {
	t.Helper()
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping cerebro runtime account integration test")
	}
	queries := cerebrodb.New(runtimeAccountTestPool)
	bus := events.New()
	svc := NewAccountService(queries, account.NewService(queries, bus), bus)
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(),
			`UPDATE agent_runtime SET current_account_id = NULL WHERE id = $1`, runtimeAccountTestRuntimeID)
		_, _ = runtimeAccountTestPool.Exec(context.Background(),
			`DELETE FROM cerebro_account WHERE workspace_id = $1`, runtimeAccountTestWSID)
	})
	return svc, runtimeAccountTestPool
}

// TestAccountService_RecordAccount_LinksRuntimeOnFirstCall is the core JEH-997
// integration test: the first call creates the cerebro_account row AND moves
// agent_runtime.current_account_id from NULL to the account's ID.
func TestAccountService_RecordAccount_LinksRuntimeOnFirstCall(t *testing.T) {
	svc, pool := newRuntimeAccountService(t)
	ctx := context.Background()

	rec, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "sara@firtal.dk")
	if err != nil {
		t.Fatalf("RecordAccount failed: %v", err)
	}
	if !rec.AccountCreated {
		t.Errorf("AccountCreated = false, want true (first report)")
	}
	if !rec.RuntimeUpdated {
		t.Errorf("RuntimeUpdated = false, want true (initial bind)")
	}
	if rec.LoginIdentity != "sara@firtal.dk" || rec.Provider != "claude" {
		t.Errorf("identity=%q provider=%q, want sara@firtal.dk / claude", rec.LoginIdentity, rec.Provider)
	}

	// Confirm the DB write actually landed.
	var current pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT current_account_id FROM agent_runtime WHERE id = $1`, runtimeAccountTestRuntimeID).Scan(&current); err != nil {
		t.Fatalf("read runtime: %v", err)
	}
	if !current.Valid {
		t.Fatalf("agent_runtime.current_account_id still NULL after RecordAccount")
	}
	if current != rec.AccountID {
		t.Errorf("current_account_id = %v, want %v", current, rec.AccountID)
	}
}

// TestAccountService_RecordAccount_IdempotentOnRepeat covers the heartbeat
// re-report path: the second call must NOT touch the runtime row (it is
// already linked) and must report account_created=false / runtime_updated=
// false so the daemon stays quiet on the log.
func TestAccountService_RecordAccount_IdempotentOnRepeat(t *testing.T) {
	svc, _ := newRuntimeAccountService(t)
	ctx := context.Background()

	first, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "idempotent@firtal.dk")
	if err != nil {
		t.Fatalf("first RecordAccount: %v", err)
	}
	if !first.RuntimeUpdated {
		t.Fatalf("first call should set RuntimeUpdated=true")
	}

	second, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "idempotent@firtal.dk")
	if err != nil {
		t.Fatalf("second RecordAccount: %v", err)
	}
	if second.AccountCreated || second.RuntimeUpdated {
		t.Errorf("second call must be idempotent: got AccountCreated=%v RuntimeUpdated=%v", second.AccountCreated, second.RuntimeUpdated)
	}
	if second.AccountID != first.AccountID {
		t.Errorf("second call returned a different account id: %v vs %v", second.AccountID, first.AccountID)
	}
}

// TestAccountService_RecordAccount_SwitchesAccount covers the login-change
// path: when the daemon reports a new identity for the same runtime, the
// service must re-link agent_runtime.current_account_id to the new account.
func TestAccountService_RecordAccount_SwitchesAccount(t *testing.T) {
	svc, pool := newRuntimeAccountService(t)
	ctx := context.Background()

	first, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "before@firtal.dk")
	if err != nil {
		t.Fatalf("first RecordAccount: %v", err)
	}
	second, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "after@firtal.dk")
	if err != nil {
		t.Fatalf("second RecordAccount: %v", err)
	}
	if !second.RuntimeUpdated {
		t.Fatalf("switching identity must set RuntimeUpdated=true")
	}
	if !second.AccountCreated {
		t.Fatalf("switching to a brand-new identity must report AccountCreated=true on its first sighting")
	}
	if second.AccountID == first.AccountID {
		t.Fatalf("expected switch to bind to a different account row")
	}

	var current pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT current_account_id FROM agent_runtime WHERE id = $1`, runtimeAccountTestRuntimeID).Scan(&current); err != nil {
		t.Fatalf("read runtime: %v", err)
	}
	if current != second.AccountID {
		t.Errorf("runtime did not switch: current_account_id=%v want %v", current, second.AccountID)
	}
}

// TestAccountService_RecordAccount_RejectsBlankFields surfaces the
// handler.ErrRuntimeAccount* sentinels so the HTTP layer maps them to 400.
func TestAccountService_RecordAccount_RejectsBlankFields(t *testing.T) {
	svc, _ := newRuntimeAccountService(t)
	ctx := context.Background()

	if _, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "  ", "x"); !errors.Is(err, handler.ErrRuntimeAccountInvalidProvider) {
		t.Errorf("expected ErrRuntimeAccountInvalidProvider, got %v", err)
	}
	if _, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "  "); !errors.Is(err, handler.ErrRuntimeAccountInvalidLoginIdentity) {
		t.Errorf("expected ErrRuntimeAccountInvalidLoginIdentity, got %v", err)
	}
}

// TestAccountService_RecordAccount_MissingRuntime covers the stale-daemon
// case: an account row gets created (it is workspace-scoped, not runtime-
// scoped, so that succeeds) but the runtime link step surfaces ErrNoRows
// because the runtime row was deleted out from under the daemon.
func TestAccountService_RecordAccount_MissingRuntime(t *testing.T) {
	svc, _ := newRuntimeAccountService(t)
	ctx := context.Background()

	// Generate a runtime UUID that definitely does not exist.
	var bogus pgtype.UUID
	bogus.Valid = true
	for i := range bogus.Bytes {
		bogus.Bytes[i] = byte(0xff - i)
	}

	_, err := svc.RecordAccount(ctx, bogus, runtimeAccountTestWSID, "claude", "missing-runtime@firtal.dk")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows for missing runtime, got %v", err)
	}
}

// TestAccountService_RecordAccount_TimestampsExposed pins that the response
// carries valid created/updated_at — the HTTP layer relies on them to
// populate its JSON body, and a NULL would surface as an empty string.
func TestAccountService_RecordAccount_TimestampsExposed(t *testing.T) {
	svc, _ := newRuntimeAccountService(t)
	ctx := context.Background()

	rec, err := svc.RecordAccount(ctx, runtimeAccountTestRuntimeID, runtimeAccountTestWSID, "claude", "timestamps@firtal.dk")
	if err != nil {
		t.Fatalf("RecordAccount: %v", err)
	}
	if !rec.CreatedAt.Valid || !rec.UpdatedAt.Valid {
		t.Fatalf("timestamps not set: created=%v updated=%v", rec.CreatedAt, rec.UpdatedAt)
	}
	if rec.CreatedAt.Time.After(time.Now().Add(time.Second)) {
		t.Errorf("created_at is in the future: %v", rec.CreatedAt.Time)
	}
}
