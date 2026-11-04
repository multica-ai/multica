package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Goals and key results (spec 4.1). A goal has a scope, an owner, a period and
// key results. Progress and confidence are computed on read from the latest
// readings in the period; the derived confidence is stored so a change can be
// reported. A squad's goal comes from a goal proposal and is active at approval.
// Every other goal is drafted and then activated by the boss or an admin.

// Goal statuses, as the goal table's CHECK allows them.
const (
	GoalStatusDraft    = "draft"
	GoalStatusActive   = "active"
	GoalStatusAchieved = "achieved"
	GoalStatusMissed   = "missed"
	GoalStatusDropped  = "dropped"
)

// allGoalStatuses lists every status, for reads that take all goals.
var allGoalStatuses = []string{GoalStatusDraft, GoalStatusActive, GoalStatusAchieved, GoalStatusMissed, GoalStatusDropped}

// Goal limits, bounded for the reader.
const (
	goalMaxTitle          = 200
	goalMaxWhy            = 4000
	goalMaxKeyResults     = 10
	goalMaxPeriodDays     = 2 * 366
	goalMaxConfidenceNote = 2000
	goalSeriesPoints      = 60
)

var (
	// ErrGoalNotFound: no goal with that id in the workspace.
	ErrGoalNotFound = errors.New("goal not found")
	// ErrGoalInvalid: a field is missing or out of range. The message says which.
	ErrGoalInvalid = errors.New("goal is invalid")
	// ErrGoalState: the goal's status does not allow the change.
	ErrGoalState = errors.New("the goal's status does not allow this change")
	// ErrGoalSquadScope: a squad's goal comes from a goal proposal, not a direct write.
	ErrGoalSquadScope = errors.New("a squad's goal comes from a goal proposal: the squad files one with kind goal")
	// ErrGoalOwnerInvalid: the owner is not a squad, member or agent of this workspace.
	ErrGoalOwnerInvalid = errors.New("owner is not in this workspace")
)

// GoalKeyResultInput is one key result to write. An empty ScopeType reads the
// metric at the goal's scope.
type GoalKeyResultInput struct {
	Title       string
	MetricKey   string
	ScopeType   string
	ScopeID     pgtype.UUID
	StartValue  float64
	TargetValue float64
	Weight      int
}

// GoalActor is who writes: a member (the user id) or an agent.
type GoalActor struct {
	Type string
	ID   pgtype.UUID
}

// GoalWriteParams are the already-authorized fields of a new goal.
type GoalWriteParams struct {
	WorkspaceID  pgtype.UUID
	ScopeType    string
	ScopeID      pgtype.UUID
	ParentGoalID pgtype.UUID
	Title        string
	Why          string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	OwnerType    string
	OwnerID      pgtype.UUID
	KeyResults   []GoalKeyResultInput
	Actor        GoalActor
}

// KeyResultView is one key result with its progress. Current is nil when no
// reading falls in the goal's period.
type KeyResultView struct {
	ID          string
	GoalID      string
	Title       string
	MetricKey   string
	ScopeType   string
	ScopeID     pgtype.UUID
	StartValue  float64
	TargetValue float64
	Weight      int
	Current     *float64
	ObservedAt  *time.Time
	Progress    *float64
	// Series is the key result's readings in the period, oldest first. Only a
	// goal's detail carries it.
	Series []GoalSeriesPoint
}

// GoalSeriesPoint is one reading of a key result's series.
type GoalSeriesPoint struct {
	ObservedAt time.Time
	Value      float64
}

// GoalView is a goal with its key results, progress and confidence.
type GoalView struct {
	Goal       db.Goal
	KeyResults []KeyResultView
	// Progress is the weighted mean of the key results that have a reading. Nil
	// when none does.
	Progress *float64
	// ReadingsMissing counts key results with no reading in the period.
	ReadingsMissing int
	// Derived is the confidence the readings give. Effective is the owner's
	// override when one is set, otherwise Derived.
	Derived   string
	Effective string
	// Overridden is true when the owner's override is in force.
	Overridden bool
}

// GoalFilter selects goals for a list. Empty fields do not filter.
type GoalFilter struct {
	ScopeType string
	ScopeID   pgtype.UUID
	Status    string
}

// GoalService creates, reads and activates goals.
type GoalService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Metrics   *MetricService
	// Entitlements is the issue-count policy the planning issues obey. Nil means
	// no limit, as for every other writer when no provider is wired.
	Entitlements entitlement.Provider
	// Now is the clock; nil means time.Now. Tests pin it.
	Now func() time.Time
}

