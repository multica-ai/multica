package loops

import (
	"strings"
	"testing"
)

// okLimits is a bound that passes validation, so a test can vary one thing at
// a time without every case restating four numbers.
func okLimits() PhaseLimits {
	return PhaseLimits{MaxSteps: 10, MaxRounds: 3, NoProgressStalls: 2}
}

func sessionBlock(id string) Block {
	return Block{ID: id, Type: BlockSession, Skill: "build"}
}

func chainWith(blocks ...Block) *Chain {
	return &Chain{
		Version: ChainVersion,
		Phases: []Phase{{
			ID:     "p1",
			Blocks: blocks,
			Limits: okLimits(),
		}},
	}
}

// assertErrContains fails unless err mentions want. Validate aggregates every
// problem, so a test asserts on the message it cares about rather than on the
// whole join.
func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected an error mentioning %q, got: %v", want, err)
	}
}

func TestChainValidateAcceptsMinimalChain(t *testing.T) {
	c := chainWith(sessionBlock("build"))
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a one-session chain to be valid, got: %v", err)
	}
}

// The headline departure from the old spec: a chain with no machine-decided
// block is VALID. The old model refused to compile without a programmatic
// check, which authors satisfied with ["true"] — a requirement met by a decoy.
func TestChainValidateAllowsChainWithNoMachineGate(t *testing.T) {
	c := chainWith(
		sessionBlock("build"),
		Block{ID: "review", Type: BlockReview, Rubric: "is it good"},
	)
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a chain with no command block to be valid, got: %v", err)
	}
	if c.HasMachineGate() {
		t.Fatal("expected HasMachineGate to be false for a session+review chain")
	}
}

func TestChainHasMachineGateFindsCommandBlock(t *testing.T) {
	c := chainWith(
		sessionBlock("build"),
		Block{ID: "tests", Type: BlockCommand, Check: []string{"make", "check"}},
	)
	if err := c.Validate(); err != nil {
		t.Fatalf("expected chain to be valid, got: %v", err)
	}
	if !c.HasMachineGate() {
		t.Fatal("expected HasMachineGate to be true when a command block is present")
	}
}

func TestEvalBlockIsMachineDecided(t *testing.T) {
	c := chainWith(
		sessionBlock("build"),
		Block{ID: "quality", Type: BlockEval, EvalKey: "answer-quality"},
	)
	if err := c.Validate(); err != nil {
		t.Fatalf("expected chain to be valid, got: %v", err)
	}
	if !c.HasMachineGate() {
		t.Fatal("an eval block scored by the server must count as a machine gate")
	}
}

func TestMachineDecidedIsTrueForCommandAndEval(t *testing.T) {
	for _, tc := range []struct {
		typ  BlockType
		want bool
	}{
		{BlockCommand, true},
		{BlockSession, false},
		{BlockReview, false},
		{BlockHuman, false},
		{BlockEval, true},
	} {
		if got := (Block{Type: tc.typ}).MachineDecided(); got != tc.want {
			t.Errorf("MachineDecided(%s) = %t, want %t", tc.typ, got, tc.want)
		}
	}
}

func TestChainValidateRejectsWrongVersion(t *testing.T) {
	c := chainWith(sessionBlock("build"))
	c.Version = 1
	assertErrContains(t, c.Validate(), "version must be 2")
}

func TestChainValidateRejectsEmptyChain(t *testing.T) {
	c := &Chain{Version: ChainVersion}
	assertErrContains(t, c.Validate(), "at least one phase is required")
}

func TestChainValidateRejectsDuplicatePhaseID(t *testing.T) {
	c := &Chain{
		Version: ChainVersion,
		Phases: []Phase{
			{ID: "p", Blocks: []Block{sessionBlock("a")}, Limits: okLimits()},
			{ID: "p", Blocks: []Block{sessionBlock("b")}, Limits: okLimits()},
		},
	}
	assertErrContains(t, c.Validate(), `phase id "p" is duplicated`)
}

func TestChainValidateRejectsPhaseWithNoBlocks(t *testing.T) {
	c := &Chain{
		Version: ChainVersion,
		Phases:  []Phase{{ID: "p", Limits: okLimits()}},
	}
	assertErrContains(t, c.Validate(), "at least one block is required")
}

func TestChainValidateRejectsDuplicateBlockID(t *testing.T) {
	c := chainWith(sessionBlock("dup"), sessionBlock("dup"))
	assertErrContains(t, c.Validate(), `block id "dup" is duplicated`)
}

