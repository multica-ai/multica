package loops

// Pure unit tests for the Chain v2 block dispatcher.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeDispatchQueries stubs the upstream issue queries the TaskDispatcher needs.
type fakeDispatchQueries struct {
	agent             db.Agent
	agents            map[[16]byte]db.Agent
	runtimes          map[[16]byte]db.AgentRuntime
	runningTasks      map[[16]byte]int64
	agentSkills       map[[16]byte][]db.ListAgentSkillsRow
	getErr            error
	issue             db.Issue
	getIssueErr       error
	created           []db.CreateQuickCreateTaskParams
	comments          []db.CreateCommentParams
	issueTasks        []db.CreateAgentTaskParams
	inboxItems        []db.CreateInboxItemParams
	modelOverrides    []db.SetAgentTaskModelOverrideParams
	thinkingOverrides []db.SetAgentTaskThinkingOverrideParams
}

type fakeBusyWakeupScheduler struct {
	calls []BlockDispatch
}

func (f *fakeBusyWakeupScheduler) ScheduleBusyWakeup(_ context.Context, d BlockDispatch) error {
	f.calls = append(f.calls, d)
	return nil
}

type fakeEvalBlockRunner struct{ calls int }

func (f *fakeEvalBlockRunner) RunEvalBlock(_ context.Context, _ BlockDispatch) (StepStatus, json.RawMessage, error) {
	f.calls++
	return StepCompleted, json.RawMessage(`{"score":1}`), nil
}

type waitingEvalBlockRunner struct{}

func (*waitingEvalBlockRunner) RunEvalBlock(_ context.Context, _ BlockDispatch) (StepStatus, json.RawMessage, error) {
	return StepWaiting, json.RawMessage(`{"pending":true}`), nil
}

func TestTaskDispatcher_EvalBlockRunsServerEvalAndCompletes(t *testing.T) {
	runner := &fakeEvalBlockRunner{}
	got, err := NewTaskDispatcher(&fakeDispatchQueries{}).WithEvalBlockRunner(runner).DispatchBlock(context.Background(), BlockDispatch{Block: Block{ID: "quality", Type: BlockEval, EvalKey: "delivery"}})
	if err != nil || got.Status != StepCompleted || runner.calls != 1 {
		t.Fatalf("eval dispatch = %+v calls=%d err=%v", got, runner.calls, err)
	}
}

func (f *fakeDispatchQueries) GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error) {
	if agent, ok := f.agents[id.Bytes]; ok {
		return agent, nil
	}
	return f.agent, f.getErr
}

func (f *fakeDispatchQueries) GetAgentRuntime(_ context.Context, id pgtype.UUID) (db.AgentRuntime, error) {
	return f.runtimes[id.Bytes], nil
}

func (f *fakeDispatchQueries) CountRunningTasks(_ context.Context, id pgtype.UUID) (int64, error) {
	return f.runningTasks[id.Bytes], nil
}

func (f *fakeDispatchQueries) ListAgentSkills(_ context.Context, id pgtype.UUID) ([]db.ListAgentSkillsRow, error) {
	return f.agentSkills[id.Bytes], nil
}

