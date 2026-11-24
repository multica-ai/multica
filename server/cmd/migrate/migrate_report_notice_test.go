package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestMigrationReportForwardsOnlyMarkedNotices runs the notices a migration
// run really produces through the runner's filter. The server's own chatter
// cannot be filtered by condition code — "does not exist, skipping" and the
// USING INDEX rename carry SQLSTATE 00000 just like a plain RAISE — so only
// the explicit report marker may pass. (MUL-7212)
func TestMigrationReportForwardsOnlyMarkedNotices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := pgx.ParseConfig(testDatabaseURL())
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	var reports, dropped []string
	cfg.OnNotice = func(_ *pgconn.PgConn, notice *pgconn.Notice) {
		if report, ok := migrationReport(notice); ok {
			reports = append(reports, report)
			return
		}
		dropped = append(dropped, notice.Code+" "+notice.Message)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Skipf("could not connect to %s: %v", testDatabaseURL(), err)
	}
	defer conn.Close(context.Background())

	// Temp objects only: nothing outlives the connection.
	if _, err := conn.Exec(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS migrate_report_probe (id int);
		CREATE TEMP TABLE IF NOT EXISTS migrate_report_probe (id int);
		DROP TABLE IF EXISTS migrate_report_probe_missing;
		CREATE UNIQUE INDEX migrate_report_probe_id ON migrate_report_probe (id);
		ALTER TABLE migrate_report_probe
			ADD CONSTRAINT migrate_report_probe_pkey PRIMARY KEY USING INDEX migrate_report_probe_id;
		DO $$ BEGIN
			RAISE NOTICE 'unmarked chatter';
			RAISE NOTICE 'migration report: renamed % row(s)', 2;
		END $$;
	`); err != nil {
		t.Fatalf("produce notices: %v", err)
	}

	if want := []string{"renamed 2 row(s)"}; !slices.Equal(reports, want) {
		t.Errorf("forwarded reports = %q, want %q", reports, want)
	}
	// The probe is only meaningful if the server really sent the chatter.
	if len(dropped) != 4 {
		t.Errorf("dropped notices = %q, want the 4 unmarked ones", dropped)
	}
}
