package loops

import (
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
)

// CheckGateOp is the condition op a compiled loop uses to gate "done": it
// passes only when every programmatic check in the spec exits zero. It mirrors
// workflows.OpEvidencePresent — the argv lists are carried in the condition's
// Value as a CheckGateConfig — but unlike evidence_present's in-process DB
// lookup, the checks must run in the agent runtime where the repo lives, and
// the result is read back. Wiring that runtime execution into the workflow
// engine is the next slice; Compile pins the contract now so the rest of the
// build can target a stable shape.
const CheckGateOp = "check_passes"

// CheckGateConfig is stored under workflows.Condition.Value when Op is
// CheckGateOp. Checks is the set of argv arrays (one per programmatic
// verification); the gate passes only if all exit zero.
type CheckGateConfig struct {
	Checks [][]string `json:"checks"`
	// AgentID is the worker agent whose runtime runs the checks. The checks
	// must execute in the same checkout the build ran in, so the gate carries
	// the build agent: when the gate enqueues checks, the evaluator dispatches
	// them to this agent's runtime.
	AgentID string `json:"agent_id,omitempty"`
	// Caps are the spec's termination guards, carried into the gate so
	// Store.RecordRevision can enforce them on every GateRevise decision.
	// Zero value means "no caps configured" (never trips) — Compile
	// always fills this from a validated Spec, whose caps are required to be
	// positive, so a real compiled loop always enforces its guards.
	Caps Caps `json:"caps,omitempty"`
	// RevisionSkill is the skill re-dispatched to AgentID when the gate
	// revises (a required check failed). Compile sets it to the same build
	// skill the loop:dispatch-build rule runs, so a revision looks identical
	// to the original build round from the worker's point of view — it is
	// told what failed and asked to fix it.
	RevisionSkill string `json:"revision_skill,omitempty"`
	// JudgeChecks are the spec's judge-type verification criteria: a model
	// scores a rubric and returns a structured pass/revise verdict instead of
	// an exit code. The gate treats a judge check exactly like a programmatic
	// one for advance/revise/wait purposes — every one must report Pass
	// before the gate can advance — but it is scored by JudgeAgentID via
	// JudgeSkill, never run as a command.
	JudgeChecks []JudgeCheck `json:"judge_checks,omitempty"`
	// JudgeAgentID is the agent whose runtime scores judge checks. It should
	// be a different agent or at least a fresh session than AgentID (the
	// worker) so the verdict is blind to the worker's own reasoning — Compile
	// does not enforce that, the caller chooses the judge. Falls back to
	// AgentID when empty so a spec with judge checks but no configured judge
	// still dispatches (self-graded) instead of stalling forever. Per-check
	// judge checks that carry their own AssigneeID (Verification.AssigneeType
	// == AssigneeAgent) use that instead — this is only the fallback.
	JudgeAgentID string `json:"judge_agent_id,omitempty"`
	// JudgeSkill is the skill dispatched to JudgeAgentID for a judge check
	// that does not carry its own Skill.
	JudgeSkill string `json:"judge_skill,omitempty"`
	// HumanChecks are the spec's human-type verification criteria: a person
	// (or an agent standing in for one) must explicitly approve or reject.
	// The gate treats a human check exactly like a programmatic or judge one
	// for advance/revise/wait purposes — every one must report Approved
	// before the gate can advance.
	HumanChecks []HumanCheck `json:"human_checks,omitempty"`
}

// JudgeCheck is one judge-type verification criterion carried into the gate
// config: an ID (matches the spec's Verification.ID, used to key its reported
// outcome), the rubric the judge scores the delivered work against, and an
// optional per-check assignee/skill override. Empty AssigneeID/Skill fall
// back to CheckGateConfig.JudgeAgentID/JudgeSkill.
type JudgeCheck struct {
	ID           string `json:"id"`
	Rubric       string `json:"rubric"`
	AssigneeID   string `json:"assignee_id,omitempty"`
	AssigneeType string `json:"assignee_type,omitempty"`
	Skill        string `json:"skill,omitempty"`
}

