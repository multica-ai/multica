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

// Verification is one typed success criterion.
type Verification struct {
	ID   string    `yaml:"id"`
	Type CheckType `yaml:"type"`

	// Programmatic fields.
	Check  []string `yaml:"check,omitempty"`  // argv array, never a shell string
	Expect string   `yaml:"expect,omitempty"` // defaults to ExpectExitZero

	// Judge field.
	Rubric string `yaml:"rubric,omitempty"`

	// Human field.
	Prompt string `yaml:"prompt,omitempty"`
}

// Caps are the termination guards. All are required and must be positive — a
// loop with no guard is exactly the runaway we refuse to emit.
type Caps struct {
	MaxIterations    int `yaml:"max_iterations"`
	MaxRevisions     int `yaml:"max_revisions"`
	NoProgressStalls int `yaml:"no_progress_stalls"`
}

// Spec is a parsed loop.yaml.
type Spec struct {
	Version          int            `yaml:"version"`
	Goal             string         `yaml:"goal"`
	DefinitionOfDone string         `yaml:"definition_of_done"`
	Verification     []Verification `yaml:"verification"`
	Caps             Caps           `yaml:"caps"`
	// Planning gates the loop behind an explicit design phase. When true,
	// Compile emits a loop:planning-dispatch rule that fires the plan skill
	// when the issue enters the planning status (default "todo"), so the
	// assigned agent must produce and post a plan before the build phase
	// begins. The agent advances to the build status (default "in_progress")
	// when it is satisfied with the plan. The delivery gate is unchanged.
	Planning bool `yaml:"planning,omitempty"`
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
	if s.Goal == "" {
		errs = append(errs, errors.New("goal is required"))
	}
	if s.DefinitionOfDone == "" {
		errs = append(errs, errors.New("definition_of_done is required"))
	}

	if len(s.Verification) == 0 {
		errs = append(errs, errors.New("at least one verification criterion is required"))
	}

	seen := make(map[string]bool, len(s.Verification))
	programmatic := 0
	for i, v := range s.Verification {
		label := v.ID
		if label == "" {
			label = fmt.Sprintf("verification[%d]", i)
			errs = append(errs, fmt.Errorf("%s: id is required", label))
		} else if seen[v.ID] {
			errs = append(errs, fmt.Errorf("verification id %q is duplicated", v.ID))
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

	// The defining rule: a loop must have at least one gate the engine itself
	// can decide, so progress never depends solely on a model grading its own
	// or another agent's work.
	if len(s.Verification) > 0 && programmatic == 0 {
		errs = append(errs, errors.New("at least one programmatic verification is required (a loop needs a check the engine can run, not only judge/human criteria)"))
	}

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

// ProgrammaticChecks returns the criteria the engine can decide on its own.
func (s *Spec) ProgrammaticChecks() []Verification {
	out := make([]Verification, 0, len(s.Verification))
	for _, v := range s.Verification {
		if v.Type == CheckProgrammatic {
			out = append(out, v)
		}
	}
	return out
}