// NewGoalServiceFor builds the goal service the issue service's database and
// issue-count policy serve.
func NewGoalServiceFor(issues *IssueService) *GoalService {
	g := NewGoalService(issues.Queries, issues.TxStarter)
	g.Entitlements = issues.Entitlements
	return g
}

// NewGoalService builds the goal service over the database.
func NewGoalService(q *db.Queries, tx TxStarter) *GoalService {
	return &GoalService{Queries: q, TxStarter: tx, Metrics: NewMetricService(q, tx)}
}

func (s *GoalService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *GoalService) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Create writes a draft goal and its key results. Activation is a separate step.
// A squad's goal is refused: it comes from a goal proposal.
func (s *GoalService) Create(ctx context.Context, p GoalWriteParams) (GoalView, error) {
	if p.ScopeType == ScopeSquad {
		return GoalView{}, ErrGoalSquadScope
	}
	switch p.ScopeType {
	case ScopeCompany, ScopeFloor, ScopeInitiative:
	default:
		return GoalView{}, fmt.Errorf("%w: scope must be company, floor or initiative", ErrGoalInvalid)
	}
	if err := validateGoalFields(p.Title, p.Why, p.PeriodStart, p.PeriodEnd, p.OwnerType, p.OwnerID); err != nil {
		return GoalView{}, err
	}
	if err := validateKeyResultInputs(p.KeyResults); err != nil {
		return GoalView{}, err
	}
	var id pgtype.UUID
	err := s.inTx(ctx, func(q *db.Queries) error {
		if err := s.checkGoalRefs(ctx, q, p); err != nil {
			return err
		}
		goal, err := insertGoal(ctx, q, p, GoalStatusDraft, pgtype.UUID{})
		if err != nil {
			return err
		}
		id = goal.ID
		return nil
	})
	if err != nil {
		return GoalView{}, err
	}
	return s.Get(ctx, p.WorkspaceID, id)
}

// WriteImportedGoal writes a goal with the status it had in a company export, and
// its key results, inside q's transaction. The company import uses it (spec 4.9).
// The references are checked as a write checks them, so a metric, owner or parent
// the workspace lacks is refused. A squad's goal is written only as a draft: it
// comes from a goal proposal, and the import approves nothing.
func (s *GoalService) WriteImportedGoal(ctx context.Context, q *db.Queries, p GoalWriteParams, status string) (db.Goal, error) {
	switch p.ScopeType {
	case ScopeCompany, ScopeFloor, ScopeInitiative:
	case ScopeSquad:
		// A squad's goal normally comes from a goal proposal. An import recreates it
		// as a draft only, so no approval is skipped: the boss activates it.
		if status != GoalStatusDraft {
			return db.Goal{}, fmt.Errorf("%w: an imported squad goal is written as a draft", ErrGoalSquadScope)
		}
	default:
		return db.Goal{}, fmt.Errorf("%w: scope must be company, floor, squad or initiative", ErrGoalInvalid)
	}
	switch status {
	case GoalStatusDraft, GoalStatusActive, GoalStatusAchieved, GoalStatusMissed, GoalStatusDropped:
	default:
		return db.Goal{}, fmt.Errorf("%w: status %q is not a goal status", ErrGoalInvalid, status)
	}
	if err := validateGoalFields(p.Title, p.Why, p.PeriodStart, p.PeriodEnd, p.OwnerType, p.OwnerID); err != nil {
		return db.Goal{}, err
	}
	if err := validateKeyResultInputs(p.KeyResults); err != nil {
		return db.Goal{}, err
	}
	if err := s.checkGoalRefs(ctx, q, p); err != nil {
		return db.Goal{}, err
	}
	return insertGoal(ctx, q, p, status, pgtype.UUID{})
}

// insertGoal writes a goal and its key results inside q's transaction.
// sourceProposal is set for a goal that came from a proposal.
func insertGoal(ctx context.Context, q *db.Queries, p GoalWriteParams, status string, sourceProposal pgtype.UUID) (db.Goal, error) {
	goal, err := q.CreateGoal(ctx, db.CreateGoalParams{
		ID:               dbid.NewV7(),
		WorkspaceID:      p.WorkspaceID,
		ScopeType:        p.ScopeType,
		ScopeID:          p.ScopeID,
		ParentGoalID:     p.ParentGoalID,
		Title:            strings.TrimSpace(p.Title),
		Why:              strings.TrimSpace(p.Why),
		PeriodStart:      dateOf(p.PeriodStart),
		PeriodEnd:        dateOf(p.PeriodEnd),
		OwnerType:        p.OwnerType,
		OwnerID:          p.OwnerID,
		Status:           status,
		SourceProposalID: sourceProposal,
		CreatedByType:    p.Actor.Type,
		CreatedByID:      p.Actor.ID,
	})
	if err != nil {
		return db.Goal{}, fmt.Errorf("create goal: %w", err)
	}
	for _, kr := range p.KeyResults {
		if err := insertKeyResult(ctx, q, goal, kr); err != nil {
			return db.Goal{}, err
		}
	}
	return goal, nil
}

