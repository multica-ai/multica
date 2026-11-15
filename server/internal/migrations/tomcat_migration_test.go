package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestTomcatProtocolFamilyMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("requires the checkout's managed Postgres DATABASE_URL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// A connection-local table shadows the real table. Closing the connection
	// removes the fixture; no persistent schema or existing rows are touched.
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE runtime_profile (protocol_family text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, ctx, conn, "656_runtime_profile_add_amp.up.sql")
	assertInsertCheckViolation(t, ctx, conn, "INSERT INTO runtime_profile VALUES ('tomcat')")
	applyMigrationFile(t, ctx, conn, "660_runtime_profile_add_tomcat.up.sql")
	for _, family := range agent.SupportedTypes {
		if _, err := conn.Exec(ctx, "INSERT INTO runtime_profile VALUES ($1)", family); err != nil {
			t.Fatalf("family %q rejected after up: %v", family, err)
		}
	}
	var validated bool
	if err := conn.QueryRow(ctx, "SELECT convalidated FROM pg_constraint WHERE conrelid='runtime_profile'::regclass AND conname='runtime_profile_protocol_family_check'").Scan(&validated); err != nil || validated {
		t.Fatalf("validated=%v err=%v", validated, err)
	}
	applyMigrationFile(t, ctx, conn, "660_runtime_profile_add_tomcat.down.sql")
	assertInsertCheckViolation(t, ctx, conn, "INSERT INTO runtime_profile VALUES ('tomcat')")
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM runtime_profile WHERE protocol_family='tomcat'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback changed existing row: count=%d err=%v", count, err)
	}
	assertInsertCheckViolation(t, ctx, conn, "INSERT INTO runtime_profile VALUES ('unknown-provider')")
	for _, family := range agent.SupportedTypes {
		if family == "tomcat" {
			continue
		}
		if _, err := conn.Exec(ctx, "INSERT INTO runtime_profile VALUES ($1)", family); err != nil {
			t.Fatalf("rollback removed upstream provider %q: %v", family, err)
		}
	}
	applyMigrationFile(t, ctx, conn, "660_runtime_profile_add_tomcat.up.sql")
	if _, err := conn.Exec(ctx, "INSERT INTO runtime_profile VALUES ('tomcat')"); err != nil {
		t.Fatal(err)
	}
}
