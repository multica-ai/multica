package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// System health (spec 4.9): what the boss needs to know about the machinery the
// company runs on. Each reading is cheap, and a reading that cannot be taken is
// reported as such rather than as healthy.

// DeliveryFailureWindow is how far back a failed brief delivery counts.
const DeliveryFailureWindow = 24 * time.Hour

// BudgetCacheStaleAfter is how old the budget gate's cached counters may be
// before health calls them a risk. The counters refresh every minute.
const BudgetCacheStaleAfter = 10 * time.Minute

// BudgetGateCacheReader reads when the budget gate's counters were last
// refreshed. The budget step provides it. A zero time means the gate has no
// cache yet; a nil reader means the gate is not reported.
type BudgetGateCacheReader interface {
	CacheRefreshedAt(ctx context.Context, workspaceID pgtype.UUID) (time.Time, error)
}

// KnowledgeHealthReader reports the knowledge sidecar's state for a workspace.
// KnowledgeService.Status satisfies it.
type KnowledgeHealthReader interface {
	Status(ctx context.Context, workspaceID pgtype.UUID) (KnowledgeStatus, error)
}

// HealthReport is what GET /api/governance/health returns.
type HealthReport struct {
	CheckedAt time.Time   `json:"checked_at"`
	Jobs      []JobStatus `json:"jobs"`
	// DeliveryFailures is the number of brief deliveries to chat apps that failed
	// in the last day.
	DeliveryFailures int `json:"delivery_failures"`
	// Knowledge is the state of WeKnora, from its health check. Offered is false
	// when this deployment has no knowledge base at all.
	Knowledge  KnowledgeHealth  `json:"knowledge"`
	BudgetGate BudgetGateHealth `json:"budget_gate"`
	// Risks are the lines the digest and the Controls tab name, in plain words.
	Risks []string `json:"risks"`
}

// KnowledgeHealth is WeKnora's reading.
type KnowledgeHealth struct {
	Offered bool   `json:"offered"`
	State   string `json:"state"`
	Reason  string `json:"reason"`
}

// BudgetGateHealth is the budget gate's cache age. Reported is false until the
// budget step provides the reader.
type BudgetGateHealth struct {
	Reported        bool   `json:"reported"`
	CacheAgeSeconds *int64 `json:"cache_age_seconds"`
}

// HealthService builds the report. Knowledge and BudgetGate may be nil.
type HealthService struct {
	Queries    *db.Queries
	Knowledge  KnowledgeHealthReader
	BudgetGate BudgetGateCacheReader
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (s *HealthService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Report reads every health line for the workspace. A reader that fails is an
// error, not a healthy line: the caller shows that the reading is missing.
func (s *HealthService) Report(ctx context.Context, workspaceID pgtype.UUID) (HealthReport, error) {
	now := s.now()
	out := HealthReport{CheckedAt: now.UTC(), Risks: []string{}}

	jobs, err := ReadJobStatuses(ctx, s.Queries, now)
	if err != nil {
		return HealthReport{}, err
	}
	out.Jobs = jobs
	for _, st := range StuckJobs(jobs) {
		out.Risks = append(out.Risks, fmt.Sprintf("Scheduled job %s is stuck: %s.", st.Label, st.StuckReason))
	}

	failed, err := s.Queries.CountFailedBriefDeliveries(ctx, db.CountFailedBriefDeliveriesParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: now.Add(-DeliveryFailureWindow), Valid: true},
	})
	if err != nil {
		return HealthReport{}, fmt.Errorf("delivery failures: %w", err)
	}
	out.DeliveryFailures = int(failed)
	if failed > 0 {
		out.Risks = append(out.Risks, fmt.Sprintf("%d brief deliveries to chat apps failed in the last day and are being retried.", failed))
	}

	if s.Knowledge != nil {
		st, err := s.Knowledge.Status(ctx, workspaceID)
		if err != nil {
			return HealthReport{}, fmt.Errorf("knowledge status: %w", err)
		}
		out.Knowledge = KnowledgeHealth{Offered: st.Offered, State: st.State, Reason: st.Reason}
		if st.Offered && st.State == KnowledgeStateOffline {
			out.Risks = append(out.Risks, "WeKnora, the knowledge base, is offline; knowledge search and publishing wait for it.")
		}
	}

	if s.BudgetGate != nil {
		at, err := s.BudgetGate.CacheRefreshedAt(ctx, workspaceID)
		if err != nil {
			return HealthReport{}, fmt.Errorf("budget gate cache: %w", err)
		}
		out.BudgetGate.Reported = true
		if !at.IsZero() {
			age := int64(now.Sub(at) / time.Second)
			out.BudgetGate.CacheAgeSeconds = &age
			if now.Sub(at) > BudgetCacheStaleAfter {
				out.Risks = append(out.Risks, "The budget gate's counters have not refreshed for more than ten minutes; holds may be late.")
			}
		}
	}
	return out, nil
}
