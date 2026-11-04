package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The enforcement table (spec 4.3), cell by cell, at every level and every run class.
func TestAutonomyEnforcementTable(t *testing.T) {
	classes := []RunClass{RunClassHuman, RunClassRoutine, RunClassDelegated, RunClassExempt}
	for level := 0; level <= 3; level++ {
		for _, class := range classes {
			v := AutonomyRunVerdict(level, class)
			wantHeld := level == AutonomyLevelSuggest && class == RunClassRoutine
			if held := !v.Admit; held != wantHeld {
				t.Errorf("L%d %s: held=%v, want %v", level, class, held, wantHeld)
			}
			if wantHeld && v.Reason != VerdictReasonAutonomyL0 {
				t.Errorf("L%d routine: reason %q, want %q", level, v.Reason, VerdictReasonAutonomyL0)
			}
		}
		if got, want := AutonomyAllowsAutoPublish(level), level >= 2; got != want {
			t.Errorf("L%d auto-publish: %v, want %v", level, got, want)
		}
		if got, want := AutonomyAllowsStandingApproval(level, proposalkinds.KeyDecision), level >= 2; got != want {
			t.Errorf("L%d standing approval of a decision: %v, want %v", level, got, want)
		}
		if got, want := AutonomyAllowsStandingApproval(level, proposalkinds.KeyOpportunity), level >= 3; got != want {
			t.Errorf("L%d standing approval of an opportunity: %v, want %v", level, got, want)
		}
		if got, want := AutonomyAllowsInitiativeFromOpportunity(level), level >= 3; got != want {
			t.Errorf("L%d initiative from an opportunity: %v, want %v", level, got, want)
		}
		if AutonomyAllowsLaunch(level) || AutonomyAllowsOrgChange(level) {
			t.Errorf("L%d: launch or org change depends on the level; both are always the boss's", level)
		}
	}
}

func TestAutonomyLevelNamesAndRange(t *testing.T) {
	names := map[int]string{0: "Suggest", 1: "Work", 2: "Trusted", 3: "Autonomous"}
	for level, want := range names {
		if got := AutonomyLevelName(level); got != want {
			t.Errorf("level %d name = %q, want %q", level, got, want)
		}
		if !ValidAutonomyLevel(level) {
			t.Errorf("level %d should be valid", level)
		}
	}
	for _, level := range []int{-1, 4} {
		if ValidAutonomyLevel(level) || AutonomyLevelName(level) != "" {
			t.Errorf("level %d should be refused", level)
		}
	}
}

// autonomyEnv is a workspace with an owner, a plain member, an agent and squads.
type autonomyEnv struct {
	t       *testing.T
	fx      *testutil.Fixture
	ws      pgtype.UUID
	wsID    string
	owner   pgtype.UUID
	ownerID string
	member  pgtype.UUID
	agent   pgtype.UUID
	squad   pgtype.UUID
	squadID string
	q       *db.Queries
	svc     *AutonomyService
}

func newAutonomyEnv(t *testing.T) *autonomyEnv {
	t.Helper()
	pool := sharedTestPool(t)
	fx := testutil.New(pool, "", "")
	ownerID := fx.User(t, "Autonomy Owner", "autonomy-owner-"+strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))+"@example.test")
	fx = testutil.New(pool, "", ownerID)
	wsID := fx.Workspace(t, "Autonomy WS", "autonomy-"+strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")))
	fx = testutil.New(pool, wsID, ownerID)
	fx.Member(t, wsID, ownerID, "owner")
	memberID := fx.User(t, "Plain Member", "plain-member-"+strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))+"@example.test")
	fx.Member(t, wsID, memberID, "member")
	agentID := fx.Agent(t, "leader", "")
	squadID := fx.Squad(t, "Research squad", agentID)
	q := db.New(pool)
	return &autonomyEnv{
		t: t, fx: fx, ws: mustUUID(t, wsID), wsID: wsID,
		owner: mustUUID(t, ownerID), ownerID: ownerID, member: mustUUID(t, memberID),
		agent: mustUUID(t, agentID), squad: mustUUID(t, squadID), squadID: squadID,
		q: q, svc: &AutonomyService{Queries: q, TxStarter: pool},
	}
}

func (e *autonomyEnv) boss() GovernanceActor {
	return GovernanceActor{Type: GovernanceActorMember, ID: e.owner}
}

func (e *autonomyEnv) autonomyEvents(t *testing.T) []db.GovernanceEvent {
	t.Helper()
	page, err := NewGovernanceLog().List(context.Background(), e.q, e.ws, GovernanceFilter{Kind: GovernanceKindAutonomyChanged, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return page.Events
}

func TestAutonomyChangeIsBossOnlyLoggedAndReasoned(t *testing.T) {
	e := newAutonomyEnv(t)
	ctx := context.Background()

	// The default for an existing squad is L1.
	view, err := e.svc.Get(ctx, e.ws, e.squad)
	if err != nil || view.Level != AutonomyLevelWork || view.Reason != "" || view.ChangedAt != nil {
		t.Fatalf("default view = %+v, %v; want L1 with no reason", view, err)
	}

	view, err = e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelTrusted, e.boss(), "Two clean weeks of reviewed drafts.")
	if err != nil || view.Level != AutonomyLevelTrusted {
		t.Fatalf("boss change: %+v, %v", view, err)
	}
	if view.Reason != "Two clean weeks of reviewed drafts." || view.ChangedAt == nil {
		t.Fatalf("the view must show the reason and time of the last change: %+v", view)
	}
	events := e.autonomyEvents(t)
	if len(events) != 1 || events[0].ActorType != GovernanceActorMember {
		t.Fatalf("autonomy events = %+v, want one member entry", events)
	}
	if !strings.Contains(events[0].Summary, "L1 Work to L2 Trusted") {
		t.Errorf("summary = %q", events[0].Summary)
	}

	// The same level again changes nothing and writes nothing.
	if _, err := e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelTrusted, e.boss(), "again"); err != nil {
		t.Fatal(err)
	}
	if n := len(e.autonomyEvents(t)); n != 1 {
		t.Fatalf("an unchanged level wrote %d entries in total, want 1", n)
	}
}

