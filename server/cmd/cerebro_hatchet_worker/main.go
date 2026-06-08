// CEREBRO-PATCH(cerebro-hatchet-worker): FIR-43 — net-new cerebro Hatchet worker
// that hosts cron workflows previously run as one-off CLI jobs. It POSTs fetched
// rates to the backend (no DB credential — see CEREBRO_FX_INGEST_URL below);
// first workflow is the daily USD→{DKK,EUR} reference-rate refresh that
// powers the display-currency feature (FIR-40). Designed to be one Sliplane
// container hosting many workflows — never one container per job. New cron
// jobs land here as additional NewStandaloneTask registrations.
//
// Deployment: see docs/cerebro-hatchet-worker.md. Required env:
//   HATCHET_CLIENT_TOKEN          — Hatchet JWT (creds in Infisical /runs)
//   CEREBRO_FX_INGEST_URL         — internal URL of multica-backend's
//                                   POST /api/cerebro/exchange-rates endpoint
//   CEREBRO_EXCHANGE_INGEST_KEY   — service key presented as a Bearer token
//
// The worker holds NO database credential: it fetches rates from Frankfurter
// and POSTs them to the backend, which is the only writer of
// cerebro_exchange_rates (the static seed in migration 9066 is a cold-start
// bootstrap that gets overwritten on the worker's first successful tick).
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

	ingestURL := strings.TrimSpace(os.Getenv("CEREBRO_FX_INGEST_URL"))
	if ingestURL == "" {
		return errors.New("CEREBRO_FX_INGEST_URL is required")
	}
	ingestKey := strings.TrimSpace(os.Getenv("CEREBRO_EXCHANGE_INGEST_KEY"))
	if ingestKey == "" {
		return errors.New("CEREBRO_EXCHANGE_INGEST_KEY is required")
	}
	base := envOr("CEREBRO_FX_BASE", "USD")
	symbols := splitCSV(envOr("CEREBRO_FX_SYMBOLS", "DKK,EUR"))
	endpoint := envOr("CEREBRO_FX_ENDPOINT", exchangerates.DefaultEndpoint)
	// Daily at 04:30 UTC: ECB publishes the daily reference rate around 16:00
	// CET (≈15:00 UTC), and Frankfurter mirrors within a couple of hours.
	// 04:30 UTC the next morning gives the source a generous settle window
	// while still landing the rate before EU business hours start.
	cronExpr := envOr("CEREBRO_FX_CRON", "30 4 * * *")

	httpClient := &http.Client{Timeout: 30 * time.Second}

	client, err := hatchet.NewClient()
	if err != nil {
		return err
	}

	fxTask := client.NewStandaloneTask(
		"cerebro-fetch-exchange-rates",
		func(taskCtx hatchet.Context, _ fxInput) (fxOutput, error) {
			out, err := refreshRates(taskCtx, httpClient, endpoint, base, symbols, ingestURL, ingestKey)
			if err != nil {
				// Best-effort: a fetch/ingest failure leaves the previously-cached
				// rows in place (the display layer's fallback). The task surfaces
				// the error to Hatchet so retries / alerting work.
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
		out, err := refreshRates(ctx, httpClient, endpoint, base, symbols, ingestURL, ingestKey)
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

// refreshRates is the shared fetch+submit path used both by the cron task body
// and the bootstrap-on-startup goroutine. It fetches from Frankfurter and POSTs
// the snapshot to the backend ingestion endpoint — no direct DB access.
func refreshRates(
	ctx context.Context,
	httpClient *http.Client,
	endpoint, base string,
	symbols []string,
	ingestURL, ingestKey string,
) (fxOutput, error) {
	snap, err := exchangerates.Fetch(ctx, httpClient, endpoint, base, symbols)
	if err != nil {
		return fxOutput{Base: base}, err
	}
	written, err := exchangerates.Submit(ctx, httpClient, ingestURL, ingestKey, base, snap)
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