func insertKeyResult(ctx context.Context, q *db.Queries, goal db.Goal, kr GoalKeyResultInput) error {
	start, err := numericFromFloat(kr.StartValue)
	if err != nil {
		return fmt.Errorf("%w: key result start value", ErrGoalInvalid)
	}
	target, err := numericFromFloat(kr.TargetValue)
	if err != nil {
		return fmt.Errorf("%w: key result target value", ErrGoalInvalid)
	}
	scopeType, scopeID := goal.ScopeType, goal.ScopeID
	if kr.ScopeType != "" {
		scopeType, scopeID = kr.ScopeType, kr.ScopeID
	}
	weight := kr.Weight
	if weight == 0 {
		weight = 1
	}
	if _, err := q.CreateKeyResult(ctx, db.CreateKeyResultParams{
		ID: dbid.NewV7(), WorkspaceID: goal.WorkspaceID, GoalID: goal.ID, Title: strings.TrimSpace(kr.Title),
		MetricKey: kr.MetricKey, ScopeType: scopeType, ScopeID: scopeID,
		StartValue: start, TargetValue: target, Weight: int32(weight),
	}); err != nil {
		return fmt.Errorf("create key result: %w", err)
	}
	return nil
}

// validateGoalFields checks the fields that need no database.
func validateGoalFields(title, why string, start, end time.Time, ownerType string, ownerID pgtype.UUID) error {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return fmt.Errorf("%w: title is required", ErrGoalInvalid)
	case len([]rune(title)) > goalMaxTitle:
		return fmt.Errorf("%w: title must be at most %d characters", ErrGoalInvalid, goalMaxTitle)
	case len(why) > goalMaxWhy:
		return fmt.Errorf("%w: why must be at most %d characters", ErrGoalInvalid, goalMaxWhy)
	}
	if err := validatePeriod(start, end); err != nil {
		return err
	}
	switch ownerType {
	case "squad", "member", "agent":
	default:
		return fmt.Errorf("%w: owner_type must be squad, member or agent", ErrGoalInvalid)
	}
	if !ownerID.Valid {
		return fmt.Errorf("%w: owner_id is required", ErrGoalInvalid)
	}
	return nil
}

// validatePeriod requires a forward period of at most two years.
func validatePeriod(start, end time.Time) error {
	if start.IsZero() || end.IsZero() {
		return fmt.Errorf("%w: period_start and period_end are required", ErrGoalInvalid)
	}
	if !start.Before(end) {
		return fmt.Errorf("%w: period_start must be before period_end", ErrGoalInvalid)
	}
	if end.Sub(start) > goalMaxPeriodDays*24*time.Hour {
		return fmt.Errorf("%w: a period is at most two years", ErrGoalInvalid)
	}
	return nil
}

// validateKeyResultInputs checks the key results of a goal: none to ten, each
// with a title, a metric key, finite values whose target differs from its start,
// and a weight from 1 to 10 (0 means 1). A goal may have no key results yet: it
// is a valid draft, its progress is not set, and its confidence is unknown.
func validateKeyResultInputs(krs []GoalKeyResultInput) error {
	if len(krs) > goalMaxKeyResults {
		return fmt.Errorf("%w: at most %d key results", ErrGoalInvalid, goalMaxKeyResults)
	}
	for i, kr := range krs {
		title := strings.TrimSpace(kr.Title)
		switch {
		case title == "" || len([]rune(title)) > goalMaxTitle:
			return fmt.Errorf("%w: key result %d needs a title of at most %d characters", ErrGoalInvalid, i+1, goalMaxTitle)
		case !metricKeyPattern.MatchString(kr.MetricKey):
			return fmt.Errorf("%w: key result %d: %v", ErrGoalInvalid, i+1, ErrMetricKeyInvalid)
		case math.IsNaN(kr.StartValue) || math.IsInf(kr.StartValue, 0) || math.IsNaN(kr.TargetValue) || math.IsInf(kr.TargetValue, 0):
			return fmt.Errorf("%w: key result %d values must be finite numbers", ErrGoalInvalid, i+1)
		case kr.StartValue == kr.TargetValue:
			return fmt.Errorf("%w: key result %d needs a target different from its start", ErrGoalInvalid, i+1)
		case kr.Weight != 0 && (kr.Weight < 1 || kr.Weight > 10):
			return fmt.Errorf("%w: key result %d weight must be from 1 to 10", ErrGoalInvalid, i+1)
		}
	}
	return nil
}

