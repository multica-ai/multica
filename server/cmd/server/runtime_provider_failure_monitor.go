package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// runtimeProviderFailureMonitorConfig controls runtime-wide provider
// capacity/session-limit alerting. It deliberately alerts without pausing
// anything: retries and fallback handle individual tasks, while the alert
// tells workspace operators that upstream capacity is repeatedly degrading.
type runtimeProviderFailureMonitorConfig struct {
	Interval     time.Duration
	Lookback     time.Duration
	Threshold    int64
	StartupDelay time.Duration
}

func defaultRuntimeProviderFailureMonitorConfig() runtimeProviderFailureMonitorConfig {
	return runtimeProviderFailureMonitorConfig{
		Interval:     time.Hour,
		Lookback:     24 * time.Hour,
		Threshold:    3,
		StartupDelay: time.Minute,
	}
}

func envRuntimeProviderFailureMonitorConfig() runtimeProviderFailureMonitorConfig {
	cfg := defaultRuntimeProviderFailureMonitorConfig()
	cfg.Interval = envDurationOrZero("RUNTIME_PROVIDER_FAILURE_MONITOR_INTERVAL", cfg.Interval)
	cfg.Lookback = envDurationPositive("RUNTIME_PROVIDER_FAILURE_MONITOR_LOOKBACK", cfg.Lookback)
	cfg.StartupDelay = envDurationNonNegative("RUNTIME_PROVIDER_FAILURE_MONITOR_STARTUP_DELAY", cfg.StartupDelay)
	if v, ok := envInt64Positive("RUNTIME_PROVIDER_FAILURE_MONITOR_THRESHOLD"); ok {
		cfg.Threshold = v
	}
	return cfg
}

func runRuntimeProviderFailureMonitor(ctx context.Context, queries *db.Queries, bus *events.Bus, cfg runtimeProviderFailureMonitorConfig) {
	if cfg.Interval <= 0 {
		slog.Info("runtime provider failure monitor: disabled (interval <= 0)")
		return
	}

	slog.Info(
		"runtime provider failure monitor: starting",
		"interval", cfg.Interval.String(),
		"lookback", cfg.Lookback.String(),
		"threshold", cfg.Threshold,
	)

	if cfg.StartupDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.StartupDelay):
		}
	}

	tickRuntimeProviderFailureMonitor(ctx, queries, bus, cfg)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickRuntimeProviderFailureMonitor(ctx, queries, bus, cfg)
		}
	}
}

func tickRuntimeProviderFailureMonitor(ctx context.Context, queries *db.Queries, bus *events.Bus, cfg runtimeProviderFailureMonitorConfig) {
	if cfg.Threshold <= 0 || cfg.Lookback <= 0 {
		return
	}

	since := time.Now().Add(-cfg.Lookback)
	candidates, err := queries.SelectWorkspacesExceedingProviderFailureThreshold(
		ctx,
		db.SelectWorkspacesExceedingProviderFailureThresholdParams{
			Since:         pgtype.Timestamptz{Time: since, Valid: true},
			FailureReason: taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(),
			Threshold:     cfg.Threshold,
		},
	)
	if err != nil {
		slog.Warn("runtime provider failure monitor: failed to query candidates", "error", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	slog.Info("runtime provider failure monitor: candidates", "count", len(candidates))
	for _, c := range candidates {
		emitRuntimeProviderFailureAlert(ctx, queries, bus, c, cfg)
	}
}

type runtimeProviderFailureRecipient struct {
	Type string
	ID   pgtype.UUID
}

func runtimeProviderFailureRecipients(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
) []runtimeProviderFailureRecipient {
	members, err := queries.ListMembers(ctx, workspaceID)
	if err != nil {
		slog.Warn("runtime provider failure monitor: failed to list members",
			"workspace_id", util.UUIDToString(workspaceID),
			"error", err,
		)
		return nil
	}

	recipients := make([]runtimeProviderFailureRecipient, 0, len(members))
	for _, m := range members {
		if m.Role == "owner" || m.Role == "admin" {
			recipients = append(recipients, runtimeProviderFailureRecipient{
				Type: "member",
				ID:   m.UserID,
			})
		}
	}
	return recipients
}

func emitRuntimeProviderFailureAlert(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	c db.SelectWorkspacesExceedingProviderFailureThresholdRow,
	cfg runtimeProviderFailureMonitorConfig,
) {
	recipients := runtimeProviderFailureRecipients(ctx, queries, c.WorkspaceID)
	if len(recipients) == 0 {
		return
	}

	title := "Runtime provider capacity alert"
	body := fmt.Sprintf(
		"%d capacity/session-limit task failures were recorded in the last %s. Runtime retries and model fallback will continue, but inspect provider capacity or account limits if this repeats.",
		c.FailedTasks, formatLookback(cfg.Lookback),
	)
	details, _ := json.Marshal(map[string]any{
		"reason":            "provider_capacity_or_session_limit",
		"failure_reason":    taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(),
		"failed_tasks":      c.FailedTasks,
		"threshold":         cfg.Threshold,
		"lookback_seconds":  int64(cfg.Lookback.Seconds()),
		"first_failed_at":   util.TimestampToString(c.FirstFailedAt),
		"last_failed_at":    util.TimestampToString(c.LastFailedAt),
		"sample_task_id":    util.UUIDToString(c.SampleTaskID),
		"sample_task_error": c.SampleError,
	})

	workspaceID := util.UUIDToString(c.WorkspaceID)
	emitted := make(map[string]bool, len(recipients))
	for _, r := range recipients {
		key := r.Type + ":" + util.UUIDToString(r.ID)
		if emitted[key] {
			continue
		}
		emitted[key] = true

		item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   c.WorkspaceID,
			RecipientType: r.Type,
			RecipientID:   r.ID,
			Type:          "runtime_provider_capacity_alert",
			Severity:      "attention",
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText("system"),
			ActorID:       pgtype.UUID{},
			Details:       details,
		})
		if err != nil {
			slog.Warn("runtime provider failure monitor: inbox write failed",
				"workspace_id", workspaceID,
				"recipient_type", r.Type,
				"recipient_id", util.UUIDToString(r.ID),
				"error", err,
			)
			continue
		}

		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			ActorID:     "",
			Payload:     map[string]any{"item": inboxItemToResponse(item)},
		})
	}
}
