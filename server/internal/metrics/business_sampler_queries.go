package metrics

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SQL bodies for BusinessSamplerCollector. Kept in a separate file so the
// orchestration logic in business_sampler.go stays readable. Every query:
//   - is parameterised; never concatenates user / label input
//   - uses an explicit `LIMIT 100` belt-and-braces (the result set is
//     already bounded by GROUP BY, but the LIMIT is the spec-mandated
//     last-line-of-defense against an accidental high-cardinality output)
//   - normalises raw column values (source / runtime_mode / provider)
//     through the existing BusinessMetrics whitelists so the sampler
//     cannot widen the metric label space.

// queryActiveUsers fills snap.activeUsers with a count of distinct user_ids
// that have either sent a chat message or had an agent task created for one
// of their issues inside the short rolling window. We deliberately skip
// Postgres `now() - interval '$1'` substitution and instead pass a typed
// interval as a bind parameter so the caller cannot inject SQL via the
// window string.
//
// Note on cardinality: there is intentionally NO `LIMIT` on the inner
// distinct subquery — capping it would silently truncate the COUNT to 100
// and report a wrong active-user value. The result row count is already
// pinned to 1 (the COUNT scalar). Cardinality of the *output metric* is
// bounded by the fixed `samplerWindows` allow-list, not by the SQL shape.
func (c *BusinessSamplerCollector) queryActiveUsers(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT count(DISTINCT user_id) FROM (
  SELECT cs.creator_id AS user_id
  FROM chat_session cs
  WHERE EXISTS (
    SELECT 1 FROM chat_message cm
    WHERE cm.chat_session_id = cs.id
      AND cm.created_at > now() - $1::interval
  )
  UNION ALL
  SELECT i.creator_id AS user_id
  FROM issue i
  WHERE i.creator_type = 'member'
    AND EXISTS (
      SELECT 1 FROM agent_task_queue atq
      WHERE atq.issue_id = i.id
        AND atq.created_at > now() - $1::interval
    )
) u
`
	for _, w := range samplerWindows {
		var n int64
		if err := tx.QueryRow(ctx, stmt, w.d).Scan(&n); err != nil {
			return fmt.Errorf("active_users window=%s: %w", w.label, err)
		}
		snap.activeUsers[w.label] = float64(n)
	}
	return nil
}

// queryActiveWorkspaces counts distinct workspaces with chat or task
// activity in the short window. Mirrors queryActiveUsers and intentionally
// has no inner LIMIT for the same reason: a LIMIT inside the distinct
// subquery would truncate the COUNT and report a wrong value.
func (c *BusinessSamplerCollector) queryActiveWorkspaces(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT count(DISTINCT workspace_id) FROM (
  SELECT cs.workspace_id
  FROM chat_session cs
  WHERE EXISTS (
    SELECT 1 FROM chat_message cm
    WHERE cm.chat_session_id = cs.id
      AND cm.created_at > now() - $1::interval
  )
  UNION ALL
  SELECT i.workspace_id
  FROM issue i
  WHERE EXISTS (
    SELECT 1 FROM agent_task_queue atq
    WHERE atq.issue_id = i.id
      AND atq.created_at > now() - $1::interval
  )
) w
`
	for _, w := range samplerWindows {
		var n int64
		if err := tx.QueryRow(ctx, stmt, w.d).Scan(&n); err != nil {
			return fmt.Errorf("active_workspaces window=%s: %w", w.label, err)
		}
		snap.activeWorkspaces[w.label] = float64(n)
	}
	return nil
}

// queryTaskQueued counts agent_task_queue rows in `queued` status, grouped
// by inferred source. The CASE expression here mirrors the source
// derivation in service/task.go so sampler and event-time metrics agree.
func (c *BusinessSamplerCollector) queryTaskQueued(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT
  CASE
    WHEN chat_session_id IS NOT NULL THEN 'chat'
    WHEN autopilot_run_id IS NOT NULL THEN 'autopilot'
    WHEN issue_id IS NOT NULL THEN 'issue'
    ELSE 'other'
  END AS source,
  count(*) AS n
FROM agent_task_queue
WHERE status = 'queued'
GROUP BY 1
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("task_queued: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawSource string
		var n int64
		if err := rows.Scan(&rawSource, &n); err != nil {
			return fmt.Errorf("task_queued scan: %w", err)
		}
		snap.taskQueued[NormalizeTaskSource(rawSource)] += float64(n)
	}
	return rows.Err()
}