// checkGoalRefs checks what a goal points at: its scope object, its owner, its
// parent goal, and each key result's metric and its own scope.
func (s *GoalService) checkGoalRefs(ctx context.Context, q *db.Queries, p GoalWriteParams) error {
	if err := s.Metrics.ValidateScope(ctx, q, p.WorkspaceID, p.ScopeType, p.ScopeID); err != nil {
		return fmt.Errorf("%w: %v", ErrGoalInvalid, err)
	}
	if err := checkGoalOwner(ctx, q, p.WorkspaceID, p.OwnerType, p.OwnerID); err != nil {
		return err
	}
	if p.ParentGoalID.Valid {
		if _, err := q.GetGoalInWorkspace(ctx, db.GetGoalInWorkspaceParams{ID: p.ParentGoalID, WorkspaceID: p.WorkspaceID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: parent goal is not in this workspace", ErrGoalInvalid)
			}
			return err
		}
	}
	return s.checkKeyResultRefs(ctx, q, p.WorkspaceID, p.KeyResults)
}

// checkKeyResultRefs confirms each key result's metric exists and its scope, when
// it names one, is in the workspace.
func (s *GoalService) checkKeyResultRefs(ctx context.Context, q *db.Queries, ws pgtype.UUID, krs []GoalKeyResultInput) error {
	for i, kr := range krs {
		if _, err := q.GetMetricInWorkspace(ctx, db.GetMetricInWorkspaceParams{WorkspaceID: ws, Key: kr.MetricKey}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: key result %d: %v", ErrGoalInvalid, i+1, ErrMetricNotFound)
			}
			return err
		}
		if kr.ScopeType != "" {
			if err := s.Metrics.ValidateScope(ctx, q, ws, kr.ScopeType, kr.ScopeID); err != nil {
				return fmt.Errorf("%w: key result %d: %v", ErrGoalInvalid, i+1, err)
			}
		}
	}
	return nil
}

// checkGoalOwner confirms the owner exists in the workspace: a squad, a member
// (by user id) or an agent.
func checkGoalOwner(ctx context.Context, q *db.Queries, ws pgtype.UUID, ownerType string, ownerID pgtype.UUID) error {
	var err error
	switch ownerType {
	case "squad":
		_, err = q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: ownerID, WorkspaceID: ws})
	case "member":
		_, err = q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: ownerID, WorkspaceID: ws})
	case "agent":
		_, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: ownerID, WorkspaceID: ws})
	default:
		return fmt.Errorf("%w: owner_type must be squad, member or agent", ErrGoalInvalid)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGoalOwnerInvalid
	}
	if err != nil {
		return fmt.Errorf("check owner: %w", err)
	}
	return nil
}

// GoalUpdate is a change to a goal that has not closed. KeyResults nil leaves the
// key results as they are. A draft takes every change; an active goal keeps its
// period and its key results, which its readings are judged by.
type GoalUpdate struct {
	Title        string
	Why          string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	OwnerType    string
	OwnerID      pgtype.UUID
	ParentGoalID pgtype.UUID
	KeyResults   []GoalKeyResultInput
}

