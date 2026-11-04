package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Autonomy levels (spec 4.3). A squad's level says how far the platform lets it
// act without the boss. Each cell of the enforcement table is one function below,
// so the table is what the tests check.
const (
	AutonomyLevelSuggest    = 0 // L0: routines held; runs only when assigned or mentioned
	AutonomyLevelWork       = 1 // L1: the default; routines run, every decision is the boss's
	AutonomyLevelTrusted    = 2 // L2: knowledge drafts may auto-publish after review; standing approvals apply
	AutonomyLevelAutonomous = 3 // L3: standing approvals may create initiatives at the first stage
)

// VerdictReasonAutonomyL0 is the verdict reason of a routine run that an L0 squad
// holds. It is the dispatch reason code of the same name.
const VerdictReasonAutonomyL0 = "autonomy_l0"

// AutonomyReasonMax bounds the reason a level change records, as the governance
// summary does.
const AutonomyReasonMax = 500

var (
	// ErrAutonomyLevelInvalid: the level is not 0 to 3.
	ErrAutonomyLevelInvalid = errors.New("autonomy level must be 0, 1, 2 or 3")
	// ErrAutonomyReasonRequired: a level change needs a reason the boss can read later.
	ErrAutonomyReasonRequired = errors.New("a reason is required to change a squad's autonomy level")
	// ErrAutonomySquadNotFound: no live squad of that id in the workspace.
	ErrAutonomySquadNotFound = errors.New("squad not found in this workspace")
	// ErrGovernanceActorNotBoss: the actor is not an owner or admin member. Agents
	// are never the boss for levels or policies.
	ErrGovernanceActorNotBoss = errors.New("only the owner or an admin can change this")
)

// AutonomyLevelName is the name the Governance tab and the squad panel show.
func AutonomyLevelName(level int) string {
	switch level {
	case AutonomyLevelSuggest:
		return "Suggest"
	case AutonomyLevelWork:
		return "Work"
	case AutonomyLevelTrusted:
		return "Trusted"
	case AutonomyLevelAutonomous:
		return "Autonomous"
	}
	return ""
}

// ValidAutonomyLevel reports whether level is one of the four levels.
func ValidAutonomyLevel(level int) bool {
	return level >= AutonomyLevelSuggest && level <= AutonomyLevelAutonomous
}

// Enforcement table, one function per cell.

// AutonomyRunVerdict is the L0 cell: a routine run of an L0 squad is held with
// the autonomy_l0 reason. Every other class, and every other level, is admitted
// here; the company controls and budgets decide those.
func AutonomyRunVerdict(level int, class RunClass) Verdict {
	if level == AutonomyLevelSuggest && class == RunClassRoutine {
		return Verdict{Reason: VerdictReasonAutonomyL0}
	}
	return Verdict{Admit: true}
}

// AutonomyAllowsAutoPublish is the L2 cell: a knowledge draft of the squad may
// publish through auto_after_review (spec 3.3).
func AutonomyAllowsAutoPublish(level int) bool {
	return level >= AutonomyLevelTrusted
}

// AutonomyAllowsStandingApproval is the L2 cell for standing approvals: a squad at
// L2 or above may have its proposals approved by a policy. An opportunity, which
// creates an initiative, needs L3 (the next cell).
func AutonomyAllowsStandingApproval(level int, kind string) bool {
	if kind == proposalkinds.KeyOpportunity {
		return AutonomyAllowsInitiativeFromOpportunity(level)
	}
	return level >= AutonomyLevelTrusted
}

// AutonomyAllowsInitiativeFromOpportunity is the L3 cell: a standing approval may
// create an initiative from the squad's own opportunity, at the first stage only
// (the first stage check is made by the policy engine).
func AutonomyAllowsInitiativeFromOpportunity(level int) bool {
	return level >= AutonomyLevelAutonomous
}

// AutonomyAllowsLaunch is the cell that never depends on level: launching is the
// boss's decision at every level. The Launch gate is a stage_gate, which no
// standing approval may decide.
func AutonomyAllowsLaunch(int) bool { return false }

// AutonomyAllowsOrgChange is the cell that never depends on level: organisation
// changes, autonomy changes among them, are the boss's at every level.
func AutonomyAllowsOrgChange(int) bool { return false }

// requireGovernanceBoss refuses an actor that is not an owner or admin member of
// the workspace. It reads the member row through q, so the check runs inside the
// caller's transaction.
func requireGovernanceBoss(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor) error {
	if actor.Type != GovernanceActorMember || !actor.ID.Valid {
		return ErrGovernanceActorNotBoss
	}
	m, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actor.ID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGovernanceActorNotBoss
	}
	if err != nil {
		return fmt.Errorf("load member: %w", err)
	}
	if m.Role != "owner" && m.Role != "admin" {
		return ErrGovernanceActorNotBoss
	}
	return nil
}

// AutonomyChange is the outcome of ApplyAutonomyChange.
type AutonomyChange struct {
	SquadID pgtype.UUID
	From    int
	To      int
	// Changed is false when the squad already had the level; nothing is written then.
	Changed bool
}

