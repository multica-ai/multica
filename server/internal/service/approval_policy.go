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

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Standing approvals (spec 4.4). A policy approves a narrow class of proposals
// as the system actor policy:<id>, applies each exactly as a human approval would,
// and is written to the governance log. The engine runs after a filing commits,
// under the workspace proposal lock, so the weekly cap cannot be passed by
// concurrent filings. A policy that cannot apply leaves the proposal pending for
// the boss, which is the safe outcome.

// StandingApprovalWindow is the span the weekly cap counts over.
const StandingApprovalWindow = 7 * 24 * time.Hour

// PolicyActorPrefix names the system actor of a policy's decision: policy:<id>.
const PolicyActorPrefix = "policy:"

var (
	// ErrPolicyNotFound: no policy of that id in the workspace.
	ErrPolicyNotFound = errors.New("approval policy not found")
	// ErrAutoApprovalNotFound: no auto-approved proposal of that id, or it is
	// already reversed.
	ErrAutoApprovalNotFound = errors.New("auto-approved proposal not found")
	// ErrReversalNoteRequired: a reversal names no reason.
	ErrReversalNoteRequired = errors.New("a reversal needs a note saying why")
)

// ApprovalPolicyInvalidError is a policy spec the rules refuse. Message is for
// the caller.
type ApprovalPolicyInvalidError struct{ Message string }

func (e *ApprovalPolicyInvalidError) Error() string { return e.Message }

// SquadBudgetReader is the read the policy engine needs from the budget work
// (spec 4.2): the squad's remaining budget in micros, read inside the caller's
// transaction through q. bounded is false when the squad has no budget. A
// costed filing is then refused, because money the boss has not bounded cannot be
// approved on the boss's behalf. The budget service wires this in; until then the
// ProposalService's Budget is nil, and every costed filing waits for the boss.
type SquadBudgetReader interface {
	RemainingSquadBudgetMicros(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID) (remaining int64, bounded bool, err error)
}

// ApprovalPolicyService creates, revokes and reads policies, and decides filings.
type ApprovalPolicyService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	// Kinds validates a policy's kind. Nil means the decision and opportunity
	// kinds are unknown and every policy is refused.
	Kinds  *proposalkinds.Registry
	Budget SquadBudgetReader
	Now    func() time.Time
}

func (s *ApprovalPolicyService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *ApprovalPolicyService) kindKnown(key string) bool {
	return s.Kinds != nil && s.Kinds.Has(key)
}

// policyRule is a policy as the engine reads it.
type policyRule struct {
	ID         pgtype.UUID
	Kind       string
	Match      proposalkinds.PolicyMatch
	MaxPerWeek int32
	ValidUntil pgtype.Date
}

// policyFiling is what the engine knows about a filing.
type policyFiling struct {
	WorkspaceID pgtype.UUID
	ProposalID  pgtype.UUID
	SquadID     pgtype.UUID
	FloorID     pgtype.UUID
	// SquadLevel is the filing squad's autonomy level.
	SquadLevel int
	Kind       string
	Urgent     bool
	Payload    []byte
	// EstimateMicros is the filing's cost estimate, nil when it gave none.
	EstimateMicros *int64
	// FirstStage is the key of the default pipeline's first stage, "" when none.
	FirstStage string
	Now        time.Time
	// Tags are the proposal's tags (ProposalTags at filing).
	Tags []string
}