// Update changes a goal. Its scope is fixed; a squad's goal may be updated too,
// since the squad owns it.
func (s *GoalService) Update(ctx context.Context, ws, id pgtype.UUID, u GoalUpdate) (GoalView, error) {
	err := s.inTx(ctx, func(q *db.Queries) error {
		goal, err := lockGoal(ctx, q, ws, id)
		if err != nil {
			return err
		}
		if goal.Status != GoalStatusDraft && goal.Status != GoalStatusActive {
			return ErrGoalState
		}
		periodChanged := !dateOf(u.PeriodStart).Time.Equal(goal.PeriodStart.Time) || !dateOf(u.PeriodEnd).Time.Equal(goal.PeriodEnd.Time)
		if goal.Status == GoalStatusActive && (periodChanged || u.KeyResults != nil) {
			return fmt.Errorf("%w: an active goal's period and key results are fixed; change its title, why, owner or parent", ErrGoalState)
		}
		if err := validateGoalFields(u.Title, u.Why, u.PeriodStart, u.PeriodEnd, u.OwnerType, u.OwnerID); err != nil {
			return err
		}
		if u.KeyResults != nil {
			if err := validateKeyResultInputs(u.KeyResults); err != nil {
				return err
			}
		}
		if err := checkGoalOwner(ctx, q, ws, u.OwnerType, u.OwnerID); err != nil {
			return err
		}
		if u.ParentGoalID.Valid {
			if _, err := q.GetGoalInWorkspace(ctx, db.GetGoalInWorkspaceParams{ID: u.ParentGoalID, WorkspaceID: ws}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("%w: parent goal is not in this workspace", ErrGoalInvalid)
				}
				return err
			}
		}
		if err := s.checkKeyResultRefs(ctx, q, ws, u.KeyResults); err != nil {
			return err
		}
		if _, err := q.UpdateGoal(ctx, db.UpdateGoalParams{
			ID: id, WorkspaceID: ws, Title: strings.TrimSpace(u.Title), Why: strings.TrimSpace(u.Why),
			PeriodStart: dateOf(u.PeriodStart), PeriodEnd: dateOf(u.PeriodEnd),
			OwnerType: u.OwnerType, OwnerID: u.OwnerID, ParentGoalID: u.ParentGoalID,
		}); err != nil {
			return fmt.Errorf("update goal: %w", err)
		}
		if u.KeyResults != nil {
			if err := q.DeleteGoalKeyResults(ctx, db.DeleteGoalKeyResultsParams{WorkspaceID: ws, GoalID: id}); err != nil {
				return fmt.Errorf("replace key results: %w", err)
			}
			for _, kr := range u.KeyResults {
				if err := insertKeyResult(ctx, q, goal, kr); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return GoalView{}, err
	}
	return s.Get(ctx, ws, id)
}

func lockGoal(ctx context.Context, q *db.Queries, ws, id pgtype.UUID) (db.Goal, error) {
	goal, err := q.LockGoalInWorkspace(ctx, db.LockGoalInWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Goal{}, ErrGoalNotFound
	}
	if err != nil {
		return db.Goal{}, fmt.Errorf("lock goal: %w", err)
	}
	return goal, nil
}

// Activate makes a draft goal active and records it in the governance log.
func (s *GoalService) Activate(ctx context.Context, ws, id pgtype.UUID, actor GoalActor) (GoalView, error) {
	err := s.inTx(ctx, func(q *db.Queries) error {
		goal, err := lockGoal(ctx, q, ws, id)
		if err != nil {
			return err
		}
		if goal.Status != GoalStatusDraft {
			return ErrGoalState
		}
		if _, err := q.SetGoalStatus(ctx, db.SetGoalStatusParams{ID: id, WorkspaceID: ws, Status: GoalStatusActive}); err != nil {
			return fmt.Errorf("activate goal: %w", err)
		}
		return NewGovernanceLog().Record(ctx, q, GovernanceEvent{
			WorkspaceID: ws, Kind: GovernanceKindGoalActivated,
			ActorType: governanceActorType(actor.Type), ActorID: actor.ID,
			SubjectType: "goal", SubjectID: id,
			Summary: "Goal activated: " + goal.Title,
			Details: map[string]any{"scope_type": goal.ScopeType, "scope_id": uuidOrEmpty(goal.ScopeID)},
		})
	})
	if err != nil {
		return GoalView{}, err
	}
	s.refreshOne(ctx, ws, id)
	return s.Get(ctx, ws, id)
}

// ActivateFromProposal implements proposalkinds.Goals. The approval of a squad's
// goal proposal writes the goal active, with its key results and its governance
// entry, inside the decision transaction.
func (s *GoalService) ActivateFromProposal(ctx context.Context, q *db.Queries, d proposalkinds.Decision, g proposalkinds.GoalFiling) error {
	start, err := time.Parse("2006-01-02", g.PeriodStart)
	if err != nil {
		return fmt.Errorf("%w: period_start: %v", ErrGoalInvalid, err)
	}
	end, err := time.Parse("2006-01-02", g.PeriodEnd)
	if err != nil {
		return fmt.Errorf("%w: period_end: %v", ErrGoalInvalid, err)
	}
	parent := pgtype.UUID{}
	if g.ParentGoalID != "" {
		if parent, err = util.ParseUUID(g.ParentGoalID); err != nil {
			return fmt.Errorf("%w: parent_goal_id", ErrGoalInvalid)
		}
	}
	krs := make([]GoalKeyResultInput, 0, len(g.KeyResults))
	for _, kr := range g.KeyResults {
		krs = append(krs, GoalKeyResultInput{
			Title: kr.Title, MetricKey: kr.MetricKey, StartValue: kr.StartValue, TargetValue: kr.TargetValue, Weight: kr.Weight,
		})
	}
	squad := d.Proposal.SquadID
	p := GoalWriteParams{
		WorkspaceID: d.WorkspaceID, ScopeType: ScopeSquad, ScopeID: squad, ParentGoalID: parent,
		Title: g.Title, Why: g.Why, PeriodStart: start, PeriodEnd: end,
		OwnerType: "squad", OwnerID: squad, KeyResults: krs,
		Actor: GoalActor{Type: ReadingActorMember, ID: d.DeciderID},
	}
	goal, err := insertGoal(ctx, q, p, GoalStatusActive, d.Proposal.ID)
	if err != nil {
		return err
	}
	return NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: d.WorkspaceID, Kind: GovernanceKindGoalActivated,
		ActorType: GovernanceActorMember, ActorID: d.DeciderID,
		SubjectType: "goal", SubjectID: goal.ID,
		Summary: "Goal activated from a squad proposal: " + goal.Title,
		Details: map[string]any{
			"scope_type":  ScopeSquad,
			"scope_id":    util.UUIDToString(squad),
			"proposal_id": util.UUIDToString(d.Proposal.ID),
		},
	})
}

