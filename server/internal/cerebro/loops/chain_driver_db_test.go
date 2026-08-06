package loops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type blockingBlockDispatcher struct {
	entered chan struct{}
	release chan struct{}
}

func (d *blockingBlockDispatcher) DispatchBlock(context.Context, BlockDispatch) (BlockDispatchResult, error) {
	d.entered <- struct{}{}
	<-d.release
	return BlockDispatchResult{Status: StepRunning}, nil
}

type retryingBlockDispatcher struct {
	calls int
}

type completingBlockDispatcher struct {
	dispatches []BlockDispatch
}

type recordingChainLifecycle struct {
	completed int
}

func (l *recordingChainLifecycle) AfterStepCompleted(context.Context, BlockDispatch, ChainStep) error {
	l.completed++
	return nil
}

func (d *completingBlockDispatcher) DispatchBlock(_ context.Context, dispatch BlockDispatch) (BlockDispatchResult, error) {
	d.dispatches = append(d.dispatches, dispatch)
	outcome, _ := json.Marshal(map[string]any{"output": dispatch.Block.ID + " output"})
	return BlockDispatchResult{Status: StepCompleted, Outcome: outcome}, nil
}

func TestChainDriver_CarriesPreviousStepsAcrossPhases(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	dispatcher := &completingBlockDispatcher{}
	driver := NewChainDriver(NewStore(loopTestPool), dispatcher)
	lifecycle := &recordingChainLifecycle{}
	driver.lifecycle = lifecycle
	limits := PhaseLimits{MaxSteps: 2, MaxRounds: 1, NoProgressStalls: 1}
	chain := &Chain{Version: ChainVersion, Phases: []Phase{
		{ID: "build", Limits: limits, Blocks: []Block{{ID: "builder", Type: BlockSession, Skill: "build"}}},
		{ID: "approval", Limits: limits, Blocks: []Block{{ID: "approver", Type: BlockHuman, Prompt: "Approve {{previous.output}}", ApproverType: AssigneeMember, ApproverID: "member"}}},
	}}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "in_progress", AgentID: "agent"}

	decision, err := driver.Advance(ctx, chain, run)
	if err != nil || decision.Kind != ChainDone {
		t.Fatalf("advance = %+v, error = %v", decision, err)
	}
	if len(dispatcher.dispatches) != 2 {
		t.Fatalf("dispatches = %+v", dispatcher.dispatches)
	}
	if lifecycle.completed != 2 {
		t.Fatalf("completed lifecycle events = %d, want 2", lifecycle.completed)
	}
	previous := dispatcher.dispatches[1].PreviousSteps
	if len(previous) != 1 || previous[0].BlockID != "builder" || !strings.Contains(string(previous[0].Outcome), "builder output") {
		t.Fatalf("approval previous steps = %+v", previous)
	}
}

func (d *retryingBlockDispatcher) DispatchBlock(context.Context, BlockDispatch) (BlockDispatchResult, error) {
	d.calls++
	if d.calls == 1 {
		return BlockDispatchResult{Status: StepPending, Outcome: json.RawMessage(`{"all_agents_busy":true,"policy":"wait"}`)}, nil
	}
	return BlockDispatchResult{Status: StepRunning, Outcome: json.RawMessage(`{"dispatched":true}`)}, nil
}

