package rounds

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRoundSimplificationMigration(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not configured")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ctx := context.Background()
	var cycleTables int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('cerebro_round_cycle','cerebro_round_cycle_item')`).Scan(&cycleTables)
	if err != nil {
		t.Fatal(err)
	}
	if cycleTables != 2 {
		t.Fatalf("round cycle tables = %d, want 2", cycleTables)
	}

	var legacyTables int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('cerebro_round_held_trigger','cerebro_round_run','cerebro_round_run_item')`).Scan(&legacyTables)
	if err != nil {
		t.Fatal(err)
	}
	if legacyTables != 0 {
		t.Fatalf("legacy round execution tables = %d, want 0", legacyTables)
	}

	var legacyColumns int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='cerebro_round' AND column_name IN ('mode','schedule_cron','timezone','next_run_at','cycle_opened_at')`).Scan(&legacyColumns)
	if err != nil {
		t.Fatal(err)
	}
	if legacyColumns != 0 {
		t.Fatalf("legacy round columns = %d, want 0", legacyColumns)
	}
}
