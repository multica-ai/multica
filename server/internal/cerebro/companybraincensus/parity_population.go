package companybraincensus

import (
	"context"
	"fmt"
	"time"
)

// FrozenCensus identifies one immutable migration census and the logical
// Company Brain connection whose target permissions must be compared with it.
type FrozenCensus struct {
	Report                   Report
	Version                  int64
	CompanyBrainConnectionID string
}

// FrozenCensusLoader returns migration evidence that has already been frozen.
// Implementations must not regenerate the census from live Connections.
type FrozenCensusLoader interface {
	LoadFrozenCensus(context.Context, string) (FrozenCensus, error)
}

// CurrentTargetPermissionLoader returns the exact current source-scoped
// permission evidence for the frozen census's logical Company Brain connection.
type CurrentTargetPermissionLoader interface {
	LoadCurrentTargetPermissions(context.Context, string, string) ([]TargetPermission, error)
}

// ParityProofBatchWriter accepts one complete deterministic EvaluateParity
// batch. ParityProofWriter implements this boundary.
type ParityProofBatchWriter interface {
	Write(context.Context, string, []ParityEvaluation) error
}

var _ ParityProofBatchWriter = (*ParityProofWriter)(nil)

// ParityPopulationCoordinator joins the isolated parity evaluator and proof
// writer. It is intentionally not registered with any API, CLI, scheduler, or
// feature flag.
type ParityPopulationCoordinator struct {
	frozen  FrozenCensusLoader
	current CurrentTargetPermissionLoader
	writer  ParityProofBatchWriter
	now     func() time.Time
}

func NewParityPopulationCoordinator(
	frozen FrozenCensusLoader,
	current CurrentTargetPermissionLoader,
	writer ParityProofBatchWriter,
) *ParityPopulationCoordinator {
	return &ParityPopulationCoordinator{
		frozen: frozen, current: current, writer: writer, now: time.Now,
	}
}

// Populate loads one frozen census and the exact current target evidence,
// evaluates the complete actor population once, and writes that single batch.
func (c *ParityPopulationCoordinator) Populate(
	ctx context.Context,
	workspaceID string,
) error {
	if c == nil || c.frozen == nil {
		return fmt.Errorf("frozen census loader is required")
	}
	if c.current == nil {
		return fmt.Errorf("current target permission loader is required")
	}
	if c.writer == nil {
		return fmt.Errorf("parity proof writer is required")
	}
	if c.now == nil {
		return fmt.Errorf("parity population clock is required")
	}

	frozen, err := c.frozen.LoadFrozenCensus(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("load frozen Company Brain census: %w", err)
	}
	targets, err := c.current.LoadCurrentTargetPermissions(
		ctx,
		workspaceID,
		frozen.CompanyBrainConnectionID,
	)
	if err != nil {
		return fmt.Errorf("load current Company Brain target permissions: %w", err)
	}
	evaluations := EvaluateParity(
		frozen.Report,
		targets,
		frozen.Version,
		frozen.CompanyBrainConnectionID,
		c.now(),
	)
	if err := c.writer.Write(ctx, workspaceID, evaluations); err != nil {
		return fmt.Errorf("write Company Brain parity proof batch: %w", err)
	}
	return nil
}
