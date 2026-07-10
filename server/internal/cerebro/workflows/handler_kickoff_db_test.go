package workflows

// FIR-2283 followup point 1 — the regression guard for the bug Jesper hit:
// creating an issue directly on a workflow attached the loop's rules but never
// STARTED them, so the agent never entered plan mode. This test drives the real
// Handler.ActivateForIssue path and proves that activating an issue_loop recipe
// with a planning phase dispatches the plan run WITH the plan-mode prompt (the
// "you are planning, do not write code" preamble), not a bare build run.
//
// It uses the real column store + engine (Dispatch/Execute + actionRunSkill)
// against the test DB, a fake IssueLoopCompiler that materializes the same
// loop:planning-dispatch rule loops.Compile emits (the compile→materialize path
// itself is covered in the loops package), and the in-package fakeIssueActions
// to capture the enqueued task without needing a live runtime row.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// planKickoffFakeCompiler stands in for loops.IssueLoopBridge: on ActivateOnIssue
// it materializes exactly the loop:planning-dispatch rule Compile would emit for
// a planning recipe (status_changed→planningStatus, run_skill with PlanMode, and
// the issue-scope condition), so the Handler's kickoff has a real rule to fire.
type planKickoffFakeCompiler struct {
	cq         *cerebrodb.Queries
	planSkill  string
	planAgent  string
	planStatus string
}

func (c *planKickoffFakeCompiler) SyncIssueLoop(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	return nil
}

func (c *planKickoffFakeCompiler) ActivateOnIssue(ctx context.Context, workspaceID, workflowID, projectID, createdByID, issueID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	_, err := c.cq.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   workspaceID,
		ProjectID:     projectID,
		Name:          "loop:planning-dispatch",
		Enabled:       true,
		TriggerType:   TriggerStatusChanged,
		TriggerConfig: mustJSON(TriggerConfigStatusChanged{ToStatus: c.planStatus}),
		Conditions:    mustJSON([]Condition{{Field: "issue.id", Op: "eq", Value: uuidString(issueID)}}),
		ActionType:    ActionRunSkill,
		ActionConfig:  mustJSON(ActionConfigRunSkill{SkillName: c.planSkill, AgentID: c.planAgent, PlanMode: true}),
		CreatedByID:   createdByID,
		CreatedByType: createdByType,
		EditorMode:    "form",
		EditorLayout:  []byte(`null`),
	})
	return err
}

