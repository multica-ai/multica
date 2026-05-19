// Package conversation owns the channel conversation, message, entity
// reference, and turn persistence boundary.
package conversation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChannelMigrationDDL(t *testing.T) {
	dbURL := os.Getenv("CHANNEL_MIGRATION_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set CHANNEL_MIGRATION_TEST_DATABASE_URL to validate channel migration DDL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect migration test database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin migration ddl transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	schema := fmt.Sprintf("channel_migration_check_%d", time.Now().UnixNano())
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create migration check schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set migration check search_path: %v", err)
	}
	if _, err := tx.Exec(ctx, channelMigrationPrerequisites); err != nil {
		t.Fatalf("create migration prerequisites: %v", err)
	}
	upSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "090_channel_integration.up.sql"))
	if err != nil {
		t.Fatalf("read channel migration up: %v", err)
	}
	if _, err := tx.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("run channel migration up: %v", err)
	}
	downSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "090_channel_integration.down.sql"))
	if err != nil {
		t.Fatalf("read channel migration down: %v", err)
	}
	if _, err := tx.Exec(ctx, string(downSQL)); err != nil {
		t.Fatalf("run channel migration down: %v", err)
	}
}

const channelMigrationPrerequisites = `
CREATE TABLE workspace (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE "user" (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE project (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE issue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE inbox_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE comment (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE agent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

CREATE TABLE agent_task_queue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);
`
