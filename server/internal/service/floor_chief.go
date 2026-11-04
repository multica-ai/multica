package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Floor chiefs (spec 4.8). A floor's chief is an agent that publishes the floor's
// daily brief. Naming the chief creates that brief's routine: one autopilot,
// assigned to the chief, recorded on the floor_chief row. Naming the same chief
// again finds the routine and creates nothing; naming another chief pauses the
// previous routine and creates the new one. A chief named again after a clear
// gets its old routine back, not a second one.

// FloorBriefCron runs a floor's brief at 07:00, an hour before the Secretary's
// daily brief reads the floor briefs.
const FloorBriefCron = "0 7 * * *"

// FloorBriefTitlePrefix starts the title of a floor brief routine.
const FloorBriefTitlePrefix = "Daily floor brief: "

// FloorChiefInput names a floor's chief.
type FloorChiefInput struct {
	WorkspaceID pgtype.UUID
	FloorID     pgtype.UUID
	AgentID     pgtype.UUID
	// ActorUserID is the member who names the chief. It is the creator of the
	// routine and sets its timezone when Timezone is empty.
	ActorUserID pgtype.UUID
	// Timezone is a valid IANA name, or empty for the actor's timezone and then UTC.
	Timezone string
	// RoutineStatus is active when the boss names the chief. An import recreates
	// the routine paused, as it recreates every routine.
	RoutineStatus string
}

// FloorBriefDescription is the run prompt of a floor's brief routine. It names
// the floor, so the chief reads that floor's digest and publishes to it.
func FloorBriefDescription(floorID pgtype.UUID, floorName string) string {
	id := util.UUIDToString(floorID)
	return fmt.Sprintf(`Daily floor brief for %s, the floor you chair. The routine starts this run every morning; the boss can also start it by hand.

Scope: this floor only. Start with `+"`multica brief digest --floor %s --since 24h --json`"+`, and open only the issues the digest flags. Write the brief to a file in the order of your instructions, then publish it with `+"`multica brief publish --kind daily --scope floor --floor %s --title \"Floor brief\" --file <path>`"+`. If nothing on the floor needs the boss, publish a short brief that says so.

Language: write in the language of the boss's last message to you. With no message from the boss, write in English.

Do not change any issue, proposal, or agent. Ask a leader only through a comment on a flagged issue, at most three per brief.`, floorName, id, id)
}

// SetFloorChiefIn names the chief of a floor inside q's transaction and makes the
// chief's floor brief routine, once. It returns the floor_chief row.
func SetFloorChiefIn(ctx context.Context, q *db.Queries, in FloorChiefInput) (db.FloorChief, error) {
	floor, err := q.GetOfficeFloorByID(ctx, db.GetOfficeFloorByIDParams{ID: in.FloorID, WorkspaceID: in.WorkspaceID})
	if err != nil {
		return db.FloorChief{}, fmt.Errorf("read floor: %w", err)
	}
	prev, err := q.GetFloorChief(ctx, db.GetFloorChiefParams{WorkspaceID: in.WorkspaceID, FloorID: in.FloorID})
	hasPrev := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.FloorChief{}, fmt.Errorf("read floor chief: %w", err)
	}
	if hasPrev && prev.AgentID != in.AgentID && prev.BriefAutopilotID.Valid {
		if err := pauseFloorBriefRoutine(ctx, q, prev.BriefAutopilotID); err != nil {
			return db.FloorChief{}, err
		}
	}
	row, err := q.UpsertFloorChief(ctx, db.UpsertFloorChiefParams{
		WorkspaceID: in.WorkspaceID, FloorID: in.FloorID, AgentID: in.AgentID, CreatedBy: in.ActorUserID,
	})
	if err != nil {
		return db.FloorChief{}, fmt.Errorf("set floor chief: %w", err)
	}
	if row.BriefAutopilotID.Valid {
		return row, nil
	}
	// A chief named again after a clear: the routine the clear paused is still
	// there, and it is found rather than duplicated.
	existing, found, err := findFloorBriefRoutine(ctx, q, in.WorkspaceID, in.AgentID, floor.Name)
	if err != nil {
		return db.FloorChief{}, err
	}
	if found {
		if existing.Status == "paused" && in.RoutineStatus == "active" {
			if _, err := q.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
				ID: existing.ID, Status: pgtype.Text{String: "active", Valid: true},
			}); err != nil {
				return db.FloorChief{}, fmt.Errorf("resume the floor brief routine: %w", err)
			}
		}
		row, err = q.SetFloorChiefBriefAutopilot(ctx, db.SetFloorChiefBriefAutopilotParams{
			WorkspaceID: in.WorkspaceID, FloorID: in.FloorID, BriefAutopilotID: existing.ID,
		})
		if err != nil {
			return db.FloorChief{}, fmt.Errorf("record floor brief routine: %w", err)
		}
		return row, nil
	}

	timezone, err := resolveBriefTimezone(ctx, q, OfficeSetupInput{Timezone: in.Timezone, Actor: db.Member{UserID: in.ActorUserID}})
	if err != nil {
		return db.FloorChief{}, err
	}
	next, err := ComputeNextRun(FloorBriefCron, timezone)
	if err != nil {
		return db.FloorChief{}, fmt.Errorf("floor brief schedule: %w", err)
	}
	ap, err := CreateAgentScheduledAutopilot(ctx, q, AgentScheduledAutopilot{
		WorkspaceID: in.WorkspaceID, AgentID: in.AgentID, ActorUserID: in.ActorUserID,
		Title:       FloorBriefTitlePrefix + floor.Name,
		Description: pgtype.Text{String: FloorBriefDescription(in.FloorID, floor.Name), Valid: true},
		Status:      in.RoutineStatus, Cron: FloorBriefCron, Timezone: timezone, NextRun: next,
	})
	if err != nil {
		return db.FloorChief{}, fmt.Errorf("floor brief routine: %w", err)
	}
	row, err = q.SetFloorChiefBriefAutopilot(ctx, db.SetFloorChiefBriefAutopilotParams{
		WorkspaceID: in.WorkspaceID, FloorID: in.FloorID, BriefAutopilotID: ap.ID,
	})
	if err != nil {
		return db.FloorChief{}, fmt.Errorf("record floor brief routine: %w", err)
	}
	return row, nil
}

