// Package loops defines the loop.yaml specification that drives a Multica
// build-loop: a coached goal, typed verification criteria, and termination
// guards. A Spec is authored during the planning phase and compiled onto the
// existing cerebro-workflows engine (trigger -> condition -> action), so this
// package owns only the spec shape, parsing, and validation — not execution.
//
// The design mirrors ksimback/looper's loop.yaml: every success criterion is
// typed (programmatic / judge / human), and the loop refuses to run without at
// least one machine-checkable gate and explicit termination guards. That is
// what makes a loop trustworthy even when the worker agent is not.
package loops

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// CheckType classifies how a verification criterion is decided.
type CheckType string

const (
	// CheckProgrammatic — a command returns pass/fail. The only fully
	// trustworthy gate: the engine runs it, the agent's opinion is irrelevant.
	CheckProgrammatic CheckType = "programmatic"
	// CheckJudge — a model scores a rubric and returns a structured verdict.
	// Weaker than programmatic; used for semantic quality code cannot check.
	CheckJudge CheckType = "judge"
	// CheckHuman — a person signs off. For taste, business, or legal calls.
	CheckHuman CheckType = "human"
)

// ExpectExitZero is the default (and currently only) expectation for a
// programmatic check: the command must exit 0 to pass.
const ExpectExitZero = "exit_zero"

// Assignee types a judge or human check's AssigneeType may carry. Mirrors
// workflows.AssigneeTypeAgent / workflows.AssigneeTypeMember (spelled out
// locally rather than imported so this file stays independent of the
// workflows package — see the package doc: spec.go owns only the spec
// shape, not execution).
const (
	AssigneeAgent  = "agent"
	AssigneeMember = "member"
)

