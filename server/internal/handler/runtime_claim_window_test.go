package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRuntimeClaimWindow_DefaultsNull(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, _, _ := runtimeVisibilityFixture(t)
	var start pgtype.Time
	var timezone pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT claim_window_start, claim_window_timezone
		FROM agent_runtime
		WHERE id = $1
	`, runtimeID).Scan(&start, &timezone); err != nil {
		t.Fatalf("load runtime claim window: %v", err)
	}
	if start.Valid || timezone.Valid {
		t.Fatalf("claim window defaults = start valid %v, timezone valid %v; want both false", start.Valid, timezone.Valid)
	}
}