// ClearFloorChiefIn removes a floor's chief inside q's transaction. The chief's
// floor brief routine is paused, not deleted, so the boss can resume it.
func ClearFloorChiefIn(ctx context.Context, q *db.Queries, ws, floorID pgtype.UUID) error {
	prev, err := q.GetFloorChief(ctx, db.GetFloorChiefParams{WorkspaceID: ws, FloorID: floorID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read floor chief: %w", err)
	}
	if prev.BriefAutopilotID.Valid {
		if err := pauseFloorBriefRoutine(ctx, q, prev.BriefAutopilotID); err != nil {
			return err
		}
	}
	return q.DeleteFloorChief(ctx, db.DeleteFloorChiefParams{WorkspaceID: ws, FloorID: floorID})
}

// findFloorBriefRoutine returns the floor brief routine of an agent for a floor
// name, if one exists and is not archived.
func findFloorBriefRoutine(ctx context.Context, q *db.Queries, ws, agentID pgtype.UUID, floorName string) (db.Autopilot, bool, error) {
	rows, err := q.ListAutopilots(ctx, db.ListAutopilotsParams{WorkspaceID: ws})
	if err != nil {
		return db.Autopilot{}, false, fmt.Errorf("list routines: %w", err)
	}
	title := FloorBriefTitlePrefix + floorName
	for _, row := range rows {
		ap := row.Autopilot
		if ap.AssigneeType == "agent" && ap.AssigneeID == agentID && ap.Title == title && ap.Status != "archived" {
			return ap, true, nil
		}
	}
	return db.Autopilot{}, false, nil
}

func pauseFloorBriefRoutine(ctx context.Context, q *db.Queries, id pgtype.UUID) error {
	if _, err := q.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
		ID: id, Status: pgtype.Text{String: "paused", Valid: true},
	}); err != nil {
		return fmt.Errorf("pause the previous chief's floor brief routine: %w", err)
	}
	return nil
}

// AgentScheduledAutopilot is a routine assigned to an agent that runs on a schedule.
type AgentScheduledAutopilot struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	ActorUserID pgtype.UUID
	Title       string
	Description pgtype.Text
	Status      string
	Cron        string
	Timezone    string
	NextRun     time.Time
}

// CreateAgentScheduledAutopilot creates a schedule-triggered autopilot assigned to
// an agent: the autopilot, its first rule version, and its schedule trigger. It is
// the agent counterpart of CreateSquadScheduledAutopilot. The agent runs the task
// itself (run_only), as the Secretary's brief routines do.
func CreateAgentScheduledAutopilot(ctx context.Context, q *db.Queries, in AgentScheduledAutopilot) (db.Autopilot, error) {
	ap, err := q.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   in.WorkspaceID,
		Title:         in.Title,
		AssigneeType:  "agent",
		AssigneeID:    in.AgentID,
		Status:        in.Status,
		ExecutionMode: "run_only",
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
