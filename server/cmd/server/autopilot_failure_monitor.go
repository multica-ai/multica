package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// failureMonitorConfig is the tunable knob set for the autopilot failure
// monitor. Defaults match the proposal in MUL-1336 §6 action item #2:
// pause autopilots whose recent run history is dominated by failures and that
// have run enough times that the failure rate is statistically meaningful.
//
// All values can be overridden via env vars (see envFailureMonitorConfig).
// Setting Interval <= 0 disables the monitor entirely.
type failureMonitorConfig struct {
	Interval     time.Duration
	Lookback     time.Duration
	MinRuns      int64
	FailRatio    float64
	StartupDelay time.Duration
}

func defaultFailureMonitorConfig() failureMonitorConfig {
	return failureMonitorConfig{
		Interval:     24 * time.Hour,
		Lookback:     7 * 24 * time.Hour,
		MinRuns:      50,
		FailRatio:    0.9,
		StartupDelay: 1 * time.Minute,
	}
}

func envFailureMonitorConfig() failureMonitorConfig {
	cfg := defaultFailureMonitorConfig()
	cfg.Interval = envDurationOrZero("AUTOPILOT_FAIL_MONITOR_INTERVAL", cfg.Interval)
	cfg.Lookback = envDurationPositive("AUTOPILOT_FAIL_MONITOR_LOOKBACK", cfg.Lookback)
	cfg.StartupDelay = envDurationNonNegative("AUTOPILOT_FAIL_MONITOR_STARTUP_DELAY", cfg.StartupDelay)
	if v, ok := envInt64Positive("AUTOPILOT_FAIL_MONITOR_MIN_RUNS"); ok {
		cfg.MinRuns = v
	}
	if v, ok := envFloatInUnitInterval("AUTOPILOT_FAIL_MONITOR_FAIL_RATIO"); ok {
		cfg.FailRatio = v
	}
	return cfg
}

// runAutopilotFailureMonitor periodically pauses autopilots whose recent run
// history exceeds the configured failure threshold. This stops runaway
// scheduled autopilots from burning tasks/tokens on a hot loop (e.g. the
// `Registro de ls cada 5 min` case in MUL-1336: 1,475 / 1,476 runs failed
// over 7 days, still firing every 5 min). The monitor leaves a
// `severity=attention` inbox notification for the autopilot's creator (or the
// agent's owner if the autopilot was created by an agent) so somebody human
// learns that auto-pause happened.
//
// Disable with `AUTOPILOT_FAIL_MONITOR_INTERVAL=0`.
func runAutopilotFailureMonitor(ctx context.Context, queries *db.Queries, bus *events.Bus, cfg failureMonitorConfig) {
	if cfg.Interval <= 0 {
		slog.Info("autopilot failure monitor: disabled (interval <= 0)")
		return
	}

	slog.Info(
		"autopilot failure monitor: starting",
		"interval", cfg.Interval.String(),
		"lookback", cfg.Lookback.String(),
		"min_runs", cfg.MinRuns,
		"fail_ratio", cfg.FailRatio,
	)

	// Stagger startup so we don't all-or-nothing hit the DB the moment the
	// process boots — important during a fleet rolling restart.
	if cfg.StartupDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.StartupDelay):
		}
	}

	// Run once immediately after the startup delay so a freshly-deployed node
	// catches existing offenders without waiting a full interval.
	tickAutopilotFailureMonitor(ctx, queries, bus, cfg)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickAutopilotFailureMonitor(ctx, queries, bus, cfg)
		}
	}
}