func TestActivateForIssue_StartsPlanPhaseWithPlanModePrompt(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Plan me", "todo", 1, pgtype.UUID{})

	cq := cerebrodb.New(pool)
	cols := NewIssueLoopColumnStore(pool)

	// The planning recipe: a plan phase, a plan skill, a build skill, and a
	// pinned build agent that the plan-dispatch rule runs as.
	planAgent := "aaaaaaaa-1111-2222-3333-444444444444"
	loopSpec := []byte(`{
		"version": 1,
		"planning": true,
		"planning_status": "todo",
		"build_status": "in_progress",
		"plan_skill": "plan",
		"build_skill": "build",
		"build_agent_id": "` + planAgent + `",
		"verification": [{"id": "t", "type": "programmatic", "check": ["true"]}],
		"caps": {"max_iterations": 5, "max_revisions": 3, "no_progress_stalls": 2}
	}`)

	// The recipe row. Its own trigger never matches the kickoff event (sentinel
	// to-status), so only the generated plan-dispatch rule fires.
	recipe, err := cq.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   f.workspaceID,
		Name:          "Issue loop recipe",
		Enabled:       true,
		TriggerType:   TriggerStatusChanged,
		TriggerConfig: mustJSON(TriggerConfigStatusChanged{ToStatus: "__recipe_never__"}),
		Conditions:    mustJSON([]Condition{}),
		ActionType:    ActionRunSkill,
		ActionConfig:  mustJSON(ActionConfigRunSkill{SkillName: "unused", AgentID: planAgent}),
		CreatedByID:   f.userID,
		CreatedByType: "member",
		EditorMode:    "form",
		EditorLayout:  []byte(`null`),
	})
	if err != nil {
		t.Fatalf("create recipe: %v", err)
	}
	if err := cols.Set(ctx, recipe.ID, WorkflowTypeIssueLoop, loopSpec); err != nil {
		t.Fatalf("set loop columns: %v", err)
	}

	// The agent that run_skill validates + dispatches to: has the plan skill
	// attached and a runtime, so the plan phase can actually start.
	fake := &fakeIssueActions{
		Skills:      []db.ListSkillSummariesByWorkspaceRow{{Name: "plan"}, {Name: "build"}},
		AgentSkills: []db.Skill{{Name: "plan"}, {Name: "build"}},
		Agent:       db.Agent{RuntimeID: mustUUID("bbbbbbbb-1111-2222-3333-444444444444")},
	}
	// GetIssue must return the real target issue so the issue-bound dispatch
	// anchors comment + task on it.
	fake.ParentIssue = db.Issue{ID: issueID, WorkspaceID: f.workspaceID, Title: "Plan me", Status: "todo"}
	svc := &Service{queries: cq, issues: fake, enabled: true, sessionStamper: NewSessionPhaseStamper(pool)}
	h := NewHandler(cq).
		WithService(svc).
		WithIssueLoopColumns(cols).
		WithIssueLoopCompiler(&planKickoffFakeCompiler{cq: cq, planSkill: "plan", planAgent: planAgent, planStatus: "todo"})

	if err := h.ActivateForIssue(ctx, f.workspaceID, recipe.ID, issueID, f.userID, "member"); err != nil {
		t.Fatalf("activate for issue: %v", err)
	}

	// The plan phase must have been dispatched as an ISSUE-BOUND task (Tine
	// live-test fix) — visible on the issue, never a detached quick_create.
	if fake.TaskQueued.Context != nil {
		t.Fatal("plan phase was dispatched as a detached quick_create task; want an issue-bound task")
	}
	if fake.IssueTaskQueuedCount == 0 {
		t.Fatal("activation did not dispatch the plan phase: no issue-bound task enqueued")
	}
	if got := uuidString(fake.IssueTaskQueued.IssueID); got != uuidString(issueID) {
		t.Fatalf("plan task bound to issue %s, want %s", got, uuidString(issueID))
	}
	if !fake.IssueTaskQueued.TriggerCommentID.Valid {
		t.Fatal("plan task has no trigger comment: the kickoff comment must open the session thread")
	}
	if !fake.IssueTaskQueued.ForceFreshSession.Bool {
		t.Fatal("plan task must force a fresh session")
	}
	var taskCtx map[string]any
	if err := json.Unmarshal(fake.IssueTaskQueued.Context, &taskCtx); err != nil {
		t.Fatalf("task context is not JSON: %v", err)
	}
	if taskCtx["plan_mode"] != true {
		t.Fatalf("dispatched task is not in plan mode: plan_mode = %v", taskCtx["plan_mode"])
	}
	if taskCtx["loop_phase"] != "plan" {
		t.Fatalf("dispatched task loop_phase = %v, want \"plan\"", taskCtx["loop_phase"])
	}
	// The plan-mode instruction now lives in the kickoff comment the agent is
	// triggered on — that is what makes the plan phase visible on the issue.
	prompt := fake.CreatedComment.Content
	if !strings.Contains(prompt, "PLAN MODE") {
		t.Fatalf("kickoff comment is not the plan-mode prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "must NOT write or edit") {
		t.Fatalf("plan-mode kickoff missing the no-code instruction: %q", prompt)
	}
	if fake.CreatedComment.AuthorType != "agent" {
		t.Fatalf("kickoff comment author_type = %q, want \"agent\"", fake.CreatedComment.AuthorType)
	}
	// And the agent it dispatched to is the recipe's build/plan agent.
	if got := uuidString(fake.IssueTaskQueued.AgentID); got != planAgent {
		t.Fatalf("plan dispatched to agent %s, want %s", got, planAgent)
	}
}

