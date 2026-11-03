package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// OperatingReviewTitle names the weekly review autopilot.
	OperatingReviewTitle = "Operating review"
	// OperatingReviewPurpose is the purpose of the review's routine. Routines
	// cannot change their purpose, so it marks the review; creating another
	// routine with this purpose is refused.
	OperatingReviewPurpose = "Operating review"
	// OperatingReviewCadence is Mondays at 09:00 in the saving member's timezone.
	OperatingReviewCadence = "0 9 * * 1"
)

type operatingReviewResponsibility struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type operatingReviewOutput struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	KBKey       string `json:"kb_key"`
	CadenceHint string `json:"cadence_hint"`
}

// OperatingReviewDescription is the work instruction the weekly review runs with.
// It carries the charter as saved when the review was created. Later charter edits
// do not rewrite it: the leader's briefing carries the current charter, and the
// description says so.
func OperatingReviewDescription(charter db.SquadCharter) (string, error) {
	var responsibilities []operatingReviewResponsibility
	if err := json.Unmarshal(charter.Responsibilities, &responsibilities); err != nil {
		return "", fmt.Errorf("decode charter responsibilities: %w", err)
	}
	var outputs []operatingReviewOutput
	if err := json.Unmarshal(charter.Outputs, &outputs); err != nil {
		return "", fmt.Errorf("decode charter outputs: %w", err)
	}

	var b strings.Builder
	b.WriteString("This is the squad's weekly operating review. Work through the steps below and record the result.\n\n")
	b.WriteString("The charter below is the one saved when this review was created. Your briefing carries the current charter; where the two differ, follow the briefing.\n\n")
	b.WriteString("Charter\n")
	if charter.Mission != "" {
		b.WriteString("Mission: " + charter.Mission + "\n")
	}
	if len(responsibilities) > 0 {
		b.WriteString("Responsibilities:\n")
		for _, r := range responsibilities {
			b.WriteString("- " + r.Text + "\n")
		}
	}
	if len(outputs) > 0 {
		b.WriteString("Outputs:\n")
		for _, o := range outputs {
			line := "- " + o.Key + " (" + o.Kind
			if o.CadenceHint != "" {
				line += ", " + o.CadenceHint
			}
			b.WriteString(line + ")\n")
		}
	}
	if charter.Boundaries != "" {
		b.WriteString("Boundaries: " + charter.Boundaries + "\n")
	}
	if charter.Escalation != "" {
		b.WriteString("Escalation: " + charter.Escalation + "\n")
	}
	// Goals are recorded once phase 4 adds them; until then the line says so.
	b.WriteString("\nGoals: none recorded yet\n\n")

	b.WriteString("Steps\n")
	b.WriteString("1. Compare last week's outputs with the charter; list what was late or missing.\n")
	b.WriteString("2. Search the company knowledge for prior art, decisions and stale documents in the squad's area.\n")
	b.WriteString("3. Name at most one thing worth doing that no one asked for, with evidence.\n")
	b.WriteString("4. File it as a proposal (the attention limits apply) or record `no action` with the reason. Padding is a failure; an honest `no action` is a success.\n")
	b.WriteString("5. Draft updates for stale knowledge-base documents (drafts only).\n")
	return b.String(), nil
}

// routineDefaultGraceHours is the grace period a routine gets when a request
// does not name one. It matches the handler's default.
const routineDefaultGraceHours = 24

// CreateOperatingReview makes a squad's weekly operating review and its routine.
// The first charter save calls it (the handler and the starter company both
// reach it), so the review is created the same way on both paths. It starts
// paused when the charter's switch is off. The caller runs it in its transaction.
func CreateOperatingReview(ctx context.Context, q *db.Queries, squad db.Squad, charter db.SquadCharter, actorUserID pgtype.UUID, timezone string) (db.Autopilot, error) {
	nextRun, err := ComputeNextRun(OperatingReviewCadence, timezone)
	if err != nil {
		return db.Autopilot{}, err
	}
	description, err := OperatingReviewDescription(charter)
	if err != nil {
		return db.Autopilot{}, err
	}
	status := "active"
	if !charter.OperatingReviewEnabled {
		status = "paused"
	}
	ap, err := CreateSquadScheduledAutopilot(ctx, q, SquadScheduledAutopilot{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
		ActorUserID: actorUserID,
		Title:       OperatingReviewTitle,
		Description: pgtype.Text{String: description, Valid: true},
		Status:      status,
		Cron:        OperatingReviewCadence,
		Timezone:    timezone,
		NextRun:     nextRun,
	})
	if err != nil {
		return db.Autopilot{}, err
	}
	if _, err := q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID:    ap.ID,
		WorkspaceID:    squad.WorkspaceID,
		SquadID:        squad.ID,
		Purpose:        OperatingReviewPurpose,
		ExpectedOutput: "report",
		Required:       true,
		GraceHours:     routineDefaultGraceHours,
	}); err != nil {
		return db.Autopilot{}, err
	}
	return ap, nil
}
