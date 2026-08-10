package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/runtimepool"
	"github.com/multica-ai/multica/server/internal/service"
)

type runtimePoolSweepRecorder struct {
	limits []int
}

func (r *runtimePoolSweepRecorder) AssignWaiting(context.Context, runtimepool.AssignRequest) (runtimepool.AssignResult, error) {
	return runtimepool.AssignResult{}, nil
}

func (r *runtimePoolSweepRecorder) SweepWaiting(_ context.Context, limit int) ([]runtimepool.AssignResult, error) {
	r.limits = append(r.limits, limit)
	return nil, nil
}

// Break caught: omitting Pool recovery from the existing runtime-sweeper tick
// leaves committed waiting/deferred Tasks dependent on an in-process wake.
func TestRuntimePoolSweepUsesExistingTickerBound(t *testing.T) {
	recorder := &runtimePoolSweepRecorder{}
	taskSvc := &service.TaskService{RuntimePool: recorder}

	sweepRuntimePool(context.Background(), taskSvc)

	if len(recorder.limits) != 1 || recorder.limits[0] != runtimepool.WorkspaceSweepLimit {
		t.Fatalf("SweepWaiting limits = %v; want [%d]", recorder.limits, runtimepool.WorkspaceSweepLimit)
	}
}
