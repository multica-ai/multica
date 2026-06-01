package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("multica-repocache starting", "version", version, "commit", commit)
	if err := run(logger); err != nil {
		logger.Error("repocache exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		return err
	}
	cache := repocache.New(cfg.RepoRoot, logger)

	cli := daemon.NewClient(cfg.ServerBaseURL)
	cli.SetToken(cfg.Token)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go RunSyncLoop(ctx, logger, cli, cache, cfg)

	adminSrv := &http.Server{
		Addr:              cfg.AdminAddr,
		Handler:           NewAdminMux(cache, cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("admin server", "error", err)
		}
	}()

	metricsSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           NewMetricsMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = adminSrv.Shutdown(shutdownCtx)
	_ = metricsSrv.Shutdown(shutdownCtx)
	return nil
}