// evaluatePolicy applies one policy to one filing and returns the refusal reason,
// or "" when the policy approves. It is pure: the caller reads the weekly count,
// the remaining budget (nil when unknown or unbounded) and the company state.
// The order is the order the spec states the rules in.
func evaluatePolicy(rule policyRule, f policyFiling, weekCount int64, remaining *int64, paused bool) string {
	if paused {
		return proposalkinds.PolicyRefusalPaused
	}
	if rule.ValidUntil.Valid {
		today := time.Date(f.Now.Year(), f.Now.Month(), f.Now.Day(), 0, 0, 0, 0, time.UTC)
		if today.After(rule.ValidUntil.Time) {
			return proposalkinds.PolicyRefusalExpired
		}
	}
	if rule.Kind != f.Kind {
		return proposalkinds.PolicyRefusalKind
	}
	if reason := proposalkinds.ForbiddenForStandingApproval(f.Kind, f.Urgent, f.Payload); reason != "" {
		return reason
	}
	if !AutonomyAllowsStandingApproval(f.SquadLevel, f.Kind) {
		return proposalkinds.PolicyRefusalAutonomy
	}
	// A budget request is judged by its increase over the budget's amount. An
	// increase that cannot be read is refused, not waved through.
	if f.Kind == proposalkinds.KeyBudget && f.EstimateMicros == nil {
		return proposalkinds.PolicyRefusalCostUnknown
	}
	if f.Kind == proposalkinds.KeyOpportunity && !opportunityAtFirstStage(f) {
		return proposalkinds.PolicyRefusalStage
	}
	m := rule.Match
	if len(m.SquadIDs) > 0 && !containsString(m.SquadIDs, util.UUIDToString(f.SquadID)) {
		return proposalkinds.PolicyRefusalSquad
	}
	if len(m.FloorIDs) > 0 && (!f.FloorID.Valid || !containsString(m.FloorIDs, util.UUIDToString(f.FloorID))) {
		return proposalkinds.PolicyRefusalFloor
	}
	if !hasAllTags(f.Tags, m.Tags) {
		return proposalkinds.PolicyRefusalTags
	}
	if len(m.PayloadEquals) > 0 && !payloadMatches(f.Payload, m.PayloadEquals) {
		return proposalkinds.PolicyRefusalPayload
	}
	if m.MaxCostMicros != nil {
		if f.EstimateMicros == nil {
			return proposalkinds.PolicyRefusalCostUnknown
		}
		if *f.EstimateMicros > *m.MaxCostMicros {
			return proposalkinds.PolicyRefusalCost
		}
	}
	if f.EstimateMicros != nil {
		if remaining == nil {
			return proposalkinds.PolicyRefusalBudgetUnknown
		}
		if *f.EstimateMicros > *remaining {
			return proposalkinds.PolicyRefusalBudget
		}
	}
	if weekCount >= int64(rule.MaxPerWeek) {
		return proposalkinds.PolicyRefusalCap
	}
	return ""
}

// opportunityAtFirstStage reports whether an opportunity starts at the default
// pipeline's first stage (spec 4.3, L3). An opportunity names no stage by default.
func opportunityAtFirstStage(f policyFiling) bool {
	if f.FirstStage == "" {
		return false
	}
	var o proposalkinds.Opportunity
	if err := json.Unmarshal(f.Payload, &o); err != nil {
		return false
	}
	target := o.TargetStage
	if target == "" {
		target = proposalkinds.DefaultOpportunityStage
	}
	return target == f.FirstStage
}