func TestChainDriver_RunsEveryBlockThroughOneIssueBoundPath(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	var workspaceID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, issueID).Scan(&workspaceID); err != nil {
		t.Fatalf("load workspace: %v", err)
	}

	agentID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	queries := &fakeDispatchQueries{
		agent: db.Agent{
			ID:                 agentID,
			WorkspaceID:        workspaceID,
			RuntimeID:          runtimeID,
			MaxConcurrentTasks: 1,
		},
		runtimes: map[[16]byte]db.AgentRuntime{
			runtimeID.Bytes: {ID: runtimeID, Status: "online"},
		},
		agentSkills: map[[16]byte][]db.ListAgentSkillsRow{
			agentID.Bytes: {{Name: "build-skill"}, {Name: "review-skill"}},
		},
		issue: db.Issue{
			ID:          issueID,
			WorkspaceID: workspaceID,
			Title:       "Run the block chain",
		},
	}
	store := NewStore(loopTestPool)
	driver := NewChainDriver(store, NewTaskDispatcher(queries).WithEvalBlockRunner(&waitingEvalBlockRunner{}))
	chain := &Chain{
		Version:    ChainVersion,
		DoneStatus: "done",
		Phases: []Phase{{
			ID: "delivery", Status: "in_progress",
			Limits: PhaseLimits{MaxSteps: 5, MaxRounds: 2, NoProgressStalls: 2},
			Blocks: []Block{
				{ID: "build", Type: BlockSession, Skill: "build-skill", Goal: "Build the approved scope"},
				{ID: "tests", Type: BlockCommand, Check: []string{"go", "test", "./internal/cerebro/loops"}},
				{ID: "review", Type: BlockReview, Skill: "review-skill", Rubric: "Reject any scope drift"},
				{ID: "signoff", Type: BlockHuman, Prompt: "Approve the delivery", ApproverType: AssigneeAgent, ApproverID: uuidToString(agentID)},
				{ID: "quality", Type: BlockEval, EvalKey: "workflow-quality"},
			},
		}},
	}
	run := ChainRun{
		IssueID:     issueID,
		WorkflowID:  workflowID,
		IssueStatus: "in_progress",
		AgentID:     uuidToString(agentID),
	}

	for _, wantBlock := range []string{"build", "tests", "review", "signoff", "quality"} {
		decision, err := driver.Advance(ctx, chain, run)
		if err != nil {
			t.Fatalf("advance to %s: %v", wantBlock, err)
		}
		if decision.Kind != ChainWait || decision.Step.BlockID != wantBlock {
			t.Fatalf("advance decision for %s = %+v", wantBlock, decision)
		}
		outcome, _ := json.Marshal(map[string]any{"block": wantBlock, "passed": true})
		if err := store.RecordStepOutcome(ctx, decision.Step.StepRef, StepCompleted, outcome); err != nil {
			t.Fatalf("complete %s: %v", wantBlock, err)
		}
	}

	decision, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("finish chain: %v", err)
	}
	if decision.Kind != ChainDone || decision.Status != "done" {
		t.Fatalf("final decision = %+v", decision)
	}
	phase, err := store.LoadPhaseRun(ctx, PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: "delivery"})
	if err != nil {
		t.Fatalf("load completed phase: %v", err)
	}
	if phase.Status != PhaseCompleted {
		t.Fatalf("phase status = %q, want completed", phase.Status)
	}

	// Session, command, review, and agent-human blocks all use issue-bound
	// tasks. Eval waits without creating an agent task until its run reports.
	if len(queries.comments) != 4 || len(queries.issueTasks) != 4 {
		t.Fatalf("issue-bound dispatches: comments=%d tasks=%d", len(queries.comments), len(queries.issueTasks))
	}
	var reviewComment db.CreateCommentParams
	for _, comment := range queries.comments {
		if strings.Contains(comment.Content, "Reject any scope drift") {
			reviewComment = comment
			break
		}
	}
	if !reviewComment.IssueID.Valid || reviewComment.IssueID != issueID {
		t.Fatalf("review did not land on the issue: %+v", reviewComment)
	}
	if !strings.Contains(reviewComment.Content, "review-skill") {
		t.Fatalf("review prompt omitted its skill: %q", reviewComment.Content)
	}
}

func TestChainDriver_ConcurrentAdvanceDispatchesStepOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	dispatcher := &blockingBlockDispatcher{entered: make(chan struct{}, 2), release: make(chan struct{})}
	driver := NewChainDriver(NewStore(loopTestPool), dispatcher)
	chain := &Chain{
		Version: ChainVersion,
		Phases: []Phase{{
			ID: "build", Status: "in_progress",
			Limits: PhaseLimits{MaxSteps: 1, MaxRounds: 1, NoProgressStalls: 1},
			Blocks: []Block{{ID: "build", Type: BlockSession, Skill: "build"}},
		}},
	}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "in_progress", AgentID: "agent"}

	firstDone := make(chan error, 1)
	go func() {
		_, err := driver.Advance(ctx, chain, run)
		firstDone <- err
	}()
	<-dispatcher.entered

	secondDone := make(chan error, 1)
	go func() {
		_, err := driver.Advance(ctx, chain, run)
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second advance: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(dispatcher.release)
		<-firstDone
		<-secondDone
		t.Fatal("second advance dispatched the already-claimed step")
	}
	close(dispatcher.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first advance: %v", err)
	}
	select {
	case <-dispatcher.entered:
		t.Fatal("step was dispatched more than once")
	default:
	}
}

