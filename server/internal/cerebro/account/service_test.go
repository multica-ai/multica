package account

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

const (
	accountTestEmail         = "account-test@multica.ai"
	accountTestName          = "Account Test User"
	accountTestWorkspaceSlug = "account-tests"
)

var (
	accountTestPool        *pgxpool.Pool
	accountTestUserID      pgtype.UUID
	accountTestWorkspaceID pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping account integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping account integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	if err := cleanupAccountTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean account test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	var userID, workspaceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, accountTestName, accountTestEmail).Scan(&userID); err != nil {
		fmt.Printf("Failed to create test user: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Account Tests", accountTestWorkspaceSlug, "Temporary workspace for account tests", "ACC").Scan(&workspaceID); err != nil {
		fmt.Printf("Failed to create test workspace: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	accountTestPool = pool
	accountTestUserID = userID
	accountTestWorkspaceID = workspaceID

	code := m.Run()

	if err := cleanupAccountTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean account test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func cleanupAccountTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, accountTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, accountTestEmail); err != nil {
		return err
	}
	return nil
}

func newAccountTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	if accountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping account integration test")
	}
	queries := cerebrodb.New(accountTestPool)
	bus := events.New()
	svc := NewService(queries, bus)
	cleanup := func() {
		_, _ = accountTestPool.Exec(context.Background(),
			`DELETE FROM cerebro_account WHERE workspace_id = $1`, accountTestWorkspaceID)
	}
	cleanup()
	return svc, cleanup
}

func TestServiceCreate_TrimsAndValidates(t *testing.T) {
	svc, cleanup := newAccountTestService(t)
	defer cleanup()

	if _, err := svc.Create(context.Background(), accountTestWorkspaceID, accountTestUserID, "  ", "abc"); !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("expected ErrInvalidProvider, got %v", err)
	}
	if _, err := svc.Create(context.Background(), accountTestWorkspaceID, accountTestUserID, "claude", " "); !errors.Is(err, ErrInvalidLoginIdentity) {
		t.Fatalf("expected ErrInvalidLoginIdentity, got %v", err)
	}
}

func TestServiceCRUD_RoundTrip(t *testing.T) {
	svc, cleanup := newAccountTestService(t)
	defer cleanup()

	ctx := context.Background()

	created, err := svc.Create(ctx, accountTestWorkspaceID, accountTestUserID, "  claude  ", "  jesper@firtal.dk  ")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if created.Provider != "claude" || created.LoginIdentity != "jesper@firtal.dk" {
		t.Fatalf("Create did not trim inputs: %+v", created)
	}

	got, err := svc.Get(ctx, accountTestWorkspaceID, created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("Get returned wrong account: got %v want %v", got.ID, created.ID)
	}

	list, err := svc.List(ctx, accountTestWorkspaceID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len: got %d want 1", len(list))
	}

	if _, err := svc.Create(ctx, accountTestWorkspaceID, accountTestUserID, "claude", "jesper@firtal.dk"); !errors.Is(err, ErrAccountAlreadyExists) {
		t.Fatalf("expected ErrAccountAlreadyExists, got %v", err)
	}

	if _, err := svc.Delete(ctx, accountTestWorkspaceID, accountTestUserID, created.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := svc.Get(ctx, accountTestWorkspaceID, created.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound after delete, got %v", err)
	}
}

func TestServiceGet_OtherWorkspace(t *testing.T) {
	svc, cleanup := newAccountTestService(t)
	defer cleanup()

	ctx := context.Background()
	created, err := svc.Create(ctx, accountTestWorkspaceID, accountTestUserID, "claude", "ws-isolation@firtal.dk")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var other pgtype.UUID
	other.Valid = true
	for i := range other.Bytes {
		other.Bytes[i] = 0xee
	}
	if _, err := svc.Get(ctx, other, created.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound for cross-workspace lookup, got %v", err)
	}
}