// ApplyAutonomyChange sets a squad's autonomy level inside the caller's
// transaction: q is bound to it, and the caller commits. It writes one
// autonomy_changed governance entry with the reason, in the same transaction.
//
// The level API and the org_change autonomy_change proposal both come through
// here, so the boss's check and the log entry are the same in both. Only an owner
// or admin member may change a level. An agent never may.
func ApplyAutonomyChange(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID, level int, actor GovernanceActor, reason string) (AutonomyChange, error) {
	if !ValidAutonomyLevel(level) {
		return AutonomyChange{}, ErrAutonomyLevelInvalid
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > AutonomyReasonMax {
		return AutonomyChange{}, ErrAutonomyReasonRequired
	}
	if err := requireGovernanceBoss(ctx, q, ws, actor); err != nil {
		return AutonomyChange{}, err
	}
	squad, err := q.LockSquadForAutonomy(ctx, db.LockSquadForAutonomyParams{ID: squadID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && squad.ArchivedAt.Valid) {
		return AutonomyChange{}, ErrAutonomySquadNotFound
	}
	if err != nil {
		return AutonomyChange{}, fmt.Errorf("lock squad: %w", err)
	}
	from := int(squad.AutonomyLevel)
	out := AutonomyChange{SquadID: squadID, From: from, To: level}
	if from == level {
		return out, nil
	}
	if _, err := q.SetSquadAutonomyLevel(ctx, db.SetSquadAutonomyLevelParams{
		ID: squadID, WorkspaceID: ws, AutonomyLevel: int32(level),
	}); err != nil {
		return AutonomyChange{}, fmt.Errorf("set autonomy level: %w", err)
	}
	out.Changed = true
	if err := NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindAutonomyChanged,
		ActorType:   actor.Type,
		ActorID:     actor.ID,
		SubjectType: "squad",
		SubjectID:   squadID,
		Summary: fmt.Sprintf("%s moved from L%d %s to L%d %s: %s",
			squad.Name, from, AutonomyLevelName(from), level, AutonomyLevelName(level), reason),
		Details: map[string]any{
			"from":   from,
			"to":     level,
			"reason": reason,
		},
	}); err != nil {
		return AutonomyChange{}, err
	}
	return out, nil
}

// AutonomyView is a squad's level as the API reports it.
type AutonomyView struct {
	SquadID pgtype.UUID
	Level   int
	// Reason and ChangedAt come from the latest autonomy_changed entry. They are
	// empty and nil while the level is still the default.
	Reason    string
	ChangedAt *time.Time
}

// AutonomyService reads and changes squad autonomy levels for the API.
type AutonomyService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// Get returns a live squad's level and the reason of its last change.
func (s *AutonomyService) Get(ctx context.Context, ws, squadID pgtype.UUID) (AutonomyView, error) {
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: squadID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && squad.ArchivedAt.Valid) {
		return AutonomyView{}, ErrAutonomySquadNotFound
	}
	if err != nil {
		return AutonomyView{}, fmt.Errorf("load squad: %w", err)
	}
	view := AutonomyView{SquadID: squadID, Level: int(squad.AutonomyLevel)}
	last, err := s.Queries.GetLastAutonomyChange(ctx, db.GetLastAutonomyChangeParams{WorkspaceID: ws, SubjectID: squadID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return AutonomyView{}, fmt.Errorf("load last autonomy change: %w", err)
	default:
		view.ChangedAt = &last.CreatedAt.Time
		view.Reason = autonomyReasonOf(last)
	}
	return view, nil
}

func autonomyReasonOf(e db.GovernanceEvent) string {
	var d struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(e.Details, &d); err != nil {
		return ""
	}
	return d.Reason
}

// Set changes a squad's level, boss only, and returns the level after the change.
func (s *AutonomyService) Set(ctx context.Context, ws, squadID pgtype.UUID, level int, actor GovernanceActor, reason string) (AutonomyView, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return AutonomyView{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := ApplyAutonomyChange(ctx, s.Queries.WithTx(tx), ws, squadID, level, actor, reason); err != nil {
		return AutonomyView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutonomyView{}, fmt.Errorf("commit: %w", err)
	}
	return s.Get(ctx, ws, squadID)
}

// autonomyHold is the L0 cell at autopilot dispatch: it reports whether the squad's
// level holds this routine run, with the reason to record on the skipped run. Only
// a squad-assigned autopilot run that the company's classes call routine is held.
// A person's run (manual, chat) and a delegated one are never held here, so an L0
// squad still runs what someone assigns or mentions.
func (s *AutopilotService) autonomyHold(ctx context.Context, ap db.Autopilot, source string) (string, dispatch.ReasonCode, bool) {
	if ap.AssigneeType != "squad" || !ap.AssigneeID.Valid {
		return "", "", false
	}
	class := ResolveRunClass(RunClassInput{AutopilotSource: source})
	if class != RunClassRoutine {
		return "", "", false
	}
	level, err := s.Queries.GetSquadAutonomyLevel(ctx, db.GetSquadAutonomyLevelParams{ID: ap.AssigneeID, WorkspaceID: ap.WorkspaceID})
	if err != nil {
		// As the company check does: a transient failure never swallows a run.
		slog.Warn("autopilot admission: squad autonomy level unreadable; dispatching",
			"autopilot_id", util.UUIDToString(ap.ID), "error", err)
		return "", "", false
	}
	if AutonomyRunVerdict(int(level), class).Admit {
		return "", "", false
	}
	return "the squad is at autonomy L0 (Suggest); its routines are held until someone assigns or mentions it",
		dispatch.ReasonAutonomyL0, true
}

// wakeupAutonomyVerdict is the L0 cell for an issue wakeup: a routine firing on an
// issue that a squad holds is held while that squad is at L0. An issue held by no
// squad is admitted here; the company controls decide it.
func wakeupAutonomyVerdict(ctx context.Context, q *db.Queries, issue db.Issue) (Verdict, error) {
	if issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return Verdict{Admit: true}, nil
	}
	level, err := q.GetSquadAutonomyLevel(ctx, db.GetSquadAutonomyLevelParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Verdict{Admit: true}, nil
	}
	if err != nil {
		return Verdict{}, fmt.Errorf("read squad autonomy level for wakeup: %w", err)
	}
	return AutonomyRunVerdict(int(level), RunClassRoutine), nil
}