func (f *fakeDispatchQueries) CreateInboxItem(_ context.Context, arg db.CreateInboxItemParams) (db.InboxItem, error) {
	f.inboxItems = append(f.inboxItems, arg)
	return db.InboxItem{ID: mustDispatchUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")}, nil
}

func (f *fakeDispatchQueries) CreateQuickCreateTask(ctx context.Context, arg db.CreateQuickCreateTaskParams) (db.AgentTaskQueue, error) {
	f.created = append(f.created, arg)
	return db.AgentTaskQueue{ID: mustDispatchUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")}, nil
}

func (f *fakeDispatchQueries) SetAgentTaskModelOverride(ctx context.Context, arg db.SetAgentTaskModelOverrideParams) error {
	f.modelOverrides = append(f.modelOverrides, arg)
	return nil
}

func (f *fakeDispatchQueries) SetAgentTaskThinkingOverride(ctx context.Context, arg db.SetAgentTaskThinkingOverrideParams) error {
	f.thinkingOverrides = append(f.thinkingOverrides, arg)
	return nil
}

func (f *fakeDispatchQueries) GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error) {
	if f.getIssueErr != nil {
		return db.Issue{}, f.getIssueErr
	}
	if f.issue.ID.Valid {
		return f.issue, nil
	}
	return db.Issue{ID: id}, nil
}

func (f *fakeDispatchQueries) CreateComment(ctx context.Context, arg db.CreateCommentParams) (db.Comment, error) {
	f.comments = append(f.comments, arg)
	var id pgtype.UUID
	_ = id.Scan("99999999-9999-9999-9999-999999999999")
	return db.Comment{ID: id, IssueID: arg.IssueID, AuthorType: arg.AuthorType, AuthorID: arg.AuthorID, Content: arg.Content}, nil
}

func (f *fakeDispatchQueries) CreateAgentTask(ctx context.Context, arg db.CreateAgentTaskParams) (db.AgentTaskQueue, error) {
	f.issueTasks = append(f.issueTasks, arg)
	return db.AgentTaskQueue{ID: mustDispatchUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")}, nil
}

func mustDispatchUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func mustScanUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

func TestTaskDispatcher_DispatchBlockUsesFirstAvailableAgent(t *testing.T) {
	firstID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	secondID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	firstRuntime := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	secondRuntime := mustScanUUID(t, "44444444-4444-4444-4444-444444444444")
	issueID := mustScanUUID(t, "55555555-5555-5555-5555-555555555555")
	workspaceID := mustScanUUID(t, "66666666-6666-6666-6666-666666666666")
	q := &fakeDispatchQueries{
		agents: map[[16]byte]db.Agent{
			firstID.Bytes:  {ID: firstID, WorkspaceID: workspaceID, RuntimeID: firstRuntime, MaxConcurrentTasks: 1},
			secondID.Bytes: {ID: secondID, WorkspaceID: workspaceID, RuntimeID: secondRuntime, MaxConcurrentTasks: 1},
		},
		runtimes: map[[16]byte]db.AgentRuntime{
			firstRuntime.Bytes:  {ID: firstRuntime, Status: "online"},
			secondRuntime.Bytes: {ID: secondRuntime, Status: "online"},
		},
		runningTasks: map[[16]byte]int64{firstID.Bytes: 1},
		agentSkills: map[[16]byte][]db.ListAgentSkillsRow{
			secondID.Bytes: {{Name: "build-skill"}},
		},
		issue: db.Issue{ID: issueID, WorkspaceID: workspaceID, Title: "Build"},
	}

	result, err := NewTaskDispatcher(q).DispatchBlock(context.Background(), BlockDispatch{
		Run:   ChainRun{IssueID: issueID},
		Phase: Phase{ID: "build", Limits: PhaseLimits{MaxSteps: 3, MaxRounds: 1, NoProgressStalls: 1, MaxWaitSeconds: 60}},
		Block: Block{ID: "build", Type: BlockSession, Skill: "build-skill", Agents: []AgentRef{{AgentID: uuidToString(firstID)}, {AgentID: uuidToString(secondID)}}, Steps: StepsConfig{Allowed: true, Max: 2}},
		Step:  ChainStep{StepRef: StepRef{BlockID: "build", Number: 1}},
	})
	if err != nil {
		t.Fatalf("dispatch block: %v", err)
	}
	if result.Status != StepRunning {
		t.Fatalf("status = %q, want running", result.Status)
	}
	if len(q.issueTasks) != 1 || q.issueTasks[0].AgentID != secondID {
		t.Fatalf("task agent = %+v, want second available agent", q.issueTasks)
	}
	var taskContext struct {
		LoopStep struct {
			Steps  StepsConfig `json:"steps"`
			Limits PhaseLimits `json:"phase_limits"`
		} `json:"loop_step"`
	}
	if err := json.Unmarshal(q.issueTasks[0].Context, &taskContext); err != nil {
		t.Fatalf("decode workflow task context: %v", err)
	}
	if !taskContext.LoopStep.Steps.Allowed || taskContext.LoopStep.Steps.Max != 2 || taskContext.LoopStep.Limits.MaxSteps != 3 {
		t.Fatalf("step capability missing from task context: %+v", taskContext.LoopStep)
	}
	if len(q.comments) != 1 || !strings.Contains(q.comments[0].Content, "open_loop_step") {
		t.Fatalf("steps-enabled prompt omitted open_loop_step: %+v", q.comments)
	}
}

func TestTaskDispatcher_DispatchBlockRejectsRunnerMissingSkill(t *testing.T) {
	agentID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	issueID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	workspaceID := mustScanUUID(t, "44444444-4444-4444-4444-444444444444")
	q := &fakeDispatchQueries{
		agents:      map[[16]byte]db.Agent{agentID.Bytes: {ID: agentID, WorkspaceID: workspaceID, RuntimeID: runtimeID, MaxConcurrentTasks: 1}},
		runtimes:    map[[16]byte]db.AgentRuntime{runtimeID.Bytes: {ID: runtimeID, Status: "online"}},
		agentSkills: map[[16]byte][]db.ListAgentSkillsRow{agentID.Bytes: {}},
		issue:       db.Issue{ID: issueID, WorkspaceID: workspaceID, Title: "Build"},
	}

	_, err := NewTaskDispatcher(q).DispatchBlock(context.Background(), BlockDispatch{
		Run:   ChainRun{IssueID: issueID},
		Phase: Phase{ID: "build", Limits: PhaseLimits{MaxWaitSeconds: 60}},
		Block: Block{ID: "build", Type: BlockSession, Skill: "build-skill", Agents: []AgentRef{{AgentID: uuidToString(agentID)}}},
		Step:  ChainStep{StepRef: StepRef{BlockID: "build", Number: 1}},
	})
	if err == nil {
		t.Fatal("expected missing attached skill to fail dispatch")
	}
	if len(q.issueTasks) != 0 {
		t.Fatal("no task should be enqueued without the configured skill")
	}
}

func TestTaskDispatcher_DispatchBlockRequiresEveryConfiguredSkill(t *testing.T) {
	agentID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	issueID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	workspaceID := mustScanUUID(t, "44444444-4444-4444-4444-444444444444")
	q := &fakeDispatchQueries{
		agents:      map[[16]byte]db.Agent{agentID.Bytes: {ID: agentID, WorkspaceID: workspaceID, RuntimeID: runtimeID, MaxConcurrentTasks: 1}},
		runtimes:    map[[16]byte]db.AgentRuntime{runtimeID.Bytes: {ID: runtimeID, Status: "online"}},
		agentSkills: map[[16]byte][]db.ListAgentSkillsRow{agentID.Bytes: {{Name: "plan"}}},
		issue:       db.Issue{ID: issueID, WorkspaceID: workspaceID, Title: "Build"},
	}

	_, err := NewTaskDispatcher(q).DispatchBlock(context.Background(), BlockDispatch{
		Run:   ChainRun{IssueID: issueID},
		Phase: Phase{ID: "build", Limits: PhaseLimits{MaxWaitSeconds: 60}},
		Block: Block{ID: "build", Type: BlockSession, Skills: []string{"plan", "review"}, Agents: []AgentRef{{AgentID: uuidToString(agentID)}}},
		Step:  ChainStep{StepRef: StepRef{BlockID: "build", Number: 1}},
	})
	if err == nil || !strings.Contains(err.Error(), "review") {
		t.Fatalf("error = %v, want missing review skill", err)
	}
	if len(q.issueTasks) != 0 {
		t.Fatal("no task should be enqueued unless the runner has every skill")
	}
}

func TestBuildBlockPromptListsEveryConfiguredSkill(t *testing.T) {
	prompt, err := buildBlockPrompt(Block{ID: "build", Type: BlockSession, Skills: []string{"plan", "review"}, Goal: "Ship it"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "- plan") || !strings.Contains(prompt, "- review") {
		t.Fatalf("prompt omitted skills: %s", prompt)
	}
}

func TestRenderApprovalPromptUsesPreviousOutcomeAndRemovesUnknownPlaceholders(t *testing.T) {
	steps := []ChainStep{{
		StepRef: StepRef{BlockID: "build", Number: 1},
		Status:  StepCompleted,
		Outcome: json.RawMessage(`{"output":"Feature shipped","evidence":"Tests green"}`),
	}}
	prompt := renderApprovalPrompt("Delivered: {{previous.output}}\nEvidence: {{previous.evidence}}\n{{missing.value}}", steps)
	if prompt != "Delivered: Feature shipped\nEvidence: Tests green" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestTaskDispatcher_DispatchBlockAppliesEveryAllBusyPolicy(t *testing.T) {
	agentID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	issueID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	workspaceID := mustScanUUID(t, "44444444-4444-4444-4444-444444444444")
	memberID := mustScanUUID(t, "55555555-5555-5555-5555-555555555555")

	for _, policy := range []BusyPolicy{BusyWait, BusyPause, BusyWakeup, BusyPingMember} {
		t.Run(string(policy), func(t *testing.T) {
			wakeups := &fakeBusyWakeupScheduler{}
			q := &fakeDispatchQueries{
				agents:       map[[16]byte]db.Agent{agentID.Bytes: {ID: agentID, WorkspaceID: workspaceID, RuntimeID: runtimeID, MaxConcurrentTasks: 1}},
				runtimes:     map[[16]byte]db.AgentRuntime{runtimeID.Bytes: {ID: runtimeID, Status: "online"}},
				runningTasks: map[[16]byte]int64{agentID.Bytes: 1},
				issue:        db.Issue{ID: issueID, WorkspaceID: workspaceID, Title: "Build", CreatorType: "member", CreatorID: memberID},
			}
			result, err := NewTaskDispatcher(q).WithBusyWakeupScheduler(wakeups).DispatchBlock(context.Background(), BlockDispatch{
				Run:   ChainRun{IssueID: issueID},
				Phase: Phase{ID: "build", Limits: PhaseLimits{MaxWaitSeconds: 60}},
				Block: Block{ID: "build", Type: BlockSession, Skill: "build-skill", OnAllBusy: policy, Agents: []AgentRef{{AgentID: uuidToString(agentID)}}},
				Step:  ChainStep{StepRef: StepRef{BlockID: "build", Number: 1}},
			})
			if err != nil {
				t.Fatalf("dispatch block: %v", err)
			}
			wantStatus := StepWaiting
			if policy == BusyWait || policy == BusyWakeup {
				wantStatus = StepPending
			}
			if result.Status != wantStatus {
				t.Fatalf("status = %q, want %q", result.Status, wantStatus)
			}
			var outcome map[string]any
			if err := json.Unmarshal(result.Outcome, &outcome); err != nil {
				t.Fatalf("decode outcome: %v", err)
			}
			if outcome["policy"] != string(policy) {
				t.Fatalf("policy outcome = %v, want %q", outcome["policy"], policy)
			}
			if policy == BusyPingMember && len(q.inboxItems) != 1 {
				t.Fatalf("ping_member notifications = %d, want 1", len(q.inboxItems))
			}
			wantWakeups := 0
			if policy == BusyWakeup {
				wantWakeups = 1
			}
			if len(wakeups.calls) != wantWakeups {
				t.Fatalf("scheduled wakeups = %d, want %d", len(wakeups.calls), wantWakeups)
			}
		})
	}
}

func TestTaskDispatcher_DispatchBlockFailsExpiredBusyWait(t *testing.T) {
	agentID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	issueID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	workspaceID := mustScanUUID(t, "44444444-4444-4444-4444-444444444444")
	q := &fakeDispatchQueries{
		agents:       map[[16]byte]db.Agent{agentID.Bytes: {ID: agentID, WorkspaceID: workspaceID, RuntimeID: runtimeID, MaxConcurrentTasks: 1}},
		runtimes:     map[[16]byte]db.AgentRuntime{runtimeID.Bytes: {ID: runtimeID, Status: "online"}},
		runningTasks: map[[16]byte]int64{agentID.Bytes: 1},
	}

	result, err := NewTaskDispatcher(q).DispatchBlock(context.Background(), BlockDispatch{
		Run:   ChainRun{IssueID: issueID},
		Phase: Phase{ID: "build", Limits: PhaseLimits{MaxWaitSeconds: 60}},
		Block: Block{ID: "build", Type: BlockSession, OnAllBusy: BusyWait, Agents: []AgentRef{{AgentID: uuidToString(agentID)}}},
		Step:  ChainStep{StepRef: StepRef{BlockID: "build", Number: 1}, CreatedAt: time.Now().Add(-61 * time.Second)},
	})
	if err != nil {
		t.Fatalf("dispatch block: %v", err)
	}
	if result.Status != StepFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(string(result.Outcome), "max_wait_seconds exceeded") {
		t.Fatalf("outcome = %s, want wait-limit reason", result.Outcome)
	}
}

func TestTaskDispatcher_DispatchBlockNotifiesMemberApprover(t *testing.T) {
	issueID := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	workspaceID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	memberID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	q := &fakeDispatchQueries{issue: db.Issue{ID: issueID, WorkspaceID: workspaceID, Title: "Approve"}}

	result, err := NewTaskDispatcher(q).DispatchBlock(context.Background(), BlockDispatch{
		Run:   ChainRun{IssueID: issueID},
		Phase: Phase{ID: "approval"},
		Block: Block{ID: "signoff", Type: BlockHuman, Prompt: "Approve delivery", ApproverType: AssigneeMember, ApproverID: uuidToString(memberID)},
		Step:  ChainStep{StepRef: StepRef{BlockID: "signoff", Number: 1}},
	})
	if err != nil {
		t.Fatalf("dispatch block: %v", err)
	}
	if result.Status != StepWaiting {
		t.Fatalf("status = %q, want waiting", result.Status)
	}
	if len(q.inboxItems) != 1 || q.inboxItems[0].RecipientID != memberID {
		t.Fatalf("member notification = %+v", q.inboxItems)
	}
	if q.inboxItems[0].Route != "inbox" {
		t.Fatalf("member notification route = %q, want inbox", q.inboxItems[0].Route)
	}
}
