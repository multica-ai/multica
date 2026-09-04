package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RuntimeLookup is the only way production code reads agent_runtime rows by id
// (MUL-6884) — one row via Get, or many in one query via GetMany.
//
// Point-read callers share one SQL fingerprint, while GetMany uses a separate
// batch-query fingerprint. pg_stat_statements can show that either query is
// busy, but not whether daemon heartbeats, browser polling loops, or readiness
// gates are driving it. Routing both query shapes through one type, carrying
// the source with each lookup, makes that question answerable before anyone
// starts changing heartbeat intervals.
//
// Source is a closed enum from the metrics package (obsmetrics.RuntimeLookupSource*).
// Metrics may be nil — tests and self-hosted deployments without the metrics
// listener run without it, and the counter is best-effort by construction.
type RuntimeLookup struct {
	// Queries is the connection the read runs on. Callers inside a
	// transaction must pass the transaction's queries, not the pool's, so the
	// read sees the same snapshot as the rest of the transaction.
	Queries *db.Queries
	Metrics *obsmetrics.BusinessMetrics
	Source  string
}

// Get reads one agent_runtime row and counts it against the lookup's source.
//
// The row and error are returned untouched: callers already distinguish
// pgx.ErrNoRows (the runtime was deleted — the daemon reads that as "drop your
// local registration") from a transient database failure, and collapsing the
// two here would turn a hiccup into a spurious self-heal.
func (l RuntimeLookup) Get(ctx context.Context, id pgtype.UUID) (db.AgentRuntime, error) {
	rt, err := l.Queries.GetAgentRuntime(ctx, id)
	l.Metrics.RecordAgentRuntimeLookup(l.Source, runtimeLookupResult(err))
	return rt, err
}

// GetMany reads the requested agent_runtime rows in one query and keeps the
// per-source attribution GetAgentRuntime callers get, without issuing N point
// reads (MUL-6788). It returns the rows keyed by their canonical UUID string
// so a differently-cased request id still resolves.
//
// Metric accounting mirrors N individual Get calls: a batch read error counts
// one "error" per requested id (the whole lookup failed for each), and on
// success each id is counted "ok" when its row came back and "not_found" when
// it did not — so multica_agent_runtime_lookup_total keeps the same shape it
// had before batching. The read error is returned untouched so callers can fail
// closed instead of treating a hiccup as "every runtime is gone".
func (l RuntimeLookup) GetMany(ctx context.Context, ids []pgtype.UUID) (map[string]db.AgentRuntime, error) {
	rows, err := l.Queries.GetAgentRuntimes(ctx, ids)
	if err != nil {
		for range ids {
			l.Metrics.RecordAgentRuntimeLookup(l.Source, obsmetrics.RuntimeLookupResultError)
		}
		return nil, err
	}
	byID := make(map[string]db.AgentRuntime, len(rows))
	for _, rt := range rows {
		byID[util.UUIDToString(rt.ID)] = rt
	}
	for _, id := range ids {
		if _, ok := byID[util.UUIDToString(id)]; ok {
			l.Metrics.RecordAgentRuntimeLookup(l.Source, obsmetrics.RuntimeLookupResultOK)
		} else {
			l.Metrics.RecordAgentRuntimeLookup(l.Source, obsmetrics.RuntimeLookupResultNotFound)
		}
	}
	return byID, nil
}

func runtimeLookupResult(err error) string {
	switch {
	case err == nil:
		return obsmetrics.RuntimeLookupResultOK
	case errors.Is(err, pgx.ErrNoRows):
		return obsmetrics.RuntimeLookupResultNotFound
	default:
		return obsmetrics.RuntimeLookupResultError
	}
}

// runtimeLookup returns the issue-sourced lookup, bound to the caller's
// connection so a read inside the create transaction stays inside it.
func (s *IssueService) runtimeLookup(q *db.Queries) RuntimeLookup {
	return RuntimeLookup{Queries: q, Metrics: s.Metrics, Source: obsmetrics.RuntimeLookupSourceIssue}
}

// runtimeLookup returns the task-sourced lookup for analytics context and the
// usage provider backfill.
func (s *TaskService) runtimeLookup() RuntimeLookup {
	return RuntimeLookup{Queries: s.Queries, Metrics: s.Metrics, Source: obsmetrics.RuntimeLookupSourceTask}
}

// runtimeLookup returns the autopilot-sourced lookup. AutopilotService holds no
// metrics collector of its own; it shares the task service's, which the router
// wires from the same registry.
func (s *AutopilotService) runtimeLookup() RuntimeLookup {
	var m *obsmetrics.BusinessMetrics
	if s.TaskSvc != nil {
		m = s.TaskSvc.Metrics
	}
	return RuntimeLookup{Queries: s.Queries, Metrics: m, Source: obsmetrics.RuntimeLookupSourceAutopilot}
}