// payloadMatches reports whether every key of want is present in the payload with
// an equal JSON value.
func payloadMatches(payload []byte, want map[string]any) bool {
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		return false
	}
	for key, w := range want {
		g, ok := got[key]
		if !ok {
			return false
		}
		gb, err1 := json.Marshal(g)
		wb, err2 := json.Marshal(w)
		if err1 != nil || err2 != nil || string(gb) != string(wb) {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// decide finds the policy that approves a filing, reading through q, which is
// bound to the decision transaction. It returns the approving policy, or nil with
// the refusal reason of the most relevant policy: the first one of the filing's
// kind. A workspace with no policy of that kind refuses with no_policy.
func (s *ApprovalPolicyService) decide(ctx context.Context, q *db.Queries, f policyFiling) (*db.ApprovalPolicy, string, error) {
	paused, err := CompanyIsPaused(ctx, q, f.WorkspaceID)
	if err != nil {
		return nil, "", err
	}
	active, err := q.ListActiveApprovalPolicies(ctx, f.WorkspaceID)
	if err != nil {
		return nil, "", fmt.Errorf("list approval policies: %w", err)
	}
	first := ""
	for i := range active {
		row := active[i]
		if row.Kind != f.Kind {
			continue
		}
		rule, err := ruleOf(row)
		if err != nil {
			return nil, "", err
		}
		weekCount, err := q.CountPolicyAutoApprovalsSince(ctx, db.CountPolicyAutoApprovalsSinceParams{
			WorkspaceID: f.WorkspaceID, PolicyID: row.ID, Since: pgtype.Timestamptz{Time: f.Now.Add(-StandingApprovalWindow), Valid: true},
		})
		if err != nil {
			return nil, "", fmt.Errorf("count policy approvals: %w", err)
		}
		remaining, err := s.remainingBudget(ctx, q, f)
		if err != nil {
			return nil, "", err
		}
		reason := evaluatePolicy(rule, f, weekCount, remaining, paused)
		if reason == "" {
			return &row, "", nil
		}
		if first == "" {
			first = reason
		}
	}
	if first == "" {
		first = proposalkinds.PolicyRefusalNoPolicy
	}
	return nil, first, nil
}

// remainingBudget reads the squad's remaining budget when the filing has a cost
// estimate. It returns nil when there is no estimate (no budget is needed), when
// no reader is wired, or when the squad has no budget; the rules then refuse a
// costed filing.
func (s *ApprovalPolicyService) remainingBudget(ctx context.Context, q *db.Queries, f policyFiling) (*int64, error) {
	if f.EstimateMicros == nil || s.Budget == nil {
		return nil, nil
	}
	remaining, bounded, err := s.Budget.RemainingSquadBudgetMicros(ctx, q, f.WorkspaceID, f.SquadID)
	if err != nil {
		return nil, fmt.Errorf("read squad budget: %w", err)
	}
	if !bounded {
		return nil, nil
	}
	return &remaining, nil
}

// ruleOf converts a stored policy row into the engine's rule.
func ruleOf(row db.ApprovalPolicy) (policyRule, error) {
	var m proposalkinds.PolicyMatch
	if err := json.Unmarshal(row.Match, &m); err != nil {
		return policyRule{}, fmt.Errorf("policy %s match: %w", util.UUIDToString(row.ID), err)
	}
	return policyRule{ID: row.ID, Kind: row.Kind, Match: m, MaxPerWeek: row.MaxPerWeek, ValidUntil: row.ValidUntil}, nil
}

// policyEngine returns the engine a ProposalService uses. It is built from the
// service's own handles, so a service constructed without the policy fields
// still applies policies.
func (s *ProposalService) policyEngine() *ApprovalPolicyService {
	return &ApprovalPolicyService{
		Queries:   s.Issues.Queries,
		TxStarter: s.Issues.TxStarter,
		Kinds:     s.registry(),
		Budget:    s.Budget,
	}
}

// applyStandingApproval runs the standing approval step of a filing. It returns
// the decision when a policy approved the proposal, and nil otherwise. A nil
// result with a nil error leaves the proposal pending for the boss. The steps:
//
//  1. Under the workspace proposal lock, load the proposal, its squad, the first
//     stage and the company state, and find the approving policy.
//  2. Approve the proposal through the same settlement a human approval uses,
//     with the policy as the decider of record, and commit.
//  3. Create the handoff kickoff after the commit, as a human approval does. If
//     it fails the decision is reverted and the proposal waits for the boss.
//  4. Record policy_auto_approve in the governance log once the approval stands.
func (s *ProposalService) applyStandingApproval(ctx context.Context, ws, proposalID pgtype.UUID) (*ProposalDecideResult, error) {
	engine := s.policyEngine()
	// A workspace with no active policy takes no lock: the common case costs one read.
	active, err := s.Issues.Queries.ListActiveApprovalPolicies(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("list approval policies: %w", err)
	}
	if len(active) == 0 {
		return nil, nil
	}

	tx, err := s.Issues.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Issues.Queries.WithTx(tx)

	// The lock the filings and resubmissions hold, so the weekly cap counted in
	// decide cannot be passed by a concurrent filing.
	if err := qtx.LockProposalWorkspace(ctx, ws); err != nil {
		return nil, fmt.Errorf("lock proposal workspace: %w", err)
	}
	current, err := qtx.LockProposalInWorkspace(ctx, db.LockProposalInWorkspaceParams{ID: proposalID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock proposal: %w", err)
	}
	if current.State != ProposalStatePending {
		return nil, nil
	}
	squad, err := qtx.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: current.SquadID, WorkspaceID: ws})
	if err != nil || squad.ArchivedAt.Valid {
		return nil, nil
	}
	firstStage, err := defaultFirstStage(ctx, qtx, ws)
	if err != nil {
		return nil, err
	}
	filing := policyFiling{
		WorkspaceID: ws, ProposalID: proposalID, SquadID: current.SquadID, FloorID: squad.FloorID,
		SquadLevel: int(squad.AutonomyLevel), Kind: current.Kind, Urgent: current.Urgent,
		Payload: current.Payload, FirstStage: firstStage, Now: engine.now(), Tags: current.Tags,
	}
	if current.Kind == proposalkinds.KeyBudget {
		// A budget request's cost is the raise it asks for, read from the budget
		// rather than submitted by the squad.
		estimate, err := budgetRaiseEstimate(ctx, qtx, ws, current.Payload)
		if err != nil {
			return nil, err
		}
		filing.EstimateMicros = estimate
	} else if current.EstimatedCostMicros.Valid {
		v := current.EstimatedCostMicros.Int64
		filing.EstimateMicros = &v
	}
	policy, reason, err := engine.decide(ctx, qtx, filing)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		slog.Debug("standing approval: proposal waits for the boss", "proposal_id", util.UUIDToString(proposalID), "reason", reason)
		return nil, nil
	}

	res, err := s.settleInTx(ctx, tx, qtx, ProposalDecideParams{
		WorkspaceID: ws,
		ProposalID:  proposalID,
		// The policy's creator is the accountable member that the decision's
		// sponsor and handoff creator are attributed to; the decision records the
		// policy itself as its decider.
		DeciderID: policy.CreatedBy,
		Decision:  ProposalDecisionApprove,
		Policy:    &ProposalPolicyDecision{ID: policy.ID},
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	// finishSettle creates the kickoff and reverts the decision if it cannot. The
	// proposal then waits for the boss, as any filing a policy does not approve.
	res, err = s.finishSettle(ctx, ProposalDecideParams{WorkspaceID: ws, ProposalID: proposalID, DeciderID: policy.CreatedBy, Policy: &ProposalPolicyDecision{ID: policy.ID}}, res)
	if err != nil {
		slog.Warn("standing approval: the approval could not start its work; the proposal waits for the boss",
			"proposal_id", util.UUIDToString(proposalID), "error", err)
		return nil, nil
	}
	s.recordPolicyApproval(ctx, policy, current, squad, filing)
	return &res, nil
}

// recordPolicyApproval writes policy_auto_approve after the approval stands. A
// failed write is logged: the approval has committed and the proposal is decided.
func (s *ProposalService) recordPolicyApproval(ctx context.Context, policy *db.ApprovalPolicy, proposal db.Proposal, squad db.Squad, f policyFiling) {
	details := map[string]any{
		"policy_id":   util.UUIDToString(policy.ID),
		"policy_name": policy.Name,
		"proposal_id": util.UUIDToString(proposal.ID),
		"squad_id":    util.UUIDToString(squad.ID),
		"kind":        proposal.Kind,
		"squad_level": f.SquadLevel,
	}
	if f.EstimateMicros != nil {
		details["estimate_micros"] = *f.EstimateMicros
	}
	err := NewGovernanceLog().Record(ctx, s.Issues.Queries, GovernanceEvent{
		WorkspaceID: policy.WorkspaceID,
		Kind:        GovernanceKindPolicyAutoApprove,
		ActorType:   GovernanceActorPolicy,
		ActorID:     policy.ID,
		SubjectType: "proposal",
		SubjectID:   proposal.ID,
		Summary:     fmt.Sprintf("Approved %s from %s under the policy %q.", proposal.Kind, squad.Name, policy.Name),
		Details:     details,
	})
	if err != nil {
		slog.Warn("standing approval: governance record failed", "proposal_id", util.UUIDToString(proposal.ID), "error", err)
	}
}

// defaultFirstStage returns the key of the default pipeline's first stage, or ""
// when the workspace has no default pipeline.
func defaultFirstStage(ctx context.Context, q *db.Queries, ws pgtype.UUID) (string, error) {
	pipe, err := q.GetDefaultPipeline(ctx, ws)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load default pipeline: %w", err)
	}
	var stages []PipelineStage
	if err := decodeStages(pipe.Stages, &stages); err != nil {
		return "", err
	}
	if len(stages) == 0 {
		return "", nil
	}
	return stages[0].Key, nil
}

// CreateApprovalPolicy creates a policy. Only an owner or admin member may; an
// agent is refused here, whatever the handler checked. The rules are validated
// and the policy_created entry is written in the same transaction.
func (s *ApprovalPolicyService) Create(ctx context.Context, ws pgtype.UUID, actor GovernanceActor, spec proposalkinds.PolicySpec) (db.ApprovalPolicy, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	if err := requireGovernanceBoss(ctx, qtx, ws, actor); err != nil {
		return db.ApprovalPolicy{}, err
	}
	spec, err = proposalkinds.ValidatePolicySpec(spec, s.kindKnown, s.now())
	if err != nil {
		return db.ApprovalPolicy{}, &ApprovalPolicyInvalidError{Message: err.Error()}
	}
	policy, err := createPolicyRow(ctx, qtx, ws, spec, actor.ID)
	if err != nil {
		return db.ApprovalPolicy{}, err
	}
	if err := recordPolicyCreated(ctx, qtx, policy, actor); err != nil {
		return db.ApprovalPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("commit: %w", err)
	}
	return policy, nil
}

// createPolicyRow inserts a validated spec. The spec must already be validated.
func createPolicyRow(ctx context.Context, q *db.Queries, ws pgtype.UUID, spec proposalkinds.PolicySpec, creator pgtype.UUID) (db.ApprovalPolicy, error) {
	match, err := json.Marshal(spec.Match)
	if err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("encode match: %w", err)
	}
	var validUntil pgtype.Date
	if spec.ValidUntil != "" {
		day, err := time.Parse("2006-01-02", spec.ValidUntil)
		if err != nil {
			return db.ApprovalPolicy{}, &ApprovalPolicyInvalidError{Message: "valid_until must be a date as YYYY-MM-DD"}
		}
		validUntil = pgtype.Date{Time: day, Valid: true}
	}
	policy, err := q.CreateApprovalPolicy(ctx, db.CreateApprovalPolicyParams{
		ID:          dbid.NewV7(),
		WorkspaceID: ws,
		Name:        spec.Name,
		Kind:        spec.Kind,
		Match:       match,
		MaxPerWeek:  *spec.MaxPerWeek,
		ValidUntil:  validUntil,
		CreatedBy:   creator,
	})
	if err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("create approval policy: %w", err)
	}
	return policy, nil
}

