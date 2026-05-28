package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const feishuProjectSyncInterval = 5 * time.Minute

func runFeishuProjectSyncWorker(ctx context.Context, queries *db.Queries, pool *pgxpool.Pool, taskSvc *service.TaskService) {
	store := newStorageFromEnv()
	ticker := time.NewTicker(feishuProjectSyncInterval)
	defer ticker.Stop()

	runFeishuProjectSyncOnce(ctx, queries, pool, store, taskSvc)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runFeishuProjectSyncOnce(ctx, queries, pool, store, taskSvc)
		}
	}
}

func runFeishuProjectSyncOnce(ctx context.Context, queries *db.Queries, pool *pgxpool.Pool, store service.FeishuProjectStorage, taskSvc *service.TaskService) {
	configs, err := queries.ListEnabledFeishuProjectIntegrations(ctx)
	if err != nil {
		slog.Warn("Feishu Project sync scan failed", "error", err)
		return
	}
	svc := &service.FeishuProjectSyncService{Queries: queries, Tx: pool, Client: service.NewFeishuProjectClient(), Storage: store, TaskService: taskSvc}
	for _, cfg := range configs {
		locked, unlock, err := service.TryAcquireFeishuProjectSyncLock(ctx, pool, cfg.ID)
		if err != nil {
			slog.Warn("Feishu Project sync lock failed", "integration_id", service.UUIDString(cfg.ID), "project_key", cfg.ProjectKey, "error", err)
			continue
		}
		if !locked {
			continue
		}
		if _, err := svc.Sync(ctx, cfg, "scheduled"); err != nil {
			slog.Warn("Feishu Project scheduled sync failed", "integration_id", service.UUIDString(cfg.ID), "project_key", cfg.ProjectKey, "error", err)
		}
		unlock()
	}
}
