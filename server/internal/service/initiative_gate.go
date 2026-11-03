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

// GateRetryAfter is how long a rejected stage gate waits before the sweep files
// it again. A rejection carries a note for the leader; the leader gets that long
// to answer it before the same gate comes back.
const GateRetryAfter = 7 * 24 * time.Hour

// SweepResult counts what one stage sweep did.
type SweepResult struct {
	Advanced   int
	GatesFiled int
}

// SweepStages moves active initiatives on. An auto stage whose artifacts are all
// in place advances. A gate stage whose artifacts are all in place files its
// gate proposal, unless one is pending or the last one was rejected recently.
// An attest stage waits for the owning squad. A proposal the attention limits
// refuse is left for the next sweep.
func (s *InitiativeService) SweepStages(ctx context.Context, limit int32) (SweepResult, error) {
	list, err := s.Queries.ListActiveInitiatives(ctx, limit)
	if err != nil {
		return SweepResult{}, fmt.Errorf("list active initiatives: %w", err)
	}
	var out SweepResult
	var errs []error
	for _, ini := range list {
		advanced, filed, err := s.sweepOne(ctx, ini)
		if err != nil {
			slog.Warn("initiative stage sweep failed; retried next run",
				"initiative_id", util.UUIDToString(ini.ID), "error", err)
			errs = append(errs, err)
			continue
		}
		if advanced {
			out.Advanced++
		}
		if filed {
			out.GatesFiled++
		}
	}
	return out, errors.Join(errs...)
}

