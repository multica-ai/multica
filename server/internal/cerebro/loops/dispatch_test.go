package loops

// Pure unit tests for the check dispatcher — no DB. Proves DispatchCheck turns
// one enqueued check into a single quick_create task on the worker agent's
// runtime, carrying the argv and the loop_check bookkeeping the ingress matches
// on, and that a check with nowhere to run fails loudly instead of vanishing.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeDispatchQueries stubs the upstream issue queries the TaskDispatcher needs.
type fakeDispatchQueries struct {
	agent             db.Agent
	getErr            error
	issue             db.Issue
	getIssueErr       error
	created           []db.CreateQuickCreateTaskParams
	comments          []db.CreateCommentParams
	issueTasks        []db.CreateAgentTaskParams
	modelOverrides    []db.SetAgentTaskModelOverrideParams
	thinkingOverrides []db.SetAgentTaskThinkingOverrideParams
}

func (f *fakeDispatchQueries) GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error) {
	return f.agent, f.getErr
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

// TestTaskDispatcher_EnqueuesCheckTask proves DispatchCheck resolves the agent's
// runtime and enqueues one quick_create task whose context carries the argv and
// the loop_check bookkeeping.
func TestTaskDispatcher_EnqueuesCheckTask(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	wsID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	q := &fakeDispatchQueries{agent: db.Agent{
		ID:          mustScanUUID(t, agentID),
		WorkspaceID: wsID,
		RuntimeID:   runtimeID,
	}}
	d := NewTaskDispatcher(q)

	err := d.DispatchCheck(context.Background(), CheckDispatch{
		AgentID: agentID,
		IssueID: "44444444-4444-4444-4444-444444444444",
		Gate:    "gate-1",
		Round:   1,
		Argv:    []string{"go", "test", "./..."},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(q.created) != 1 {
		t.Fatalf("want 1 task enqueued, got %d", len(q.created))
	}

	task := q.created[0]
	if task.RuntimeID != runtimeID {
		t.Fatal("task not routed to the agent's runtime")
	}
	var ctxMap map[string]any
	if err := json.Unmarshal(task.Context, &ctxMap); err != nil {
		t.Fatalf("context not valid JSON: %v", err)
	}
	if ctxMap["type"] != "quick_create" {
		t.Fatalf("task type wrong: %v", ctxMap["type"])
	}
	lc, ok := ctxMap["loop_check"].(map[string]any)
	if !ok {
		t.Fatalf("loop_check bookkeeping missing: %v", ctxMap)
	}
	if lc["gate"] != "gate-1" {
		t.Fatalf("loop_check gate wrong: %v", lc["gate"])
	}
	if lc["issue_id"] != "44444444-4444-4444-4444-444444444444" {
		t.Fatalf("loop_check issue_id wrong: %v", lc["issue_id"])
	}
	argv, ok := lc["argv"].([]any)
	if !ok || len(argv) != 3 || argv[0] != "go" {
		t.Fatalf("loop_check argv wrong: %v", lc["argv"])
	}
}

// TestTaskDispatcher_RejectsAgentWithoutRuntime proves a check with nowhere to
// run errors instead of being silently dropped (which would stall the gate).
func TestTaskDispatcher_RejectsAgentWithoutRuntime(t *testing.T) {
	q := &fakeDispatchQueries{agent: db.Agent{}} // zero RuntimeID => not Valid
	d := NewTaskDispatcher(q)
	err := d.DispatchCheck(context.Background(), CheckDispatch{
		AgentID: "11111111-1111-1111-1111-111111111111",
		Argv:    []string{"go", "test"},
	})
	if err == nil {
		t.Fatal("expected error for agent without runtime")
	}
	if len(q.created) != 0 {
		t.Fatal("no task should be enqueued when the agent has no runtime")
	}
}

// TestTaskDispatcher_RejectsEmptyArgv proves an empty check is refused before
// any agent lookup — there is nothing to run.
func TestTaskDispatcher_RejectsEmptyArgv(t *testing.T) {
	q := &fakeDispatchQueries{}
	d := NewTaskDispatcher(q)
	if err := d.DispatchCheck(context.Background(), CheckDispatch{AgentID: "x", Argv: nil}); err == nil {
		t.Fatal("expected error for empty argv")
	}
}

// TestTaskDispatcher_EnqueuesJudgeTask proves DispatchJudge resolves the judge
// agent's runtime and enqueues one quick_create task whose context carries the
// rubric and the loop_judge bookkeeping the ingress matches on.
func TestTaskDispatcher_EnqueuesJudgeTask(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	wsID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	q := &fakeDispatchQueries{agent: db.Agent{
		ID:          mustScanUUID(t, agentID),
		WorkspaceID: wsID,
		RuntimeID:   runtimeID,
	}}
	d := NewTaskDispatcher(q)

	err := d.DispatchJudge(context.Background(), JudgeDispatch{
		AgentID:   agentID,
		IssueID:   "44444444-4444-4444-4444-444444444444",
		Gate:      "gate-1",
		Round:     1,
		CheckID:   "ux-quality",
		Rubric:    "the UI must not regress",
		SkillName: "judge-skill",
	})
	if err != nil {
		t.Fatalf("dispatch judge: %v", err)
	}
	if len(q.created) != 1 {
		t.Fatalf("want 1 task enqueued, got %d", len(q.created))
	}

	task := q.created[0]
	if task.RuntimeID != runtimeID {
		t.Fatal("task not routed to the judge agent's runtime")
	}
	var ctxMap map[string]any
	if err := json.Unmarshal(task.Context, &ctxMap); err != nil {
		t.Fatalf("context not valid JSON: %v", err)
	}
	lj, ok := ctxMap["loop_judge"].(map[string]any)
	if !ok {
		t.Fatalf("loop_judge bookkeeping missing: %v", ctxMap)
	}
	if lj["check_id"] != "ux-quality" {
		t.Fatalf("loop_judge check_id wrong: %v", lj["check_id"])
	}
	if lj["gate"] != "gate-1" {
		t.Fatalf("loop_judge gate wrong: %v", lj["gate"])
	}
	prompt, _ := ctxMap["prompt"].(string)
	if prompt == "" {
		t.Fatal("judge prompt should not be empty")
	}
}

func TestTaskDispatcher_AppliesJudgeTaskOverrides(t *testing.T) {
	agentID := "11111111-1111-1111-1111-111111111111"
	runtimeID := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	wsID := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	q := &fakeDispatchQueries{agent: db.Agent{
		ID:          mustScanUUID(t, agentID),
		WorkspaceID: wsID,
		RuntimeID:   runtimeID,
	}}
	d := NewTaskDispatcher(q)

	err := d.DispatchJudge(context.Background(), JudgeDispatch{
		AgentID:       agentID,
		IssueID:       "44444444-4444-4444-4444-444444444444",
		Gate:          "gate-1",
		Round:         1,
		CheckID:       "ux-quality",
		Rubric:        "the UI must not regress",
		SkillName:     "judge-skill",
		Model:         "judge-model",
		ThinkingLevel: "high",
	})
	if err != nil {
		t.Fatalf("dispatch judge: %v", err)
	}
	if len(q.modelOverrides) != 1 || q.modelOverrides[0].ModelOverride != "judge-model" {
		t.Fatalf("model override not applied: %+v", q.modelOverrides)
	}
	if len(q.thinkingOverrides) != 1 || q.thinkingOverrides[0].ThinkingOverride != "high" {
		t.Fatalf("thinking override not applied: %+v", q.thinkingOverrides)
	}
}

// TestTaskDispatcher_RejectsEmptyRubric proves a judge check with nowhere to
// get its scoring criteria from is refused before any agent lookup.
func TestTaskDispatcher_RejectsEmptyRubric(t *testing.T) {
	q := &fakeDispatchQueries{}
	d := NewTaskDispatcher(q)
	if err := d.DispatchJudge(context.Background(), JudgeDispatch{AgentID: "x", CheckID: "a"}); err == nil {
		t.Fatal("expected error for empty rubric")
	}
}

// TestTaskDispatcher_RejectsJudgeAgentWithoutRuntime proves a judge check with
// nowhere to run errors instead of being silently dropped.
func TestTaskDispatcher_RejectsJudgeAgentWithoutRuntime(t *testing.T) {
	q := &fakeDispatchQueries{agent: db.Agent{}} // zero RuntimeID => not Valid
	d := NewTaskDispatcher(q)
	err := d.DispatchJudge(context.Background(), JudgeDispatch{
		AgentID: "11111111-1111-1111-1111-111111111111",
		CheckID: "ux-quality",
		Rubric:  "the UI must not regress",
	})
	if err == nil {
		t.Fatal("expected error for judge agent without runtime")
	}
	if len(q.created) != 0 {
		t.Fatal("no task should be enqueued when the judge agent has no runtime")
	}
}