// Every limit must actually bound the phase. A phase with an unbounded limit
// is the runaway the engine exists to refuse.
func TestChainValidateRequiresEveryLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits PhaseLimits
		want   string
	}{
		{"max_steps", PhaseLimits{MaxRounds: 3, NoProgressStalls: 2}, "limits.max_steps must be > 0"},
		{"max_rounds", PhaseLimits{MaxSteps: 10, NoProgressStalls: 2}, "limits.max_rounds must be > 0"},
		{"no_progress_stalls", PhaseLimits{MaxSteps: 10, MaxRounds: 3}, "limits.no_progress_stalls must be > 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := chainWith(sessionBlock("build"))
			c.Phases[0].Limits = tc.limits
			assertErrContains(t, c.Validate(), tc.want)
		})
	}
}

func TestChainValidateRejectsNegativeWait(t *testing.T) {
	c := chainWith(sessionBlock("build"))
	c.Phases[0].Limits.MaxWaitSeconds = -1
	assertErrContains(t, c.Validate(), "limits.max_wait_seconds cannot be negative")
}

func TestChainValidateRequiresBlockType(t *testing.T) {
	c := chainWith(Block{ID: "b"})
	assertErrContains(t, c.Validate(), "type is required")
}

func TestChainValidateRejectsUnknownBlockType(t *testing.T) {
	c := chainWith(Block{ID: "b", Type: "deploy"})
	assertErrContains(t, c.Validate(), `unknown type "deploy"`)
}

func TestChainValidateRequiresTypeSpecificFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block Block
		want  string
	}{
		{"session without skill", Block{ID: "b", Type: BlockSession}, "session block needs a skill"},
		{"command without argv", Block{ID: "b", Type: BlockCommand}, "command block needs a non-empty check argv"},
		{"review without rubric", Block{ID: "b", Type: BlockReview}, "review block needs a rubric"},
		{"human without prompt", Block{ID: "b", Type: BlockHuman}, "human block needs a prompt"},
		{"eval without key", Block{ID: "b", Type: BlockEval}, "eval block needs an eval_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertErrContains(t, chainWith(tc.block).Validate(), tc.want)
		})
	}
}

func TestChainValidateRejectsUnsupportedExpect(t *testing.T) {
	c := chainWith(Block{ID: "b", Type: BlockCommand, Check: []string{"make"}, Expect: "exit_one"})
	assertErrContains(t, c.Validate(), `unsupported expect "exit_one"`)
}

func TestChainValidateRejectsBadApprover(t *testing.T) {
	t.Run("unknown type", func(t *testing.T) {
		c := chainWith(Block{ID: "b", Type: BlockHuman, Prompt: "ok?", ApproverType: "robot", ApproverID: "x"})
		assertErrContains(t, c.Validate(), "approver_type must be")
	})
	t.Run("type without id", func(t *testing.T) {
		c := chainWith(Block{ID: "b", Type: BlockHuman, Prompt: "ok?", ApproverType: AssigneeMember})
		assertErrContains(t, c.Validate(), "needs an approver_id")
	})
}

func TestChainValidateAcceptsMemberApprover(t *testing.T) {
	c := chainWith(Block{
		ID: "signoff", Type: BlockHuman, Prompt: "Ship it?",
		ApproverType: AssigneeMember, ApproverID: "member-1",
	})
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a member-approved human block to be valid, got: %v", err)
	}
}

// A command block is run by the engine itself, so letting an author pick an
// agent for it would mean the editor and the engine disagree about what a
// command block is.
func TestChainValidateRejectsAgentOnCommandBlock(t *testing.T) {
	c := chainWith(Block{
		ID: "tests", Type: BlockCommand, Check: []string{"make", "check"},
		Agents: []AgentRef{{AgentID: "a1"}},
	})
	assertErrContains(t, c.Validate(), "command block cannot pick an agent")
}

func TestChainValidateRejectsAgentWithoutID(t *testing.T) {
	b := sessionBlock("build")
	b.Agents = []AgentRef{{Model: "opus"}}
	assertErrContains(t, chainWith(b).Validate(), "needs an agent_id")
}

// Several agents on one block is the point: a run must not stall because one
// agent hit its runtime limit.
func TestChainValidateAcceptsMultipleAgents(t *testing.T) {
	b := sessionBlock("build")
	b.Agents = []AgentRef{{AgentID: "a1"}, {AgentID: "a2"}}
	b.OnAllBusy = BusyWakeup
	if err := chainWith(b).Validate(); err != nil {
		t.Fatalf("expected a multi-agent block to be valid, got: %v", err)
	}
}