// Verification is one typed success criterion.
type Verification struct {
	ID   string    `yaml:"id" json:"id"`
	Type CheckType `yaml:"type" json:"type"`
	// Label is a short human-readable name for the condition (e.g. "Test
	// suite" for a programmatic check whose real command is hidden behind
	// "Advanced" in the Issue workflow editor). Display-only — the engine
	// never reads it, so it round-trips through the spec without touching
	// the compiled gate config.
	Label string `yaml:"label,omitempty" json:"label,omitempty"`

	// Programmatic fields.
	Check  []string `yaml:"check,omitempty" json:"check,omitempty"`   // argv array, never a shell string
	Expect string   `yaml:"expect,omitempty" json:"expect,omitempty"` // defaults to ExpectExitZero

	// Judge fields. Rubric is the free-text criterion scored; Skill, when
	// set, names a skill the assigned agent runs instead of a bare rubric
	// prompt ("AI review runs a skill or a prompt" — Rubric is always kept
	// as the human-readable criterion shown in the UI either way).
	Rubric string `yaml:"rubric,omitempty" json:"rubric,omitempty"`
	Skill  string `yaml:"skill,omitempty" json:"skill,omitempty"`

	// Human field.
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`

	// AssigneeType/AssigneeID select who performs a judge or human check —
	// "agent" (AssigneeID is an agent id) or "member" (AssigneeID is a
	// person). Unused for programmatic checks; the engine always runs those
	// itself. Empty AssigneeType on a judge check falls back to the spec-wide
	// judge params (CompileParams.JudgeAgentID) so an older spec without
	// per-check assignees still dispatches.
	AssigneeType  string `yaml:"assignee_type,omitempty" json:"assignee_type,omitempty"`
	AssigneeID    string `yaml:"assignee_id,omitempty" json:"assignee_id,omitempty"`
	Model         string `yaml:"model,omitempty" json:"model,omitempty"`
	ThinkingLevel string `yaml:"thinking_level,omitempty" json:"thinking_level,omitempty"`
}

// Caps are the termination guards. All are required and must be positive — a
// loop with no guard is exactly the runaway we refuse to emit.
type Caps struct {
	MaxIterations    int `yaml:"max_iterations" json:"max_iterations"`
	MaxRevisions     int `yaml:"max_revisions" json:"max_revisions"`
	NoProgressStalls int `yaml:"no_progress_stalls" json:"no_progress_stalls"`
}

// Spec is a parsed loop.yaml.
type Spec struct {
	Version int `yaml:"version" json:"version"`
	// Goal and DefinitionOfDone are free-text notes for a human reader — the
	// engine never reads either one. FIR-2283 v2: an Issue workflow recipe
	// describes HOW an issue is worked (a chain of steps gated by
	// Verification), not WHAT to build (that lives on the issue itself), so
	// neither field is required. Both still round-trip through the spec for
	// specs that do want a human-readable description.
	Goal             string         `yaml:"goal,omitempty" json:"goal,omitempty"`
	DefinitionOfDone string         `yaml:"definition_of_done,omitempty" json:"definition_of_done,omitempty"`
	Verification     []Verification `yaml:"verification" json:"verification"`
	Caps             Caps           `yaml:"caps" json:"caps"`
	// Planning gates the loop behind an explicit design phase. When true,
	// Compile emits a loop:planning-dispatch rule that fires the plan skill
	// when the issue enters the planning status (default "todo"), so the
	// assigned agent must produce and post a plan before the build phase
	// begins. The agent advances to the build status (default "in_progress")
	// when it is satisfied with the plan. The delivery gate is unchanged.
	Planning bool `yaml:"planning,omitempty" json:"planning,omitempty"`
	// PlanGate is an optional set of criteria that must pass before the build
	// phase is allowed to start (FIR-2283 v2 point 6 — "gates on the Plan
	// step", e.g. an adversarial AI review that tries to tear the plan down
	// before any code is written). Only meaningful when Planning is true —
	// Validate rejects a non-empty PlanGate on a spec with Planning false.
	// Unlike the delivery gate (Verification), PlanGate does NOT require a
	// programmatic entry: there is no code yet to run a command against
	// during planning, so a judge or human criterion alone is valid here.
	// Compile attaches it as a second, independently-keyed check_passes
	// condition on loop:dispatch-build, so a failing plan gate re-dispatches
	// the plan skill instead of starting the build.
	PlanGate []Verification `yaml:"plan_gate,omitempty" json:"plan_gate,omitempty"`

	// Phases splits the build into an ordered chain of build steps, each with
	// its own build skill and its own delivery gate that must pass before the
	// next phase's build is dispatched (FIR-2283 followup point 6). A spec with
	// no Phases is a single-phase loop and behaves exactly as before — the
	// top-level Verification is that single build's delivery gate. When Phases
	// is set, the top-level Verification is ignored (each phase carries its own
	// gate) and BuildSkill falls to the per-phase BuildSkill. Each phase runs
	// as its own session ("Build k") followed by a review session ("Review k"),
	// reusing the same gate engine as the single-phase delivery gate.
	Phases []BuildPhase `yaml:"phases,omitempty" json:"phases,omitempty"`
}

// BuildPhase is one ordered build step in a multi-phase loop. It carries its
// own build skill/agent and its own delivery gate (Verification) — the review
// that must pass before the next phase's build begins.
type BuildPhase struct {
	// Name is a short human label for the phase (e.g. "Backend"), shown in the
	// editor and used to name the phase's build/review sessions. Display-only.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// BuildSkill is the skill this phase's build agent runs. Required.
	BuildSkill string `yaml:"build_skill" json:"build_skill"`
	// BuildAgentID pins the phase's build agent; empty falls back to the
	// recipe-wide build agent (CompileParams.AgentID).
	BuildAgentID  string `yaml:"build_agent_id,omitempty" json:"build_agent_id,omitempty"`
	Model         string `yaml:"model,omitempty" json:"model,omitempty"`
	ThinkingLevel string `yaml:"thinking_level,omitempty" json:"thinking_level,omitempty"`
	// Goal is an optional free-text instruction for this phase's build.
	Goal string `yaml:"goal,omitempty" json:"goal,omitempty"`
	// Verification is this phase's delivery gate. Same rules as the top-level
	// gate: at least one programmatic check is required.
	Verification []Verification `yaml:"verification" json:"verification"`
}

// SpecVersion is the only supported loop.yaml schema version.
const SpecVersion = 1

// Parse unmarshals a loop.yaml document and validates it. A non-nil error means
// the spec must not run; the error aggregates every problem found so the
// planning interview can surface them all at once.
func Parse(data []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("loop.yaml is not valid YAML: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate reports every reason the spec cannot be trusted to run.
func (s *Spec) Validate() error {
	var errs []error

	if s.Version != SpecVersion {
		errs = append(errs, fmt.Errorf("version must be %d (got %d)", SpecVersion, s.Version))
	}

	// Multi-phase (FIR-2283 followup point 6): when Phases is set, each phase
	// carries its own delivery gate, so the top-level Verification is ignored
	// and its "at least one criterion" requirement moves onto every phase. A
	// single-phase spec (no Phases) keeps the original top-level gate rules.
	if len(s.Phases) > 0 {
		errs = append(errs, validatePhases(s.Phases)...)
	} else {
		if len(s.Verification) == 0 {
			errs = append(errs, errors.New("at least one verification criterion is required"))
		}
		// The defining rule for the delivery gate: a loop must have at least
		// one gate the engine itself can decide, so progress never depends
		// solely on a model grading its own or another agent's work.
		// requireProgrammatic is false for PlanGate below — there is no code
		// yet during planning, so a judge/human-only plan gate (Jesper's
		// example: an adversarial AI review of the plan) is valid.
		errs = append(errs, validateChecks("verification", s.Verification, true)...)
	}

	if len(s.PlanGate) > 0 && !s.Planning {
		errs = append(errs, errors.New("plan_gate requires planning to be true (a plan gate only makes sense with a planning phase)"))
	}
	errs = append(errs, validateChecks("plan_gate", s.PlanGate, false)...)

	if s.Caps.MaxIterations <= 0 {
		errs = append(errs, errors.New("caps.max_iterations must be > 0"))
	}
	if s.Caps.MaxRevisions <= 0 {
		errs = append(errs, errors.New("caps.max_revisions must be > 0"))
	}
	if s.Caps.NoProgressStalls <= 0 {
		errs = append(errs, errors.New("caps.no_progress_stalls must be > 0"))
	}

	return errors.Join(errs...)
}

// validatePhases validates a multi-phase build chain: at least one phase,
// each phase needs a build skill and its own delivery gate (same rules as the
// single-phase top-level gate — at least one programmatic check).
func validatePhases(phases []BuildPhase) []error {
	var errs []error
	for i, p := range phases {
		if p.BuildSkill == "" {
			errs = append(errs, fmt.Errorf("phases[%d]: build_skill is required", i))
		}
		if len(p.Verification) == 0 {
			errs = append(errs, fmt.Errorf("phases[%d]: at least one verification criterion is required", i))
		}
		errs = append(errs, validateChecks(fmt.Sprintf("phases[%d].verification", i), p.Verification, true)...)
	}
	return errs
}

// validateChecks validates one typed-criteria list (Spec.Verification or
// Spec.PlanGate): every entry needs a unique id and its type-specific
// required fields. When requireProgrammatic is true, at least one entry must
// be CheckProgrammatic — the delivery gate's defining rule. list may be
// empty; an empty PlanGate is valid (planning with no extra gate).
func validateChecks(field string, list []Verification, requireProgrammatic bool) []error {
	var errs []error
	seen := make(map[string]bool, len(list))
	programmatic := 0
	for i, v := range list {
		label := v.ID
		if label == "" {
			label = fmt.Sprintf("%s[%d]", field, i)
			errs = append(errs, fmt.Errorf("%s: id is required", label))
		} else if seen[v.ID] {
			errs = append(errs, fmt.Errorf("%s id %q is duplicated", field, v.ID))
		}
		seen[v.ID] = true

		switch v.Type {
		case CheckProgrammatic:
			programmatic++
			if len(v.Check) == 0 {
				errs = append(errs, fmt.Errorf("%s: programmatic check needs a non-empty check argv", label))
			}
			if v.Expect != "" && v.Expect != ExpectExitZero {
				errs = append(errs, fmt.Errorf("%s: unsupported expect %q (only %q)", label, v.Expect, ExpectExitZero))
			}
		case CheckJudge:
			if v.Rubric == "" {
				errs = append(errs, fmt.Errorf("%s: judge check needs a rubric", label))
			}
		case CheckHuman:
			if v.Prompt == "" {
				errs = append(errs, fmt.Errorf("%s: human check needs a prompt", label))
			}
		case "":
			errs = append(errs, fmt.Errorf("%s: type is required (programmatic|judge|human)", label))
		default:
			errs = append(errs, fmt.Errorf("%s: unknown type %q", label, v.Type))
		}
	}

	if requireProgrammatic && len(list) > 0 && programmatic == 0 {
		errs = append(errs, fmt.Errorf("at least one programmatic %s is required (a loop needs a check the engine can run, not only judge/human criteria)", field))
	}
	return errs
}

// ProgrammaticChecks returns the criteria the engine can decide on its own.
func (s *Spec) ProgrammaticChecks() []Verification {
	return filterByType(s.Verification, CheckProgrammatic)
}

// JudgeChecks returns the criteria a model must score against a rubric. Unlike
// a programmatic check, the engine cannot decide these on its own — it
// dispatches each one to a judge agent and trusts the structured verdict that
// comes back (see CheckDispatcher.DispatchJudge).
func (s *Spec) JudgeChecks() []Verification {
	return filterByType(s.Verification, CheckJudge)
}

// HumanChecks returns the criteria a person (or an agent standing in for
// one) must explicitly approve. Like a judge check, the engine cannot decide
// these on its own — it dispatches each one to the configured assignee and
// waits for a reported approve/reject decision (see CheckDispatcher.DispatchHuman).
func (s *Spec) HumanChecks() []Verification {
	return filterByType(s.Verification, CheckHuman)
}

// PlanGateProgrammaticChecks/PlanGateJudgeChecks/PlanGateHumanChecks mirror
// the Verification accessors above, filtered from Spec.PlanGate instead —
// used by Compile to build the plan gate's CheckGateConfig.
func (s *Spec) PlanGateProgrammaticChecks() []Verification {
	return filterByType(s.PlanGate, CheckProgrammatic)
}
func (s *Spec) PlanGateJudgeChecks() []Verification { return filterByType(s.PlanGate, CheckJudge) }
func (s *Spec) PlanGateHumanChecks() []Verification { return filterByType(s.PlanGate, CheckHuman) }

// filterByType returns the entries of list matching t, preserving order.
func filterByType(list []Verification, t CheckType) []Verification {
	out := make([]Verification, 0, len(list))
	for _, v := range list {
		if v.Type == t {
			out = append(out, v)
		}
	}
	return out
}
