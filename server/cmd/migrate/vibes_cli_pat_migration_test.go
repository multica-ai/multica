package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const vibesCLIPATIndexMigration = "379_vibes_cli_pat_binding_identity_index"
const vibesCLIPATIndexName = "vibes_cli_pat_binding_pat_id_uidx"

func TestVIBESCLIPATBindingMigrationSeparatesTableAndUniqueIndex(t *testing.T) {
	tableSQL := readMigrationForTest(t, "378_vibes_cli_pat_binding.up.sql")
	upperTableSQL := strings.ToUpper(tableSQL)
	for _, forbidden := range []string{"PRIMARY KEY", " UNIQUE", "FOREIGN KEY", "REFERENCES", "CREATE INDEX"} {
		if strings.Contains(upperTableSQL, forbidden) {
			t.Errorf("378 table migration contains forbidden %q", forbidden)
		}
	}
	if !strings.Contains(tableSQL, "pat_id UUID NOT NULL") {
		t.Error("378 table migration must keep pat_id as a required application identity")
	}

	indexSQL := strings.TrimSpace(readMigrationForTest(t, vibesCLIPATIndexMigration+".up.sql"))
	wantIndexSQL := "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + vibesCLIPATIndexName +
		" ON vibes_cli_pat_binding (pat_id);"
	if indexSQL != wantIndexSQL {
		t.Errorf("379 index migration = %q, want %q", indexSQL, wantIndexSQL)
	}
	if got := concurrentIndexCleanups[vibesCLIPATIndexMigration]; got != vibesCLIPATIndexName {
		t.Errorf("379 retry cleanup = %q, want %q", got, vibesCLIPATIndexName)
	}

	downIndexSQL := strings.TrimSpace(readMigrationForTest(t, vibesCLIPATIndexMigration+".down.sql"))
	if downIndexSQL != "DROP INDEX CONCURRENTLY IF EXISTS "+vibesCLIPATIndexName+";" {
		t.Errorf("379 down migration = %q", downIndexSQL)
	}
	if got := strings.TrimSpace(readMigrationForTest(t, "378_vibes_cli_pat_binding.down.sql")); got != "DROP TABLE IF EXISTS vibes_cli_pat_binding;" {
		t.Errorf("378 down migration = %q", got)
	}
}

func TestVIBESCLIPATBindingIndexRetryRemovesInterruptedBuild(t *testing.T) {
	pool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "vibes_cli_pat_retry_" + suffix
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	tableName := pgx.Identifier{schema, "vibes_cli_pat_binding"}.Sanitize()
	indexName := pgx.Identifier{schema, vibesCLIPATIndexName}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE")
	})
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+" (pat_id UUID NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	const duplicateID = "00000000-0000-0000-0000-000000000295"
	if _, err := pool.Exec(ctx, "INSERT INTO "+tableName+" (pat_id) VALUES ($1), ($1)", duplicateID); err != nil {
		t.Fatal(err)
	}
	createIndex := "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + indexName + " ON " + tableName + " (pat_id)"
	if _, err := pool.Exec(ctx, createIndex); err == nil {
		t.Fatal("interrupted unique-index build unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM "+tableName+" WHERE ctid IN (SELECT ctid FROM "+tableName+" LIMIT 1)"); err != nil {
		t.Fatal(err)
	}
	if err := cleanupInvalidConcurrentIndexHook(indexName)(ctx, pool); err != nil {
		t.Fatalf("cleanup interrupted index: %v", err)
	}
	if _, err := pool.Exec(ctx, createIndex); err != nil {
		t.Fatalf("retry unique-index build: %v", err)
	}
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT i.indisvalid FROM pg_index AS i WHERE i.indexrelid = to_regclass($1)`, indexName).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("retried unique index is not valid")
	}
}

func readMigrationForTest(t *testing.T, filename string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", filename))
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	return string(body)
}
