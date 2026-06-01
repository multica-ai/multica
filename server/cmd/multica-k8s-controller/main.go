package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// version is written by the build via -ldflags "-X main.version=...".
// Used as the daemon `cli_version` so Multica's CLI-version gate
// (MIN_QUICK_CREATE_CLI_VERSION) accepts agent-create flows.
var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("controller exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.CLIVersion = version

	// In-cluster k8s client.
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	k, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return err
	}

	cli := daemon.NewClient(cfg.ServerBaseURL)
	cli.SetToken(cfg.Token)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registered, err := RegisterAll(ctx, cli, cfg)
	if err != nil {
		return err
	}
	logger.Info("registered runtimes", "count", len(registered))
	for _, r := range registered {
		logger.Info("runtime", "workspace_id", r.WorkspaceID, "runtime_id", r.RuntimeID, "provider", r.Provider)
	}

	// Heartbeat goroutine.
	go RunHeartbeatLoop(ctx, cli, registered, cfg.HeartbeatInterval)

	// Failure sweep — every 30s.
	sweepTicker := time.NewTicker(30 * time.Second)
	defer sweepTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sweepTicker.C:
				if err := SweepFailedJobs(ctx, cli, k, cfg.Namespace); err != nil {
					logger.Warn("sweep failed jobs", "error", err)
				}
			}
		}
	}()

	// Main poll-claim-dispatch loop.
	poll := time.NewTicker(cfg.PollInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down — deregistering")
			ids := make([]string, 0, len(registered))
			for _, r := range registered {
				ids = append(ids, r.RuntimeID)
			}
			deregCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = cli.Deregister(deregCtx, ids)
			cancel()
			return nil
		case <-poll.C:
			for _, r := range registered {
				dispatched, err := DispatchOnce(ctx, cli, k, cfg.Namespace, cfg.ImagePullSecret, r, cfg.ClaudeBroker, cfg.RepoCache)
				if err != nil {
					logger.Warn("dispatch", "runtime", r.RuntimeID, "error", err)
					continue
				}
				if dispatched {
					logger.Info("dispatched task", "runtime", r.RuntimeID)
				}
			}
		}
	}
}
