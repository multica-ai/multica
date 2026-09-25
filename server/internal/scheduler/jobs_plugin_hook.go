package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

const (
	// JobNamePluginHookScheduleDispatch is persisted in sys_cron_executions and
	// must remain stable across releases.
	JobNamePluginHookScheduleDispatch = "plugin_hook_schedule_dispatch"
	ScopeKindPluginHookSchedule       = "plugin_hook_schedule"
	pluginHookScheduleAttempts        = 3
	pluginHookSchedulePageSize        = 1024
)

type pluginHookScheduleConfig struct {
	ID             pgtype.UUID
	InstallationID pgtype.UUID
	HookKey        string
	CronExpression string
	Timezone       string
	Generation     pgtype.UUID
	ActivatedAt    time.Time
}

type pluginHookScheduleCache struct {
	mu        sync.RWMutex
	schedules map[string]pluginHookScheduleConfig
	coalesced map[string]int
}

func newPluginHookScheduleCache() *pluginHookScheduleCache {
	return &pluginHookScheduleCache{
		schedules: make(map[string]pluginHookScheduleConfig),
		coalesced: make(map[string]int),
	}
}

func (c *pluginHookScheduleCache) replace(schedules map[string]pluginHookScheduleConfig) {
	c.mu.Lock()
	c.schedules = schedules
	// Preserve the count across retry ticks, but discard entries for schedules
	// no longer returned by the database. setCoalesced further bounds this to
	// one pending entry per active scope, including on a losing replica.
	for key := range c.coalesced {
		scopeID, _, _ := strings.Cut(key, "/")
		if _, active := schedules[scopeID]; !active {
			delete(c.coalesced, key)
		}
	}
	c.mu.Unlock()
}

func (c *pluginHookScheduleCache) get(scopeID string) (pluginHookScheduleConfig, bool) {
	c.mu.RLock()
	config, ok := c.schedules[scopeID]
	c.mu.RUnlock()
	return config, ok
}

func (c *pluginHookScheduleCache) setCoalesced(scopeID string, plan time.Time, count int) {
	c.mu.Lock()
	for key := range c.coalesced {
		if strings.HasPrefix(key, scopeID+"/") {
			delete(c.coalesced, key)
		}
	}
	c.coalesced[scopeID+"/"+plan.UTC().Format(time.RFC3339Nano)] = count
	c.mu.Unlock()
}

func (c *pluginHookScheduleCache) takeCoalesced(scopeID string, plan time.Time) int {
	key := scopeID + "/" + plan.UTC().Format(time.RFC3339Nano)
	c.mu.Lock()
	count := c.coalesced[key]
	delete(c.coalesced, key)
	c.mu.Unlock()
	return count
}

// PluginHookScheduleDispatchJob drives manifest-declared Plugin schedules
// through the shared DB scheduler. Scope includes a generation UUID, so a
// changed/re-enabled schedule starts a fresh serial timeline while old in-flight
// delivery is allowed to finish.
func PluginHookScheduleDispatchJob(queries *db.Queries, plugins *service.PluginService) JobSpec {
	cache := newPluginHookScheduleCache()
	return JobSpec{
		Name:              JobNamePluginHookScheduleDispatch,
		CatchUpMode:       CatchUpLatestOnly,
		RunTimeout:        45 * time.Second,
		StaleTimeout:      90 * time.Second,
		HeartbeatInterval: 15 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       pluginHookScheduleAttempts,
		RetryBackoff:      []time.Duration{30 * time.Second, 2 * time.Minute},
		MaxPlansPerTick:   1,
		Scopes:            pluginHookScheduleScopes(queries, plugins, cache),
		PlansForScope:     pluginHookSchedulePlans(cache),
		Handler:           pluginHookScheduleHandler(queries, plugins, cache),
	}
}

