package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The rules of a standing approval, one refusal per row. evaluatePolicy is pure:
// the caller supplies the weekly count, the remaining budget and the company state.
func TestEvaluatePolicyRefusesEachWayAndApprovesTheRest(t *testing.T) {
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	squad := "00000000-0000-4000-8000-0000000000aa"
	otherSquad := "00000000-0000-4000-8000-0000000000bb"
	floor := "00000000-0000-4000-8000-0000000000cc"
	cost := func(v int64) *int64 { return &v }
	ok := func(d int) pgtype.Date {
		return pgtype.Date{Time: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC).AddDate(0, 0, d), Valid: true}
	}
	fuid := func(s string) pgtype.UUID { return mustUUID(t, s) }

	base := policyRule{Kind: proposalkinds.KeyDecision, MaxPerWeek: 3}
	filing := policyFiling{
		SquadID: fuid(squad), FloorID: fuid(floor), SquadLevel: 2, Kind: proposalkinds.KeyDecision,
		Payload: []byte(`{}`), Now: now,
	}
	with := func(f func(*policyRule, *policyFiling)) (policyRule, policyFiling) {
		r, fi := base, filing
		f(&r, &fi)
		return r, fi
	}
	run := func(r policyRule, f policyFiling, week int64, remaining *int64, paused bool) string {
		return evaluatePolicy(r, f, week, remaining, paused)
	}
	remainingOf := func(v int64) *int64 { return &v }

	type tc struct {
		name   string
		rule   policyRule
		f      policyFiling
		week   int64
		budget *int64
		paused bool
		want   string
	}
	r1, f1 := with(func(r *policyRule, f *policyFiling) {})
	cases := []tc{
		{name: "a plain match approves", rule: r1, f: f1, want: ""},
		{name: "a paused company refuses everything", rule: r1, f: f1, paused: true, want: proposalkinds.PolicyRefusalPaused},
		{name: "an expired policy", rule: func() policyRule { r := base; r.ValidUntil = ok(-1); return r }(), f: f1, want: proposalkinds.PolicyRefusalExpired},
		{name: "a policy ending today still applies", rule: func() policyRule { r := base; r.ValidUntil = ok(0); return r }(), f: f1, want: ""},
		{name: "another kind", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyOpportunity; return r }(), f: f1, want: proposalkinds.PolicyRefusalKind},
		{name: "urgent", rule: r1, f: func() policyFiling { f := f1; f.Urgent = true; return f }(), want: proposalkinds.PolicyRefusalUrgent},
		{name: "a policy naming tags approves a filing that carries them", rule: func() policyRule { r := base; r.Match.Tags = []string{"docs"}; return r }(), f: func() policyFiling { f := f1; f.Tags = []string{"docs", "q4"}; return f }(), want: ""},
		{name: "a policy naming tags refuses a filing without them", rule: func() policyRule { r := base; r.Match.Tags = []string{"docs", "q4"}; return r }(), f: func() policyFiling { f := f1; f.Tags = []string{"docs"}; return f }(), want: proposalkinds.PolicyRefusalTags},
		{name: "org_change is never approved", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyOrgChange; return r }(), f: func() policyFiling { f := f1; f.Kind = proposalkinds.KeyOrgChange; return f }(), want: proposalkinds.PolicyRefusalForbidden},
		{name: "a launch gate is never approved", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyStageGate; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyStageGate
			f.Payload = []byte(`{"initiative_id":"11111111-1111-1111-1111-111111111111","from":"launch","to":"operate"}`)
			return f
		}(), want: proposalkinds.PolicyRefusalForbidden},
		{name: "a stage gate off the launch stage is eligible", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyStageGate; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyStageGate
			f.Payload = []byte(`{"initiative_id":"11111111-1111-1111-1111-111111111111","from":"define","to":"plan"}`)
			return f
		}(), want: ""},
		{name: "a budget raise with no readable increase is refused", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyBudget; return r }(), f: func() policyFiling { f := f1; f.Kind = proposalkinds.KeyBudget; f.EstimateMicros = nil; return f }(), want: proposalkinds.PolicyRefusalCostUnknown},
		{name: "a budget raise within the remaining budget is eligible", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyBudget; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyBudget
			est := int64(40)
			f.EstimateMicros = &est
			return f
		}(), budget: remainingOf(50), want: ""},
		{name: "a budget raise above the remaining budget is refused", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyBudget; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyBudget
			est := int64(60)
			f.EstimateMicros = &est
			return f
		}(), budget: remainingOf(50), want: proposalkinds.PolicyRefusalBudget},
		{name: "an L1 squad is not eligible", rule: r1, f: func() policyFiling { f := f1; f.SquadLevel = 1; return f }(), want: proposalkinds.PolicyRefusalAutonomy},
		{name: "an opportunity needs L3", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyOpportunity; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyOpportunity
			f.SquadLevel = 2
			f.FirstStage = "validate"
			f.Payload = []byte(`{"thesis":"x","evidence_doc_ids":["a"]}`)
			return f
		}(), want: proposalkinds.PolicyRefusalAutonomy},
		{name: "an L3 opportunity at the first stage approves", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyOpportunity; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyOpportunity
			f.SquadLevel = 3
			f.FirstStage = "validate"
			f.Payload = []byte(`{"thesis":"x","evidence_doc_ids":["a"]}`)
			return f
		}(), want: ""},
		{name: "an L3 opportunity past the first stage is refused", rule: func() policyRule { r := base; r.Kind = proposalkinds.KeyOpportunity; return r }(), f: func() policyFiling {
			f := f1
			f.Kind = proposalkinds.KeyOpportunity
			f.SquadLevel = 3
			f.FirstStage = "validate"
			f.Payload = []byte(`{"thesis":"x","evidence_doc_ids":["a"],"target_stage":"build"}`)
			return f
		}(), want: proposalkinds.PolicyRefusalStage},
		{name: "a squad outside the policy's list", rule: func() policyRule { r := base; r.Match.SquadIDs = []string{otherSquad}; return r }(), f: f1, want: proposalkinds.PolicyRefusalSquad},
		{name: "a squad in the policy's list approves", rule: func() policyRule { r := base; r.Match.SquadIDs = []string{squad}; return r }(), f: f1, want: ""},
		{name: "a floor outside the policy's list", rule: func() policyRule { r := base; r.Match.FloorIDs = []string{otherSquad}; return r }(), f: f1, want: proposalkinds.PolicyRefusalFloor},
		{name: "a squad on no floor cannot match a floor list", rule: func() policyRule { r := base; r.Match.FloorIDs = []string{floor}; return r }(), f: func() policyFiling { f := f1; f.FloorID = pgtype.UUID{}; return f }(), want: proposalkinds.PolicyRefusalFloor},
		{name: "a payload that differs", rule: func() policyRule { r := base; r.Match.PayloadEquals = map[string]any{"market": "eu"}; return r }(), f: func() policyFiling { f := f1; f.Payload = []byte(`{"market":"us"}`); return f }(), want: proposalkinds.PolicyRefusalPayload},
		{name: "a payload that matches", rule: func() policyRule {
			r := base
			r.Match.PayloadEquals = map[string]any{"market": "eu", "n": 2.0}
			return r
		}(), f: func() policyFiling { f := f1; f.Payload = []byte(`{"market":"eu","n":2}`); return f }(), want: ""},
		{name: "a cost with no estimate cannot meet a cost limit", rule: func() policyRule { r := base; r.Match.MaxCostMicros = cost(100); return r }(), f: f1, want: proposalkinds.PolicyRefusalCostUnknown},
		{name: "a cost over the limit", rule: func() policyRule { r := base; r.Match.MaxCostMicros = cost(100); return r }(), f: func() policyFiling { f := f1; f.EstimateMicros = cost(101); return f }(), budget: remainingOf(1000), want: proposalkinds.PolicyRefusalCost},
		{name: "a cost at the limit approves", rule: func() policyRule { r := base; r.Match.MaxCostMicros = cost(100); return r }(), f: func() policyFiling { f := f1; f.EstimateMicros = cost(100); return f }(), budget: remainingOf(1000), want: ""},
		{name: "a costed filing with no budget read", rule: r1, f: func() policyFiling { f := f1; f.EstimateMicros = cost(5); return f }(), want: proposalkinds.PolicyRefusalBudgetUnknown},
		{name: "a cost over the remaining budget", rule: r1, f: func() policyFiling { f := f1; f.EstimateMicros = cost(500); return f }(), budget: remainingOf(499), want: proposalkinds.PolicyRefusalBudget},
		{name: "a cost within the remaining budget", rule: r1, f: func() policyFiling { f := f1; f.EstimateMicros = cost(499); return f }(), budget: remainingOf(499), want: ""},
		{name: "the weekly cap is used up", rule: func() policyRule { r := base; r.MaxPerWeek = 2; return r }(), f: f1, week: 2, want: proposalkinds.PolicyRefusalCap},
		{name: "one approval left in the week", rule: func() policyRule { r := base; r.MaxPerWeek = 2; return r }(), f: f1, week: 1, want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := run(c.rule, c.f, c.week, c.budget, c.paused)
			if got != c.want {
				t.Fatalf("refusal = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOpportunityFirstStageReadsTheTargetOrTheDefault(t *testing.T) {
	f := policyFiling{Kind: proposalkinds.KeyOpportunity, FirstStage: "validate", Payload: []byte(`{"thesis":"x"}`)}
	if !opportunityAtFirstStage(f) {
		t.Error("no target stage means the default first stage")
	}
	f.FirstStage = ""
	if opportunityAtFirstStage(f) {
		t.Error("a workspace with no pipeline cannot approve an initiative")
	}
}

func TestPayloadMatchesComparesJSONValues(t *testing.T) {
	if !payloadMatches([]byte(`{"a":1,"b":"x"}`), map[string]any{"a": 1.0}) {
		t.Error("a subset of the payload should match")
	}
	if payloadMatches([]byte(`{"a":"1"}`), map[string]any{"a": 1.0}) {
		t.Error("a string is not the number it looks like")
	}
	if payloadMatches([]byte(`{}`), map[string]any{"a": 1.0}) {
		t.Error("a missing key should not match")
	}
	if payloadMatches([]byte(`not json`), map[string]any{"a": 1.0}) {
		t.Error("an unreadable payload should not match")
	}
}

// policyEnv is a workspace with an owner, a plain member and squads, and a
// proposal service whose budget reader is set per test.
type policyEnv struct {
	t        *testing.T
	fx       *testutil.Fixture
	q        *db.Queries
	ws       pgtype.UUID
	wsID     string
	owner    pgtype.UUID
	ownerID  string
	member   pgtype.UUID
	agent    pgtype.UUID
	props    *ProposalService
	policies *ApprovalPolicyService
	budget   *fakeBudget
}

// fakeBudget is a squad budget reader with one answer for every squad.
type fakeBudget struct {
	remaining int64
	bounded   bool
}

func (f *fakeBudget) RemainingSquadBudgetMicros(context.Context, *db.Queries, pgtype.UUID, pgtype.UUID) (int64, bool, error) {
	return f.remaining, f.bounded, nil
}

func newPolicyEnv(t *testing.T) *policyEnv {
	t.Helper()
	pool := sharedTestPool(t)
	slug := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	fx := testutil.New(pool, "", "")
	ownerID := fx.User(t, "Policy Owner", "policy-owner-"+slug+"@example.test")
	memberID := fx.User(t, "Policy Member", "policy-member-"+slug+"@example.test")
	fx = testutil.New(pool, "", ownerID)
	wsID := fx.Workspace(t, "Policy WS", "policy-"+slug)
	fx = testutil.New(pool, wsID, ownerID)
	fx.Member(t, wsID, ownerID, "owner")
	fx.Member(t, wsID, memberID, "member")
	agentID := fx.Agent(t, "leader", "")
	q := db.New(pool)
	issues := NewIssueService(q, pool, nil, nil, nil)
	_, props := NewInitiativeSet(issues, nil)
	budget := &fakeBudget{}
	props.Budget = budget
	return &policyEnv{
		t: t, fx: fx, q: q, ws: mustUUID(t, wsID), wsID: wsID,
		owner: mustUUID(t, ownerID), ownerID: ownerID, member: mustUUID(t, memberID), agent: mustUUID(t, agentID),
		props:    props,
		policies: &ApprovalPolicyService{Queries: q, TxStarter: pool, Kinds: props.registry(), Budget: budget},
		budget:   budget,
	}
}

// squad makes a squad at the given level.
func (e *policyEnv) squad(name string, level int) pgtype.UUID {
	id := e.fx.Squad(e.t, name, util.UUIDToString(e.agent))
	e.fx.Exec(e.t, `UPDATE squad SET autonomy_level = $2 WHERE id = $1`, id, level)
	return mustUUID(e.t, id)
}

func (e *policyEnv) boss() GovernanceActor {
	return GovernanceActor{Type: GovernanceActorMember, ID: e.owner}
}

func (e *policyEnv) file(squad pgtype.UUID, kind, payload string, urgent bool) ProposalCreateResult {
	e.t.Helper()
	params := ProposalCreateParams{
		WorkspaceID: e.ws, SquadID: squad,
		Content: ProposalContent{
			Title: "Draft the research note", Summary: "A short note.", Evidence: []string{"https://example.test/a"},
			Recommendation: "Publish it.", Options: []string{"Publish", "Wait"}, CostRisk: "Low.", Ask: "Approve?",
		},
		CreatorType: "member", CreatorID: e.owner, Kind: kind, Payload: []byte(payload),
	}
	if urgent {
		params.Urgent = true
		params.UrgentReason = "Needed today."
	}
	res, err := e.props.Create(context.Background(), params)
	if err != nil {
		e.t.Fatalf("file: %v", err)
	}
	return res
}

func (e *policyEnv) createPolicy(spec proposalkinds.PolicySpec) db.ApprovalPolicy {
	e.t.Helper()
	p, err := e.policies.Create(context.Background(), e.ws, e.boss(), spec)
	if err != nil {
		e.t.Fatalf("create policy: %v", err)
	}
	return p
}

func (e *policyEnv) countGovernance(kind string) int {
	e.t.Helper()
	page, err := NewGovernanceLog().List(context.Background(), e.q, e.ws, GovernanceFilter{Kind: kind, Limit: 200})
	if err != nil {
		e.t.Fatal(err)
	}
	return len(page.Events)
}

func decisionPolicy(name string, squads ...string) proposalkinds.PolicySpec {
	return proposalkinds.PolicySpec{Name: name, Kind: proposalkinds.KeyDecision, Match: proposalkinds.PolicyMatch{SquadIDs: squads}}
}

func TestStandingApprovalApprovesAnL2FilingAsThePolicy(t *testing.T) {
	e := newPolicyEnv(t)
	sq := e.squad("Trusted squad", AutonomyLevelTrusted)
	policy := e.createPolicy(decisionPolicy("Routine notes", e.squadString(sq)))

	res := e.file(sq, "", `{}`, false)
	if res.AutoDecision == nil {
		t.Fatalf("an L2 squad's matching filing was not approved: state %q", res.Proposal.State)
	}
	if res.Proposal.State != ProposalStateApproved || res.Issue.Status != "done" {
		t.Fatalf("proposal %q, issue %q; want approved and done", res.Proposal.State, res.Issue.Status)
	}
	if !res.Proposal.DecidedByPolicyID.Valid || res.Proposal.DecidedByPolicyID != policy.ID {
		t.Fatalf("decided_by_policy_id = %v, want the policy", res.Proposal.DecidedByPolicyID)
	}
	if res.Proposal.DecidedBy.Valid {
		t.Fatal("a policy decision records no member as the decider")
	}
	if n := e.countGovernance(GovernanceKindPolicyAutoApprove); n != 1 {
		t.Fatalf("policy_auto_approve entries = %d, want 1", n)
	}
	page, _ := NewGovernanceLog().List(context.Background(), e.q, e.ws, GovernanceFilter{Kind: GovernanceKindPolicyAutoApprove, Limit: 5})
	if page.Events[0].ActorType != GovernanceActorPolicy || page.Events[0].ActorID != policy.ID {
		t.Fatalf("the entry's actor is %s %v, want policy:%v", page.Events[0].ActorType, page.Events[0].ActorID, policy.ID)
	}
}

func TestStandingApprovalNeverAppliesToAnL1Squad(t *testing.T) {
	e := newPolicyEnv(t)
	sq := e.squad("Work squad", AutonomyLevelWork)
	e.createPolicy(decisionPolicy("Any", e.squadString(sq)))
	res := e.file(sq, "", `{}`, false)
	if res.AutoDecision != nil || res.Proposal.State != ProposalStatePending {
		t.Fatalf("an L1 squad's filing was approved by a policy: %+v", res.Proposal.State)
	}
	if n := e.countGovernance(GovernanceKindPolicyAutoApprove); n != 0 {
		t.Fatalf("an unapproved filing wrote %d auto-approve entries", n)
	}
}

func TestStandingApprovalRefusesUrgentFilings(t *testing.T) {
	e := newPolicyEnv(t)
	sq := e.squad("Urgent squad", AutonomyLevelTrusted)
	e.createPolicy(decisionPolicy("Any"))
	res := e.file(sq, "", `{}`, true)
	if res.AutoDecision != nil || res.Proposal.State != ProposalStatePending {
		t.Fatalf("an urgent filing was approved by a policy: state %q", res.Proposal.State)
	}
}

func TestWeeklyCapLimitsAutoApprovalsAcrossFilings(t *testing.T) {
	e := newPolicyEnv(t)
	first := e.squad("First L2", AutonomyLevelTrusted)
	second := e.squad("Second L2", AutonomyLevelTrusted)
	one := int32(1)
	spec := decisionPolicy("One a week")
	spec.MaxPerWeek = &one
	e.createPolicy(spec)

	if res := e.file(first, "", `{}`, false); res.AutoDecision == nil {
		t.Fatal("the first filing of the week was not approved")
	}
	if res := e.file(second, "", `{}`, false); res.AutoDecision != nil {
		t.Fatal("a second approval came in a week that allows one")
	}
	views, err := e.policies.List(context.Background(), e.ws)
	if err != nil || len(views) != 1 || views[0].WeekApprovals != 1 {
		t.Fatalf("list: %+v, %v; want one policy with one approval this week", views, err)
	}
}

func TestStandingApprovalRefusesCostsOverTheLimitAndUnboundedBudgets(t *testing.T) {
	e := newPolicyEnv(t)
	limit := int64(100)
	e.createPolicy(proposalkinds.PolicySpec{Name: "Small", Kind: proposalkinds.KeyDecision, Match: proposalkinds.PolicyMatch{MaxCostMicros: &limit}})

	est := func(v int64) *int64 { return &v }
	file := func(squad pgtype.UUID, cost *int64) ProposalCreateResult {
		e.t.Helper()
		params := ProposalCreateParams{
			WorkspaceID: e.ws, SquadID: squad,
			Content: ProposalContent{
				Title: "Costed", Summary: "s", Evidence: []string{"https://example.test"},
				Recommendation: "r", Options: []string{"o"}, CostRisk: "c", Ask: "a",
			},
			CreatorType: "member", CreatorID: e.owner, EstimatedCostMicros: cost,
		}
		res, err := e.props.Create(context.Background(), params)
		if err != nil {
			e.t.Fatal(err)
		}
		return res
	}

	over := e.squad("Over limit", AutonomyLevelTrusted)
	e.budget.bounded, e.budget.remaining = true, 1000
	if res := file(over, est(101)); res.AutoDecision != nil {
		t.Fatal("a cost over the policy's limit was approved")
	}
	none := e.squad("No estimate", AutonomyLevelTrusted)
	if res := file(none, nil); res.AutoDecision != nil {
		t.Fatal("a policy with a cost limit approved a filing with no estimate")
	}
	unbounded := e.squad("No budget", AutonomyLevelTrusted)
	e.budget.bounded = false
	if res := file(unbounded, est(5)); res.AutoDecision != nil {
		t.Fatal("a costed filing was approved for a squad with no budget")
	}
	within := e.squad("Within", AutonomyLevelTrusted)
	e.budget.bounded, e.budget.remaining = true, 1000
	if res := file(within, est(50)); res.AutoDecision == nil {
		t.Fatal("a cost within the limit and the budget was not approved")
	}
}

func TestRevokeStopsThePolicyInOneStepAndIsRecorded(t *testing.T) {
	e := newPolicyEnv(t)
	policy := e.createPolicy(decisionPolicy("Revocable"))
	if _, err := e.policies.Revoke(context.Background(), e.ws, policy.ID, e.boss()); err != nil {
		t.Fatal(err)
	}
	sq := e.squad("After revoke", AutonomyLevelTrusted)
	if res := e.file(sq, "", `{}`, false); res.AutoDecision != nil {
		t.Fatal("a revoked policy approved a filing")
	}
	// A second revoke changes nothing and writes no second entry.
	if _, err := e.policies.Revoke(context.Background(), e.ws, policy.ID, e.boss()); err != nil {
		t.Fatal(err)
	}
	if n := e.countGovernance(GovernanceKindPolicyRevoked); n != 1 {
		t.Fatalf("policy_revoked entries = %d, want 1", n)
	}
	if _, err := e.policies.Revoke(context.Background(), e.ws, mustUUID(t, "00000000-0000-4000-8000-0000000000ff"), e.boss()); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("an unknown policy: err = %v, want ErrPolicyNotFound", err)
	}
}

func TestAnAgentCannotCreateOrRevokeAPolicy(t *testing.T) {
	e := newPolicyEnv(t)
	ctx := context.Background()
	agent := GovernanceActor{Type: GovernanceActorAgent, ID: e.agent}
	member := GovernanceActor{Type: GovernanceActorMember, ID: e.member}

	if _, err := e.policies.Create(ctx, e.ws, agent, decisionPolicy("By an agent")); !errors.Is(err, ErrGovernanceActorNotBoss) {
		t.Fatalf("an agent creating a policy: err = %v, want ErrGovernanceActorNotBoss", err)
	}
	if _, err := e.policies.Create(ctx, e.ws, member, decisionPolicy("By a member")); !errors.Is(err, ErrGovernanceActorNotBoss) {
		t.Fatalf("a plain member creating a policy: err = %v, want ErrGovernanceActorNotBoss", err)
	}
	policy := e.createPolicy(decisionPolicy("Owned by the boss"))
	if _, err := e.policies.Revoke(ctx, e.ws, policy.ID, agent); !errors.Is(err, ErrGovernanceActorNotBoss) {
		t.Fatalf("an agent revoking a policy: err = %v, want ErrGovernanceActorNotBoss", err)
	}
	views, err := e.policies.List(ctx, e.ws)
	if err != nil || len(views) != 1 || !views[0].Policy.Active {
		t.Fatalf("after the refusals: %+v, %v; want the one policy still active", views, err)
	}
}

// Budget and stage gate policies can be made: a budget raise is judged by the
// remaining budget and a stage gate by its stage, per filing (see evaluatePolicy).
func TestAPolicyCannotBeMadeForForbiddenKinds(t *testing.T) {
	e := newPolicyEnv(t)
	for _, kind := range []string{proposalkinds.KeyOrgChange, proposalkinds.KeyStandingApproval} {
		spec := decisionPolicy("Forbidden " + kind)
		spec.Kind = kind
		var invalid *ApprovalPolicyInvalidError
		if _, err := e.policies.Create(context.Background(), e.ws, e.boss(), spec); !errors.As(err, &invalid) {
			t.Errorf("a policy for %q: err = %v, want an invalid policy", kind, err)
		}
	}
}

func TestStandingApprovalProposalCreatesThePolicyOnApproval(t *testing.T) {
	e := newPolicyEnv(t)
	sq := e.squad("Proposing squad", AutonomyLevelTrusted)
	payload := `{"name":"Routine notes","kind":"decision","match":{"squad_ids":["` + e.squadString(sq) + `"]}}`
	filed := e.file(sq, proposalkinds.KeyStandingApproval, payload, false)
	if filed.AutoDecision != nil || filed.Proposal.State != ProposalStatePending {
		t.Fatalf("a standing_approval filing was decided on filing: %q", filed.Proposal.State)
	}
	views, _ := e.policies.List(context.Background(), e.ws)
	if len(views) != 0 {
		t.Fatal("a policy existed before the boss approved the proposal")
	}

	if _, err := e.props.Decide(context.Background(), ProposalDecideParams{
		WorkspaceID: e.ws, ProposalID: filed.Proposal.ID, DeciderID: e.owner, Decision: ProposalDecisionApprove,
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	views, err := e.policies.List(context.Background(), e.ws)
	if err != nil || len(views) != 1 || views[0].Policy.Name != "Routine notes" {
		t.Fatalf("after approval: %+v, %v; want the policy the proposal asked for", views, err)
	}
	if !views[0].Policy.CreatedBy.Valid || views[0].Policy.CreatedBy != e.owner {
		t.Fatal("the policy's creator is the approving boss")
	}
	if n := e.countGovernance(GovernanceKindPolicyCreated); n != 1 {
		t.Fatalf("policy_created entries = %d, want 1", n)
	}
}

func TestReversedAutoApprovalsFlagThePolicyForTheWeeklyReview(t *testing.T) {
	e := newPolicyEnv(t)
	ctx := context.Background()
	sq := e.squad("Reviewed squad", AutonomyLevelTrusted)
	policy := e.createPolicy(decisionPolicy("Reviewed"))
	res := e.file(sq, "", `{}`, false)
	if res.AutoDecision == nil {
		t.Fatal("setup: the filing was not auto-approved")
	}
	since := time.Now().Add(-time.Hour)
	if got, err := e.policies.ListReversedAutoApprovals(ctx, e.ws, since); err != nil || len(got) != 0 {
		t.Fatalf("before a reversal: %+v, %v", got, err)
	}
	if err := e.policies.ReverseAutoApproval(ctx, e.ws, res.Proposal.ID, "  "); !errors.Is(err, ErrReversalNoteRequired) {
		t.Fatalf("a reversal with no note: err = %v, want ErrReversalNoteRequired", err)
	}
	if err := e.policies.ReverseAutoApproval(ctx, e.ws, res.Proposal.ID, "The initiative was not needed."); err != nil {
		t.Fatalf("reverse: %v", err)
	}
	got, err := e.policies.ListReversedAutoApprovals(ctx, e.ws, since)
	if err != nil || len(got) != 1 || got[0].PolicyID != policy.ID || got[0].ReversalNote != "The initiative was not needed." {
		t.Fatalf("reversed list: %+v, %v", got, err)
	}
	if err := e.policies.ReverseAutoApproval(ctx, e.ws, res.Proposal.ID, "again"); !errors.Is(err, ErrAutoApprovalNotFound) {
		t.Fatalf("a second reversal: err = %v, want ErrAutoApprovalNotFound", err)
	}
	views, err := e.policies.List(ctx, e.ws)
	if err != nil || len(views) != 1 || !views[0].Flagged {
		t.Fatalf("the policy list must show the flag: %+v, %v", views, err)
	}
	history, flagged, err := e.policies.AutoApprovals(ctx, e.ws, policy.ID, 10)
	if err != nil || !flagged || len(history) != 1 || history[0].ReversedAt == nil {
		t.Fatalf("history: %+v flagged=%v err=%v", history, flagged, err)
	}
}

func TestRecentAutoApprovalsFeedTheDigest(t *testing.T) {
	e := newPolicyEnv(t)
	e.createPolicy(decisionPolicy("Digest"))
	sq := e.squad("Digest squad", AutonomyLevelTrusted)
	res := e.file(sq, "", `{}`, false)
	if res.AutoDecision == nil {
		t.Fatal("setup: not auto-approved")
	}
	got, err := e.policies.ListRecentAutoApprovals(context.Background(), e.ws, time.Now().Add(-time.Hour), 20, nil)
	if err != nil || len(got) != 1 || got[0].PolicyName != "Digest" || got[0].ProposalID != res.Proposal.ID {
		t.Fatalf("recent auto-approvals: %+v, %v", got, err)
	}
}

func TestPausedCompanyStopsStandingApprovals(t *testing.T) {
	e := newPolicyEnv(t)
	sq := e.squad("Paused squad", AutonomyLevelTrusted)
	e.createPolicy(decisionPolicy("While paused"))
	e.fx.Exec(e.t, `UPDATE workspace SET company_state = 'paused' WHERE id = $1`, e.wsID)
	if res := e.file(sq, "", `{}`, false); res.AutoDecision != nil {
		t.Fatal("a standing approval applied while the company was paused")
	}
}

func (e *policyEnv) squadString(id pgtype.UUID) string {
	return util.UUIDToString(id)
}
