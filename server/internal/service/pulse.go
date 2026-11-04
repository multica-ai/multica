package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The pulse (spec 4.8): a compact read of the company for the Company header
// strip. It is cached per workspace for a minute, so the header costs one
// read per minute however many people have the Company open.

// PulseTTL is how long a workspace's pulse is reused.
const PulseTTL = time.Minute

// PulseGoalSource counts the goals at risk. The goals tables belong to the goals
// step; until it provides this, the pulse reads zero and says so in its report.
type PulseGoalSource interface {
	GoalsAtRisk(ctx context.Context, workspaceID pgtype.UUID) (int, error)
}

// PulseBudgetSource reads the budget figures. The budget step provides it;
// until then a nil source reads zero.
type PulseBudgetSource interface {
	// BudgetsAboveSoftLine counts budgets past their soft line this period.
	BudgetsAboveSoftLine(ctx context.Context, workspaceID pgtype.UUID) (int, error)
	// BudgetHoldsInForce counts holds not yet released.
	BudgetHoldsInForce(ctx context.Context, workspaceID pgtype.UUID) (int, error)
}

// Pulse is the company at a glance.
type Pulse struct {
	// State is the company state in force: running, paused or quiet.
	State string `json:"state"`
	// DecisionsWaiting is the number of pending proposals, gates included.
	DecisionsWaiting int `json:"decisions_waiting"`
	// OldestDecisionAgeSeconds is the age of the oldest pending decision; -1 when
	// none waits.
	OldestDecisionAgeSeconds int64 `json:"oldest_decision_age_seconds"`
	GoalsAtRisk              int   `json:"goals_at_risk"`
	BudgetsAboveSoftLine     int   `json:"budgets_above_soft_line"`
	BudgetHolds              int   `json:"budget_holds"`
	StalledInitiatives       int   `json:"stalled_initiatives"`
	RoutinesLate             int   `json:"routines_late"`
	// KnowledgeRetrievalHitRate is nil until an eval run is recorded.
	KnowledgeRetrievalHitRate *float64  `json:"knowledge_retrieval_hit_rate"`
	RuntimesOffline           int       `json:"runtimes_offline"`
	GeneratedAt               time.Time `json:"generated_at"`
}

// PulseService builds the pulse. Goals and Budgets may be nil.
type PulseService struct {
	Queries *db.Queries
	Goals   PulseGoalSource
	Budgets PulseBudgetSource
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (s *PulseService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Build reads the pulse now.
func (s *PulseService) Build(ctx context.Context, workspaceID pgtype.UUID) (Pulse, error) {
	now := s.now()
	out := Pulse{OldestDecisionAgeSeconds: -1, GeneratedAt: now.UTC()}

	state, err := (&CompanyStateService{Queries: s.Queries}).Get(ctx, workspaceID)
	if err != nil {
		return Pulse{}, err
	}
	out.State = state.State

	decisions, err := s.Queries.CountPendingProposalsForPulse(ctx, workspaceID)
	if err != nil {
		return Pulse{}, fmt.Errorf("pulse decisions: %w", err)
	}
	out.DecisionsWaiting = int(decisions.Pending)
	if decisions.Pending > 0 && decisions.OldestCreatedAt.Valid {
		out.OldestDecisionAgeSeconds = int64(now.Sub(decisions.OldestCreatedAt.Time) / time.Second)
	}

	if s.Goals != nil {
		if out.GoalsAtRisk, err = s.Goals.GoalsAtRisk(ctx, workspaceID); err != nil {
			return Pulse{}, fmt.Errorf("pulse goals: %w", err)
		}
	}
	if s.Budgets != nil {
		if out.BudgetsAboveSoftLine, err = s.Budgets.BudgetsAboveSoftLine(ctx, workspaceID); err != nil {
			return Pulse{}, fmt.Errorf("pulse budgets: %w", err)
		}
		if out.BudgetHolds, err = s.Budgets.BudgetHoldsInForce(ctx, workspaceID); err != nil {
			return Pulse{}, fmt.Errorf("pulse holds: %w", err)
		}
	}

	pipeline, err := NewDigestService(s.Queries).computePipeline(ctx, workspaceID, now)
	if err != nil {
		return Pulse{}, err
	}
	for _, ini := range pipeline {
		if ini.Stalled {
			out.StalledInitiatives++
		}
	}

	states, err := RoutineComplianceStates(ctx, s.Queries, workspaceID, pgtype.UUID{}, now)
	if err != nil {
		return Pulse{}, fmt.Errorf("pulse routines: %w", err)
	}
	for _, r := range states {
		if r.Required && r.State != RoutineStateOnTime {
			out.RoutinesLate++
		}
	}

	runs, err := s.Queries.ListKnowledgeEvalRuns(ctx, db.ListKnowledgeEvalRunsParams{WorkspaceID: workspaceID, Limit: 1})
	if err != nil {
		return Pulse{}, fmt.Errorf("pulse retrieval: %w", err)
	}
	if h := retrievalHealth(runs); h != nil {
		rate := h.HitRate
		out.KnowledgeRetrievalHitRate = &rate
	}

	out.RuntimesOffline, err = s.countOfflineRuntimes(ctx, workspaceID)
	if err != nil {
		return Pulse{}, err
	}
	return out, nil
}

func (s *PulseService) countOfflineRuntimes(ctx context.Context, workspaceID pgtype.UUID) (int, error) {
	n, err := s.Queries.CountOfflineRuntimes(ctx, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("pulse runtimes: %w", err)
	}
	return int(n), nil
}

// PulseCache keeps each workspace's pulse for ttl. Two requests that arrive
// together may both build; the second write wins, which costs one extra read.
type PulseCache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]pulseEntry
}

type pulseEntry struct {
	pulse Pulse
	at    time.Time
}

// NewPulseCache returns a cache that keeps entries for ttl.
func NewPulseCache(ttl time.Duration) *PulseCache {
	return &PulseCache{ttl: ttl, now: time.Now, entries: map[string]pulseEntry{}}
}

// Get returns the workspace's cached pulse, or builds and caches one.
func (c *PulseCache) Get(ctx context.Context, workspaceID string, build func(context.Context) (Pulse, error)) (Pulse, error) {
	now := c.now()
	c.mu.Lock()
	if e, ok := c.entries[workspaceID]; ok && now.Sub(e.at) < c.ttl {
		c.mu.Unlock()
		return e.pulse, nil
	}
	c.mu.Unlock()

	p, err := build(ctx)
	if err != nil {
		return Pulse{}, err
	}
	c.mu.Lock()
	c.entries[workspaceID] = pulseEntry{pulse: p, at: now}
	c.mu.Unlock()
	return p, nil
}
