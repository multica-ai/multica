package loops

import "github.com/multica-ai/multica/server/internal/cerebro/workflows"

// CheckGateOp remains as the wire contract for already-materialized standard
// workflow gates. Issue workflows no longer compile this condition.
const CheckGateOp = "check_passes"

// GateLimits reads historical gate configuration while old generated rows are
// drained. New block chains use PhaseLimits instead.
type GateLimits struct {
	MaxIterations    int `json:"max_iterations"`
	MaxRevisions     int `json:"max_revisions"`
	NoProgressStalls int `json:"no_progress_stalls"`
}

type CheckGateConfig struct {
	Checks           [][]string   `json:"checks"`
	EvalPhase        string       `json:"eval_phase,omitempty"`
	AgentID          string       `json:"agent_id,omitempty"`
	Caps             GateLimits   `json:"caps,omitempty"`
	RevisionSkill    string       `json:"revision_skill,omitempty"`
	RevisionModel    string       `json:"revision_model,omitempty"`
	RevisionThinking string       `json:"revision_thinking,omitempty"`
	JudgeChecks      []JudgeCheck `json:"judge_checks,omitempty"`
	JudgeAgentID     string       `json:"judge_agent_id,omitempty"`
	JudgeSkill       string       `json:"judge_skill,omitempty"`
	JudgeModel       string       `json:"judge_model,omitempty"`
	JudgeThinking    string       `json:"judge_thinking,omitempty"`
	HumanChecks      []HumanCheck `json:"human_checks,omitempty"`
	RevertStatus     string       `json:"revert_status,omitempty"`
	Phases           []GatePhase  `json:"phases,omitempty"`
}

func (c CheckGateConfig) EvalBindingPhase() string { return c.EvalPhase }

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

type JudgeCheck struct {
	ID            string `json:"id"`
	Rubric        string `json:"rubric"`
	AssigneeID    string `json:"assignee_id,omitempty"`
	AssigneeType  string `json:"assignee_type,omitempty"`
	Skill         string `json:"skill,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
}

type HumanCheck struct {
	ID           string `json:"id"`
	Prompt       string `json:"prompt"`
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
}

// Rule is retained only for the generic planning companion used by standard
// run_skill workflows. Issue workflows execute Chain directly.
type Rule struct {
	Name          string
	TriggerType   string
	TriggerConfig any
	Conditions    []workflows.Condition
	ActionType    string
	ActionConfig  any
}

func PlanningDispatchRule(agentID, skillName, planningStatus, buildStatus string) Rule {
	if planningStatus == "" {
		planningStatus = "todo"
	}
	if buildStatus == "" {
		buildStatus = "in_progress"
	}
	return Rule{
		Name:          "loop:planning-dispatch",
		TriggerType:   workflows.TriggerStatusChanged,
		TriggerConfig: workflows.TriggerConfigStatusChanged{ToStatus: planningStatus},
		ActionType:    workflows.ActionRunSkill,
		ActionConfig: workflows.ActionConfigRunSkill{
			SkillName: skillName, AgentID: agentID, PlanMode: true, LoopPhase: "plan",
			AdvanceFromStatus: planningStatus, AdvanceToStatus: buildStatus,
		},
	}
}
