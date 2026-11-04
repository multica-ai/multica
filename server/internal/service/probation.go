package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Probation (spec 4.6). Each applied change that has a probation window is
// judged once it ends. The rule, by change type:
//
//   - Most types (instruction, skill, charter, autonomy, move, merge, unretire,
//     unmerge): the subject's success rate in the week before the change, the
//     before figure, is compared with the newest completed week after the window
//     opened, the after figure. Both are the subject's own scorecard.
//   - hire: the before figure is the joining squad's scorecard for the week before
//     the hire. The after figure is the new agent's scorecard at the window's end.
//   - retire (agent or squad): the before figure is the scorecard of the squad the
//     retired agent belonged to (or the retired squad itself) for the week before
//     the retire. The after figure is that squad's scorecard at the window's end.
//     A retired squad has no later scorecards, so a squad retire defers.
//
// The after week must end after the before week, or it is the same figure again.
// Improved, neutral or worse is stored. Worse also files a rollback proposal: a
// hire is undone by retiring the agent it created, a retire by unretiring, and the
// other types by their own revert payloads. A window whose figures are missing
// stays open and is judged on a later night.

// ProbationService judges the windows that have ended.
type ProbationService struct {
	Queries    *db.Queries
	Changes    *OrgChangeService
	Scorecards *ScorecardService
	// Proposals files the rollback proposal. Nil leaves a worse verdict without
	// one, and the verdict still stands.
	Proposals *ProposalService
	Now       func() time.Time
}

// probationBatch bounds the windows judged in one run.
const probationBatch int32 = 200

func (p *ProbationService) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}
	return p.Now()
}

// CheckDue judges every window that has ended and has no outcome. It returns
// how many outcomes it stored. A failure on one window does not stop the others.
func (p *ProbationService) CheckDue(ctx context.Context) (int, error) {
	now := p.now()
	due, err := p.Queries.ListDueOrgChangeProbations(ctx, db.ListDueOrgChangeProbationsParams{
		Now: pgtype.Timestamptz{Time: now, Valid: true}, RowLimit: probationBatch,
	})
	if err != nil {
		return 0, fmt.Errorf("list due probations: %w", err)
	}
	decided := 0
	var errs []error
	for _, app := range due {
		ok, err := p.judge(ctx, app, now)
		if err != nil {
			slog.Warn("probation: window failed", "application_id", util.UUIDToString(app.ID), "error", err)
			errs = append(errs, fmt.Errorf("application %s: %w", util.UUIDToString(app.ID), err))
			continue
		}
		if ok {
			decided++
		}
	}
	// A failed window stays open and is retried on the next run; the run reports
	// the failure so the job shows it.
	return decided, errors.Join(errs...)
}

