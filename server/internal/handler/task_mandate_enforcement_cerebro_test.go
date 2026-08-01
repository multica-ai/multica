package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestTaskMandateEnforcementDefaultsOffWithoutFlagStore(t *testing.T) {
	t.Parallel()
	if (&Handler{}).taskMandateEnforcementEnabled(context.Background(), pgtype.UUID{Valid: true}) {
		t.Fatal("Task Mandate enforcement must default off without a feature flag store")
	}
}