// SetStatus closes an active goal as achieved, missed or dropped, or drops a
// draft. A closed goal does not change again.
func (s *GoalService) SetStatus(ctx context.Context, ws, id pgtype.UUID, status string) (GoalView, error) {
	switch status {
	case GoalStatusAchieved, GoalStatusMissed, GoalStatusDropped:
	default:
		return GoalView{}, fmt.Errorf("%w: status must be achieved, missed or dropped", ErrGoalInvalid)
	}
	err := s.inTx(ctx, func(q *db.Queries) error {
		goal, err := lockGoal(ctx, q, ws, id)
		if err != nil {
			return err
		}
		allowed := goal.Status == GoalStatusActive || (goal.Status == GoalStatusDraft && status == GoalStatusDropped)
		if !allowed {
			return ErrGoalState
		}
		_, err = q.SetGoalStatus(ctx, db.SetGoalStatusParams{ID: id, WorkspaceID: ws, Status: status})
		return err
	})
	if err != nil {
		return GoalView{}, err
	}
	return s.Get(ctx, ws, id)
}

// Delete removes a draft goal and its key results. A goal that has been active
// is closed with SetStatus instead, so its history stays.
func (s *GoalService) Delete(ctx context.Context, ws, id pgtype.UUID) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		goal, err := lockGoal(ctx, q, ws, id)
		if err != nil {
			return err
		}
		if goal.Status != GoalStatusDraft {
			return ErrGoalState
		}
		if err := q.DeleteGoalKeyResults(ctx, db.DeleteGoalKeyResultsParams{WorkspaceID: ws, GoalID: id}); err != nil {
			return fmt.Errorf("delete key results: %w", err)
		}
		_, err = q.DeleteDraftGoal(ctx, db.DeleteDraftGoalParams{ID: id, WorkspaceID: ws})
		if err != nil {
			return fmt.Errorf("delete goal: %w", err)
		}
		return nil
	})
}

// SetConfidenceOverride sets or clears the owner's confidence on an active goal.
// A value needs a note saying why. A nil value clears the override, and the
// derived confidence is shown again.
func (s *GoalService) SetConfidenceOverride(ctx context.Context, ws, id pgtype.UUID, value *string, note string) (GoalView, error) {
	note = strings.TrimSpace(note)
	if value != nil {
		switch *value {
		case ConfidenceOnTrack, ConfidenceAtRisk, ConfidenceOffTrack:
		default:
			return GoalView{}, fmt.Errorf("%w: confidence must be on_track, at_risk or off_track", ErrGoalInvalid)
		}
		if note == "" {
			return GoalView{}, fmt.Errorf("%w: an override needs a note saying why", ErrGoalInvalid)
		}
	}
	if len(note) > goalMaxConfidenceNote {
		return GoalView{}, fmt.Errorf("%w: note must be at most %d characters", ErrGoalInvalid, goalMaxConfidenceNote)
	}
	err := s.inTx(ctx, func(q *db.Queries) error {
		goal, err := lockGoal(ctx, q, ws, id)
		if err != nil {
			return err
		}
		if goal.Status != GoalStatusActive {
			return ErrGoalState
		}
		_, err = q.SetGoalConfidenceOverride(ctx, db.SetGoalConfidenceOverrideParams{
			ID: id, WorkspaceID: ws, ConfidenceOverride: overrideText(value), ConfidenceNote: note,
		})
		return err
	})
	if err != nil {
		return GoalView{}, err
	}
	return s.Get(ctx, ws, id)
}

