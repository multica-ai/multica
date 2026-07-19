package loops

// blocks.go defines the block chain that replaces the fixed loop shape
// (plan -> plan gate -> build phases -> delivery gate) with an ordered chain
// the author composes: phases > blocks > steps > gates.
//
//	Phase — an ordered stage of the run. Owns the limits.
//	Block — one unit of work in a phase. Its Type decides what it does:
//	        run a skill, run a command, ask a model, ask a person, or require
//	        an eval. A gate is not a separate concept — it is a block type.
//	Step  — a RUNTIME instance of a block. An ordinary block is one step; a
//	        block with Steps.Allowed lets the agent open more steps of the
//	        SAME block (a Build block may open Build 2, Build 3 — never a
//	        Review), so "what kind of thing is this" stays readable off the
//	        configuration.
//
// Two deliberate departures from the old model:
//
//   - No block type is required. The old spec refused to compile without a
//     programmatic check, which was satisfiable with a placeholder command
//     (["true"]) — a requirement met by a decoy is false assurance, not
//     safety. A machine-decided gate is now opted in as a BlockCommand.
//   - Limits live on the Phase, not on a gate. Every loop back is bounded by
//     the phase that owns it, and exhausting a limit FAILS the run rather
//     than letting half-finished work advance.

import (
	"errors"
	"fmt"
)

// BlockType classifies what a block does when its step runs.
type BlockType string

const (
	ExpectExitZero = "exit_zero"
	AssigneeAgent  = "agent"
	AssigneeMember = "member"

	// BlockSession runs a skill as an agent, on the issue. The plan step and
	// the build step are both just this.
	BlockSession BlockType = "session"
	// BlockCommand runs a command and decides on the exit code. This is the
	// machine-decided gate: the engine runs it, the agent's opinion is
	// irrelevant. Opt-in — see the package note above.
	BlockCommand BlockType = "command"
	// BlockReview asks a model to score a rubric and return a verdict.
	BlockReview BlockType = "review"
	// BlockHuman stops the chain until a person approves.
	BlockHuman BlockType = "human"
	// BlockEval requires a bound eval to have a passing run produced by the
	// server-owned target, grader, and threshold runner.
	BlockEval BlockType = "eval"
)

// BusyPolicy decides what a block does when every agent in its Agents list is
// unavailable (runtime limit, offline). A block that simply waited forever is
// the failure the old human check already had: a gate nobody is told about,
// waiting for someone who never looks.
type BusyPolicy string

const (
	// BusyWait holds the step until an agent frees up, bounded by
	// PhaseLimits.MaxWaitSeconds.
	BusyWait BusyPolicy = "wait"
	// BusyPause suspends the run so it can be resumed deliberately.
	BusyPause BusyPolicy = "pause"
	// BusyWakeup schedules a wakeup to retry later instead of holding a slot.
	BusyWakeup BusyPolicy = "wakeup"
	// BusyPingMember notifies a person that the run cannot proceed.
	BusyPingMember BusyPolicy = "ping_member"
)

// AgentRef is one candidate runner for a block. A block carries a list rather
// than a single agent so a run does not stall on one agent's runtime limit —
// the engine takes the first available and records which one it picked.
type AgentRef struct {
	AgentID       string `yaml:"agent_id" json:"agent_id"`
	Model         string `yaml:"model,omitempty" json:"model,omitempty"`
	ThinkingLevel string `yaml:"thinking_level,omitempty" json:"thinking_level,omitempty"`
}

// StepsConfig makes a block a "steps block": the agent running it may open
// further steps of the same block. Max bounds how many it may open; the
// phase's own MaxSteps bounds the phase as a whole, so a step block can never
// outrun its phase.
type StepsConfig struct {
	Allowed bool `yaml:"allowed" json:"allowed"`
	Max     int  `yaml:"max,omitempty" json:"max,omitempty"`
}

