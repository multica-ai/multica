// CEREBRO-PATCH(cerebro-fetch-exchange-rates): FIR-40 — net-new cerebro daily
// rate-fetch job (scheduled by runs.firtal.com); not a modification of any
// upstream file. Logic lives in server/internal/cerebro/exchangerates.
//
// Command cerebro_fetch_exchange_rates fetches USD->{DKK,EUR,...} reference
// rates from the Frankfurter ECB API and upserts them into
// cerebro_exchange_rates. FIR-40 — scheduled daily by runs.firtal.com. The
// display layer only ever reads the cached row, never this command.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/exchangerates"
	"github.com/multica-ai/multica/server/internal/logger"
)

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("fetch exchange rates failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		base     = flag.String("base", "USD", "base currency cost is stored in")
		symbols  = flag.String("symbols", "DKK,EUR", "comma-separated target currencies")
		endpoint = flag.String("endpoint", exchangerates.DefaultEndpoint, "rates API endpoint")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	targets := splitSymbols(*symbols)
	client := &http.Client{Timeout: 30 * time.Second}

	snap, err := exchangerates.Fetch(ctx, client, *endpoint, *base, targets)
	if err != nil {
		return err
	}

	queries := cerebrodb.New(pool)
	written, err := exchangerates.Store(ctx, queries, *base, snap)
	if err != nil {
		return err
	}

	slog.Info("exchange rates updated", "base", *base, "date", snap.Date, "written", written)
	return nil
}

func splitSymbols(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
