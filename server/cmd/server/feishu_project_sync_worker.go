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

func runFeishuProjectSyncWorker(ctx context.Context, queries *db.Queries, pool *pgxpool.Pool) {
	store := newStorageFromEnv()
	ticker := time.NewTicker(feishuProjectSyncInterval)
	defer ticker.Stop()

	runFeishuProjectSyncOnce(ctx, queries, pool, store)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runFeishuProjectSyncOnce(ctx, queries, pool, store)
		}
	}
}

func runFeishuProjectSyncOnce(ctx context.Context, queries *db.Queries, pool *pgxpool.Pool, store service.FeishuProjectStorage) {
	configs, err := queries.ListEnabledFeishuProjectIntegrations(ctx)
	if err != nil {
		slog.Warn("Feishu Project sync scan failed", "error", err)
		return
	}
	svc := &service.FeishuProjectSyncService{Queries: queries, Tx: pool, Client: service.NewFeishuProjectClient(), Storage: store}
	for _, cfg := range configs {
		lockKey := "feishu-project-sync:" + service.UUIDString(cfg.ID)
		locked, unlock, err := tryFeishuProjectSyncLock(ctx, pool, lockKey)
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

func tryFeishuProjectSyncLock(ctx context.Context, pool *pgxpool.Pool, key string) (bool, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, func() {}, err
	}

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", key).Scan(&locked); err != nil {
		conn.Release()
		return false, func() {}, err
	}
	if !locked {
		conn.Release()
		return false, func() {}, nil
	}

	return true, func() {
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key); err != nil {
			slog.Warn("Feishu Project sync unlock failed", "error", err)
		}
		conn.Release()
	}, nil
}
