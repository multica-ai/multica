package loops

// materialize_issue_loop.go implements workflows.IssueLoopCompiler: the
// bridge from a saved Issue workflow recipe (workflow_type == "issue_loop",
// its loop_spec column) to the engine actually running it. This is the
// "Compile() is never called from the live app" gap FIR-2283's plan
// identified — SyncIssueLoop is what closes it.
//
// On every Create/Update of an issue_loop workflow, workflows.Handler calls
// SyncIssueLoop, which:
//  1. parses loop_spec into a Spec (plus the issue-specific CompileParams
//     bindings the editor collects alongside it — worker agent/skill,
//     planning agent/skill, status names),
//  2. calls Compile to get the dispatch/gate/escalate rules,
//  3. deletes any rules a previous sync generated for this recipe, and
//  4. persists the fresh set as real cerebro_workflow rows (workflow_type
//     stays "standard" on those — they are literally standard rules, just
//     machine-authored), each tagged back to the recipe via
//     generated_from_workflow_id so List can hide them and a future sync can
//     find and replace them.
//
// Reuses the same "loops writes directly through cerebrodb" pattern
// materialize.go already established for the planning-dispatch rule — no new
// dependency shape, just a second write path through the same seam.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
)

// IssueLoopBridge implements workflows.IssueLoopCompiler over the generated
// cerebro DB queries and the issue-loop column store.
type IssueLoopBridge struct {
	queries *cerebrodb.Queries
	columns *workflows.IssueLoopColumnStore
}

// NewIssueLoopBridge builds an IssueLoopBridge over the given queries and
// issue-loop column store.
func NewIssueLoopBridge(queries *cerebrodb.Queries, columns *workflows.IssueLoopColumnStore) *IssueLoopBridge {
	return &IssueLoopBridge{queries: queries, columns: columns}
}

// issueLoopSpecWire is the JSON wire shape of an issue_loop recipe's
// loop_spec column: the loops.Spec fields the Issue workflow editor sets
// (embedded, so its keys flatten into the same JSON object), plus the
// issue-specific bindings CompileParams needs that a hand-authored loop.yaml
// would get from elsewhere (worker agent/skill, planning agent/skill, status
// names, judge fallback).
type issueLoopSpecWire struct {
	Spec

	// Build bindings — who builds, and what skill it runs each round.
	BuildAgentID string `json:"build_agent_id"`
	BuildSkill   string `json:"build_skill"`

	// Planning bindings — only meaningful when Spec.Planning is true.
	// PlanAgentID is accepted for a future per-phase agent but not yet wired:
	// Compile always dispatches the planning phase to the worker agent (see
	// PlanningDispatchRule(params.AgentID, ...)), so today planning and build
	// share the same agent regardless of PlanAgentID.
	PlanAgentID string `json:"plan_agent_id,omitempty"`
	PlanSkill   string `json:"plan_skill,omitempty"`

	// Status names. Empty falls back to CompileParams.withDefaults()
	// (todo / in_progress / in_review / done).
	PlanningStatus string `json:"planning_status,omitempty"`
	BuildStatus    string `json:"build_status,omitempty"`
	ReviewStatus   string `json:"review_status,omitempty"`
	DoneStatus     string `json:"done_status,omitempty"`

	// Spec-wide judge fallback bindings, used only by a judge check whose own
	// Verification doesn't carry AssigneeID/Skill.
	JudgeAgentID string `json:"judge_agent_id,omitempty"`
	JudgeSkill   string `json:"judge_skill,omitempty"`
}

// SyncIssueLoop parses workflowID's loop_spec, compiles it PROJECT-WIDE, and
// replaces its previously-generated project-wide child rules with the fresh
// set. Called after every create/update of the recipe itself.
func (b *IssueLoopBridge) SyncIssueLoop(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	return b.syncIssueLoop(ctx, workspaceID, workflowID, projectID, createdByID, createdByType, loopSpecJSON, pgtype.UUID{})
}