// overrideText is the nullable confidence override column for a value.
func overrideText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}

// Get returns one goal with its key results, their series and the progress
// computed from the latest readings.
func (s *GoalService) Get(ctx context.Context, ws, id pgtype.UUID) (GoalView, error) {
	goal, err := s.Queries.GetGoalInWorkspace(ctx, db.GetGoalInWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return GoalView{}, ErrGoalNotFound
	}
	if err != nil {
		return GoalView{}, fmt.Errorf("get goal: %w", err)
	}
	rows, err := s.Queries.ListKeyResultLatestReadings(ctx, db.ListKeyResultLatestReadingsParams{
		WorkspaceID: ws, Statuses: allGoalStatuses, GoalID: id,
	})
	if err != nil {
		return GoalView{}, fmt.Errorf("list key results: %w", err)
	}
	view := buildGoalViews([]db.Goal{goal}, rows, s.now())[0]
	for i := range view.KeyResults {
		kr := &view.KeyResults[i]
		series, err := s.Queries.ListMetricReadingsForSeries(ctx, db.ListMetricReadingsForSeriesParams{
			WorkspaceID: ws, MetricKey: kr.MetricKey, ScopeType: kr.ScopeType, ScopeID: kr.ScopeID,
			Since:    pgtype.Timestamptz{Time: goal.PeriodStart.Time, Valid: true},
			Until:    pgtype.Timestamptz{Time: goal.PeriodEnd.Time.AddDate(0, 0, 1), Valid: true},
			RowLimit: goalSeriesPoints,
		})
		if err != nil {
			return GoalView{}, fmt.Errorf("list key result series: %w", err)
		}
		// The query is newest first; the chart reads oldest first.
		for j := len(series) - 1; j >= 0; j-- {
			if v, ok := floatFromNumeric(series[j].Value); ok {
				kr.Series = append(kr.Series, GoalSeriesPoint{ObservedAt: series[j].ObservedAt.Time, Value: v})
			}
		}
	}
	return view, nil
}

// GoalChildren lists the goals whose parent is this one.
func (s *GoalService) GoalChildren(ctx context.Context, ws, parent pgtype.UUID) ([]db.Goal, error) {
	all, err := s.Queries.ListGoalsInWorkspace(ctx, db.ListGoalsInWorkspaceParams{WorkspaceID: ws})
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	var out []db.Goal
	for _, g := range all {
		if g.ParentGoalID == parent {
			out = append(out, g)
		}
	}
	return out, nil
}

// List returns the goals of the workspace that match the filter, with progress
// and confidence. Key results carry no series here.
func (s *GoalService) List(ctx context.Context, ws pgtype.UUID, f GoalFilter) ([]GoalView, error) {
	goals, err := s.Queries.ListGoalsInWorkspace(ctx, db.ListGoalsInWorkspaceParams{
		WorkspaceID: ws,
		ScopeType:   pgtype.Text{String: f.ScopeType, Valid: f.ScopeType != ""},
		ScopeID:     f.ScopeID,
		Status:      pgtype.Text{String: f.Status, Valid: f.Status != ""},
	})
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	rows, err := s.Queries.ListKeyResultLatestReadings(ctx, db.ListKeyResultLatestReadingsParams{
		WorkspaceID: ws, Statuses: allGoalStatuses,
	})
	if err != nil {
		return nil, fmt.Errorf("list key results: %w", err)
	}
	return buildGoalViews(goals, rows, s.now()), nil
}

// GoalsAtRisk counts the active goals whose effective confidence, after an
// owner's override, is at risk or off track. The pulse reads it (spec 4.8).
func (s *GoalService) GoalsAtRisk(ctx context.Context, ws pgtype.UUID) (int, error) {
	views, err := s.List(ctx, ws, GoalFilter{Status: GoalStatusActive})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, v := range views {
		if v.Effective == ConfidenceAtRisk || v.Effective == ConfidenceOffTrack {
			n++
		}
	}
	return n, nil
}

