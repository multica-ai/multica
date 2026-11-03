package scheduler

import (
	"context"
	"time"
)

// The Librarian's routines run as scheduler jobs: draft triage daily, freshness
// weekly, the taxonomy audit monthly, the retrieval eval weekly, and the
// taxonomy backfill hourly (it does nothing once every workspace is current).
// Each job runs its routine for every workspace whose knowledge is ready.

// Scheduler job names, stable across releases.
const (
	JobNameKnowledgeTriage           = "knowledge_triage"
	JobNameKnowledgeFreshness        = "knowledge_freshness"
	JobNameKnowledgeAudit            = "knowledge_audit"
	JobNameKnowledgeEval             = "knowledge_eval"
	JobNameKnowledgeTaxonomyBackfill = "knowledge_taxonomy_backfill"
	JobNameKnowledgeRepoDocs         = "knowledge_repo_docs"
)

// KnowledgeLibrarianRunner runs one Librarian routine for every workspace with
// ready knowledge and returns how many workspaces completed. The routine names
// are the service's LibrarianJob constants.
type KnowledgeLibrarianRunner interface {
	RunLibrarian(ctx context.Context, job string, now time.Time) (int, error)
}

// KnowledgeLibrarianJob returns the job that runs one routine on a cadence. A
// failed pass is retried by the scheduler; the routines are idempotent, so a
// retry repeats the report and nothing else.
func KnowledgeLibrarianJob(runner KnowledgeLibrarianRunner, name, routine string, cadence time.Duration) JobSpec {
	return JobSpec{
		Name:              name,
		Cadence:           cadence,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     cadence,
		RunTimeout:        30 * time.Minute,
		StaleTimeout:      45 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := runner.RunLibrarian(ctx, routine, in.PlanTime)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}

// KnowledgeRepoDocsRunner runs the repo-docs sync for every workspace with ready
// knowledge. A workspace with the sync off does nothing.
type KnowledgeRepoDocsRunner interface {
	RunRepoDocsSync(ctx context.Context) (int, error)
}

// KnowledgeRepoDocsJob returns the daily repo-docs sync (plan 10.12).
func KnowledgeRepoDocsJob(runner KnowledgeRepoDocsRunner) JobSpec {
	return JobSpec{
		Name:              JobNameKnowledgeRepoDocs,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        30 * time.Minute,
		StaleTimeout:      45 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := runner.RunRepoDocsSync(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
