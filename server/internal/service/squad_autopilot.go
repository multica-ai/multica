package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SquadScheduledAutopilot describes an autopilot assigned to a squad that runs on
// a cron schedule. It is the shape the routine and operating review paths share.
type SquadScheduledAutopilot struct {
	WorkspaceID pgtype.UUID
	SquadID     pgtype.UUID
	ActorUserID pgtype.UUID
	Title       string
	Description pgtype.Text
	// Status is "active" or "paused".
	Status   string
	Cron     string
	Timezone string
	NextRun  time.Time
}

// CreateSquadScheduledAutopilot creates a create_issue autopilot assigned to the
// squad, its enabled schedule trigger, and the rule version the autopilot handlers
// record for a substantive publish. The caller runs it in its transaction, so the
// autopilot and its schedule commit together.
func CreateSquadScheduledAutopilot(ctx context.Context, q *db.Queries, in SquadScheduledAutopilot) (db.Autopilot, error) {
	ap, err := q.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   in.WorkspaceID,
		Title:         in.Title,
		AssigneeType:  "squad",
		AssigneeID:    in.SquadID,
		Status:        in.Status,
		ExecutionMode: "create_issue",
		CreatedByType: "member",
		CreatedByID:   in.ActorUserID,
		Description:   in.Description,
	})
	if err != nil {
		return db.Autopilot{}, fmt.Errorf("create autopilot: %w", err)
	}
	if err := RecordAutopilotRuleVersion(ctx, q, ap, "member", in.ActorUserID); err != nil {
		return db.Autopilot{}, fmt.Errorf("record rule version: %w", err)
	}
	if _, err := q.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:     ap.ID,
		Kind:            "schedule",
		Enabled:         true,
		CronExpression:  pgtype.Text{String: in.Cron, Valid: true},
		Timezone:        pgtype.Text{String: in.Timezone, Valid: true},
		NextRunAt:       pgtype.Timestamptz{Time: in.NextRun, Valid: true},
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   in.ActorUserID,
		CreatedByType:   pgtype.Text{String: "member", Valid: true},
		CreatedByID:     in.ActorUserID,
	}); err != nil {
		return db.Autopilot{}, fmt.Errorf("create schedule trigger: %w", err)
	}
	return ap, nil
}
