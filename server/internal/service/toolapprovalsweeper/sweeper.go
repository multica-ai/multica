package toolapprovalsweeper

import (
	"context"
	"fmt"
	"time"
)

const defaultRetentionDays = 90
const defaultBatchSize = 100

type RetentionResult struct {
	ApprovalsDeleted    int
	ActionEventsDeleted int
}

type Result struct {
	Expired             int
	ApprovalsDeleted    int
	ActionEventsDeleted int
}

type Store interface {
	ExpireDue(context.Context, time.Time, int32) (int, error)
	DeleteRetained(context.Context, time.Time, int32) (RetentionResult, error)
}

type Config struct {
	RetentionDays int
	BatchSize     int32
}

type Sweeper struct {
	store  Store
	config Config
	now    func() time.Time
}

func New(store Store, config Config) *Sweeper {
	if config.RetentionDays <= 0 {
		config.RetentionDays = defaultRetentionDays
	}
	if config.BatchSize <= 0 {
		config.BatchSize = defaultBatchSize
	}
	return &Sweeper{store: store, config: config, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Sweeper) RunOnce(ctx context.Context) (Result, error) {
	now := s.now().UTC()
	expired, err := s.store.ExpireDue(ctx, now, s.config.BatchSize)
	if err != nil {
		return Result{}, fmt.Errorf("expire tool approvals: %w", err)
	}
	retained, err := s.store.DeleteRetained(ctx, now.AddDate(0, 0, -s.config.RetentionDays), s.config.BatchSize)
	if err != nil {
		return Result{}, fmt.Errorf("delete retained tool controls: %w", err)
	}
	return Result{Expired: expired, ApprovalsDeleted: retained.ApprovalsDeleted, ActionEventsDeleted: retained.ActionEventsDeleted}, nil
}