// TestActivateForIssue_NonPlanningStartsBuildPhase proves the kickoff also
// starts a recipe that has no planning phase: it fires the build-status entry
// rule instead, so a non-planning workflow issue is not left inert either.
func TestActivateForIssue_NonPlanningStartsBuildPhase(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Build me", "in_progress", 2, pgtype.UUID{})

	cq := cerebrodb.New(pool)
	cols := NewIssueLoopColumnStore(pool)
	buildAgent := "cccccccc-1111-2222-3333-444444444444"
	loopSpec := []byte(`{
		"version": 1,
		"planning": false,
		"build_status": "in_progress",
		"build_skill": "build",
		"build_agent_id": "` + buildAgent + `",
		"verification": [{"id": "t", "type": "programmatic", "check": ["true"]}],
		"caps": {"max_iterations": 5, "max_revisions": 3, "no_progress_stalls": 2}
	}`)
	recipe, err := cq.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   f.workspaceID,
		Name:          "Issue loop recipe (no plan)",
		Enabled:       true,
		TriggerType:   TriggerStatusChanged,
		TriggerConfig: mustJSON(TriggerConfigStatusChanged{ToStatus: "__recipe_never__"}),
		Conditions:    mustJSON([]Condition{}),
		ActionType:    ActionRunSkill,
		ActionConfig:  mustJSON(ActionConfigRunSkill{SkillName: "unused", AgentID: buildAgent}),
		CreatedByID:   f.userID,
		CreatedByType: "member",
		EditorMode:    "form",
		EditorLayout:  []byte(`null`),
	})
	if err != nil {
		t.Fatalf("create recipe: %v", err)
	}
	if err := cols.Set(ctx, recipe.ID, WorkflowTypeIssueLoop, loopSpec); err != nil {
		t.Fatalf("set loop columns: %v", err)
	}

	fake := &fakeIssueActions{
		Skills:      []db.ListSkillSummariesByWorkspaceRow{{Name: "build"}},
		AgentSkills: []db.Skill{{Name: "build"}},
		Agent:       db.Agent{RuntimeID: mustUUID("dddddddd-1111-2222-3333-444444444444")},
	}
	svc := &Service{queries: cq, issues: fake, enabled: true}

	// A non-planning compiler that materializes the loop:dispatch-build rule
	// (status_changed→build_status, run_skill WITHOUT plan mode).
	compiler := &buildKickoffFakeCompiler{cq: cq, buildSkill: "build", buildAgent: buildAgent, buildStatus: "in_progress"}
	h := NewHandler(cq).WithService(svc).WithIssueLoopColumns(cols).WithIssueLoopCompiler(compiler)

	if err := h.ActivateForIssue(ctx, f.workspaceID, recipe.ID, issueID, f.userID, "member"); err != nil {
		t.Fatalf("activate for issue: %v", err)
	}
	if fake.TaskQueued.Context == nil {
		t.Fatal("non-planning activation did not dispatch the build phase")
	}
	var taskCtx map[string]any
	_ = json.Unmarshal(fake.TaskQueued.Context, &taskCtx)
	if _, isPlan := taskCtx["plan_mode"]; isPlan {
		t.Fatalf("non-planning build run must NOT be plan mode: %v", taskCtx)
	}
}

type buildKickoffFakeCompiler struct {
	cq          *cerebrodb.Queries
	buildSkill  string
	buildAgent  string
	buildStatus string
}

func (c *buildKickoffFakeCompiler) SyncIssueLoop(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	return nil
}

func (c *buildKickoffFakeCompiler) ActivateOnIssue(ctx context.Context, workspaceID, workflowID, projectID, createdByID, issueID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	_, err := c.cq.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   workspaceID,
		ProjectID:     projectID,
		Name:          "loop:dispatch-build",
		Enabled:       true,
		TriggerType:   TriggerStatusChanged,
		TriggerConfig: mustJSON(TriggerConfigStatusChanged{ToStatus: c.buildStatus}),
		Conditions:    mustJSON([]Condition{{Field: "issue.id", Op: "eq", Value: uuidString(issueID)}}),
		ActionType:    ActionRunSkill,
		ActionConfig:  mustJSON(ActionConfigRunSkill{SkillName: c.buildSkill, AgentID: c.buildAgent}),
		CreatedByID:   createdByID,
		CreatedByType: createdByType,
		EditorMode:    "form",
		EditorLayout:  []byte(`null`),
	})
	return err
}