func TestChainDriver_RetriesPendingBusyStep(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	dispatcher := &retryingBlockDispatcher{}
	driver := NewChainDriver(NewStore(loopTestPool), dispatcher)
	chain := &Chain{
		Version: ChainVersion,
		Phases: []Phase{{
			ID: "build", Status: "in_progress",
			Limits: PhaseLimits{MaxSteps: 1, MaxRounds: 1, NoProgressStalls: 1, MaxWaitSeconds: 60},
			Blocks: []Block{{ID: "build", Type: BlockSession, Skill: "build", OnAllBusy: BusyWait}},
		}},
	}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "in_progress", AgentID: "agent"}

	first, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("first advance: %v", err)
	}
	if first.Kind != ChainWait || first.Step.Status != StepPending {
		t.Fatalf("first decision = %+v, want pending retry", first)
	}
	second, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("second advance: %v", err)
	}
	if second.Kind != ChainWait || second.Step.Status != StepRunning {
		t.Fatalf("second decision = %+v, want running dispatch", second)
	}
	if dispatcher.calls != 2 {
		t.Fatalf("dispatch calls = %d, want 2", dispatcher.calls)
	}
}

func TestChainDriver_DispatchesAgentOpenedStepBeforeNextBlock(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	store := NewStore(loopTestPool)
	dispatcher := &recordingStepDispatcher{}
	driver := NewChainDriver(store, dispatcher)
	limits := PhaseLimits{MaxSteps: 3, MaxRounds: 1, NoProgressStalls: 1}
	chain := &Chain{Version: ChainVersion, Phases: []Phase{{
		ID: "delivery", Status: "in_progress", Limits: limits,
		Blocks: []Block{
			{ID: "build", Type: BlockSession, Skill: "build", Steps: StepsConfig{Allowed: true, Max: 2}},
			{ID: "review", Type: BlockReview, Rubric: "Review the build"},
		},
	}}}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "in_progress", AgentID: "agent"}

	first, err := driver.Advance(ctx, chain, run)
	if err != nil || first.Step.BlockID != "build" || first.Step.Number != 1 {
		t.Fatalf("dispatch first build step: decision=%+v err=%v", first, err)
	}
	if _, _, err := store.OpenNextStep(ctx, first.Step.StepRef, chain.Phases[0].Blocks[0].Steps, limits); err != nil {
		t.Fatalf("agent opens second build step: %v", err)
	}
	if err := store.RecordStepOutcome(ctx, first.Step.StepRef, StepCompleted, json.RawMessage(`{"done":true}`)); err != nil {
		t.Fatalf("complete first build step: %v", err)
	}

	second, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("dispatch second build step: %v", err)
	}
	if second.Step.BlockID != "build" || second.Step.Number != 2 {
		t.Fatalf("agent-opened step was skipped: %+v", second)
	}
	if len(dispatcher.steps) != 2 || dispatcher.steps[1].Number != 2 {
		t.Fatalf("dispatch order = %+v", dispatcher.steps)
	}
}

type recordingStepDispatcher struct {
	steps []StepRef
}

func (d *recordingStepDispatcher) DispatchBlock(_ context.Context, dispatch BlockDispatch) (BlockDispatchResult, error) {
	d.steps = append(d.steps, dispatch.Step.StepRef)
	return BlockDispatchResult{Status: StepRunning}, nil
}