func (s *InitiativeService) sweepOne(ctx context.Context, ini db.Initiative) (advanced, filed bool, err error) {
	pipe, stages, err := s.pipelineOf(ctx, s.Queries, ini)
	if err != nil {
		return false, false, err
	}
	idx := stageIndex(stages, ini.StageKey)
	if idx < 0 {
		return false, false, ErrUnknownStage
	}
	missing, err := s.missingRequired(ctx, s.Queries, ini)
	if err != nil {
		return false, false, err
	}
	if len(missing) > 0 {
		return false, false, nil
	}
	switch stages[idx].Exit.Mode {
	case PipelineExitAuto:
		err := s.inTx(ctx, func(tx pgx.Tx, q *db.Queries) error {
			locked, err := s.lockInitiative(ctx, q, ini.WorkspaceID, ini.ID)
			if err != nil {
				return err
			}
			if locked.StageKey != ini.StageKey || live(locked) != nil {
				return nil
			}
			_, err = s.advanceLocked(ctx, q, locked, pipe, stages, InitiativeActor{Type: "system"},
				"Every required artifact of the stage is published.", pgtype.UUID{}, time.Now())
			advanced = err == nil
			return err
		})
		return advanced, false, err
	case PipelineExitGate:
		ready, err := s.gateIsDue(ctx, ini, stages[idx].Key)
		if err != nil || !ready {
			return false, false, err
		}
		next := ""
		if idx+1 < len(stages) {
			next = stages[idx+1].Key
		}
		g := proposalkinds.Gate{InitiativeID: util.UUIDToString(ini.ID), From: stages[idx].Key, To: next}
		if err := s.fileGate(ctx, ini, g, stages[idx].Name); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	return false, false, nil
}

// gateIsDue reports whether no gate for the stage is pending, and the last one
// was not rejected within GateRetryAfter. A gate that expired unanswered is due
// again.
func (s *InitiativeService) gateIsDue(ctx context.Context, ini db.Initiative, stageKey string) (bool, error) {
	gates, err := s.Queries.ListGateProposalsForInitiative(ctx, db.ListGateProposalsForInitiativeParams{
		WorkspaceID: ini.WorkspaceID, InitiativeID: util.UUIDToString(ini.ID),
	})
	if err != nil {
		return false, fmt.Errorf("list gate proposals: %w", err)
	}
	var last *db.Proposal
	for i := range gates {
		g, err := decodeGatePayload(gates[i].Proposal.Payload)
		if err != nil || g.GateDay > 0 || g.From != stageKey {
			continue
		}
		p := gates[i].Proposal
		if p.State == ProposalStatePending {
			return false, nil
		}
		last = &p
	}
	if last == nil {
		return true, nil
	}
	switch last.State {
	case ProposalStateApproved:
		return false, nil
	case ProposalStateRejected:
		return last.DecidedAt.Valid && time.Since(last.DecidedAt.Time) >= GateRetryAfter, nil
	}
	return true, nil
}

func decodeGatePayload(raw []byte) (proposalkinds.Gate, error) {
	var g proposalkinds.Gate
	err := json.Unmarshal(raw, &g)
	return g, err
}

// fileGate files a stage_gate proposal for the initiative's owning squad. The
// squad's leader is the filer, because the gate is the leader's ask of the boss.
func (s *InitiativeService) fileGate(ctx context.Context, ini db.Initiative, g proposalkinds.Gate, stageName string) error {
	if !ini.OwnerSquadID.Valid {
		return fmt.Errorf("initiative INI-%d has no owning squad to file its gate", ini.Seq)
	}
	squad, err := liveSquad(ctx, s.Queries, ini.WorkspaceID, ini.OwnerSquadID)
	if err != nil {
		return fmt.Errorf("owning squad: %w", err)
	}
	payload, err := json.Marshal(g)
	if err != nil {
		return err
	}
	title, summary, recommendation, ask := gateText(ini, g, stageName)
	_, err = s.Proposals.Create(ctx, ProposalCreateParams{
		WorkspaceID: ini.WorkspaceID,
		SquadID:     squad.ID,
		Content: ProposalContent{
			Title:          title,
			Summary:        summary,
			Evidence:       []string{fmt.Sprintf("Initiative INI-%d, stage %s; its artifacts are listed on the initiative.", ini.Seq, stageName)},
			Recommendation: recommendation,
			Options:        gateOptions(g),
			CostRisk:       "The gate itself costs nothing. The stage's artifacts state any spend.",
			Ask:            ask,
		},
		CreatorType: "agent",
		CreatorID:   squad.LeaderID,
		Kind:        proposalkinds.KeyStageGate,
		Payload:     payload,
		SystemFiled: true,
	})
	return err
}

func gateText(ini db.Initiative, g proposalkinds.Gate, stageName string) (title, summary, recommendation, ask string) {
	if g.GateDay > 0 {
		title = fmt.Sprintf("Review at %d days: INI-%d %s", g.GateDay, ini.Seq, ini.Title)
		summary = fmt.Sprintf("The initiative has been live for %d days. Decide whether it continues, scales or is sunset.", g.GateDay)
		recommendation = "Continue while the weekly metrics hold; scale or sunset on the evidence in them."
		ask = "Choose continue, scale or sunset."
		return
	}
	title = fmt.Sprintf("Gate: INI-%d %s, %s to %s", ini.Seq, ini.Title, stageName, g.To)
	summary = fmt.Sprintf("The %s stage has its required artifacts. Decide whether the initiative moves on.", stageName)
	recommendation = "Move on to the next stage."
	ask = "Approve to advance the initiative, or reject with a note for the squad."
	return
}

func gateOptions(g proposalkinds.Gate) []string {
	if g.GateDay > 0 {
		return []string{"Continue: keep operating and review again at the next gate.", "Scale: start a follow-on initiative at Define; this one keeps operating.", "Sunset: close the initiative and run its retrospective."}
	}
	return []string{"Approve: advance to the next stage.", "Reject: keep the initiative here, with your note to the squad."}
}

// fileDueOperateGates files the review gates of operating initiatives that have
// reached a review day. A day counts as filed once any gate for it exists, so a
// rejected or expired review is not filed again.
func (s *InitiativeService) fileDueOperateGates(ctx context.Context, limit int32, now time.Time) (int, error) {
	list, err := s.Queries.ListOperatingInitiatives(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("list operating initiatives: %w", err)
	}
	filed := 0
	var errs []error
	for _, ini := range list {
		gates, err := s.Queries.ListGateProposalsForInitiative(ctx, db.ListGateProposalsForInitiativeParams{
			WorkspaceID: ini.WorkspaceID, InitiativeID: util.UUIDToString(ini.ID),
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		seen := map[int]bool{}
		for _, row := range gates {
			if g, err := decodeGatePayload(row.Proposal.Payload); err == nil && g.GateDay > 0 {
				seen[g.GateDay] = true
			}
		}
		for _, day := range ini.GateDays {
			if seen[int(day)] || !ini.OperatingSince.Valid {
				continue
			}
			if now.Before(ini.OperatingSince.Time.Add(time.Duration(day) * 24 * time.Hour)) {
				continue
			}
			g := proposalkinds.Gate{
				InitiativeID: util.UUIDToString(ini.ID), From: StageOperate, To: StageOperate, GateDay: int(day),
			}
			if err := s.fileGate(ctx, ini, g, "Operate"); err != nil {
				errs = append(errs, fmt.Errorf("file review at %d days: %w", day, err))
				continue
			}
			filed++
		}
	}
	return filed, errors.Join(errs...)
}