func pluginHookScheduleScopes(
	queries *db.Queries,
	plugins *service.PluginService,
	cache *pluginHookScheduleCache,
) ScopeProvider {
	return func(ctx context.Context, _ time.Time) ([]Scope, error) {
		if plugins == nil || !featureflags.PluginsV1Enabled(ctx, plugins.FeatureFlags) {
			cache.replace(map[string]pluginHookScheduleConfig{})
			return nil, nil
		}
		rows, err := queries.ListEnabledPluginHookSchedules(ctx)
		if err != nil {
			return nil, fmt.Errorf("plugin schedule scope: list enabled schedules: %w", err)
		}
		configs := make(map[string]pluginHookScheduleConfig, len(rows))
		scopes := make([]Scope, 0, len(rows))
		for _, row := range rows {
			scopeID := pluginHookScheduleScopeID(row.ID, row.Generation)
			if scopeID == "" || !row.ActivatedAt.Valid {
				continue
			}
			configs[scopeID] = pluginHookScheduleConfig{
				ID:             row.ID,
				InstallationID: row.InstallationID,
				HookKey:        row.HookKey,
				CronExpression: row.CronExpression,
				Timezone:       row.Timezone,
				Generation:     row.Generation,
				ActivatedAt:    row.ActivatedAt.Time.UTC(),
			}
			scopes = append(scopes, Scope{Kind: ScopeKindPluginHookSchedule, ID: scopeID})
		}
		cache.replace(configs)
		return scopes, nil
	}
}

func pluginHookSchedulePlans(cache *pluginHookScheduleCache) func(
	context.Context, Scope, time.Time, LatestPlanInfo,
) ([]time.Time, error) {
	return func(_ context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
		config, ok := cache.get(scope.ID)
		if !ok {
			return nil, nil
		}

		// A generation is strictly serial. Do not plan a newer occurrence while
		// its latest row is running, nor while that row still owns retry budget.
		if latest.Found {
			switch latest.Status {
			case "RUNNING":
				return nil, nil
			case "FAILED":
				if latest.Attempt < latest.MaxAttempts {
					if latest.RetryEligible(now) {
						return []time.Time{latest.PlanTime}, nil
					}
					return nil, nil
				}
			}
		}

		after := config.ActivatedAt
		if latest.Found {
			after = latest.PlanTime
		}
		plan, count, err := latestPluginHookOccurrence(
			config.CronExpression, config.Timezone, after, now,
		)
		if err != nil {
			return nil, fmt.Errorf("plugin schedule plans for %s: %w", scope.ID, err)
		}
		if count == 0 {
			return nil, nil
		}
		cache.setCoalesced(scope.ID, plan, count-1)
		return []time.Time{plan}, nil
	}
}

// latestPluginHookOccurrence enumerates in bounded pages so a long outage still
// collapses to the truly latest due occurrence instead of stopping at the first
// 1024 entries returned by service.NextOccurrencesUTC.
func latestPluginHookOccurrence(cronExpression, timezone string, after, until time.Time) (time.Time, int, error) {
	var latest time.Time
	count := 0
	cursor := after
	for {
		occurrences, err := service.NextOccurrencesUTC(cronExpression, timezone, cursor, until)
		if err != nil {
			return time.Time{}, 0, err
		}
		if len(occurrences) == 0 {
			return latest, count, nil
		}
		count += len(occurrences)
		latest = occurrences[len(occurrences)-1]
		if len(occurrences) < pluginHookSchedulePageSize || !latest.Before(until) {
			return latest, count, nil
		}
		cursor = latest
	}
}

