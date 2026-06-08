// CEREBRO-PATCH(cerebro-hatchet-worker): FIR-43 — net-new cerebro Hatchet
// worker. Hosts cron workflows that previously lived as one-off CLI jobs;
// first workflow is the daily USD→{DKK,EUR} reference-rate refresh that
// powers the display-currency feature (FIR-40). Designed to be one Sliplane
// container hosting many workflows — never one container per job. New cron
// jobs land here as additional NewStandaloneTask registrations.
//
// Deployment: see docs/cerebro-hatchet-worker.md. Required env:
//   HATCHET_CLIENT_TOKEN — Hatchet JWT (creds in Infisical /runs)
//   DATABASE_URL         — Multica Postgres (internal Sliplane URL)
//
// The display layer only ever reads the cached row from
// cerebro_exchange_rates; this worker is the only writer of those rows
// (the static seed in migration 9066 is a cold-start bootstrap that gets
// overwritten on the worker's first successful tick).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	hatchet "github.com/hatchet-dev/hatchet/sdks/go"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/exchangerates"
	"github.com/multica-ai/multica/server/internal/logger"
)

const workerName = "multica-hatchet-worker"

// fxInput is intentionally empty: the cron does not take parameters. Hatchet
// still requires concrete input/output types for the task signature.
type fxInput struct{}

type fxOutput struct {
	Base    string `json:"base"`
	Date    string `json:"date"`
	Written int    `json:"written"`
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("hatchet worker exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	base := envOr("CEREBRO_FX_BASE", "USD")
	symbols := splitCSV(envOr("CEREBRO_FX_SYMBOLS", "DKK,EUR"))
	endpoint := envOr("CEREBRO_FX_ENDPOINT", exchangerates.DefaultEndpoint)
	// Daily at 04:30 UTC: ECB publishes the daily reference rate around 16:00
	// CET (≈15:00 UTC), and Frankfurter mirrors within a couple of hours.
	// 04:30 UTC the next morning gives the source a generous settle window
	// while still landing the rate before EU business hours start.
	cronExpr := envOr("CEREBRO_FX_CRON", "30 4 * * *")

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	queries := cerebrodb.New(pool)
	httpClient := &http.Client{Timeout: 30 * time.Second}

	client, err := hatchet.NewClient()
	if err != nil {
		return err
	}

	fxTask := client.NewStandaloneTask(
		"cerebro-fetch-exchange-rates",
		func(taskCtx hatchet.Context, _ fxInput) (fxOutput, error) {
			out, err := refreshRates(taskCtx, httpClient, queries, endpoint, base, symbols)
			if err != nil {
				// Best-effort: a fetch failure leaves the previously-cached
				// rows in place (the display layer's fallback). The task
				// surfaces the error to Hatchet so retries / alerting work.
				slog.Error("fx refresh failed", "error", err)
				return out, err
			}
			slog.Info("fx refresh complete", "base", out.Base, "date", out.Date, "written", out.Written)
			return out, nil
		},
		hatchet.WithWorkflowCron(cronExpr),
		hatchet.WithWorkflowDescription("FIR-43 — Daily USD→{DKK,EUR} reference-rate refresh (ECB via Frankfurter)."),
	)

	worker, err := client.NewWorker(workerName, hatchet.WithWorkflows(fxTask))
	if err != nil {
		return err
	}

	// Bootstrap fetch: kick the cron once on startup so a fresh deploy does
	// not wait up to 24h for its first tick. Best-effort — a failure here is
	// logged but does not block worker start, because the static seed in
	// migration 9066 keeps the display usable until the next attempt.
	go func() {
		out, err := refreshRates(ctx, httpClient, queries, endpoint, base, symbols)
		if err != nil {
			slog.Warn("bootstrap fx refresh failed (cron will retry)", "error", err)
			return
		}
		slog.Info("bootstrap fx refresh complete", "base", out.Base, "date", out.Date, "written", out.Written)
	}()

	slog.Info("starting hatchet worker",
		"worker", workerName,
		"cron", cronExpr,
		"base", base,
		"symbols", strings.Join(symbols, ","),
	)
	return worker.StartBlocking(ctx)
}

// refreshRates is the shared fetch+store path used both by the cron task body
// and the bootstrap-on-startup goroutine.
func refreshRates(
	ctx context.Context,
	httpClient *http.Client,
	queries *cerebrodb.Queries,
	endpoint, base string,
	symbols []string,
) (fxOutput, error) {
	snap, err := exchangerates.Fetch(ctx, httpClient, endpoint, base, symbols)
	if err != nil {
		return fxOutput{Base: base}, err
	}
	written, err := exchangerates.Store(ctx, queries, base, snap)
	if err != nil {
		return fxOutput{Base: base, Date: snap.Date, Written: written}, err
	}
	return fxOutput{Base: base, Date: snap.Date, Written: written}, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