// RefreshConfidence recomputes the derived confidence of every active goal in the
// workspace and stores the ones that changed. The nightly job runs it, and a
// reading or a goal change runs it for the goals it touches.
func (s *GoalService) RefreshConfidence(ctx context.Context, ws pgtype.UUID) (int, error) {
	goals, err := s.Queries.ListGoalsByStatus(ctx, db.ListGoalsByStatusParams{WorkspaceID: ws, Statuses: []string{GoalStatusActive}})
	if err != nil {
		return 0, fmt.Errorf("list active goals: %w", err)
	}
	if len(goals) == 0 {
		return 0, nil
	}
	rows, err := s.Queries.ListKeyResultLatestReadings(ctx, db.ListKeyResultLatestReadingsParams{
		WorkspaceID: ws, Statuses: []string{GoalStatusActive},
	})
	if err != nil {
		return 0, fmt.Errorf("list key results: %w", err)
	}
	changed := 0
	for _, v := range buildGoalViews(goals, rows, s.now()) {
		if v.Derived == v.Goal.Confidence {
			continue
		}
		if _, err := s.Queries.SetGoalDerivedConfidence(ctx, db.SetGoalDerivedConfidenceParams{
			ID: v.Goal.ID, WorkspaceID: ws, Confidence: v.Derived,
		}); err != nil {
			return changed, fmt.Errorf("store confidence: %w", err)
		}
		changed++
	}
	return changed, nil
}

// refreshOne recomputes one goal's derived confidence after a change. A failure
// is logged by the caller's next nightly run; the write that asked for it has
// already committed, so it is not reported as a failed write.
func (s *GoalService) refreshOne(ctx context.Context, ws, id pgtype.UUID) {
	view, err := s.Get(ctx, ws, id)
	if err != nil || view.Goal.Status != GoalStatusActive || view.Derived == view.Goal.Confidence {
		return
	}
	_, _ = s.Queries.SetGoalDerivedConfidence(ctx, db.SetGoalDerivedConfidenceParams{
		ID: id, WorkspaceID: ws, Confidence: view.Derived,
	})
}

// buildGoalViews joins goals with their key result rows and computes progress and
// confidence for each. It touches no database, so the arithmetic is tested on its
// own. A draft goal's confidence is unknown: it has no line to be measured against.
func buildGoalViews(goals []db.Goal, rows []db.ListKeyResultLatestReadingsRow, now time.Time) []GoalView {
	byGoal := make(map[string][]db.ListKeyResultLatestReadingsRow, len(goals))
	for _, r := range rows {
		key := util.UUIDToString(r.GoalID)
		byGoal[key] = append(byGoal[key], r)
	}
	out := make([]GoalView, 0, len(goals))
	for _, g := range goals {
		view := GoalView{Goal: g}
		states := make([]KeyResultState, 0)
		for _, r := range byGoal[util.UUIDToString(g.ID)] {
			kr := KeyResultView{
				ID: util.UUIDToString(r.KeyResultID), GoalID: util.UUIDToString(r.GoalID), Title: r.Title,
				MetricKey: r.MetricKey, ScopeType: r.ScopeType, ScopeID: r.ScopeID, Weight: int(r.Weight),
			}
			kr.StartValue, _ = floatFromNumeric(r.StartValue)
			kr.TargetValue, _ = floatFromNumeric(r.TargetValue)
			state := KeyResultState{Weight: int(r.Weight)}
			if cur, ok := floatFromNumeric(r.CurrentValue); ok {
				progress := KeyResultProgress(kr.StartValue, kr.TargetValue, cur)
				kr.Current = &cur
				kr.Progress = &progress
				if r.ObservedAt.Valid {
					at := r.ObservedAt.Time
					kr.ObservedAt = &at
				}
				state.Progress = progress
				state.HasRead = true
			} else {
				view.ReadingsMissing++
			}
			states = append(states, state)
			view.KeyResults = append(view.KeyResults, kr)
		}
		if progress, ok := GoalProgress(states); ok {
			view.Progress = &progress
		}
		view.Derived = ConfidenceUnknown
		if g.Status != GoalStatusDraft && len(states) > 0 {
			expected := ExpectedShare(g.PeriodStart.Time, g.PeriodEnd.Time, now)
			progress := 0.0
			if view.Progress != nil {
				progress = *view.Progress
			}
			view.Derived = DeriveConfidence(progress, expected, view.ReadingsMissing == 0)
		}
		var override *string
		if g.ConfidenceOverride.Valid {
			o := g.ConfidenceOverride.String
			override = &o
		}
		view.Effective = EffectiveConfidence(view.Derived, override)
		view.Overridden = override != nil
		out = append(out, view)
	}
	return out
}

// dateOf is a calendar date at UTC midnight, as the goal's date columns store it.
func dateOf(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func governanceActorType(t string) string {
	switch t {
	case ReadingActorAgent:
		return GovernanceActorAgent
	case ReadingActorMember:
		return GovernanceActorMember
	}
	return GovernanceActorSystem
}

func uuidOrEmpty(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return util.UUIDToString(u)
}