// HumanCheck is one human-type verification criterion carried into the gate
// config: an ID (matches the spec's Verification.ID), the prompt shown to the
// assignee, and who must approve — AssigneeType is AssigneeAgent or
// AssigneeMember, AssigneeID the corresponding agent or member id.
type HumanCheck struct {
	ID           string `json:"id"`
	Prompt       string `json:"prompt"`
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
}

// CompileParams supplies the issue-specific bindings the spec itself does not
// carry: which agent worker builds, which skill it runs, and the status names
// that mark the loop's phases.
type CompileParams struct {
	AgentID      string // worker dispatched to build (run_skill)
	BuildSkill   string // skill the worker runs each build round
	BuildStatus  string // status whose entry dispatches a build round
	ReviewStatus string // status the worker sets when it claims a round done
	DoneStatus   string // terminal status, set only when the gate passes

	// Planning-phase params — only used when Spec.Planning is true.
	// PlanSkill is the skill dispatched during the planning phase; if empty,
	// BuildSkill is used (the agent distinguishes the phase from the issue
	// status). PlanningStatus is the status that triggers the planning
	// dispatch; it defaults to "todo" so creating the issue in the default
	// state fires the plan immediately.
	PlanSkill      string
	PlanningStatus string

	// Judge params — only used when the spec has judge-type verification
	// criteria (Spec.JudgeChecks). JudgeAgentID is the agent that scores them;
	// if empty, AgentID is used (the worker judges its own delivery — allowed
	// so a spec with judge checks never silently stalls, but callers that
	// want a blind review must set a distinct JudgeAgentID). JudgeSkill is
	// the skill dispatched to score a judge check; if empty, BuildSkill is
	// used.
	JudgeAgentID string
	JudgeSkill   string
}

func (p *CompileParams) withDefaults() CompileParams {
	out := *p
	if out.BuildStatus == "" {
		out.BuildStatus = "in_progress"
	}
	if out.ReviewStatus == "" {
		out.ReviewStatus = "in_review"
	}
	if out.DoneStatus == "" {
		out.DoneStatus = "done"
	}
	if out.PlanningStatus == "" {
		out.PlanningStatus = "todo"
	}
	if out.PlanSkill == "" {
		out.PlanSkill = out.BuildSkill
	}
	if out.JudgeAgentID == "" {
		out.JudgeAgentID = out.AgentID
	}
	if out.JudgeSkill == "" {
		out.JudgeSkill = out.BuildSkill
	}
	return out
}

// Rule is one compiled workflow rule (trigger -> conditions -> action) in the
// shape the cerebro-workflows engine stores. Config fields use the engine's own
// config structs so a Rule maps directly onto a cerebro_workflow row.
type Rule struct {
	Name          string
	TriggerType   string
	TriggerConfig any
	Conditions    []workflows.Condition
	ActionType    string
	ActionConfig  any
}

