package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/deploy"
	// Side-effect import: registers all built-in adapters at boot.
	_ "github.com/multica-ai/multica/server/pkg/deploy/adapters"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// shipHubAdapterPollInterval matches the existing Ship Hub reconciler
// cadence. 5 minutes is a reasonable default for "recently deployed?"
// telemetry and stays well under provider rate limits at the team
// sizes Ship Hub targets.
const shipHubAdapterPollInterval = 5 * time.Minute

// runShipHubAdapterPoller periodically iterates every deploy_environment
// whose adapter supports polling and refreshes its current_sha /
// current_deployed_at. Webhook-driven adapters (vercel, cloudflare,
// render) are still polled to handle the "Multica was offline when the
// webhook fired" case; the no-op path is cheap (one SELECT + one HTTP
// GET).
//
// Errors are logged per-environment so one bad token can't starve the
// rest of the workspace.
func runShipHubAdapterPoller(ctx context.Context, queries *db.Queries) {
	pollableKinds := deploy.PollableNames()
	if len(pollableKinds) == 0 {
		slog.Info("ship hub adapter poller: no pollable adapters registered, skipping")
		return
	}
	slog.Info("ship hub adapter poller started",
		"interval", shipHubAdapterPollInterval.String(),
		"adapters", pollableKinds)
	t := time.NewTicker(shipHubAdapterPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("ship hub adapter poller stopped")
			return
		case <-t.C:
			runShipHubAdapterPollOnce(ctx, queries, pollableKinds)
		}
	}
}

// runShipHubAdapterPollOnce is the per-tick body. Extracted so a future
// test can drive it without spinning up the goroutine.
func runShipHubAdapterPollOnce(ctx context.Context, queries *db.Queries, pollableKinds []string) {
	envs, err := queries.ListDeployEnvironmentsByAdapter(ctx, pollableKinds)
	if err != nil {
		slog.Warn("ship hub adapter poller: list envs failed", "error", err)
		return
	}
	for _, env := range envs {
		// Skip github_actions even though it's listed as pollable in
		// some configs — its adapter returns ErrPollNotSupported and
		// we'd burn one round-trip per env per tick to re-discover that.
		adapter, err := deploy.Get(env.AdapterKind)
		if err != nil {
			continue
		}
		if !adapter.SupportsPoll() {
			continue
		}
		cfg, err := queries.GetDeployAdapterConfig(ctx, env.ID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("ship hub adapter poller: load config failed",
					"environment_id", env.ID, "error", err)
			}
			continue
		}
		configPlain, err := secrets.DecryptString(cfg.ConfigEncrypted)
		if err != nil {
			slog.Warn("ship hub adapter poller: decrypt config failed",
				"environment_id", env.ID, "error", err)
			continue
		}
		adapterEnv := &deploy.Environment{
			ID:           env.ID,
			WorkspaceID:  env.WorkspaceID,
			AdapterKind:  env.AdapterKind,
			Config:       json.RawMessage(configPlain),
			TargetBranch: env.TargetBranch,
			Name:         env.Name,
		}
		state, err := adapter.PollCurrent(ctx, adapterEnv)
		if err != nil {
			if errors.Is(err, deploy.ErrPollNotSupported) {
				continue
			}
			slog.Warn("ship hub adapter poller: poll failed",
				"environment_id", env.ID, "adapter", env.AdapterKind, "error", err)
			continue
		}
		if state == nil || state.CurrentSHA == "" {
			continue
		}
		// No change since last tick — short-circuit so we don't write a
		// duplicate deploy row every 5 minutes.
		if env.CurrentSha.Valid && env.CurrentSha.String == state.CurrentSHA {
			continue
		}
		// New SHA detected. Insert a deploy row + bump the env's
		// current_sha. We don't publish a WS event from the poller —
		// the next page render picks up the change via the regular
		// query, and avoiding the publish path keeps the poller
		// independent of the WS hub.
		if _, err := queries.InsertDeploy(ctx, db.InsertDeployParams{
			WorkspaceID:   env.WorkspaceID,
			EnvironmentID: env.ID,
			Ref:           env.TargetBranch,
			Sha:           state.CurrentSHA,
			Status:        db.DeployStatusSucceeded,
			TriggeredBy:   pgtype.UUID{},
			StartedAt:     pgtype.Timestamptz{Time: state.DeployedAt, Valid: !state.DeployedAt.IsZero()},
			CompletedAt:   pgtype.Timestamptz{Time: state.DeployedAt, Valid: !state.DeployedAt.IsZero()},
			LogUrl:        pgtype.Text{String: state.LogURL, Valid: state.LogURL != ""},
		}); err != nil {
			slog.Warn("ship hub adapter poller: insert deploy failed",
				"environment_id", env.ID, "error", err)
			continue
		}
		if _, err := queries.UpdateDeployEnvironmentCurrent(ctx, db.UpdateDeployEnvironmentCurrentParams{
			ID:                env.ID,
			CurrentSha:        pgtype.Text{String: state.CurrentSHA, Valid: true},
			CurrentDeployedAt: pgtype.Timestamptz{Time: state.DeployedAt, Valid: !state.DeployedAt.IsZero()},
		}); err != nil {
			slog.Warn("ship hub adapter poller: update current sha failed",
				"environment_id", env.ID, "error", err)
		}
	}
}