func pluginHookScheduleHandler(
	queries *db.Queries,
	plugins *service.PluginService,
	cache *pluginHookScheduleCache,
) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		scheduleID, generation, err := parsePluginHookScheduleScopeID(in.Scope.ID)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("parse plugin hook schedule scope: %w", err)
		}
		if plugins == nil || !featureflags.PluginsV1Enabled(ctx, plugins.FeatureFlags) {
			return pluginHookScheduleSkipped("feature_disabled"), nil
		}

		row, err := queries.GetPluginHookSchedule(ctx, scheduleID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pluginHookScheduleSkipped("schedule_not_found"), nil
			}
			return HandlerResult{}, fmt.Errorf("load plugin hook schedule: %w", err)
		}
		if !row.Enabled || row.Generation != generation {
			return pluginHookScheduleSkipped("schedule_generation_changed"), nil
		}

		installation, err := queries.GetPluginInstallation(ctx, row.InstallationID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pluginHookScheduleSkipped("installation_not_found"), nil
			}
			return HandlerResult{}, fmt.Errorf("load plugin installation: %w", err)
		}
		if !installation.Enabled {
			return pluginHookScheduleSkipped("installation_disabled"), nil
		}
		hook, err := service.FindHook(installation, row.HookKey)
		if err != nil || hook.Schedule == nil ||
			!service.HookAllowsTrigger(hook, plugincontract.TriggerSchedule) ||
			hook.Transport.Type != plugincontract.TransportHTTP ||
			hook.Schedule.Cron != row.CronExpression || hook.Schedule.Timezone != row.Timezone {
			return pluginHookScheduleSkipped("manifest_changed"), nil
		}

		if plugins.HookBreakerOpen(ctx, installation.ID, hook.Key) {
			advancePluginHookNextRun(ctx, queries, row, in.PlanTime)
			result := pluginHookScheduleSkipped("circuit_open")
			result.Result["coalesced_occurrences"] = cache.takeCoalesced(in.Scope.ID, in.PlanTime)
			result.Result["dispatch_lag_ms"] = max(time.Since(in.PlanTime).Milliseconds(), 0)
			return result, nil
		}

		deliveryID := pluginHookScheduleDeliveryID(installation.ID, hook.Key, row.Generation, in.PlanTime)
		_, err = plugins.InvokeHook(ctx, service.HookInvocation{
			Installation: installation,
			Hook:         hook,
			Trigger:      plugincontract.TriggerSchedule,
			Actor:        service.HookActor{Type: "plugin", ID: installation.ID},
			DeliveryID:   deliveryID,
			PlannedAt:    pgtype.Timestamptz{Time: in.PlanTime.UTC(), Valid: true},
		}, in.Attempt)
		if err != nil {
			if in.Job != nil && in.Attempt >= in.Job.MaxAttempts {
				advancePluginHookNextRun(ctx, queries, row, in.PlanTime)
				cache.takeCoalesced(in.Scope.ID, in.PlanTime)
			}
			return HandlerResult{}, fmt.Errorf("deliver scheduled plugin hook: %w", err)
		}

		advancePluginHookNextRun(ctx, queries, row, in.PlanTime)
		return HandlerResult{RowsAffected: 1, Result: map[string]any{
			"delivery_id":           deliveryID,
			"coalesced_occurrences": cache.takeCoalesced(in.Scope.ID, in.PlanTime),
			"dispatch_lag_ms":       max(time.Since(in.PlanTime).Milliseconds(), 0),
		}}, nil
	}
}

func pluginHookScheduleSkipped(reason string) HandlerResult {
	return HandlerResult{RowsAffected: 0, Result: map[string]any{"skipped_reason": reason}}
}

func advancePluginHookNextRun(ctx context.Context, queries *db.Queries, row db.PluginHookSchedule, plan time.Time) {
	next, err := service.NextOccurrenceAfterUTC(row.CronExpression, row.Timezone, plan)
	if err != nil {
		return
	}
	_, _ = queries.UpdatePluginHookScheduleNextRun(ctx, db.UpdatePluginHookScheduleNextRunParams{
		ID:         row.ID,
		Generation: row.Generation,
		NextRunAt:  pgtype.Timestamptz{Time: next.UTC(), Valid: true},
	})
}

func pluginHookScheduleScopeID(id, generation pgtype.UUID) string {
	idString := util.UUIDToString(id)
	generationString := util.UUIDToString(generation)
	if idString == "" || generationString == "" {
		return ""
	}
	return idString + ":" + generationString
}

func parsePluginHookScheduleScopeID(scopeID string) (pgtype.UUID, pgtype.UUID, error) {
	parts := strings.Split(scopeID, ":")
	if len(parts) != 2 {
		return pgtype.UUID{}, pgtype.UUID{}, errors.New("expected schedule:generation scope")
	}
	id, err := util.ParseUUID(parts[0])
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	generation, err := util.ParseUUID(parts[1])
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	return id, generation, nil
}

func pluginHookScheduleDeliveryID(
	installationID pgtype.UUID,
	hookKey string,
	generation pgtype.UUID,
	plan time.Time,
) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		util.UUIDToString(installationID),
		hookKey,
		util.UUIDToString(generation),
		plan.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return "psd_" + hex.EncodeToString(digest[:])
}