// ActivateOnIssue compiles workflowID's saved recipe scoped to issueID alone
// (FIR-2283 v2 point 8), independent from the project-wide compile and from
// any other issue's activation of the same recipe.
func (b *IssueLoopBridge) ActivateOnIssue(ctx context.Context, workspaceID, workflowID, projectID, createdByID, issueID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	if !issueID.Valid {
		return fmt.Errorf("activate on issue: issueID is required")
	}
	return b.syncIssueLoop(ctx, workspaceID, workflowID, projectID, createdByID, createdByType, loopSpecJSON, issueID)
}

// syncIssueLoop is the shared compile-and-materialize path for both
// SyncIssueLoop (issueID zero value — project-wide) and ActivateOnIssue
// (issueID set). Delete-then-recreate always replaces the previous
// generated set for the SAME scope rather than diffing, so a re-sync never
// leaves a stale rule behind; DeleteGeneratedChildren[ForIssue] is scoped so
// this never touches a different scope's rows.
func (b *IssueLoopBridge) syncIssueLoop(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, loopSpecJSON []byte, issueID pgtype.UUID) error {
	var wire issueLoopSpecWire
	if err := json.Unmarshal(loopSpecJSON, &wire); err != nil {
		return fmt.Errorf("loop_spec: invalid JSON: %w", err)
	}
	// build_agent_id is intentionally optional (FIR-2283 v2): a recipe with no
	// pinned build agent falls back to the triggering issue's own assignee at
	// dispatch time — see CompileParams.AgentID.
	if wire.BuildSkill == "" {
		return fmt.Errorf("loop_spec: build_skill is required")
	}

	params := CompileParams{
		AgentID:        wire.BuildAgentID,
		BuildSkill:     wire.BuildSkill,
		BuildStatus:    wire.BuildStatus,
		ReviewStatus:   wire.ReviewStatus,
		DoneStatus:     wire.DoneStatus,
		PlanSkill:      wire.PlanSkill,
		PlanningStatus: wire.PlanningStatus,
		JudgeAgentID:   wire.JudgeAgentID,
		JudgeSkill:     wire.JudgeSkill,
	}
	if issueID.Valid {
		params.IssueID = util.UUIDToString(issueID)
	}

	rules, err := Compile(&wire.Spec, params)
	if err != nil {
		return fmt.Errorf("compile loop spec: %w", err)
	}

	if issueID.Valid {
		if err := b.columns.DeleteAllGeneratedChildrenForIssue(ctx, issueID); err != nil {
			return err
		}
	} else {
		if err := b.columns.DeleteGeneratedChildren(ctx, workflowID); err != nil {
			return err
		}
	}
	for _, rule := range rules {
		if err := b.materializeRule(ctx, workspaceID, workflowID, projectID, createdByID, createdByType, rule, issueID); err != nil {
			return err
		}
	}
	return nil
}

func (b *IssueLoopBridge) materializeRule(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, rule Rule, issueID pgtype.UUID) error {
	triggerConfigJSON, err := json.Marshal(rule.TriggerConfig)
	if err != nil {
		return fmt.Errorf("marshal %s trigger config: %w", rule.Name, err)
	}
	conditionsJSON := []byte("[]")
	if len(rule.Conditions) > 0 {
		conditionsJSON, err = json.Marshal(rule.Conditions)
		if err != nil {
			return fmt.Errorf("marshal %s conditions: %w", rule.Name, err)
		}
	}
	actionConfigJSON, err := json.Marshal(rule.ActionConfig)
	if err != nil {
		return fmt.Errorf("marshal %s action config: %w", rule.Name, err)
	}

	row, err := b.queries.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   workspaceID,
		ProjectID:     projectID,
		Name:          rule.Name,
		Enabled:       true,
		TriggerType:   rule.TriggerType,
		TriggerConfig: triggerConfigJSON,
		Conditions:    conditionsJSON,
		ActionType:    rule.ActionType,
		ActionConfig:  actionConfigJSON,
		EditorMode:    workflows.EditorModeForm,
		EditorLayout:  []byte("null"),
		CreatedByID:   createdByID,
		CreatedByType: createdByType,
	})
	if err != nil {
		return fmt.Errorf("create %s workflow: %w", rule.Name, err)
	}
	if issueID.Valid {
		return b.columns.SetGeneratedFromForIssue(ctx, row.ID, workflowID, issueID)
	}
	return b.columns.SetGeneratedFrom(ctx, row.ID, workflowID)
}
