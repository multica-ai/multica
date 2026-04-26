package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/robfig/cron/v3"
)

const defaultAutopilotSchedulerBatchSize int32 = 20

type AutopilotSchedulerConfig struct {
	Interval  time.Duration
	BatchSize int32
	Now       func() time.Time
}

type AutopilotScheduler struct {
	service   *AutopilotService
	interval  time.Duration
	batchSize int32
	now       func() time.Time
}

func NewAutopilotScheduler(service *AutopilotService, cfg AutopilotSchedulerConfig) *AutopilotScheduler {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultAutopilotSchedulerBatchSize
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AutopilotScheduler{
		service:   service,
		interval:  interval,
		batchSize: batchSize,
		now:       now,
	}
}

func (s *AutopilotScheduler) Run(ctx context.Context) {
	s.runOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *AutopilotScheduler) runOnce(ctx context.Context) {
	runs, err := s.service.ProcessDueSchedules(ctx, s.now(), s.batchSize)
	if err != nil {
		slog.Warn("autopilot scheduler run completed with errors", "error", err, "runs", len(runs))
		return
	}
	if len(runs) > 0 {
		slog.Info("autopilot scheduler processed runs", "runs", len(runs))
	}
}

func NormalizeSchedule(cronExpr, timezone string, now time.Time) (string, string, time.Time, error) {
	cronExpr = strings.TrimSpace(cronExpr)
	if cronExpr == "" {
		return "", "", time.Time{}, fmt.Errorf("cron is required")
	}

	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if timezone == "Local" {
		return "", "", time.Time{}, fmt.Errorf("timezone must be an IANA timezone")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("invalid timezone")
	}

	schedule, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("invalid cron")
	}

	next := schedule.Next(now.In(location))
	if next.IsZero() {
		return "", "", time.Time{}, fmt.Errorf("cron has no next run")
	}
	return cronExpr, timezone, next, nil
}

func NextScheduleRunAt(cronExpr, timezone string, now time.Time) (time.Time, error) {
	_, _, next, err := NormalizeSchedule(cronExpr, timezone, now)
	return next, err
}

func ScheduleIdempotencyKey(triggerID pgtype.UUID, scheduledFor time.Time) string {
	return "schedule:" + util.UUIDToString(triggerID) + ":" + scheduledFor.UTC().Format(time.RFC3339Nano)
}
