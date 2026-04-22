package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// dbStatsInterval is how often the pool stats are sampled and logged.
	// 15s lines up with the daemon heartbeat cadence so it's easy to
	// correlate with traffic patterns in the prod logs.
	dbStatsInterval = 15 * time.Second

	// defaultMaxConns / defaultMinConns are the per-pod pgxpool sizing
	// defaults. They replace pgx's built-in default of max(4, NumCPU),
	// which is far too small for our daemon-poll traffic pattern (~3800
	// acquires/s observed in prod) and was the root cause of the 3s+
	// /tasks/claim tail latency.
	//
	// The numbers follow the conventional "small pool, lots of waiters"
	// guidance for Postgres (HikariCP / PG community formula
	// `(core_count * 2) + effective_spindle_count`): 25 leaves headroom
	// for bursts and the occasional long-running query while staying well
	// below typical managed-Postgres `max_connections` ceilings when
	// multiplied across pods. MinConns=5 keeps a warm baseline so cold
	// pods don't pay handshake cost on first traffic.
	//
	// Both values are overridable via DATABASE_MAX_CONNS / DATABASE_MIN_CONNS.
	defaultMaxConns int32 = 25
	defaultMinConns int32 = 5
)

// newDBPool builds a pgxpool with sane production defaults and env overrides.
//
// pgxpool.New(ctx, url) — used previously — silently picks MaxConns =
// max(4, NumCPU). On our prod pods (small CPU request) that resolved to 4,
// which got fully saturated by the daemon claim/heartbeat traffic and showed
// up as ~900ms acquire waits on every query.
//
// Configuration precedence (highest first):
//  1. DATABASE_MAX_CONNS / DATABASE_MIN_CONNS env vars
//  2. pool_max_conns / pool_min_conns query params on DATABASE_URL
//     (honored natively by pgxpool.ParseConfig)
//  3. The defaults defined here (defaultMaxConns / defaultMinConns)
//
// pgx's own built-in default (max(4, NumCPU)) is intentionally NOT used as a
// fallback — it is the value that caused the prod incident.
func newDBPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	urlParams := poolParamsFromURL(dbURL)

	// Compute the non-env fallback first: honor URL pool_* params if the
	// operator set them, otherwise use our code default. This fallback is
	// also what an *invalid* env value falls back to — never pgx's built-in
	// default of 4/0, which is the value that caused the prod incident.
	maxFallback := defaultMaxConns
	if urlParams["pool_max_conns"] {
		maxFallback = cfg.MaxConns
	}
	cfg.MaxConns = envInt32("DATABASE_MAX_CONNS", maxFallback)

	minFallback := defaultMinConns
	if urlParams["pool_min_conns"] {
		minFallback = cfg.MinConns
	}
	cfg.MinConns = envInt32("DATABASE_MIN_CONNS", minFallback)

	if cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = cfg.MaxConns
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}

// poolParamsFromURL returns the set of pool_* query params present on the
// database URL. Used to detect whether the operator already tuned the pool
// via the connection string, so env-less upgrades don't silently override
// existing configuration.
func poolParamsFromURL(dbURL string) map[string]bool {
	out := map[string]bool{}
	u, err := url.Parse(dbURL)
	if err != nil {
		return out
	}
	for k := range u.Query() {
		out[k] = true
	}
	return out
}

// envInt32 reads an int32 from the named env var. Empty / invalid values fall
// back to def and emit a warn so misconfiguration is visible in startup logs.
func envInt32(name string, def int32) int32 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default",
			"name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return int32(v)
}

// logPoolConfig prints the effective pgxpool configuration once at startup.
// Surfacing this is critical because pgxpool defaults are surprisingly small
// (MaxConns = max(4, NumCPU)) — without seeing the value in the log it's
// easy to mistake pool exhaustion for "the database is slow".
func logPoolConfig(pool *pgxpool.Pool) {
	cfg := pool.Config()
	slog.Info("db pool config",
		"max_conns", cfg.MaxConns,
		"min_conns", cfg.MinConns,
		"max_conn_lifetime", cfg.MaxConnLifetime.String(),
		"max_conn_idle_time", cfg.MaxConnIdleTime.String(),
		"health_check_period", cfg.HealthCheckPeriod.String(),
	)
}

// runDBStatsLogger samples pool.Stat() periodically. It always emits an INFO
// line so operators can see baseline pressure, and emits a WARN whenever the
// EmptyAcquireCount delta is positive — that's the direct symptom of pool
// exhaustion (a request had to wait because no idle conn was available) and
// the smoking gun we're looking for to confirm the slow /tasks/claim
// hypothesis.
func runDBStatsLogger(ctx context.Context, pool *pgxpool.Pool) {
	ticker := time.NewTicker(dbStatsInterval)
	defer ticker.Stop()

	var (
		lastEmpty      int64
		lastAcquire    int64
		lastAcquireDur time.Duration
		lastCanceled   int64
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		s := pool.Stat()
		emptyDelta := s.EmptyAcquireCount() - lastEmpty
		acquireDelta := s.AcquireCount() - lastAcquire
		acquireDurDelta := s.AcquireDuration() - lastAcquireDur
		canceledDelta := s.CanceledAcquireCount() - lastCanceled

		// Average wait per acquire over the last sampling window. Useful
		// because cumulative AcquireDuration alone hides whether the
		// situation is improving or worsening.
		var avgAcquireMs int64
		if acquireDelta > 0 {
			avgAcquireMs = (acquireDurDelta).Milliseconds() / acquireDelta
		}

		fields := []any{
			"max_conns", s.MaxConns(),
			"total_conns", s.TotalConns(),
			"acquired_conns", s.AcquiredConns(),
			"idle_conns", s.IdleConns(),
			"constructing_conns", s.ConstructingConns(),
			"acquire_count_delta", acquireDelta,
			"empty_acquire_delta", emptyDelta,
			"canceled_acquire_delta", canceledDelta,
			"avg_acquire_ms", avgAcquireMs,
		}

		if emptyDelta > 0 || canceledDelta > 0 {
			slog.Warn("db pool pressure", fields...)
		} else {
			slog.Info("db pool stats", fields...)
		}

		lastEmpty = s.EmptyAcquireCount()
		lastAcquire = s.AcquireCount()
		lastAcquireDur = s.AcquireDuration()
		lastCanceled = s.CanceledAcquireCount()
	}
}
