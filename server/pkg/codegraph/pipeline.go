package codegraph

import (
	"context"
	"time"
)

// Pipeline bounds admitted work separately from each scarce execution resource.
type Pipeline struct {
	Builds  chan struct{}
	Clone   chan struct{}
	Extract chan struct{}
	Publish chan struct{}
}

func NewPipeline() *Pipeline {
	return &Pipeline{make(chan struct{}, 3), make(chan struct{}, 2), make(chan struct{}, 1), make(chan struct{}, 1)}
}

// ActiveBudget is owned by one build. Slot waits do not spend its execution time.
type ActiveBudget struct{ Remaining time.Duration }

func (b *ActiveBudget) Run(ctx context.Context, gate chan struct{}, run func(context.Context) error) error {
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-gate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.Remaining <= 0 {
		return context.DeadlineExceeded
	}
	started := time.Now()
	stageCtx, cancel := context.WithTimeout(ctx, b.Remaining)
	defer func() { cancel(); b.Remaining -= time.Since(started) }()
	err := run(stageCtx)
	if err != nil {
		return err
	}
	return stageCtx.Err()
}
