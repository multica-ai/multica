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

	if _, ok := byName["loop:escalate-stalled"]; !ok {
		t.Fatal("escalation rule missing")
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

func TestCompile_RequiresAgentAndSkill(t *testing.T) {
	if _, err := Compile(goodSpec(t), CompileParams{}); err == nil {
		t.Fatal("expected error for missing agent_id and build_skill")
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
}

func TestPlanningDispatchRule_CustomStatus(t *testing.T) {
	rule := PlanningDispatchRule("agent-1", "build", "backlog")
	tc := rule.TriggerConfig.(workflows.TriggerConfigStatusChanged)
	if tc.ToStatus != "backlog" {
		t.Fatalf("expected custom planning status, got %q", tc.ToStatus)
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
