package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The weekly metrics routine a launch starts (spec 3.6): 09:00 on Mondays, in
// UTC, through the squad routine path the charter routines use.
const (
	metricsRoutineCron     = "0 9 * * 1"
	metricsRoutineTimezone = "UTC"
	metricsRoutineGrace    = 24
	metricsRoutineOutput   = "report"
)

// startMetricsRoutine creates the weekly metrics_report routine for the squad that
// owns the operate stage. It is the same work as POST /api/squads/{id}/routines
// with a cadence: a squad-assigned create_issue autopilot with its schedule, then
// the routine link. It runs inside the launch transaction, so the routine exists
// exactly when the initiative is operating.
func (s *InitiativeService) startMetricsRoutine(ctx context.Context, q *db.Queries, ini db.Initiative, owner pgtype.UUID, actor InitiativeActor, now time.Time) error {
	if !owner.Valid {
		return fmt.Errorf("%w: operate needs an owning squad for its weekly metrics report", ErrStageOwnerRequired)
	}
	next, err := ComputeNextRun(metricsRoutineCron, metricsRoutineTimezone)
	if err != nil {
		return fmt.Errorf("metrics routine schedule: %w", err)
	}
	// The routine belongs to the member who launched the initiative; agents do not
	// own autopilots, so the sponsor stands in when an agent made the change.
	owningUser := actor.ID
	if actor.Type != "member" {
		owningUser = ini.SponsorUserID
	}
	if !owningUser.Valid {
		return fmt.Errorf("the metrics routine needs a member to own it")
	}
	title := clipRunes(fmt.Sprintf("Weekly metrics: INI-%d %s", ini.Seq, ini.Title), routinePurposeMax)
	ap, err := CreateSquadScheduledAutopilot(ctx, q, SquadScheduledAutopilot{
		WorkspaceID: ini.WorkspaceID,
		SquadID:     owner,
		ActorUserID: owningUser,
		Title:       title,
		Description: pgtype.Text{String: fmt.Sprintf(
			"Write this week's metrics report for initiative INI-%d (kind metrics_report): the key numbers against the targets in its plan, the change since last week, and what the numbers do not show.",
			ini.Seq), Valid: true},
		Status:   "active",
		Cron:     metricsRoutineCron,
		Timezone: metricsRoutineTimezone,
		NextRun:  next,
	})
	if err != nil {
		return fmt.Errorf("create metrics autopilot: %w", err)
	}
	if _, err := q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID:    ap.ID,
		WorkspaceID:    ini.WorkspaceID,
		SquadID:        owner,
		Purpose:        title,
		ExpectedOutput: metricsRoutineOutput,
		Required:       true,
		GraceHours:     metricsRoutineGrace,
	}); err != nil {
		return fmt.Errorf("link metrics routine: %w", err)
	}
	return nil
}

// routinePurposeMax bounds a routine's purpose, as the routine handler does.
const routinePurposeMax = 200

func clipRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// RetroHook runs when an initiative closes, inside the closing transaction. It
// must write only through q: phase 4 (11.16) starts the retrospective routine
// here, and an external effect would survive a rolled-back close.
type RetroHook interface {
	OnInitiativeClosed(ctx context.Context, q *db.Queries, ini db.Initiative, reason string) error
}

// NoopRetroHook is the hook until phase 4 (11.16) provides the retrospective.
// Closing an initiative records it and does nothing else.
type NoopRetroHook struct{}

// OnInitiativeClosed does nothing.
func (NoopRetroHook) OnInitiativeClosed(context.Context, *db.Queries, db.Initiative, string) error {
	return nil
}

// applyOperateOutcome settles a review gate's outcome, inside the decision
// transaction. Continue keeps the initiative operating. Scale starts a follow-on
// at define and keeps this one operating. Sunset moves the initiative to learn
// and closes it, which runs the retrospective hook.
func (s *InitiativeService) applyOperateOutcome(ctx context.Context, q *db.Queries, ini db.Initiative, pipe db.Pipeline, stages []PipelineStage, d proposalkinds.Decision, actor InitiativeActor, now time.Time) error {
	if d.Outcome == "" {
		return ErrOutcomeRequired
	}
	if !proposalkinds.ValidOutcome(d.Outcome) {
		return ErrOutcomeInvalid
	}
	switch d.Outcome {
	case proposalkinds.OutcomeContinue:
		return s.recordEvent(ctx, q, ini, InitiativeEventAdvanced, StageOperate, StageOperate, actor, "Continue: the boss chose to keep operating.", d.Proposal.ID)
	case proposalkinds.OutcomeScale:
		if err := s.recordEvent(ctx, q, ini, InitiativeEventAdvanced, StageOperate, StageOperate, actor, "Scale: a follow-on initiative starts at Define.", d.Proposal.ID); err != nil {
			return err
		}
		_, err := s.openInTx(ctx, d.Tx, q, CreateInitiativeParams{
			WorkspaceID:      ini.WorkspaceID,
			PipelineID:       pipe.ID,
			StageKey:         StageDefine,
			Title:            ini.Title + " (scaled)",
			Thesis:           ini.Thesis,
			SourceProposalID: d.Proposal.ID,
			SponsorUserID:    d.DeciderID,
			Actor:            actor,
		})
		return err
	case proposalkinds.OutcomeSunset:
		learned, err := s.moveTo(ctx, q, ini, pipe, stages, StageLearn, InitiativeEventAdvanced, actor, "Sunset: the boss chose to end the initiative.", d.Proposal.ID, now)
		if err != nil {
			return err
		}
		_, err = s.closeLocked(ctx, q, learned, pipe, stages, actor, "sunset", "Sunset by the boss at a review gate.", now)
		return err
	}
	return ErrOutcomeInvalid
}

// ScheduleOperate runs the scheduled work of operating initiatives: it files the
// review gates whose day has come. It is called by the daily scheduler job.
func (s *InitiativeService) ScheduleOperate(ctx context.Context, limit int32) (int, error) {
	return s.fileDueOperateGates(ctx, limit, time.Now())
}
