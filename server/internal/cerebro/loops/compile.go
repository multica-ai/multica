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

// issueScopeField/issueScopeOp select the engine's generic "eq" condition
// against the trigger event's own issue id (workflows.evaluate walks
// te.Raw["issue"]["id"] for the dotted path "issue.id" — see
// workflows/conditions.go and workflows/listener.go's onIssueUpdated, which
// always populates payload["issue"]["id"]). Compile appends this condition to
// every rule when CompileParams.IssueID is set, so a per-issue Issue workflow
// activation reuses the existing pure evaluator instead of adding a new op.
const (
	issueScopeField = "issue.id"
	issueScopeOp    = "eq"
)

// issueScopeCondition returns the single-issue scoping condition for
// issueID, or nil when issueID is empty (project-wide — the default).
func issueScopeCondition(issueID string) []workflows.Condition {
	if issueID == "" {
		return nil
	}
	return []workflows.Condition{{Field: issueScopeField, Op: issueScopeOp, Value: issueID}}
}

// splitChecks converts one CheckType-filtered triple of Verification slices
// (as returned by Spec's ProgrammaticChecks/JudgeChecks/HumanChecks or their
// PlanGate* equivalents) into the argv/JudgeCheck/HumanCheck shapes a
// CheckGateConfig carries. Shared by the delivery gate and the plan gate so
// both build their gate config the same way.
func splitChecks(programmatic, judge, human []Verification) ([][]string, []JudgeCheck, []HumanCheck) {
	checks := make([][]string, 0, len(programmatic))
	for _, v := range programmatic {
		checks = append(checks, v.Check)
	}
	judgeChecks := make([]JudgeCheck, 0, len(judge))
	for _, v := range judge {
		judgeChecks = append(judgeChecks, JudgeCheck{
			ID:            v.ID,
			Rubric:        v.Rubric,
			AssigneeID:    v.AssigneeID,
			AssigneeType:  v.AssigneeType,
			Skill:         v.Skill,
			Model:         v.Model,
			ThinkingLevel: v.ThinkingLevel,
		})
	}
	humanChecks := make([]HumanCheck, 0, len(human))
	for _, v := range human {
		humanChecks = append(humanChecks, HumanCheck{
			ID:           v.ID,
			Prompt:       v.Prompt,
			AssigneeType: v.AssigneeType,
			AssigneeID:   v.AssigneeID,
		})
	}
	return checks, judgeChecks, humanChecks
}

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
	RevisionSkill    string `json:"revision_skill,omitempty"`
	RevisionModel    string `json:"revision_model,omitempty"`
	RevisionThinking string `json:"revision_thinking,omitempty"`
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
	JudgeSkill    string `json:"judge_skill,omitempty"`
	JudgeModel    string `json:"judge_model,omitempty"`
	JudgeThinking string `json:"judge_thinking,omitempty"`
	// HumanChecks are the spec's human-type verification criteria: a person
	// (or an agent standing in for one) must explicitly approve or reject.
	// The gate treats a human check exactly like a programmatic or judge one
	// for advance/revise/wait purposes — every one must report Approved
	// before the gate can advance.
	HumanChecks []HumanCheck `json:"human_checks,omitempty"`
	// RevertStatus is the status the engine moves the issue back to when this
	// gate revises (see gate_evaluator.go's StatusSetter). Answers "how does
	// it gate towards done" for the board, not just the DB: a failing gate
	// visibly sends the issue back to Build/Plan instead of leaving it
	// sitting on a status the loop has already moved past. Empty means no
	// revert (the pre-FIR-2283-v2-slice2d behavior) — Compile always fills
	// this in for both the delivery gate (BuildStatus) and the plan gate
	// (PlanningStatus).
	RevertStatus string `json:"revert_status,omitempty"`
	// Phases carries a multi-phase build chain (FIR-2283 followup point 6).
	// When non-empty, the gate evaluator reads the issue's current phase index
	// and evaluates that phase's checks instead of the top-level Checks; on
	// advance it dispatches the next phase's BuildSkill (a fresh "Build k"
	// session) rather than marking the issue done — only the final phase's gate
	// advances to done. Empty means a single-phase loop (unchanged behavior).
	Phases []GatePhase `json:"phases,omitempty"`
}

