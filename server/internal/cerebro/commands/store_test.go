package commands

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var commandTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		fmt.Printf("Skipping command integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	commandTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func TestStoreCreateListDelete(t *testing.T) {
	if commandTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	var workspaceID uuid.UUID
	if err := commandTestPool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('Command Test', 'command-test-'||gen_random_uuid()) RETURNING id`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = commandTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, workspaceID)
	})
	store := NewStore(commandTestPool)
	created, err := store.Create(ctx, workspaceID, uuid.New(), "member", CommandInput{Key: "frontend-tests", Title: "Frontend tests", Argv: []string{"pnpm", "test"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, workspaceID)
	if err != nil || len(items) != 1 || items[0].Argv[1] != "test" {
		t.Fatalf("items = %+v error = %v", items, err)
	}
	if err := store.Delete(ctx, workspaceID, created.ID); err != nil {
		t.Fatal(err)
	}
	items, _ = store.List(ctx, workspaceID)
	if len(items) != 0 {
		t.Fatalf("items after delete = %+v", items)
	}
}