// Block is one unit of work in a phase.
type Block struct {
	// ID is stable and unique within the phase; outcomes are keyed to it.
	ID   string    `yaml:"id" json:"id"`
	Type BlockType `yaml:"type" json:"type"`
	// Name is the human label shown in the editor and used to name the step's
	// session ("Build", "Security review"). Display-only.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Agents are the candidate runners, in preference order. Empty means the
	// engine falls back to the issue's own assignee, so a recipe stays
	// reusable across issues with different owners.
	Agents    []AgentRef `yaml:"agents,omitempty" json:"agents,omitempty"`
	OnAllBusy BusyPolicy `yaml:"on_all_busy,omitempty" json:"on_all_busy,omitempty"`

	// Steps, when Allowed, lets this block open further steps of itself.
	Steps StepsConfig `yaml:"steps,omitempty" json:"steps,omitempty"`

	// Skill is the skill run by a BlockSession, and the skill a BlockReview
	// runs instead of a bare rubric prompt when set.
	Skill string `yaml:"skill,omitempty" json:"skill,omitempty"`
	// Goal is free-text instruction for a BlockSession's step.
	Goal string `yaml:"goal,omitempty" json:"goal,omitempty"`

	// Check is a BlockCommand's argv. Never a shell string.
	Check  []string `yaml:"check,omitempty" json:"check,omitempty"`
	Expect string   `yaml:"expect,omitempty" json:"expect,omitempty"`

	// Rubric is what a BlockReview scores against.
	Rubric string `yaml:"rubric,omitempty" json:"rubric,omitempty"`

	// Prompt is what a BlockHuman asks its approver to sign off.
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// ApproverType/ApproverID name who signs off a BlockHuman —
	// AssigneeMember (a person) or AssigneeAgent (an agent standing in).
	// Unlike the old human check, a member approver is notified rather than
	// left to notice the gate by chance.
	ApproverType string `yaml:"approver_type,omitempty" json:"approver_type,omitempty"`
	ApproverID   string `yaml:"approver_id,omitempty" json:"approver_id,omitempty"`

	// EvalKey names the eval a BlockEval requires a passing run of.
	EvalKey string `yaml:"eval_key,omitempty" json:"eval_key,omitempty"`
}

// PhaseLimits bounds one phase. Every loop back in the phase is counted here,
// so the bound is on the chain rather than on each gate — ten gates with five
// attempts each is fifty rounds, which is not a bound anyone chose.
type PhaseLimits struct {
	// MaxSteps caps how many steps the phase may open in total, including
	// those a steps block adds. Exceeding it fails the run.
	MaxSteps int `yaml:"max_steps" json:"max_steps"`
	// MaxRounds caps how many times the phase may send work back for rework.
	MaxRounds int `yaml:"max_rounds" json:"max_rounds"`
	// NoProgressStalls caps consecutive rounds that changed nothing.
	NoProgressStalls int `yaml:"no_progress_stalls" json:"no_progress_stalls"`
	// MaxWaitSeconds bounds a BusyWait block. Zero means the phase default.
	MaxWaitSeconds int `yaml:"max_wait_seconds,omitempty" json:"max_wait_seconds,omitempty"`
}

// Phase is one ordered stage of the chain.
type Phase struct {
	// ID is stable and unique within the chain.
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Status, when set, is the issue status whose entry starts this phase.
	// Empty chains phases directly: the phase starts when the previous ends.
	Status string      `yaml:"status,omitempty" json:"status,omitempty"`
	Blocks []Block     `yaml:"blocks" json:"blocks"`
	Limits PhaseLimits `yaml:"limits" json:"limits"`
}

// Chain is a whole workflow: an ordered list of phases and the status the run
// lands on when the last phase completes.
type Chain struct {
	Version    int     `yaml:"version" json:"version"`
	Phases     []Phase `yaml:"phases" json:"phases"`
	DoneStatus string  `yaml:"done_status,omitempty" json:"done_status,omitempty"`
}

// ChainVersion is the only supported chain schema version.
const ChainVersion = 2

// MachineDecided reports whether the block's outcome is decided by the engine
// rather than asserted by an agent or a person. Commands use their exit code;
// evals use the cases and verdict produced by the server-owned runner.
func (b Block) MachineDecided() bool {
	return b.Type == BlockCommand || b.Type == BlockEval
}

// HasMachineGate reports whether any block in the chain is machine-decided.
// The chain does not REQUIRE one — the old requirement was satisfiable with a
// placeholder command, which bought nothing. This is what surfaces the fact to
// the author instead of enforcing it.
func (c *Chain) HasMachineGate() bool {
	for _, p := range c.Phases {
		for _, b := range p.Blocks {
			if b.MachineDecided() {
				return true
			}
		}
	}
	return false
}

// Validate reports every reason the chain cannot be trusted to run.
func (c *Chain) Validate() error {
	var errs []error

	if c.Version != ChainVersion {
		errs = append(errs, fmt.Errorf("version must be %d (got %d)", ChainVersion, c.Version))
	}
	if len(c.Phases) == 0 {
		errs = append(errs, errors.New("at least one phase is required"))
	}

	seenPhase := make(map[string]bool, len(c.Phases))
	for i, p := range c.Phases {
		label := p.ID
		if label == "" {
			label = fmt.Sprintf("phases[%d]", i)
			errs = append(errs, fmt.Errorf("%s: id is required", label))
		} else if seenPhase[p.ID] {
			errs = append(errs, fmt.Errorf("phase id %q is duplicated", p.ID))
		}
		seenPhase[p.ID] = true
		errs = append(errs, validatePhase(label, p)...)
	}

	return errors.Join(errs...)
}

