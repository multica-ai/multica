package identitysync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// StartWorker launches the FIR-2596 sync worker in a goroutine and returns
// immediately. When BigQuery is not configured (CEREBRO_GWS_SYNC_BQ_PROJECT
// unset), it logs and does nothing — a self-host install without a data lake
// simply never runs the sync. Per-workspace opt-in is a separate gate
// (cerebro_workspace_auth_settings.google_workspace_sync_enabled).
func StartWorker(ctx context.Context, cerebro *cerebrodb.Queries, upstream *db.Queries) {
	syncer, ok, err := NewSyncerFromEnv(ctx, cerebro, upstream)
	if !ok {
		slog.Info("gws group sync worker disabled: CEREBRO_GWS_SYNC_BQ_PROJECT unset")
		return
	}
	if err != nil {
		slog.Error("gws group sync worker: bigquery init failed, worker not started", "error", err)
		return
	}
	w := &worker{
		cerebro:  cerebro,
		syncer:   syncer,
		interval: syncer.interval,
	}
	slog.Info("gws group sync worker started",
		"project", syncer.cfg.Project, "dataset", syncer.cfg.Dataset, "table", syncer.cfg.Table,
		"interval", syncer.interval.String())
	go w.run(ctx)
}

// Syncer reconciles BigQuery Google Workspace group membership into one
// workspace on demand. The background worker and login-time self-heal share it
// so both paths use the same source query and reconciliation logic.
type Syncer struct {
	cfg        config
	source     GroupSource
	reconciler *Reconciler
	interval   time.Duration
}

func NewSyncerFromEnv(ctx context.Context, cerebro *cerebrodb.Queries, upstream *db.Queries) (*Syncer, bool, error) {
	cfg, ok := loadConfig()
	if !ok {
		return nil, false, nil
	}
	source, err := newBigQuerySource(ctx, cfg)
	if err != nil {
		return nil, true, err
	}
	return &Syncer{
		cfg:        cfg,
		source:     source,
		reconciler: NewReconciler(&dbStore{cerebro: cerebro, upstream: upstream}),
		interval:   cfg.Interval,
	}, true, nil
}

func (s *Syncer) SyncWorkspaceNow(ctx context.Context, workspaceID pgtype.UUID) (SyncResult, error) {
	membership, err := s.source.FetchGroupMembers(ctx, SyncedGroupEmails())
	if err != nil {
		return SyncResult{}, fmt.Errorf("fetching membership from BigQuery: %w", err)
	}
	return s.SyncWorkspaceSnapshot(ctx, workspaceID, membership)
}

func (s *Syncer) SyncWorkspaceSnapshot(ctx context.Context, workspaceID pgtype.UUID, membership map[string][]string) (SyncResult, error) {
	return s.reconciler.SyncWorkspace(ctx, workspaceID, membership)
}

type worker struct {
	cerebro  *cerebrodb.Queries
	syncer   *Syncer
	interval time.Duration
}

func (w *worker) run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.tick(ctx) // run once on startup, then every interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick fetches the Google membership snapshot once, then reconciles every
// workspace that has opted in. A failure for one workspace is logged and the
// loop continues with the rest.
func (w *worker) tick(ctx context.Context) {
	workspaceIDs, err := w.cerebro.ListWorkspacesWithGoogleWorkspaceSync(ctx)
	if err != nil {
		slog.Warn("gws group sync: listing opted-in workspaces failed", "error", err)
		return
	}
	if len(workspaceIDs) == 0 {
		return
	}
	membership, err := w.syncer.source.FetchGroupMembers(ctx, SyncedGroupEmails())
	if err != nil {
		slog.Warn("gws group sync: fetching membership from BigQuery failed", "error", err)
		return
	}
	for _, wsID := range workspaceIDs {
		res, err := w.syncer.SyncWorkspaceSnapshot(ctx, wsID, membership)
		if err != nil {
			slog.Warn("gws group sync: workspace sync failed",
				"workspace_id", uuidString(wsID), "error", err)
			continue
		}
		slog.Info("gws group sync: workspace synced",
			"workspace_id", uuidString(wsID),
			"groups", res.GroupsProcessed,
			"members_added", res.MembersAdded,
			"members_removed", res.MembersRemoved,
			"users_skipped", res.UsersSkipped)
	}
}

// uuidString renders a pgtype.UUID for logging.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", u.Bytes[0:4], u.Bytes[4:6], u.Bytes[6:8], u.Bytes[8:10], u.Bytes[10:16])
}
