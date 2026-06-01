package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// SyncOnce iterates configured workspaces, fetches each workspace's repo list
// from Multica, and asks the Cache to mirror them. Errors from one workspace
// do not abort sync of the others — they're aggregated and returned together.
func SyncOnce(ctx context.Context, cli *daemon.Client, cache *repocache.Cache, cfg *Config) error {
	var errs []string
	for _, w := range cfg.Workspaces {
		resp, err := cli.GetWorkspaceRepos(ctx, w.ID)
		if err != nil {
			syncTotal.WithLabelValues(w.ID, "repos_fetch_error").Inc()
			errs = append(errs, fmt.Sprintf("workspace %s: get repos: %v", w.ID, err))
			continue
		}
		repos := make([]repocache.RepoInfo, 0, len(resp.Repos))
		for _, r := range resp.Repos {
			repos = append(repos, repocache.RepoInfo{URL: r.URL})
		}
		start := time.Now()
		if err := cache.Sync(w.ID, repos); err != nil {
			syncTotal.WithLabelValues(w.ID, "sync_error").Inc()
			errs = append(errs, fmt.Sprintf("workspace %s: sync: %v", w.ID, err))
			continue
		}
		fetchDuration.WithLabelValues(w.ID).Observe(time.Since(start).Seconds())
		syncTotal.WithLabelValues(w.ID, "ok").Inc()
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// RunSyncLoop runs SyncOnce on cfg.FetchInterval ticks until ctx is cancelled.
// A failed initial sync is logged but does not abort the loop.
func RunSyncLoop(ctx context.Context, logger *slog.Logger, cli *daemon.Client, cache *repocache.Cache, cfg *Config) {
	if err := SyncOnce(ctx, cli, cache, cfg); err != nil {
		logger.Warn("initial sync had errors", "error", err)
	}
	t := time.NewTicker(cfg.FetchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := SyncOnce(ctx, cli, cache, cfg); err != nil {
				logger.Warn("sync errors", "error", err)
			}
		}
	}
}