// queryTaskRunning counts agent_task_queue rows in `dispatched` or
// `running` status, grouped by source × runtime_mode. runtime_mode comes
// from the joined agent_runtime row; values outside the allow-list collapse
// to "unknown".
func (c *BusinessSamplerCollector) queryTaskRunning(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	// Keep dispatched and running in separate UNION ALL branches so
	// Postgres can match the existing dispatched partial index and the
	// running-only partial index from migration 114 independently.
	const stmt = `
WITH in_flight AS (
  SELECT chat_session_id, autopilot_run_id, issue_id, runtime_id
  FROM agent_task_queue
  WHERE status = 'dispatched'
  UNION ALL
  SELECT chat_session_id, autopilot_run_id, issue_id, runtime_id
  FROM agent_task_queue
  WHERE status = 'running'
)
SELECT
  CASE
    WHEN atq.chat_session_id IS NOT NULL THEN 'chat'
    WHEN atq.autopilot_run_id IS NOT NULL THEN 'autopilot'
    WHEN atq.issue_id IS NOT NULL THEN 'issue'
    ELSE 'other'
  END AS source,
  COALESCE(ar.runtime_mode, 'unknown') AS runtime_mode,
  count(*) AS n
FROM in_flight atq
LEFT JOIN agent_runtime ar ON ar.id = atq.runtime_id
GROUP BY 1, 2
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("task_running: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawSource, rawMode string
		var n int64
		if err := rows.Scan(&rawSource, &rawMode, &n); err != nil {
			return fmt.Errorf("task_running scan: %w", err)
		}
		key := taskRunningKey{
			source:      NormalizeTaskSource(rawSource),
			runtimeMode: NormalizeRuntimeMode(rawMode),
		}
		snap.taskRunning[key] += float64(n)
	}
	return rows.Err()
}

// queryTaskStuck counts `running` agent_task_queue rows whose started_at
// crosses the stuck threshold, grouped by inferred source.
func (c *BusinessSamplerCollector) queryTaskStuck(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	stmt := `
SELECT
  CASE
    WHEN chat_session_id IS NOT NULL THEN 'chat'
    WHEN autopilot_run_id IS NOT NULL THEN 'autopilot'
    WHEN issue_id IS NOT NULL THEN 'issue'
    ELSE 'other'
  END AS source,
  count(*) AS n
FROM agent_task_queue
WHERE status = 'running'
  AND started_at IS NOT NULL
  AND started_at < now() - interval '` + stuckRunningInterval + `'
GROUP BY 1
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("task_stuck: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawSource string
		var n int64
		if err := rows.Scan(&rawSource, &n); err != nil {
			return fmt.Errorf("task_stuck scan: %w", err)
		}
		snap.taskStuck[NormalizeTaskSource(rawSource)] += float64(n)
	}
	return rows.Err()
}

// queryRuntimeOnline counts agent_runtime rows whose last_seen_at is within
// the online window, grouped by runtime_mode × provider. Both labels go
// through the BusinessMetrics whitelists so a misbehaving runtime registering
// itself with an exotic provider name cannot inflate the series space.
func (c *BusinessSamplerCollector) queryRuntimeOnline(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT runtime_mode, provider, count(*) AS n
FROM agent_runtime
WHERE last_seen_at IS NOT NULL
  AND last_seen_at > now() - ($1::int * interval '1 second')
GROUP BY 1, 2
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt, runtimeOnlineWindowSeconds)
	if err != nil {
		return fmt.Errorf("runtime_online: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawMode, rawProvider string
		var n int64
		if err := rows.Scan(&rawMode, &rawProvider, &n); err != nil {
			return fmt.Errorf("runtime_online scan: %w", err)
		}
		key := runtimeOnlineKey{
			runtimeMode: NormalizeRuntimeMode(rawMode),
			provider:    NormalizeRuntimeProvider(rawProvider),
		}
		snap.runtimeOnline[key] += float64(n)
	}
	return rows.Err()
}

// queryRuntimeHeartbeatAge fills the heartbeat-age histogram per runtime_mode.
// We pull at most 100 rows and bucketise in Go because Postgres does not
// return histogram-shaped output. Rows older than 15 minutes are dropped —
// they are clearly offline and would just smear the tail of the histogram.
func (c *BusinessSamplerCollector) queryRuntimeHeartbeatAge(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `
SELECT runtime_mode, EXTRACT(EPOCH FROM (now() - last_seen_at))::float8 AS age
FROM agent_runtime
WHERE last_seen_at IS NOT NULL
  AND last_seen_at > now() - interval '15 minutes'
ORDER BY last_seen_at DESC
LIMIT 100
`
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return fmt.Errorf("runtime_heartbeat_age: %w", err)
	}
	defer rows.Close()

	perMode := map[string][]float64{}
	for rows.Next() {
		var rawMode string
		var age float64
		if err := rows.Scan(&rawMode, &age); err != nil {
			return fmt.Errorf("runtime_heartbeat_age scan: %w", err)
		}
		if age < 0 {
			age = 0
		}
		mode := NormalizeRuntimeMode(rawMode)
		perMode[mode] = append(perMode[mode], age)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for mode, ages := range perMode {
		hist := samplerHistogram{
			buckets: make(map[float64]uint64, len(heartbeatAgeBuckets)),
		}
		for _, b := range heartbeatAgeBuckets {
			hist.buckets[b] = 0
		}
		for _, age := range ages {
			hist.count++
			hist.sum += age
			for _, b := range heartbeatAgeBuckets {
				if age <= b {
					hist.buckets[b]++
				}
			}
		}
		snap.heartbeatAge[mode] = hist
	}
	return nil
}

// queryWorkspaceTotal exposes a lifetime workspace count. Single integer,
// no labels — useful for sizing dashboards and capacity planning.
func (c *BusinessSamplerCollector) queryWorkspaceTotal(
	ctx context.Context, tx pgx.Tx, snap *samplerSnapshot,
) error {
	const stmt = `SELECT count(*) FROM workspace LIMIT 100`
	var n int64
	if err := tx.QueryRow(ctx, stmt).Scan(&n); err != nil {
		return fmt.Errorf("workspace_total: %w", err)
	}
	snap.workspaceTotal = float64(n)
	snap.workspaceTotalKnown = true
	return nil
}
