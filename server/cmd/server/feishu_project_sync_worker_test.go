package main

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The worker should run reconcile when last_reconciled_at is older than the
// configured interval (or NULL — first tick after deploy) and otherwise run
// the cheap incremental path.
func TestFeishuProjectSyncTriggerFor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	interval := service.FeishuProjectReconcileInterval()

	tests := []struct {
		name string
		cfg  db.FeishuProjectIntegration
		want string
	}{
		{
			name: "never reconciled → reconcile on first tick",
			cfg:  db.FeishuProjectIntegration{},
			want: "reconcile",
		},
		{
			name: "reconciled exactly at interval boundary → reconcile",
			cfg: db.FeishuProjectIntegration{LastReconciledAt: pgtype.Timestamptz{
				Time: now.Add(-interval), Valid: true,
			}},
			want: "reconcile",
		},
		{
			name: "reconciled slightly past interval → reconcile",
			cfg: db.FeishuProjectIntegration{LastReconciledAt: pgtype.Timestamptz{
				Time: now.Add(-interval - time.Minute), Valid: true,
			}},
			want: "reconcile",
		},
		{
			name: "reconciled within interval → scheduled (incremental)",
			cfg: db.FeishuProjectIntegration{LastReconciledAt: pgtype.Timestamptz{
				Time: now.Add(-interval + time.Minute), Valid: true,
			}},
			want: "scheduled",
		},
		{
			name: "reconciled minutes ago → scheduled (incremental)",
			cfg: db.FeishuProjectIntegration{LastReconciledAt: pgtype.Timestamptz{
				Time: now.Add(-5 * time.Minute), Valid: true,
			}},
			want: "scheduled",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := feishuProjectSyncTriggerFor(tc.cfg, now); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
