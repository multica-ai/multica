package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The job status registry (spec 4.9). The scheduler writes each job's last success
// and last failure to the job_status table when its plan finishes (one hook in the
// scheduler, so no job reports by itself). Stuck runs come from the scheduler's own
// execution log (sys_cron_executions), which also backs the fallback for a job with
// no status row yet. A job is stuck when a run is still RUNNING past its lease, or
// when it has not succeeded for more than StuckCadences cadences.

// StuckCadences is how many cadences a job may go without a success before it
// is stuck.
const StuckCadences = 3

// ScheduledJob is a job the registry watches. Names are the scheduler's JobName
// constants (internal/scheduler). The scheduler imports this package, so the
// literals are kept here; a job added to the scheduler gets a row here too.
type ScheduledJob struct {
	Name    string
	Label   string
	Cadence time.Duration
}

// WatchedJobs are the scheduled jobs the registry reports, in display order.
var WatchedJobs = []ScheduledJob{
	{Name: "squad_attention", Label: "Attention recompute", Cadence: 24 * time.Hour},
	{Name: "brief_catchup", Label: "Brief catch-up", Cadence: time.Hour},
	{Name: "brief_push_dispatch", Label: "Brief delivery retries", Cadence: time.Minute},
	{Name: "retro_trigger", Label: "Retrospective triggers", Cadence: time.Hour},
	{Name: "knowledge_freshness", Label: "Knowledge freshness", Cadence: 7 * 24 * time.Hour},
	{Name: "knowledge_eval", Label: "Knowledge eval", Cadence: 7 * 24 * time.Hour},
	{Name: "proposal_expiry", Label: "Proposal expiry", Cadence: time.Hour},
	{Name: "metric_nightly", Label: "Metric readings and goal confidence", Cadence: 24 * time.Hour},
	{Name: "planning_cycle", Label: "Planning cycle", Cadence: 24 * time.Hour},
}

// JobStatus is one watched job's state.
type JobStatus struct {
	Name    string        `json:"name"`
	Label   string        `json:"label"`
	Cadence time.Duration `json:"-"`
	// CadenceSeconds is the cadence, for the reader.
	CadenceSeconds int64 `json:"cadence_seconds"`
	// LastSuccessAt is nil until the job has succeeded in the log's window.
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastFailedAt  *time.Time `json:"last_failed_at"`
	// LagSeconds is the time since the last success, or -1 when there is none.
	LagSeconds int64 `json:"lag_seconds"`
	// Stuck is set for a job that holds a lease past its stale time, or that has
	// gone StuckCadences cadences without a success.
	Stuck bool `json:"stuck"`
	// StuckReason names why, in words for the risk line.
	StuckReason string `json:"stuck_reason"`
}

// jobStatusWindow bounds how far back the registry reads the execution log.
const jobStatusWindow = 30 * 24 * time.Hour

// ReadJobStatuses reads the watched jobs' state from the scheduler's log.
func ReadJobStatuses(ctx context.Context, q *db.Queries, now time.Time) ([]JobStatus, error) {
	names := make([]string, 0, len(WatchedJobs))
	for _, j := range WatchedJobs {
		names = append(names, j.Name)
	}
	rows, err := q.ListSchedulerJobRuns(ctx, db.ListSchedulerJobRunsParams{
		JobNames: names,
		Now:      pgtype.Timestamptz{Time: now, Valid: true},
		Since:    pgtype.Timestamptz{Time: now.Add(-jobStatusWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("read scheduler log: %w", err)
	}
	byName := make(map[string]db.ListSchedulerJobRunsRow, len(rows))
	for _, r := range rows {
		byName[r.JobName] = r
	}
	// The job status table holds each job's last success and failure, written when
	// the job's plan finishes. A job with no row yet (none has run since the table
	// was added) falls back to the execution log.
	statusRows, err := q.ListJobStatuses(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("read job status: %w", err)
	}
	statusByName := make(map[string]db.ListJobStatusesRow, len(statusRows))
	for _, r := range statusRows {
		statusByName[r.JobName] = r
	}
	out := make([]JobStatus, 0, len(WatchedJobs))
	for _, j := range WatchedJobs {
		st := JobStatus{
			Name: j.Name, Label: j.Label, Cadence: j.Cadence,
			CadenceSeconds: int64(j.Cadence / time.Second), LagSeconds: -1,
		}
		r, inLog := byName[j.Name]
		if status, ok := statusByName[j.Name]; ok {
			if status.LastSuccessAt.Valid {
				t := status.LastSuccessAt.Time
				st.LastSuccessAt = &t
				st.LagSeconds = int64(now.Sub(t) / time.Second)
			}
			if status.LastFailedAt.Valid {
				t := status.LastFailedAt.Time
				st.LastFailedAt = &t
			}
		} else if inLog {
			if r.LastSuccessAt.Valid {
				t := r.LastSuccessAt.Time
				st.LastSuccessAt = &t
				st.LagSeconds = int64(now.Sub(t) / time.Second)
			}
			if r.LastFailedAt.Valid {
				t := r.LastFailedAt.Time
				st.LastFailedAt = &t
			}
		}
		if inLog {
			st.Stuck, st.StuckReason = stuckReason(st, int(r.Stuck), now)
		} else {
			st.Stuck, st.StuckReason = stuckReason(st, 0, now)
		}
		out = append(out, st)
	}
	return out, nil
}

// stuckReason decides whether a job is stuck. A run holding its lease past the
// stale time is stuck whatever the success history. Otherwise a job with a
// success older than StuckCadences cadences is stuck. A job that never
// succeeded is stuck only once it has failed, so a fresh install does not report
// jobs that have not had their first run yet.
func stuckReason(st JobStatus, staleRuns int, now time.Time) (bool, string) {
	if staleRuns > 0 {
		return true, "a run is holding its lease past the stale time"
	}
	limit := time.Duration(StuckCadences) * st.Cadence
	switch {
	case st.LastSuccessAt != nil && now.Sub(*st.LastSuccessAt) > limit:
		return true, fmt.Sprintf("no success for more than %d cadences", StuckCadences)
	case st.LastSuccessAt == nil && st.LastFailedAt != nil:
		return true, "it has failed and has never succeeded"
	}
	return false, ""
}

// StuckJobs returns the statuses that are stuck, in registry order.
func StuckJobs(statuses []JobStatus) []JobStatus {
	var out []JobStatus
	for _, s := range statuses {
		if s.Stuck {
			out = append(out, s)
		}
	}
	return out
}
