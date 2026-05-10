package scheduler

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Config holds configurable hours for the nightly scheduler.
type Config struct {
	// ReviewHour is the local hour (0-23) to trigger nightly review generation. Default: 22.
	ReviewHour int
	// PlanHour is the local hour (0-23) to trigger morning plan generation. Default: 7.
	PlanHour int
}

// DefaultConfig returns sensible defaults read from environment variables.
func DefaultConfig() Config {
	cfg := Config{ReviewHour: 22, PlanHour: 7}
	if h := os.Getenv("REVIEW_HOUR"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v >= 0 && v <= 23 {
			cfg.ReviewHour = v
		}
	}
	if h := os.Getenv("PLAN_HOUR"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v >= 0 && v <= 23 {
			cfg.PlanHour = v
		}
	}
	return cfg
}

// NightlyScheduler triggers daily review and plan generation at configured hours.
// Uses the goroutine+timer pattern consistent with the runtime sweeper.
type NightlyScheduler struct {
	reviewSvc *service.ReviewService
	planSvc   *service.DailyPlanService
	q         *db.Queries
	cfg       Config
}

// New creates a NightlyScheduler with the given services and config.
func New(reviewSvc *service.ReviewService, planSvc *service.DailyPlanService, q *db.Queries, cfg Config) *NightlyScheduler {
	return &NightlyScheduler{
		reviewSvc: reviewSvc,
		planSvc:   planSvc,
		q:         q,
		cfg:       cfg,
	}
}

// Start launches two goroutines — one for review, one for plan — that sleep until
// the configured hour each day and then process all workspaces/users.
// Exits when ctx is cancelled.
func (s *NightlyScheduler) Start(ctx context.Context) {
	go s.runLoop(ctx, s.cfg.ReviewHour, "review", s.runReviewCycle)
	go s.runLoop(ctx, s.cfg.PlanHour, "plan", s.runPlanCycle)
}

// runLoop waits until the next fire time at the given hour, then calls fn and repeats.
func (s *NightlyScheduler) runLoop(ctx context.Context, hour int, name string, fn func(context.Context)) {
	for {
		wait := nextFireTime(hour)
		slog.Info("scheduler: next trigger", "job", name, "in", wait.Round(time.Minute).String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		fn(ctx)
	}
}

// runReviewCycle generates review drafts for all active users across all workspaces.
func (s *NightlyScheduler) runReviewCycle(ctx context.Context) {
	slog.Info("scheduler: starting nightly review cycle")
	today := time.Now().UTC()

	workspaces, err := s.q.ListAllWorkspaces(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list workspaces", "error", err)
		return
	}

	for _, ws := range workspaces {
		members, err := s.q.ListMembers(ctx, ws.ID)
		if err != nil {
			slog.Warn("scheduler: failed to list members", "workspace", util.UUIDToString(ws.ID), "error", err)
			continue
		}
		for _, m := range members {
			if _, err := s.reviewSvc.GenerateReviewDraft(ctx, ws.ID, m.UserID, today, "scheduled"); err != nil {
				slog.Warn("scheduler: review draft failed", "workspace", util.UUIDToString(ws.ID), "user", util.UUIDToString(m.UserID), "error", err)
			}
		}
	}

	slog.Info("scheduler: nightly review cycle complete", "workspaces", len(workspaces))
}

// runPlanCycle generates plan drafts (for tomorrow) for all active users across all workspaces.
func (s *NightlyScheduler) runPlanCycle(ctx context.Context) {
	slog.Info("scheduler: starting morning plan cycle")
	tomorrow := time.Now().UTC().Add(24 * time.Hour)

	workspaces, err := s.q.ListAllWorkspaces(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list workspaces", "error", err)
		return
	}

	for _, ws := range workspaces {
		members, err := s.q.ListMembers(ctx, ws.ID)
		if err != nil {
			slog.Warn("scheduler: failed to list members", "workspace", util.UUIDToString(ws.ID), "error", err)
			continue
		}
		for _, m := range members {
			if _, err := s.planSvc.GeneratePlanDraft(ctx, ws.ID, m.UserID, tomorrow, "scheduled"); err != nil {
				slog.Warn("scheduler: plan draft failed", "workspace", util.UUIDToString(ws.ID), "user", util.UUIDToString(m.UserID), "error", err)
			}
		}
	}

	slog.Info("scheduler: morning plan cycle complete", "workspaces", len(workspaces))
}

// nextFireTime calculates the duration until the next occurrence of the given hour (local time).
func nextFireTime(hour int) time.Duration {
	now := time.Now().Local()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !now.Before(next) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}