// recordPolicyCreated writes policy_created through q.
func recordPolicyCreated(ctx context.Context, q *db.Queries, policy db.ApprovalPolicy, actor GovernanceActor) error {
	return NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: policy.WorkspaceID,
		Kind:        GovernanceKindPolicyCreated,
		ActorType:   actor.Type,
		ActorID:     actor.ID,
		SubjectType: "approval_policy",
		SubjectID:   policy.ID,
		Summary:     fmt.Sprintf("Created the standing approval %q for %s proposals.", policy.Name, policy.Kind),
		Details:     map[string]any{"kind": policy.Kind, "max_per_week": policy.MaxPerWeek, "match": json.RawMessage(policy.Match)},
	})
}

// Revoke deactivates a policy, boss only. A policy already revoked is returned
// unchanged, with no second entry. The policy_revoked entry is written in the same
// transaction as the change.
func (s *ApprovalPolicyService) Revoke(ctx context.Context, ws, policyID pgtype.UUID, actor GovernanceActor) (db.ApprovalPolicy, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	if err := requireGovernanceBoss(ctx, qtx, ws, actor); err != nil {
		return db.ApprovalPolicy{}, err
	}
	current, err := qtx.LockApprovalPolicyInWorkspace(ctx, db.LockApprovalPolicyInWorkspaceParams{ID: policyID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ApprovalPolicy{}, ErrPolicyNotFound
	}
	if err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("lock approval policy: %w", err)
	}
	revoked, err := revokePolicyIn(ctx, qtx, ws, current, actor)
	if err != nil {
		return db.ApprovalPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("commit: %w", err)
	}
	return revoked, nil
}