// tickAutopilotFailureMonitor performs a single sweep: query candidates,
// attempt to pause each, and emit notifications + WS events on success.
func tickAutopilotFailureMonitor(ctx context.Context, queries *db.Queries, bus *events.Bus, cfg failureMonitorConfig) {
	since := time.Now().Add(-cfg.Lookback)
	candidates, err := queries.SelectAutopilotsExceedingFailureThreshold(
		ctx,
		db.SelectAutopilotsExceedingFailureThresholdParams{
			MinRuns:            cfg.MinRuns,
			FailRatioThreshold: cfg.FailRatio,
			Since:              pgtype.Timestamptz{Time: since, Valid: true},
		},
	)
	if err != nil {
		slog.Warn("autopilot failure monitor: failed to query candidates", "error", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	slog.Info("autopilot failure monitor: candidates", "count", len(candidates))

	for _, c := range candidates {
		paused, err := queries.SystemPauseAutopilot(ctx, c.ID)
		if err != nil {
			// pgx returns ErrNoRows when the WHERE status='active' clause
			// matched zero rows — i.e. another caller (manual UI action,
			// concurrent monitor) paused it first. Treat as a benign no-op.
			if isNoRows(err) {
				continue
			}
			slog.Warn("autopilot failure monitor: pause failed",
				"autopilot_id", util.UUIDToString(c.ID),
				"error", err,
			)
			continue
		}

		failPct := 100.0
		if c.TotalRuns > 0 {
			failPct = math.Round(float64(c.FailedRuns)/float64(c.TotalRuns)*1000) / 10 // one decimal place
		}

		slog.Info(
			"autopilot failure monitor: paused autopilot",
			"autopilot_id", util.UUIDToString(c.ID),
			"workspace_id", util.UUIDToString(c.WorkspaceID),
			"title", c.Title,
			"failed_runs", c.FailedRuns,
			"total_runs", c.TotalRuns,
			"fail_pct", failPct,
		)

		emitAutopilotPausedNotifications(ctx, queries, bus, paused, c, cfg, failPct)

		// Fan out the status change so any open UI updates the autopilot row.
		workspaceID := util.UUIDToString(paused.WorkspaceID)
		bus.Publish(events.Event{
			Type:        protocol.EventAutopilotUpdated,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"autopilot": autopilotEventPayload(paused),
				"reason":    "auto_paused_high_failure_rate",
			},
		})
	}
}

// emitAutopilotPausedNotifications creates one inbox_item per relevant
// recipient and publishes inbox:new events so each lands live. Recipients:
//
//  1. The autopilot creator if a member.
//  2. If the autopilot creator is an agent, the agent's owner_id (mapped to a
//     workspace member).
//
// Resolving against owner_id keeps us from pinging an agent whose inbox isn't
// actionable, while still attributing the alert to whoever set the autopilot
// up. If neither path lands a human (e.g. agent has no owner), we skip
// silently — the WS autopilot:updated event still surfaces the change in the
// UI for any logged-in workspace member.
func emitAutopilotPausedNotifications(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	autopilot db.Autopilot,
	candidate db.SelectAutopilotsExceedingFailureThresholdRow,
	cfg failureMonitorConfig,
	failPct float64,
) {
	recipients := resolveAutopilotPausedRecipients(ctx, queries, autopilot)
	if len(recipients) == 0 {
		return
	}

	title := fmt.Sprintf("Autopilot paused: %s", autopilot.Title)
	body := fmt.Sprintf(
		"Auto-paused after %d of %d runs failed (%.1f%%) in the last %s. Investigate the failures, fix the root cause, then re-enable from the autopilot page.",
		candidate.FailedRuns, candidate.TotalRuns, failPct, formatLookback(cfg.Lookback),
	)
	details, _ := json.Marshal(map[string]any{
		"autopilot_id":         util.UUIDToString(autopilot.ID),
		"autopilot_title":      autopilot.Title,
		"failed_runs":          candidate.FailedRuns,
		"total_runs":           candidate.TotalRuns,
		"fail_pct":             failPct,
		"lookback_seconds":     int64(cfg.Lookback.Seconds()),
		"threshold_min_runs":   cfg.MinRuns,
		"threshold_fail_ratio": cfg.FailRatio,
		"reason":               "auto_paused_high_failure_rate",
	})

	workspaceID := util.UUIDToString(autopilot.WorkspaceID)
	autopilotIDStr := util.UUIDToString(autopilot.ID)

	emitted := make(map[string]bool, len(recipients))
	for _, r := range recipients {
		key := r.Type + ":" + util.UUIDToString(r.ID)
		if emitted[key] {
			continue
		}
		emitted[key] = true

		item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   autopilot.WorkspaceID,
			RecipientType: r.Type,
			RecipientID:   r.ID,
			Type:          "autopilot_paused",
			Severity:      "attention",
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText("system"),
			ActorID:       pgtype.UUID{},
			Details:       details,
		})
		if err != nil {
			slog.Warn("autopilot failure monitor: inbox write failed",
				"autopilot_id", autopilotIDStr,
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

// pausedRecipient identifies a single inbox_item recipient.
type pausedRecipient struct {
	Type string // "member" or "agent"
	ID   pgtype.UUID
}

func resolveAutopilotPausedRecipients(
	ctx context.Context,
	queries *db.Queries,
	autopilot db.Autopilot,
) []pausedRecipient {
	if autopilot.CreatedByType == "member" {
		return []pausedRecipient{{Type: "member", ID: autopilot.CreatedByID}}
	}

	// Creator is an agent — find the agent's human owner so the alert lands
	// somewhere actionable. If we can't resolve a member, skip notification
	// rather than spam an agent that can't act on it.
	agent, err := queries.GetAgent(ctx, autopilot.CreatedByID)
	if err != nil {
		slog.Debug("autopilot failure monitor: failed to load creator agent",
			"agent_id", util.UUIDToString(autopilot.CreatedByID),
			"error", err,
		)
		return nil
	}
	if !agent.OwnerID.Valid {
		return nil
	}

	member, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      agent.OwnerID,
		WorkspaceID: autopilot.WorkspaceID,
	})
	if err != nil {
		return nil
	}
	return []pausedRecipient{{Type: "member", ID: member.UserID}}
}

// autopilotEventPayload builds the minimal payload shape consumed by
// frontend listeners (mirrors handler.AutopilotResponse). Kept here instead
// of importing the handler package to avoid a cycle (handler imports the
// service which we're sitting alongside in cmd/server).
func autopilotEventPayload(a db.Autopilot) map[string]any {
	return map[string]any{
		"id":                   util.UUIDToString(a.ID),
		"workspace_id":         util.UUIDToString(a.WorkspaceID),
		"title":                a.Title,
		"description":          util.TextToPtr(a.Description),
		"assignee_id":          util.UUIDToString(a.AssigneeID),
		"status":               a.Status,
		"execution_mode":       a.ExecutionMode,
		"issue_title_template": util.TextToPtr(a.IssueTitleTemplate),
		"created_by_type":      a.CreatedByType,
		"created_by_id":        util.UUIDToString(a.CreatedByID),
		"last_run_at":          util.TimestampToPtr(a.LastRunAt),
		"created_at":           util.TimestampToString(a.CreatedAt),
		"updated_at":           util.TimestampToString(a.UpdatedAt),
	}
}

// isNoRows wraps the sentinel for pgx :one queries that match no rows. The
// SystemPauseAutopilot UPDATE returns no rows when the autopilot was already
// paused/archived, which we want to treat as a benign no-op rather than an
// error to log.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func formatLookback(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	hours := d / time.Hour
	if hours >= 24 && d%(24*time.Hour) == 0 {
		days := hours / 24
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	if d%time.Hour == 0 {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return d.String()
}

// envDurationOrZero parses a duration env var. An explicit 0/negative is
// honored (used to disable the monitor); empty returns the default; an
// unparseable value warns and returns the default.
func envDurationOrZero(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func envDurationPositive(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func envDurationNonNegative(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func envInt64Positive(name string) (int64, bool) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, ignored", "name", name, "value", raw, "error", err)
		return 0, false
	}
	return v, true
}

func envFloatInUnitInterval(name string) (float64, bool) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 || v > 1 {
		slog.Warn("invalid env var (must be in (0,1]), ignored", "name", name, "value", raw, "error", err)
		return 0, false
	}
	return v, true
}