// GatePhase is one phase's build binding + delivery gate, carried in
// CheckGateConfig.Phases. The evaluator uses phase p's Checks/JudgeChecks/
// HumanChecks to decide phase p's gate, and dispatches phase p's BuildSkill
// (to BuildAgentID, falling back to the gate's AgentID) when it re-enters or
// advances into that phase.
type GatePhase struct {
	Name          string       `json:"name,omitempty"`
	BuildSkill    string       `json:"build_skill,omitempty"`
	BuildAgentID  string       `json:"build_agent_id,omitempty"`
	Model         string       `json:"model,omitempty"`
	ThinkingLevel string       `json:"thinking_level,omitempty"`
	Checks        [][]string   `json:"checks,omitempty"`
	JudgeChecks   []JudgeCheck `json:"judge_checks,omitempty"`
	HumanChecks   []HumanCheck `json:"human_checks,omitempty"`
}

// JudgeCheck is one judge-type verification criterion carried into the gate
// config: an ID (matches the spec's Verification.ID, used to key its reported
// outcome), the rubric the judge scores the delivered work against, and an
// optional per-check assignee/skill override. Empty AssigneeID/Skill fall
// back to CheckGateConfig.JudgeAgentID/JudgeSkill.
type JudgeCheck struct {
	ID            string `json:"id"`
	Rubric        string `json:"rubric"`
	AssigneeID    string `json:"assignee_id,omitempty"`
	AssigneeType  string `json:"assignee_type,omitempty"`
	Skill         string `json:"skill,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
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
	// IssueID scopes every compiled rule to a single issue instead of the
	// whole project (FIR-2283 v2 point 8 — "per-issue workflow activation").
	// A recipe compiled without IssueID fires for every issue in the project
	// that passes through the bound statuses, exactly as a standard workflow
	// does — that stays the default. When a caller compiles the SAME recipe
	// for one specific issue (the "run this workflow on this issue" picker),
	// IssueID makes Compile append an `issue.id eq IssueID` condition to
	// every rule, so the materialized workflow rows only ever fire for that
	// issue. Reuses the engine's existing pure condition evaluator — no new
	// condition op, no DB schema change.
	IssueID string

	// AgentID is the worker dispatched to build (run_skill). Optional (FIR-2283
	// v2): an Issue workflow recipe may leave its agent picker empty so the
	// same recipe is reusable across issues with different owners. When
	// empty, workflows.actionRunSkill resolves the dispatch/plan step to
	// whoever the triggering issue is currently assigned to. The delivery
	// gate's own check dispatch (CheckGateConfig.AgentID) still degrades to
	// "hold without dispatching" when empty — see gate_evaluator.go — so a
	// recipe with no pinned agent and an issue with no agent assignee simply
	// waits at the gate instead of silently picking an arbitrary worker.
	AgentID       string
	BuildSkill    string // skill the worker runs each build round
	BuildModel    string
	BuildThinking string
	BuildStatus   string // status whose entry dispatches a build round
	ReviewStatus  string // status the worker sets when it claims a round done
	DoneStatus    string // terminal status, set only when the gate passes

	// Planning-phase params — only used when Spec.Planning is true.
	// PlanSkill is the skill dispatched during the planning phase; if empty,
	// BuildSkill is used (the agent distinguishes the phase from the issue
	// status). PlanningStatus is the status that triggers the planning
	// dispatch; it defaults to "todo" so creating the issue in the default
	// state fires the plan immediately.
	PlanSkill      string
	PlanAgentID    string
	PlanModel      string
	PlanThinking   string
	PlanningStatus string

	// Judge params — only used when the spec has judge-type verification
	// criteria (Spec.JudgeChecks). JudgeAgentID is the agent that scores them;
	// if empty, AgentID is used (the worker judges its own delivery — allowed
	// so a spec with judge checks never silently stalls, but callers that
	// want a blind review must set a distinct JudgeAgentID). JudgeSkill is
	// the skill dispatched to score a judge check; if empty, BuildSkill is
	// used.
	JudgeAgentID  string
	JudgeSkill    string
	JudgeModel    string
	JudgeThinking string
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
	// AgentID is intentionally not required here (FIR-2283 v2) — see the
	// CompileParams.AgentID doc comment for how an empty build agent
	// resolves at runtime instead of at compile time.
	//
	// A multi-phase spec (FIR-2283 followup point 6) carries the build skill
	// per phase, so params.BuildSkill is not required — phase 0's skill drives
	// the loop:dispatch-build rule and the rest are dispatched by the gate
	// evaluator on advance.
	multiPhase := len(spec.Phases) > 0
	var errs []error
	if !multiPhase && params.BuildSkill == "" {
		errs = append(errs, errors.New("params.build_skill is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	p := params.withDefaults()

	checks, judgeChecks, humanChecks := splitChecks(spec.ProgrammaticChecks(), spec.JudgeChecks(), spec.HumanChecks())

	issueScope := issueScopeCondition(params.IssueID)

	// Build the delivery gate's config and the phase-0 build binding. For a
	// single-phase loop these are the spec's top-level gate + params build
	// skill (unchanged). For a multi-phase loop, phase 0 drives the dispatch
	// rule and the gate carries every phase's checks so the evaluator can pick
	// the current one and dispatch the next on advance.
	buildSkill := p.BuildSkill
	buildAgentID := params.AgentID
	deliveryGateConfig := CheckGateConfig{
		Checks:           checks,
		AgentID:          params.AgentID,
		Caps:             spec.Caps,
		RevisionSkill:    p.BuildSkill,
		RevisionModel:    params.BuildModel,
		RevisionThinking: params.BuildThinking,
		JudgeChecks:      judgeChecks,
		JudgeAgentID:     p.JudgeAgentID,
		JudgeSkill:       p.JudgeSkill,
		JudgeModel:       params.JudgeModel,
		JudgeThinking:    params.JudgeThinking,
		HumanChecks:      humanChecks,
		RevertStatus:     p.BuildStatus,
	}
	if multiPhase {
		gatePhases := make([]GatePhase, len(spec.Phases))
		for i, ph := range spec.Phases {
			pc, pj, phm := splitChecks(
				filterByType(ph.Verification, CheckProgrammatic),
				filterByType(ph.Verification, CheckJudge),
				filterByType(ph.Verification, CheckHuman),
			)
			gatePhases[i] = GatePhase{
				Name:          ph.Name,
				BuildSkill:    ph.BuildSkill,
				BuildAgentID:  ph.BuildAgentID,
				Model:         ph.Model,
				ThinkingLevel: ph.ThinkingLevel,
				Checks:        pc,
				JudgeChecks:   pj,
				HumanChecks:   phm,
			}
		}
		buildSkill = spec.Phases[0].BuildSkill
		if spec.Phases[0].BuildAgentID != "" {
			buildAgentID = spec.Phases[0].BuildAgentID
		}
		params.BuildModel = spec.Phases[0].Model
		params.BuildThinking = spec.Phases[0].ThinkingLevel
		// The top-level checks are unused in multi-phase; the evaluator reads
		// the current phase's checks out of Phases instead.
		deliveryGateConfig.Checks = nil
		deliveryGateConfig.JudgeChecks = nil
		deliveryGateConfig.HumanChecks = nil
		deliveryGateConfig.RevisionSkill = spec.Phases[0].BuildSkill
		deliveryGateConfig.RevisionModel = spec.Phases[0].Model
		deliveryGateConfig.RevisionThinking = spec.Phases[0].ThinkingLevel
		deliveryGateConfig.Phases = gatePhases
	}

	// Plan gate (FIR-2283 v2 point 6): when set, a check_passes condition on
	// loop:dispatch-build itself — a second, independently-keyed gate from
	// the delivery gate below (its gate key is loop:dispatch-build's own
	// materialized row id, see gate_evaluator.go's EvaluateCheckGate). A
	// failing plan gate re-dispatches the plan skill (RevisionSkill) instead
	// of starting the build; the run_skill action only fires once it passes.
	dispatchBuildConditions := append([]workflows.Condition{}, issueScope...)
	if spec.Planning && len(spec.PlanGate) > 0 {
		planChecks, planJudgeChecks, planHumanChecks := splitChecks(
			spec.PlanGateProgrammaticChecks(), spec.PlanGateJudgeChecks(), spec.PlanGateHumanChecks())
		dispatchBuildConditions = append(dispatchBuildConditions, workflows.Condition{
			Field: "loop.checks", Op: CheckGateOp, Value: CheckGateConfig{
				Checks:           planChecks,
				AgentID:          params.AgentID,
				Caps:             spec.Caps,
				RevisionSkill:    p.PlanSkill,
				RevisionModel:    params.PlanModel,
				RevisionThinking: params.PlanThinking,
				JudgeChecks:      planJudgeChecks,
				JudgeAgentID:     p.JudgeAgentID,
				JudgeSkill:       p.JudgeSkill,
				JudgeModel:       params.JudgeModel,
				JudgeThinking:    params.JudgeThinking,
				HumanChecks:      planHumanChecks,
				RevertStatus:     p.PlanningStatus,
			},
		})
	}

	rules := []Rule{
		{
			Name:          "loop:dispatch-build",
			TriggerType:   workflows.TriggerStatusChanged,
			TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: p.BuildStatus},
			Conditions:    dispatchBuildConditions,
			ActionType:    workflows.ActionRunSkill,
			ActionConfig: workflows.ActionConfigRunSkill{
				SkillName:     buildSkill,
				AgentID:       buildAgentID,
				Model:         params.BuildModel,
				ThinkingLevel: params.BuildThinking,
				// Phase marker: the build kickoff runs ON the issue in a
				// session badged "build" (see ActionConfigRunSkill.LoopPhase).
				LoopPhase: "build",
			},
		},
		{
			Name:          "loop:delivery-gate",
			TriggerType:   workflows.TriggerStatusChanged,
			TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: p.ReviewStatus},
			Conditions: append(append([]workflows.Condition{}, issueScope...),
				workflows.Condition{Field: "loop.checks", Op: CheckGateOp, Value: deliveryGateConfig},
			),
			ActionType:   workflows.ActionSetStatus,
			ActionConfig: workflows.ActionConfigSetStatus{Status: p.DoneStatus},
		},
		{
			Name:          "loop:escalate-stalled",
			TriggerType:   workflows.TriggerStatusChanged,
			TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: p.ReviewStatus},
			Conditions:    issueScope,
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
		planAgentID := params.PlanAgentID
		if planAgentID == "" {
			planAgentID = params.AgentID
		}
		planRule := PlanningDispatchRule(planAgentID, p.PlanSkill, p.PlanningStatus)
		ac := planRule.ActionConfig.(workflows.ActionConfigRunSkill)
		ac.Model = params.PlanModel
		ac.ThinkingLevel = params.PlanThinking
		planRule.ActionConfig = ac
		planRule.Conditions = issueScope
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
			// FIR-2283 followup point 3 — this dispatch IS the plan phase, so
			// the run must be told it is planning (only plan in Multica, no
			// code) rather than inferring it from the issue status.
			PlanMode: true,
			// Phase marker: the plan run happens ON the issue in a session
			// badged "plan" (see ActionConfigRunSkill.LoopPhase).
			LoopPhase: "plan",
		},
	}
}
