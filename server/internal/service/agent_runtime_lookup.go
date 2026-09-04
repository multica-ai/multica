package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RuntimeLookup is the only way production code reads a single agent_runtime
// row by id (MUL-6884).
//
// The query itself is a primary-key point read and has never been the problem.
// What is missing is attribution: daemon heartbeats, browser polling loops, and
// a dozen readiness gates all issue the same statement, so pg_stat_statements
// can report that agent_runtime is one of the busiest reads in the system while
// saying nothing about which product behaviour is driving it. Routing every
// read through one type, carrying the source with it, is what makes that
// question answerable before anyone starts changing heartbeat intervals.
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
