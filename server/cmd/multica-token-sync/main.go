package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	cfg, err := ParseFlags(os.Args[1:])
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	logger := newLogger(cfg.Verbose)
	if err := run(cfg, logger); err != nil {
		logger.Error("sync failed", "error", err)
		os.Exit(1)
	}
}

func run(cfg *Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Debug("starting", "version", version, "once", cfg.Once, "namespace", cfg.Namespace, "secret", cfg.SecretName)

	k, err := LoadClusterClient(cfg.Context)
	if err != nil {
		return err
	}
	kc := &macOSKeychain{}

	if cfg.Once {
		_, err := SyncOnce(ctx, cfg, k, kc, logger)
		return err
	}
	SyncLoop(ctx, cfg, k, kc, logger)
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
