package main

// CEREBRO-PATCH(query-census): FIR-3781 diagnostic query census.
//
// The 25 July incident drove database acquires from ~2k to ~140k per 15s
// window while HTTP request count and latency were unchanged, the pool kept 24
// idle connections, and not one log line named the source. Counting acquires
// tells you a flood exists; it cannot tell you what is flooding.
//
// This census sits at the pgx driver seam and groups by statement, so whatever
// issues the queries is named regardless of how deep in the call chain it sits
// — which is exactly what reading the diff failed to find.
//
// It is OFF unless CEREBRO_QUERY_CENSUS=1. When on it costs one map lookup
// under a mutex per query and logs the top statements once per interval.

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	censusInterval = 15 * time.Second
	censusTopN     = 12
	censusSQLChars = 110
)

type queryCensus struct {
	mu     sync.Mutex
	counts map[string]int64
	total  int64
}

func newQueryCensus() *queryCensus {
	return &queryCensus{counts: make(map[string]int64, 512)}
}

// TraceQueryStart records the statement. Normalisation is deliberately crude —
// a prefix of the SQL with whitespace collapsed. The goal is to identify the
// offender, not to build a query-analytics product.
func (c *queryCensus) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	key := strings.Join(strings.Fields(data.SQL), " ")
	if len(key) > censusSQLChars {
		key = key[:censusSQLChars]
	}
	c.mu.Lock()
	c.counts[key]++
	c.total++
	c.mu.Unlock()
	return ctx
}

func (c *queryCensus) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// report drains the window and logs the busiest statements. Draining rather
// than accumulating means each line describes one interval, so a flood that
// starts mid-run stands out instead of being averaged away.
func (c *queryCensus) report() {
	c.mu.Lock()
	counts := c.counts
	total := c.total
	c.counts = make(map[string]int64, len(counts))
	c.total = 0
	c.mu.Unlock()

	if total == 0 {
		return
	}
	type row struct {
		sql string
		n   int64
	}
	rows := make([]row, 0, len(counts))
	for sql, n := range counts {
		rows = append(rows, row{sql: sql, n: n})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })

	slog.Info("query census window",
		"total_queries", total,
		"per_second", total/int64(censusInterval/time.Second),
		"distinct_statements", len(rows),
	)
	for i, r := range rows {
		if i >= censusTopN {
			break
		}
		slog.Info("query census entry", "rank", i+1, "count", r.n, "sql", r.sql)
	}
}

func (c *queryCensus) run(ctx context.Context) {
	ticker := time.NewTicker(censusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.report()
		}
	}
}

// queryCensusEnabled reports whether the census should be attached.
func queryCensusEnabled() bool {
	return os.Getenv("CEREBRO_QUERY_CENSUS") == "1"
}

// activeQueryCensus is the process-wide census the pool tracer points at. It is
// allocated unconditionally (a few hundred bytes) so the wiring in newDBPool
// stays a single branch; nothing writes to it unless the tracer is attached.
var activeQueryCensus = newQueryCensus()