func TestChainValidateRejectsUnknownBusyPolicy(t *testing.T) {
	b := sessionBlock("build")
	b.OnAllBusy = "retry"
	assertErrContains(t, chainWith(b).Validate(), `unknown on_all_busy "retry"`)
}

func TestChainValidateAcceptsEveryBusyPolicy(t *testing.T) {
	for _, p := range []BusyPolicy{BusyWait, BusyPause, BusyWakeup, BusyPingMember} {
		t.Run(string(p), func(t *testing.T) {
			b := sessionBlock("build")
			b.OnAllBusy = p
			if err := chainWith(b).Validate(); err != nil {
				t.Fatalf("expected on_all_busy %q to be valid, got: %v", p, err)
			}
		})
	}
}

func TestChainValidateRequiresStepsMax(t *testing.T) {
	b := sessionBlock("build")
	b.Steps = StepsConfig{Allowed: true}
	assertErrContains(t, chainWith(b).Validate(), "steps block needs steps.max > 0")
}

// A steps block may never outrun the phase that owns it — the phase's limit is
// the real bound, so this is caught at save time rather than at round 11.
func TestChainValidateRejectsStepsMaxAbovePhaseLimit(t *testing.T) {
	b := sessionBlock("build")
	b.Steps = StepsConfig{Allowed: true, Max: 11}
	c := chainWith(b)
	c.Phases[0].Limits.MaxSteps = 10
	assertErrContains(t, c.Validate(), "exceeds the phase's limits.max_steps 10")
}

func TestChainValidateAcceptsStepsBlockWithinPhaseLimit(t *testing.T) {
	b := sessionBlock("build")
	b.Steps = StepsConfig{Allowed: true, Max: 4}
	c := chainWith(b)
	c.Phases[0].Limits.MaxSteps = 10
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a steps block inside the phase limit to be valid, got: %v", err)
	}
}

// The gstack sprint is the shape the old engine could not express: an ordered
// chain where plan, build, review, test, ship and reflect are all the same
// kind of thing, with the review-heavy middle carrying no code gate at all.
func TestChainValidateAcceptsGstackShapedChain(t *testing.T) {
	c := &Chain{
		Version:    ChainVersion,
		DoneStatus: "done",
		Phases: []Phase{
			{
				ID: "think", Name: "Think", Status: "todo",
				Blocks: []Block{{ID: "think", Type: BlockSession, Skill: "think"}},
				Limits: okLimits(),
			},
			{
				ID: "plan", Name: "Plan",
				Blocks: []Block{
					{ID: "plan", Type: BlockSession, Skill: "plan"},
					{ID: "plan-review", Type: BlockReview, Rubric: "tear the plan down"},
				},
				Limits: okLimits(),
			},
			{
				ID: "build", Name: "Build", Status: "in_progress",
				Blocks: []Block{
					{ID: "build", Type: BlockSession, Skill: "build",
						Steps:  StepsConfig{Allowed: true, Max: 5},
						Agents: []AgentRef{{AgentID: "a1"}, {AgentID: "a2"}}, OnAllBusy: BusyWait},
					{ID: "tests", Type: BlockCommand, Check: []string{"make", "check"}},
				},
				Limits: PhaseLimits{MaxSteps: 8, MaxRounds: 4, NoProgressStalls: 2, MaxWaitSeconds: 600},
			},
			{
				ID: "ship", Name: "Ship", Status: "in_review",
				Blocks: []Block{
					{ID: "signoff", Type: BlockHuman, Prompt: "Ship it?",
						ApproverType: AssigneeMember, ApproverID: "member-1"},
				},
				Limits: okLimits(),
			},
			{
				ID: "reflect", Name: "Reflect",
				Blocks: []Block{{ID: "retro", Type: BlockSession, Skill: "retro"}},
				Limits: okLimits(),
			},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected the gstack-shaped chain to be valid, got: %v", err)
	}
	if !c.HasMachineGate() {
		t.Fatal("expected the gstack chain's command block to register as a machine gate")
	}
}

// Validate aggregates rather than failing on the first problem, so an author
// fixing a chain sees every mistake at once.
func TestChainValidateAggregatesErrors(t *testing.T) {
	c := &Chain{
		Version: 1,
		Phases: []Phase{{
			ID:     "p",
			Blocks: []Block{{ID: "b", Type: BlockSession}},
		}},
	}
	err := c.Validate()
	for _, want := range []string{
		"version must be 2",
		"limits.max_steps must be > 0",
		"limits.max_rounds must be > 0",
		"limits.no_progress_stalls must be > 0",
		"session block needs a skill",
	} {
		assertErrContains(t, err, want)
	}
}
