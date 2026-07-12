package analytics

import (
	"context"
	"fmt"
)

type RunProjector interface {
	ProjectRun(ctx context.Context, runID string) error
}

// ProjectAfterWrite keeps source writes authoritative while making projection
// failures visible to retrying callers. A nil projector preserves startup and
// test compatibility until the Postgres adapter is wired.
func ProjectAfterWrite(ctx context.Context, projector RunProjector, runID string, write func() error) error {
	if err := write(); err != nil {
		return err
	}
	if projector == nil {
		return nil
	}
	if err := projector.ProjectRun(ctx, runID); err != nil {
		return fmt.Errorf("project analytics run after write: %w", err)
	}
	return nil
}