// validatePhase validates one phase: its limits must actually bound it, and
// every block must carry its type's required fields.
func validatePhase(label string, p Phase) []error {
	var errs []error

	if len(p.Blocks) == 0 {
		errs = append(errs, fmt.Errorf("%s: at least one block is required", label))
	}
	// A phase with no bound is the runaway we refuse to emit. Unlike the old
	// caps these are per-phase, and unlike the old caps both of them can
	// actually trip — see Chain limits enforcement.
	if p.Limits.MaxSteps <= 0 {
		errs = append(errs, fmt.Errorf("%s: limits.max_steps must be > 0", label))
	}
	if p.Limits.MaxRounds <= 0 {
		errs = append(errs, fmt.Errorf("%s: limits.max_rounds must be > 0", label))
	}
	if p.Limits.NoProgressStalls <= 0 {
		errs = append(errs, fmt.Errorf("%s: limits.no_progress_stalls must be > 0", label))
	}
	if p.Limits.MaxWaitSeconds < 0 {
		errs = append(errs, fmt.Errorf("%s: limits.max_wait_seconds cannot be negative", label))
	}

	// A steps block may never outrun the phase that owns it: the phase's
	// MaxSteps is the real bound, so a block asking for more is a
	// configuration mistake worth reporting at save time.
	seenBlock := make(map[string]bool, len(p.Blocks))
	for i, b := range p.Blocks {
		blabel := fmt.Sprintf("%s.blocks[%d]", label, i)
		if b.ID == "" {
			errs = append(errs, fmt.Errorf("%s: id is required", blabel))
		} else {
			blabel = fmt.Sprintf("%s.blocks[%q]", label, b.ID)
			if seenBlock[b.ID] {
				errs = append(errs, fmt.Errorf("%s: block id %q is duplicated", label, b.ID))
			}
		}
		seenBlock[b.ID] = true
		errs = append(errs, validateBlock(blabel, b, p.Limits)...)
	}

	return errs
}

// validateBlock validates one block's type-specific required fields plus the
// cross-cutting agent/steps rules.
func validateBlock(label string, b Block, limits PhaseLimits) []error {
	var errs []error

	switch b.Type {
	case BlockSession:
		if b.Skill == "" {
			errs = append(errs, fmt.Errorf("%s: session block needs a skill", label))
		}
	case BlockCommand:
		if len(b.Check) == 0 {
			errs = append(errs, fmt.Errorf("%s: command block needs a non-empty check argv", label))
		}
		if b.Expect != "" && b.Expect != ExpectExitZero {
			errs = append(errs, fmt.Errorf("%s: unsupported expect %q (only %q)", label, b.Expect, ExpectExitZero))
		}
	case BlockReview:
		if b.Rubric == "" {
			errs = append(errs, fmt.Errorf("%s: review block needs a rubric", label))
		}
	case BlockHuman:
		if b.Prompt == "" {
			errs = append(errs, fmt.Errorf("%s: human block needs a prompt", label))
		}
		if b.ApproverType != "" && b.ApproverType != AssigneeMember && b.ApproverType != AssigneeAgent {
			errs = append(errs, fmt.Errorf("%s: approver_type must be %q or %q (got %q)",
				label, AssigneeMember, AssigneeAgent, b.ApproverType))
		}
		if b.ApproverType != "" && b.ApproverID == "" {
			errs = append(errs, fmt.Errorf("%s: approver_type %q needs an approver_id", label, b.ApproverType))
		}
	case BlockEval:
		if b.EvalKey == "" {
			errs = append(errs, fmt.Errorf("%s: eval block needs an eval_key", label))
		}
	case "":
		errs = append(errs, fmt.Errorf("%s: type is required (session|command|review|human|eval)", label))
	default:
		errs = append(errs, fmt.Errorf("%s: unknown type %q", label, b.Type))
	}

	// A command block is run by the engine in the worker's checkout, so it
	// takes no agent list of its own and no busy policy — there is nobody to
	// be busy. Reporting it keeps the editor and the engine from disagreeing
	// about what a command block even means.
	if b.Type == BlockCommand && len(b.Agents) > 0 {
		errs = append(errs, fmt.Errorf("%s: command block cannot pick an agent (the engine runs it)", label))
	}
	for i, a := range b.Agents {
		if a.AgentID == "" {
			errs = append(errs, fmt.Errorf("%s: agents[%d] needs an agent_id", label, i))
		}
	}
	if b.OnAllBusy != "" && !validBusyPolicy(b.OnAllBusy) {
		errs = append(errs, fmt.Errorf("%s: unknown on_all_busy %q (wait|pause|wakeup|ping_member)", label, b.OnAllBusy))
	}

	if b.Steps.Allowed {
		if b.Steps.Max <= 0 {
			errs = append(errs, fmt.Errorf("%s: steps block needs steps.max > 0", label))
		} else if limits.MaxSteps > 0 && b.Steps.Max > limits.MaxSteps {
			errs = append(errs, fmt.Errorf("%s: steps.max %d exceeds the phase's limits.max_steps %d",
				label, b.Steps.Max, limits.MaxSteps))
		}
	}

	return errs
}

func validBusyPolicy(p BusyPolicy) bool {
	switch p {
	case BusyWait, BusyPause, BusyWakeup, BusyPingMember:
		return true
	}
	return false
}
