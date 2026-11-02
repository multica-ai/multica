package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestMediaJobMigrationFreshUpgradeRollback(t *testing.T) {
	database := os.Getenv("DATABASE_URL")
	if database == "" {
		t.Skip("requires DATABASE_URL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// A private schema keeps rollback independent of the application's database.
	schema := "media_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	if _, err := conn.Exec(ctx, "SET search_path TO "+schema+",public"); err != nil {
		t.Fatal(err)
	}
	stems := []string{"9035_labrastro_media_job", "9036_labrastro_media_job_identity_index", "9037_labrastro_media_job_queue_index", "9038_labrastro_media_job_running_index"}
	for cycle := 0; cycle < 2; cycle++ {
		for _, stem := range stems {
			applyMigrationFile(t, ctx, conn, stem+".up.sql")
		}
		var indexes int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND tablename='media_job'", schema).Scan(&indexes); err != nil || indexes != 3 {
			t.Fatalf("indexes=%d err=%v", indexes, err)
		}
		var constraints int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE conrelid='media_job'::regclass AND contype='f'").Scan(&constraints); err != nil || constraints != 0 {
			t.Fatal("unexpected foreign keys", err)
		}
		_, err = conn.Exec(ctx, `INSERT INTO media_job(workspace_id,caller_id,uploader_type,uploader_id,request,capability_tag) VALUES (gen_random_uuid(),gen_random_uuid(),'member',gen_random_uuid(),'{}','media-image-codex-v1')`)
		if err != nil {
			t.Fatal(err)
		}
		for i := len(stems) - 1; i >= 0; i-- {
			applyMigrationFile(t, ctx, conn, stems[i]+".down.sql")
		}
		var table *string
		if err := conn.QueryRow(ctx, "SELECT to_regclass($1)::text", schema+".media_job").Scan(&table); err != nil || table != nil {
			t.Fatal("rollback left table", err)
		}
	}
}
