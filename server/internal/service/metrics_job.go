package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

// RunMetricNightly is the work of the nightly metric job: the built-in readings of
// every workspace, the derived confidence of its active goals, then the retention
// pass over old readings. A failure in one workspace does not stop the others. It
// returns how many readings it wrote, and an error that joins what failed.
func (s *GoalService) RunMetricNightly(ctx context.Context, now time.Time) (int, error) {
	wsIDs, err := s.Queries.ListAllWorkspaceIDsForGovernance(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	written := 0
	var errs []error
	for _, ws := range wsIDs {
		n, err := s.Metrics.RollupBuiltinMetrics(ctx, ws, now)
		written += n
		if err != nil {
			errs = append(errs, fmt.Errorf("built-in metrics for workspace %s: %w", util.UUIDToString(ws), err))
		}
		if _, err := s.RefreshConfidence(ctx, ws); err != nil {
			errs = append(errs, fmt.Errorf("goal confidence for workspace %s: %w", util.UUIDToString(ws), err))
		}
	}
	if _, err := s.Metrics.Downsample(ctx, now); err != nil {
		errs = append(errs, err)
	}
	return written, errors.Join(errs...)
}
