package loops

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

func goodSpec(t *testing.T) *Spec {
	t.Helper()
	s, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("fixture spec invalid: %v", err)
	}
	return s
}

func TestCompile_ProducesGateDispatchEscalate(t *testing.T) {
	rules, err := Compile(goodSpec(t), CompileParams{AgentID: "agent-1", BuildSkill: "build"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	byName := map[string]Rule{}
	for _, r := range rules {
		byName[r.Name] = r
	}

	dispatch, ok := byName["loop:dispatch-build"]
	if !ok || dispatch.ActionType != workflows.ActionRunSkill {
		t.Fatalf("dispatch rule missing or wrong action: %+v", dispatch)
	}
	rs, ok := dispatch.ActionConfig.(workflows.ActionConfigRunSkill)
	if !ok || rs.SkillName != "build" || rs.AgentID != "agent-1" {
		t.Fatalf("dispatch run_skill config wrong: %+v", dispatch.ActionConfig)
	}

	gate, ok := byName["loop:delivery-gate"]
	if !ok || gate.ActionType != workflows.ActionSetStatus {
		t.Fatalf("gate rule missing or wrong action: %+v", gate)
	}
	if got := gate.ActionConfig.(workflows.ActionConfigSetStatus).Status; got != "done" {
		t.Fatalf("gate should set done, got %q", got)
	}
	if len(gate.Conditions) != 1 || gate.Conditions[0].Op != CheckGateOp {
		t.Fatalf("gate must carry one %s condition: %+v", CheckGateOp, gate.Conditions)
	}
	cfg, ok := gate.Conditions[0].Value.(CheckGateConfig)
	if !ok || len(cfg.Checks) != 1 {
		t.Fatalf("gate check config wrong: %+v", gate.Conditions[0].Value)
	}
	if cfg.Checks[0][0] != "pytest" {
		t.Fatalf("gate check argv not carried through: %+v", cfg.Checks)
	}
	if len(cfg.JudgeChecks) != 1 || cfg.JudgeChecks[0].ID != "note-explains" {
		t.Fatalf("gate judge check not carried through: %+v", cfg.JudgeChecks)
	}
	if cfg.JudgeAgentID != "agent-1" {
		t.Fatalf("judge agent should default to the worker agent, got %q", cfg.JudgeAgentID)
	}
	if cfg.JudgeSkill != "build" {
		t.Fatalf("judge skill should default to the build skill, got %q", cfg.JudgeSkill)
	}
	if cfg.RevertStatus != "in_progress" {
		t.Fatalf("delivery gate should revert to the build status on revise, got %q", cfg.RevertStatus)
	}

	if _, ok := byName["loop:escalate-stalled"]; !ok {
		t.Fatal("escalation rule missing")
	}
}

// TestCompile_MultiPhase covers FIR-2283 followup point 6: a spec with Phases
// drives the dispatch-build rule from phase 0 and carries every phase's checks
// on the delivery gate so the evaluator can chain build→review pairs.
func TestCompile_MultiPhase(t *testing.T) {
	spec := &Spec{
		Version: SpecVersion,
		Caps:    Caps{MaxIterations: 5, MaxRevisions: 3, NoProgressStalls: 2},
		Phases: []BuildPhase{
			{Name: "Backend", BuildSkill: "build-be", Verification: []Verification{
				{ID: "be-tests", Type: CheckProgrammatic, Check: []string{"make", "test-be"}},
			}},
			{Name: "Frontend", BuildSkill: "build-fe", Verification: []Verification{
				{ID: "fe-tests", Type: CheckProgrammatic, Check: []string{"pnpm", "test"}},
			}},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("multi-phase spec should validate: %v", err)
	}
	rules, err := Compile(spec, CompileParams{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	byName := map[string]Rule{}
	for _, r := range rules {
		byName[r.Name] = r
	}

	rs := byName["loop:dispatch-build"].ActionConfig.(workflows.ActionConfigRunSkill)
	if rs.SkillName != "build-be" {
		t.Fatalf("dispatch-build should run phase 0's skill, got %q", rs.SkillName)
	}

	gate := byName["loop:delivery-gate"]
	var cfg CheckGateConfig
	for _, c := range gate.Conditions {
		if c.Op == CheckGateOp {
			cfg = c.Value.(CheckGateConfig)
		}
	}
	if len(cfg.Phases) != 2 {
		t.Fatalf("gate should carry 2 phases, got %d", len(cfg.Phases))
	}
	if cfg.Phases[0].BuildSkill != "build-be" || cfg.Phases[1].BuildSkill != "build-fe" {
		t.Fatalf("phase build skills wrong: %+v", cfg.Phases)
	}
	if len(cfg.Phases[0].Checks) != 1 || cfg.Phases[0].Checks[0][0] != "make" {
		t.Fatalf("phase 0 checks wrong: %+v", cfg.Phases[0].Checks)
	}
	if len(cfg.Checks) != 0 {
		t.Fatalf("top-level Checks should be empty in multi-phase, got %+v", cfg.Checks)
	}
	if cfg.RevertStatus != "in_progress" {
		t.Fatalf("gate revert status wrong: %q", cfg.RevertStatus)
	}
}

// TestValidate_MultiPhase_RequiresProgrammaticAndSkill locks the per-phase
// validation: each phase needs a build skill and at least one programmatic
// check (same trust rule as the single-phase delivery gate).
func TestValidate_MultiPhase_RequiresProgrammaticAndSkill(t *testing.T) {
	noSkill := &Spec{
		Version: SpecVersion,
		Caps:    Caps{MaxIterations: 5, MaxRevisions: 3, NoProgressStalls: 2},
		Phases:  []BuildPhase{{Verification: []Verification{{ID: "t", Type: CheckProgrammatic, Check: []string{"make", "test"}}}}},
	}
	if err := noSkill.Validate(); err == nil {
		t.Fatal("phase without build_skill should fail validation")
	}

	judgeOnly := &Spec{
		Version: SpecVersion,
		Caps:    Caps{MaxIterations: 5, MaxRevisions: 3, NoProgressStalls: 2},
		Phases:  []BuildPhase{{BuildSkill: "b", Verification: []Verification{{ID: "j", Type: CheckJudge, Rubric: "looks good"}}}},
	}
	if err := judgeOnly.Validate(); err == nil {
		t.Fatal("phase with only a judge check should fail validation (needs a programmatic gate)")
	}
}

// TestCompile_PlanGate_RevertStatusIsPlanningStatus covers FIR-2283 v2 slice
// 2d (Jesper's proposal: a revising gate's action is to move the status
// back). The plan gate must revert to the PLANNING status, not the build
// status, so a failing "adversarial review of the plan" visibly sends the
// issue back to Plan.
func TestCompile_PlanGate_RevertStatusIsPlanningStatus(t *testing.T) {
	spec := goodSpec(t)
	spec.Planning = true
	spec.PlanGate = []Verification{
		{ID: "adversarial-review", Type: CheckJudge, Rubric: "The plan survives an adversarial critique"},
	}

	rules, err := Compile(spec, CompileParams{
		AgentID:        "agent-1",
		BuildSkill:     "build",
		PlanSkill:      "plan",
		PlanningStatus: "backlog",
	})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, r := range rules {
		if r.Name != "loop:dispatch-build" {
			continue
		}
		cfg := r.Conditions[0].Value.(CheckGateConfig)
		if cfg.RevertStatus != "backlog" {
			t.Fatalf("plan gate should revert to the planning status, got %q", cfg.RevertStatus)
		}
	}
}

// TestCompile_CustomJudgeAgentAndSkill proves a caller can route judge checks
// to a distinct agent/skill instead of the worker-fallback default, which is
// what makes a blind review possible (a different session grading the work).
func TestCompile_CustomJudgeAgentAndSkill(t *testing.T) {
	rules, err := Compile(goodSpec(t), CompileParams{
		AgentID:      "agent-1",
		BuildSkill:   "build",
		JudgeAgentID: "judge-agent",
		JudgeSkill:   "judge-skill",
	})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, r := range rules {
		if r.Name != "loop:delivery-gate" {
			continue
		}
		cfg := r.Conditions[0].Value.(CheckGateConfig)
		if cfg.JudgeAgentID != "judge-agent" {
			t.Fatalf("expected custom judge agent, got %q", cfg.JudgeAgentID)
		}
		if cfg.JudgeSkill != "judge-skill" {
			t.Fatalf("expected custom judge skill, got %q", cfg.JudgeSkill)
		}
	}
}

func TestCompile_RequiresBuildSkill(t *testing.T) {
	if _, err := Compile(goodSpec(t), CompileParams{}); err == nil {
		t.Fatal("expected error for missing build_skill")
	}
}

// TestCompile_AgentIDOptional locks in FIR-2283 v2: a recipe may leave its
// build agent unset so it stays reusable across issues with different
// owners. The compiled dispatch/judge-fallback config simply carries the
// empty agent through — workflows.actionRunSkill resolves it to the
// triggering issue's assignee at dispatch time (see CompileParams.AgentID).
func TestCompile_AgentIDOptional(t *testing.T) {
	rules, err := Compile(goodSpec(t), CompileParams{BuildSkill: "build"})
	if err != nil {
		t.Fatalf("expected compile to succeed with no agent_id, got: %v", err)
	}
	for _, r := range rules {
		if r.Name != "loop:dispatch-build" {
			continue
		}
		rs := r.ActionConfig.(workflows.ActionConfigRunSkill)
		if rs.AgentID != "" {
			t.Fatalf("expected empty agent_id to pass through, got %q", rs.AgentID)
		}
	}
}

func TestCompile_RejectsInvalidSpec(t *testing.T) {
	if _, err := Compile(&Spec{}, CompileParams{AgentID: "a", BuildSkill: "b"}); err == nil {
		t.Fatal("expected error compiling an invalid spec")
	}
}

func planningSpec(t *testing.T) *Spec {
	t.Helper()
	s := goodSpec(t)
	s.Planning = true
	return s
}

func TestCompile_PlanningMode_Prepends_PlanningDispatch(t *testing.T) {
	rules, err := Compile(planningSpec(t), CompileParams{AgentID: "agent-1", BuildSkill: "build"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// Planning adds one rule: 4 total.
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules with planning, got %d", len(rules))
	}
	if rules[0].Name != "loop:planning-dispatch" {
		t.Fatalf("first rule should be planning-dispatch, got %q", rules[0].Name)
	}
	tc := rules[0].TriggerConfig.(workflows.TriggerConfigStatusChanged)
	if tc.ToStatus != "todo" {
		t.Fatalf("planning dispatch default status should be todo, got %q", tc.ToStatus)
	}
	ac := rules[0].ActionConfig.(workflows.ActionConfigRunSkill)
	if ac.SkillName != "build" {
		t.Fatalf("planning dispatch should default PlanSkill to BuildSkill, got %q", ac.SkillName)
	}
}

func TestCompile_PlanningMode_CustomPlanSkillAndStatus(t *testing.T) {
	rules, err := Compile(planningSpec(t), CompileParams{
		AgentID:        "agent-1",
		BuildSkill:     "build",
		PlanSkill:      "plan-skill",
		PlanningStatus: "backlog",
	})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if rules[0].Name != "loop:planning-dispatch" {
		t.Fatalf("first rule should be planning-dispatch")
	}
	tc := rules[0].TriggerConfig.(workflows.TriggerConfigStatusChanged)
	if tc.ToStatus != "backlog" {
		t.Fatalf("expected custom planning status %q, got %q", "backlog", tc.ToStatus)
	}
	ac := rules[0].ActionConfig.(workflows.ActionConfigRunSkill)
	if ac.SkillName != "plan-skill" {
		t.Fatalf("expected custom plan skill, got %q", ac.SkillName)
	}
}

func TestCompile_NonPlanningSpec_NoPlanningDispatch(t *testing.T) {
	rules, err := Compile(goodSpec(t), CompileParams{AgentID: "a", BuildSkill: "b"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, r := range rules {
		if r.Name == "loop:planning-dispatch" {
			t.Fatal("planning-dispatch should not appear for non-planning spec")
		}
	}
}

func TestPlanningDispatchRule_Standalone(t *testing.T) {
	rule := PlanningDispatchRule("agent-1", "build", "")
	if rule.Name != "loop:planning-dispatch" {
		t.Fatalf("expected loop:planning-dispatch, got %q", rule.Name)
	}
	tc, ok := rule.TriggerConfig.(workflows.TriggerConfigStatusChanged)
	if !ok || tc.ToStatus != "todo" {
		t.Fatalf("expected default planning status todo, got %+v", rule.TriggerConfig)
	}
	if rule.ActionType != workflows.ActionRunSkill {
		t.Fatalf("expected run_skill action, got %q", rule.ActionType)
	}
	ac, ok := rule.ActionConfig.(workflows.ActionConfigRunSkill)
	if !ok || ac.SkillName != "build" || ac.AgentID != "agent-1" {
		t.Fatalf("action config wrong: %+v", rule.ActionConfig)
	}
	// FIR-2283 followup point 3 — the planning dispatch must mark the run as
	// plan mode so the action runner injects the plan-mode instruction.
	if !ac.PlanMode {
		t.Fatal("planning-dispatch action config should have PlanMode=true")
	}
}

func TestPlanningDispatchRule_CustomStatus(t *testing.T) {
	rule := PlanningDispatchRule("agent-1", "build", "backlog")
	tc := rule.TriggerConfig.(workflows.TriggerConfigStatusChanged)
	if tc.ToStatus != "backlog" {
		t.Fatalf("expected custom planning status, got %q", tc.ToStatus)
	}
}

// TestCompile_IssueIDScopesEveryRule locks in FIR-2283 v2 point 8: compiling
// with an IssueID must append an issue.id/eq condition to every rule
// (dispatch, gate, escalate, and — with planning — the planning-dispatch
// rule too), so the materialized workflow only ever fires for that one
// issue instead of every issue in the project.
func TestCompile_IssueIDScopesEveryRule(t *testing.T) {
	rules, err := Compile(planningSpec(t), CompileParams{
		IssueID:    "issue-123",
		AgentID:    "agent-1",
		BuildSkill: "build",
	})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(rules))
	}
	for _, r := range rules {
		found := false
		for _, c := range r.Conditions {
			if c.Field == issueScopeField && c.Op == issueScopeOp && c.Value == "issue-123" {
				found = true
			}
		}
		if !found {
			t.Fatalf("rule %q missing issue.id scope condition: %+v", r.Name, r.Conditions)
		}
	}
	// The delivery gate must carry the scope condition ALONGSIDE its
	// check_passes condition, not instead of it.
	for _, r := range rules {
		if r.Name != "loop:delivery-gate" {
			continue
		}
		if len(r.Conditions) != 2 {
			t.Fatalf("expected delivery-gate to carry 2 conditions (scope + check_passes), got %+v", r.Conditions)
		}
	}
}

// TestCompile_NoIssueID_StaysProjectWide proves the default (no IssueID) is
// unchanged: no scope condition is added, so an existing project-wide recipe
// keeps firing for every issue in the project exactly as before.
func TestCompile_NoIssueID_StaysProjectWide(t *testing.T) {
	rules, err := Compile(goodSpec(t), CompileParams{AgentID: "agent-1", BuildSkill: "build"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, r := range rules {
		for _, c := range r.Conditions {
			if c.Field == issueScopeField {
				t.Fatalf("rule %q should carry no issue scope condition when IssueID is empty: %+v", r.Name, r.Conditions)
			}
		}
	}
}

// TestCompile_PlanGate_AttachesToDispatchBuild covers FIR-2283 v2 point 6:
// a spec with Planning=true and a non-empty PlanGate must carry a
// check_passes condition on loop:dispatch-build (its own gate key — see
// gate_evaluator.go), whose RevisionSkill is the PLAN skill, not the build
// skill, so a failing plan gate re-dispatches planning.
func TestCompile_PlanGate_AttachesToDispatchBuild(t *testing.T) {
	spec := goodSpec(t)
	spec.Planning = true
	spec.PlanGate = []Verification{
		{ID: "adversarial-review", Type: CheckJudge, Rubric: "The plan survives an adversarial critique"},
	}

	rules, err := Compile(spec, CompileParams{
		AgentID:    "agent-1",
		BuildSkill: "build",
		PlanSkill:  "plan",
	})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	var dispatch Rule
	found := false
	for _, r := range rules {
		if r.Name == "loop:dispatch-build" {
			dispatch = r
			found = true
		}
	}
	if !found {
		t.Fatal("loop:dispatch-build rule missing")
	}
	if len(dispatch.Conditions) != 1 || dispatch.Conditions[0].Op != CheckGateOp {
		t.Fatalf("expected loop:dispatch-build to carry one %s condition, got: %+v", CheckGateOp, dispatch.Conditions)
	}
	cfg, ok := dispatch.Conditions[0].Value.(CheckGateConfig)
	if !ok {
		t.Fatalf("condition value is not a CheckGateConfig: %+v", dispatch.Conditions[0].Value)
	}
	if len(cfg.JudgeChecks) != 1 || cfg.JudgeChecks[0].ID != "adversarial-review" {
		t.Fatalf("plan gate judge check not carried through: %+v", cfg.JudgeChecks)
	}
	if cfg.RevisionSkill != "plan" {
		t.Fatalf("plan gate should revise with the plan skill, got %q", cfg.RevisionSkill)
	}

	// The delivery gate's OWN check_passes condition must be untouched by the
	// plan gate — different gate key, different checks.
	for _, r := range rules {
		if r.Name != "loop:delivery-gate" {
			continue
		}
		deliveryCfg := r.Conditions[0].Value.(CheckGateConfig)
		if len(deliveryCfg.JudgeChecks) != 1 || deliveryCfg.JudgeChecks[0].ID != "note-explains" {
			t.Fatalf("delivery gate judge checks should be unaffected by the plan gate: %+v", deliveryCfg.JudgeChecks)
		}
	}
}

// TestCompile_NoPlanGate_DispatchBuildUnconditional proves the default (no
// PlanGate, or Planning false) leaves loop:dispatch-build exactly as before:
// no check_passes condition, build dispatches unconditionally on entering
// BuildStatus.
func TestCompile_NoPlanGate_DispatchBuildUnconditional(t *testing.T) {
	rules, err := Compile(planningSpec(t), CompileParams{AgentID: "agent-1", BuildSkill: "build"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, r := range rules {
		if r.Name != "loop:dispatch-build" {
			continue
		}
		if len(r.Conditions) != 0 {
			t.Fatalf("expected no conditions on loop:dispatch-build without a plan gate, got: %+v", r.Conditions)
		}
	}
}

func TestCompile_DefaultStatuses(t *testing.T) {
	rules, err := Compile(goodSpec(t), CompileParams{AgentID: "a", BuildSkill: "b"})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for _, r := range rules {
		tc := r.TriggerConfig.(workflows.TriggerConfigStatusChanged)
		switch r.Name {
		case "loop:dispatch-build":
			if tc.ToStatus != "in_progress" {
				t.Fatalf("dispatch default status wrong: %q", tc.ToStatus)
			}
		case "loop:delivery-gate", "loop:escalate-stalled":
			if tc.ToStatus != "in_review" {
				t.Fatalf("%s default status wrong: %q", r.Name, tc.ToStatus)
			}
		}
	}
}