// revokePolicyIn deactivates a locked policy inside q's transaction and writes its
// policy_revoked entry. A policy already revoked is returned unchanged, with no
// second entry. The company import uses it to recreate a revoked policy.
func revokePolicyIn(ctx context.Context, q *db.Queries, ws pgtype.UUID, current db.ApprovalPolicy, actor GovernanceActor) (db.ApprovalPolicy, error) {
	if !current.Active {
		return current, nil
	}
	revoked, err := q.RevokeApprovalPolicy(ctx, db.RevokeApprovalPolicyParams{ID: current.ID, WorkspaceID: ws})
	if err != nil {
		return db.ApprovalPolicy{}, fmt.Errorf("revoke approval policy: %w", err)
	}
	if err := NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindPolicyRevoked,
		ActorType:   actor.Type,
		ActorID:     actor.ID,
		SubjectType: "approval_policy",
		SubjectID:   current.ID,
		Summary:     fmt.Sprintf("Revoked the standing approval %q.", revoked.Name),
		Details:     map[string]any{"kind": revoked.Kind},
	}); err != nil {
		return db.ApprovalPolicy{}, err
	}
	return revoked, nil
}

// PolicyView is a policy as the Policies list shows it.
type PolicyView struct {
	Policy db.ApprovalPolicy
	// WeekApprovals counts the auto-approvals of the last seven days.
	WeekApprovals int64
	// Flagged is set when a review reversed an auto-approval of this policy.
	Flagged bool
}