// Compile translates a validated loop Spec into the workflow rules that drive
// the loop on the existing engine:
//
//	dispatch  — entering BuildStatus runs the build skill (the worker step)
//	gate      — entering ReviewStatus, if every programmatic check exits zero
//	            AND every judge check reports a passing verdict, sets
//	            DoneStatus; otherwise the issue stays in review
//	escalate  — a stalled issue is walked up to its owner (human stop)
//
// The worker (agent) never decides the loop: the gate is decided by the
// engine from reported exit codes and judge verdicts, and only the engine
// sets DoneStatus.
func Compile(spec *Spec, params CompileParams) ([]Rule, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid loop spec: %w", err)
	}
	var errs []error
	if params.AgentID == "" {
		errs = append(errs, errors.New("params.agent_id is required"))
	}
	if params.BuildSkill == "" {
		errs = append(errs, errors.New("params.build_skill is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	p := params.withDefaults()

	checks := make([][]string, 0, len(spec.Verification))
	for _, v := range spec.ProgrammaticChecks() {
		checks = append(checks, v.Check)
	}

	judgeChecks := make([]JudgeCheck, 0, len(spec.Verification))
	for _, v := range spec.JudgeChecks() {
		judgeChecks = append(judgeChecks, JudgeCheck{
			ID:           v.ID,
			Rubric:       v.Rubric,
			AssigneeID:   v.AssigneeID,
			AssigneeType: v.AssigneeType,
			Skill:        v.Skill,
		})
	}

	humanChecks := make([]HumanCheck, 0, len(spec.Verification))
	for _, v := range spec.HumanChecks() {
		humanChecks = append(humanChecks, HumanCheck{
			ID:           v.ID,
			Prompt:       v.Prompt,
			AssigneeType: v.AssigneeType,
			AssigneeID:   v.AssigneeID,
		})
	}

	rules := []Rule{
		{
			Name:          "loop:dispatch-build",
			TriggerType:   workflows.TriggerStatusChanged,
			TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: p.BuildStatus},
			ActionType:    workflows.ActionRunSkill,
			ActionConfig: workflows.ActionConfigRunSkill{
				SkillName: p.BuildSkill,
				AgentID:   params.AgentID,
			},
		},
		{
			Name:          "loop:delivery-gate",
			TriggerType:   workflows.TriggerStatusChanged,
			TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: p.ReviewStatus},
			Conditions: []workflows.Condition{
				{Field: "loop.checks", Op: CheckGateOp, Value: CheckGateConfig{
					Checks:        checks,
					AgentID:       params.AgentID,
					Caps:          spec.Caps,
					RevisionSkill: p.BuildSkill,
					JudgeChecks:   judgeChecks,
					JudgeAgentID:  p.JudgeAgentID,
					JudgeSkill:    p.JudgeSkill,
					HumanChecks:   humanChecks,
				}},
			},
			ActionType:   workflows.ActionSetStatus,
			ActionConfig: workflows.ActionConfigSetStatus{Status: p.DoneStatus},
		},
		{
			Name:          "loop:escalate-stalled",
			TriggerType:   workflows.TriggerStatusChanged,
			TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: p.ReviewStatus},
			ActionType:    workflows.ActionEscalateToOwner,
			ActionConfig:  workflows.ActionConfigEscalateToOwner{},
		},
	}

	// Planning mode: prepend a dispatch rule that fires when the issue enters
	// the planning status. The agent sees it was triggered in the planning
	// phase (via the issue status and the trigger name), produces a plan, and
	// then manually advances the issue to BuildStatus to kick off the build.
	// The delivery gate is unchanged — planning is a pre-flight phase, not a
	// separate gate.
	if spec.Planning {
		planRule := PlanningDispatchRule(params.AgentID, p.PlanSkill, p.PlanningStatus)
		rules = append([]Rule{planRule}, rules...)
	}

	return rules, nil
}

// PlanningDispatchRule builds the loop:planning-dispatch rule on its own,
// without going through Compile/Spec.Validate. Unlike the delivery-gate and
// escalate-stalled rules, this one needs no goal/verification/caps — only a
// worker agent, the skill to dispatch, and which status marks entry to the
// planning phase — so a standalone `run_skill` workflow can get a planning
// phase wired up (FIR-2283's `loop_planning` toggle) without authoring a full
// loop.yaml spec. planningStatus defaults to "todo" when empty. skillName is
// the skill dispatched in the planning phase; callers that also have a
// separate build-phase skill (Compile, via CompileParams.withDefaults) pass
// the already-resolved PlanSkill.
func PlanningDispatchRule(agentID, skillName, planningStatus string) Rule {
	if planningStatus == "" {
		planningStatus = "todo"
	}
	return Rule{
		Name:          "loop:planning-dispatch",
		TriggerType:   workflows.TriggerStatusChanged,
		TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: planningStatus},
		ActionType:    workflows.ActionRunSkill,
		ActionConfig: workflows.ActionConfigRunSkill{
			SkillName: skillName,
			AgentID:   agentID,
		},
	}
}
