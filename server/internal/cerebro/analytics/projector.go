package analytics

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrIncompleteProjection = errors.New("analytics projection is incomplete")

// RunProjection is the complete source snapshot written atomically to the
// analytics projection. Child collections are added here as their source
// adapters land, while the orchestration contract remains stable.
type RunProjection struct {
	RunID, WorkspaceID, Population, SourceType, Status           string
	SourceID, SourceLabel, PersonID, PersonLabel                 string
	AgentID, AgentLabel, ProjectID, ProjectLabel                 string
	RuntimeID, RuntimeLabel, TraceID                             string
	StartedAt                                                    time.Time
	CompletedAt                                                  *time.Time
	DurationSeconds                                              int64
	InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens int64
	CostCents                                                    *int64
	Provider, Model                                              string
	Savings                                                      []SavingProjection
	Quality                                                      []QualityProjection
	Skills                                                       []SkillProjection
	References                                                   []ReferenceProjection
}

type SkillProjection struct {
	ID, Name                string
	InvocationCount         int
	FirstUsedAt, LastUsedAt time.Time
}

type ReferenceProjection struct {
	Kind, ID, Label, Href string
}

type SavingProjection struct {
	Type, Mode, Metric                        string
	Applied                                   bool
	BaselineValue, EffectiveValue, SavedCents int64
	MeasuredAt                                time.Time
}

type QualityProjection struct {
	Type, Category, Verdict, EvaluatorVersion string
	Score, Confidence                         *float64
	MeasuredAt                                time.Time
}

type ProjectionSource interface {
	LoadRun(ctx context.Context, runID string) (RunProjection, error)
	ListRunIDs(ctx context.Context, workspaceID, afterCursor string, limit int) ([]string, error)
}

type ProjectionStore interface {
	UpsertRun(ctx context.Context, run RunProjection) error
}

// Projector rebuilds a run from authoritative source tables. UpsertRun must
// replace the run and its child rows, making replay safe after every write path.
type Projector struct {
	source ProjectionSource
	store  ProjectionStore
}

func NewProjector(source ProjectionSource, store ProjectionStore) *Projector {
	return &Projector{source: source, store: store}
}

func (p *Projector) ProjectRun(ctx context.Context, runID string) error {
	if p == nil || p.source == nil || p.store == nil {
		return errors.New("analytics projector is not configured")
	}
	if runID == "" {
		return fmt.Errorf("%w: run id is required", ErrIncompleteProjection)
	}
	run, err := p.source.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("load analytics run %s: %w", runID, err)
	}
	if run.RunID == "" || run.WorkspaceID == "" || run.SourceType == "" || run.Status == "" {
		return fmt.Errorf("%w: run %s is missing identity, workspace, source, or status", ErrIncompleteProjection, runID)
	}
	if err := p.store.UpsertRun(ctx, run); err != nil {
		return fmt.Errorf("upsert analytics run %s: %w", runID, err)
	}
	return nil
}

// BackfillWorkspace projects one bounded page. The returned cursor advances
// only after a successful run, so callers can safely resume after interruption.
func (p *Projector) BackfillWorkspace(ctx context.Context, workspaceID, afterCursor string, limit int) (string, int, error) {
	if p == nil || p.source == nil || p.store == nil {
		return afterCursor, 0, errors.New("analytics projector is not configured")
	}
	if workspaceID == "" {
		return afterCursor, 0, errors.New("workspace id is required")
	}
	if limit < 1 || limit > 1000 {
		return afterCursor, 0, errors.New("backfill limit must be between 1 and 1000")
	}
	runIDs, err := p.source.ListRunIDs(ctx, workspaceID, afterCursor, limit)
	if err != nil {
		return afterCursor, 0, fmt.Errorf("list analytics runs: %w", err)
	}
	next := afterCursor
	for i, runID := range runIDs {
		if err := p.ProjectRun(ctx, runID); err != nil {
			return next, i, err
		}
		next = runID
	}
	return next, len(runIDs), nil
}