// Per-step status control: the board should show "in review" while a review
// step waits, which the chain could not express when the only statuses were
// the phase's and the chain's final one. The statuses are applied at step
// boundaries, so a step never has its status pulled out from under it.
func TestChainDriver_SetsStatusAroundEachStep(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	store := NewStore(loopTestPool)
	driver := NewChainDriver(store, &recordingStepDispatcher{})
	chain := &Chain{
		Version:    ChainVersion,
		DoneStatus: "done",
		Phases: []Phase{{
			ID: "delivery", Status: "todo",
			Limits: PhaseLimits{MaxSteps: 4, MaxRounds: 1, NoProgressStalls: 1},
			Blocks: []Block{
				{ID: "build", Type: BlockSession, Skill: "build", StatusOnStart: "in_progress"},
				{ID: "review", Type: BlockReview, Rubric: "Review the build", StatusOnStart: "in_review"},
			},
		}},
	}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "backlog", AgentID: "agent"}

	// The first step names its own entry status, so that one wins over the
	// phase status — one status change at the boundary, not two competing ones.
	decision, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("build status advance: %v", err)
	}
	if decision.Kind != ChainSetStatus || decision.Status != "in_progress" {
		t.Fatalf("build status decision = %+v, want set_status in_progress", decision)
	}
	run.IssueStatus = decision.Status

	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("dispatch build: %v", err)
	}
	if decision.Kind != ChainWait || decision.Step.BlockID != "build" {
		t.Fatalf("build dispatch decision = %+v", decision)
	}
	if err := store.RecordStepOutcome(ctx, decision.Step.StepRef, StepCompleted, nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}

	// The review step moves the issue to in_review before it is dispatched —
	// and the phase status does not drag it back to todo.
	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("review status advance: %v", err)
	}
	if decision.Kind != ChainSetStatus || decision.Status != "in_review" {
		t.Fatalf("review status decision = %+v, want set_status in_review", decision)
	}
	run.IssueStatus = decision.Status

	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("dispatch review: %v", err)
	}
	if decision.Kind != ChainWait || decision.Step.BlockID != "review" {
		t.Fatalf("review dispatch decision = %+v", decision)
	}
	if err := store.RecordStepOutcome(ctx, decision.Step.StepRef, StepCompleted, nil); err != nil {
		t.Fatalf("complete review: %v", err)
	}

	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("finish chain: %v", err)
	}
	if decision.Kind != ChainDone || decision.Status != "done" {
		t.Fatalf("final decision = %+v, want done", decision)
	}
}

// A step's exit status has to land even when it is the last step of the last
// phase, where there is no following step boundary to carry it.
func TestChainDriver_AppliesLastStepExitStatus(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	store := NewStore(loopTestPool)
	driver := NewChainDriver(store, &recordingStepDispatcher{})
	chain := &Chain{
		Version:    ChainVersion,
		DoneStatus: "done",
		Phases: []Phase{{
			ID:     "delivery",
			Limits: PhaseLimits{MaxSteps: 2, MaxRounds: 1, NoProgressStalls: 1},
			Blocks: []Block{{ID: "build", Type: BlockSession, Skill: "build", StatusOnDone: "in_review"}},
		}},
	}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "in_progress", AgentID: "agent"}

	decision, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("dispatch build: %v", err)
	}
	if err := store.RecordStepOutcome(ctx, decision.Step.StepRef, StepCompleted, nil); err != nil {
		t.Fatalf("complete build: %v", err)
	}

	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("exit status advance: %v", err)
	}
	if decision.Kind != ChainSetStatus || decision.Status != "in_review" {
		t.Fatalf("exit status decision = %+v, want set_status in_review", decision)
	}
	run.IssueStatus = decision.Status

	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("finish chain: %v", err)
	}
	if decision.Kind != ChainDone || decision.Status != "done" {
		t.Fatalf("final decision = %+v, want done", decision)
	}
}

// The phase status still carries a phase whose first step does not name its
// own entry status — it is the phase's opening status, not a second mechanism.
func TestChainDriver_UsesPhaseStatusWhenFirstStepNamesNone(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}

	ctx := context.Background()
	issueID := seedIssue(t)
	workflowID := seedLoopWorkflow(t, issueID)
	driver := NewChainDriver(NewStore(loopTestPool), &recordingStepDispatcher{})
	chain := &Chain{
		Version:    ChainVersion,
		DoneStatus: "done",
		Phases: []Phase{{
			ID: "delivery", Status: "in_progress",
			Limits: PhaseLimits{MaxSteps: 2, MaxRounds: 1, NoProgressStalls: 1},
			Blocks: []Block{{ID: "build", Type: BlockSession, Skill: "build"}},
		}},
	}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: "todo", AgentID: "agent"}

	decision, err := driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("phase status advance: %v", err)
	}
	if decision.Kind != ChainSetStatus || decision.Status != "in_progress" {
		t.Fatalf("phase status decision = %+v, want set_status in_progress", decision)
	}
	run.IssueStatus = decision.Status

	decision, err = driver.Advance(ctx, chain, run)
	if err != nil {
		t.Fatalf("dispatch build: %v", err)
	}
	if decision.Kind != ChainWait || decision.Step.BlockID != "build" {
		t.Fatalf("build dispatch decision = %+v", decision)
	}
}
