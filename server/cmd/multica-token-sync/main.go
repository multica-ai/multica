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

// run wires Config to the sync pipeline. Real implementation lands in Task 4.
func run(cfg *Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	_ = ctx
	_ = cfg
	logger.Info("run stub", "version", version)
	return nil
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