// List returns the workspace's policies, newest first, with their weekly counts and
// flags.
func (s *ApprovalPolicyService) List(ctx context.Context, ws pgtype.UUID) ([]PolicyView, error) {
	rows, err := s.Queries.ListApprovalPolicies(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("list approval policies: %w", err)
	}
	counts, err := s.Queries.CountPolicyAutoApprovalsByPolicy(ctx, db.CountPolicyAutoApprovalsByPolicyParams{
		WorkspaceID: ws, Since: pgtype.Timestamptz{Time: s.now().Add(-StandingApprovalWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("count policy approvals: %w", err)
	}
	week := map[string]int64{}
	for _, c := range counts {
		week[util.UUIDToString(c.PolicyID)] = c.Approvals
	}
	reversals, err := s.Queries.ListPolicyReversals(ctx, db.ListPolicyReversalsParams{
		WorkspaceID: ws, Since: pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list policy reversals: %w", err)
	}
	flagged := map[string]bool{}
	for _, r := range reversals {
		flagged[util.UUIDToString(r.PolicyID)] = true
	}
	out := make([]PolicyView, 0, len(rows))
	for _, row := range rows {
		id := util.UUIDToString(row.ID)
		out = append(out, PolicyView{Policy: row, WeekApprovals: week[id], Flagged: flagged[id]})
	}
	return out, nil
}

// AutoApprovalView is one auto-approval in a policy's history.
type AutoApprovalView struct {
	ProposalID   pgtype.UUID
	SquadID      pgtype.UUID
	Kind         string
	State        string
	Title        string
	IssueNumber  int32
	DecidedAt    time.Time
	ReversedAt   *time.Time
	ReversalNote string
}

// AutoApprovals returns a policy's recent auto-approvals, newest first, and whether
// any of them was reversed.
func (s *ApprovalPolicyService) AutoApprovals(ctx context.Context, ws, policyID pgtype.UUID, limit int32) ([]AutoApprovalView, bool, error) {
	if _, err := s.Queries.GetApprovalPolicyInWorkspace(ctx, db.GetApprovalPolicyInWorkspaceParams{ID: policyID, WorkspaceID: ws}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrPolicyNotFound
		}
		return nil, false, fmt.Errorf("load approval policy: %w", err)
	}
	rows, err := s.Queries.ListPolicyAutoApprovals(ctx, db.ListPolicyAutoApprovalsParams{
		WorkspaceID: ws, PolicyID: policyID, RowLimit: limit,
	})
	if err != nil {
		return nil, false, fmt.Errorf("list auto-approvals: %w", err)
	}
	out := make([]AutoApprovalView, 0, len(rows))
	flagged := false
	for _, r := range rows {
		v := AutoApprovalView{
			ProposalID: r.ID, SquadID: r.SquadID, Kind: r.Kind, State: r.State,
			Title: r.IssueTitle, IssueNumber: r.IssueNumber, DecidedAt: r.DecidedAt.Time,
			ReversalNote: r.ReversalNote,
		}
		if r.ReversedAt.Valid {
			t := r.ReversedAt.Time
			v.ReversedAt = &t
			flagged = true
		}
		out = append(out, v)
	}
	return out, flagged, nil
}

// ReversedAutoApproval is an auto-approval a review reversed.
type ReversedAutoApproval struct {
	ProposalID   pgtype.UUID
	PolicyID     pgtype.UUID
	Title        string
	DecidedAt    time.Time
	ReversedAt   time.Time
	ReversalNote string
}

// ListReversedAutoApprovals returns the auto-approvals reversed at or after since,
// newest reversal first. The weekly review calls it to flag the policies whose
// approvals were reversed.
func (s *ApprovalPolicyService) ListReversedAutoApprovals(ctx context.Context, ws pgtype.UUID, since time.Time) ([]ReversedAutoApproval, error) {
	rows, err := s.Queries.ListPolicyReversals(ctx, db.ListPolicyReversalsParams{
		WorkspaceID: ws, Since: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list reversed auto-approvals: %w", err)
	}
	out := make([]ReversedAutoApproval, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReversedAutoApproval{
			ProposalID: r.ID, PolicyID: r.PolicyID, Title: r.IssueTitle,
			DecidedAt: r.DecidedAt.Time, ReversedAt: r.ReversedAt.Time, ReversalNote: r.ReversalNote,
		})
	}
	return out, nil
}

// ReverseAutoApproval records that a review reversed an auto-approval. Only an
// auto-approved proposal that is not yet reversed can be reversed, and the note
// says why. The reversal flags the policy in the Policies list.
func (s *ApprovalPolicyService) ReverseAutoApproval(ctx context.Context, ws, proposalID pgtype.UUID, note string) error {
	note = strings.TrimSpace(note)
	if note == "" || len([]rune(note)) > GovernanceSummaryMax {
		return ErrReversalNoteRequired
	}
	_, err := s.Queries.ReverseAutoApproval(ctx, db.ReverseAutoApprovalParams{ID: proposalID, WorkspaceID: ws, ReversalNote: note})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAutoApprovalNotFound
	}
	if err != nil {
		return fmt.Errorf("reverse auto-approval: %w", err)
	}
	return nil
}

// RecentAutoApproval is an auto-approval decided recently, for the digest's FYI.
type RecentAutoApproval struct {
	ProposalID  pgtype.UUID
	PolicyID    pgtype.UUID
	PolicyName  string
	SquadID     pgtype.UUID
	Title       string
	IssueNumber int32
	DecidedAt   time.Time
}

// ListRecentAutoApprovals returns the auto-approvals decided at or after since.
// A nil squadIDs reads the whole company; a floor digest passes its floor's squads.
func (s *ApprovalPolicyService) ListRecentAutoApprovals(ctx context.Context, ws pgtype.UUID, since time.Time, limit int32, squadIDs []pgtype.UUID) ([]RecentAutoApproval, error) {
	rows, err := s.Queries.ListRecentPolicyApprovals(ctx, db.ListRecentPolicyApprovalsParams{
		WorkspaceID: ws, Since: pgtype.Timestamptz{Time: since, Valid: true}, RowLimit: limit, SquadIds: squadIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent auto-approvals: %w", err)
	}
	out := make([]RecentAutoApproval, 0, len(rows))
	for _, r := range rows {
		out = append(out, RecentAutoApproval{
			ProposalID: r.ID, PolicyID: r.PolicyID, PolicyName: r.PolicyName, SquadID: r.SquadID,
			Title: r.IssueTitle, IssueNumber: r.IssueNumber, DecidedAt: r.DecidedAt.Time,
		})
	}
	return out, nil
}

// budgetRaiseEstimate is the cost a budget request authorises: how far its new
// amount is above the budget's current amount. It is nil when the payload names no
// budget in this workspace, so the policy refuses the filing (see evaluatePolicy).
func budgetRaiseEstimate(ctx context.Context, q *db.Queries, ws pgtype.UUID, payload []byte) (*int64, error) {
	var req proposalkinds.BudgetRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, nil
	}
	id, err := util.ParseUUID(req.BudgetID)
	if err != nil {
		return nil, nil
	}
	b, err := q.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read budget for raise estimate: %w", err)
	}
	raise := req.AmountMicros - b.AmountMicros
	if raise < 0 {
		raise = 0
	}
	return &raise, nil
}

// hasAllTags reports whether a filing carries every tag the policy names. A
// policy with no tags does not narrow on them.
func hasAllTags(have, want []string) bool {
	for _, w := range want {
		if !containsString(have, w) {
			return false
		}
	}
	return true
}
