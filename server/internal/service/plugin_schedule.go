package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// reconcilePluginHookSchedules makes the durable schedule projection match the
// consented manifest inside the caller's installation/upgrade transaction.
// Unchanged cron/timezone pairs keep their generation: a code-only plugin
// upgrade must not reset the schedule clock or orphan a retryable execution.
func (s *PluginService) reconcilePluginHookSchedules(
	ctx context.Context,
	queries *db.Queries,
	installation db.PluginInstallation,
	manifest plugincontract.Manifest,
) error {
	existingRows, err := queries.ListPluginHookSchedulesByInstallation(ctx, installation.ID)
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin hook schedules", Err: err}
	}
	existing := make(map[string]db.PluginHookSchedule, len(existingRows))
	for _, row := range existingRows {
		existing[row.HookKey] = row
	}

	now := time.Now().UTC()
	for _, hook := range manifest.Contributes.Hooks {
		if hook.Schedule == nil || !HookAllowsTrigger(hook, plugincontract.TriggerSchedule) {
			continue
		}
		nextRun, err := pluginScheduleNextRun(*hook.Schedule, now)
		if err != nil {
			// Published manifests were validated already. Surfacing drift here is
			// safer than installing a schedule the runtime cannot evaluate.
			return &PluginError{Kind: PluginErrorInvalid, Message: fmt.Sprintf("schedule for hook %q is invalid", hook.Key), Err: err}
		}
		if !installation.Enabled {
			// Disabled installations have no meaningful "next" run. Re-enable
			// rotates the generation and computes this projection from that time.
			nextRun = pgtype.Timestamptz{}
		}

		row, found := existing[hook.Key]
		if !found {
			if _, err := queries.CreatePluginHookSchedule(ctx, db.CreatePluginHookScheduleParams{
				InstallationID: installation.ID,
				WorkspaceID:    installation.WorkspaceID,
				HookKey:        hook.Key,
				CronExpression: hook.Schedule.Cron,
				Timezone:       hook.Schedule.Timezone,
				Enabled:        installation.Enabled,
				NextRunAt:      nextRun,
			}); err != nil {
				return &PluginError{Kind: PluginErrorUnavailable, Message: "create plugin hook schedule", Err: err}
			}
			continue
		}
		delete(existing, hook.Key)
		if row.CronExpression == hook.Schedule.Cron && row.Timezone == hook.Schedule.Timezone {
			continue
		}
		if _, err := queries.UpdatePluginHookScheduleDefinition(ctx, db.UpdatePluginHookScheduleDefinitionParams{
			ID:             row.ID,
			CronExpression: hook.Schedule.Cron,
			Timezone:       hook.Schedule.Timezone,
			Enabled:        installation.Enabled,
			NextRunAt:      nextRun,
		}); err != nil {
			return &PluginError{Kind: PluginErrorUnavailable, Message: "update plugin hook schedule", Err: err}
		}
	}

	for _, orphan := range existing {
		if err := queries.DeletePluginHookSchedule(ctx, orphan.ID); err != nil {
			return &PluginError{Kind: PluginErrorUnavailable, Message: "delete removed plugin hook schedule", Err: err}
		}
	}
	return nil
}

func pluginScheduleNextRun(schedule plugincontract.HookSchedule, after time.Time) (pgtype.Timestamptz, error) {
	next, err := NextOccurrenceAfterUTC(schedule.Cron, schedule.Timezone, after)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	if next.IsZero() {
		return pgtype.Timestamptz{}, nil
	}
	return pgtype.Timestamptz{Time: next.UTC(), Valid: true}, nil
}

func (s *PluginService) setPluginHookSchedulesEnabled(
	ctx context.Context,
	queries *db.Queries,
	installationID pgtype.UUID,
	enabled bool,
) error {
	if !enabled {
		if err := queries.DisablePluginHookSchedules(ctx, installationID); err != nil {
			return &PluginError{Kind: PluginErrorUnavailable, Message: "disable plugin hook schedules", Err: err}
		}
		return nil
	}

	rows, err := queries.ListPluginHookSchedulesByInstallation(ctx, installationID)
	if err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin hook schedules", Err: err}
	}
	now := time.Now().UTC()
	for _, row := range rows {
		nextRun, err := pluginScheduleNextRun(plugincontract.HookSchedule{
			Cron: row.CronExpression, Timezone: row.Timezone,
		}, now)
		if err != nil {
			return &PluginError{Kind: PluginErrorInvalid, Message: fmt.Sprintf("schedule for hook %q is invalid", row.HookKey), Err: err}
		}
		if _, err := queries.ReactivatePluginHookSchedule(ctx, db.ReactivatePluginHookScheduleParams{
			ID:        row.ID,
			NextRunAt: nextRun,
		}); err != nil {
			return &PluginError{Kind: PluginErrorUnavailable, Message: "reactivate plugin hook schedule", Err: err}
		}
	}
	return nil
}