// judge decides one window. It returns false when the figures are not there yet.
func (p *ProbationService) judge(ctx context.Context, app db.OrgChangeApplication, now time.Time) (bool, error) {
	ws := app.WorkspaceID
	if !app.BeforeScorecardID.Valid {
		return false, nil
	}
	before, err := p.Queries.GetScorecardInWorkspace(ctx, db.GetScorecardInWorkspaceParams{ID: app.BeforeScorecardID, WorkspaceID: ws})
	if err != nil {
		return false, fmt.Errorf("load before scorecard: %w", err)
	}
	afterKind, afterID, ok := probationAfterSubject(app)
	if !ok {
		return false, nil
	}
	after, err := p.Scorecards.LatestOKAsOf(ctx, p.Queries, ws, afterKind, afterID, ScorecardPeriodWeek, now)
	if errors.Is(err, ErrScorecardNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// The after figure must come from a week that ended after the before week,
	// or it is the same figure again.
	if !after.PeriodEnd.Time.After(before.PeriodEnd.Time) {
		return false, nil
	}
	bdoc, err := DecodeScorecardDocument(before.Metrics)
	if err != nil {
		return false, err
	}
	adoc, err := DecodeScorecardDocument(after.Metrics)
	if err != nil {
		return false, err
	}
	outcome, delta := ProbationOutcome(bdoc.Metrics[MetricSuccessRate], adoc.Metrics[MetricSuccessRate])
	if outcome == "" {
		return false, nil
	}
	note := fmt.Sprintf("Success rate went from %s to %s (%+.2f) between the week before the change and the week ending %s.",
		percentText(bdoc.Metrics[MetricSuccessRate]), percentText(adoc.Metrics[MetricSuccessRate]), delta,
		after.PeriodEnd.Time.Format("2006-01-02"))

	_, err = p.Queries.SetOrgChangeProbationOutcome(ctx, db.SetOrgChangeProbationOutcomeParams{
		ID:               app.ID,
		ProbationOutcome: pgtype.Text{String: outcome, Valid: true},
		ProbationNote:    note,
		AfterScorecardID: after.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // decided by a concurrent run
	}
	if err != nil {
		return false, fmt.Errorf("record probation outcome: %w", err)
	}
	if outcome == ProbationWorse {
		p.fileRollback(ctx, app)
	}
	return true, nil
}

// fileRollback files the change that undoes a change judged worse. It needs a
// reviewer to file it; without one the verdict stands and nothing is filed.
func (p *ProbationService) fileRollback(ctx context.Context, app db.OrgChangeApplication) {
	if p.Proposals == nil {
		return
	}
	ws := app.WorkspaceID
	payload, err := rollbackPayload(ctx, p.Queries, app)
	if err != nil {
		slog.Warn("probation: no rollback payload", "application_id", util.UUIDToString(app.ID), "error", err)
		return
	}
	reviewer, err := p.Queries.GetReviewerAgent(ctx, ws)
	if err != nil {
		slog.Info("probation: no reviewer to file the rollback", "application_id", util.UUIDToString(app.ID))
		return
	}
	squad, err := p.Queries.GetReviewerSquad(ctx, ws)
	if err != nil {
		slog.Info("probation: reviewer has no squad to file from", "application_id", util.UUIDToString(app.ID))
		return
	}
	res, err := p.Proposals.Create(ctx, ProposalCreateParams{
		WorkspaceID: ws,
		SquadID:     squad.ID,
		Content: ProposalContent{
			Title:          "Roll back: " + proposalkinds.OrgChangeTypeLabel(app.ChangeType),
			Summary:        "The probation check judged the earlier change worse. This proposal undoes it.",
			Evidence:       []string{"Probation outcome of application " + util.UUIDToString(app.ID)},
			Recommendation: "Approve to restore what the earlier change replaced.",
			Options:        []string{"Approve the rollback", "Reject and keep the change"},
			CostRisk:       "None beyond the change itself.",
			Ask:            "Approve the rollback?",
		},
		CreatorType: "agent",
		CreatorID:   reviewer.ID,
		Kind:        proposalkinds.KeyOrgChange,
		Payload:     payload,
	})
	if err != nil {
		slog.Warn("probation: could not file the rollback", "application_id", util.UUIDToString(app.ID), "error", err)
		return
	}
	if err := p.Queries.SetOrgChangeRollbackProposal(ctx, db.SetOrgChangeRollbackProposalParams{
		ID: app.ID, WorkspaceID: ws, RollbackProposalID: res.Proposal.ID,
	}); err != nil {
		slog.Warn("probation: rollback filed but not linked", "application_id", util.UUIDToString(app.ID), "error", err)
	}
}

// rollbackPayload is the org change that restores what an application replaced.
func rollbackPayload(ctx context.Context, q *db.Queries, app db.OrgChangeApplication) ([]byte, error) {
	var before orgBefore
	if err := json.Unmarshal(app.BeforeState, &before); err != nil {
		return nil, err
	}
	const why = "Probation found the earlier change worse; this restores what it replaced."
	var out any
	switch app.ChangeType {
	case OrgChangeInstruction:
		out = instructionChangePayload{Type: OrgChangeInstruction, AgentID: util.UUIDToString(app.SubjectID),
			Instructions: before.Instructions, DiffSummary: "Restores the instructions the change replaced.", Rationale: why}
	case OrgChangeSkill:
		out = skillChangePayload{Type: OrgChangeSkill, AgentID: util.UUIDToString(app.SubjectID),
			AddSkillIDs: nonNilStrings(before.RemovedSkillIDs), RemoveSkillIDs: nonNilStrings(before.AddedSkillIDs), Rationale: why}
	case OrgChangeCharter:
		if before.Charter == nil {
			return nil, errors.New("no earlier charter to restore")
		}
		rev, err := charterRevision(ctx, q, app.WorkspaceID, app.SubjectID)
		if err != nil {
			return nil, err
		}
		out = charterChangePayload{Type: OrgChangeCharter, SquadID: util.UUIDToString(app.SubjectID),
			BaseRevision: rev, Charter: *before.Charter, Rationale: why}
	case OrgChangeAutonomy:
		var after autonomyChangePayload
		if err := json.Unmarshal(app.AfterState, &after); err != nil {
			return nil, err
		}
		if before.Level == nil {
			return nil, errors.New("no earlier autonomy level to restore")
		}
		out = autonomyChangePayload{Type: OrgChangeAutonomy, SquadID: util.UUIDToString(app.SubjectID),
			From: after.To, To: *before.Level, Rationale: why}
	case OrgChangeMove:
		if before.HomeSquadID == "" {
			return nil, errors.New("the agent had no earlier squad")
		}
		out = movePayload{Type: OrgChangeMove, AgentID: util.UUIDToString(app.SubjectID), ToSquadID: before.HomeSquadID, Rationale: why}
	case OrgChangeHire:
		// A hire is undone by retiring the agent it created.
		agentID, squadID := orgSubjectFields(app)
		out = retirePayload{Type: OrgChangeRetire, AgentID: agentID, SquadID: squadID, Rationale: why}
	case OrgChangeRetire:
		agentID, squadID := orgSubjectFields(app)
		out = unretirePayload{Type: OrgChangeUnretire, AgentID: agentID, SquadID: squadID, Rationale: why}
	case OrgChangeUnretire:
		agentID, squadID := orgSubjectFields(app)
		out = retirePayload{Type: OrgChangeRetire, AgentID: agentID, SquadID: squadID, Rationale: why}
	case OrgChangeMerge:
		// The merge is undone by unmerging it, which reads the snapshot the merge kept.
		out = unmergePayload{Type: OrgChangeUnmerge, ApplicationID: util.UUIDToString(app.ID), Rationale: why}
	case OrgChangeUnmerge:
		// The unmerge is undone by merging the same two squads again.
		var undone unmergePayload
		if err := json.Unmarshal(app.AfterState, &undone); err != nil {
			return nil, err
		}
		merge, err := q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{
			ID: parseUUIDMust(undone.ApplicationID), WorkspaceID: app.WorkspaceID,
		})
		if err != nil {
			return nil, fmt.Errorf("load the merge being restored: %w", err)
		}
		var merged mergePayload
		if err := json.Unmarshal(merge.AfterState, &merged); err != nil {
			return nil, err
		}
		rev, err := charterRevision(ctx, q, app.WorkspaceID, parseUUIDMust(merged.IntoSquadID))
		if err != nil {
			return nil, err
		}
		out = mergePayload{Type: OrgChangeMerge, FromSquadID: merged.FromSquadID, IntoSquadID: merged.IntoSquadID,
			BaseRevision: rev, Rationale: why}
	default:
		return nil, fmt.Errorf("%s has no rollback", app.ChangeType)
	}
	return json.Marshal(out)
}

// orgSubjectFields names an application's subject in the payload fields a retire
// or unretire carries: the agent, or the squad.
func orgSubjectFields(app db.OrgChangeApplication) (agentID, squadID string) {
	if app.SubjectType == OrgSubjectSquad {
		return "", util.UUIDToString(app.SubjectID)
	}
	return util.UUIDToString(app.SubjectID), ""
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// percentText renders a rate for a note, or "no data" when there is none.
func percentText(m ScorecardMetric) string {
	if m.Value == nil {
		return "no data"
	}
	return fmt.Sprintf("%.0f%%", *m.Value*100)
}

// probationAfterSubject is the subject whose later scorecard is the after figure.
// For a retire that is the squad the retired agent belonged to, which is recorded
// on the application; it reports false when no squad was recorded.
func probationAfterSubject(app db.OrgChangeApplication) (string, pgtype.UUID, bool) {
	if app.ChangeType != OrgChangeRetire {
		return app.SubjectType, app.SubjectID, true
	}
	var before orgBefore
	if err := json.Unmarshal(app.BeforeState, &before); err != nil {
		return "", pgtype.UUID{}, false
	}
	id := probationSquadUUID(before.ProbationSquadID)
	return ScorecardSubjectSquad, id, id.Valid
}

// probationSquadUUID parses a squad id recorded on an application. An empty or
// malformed id is not a squad, so it is invalid.
func probationSquadUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