func TestAutonomyChangeRefusesAgentsMembersAndBadInput(t *testing.T) {
	e := newAutonomyEnv(t)
	ctx := context.Background()

	agent := GovernanceActor{Type: GovernanceActorAgent, ID: e.agent}
	if _, err := e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelAutonomous, agent, "agents never change levels"); !errors.Is(err, ErrGovernanceActorNotBoss) {
		t.Fatalf("an agent: err = %v, want ErrGovernanceActorNotBoss", err)
	}
	plain := GovernanceActor{Type: GovernanceActorMember, ID: e.member}
	if _, err := e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelAutonomous, plain, "a plain member"); !errors.Is(err, ErrGovernanceActorNotBoss) {
		t.Fatalf("a plain member: err = %v, want ErrGovernanceActorNotBoss", err)
	}
	if _, err := e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelTrusted, e.boss(), "  "); !errors.Is(err, ErrAutonomyReasonRequired) {
		t.Fatalf("no reason: err = %v, want ErrAutonomyReasonRequired", err)
	}
	if _, err := e.svc.Set(ctx, e.ws, e.squad, 4, e.boss(), "out of range"); !errors.Is(err, ErrAutonomyLevelInvalid) {
		t.Fatalf("level 4: err = %v, want ErrAutonomyLevelInvalid", err)
	}
	if _, err := e.svc.Set(ctx, e.ws, e.agent, AutonomyLevelTrusted, e.boss(), "not a squad"); !errors.Is(err, ErrAutonomySquadNotFound) {
		t.Fatalf("a non-squad id: err = %v, want ErrAutonomySquadNotFound", err)
	}

	// Nothing above changed the level, and no refused change was logged.
	view, err := e.svc.Get(ctx, e.ws, e.squad)
	if err != nil || view.Level != AutonomyLevelWork {
		t.Fatalf("after refusals: %+v, %v; want L1", view, err)
	}
	if n := len(e.autonomyEvents(t)); n != 0 {
		t.Fatalf("refused changes wrote %d governance entries", n)
	}
}

func TestAutonomyChangeToAnArchivedSquadIsNotFound(t *testing.T) {
	e := newAutonomyEnv(t)
	e.fx.Exec(e.t, `UPDATE squad SET archived_at = now() WHERE id = $1`, e.squadID)
	if _, err := e.svc.Set(context.Background(), e.ws, e.squad, AutonomyLevelTrusted, e.boss(), "archived"); !errors.Is(err, ErrAutonomySquadNotFound) {
		t.Fatalf("archived squad: err = %v, want ErrAutonomySquadNotFound", err)
	}
}

// The L0 hold at autopilot dispatch: only routine runs of an L0 squad's own
// autopilots are held. A person's run and an agent's delegated run are not.
func TestL0HoldsOnlyRoutineRunsOfASquadAutopilot(t *testing.T) {
	e := newAutonomyEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelSuggest, e.boss(), "Hold the routines while we review the charter."); err != nil {
		t.Fatal(err)
	}
	s := &AutopilotService{Queries: e.q}
	routine := db.Autopilot{WorkspaceID: e.ws, AssigneeType: "squad", AssigneeID: e.squad}

	reason, code, held := s.autonomyHold(ctx, routine, "schedule")
	if !held || code != dispatch.ReasonAutonomyL0 || reason == "" {
		t.Fatalf("a scheduled run of an L0 squad: held=%v code=%q reason=%q", held, code, reason)
	}
	if _, _, held := s.autonomyHold(ctx, routine, "webhook"); !held {
		t.Error("a webhook run of an L0 squad was not held")
	}
	if _, _, held := s.autonomyHold(ctx, routine, "manual"); held {
		t.Error("a manual run (a person's) was held; L0 runs when someone asks")
	}
	if _, _, held := s.autonomyHold(ctx, db.Autopilot{WorkspaceID: e.ws, AssigneeType: "agent", AssigneeID: e.agent}, "schedule"); held {
		t.Error("an agent-assigned autopilot was held; only squad routines are held here")
	}

	if _, err := e.svc.Set(ctx, e.ws, e.squad, AutonomyLevelWork, e.boss(), "Back to Work."); err != nil {
		t.Fatal(err)
	}
	if _, _, held := s.autonomyHold(ctx, routine, "schedule"); held {
		t.Error("an L1 squad's routine was held")
	}
}
