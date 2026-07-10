package rounds

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRoundMigrationPersistsTriggerTargets(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not configured")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var columns int
	err = pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.columns WHERE table_name='cerebro_round_held_trigger' AND column_name IN ('target_type','target_id')`).Scan(&columns)
	if err != nil {
		t.Fatal(err)
	}
	if columns != 2 {
		t.Fatalf("round trigger target columns = %d, want 2", columns)
	}
}
