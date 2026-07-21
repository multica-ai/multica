package loops

// This file is the live bridge from an Issue workflow recipe to the block
// chain runtime. Version 1 recipes are migrated in migration 9147; the server
// intentionally accepts only ChainVersion after that cutover.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type WorkspaceSkillLister interface {
	ListSkillSummariesByWorkspace(context.Context, pgtype.UUID) ([]db.ListSkillSummariesByWorkspaceRow, error)
}

type MonitorEvalBindingLister interface {
	ListMonitorAdvisoryEvalKeys(context.Context, pgtype.UUID) ([]string, error)
}

type IssueLoopBridge struct {
	pool         *pgxpool.Pool
	queries      *cerebrodb.Queries
	issues       *db.Queries
	columns      *workflows.IssueLoopColumnStore
	skills       WorkspaceSkillLister
	monitorEvals MonitorEvalBindingLister
	driver       *ChainDriver
	dispatcher   *TaskDispatcher
}

func NewIssueLoopBridge(pool *pgxpool.Pool, queries *cerebrodb.Queries, issues *db.Queries, columns *workflows.IssueLoopColumnStore) *IssueLoopBridge {
	dispatcher := NewTaskDispatcher(issues)
	return &IssueLoopBridge{pool: pool, queries: queries, issues: issues, columns: columns, dispatcher: dispatcher, driver: NewChainDriver(NewStore(pool), dispatcher)}
}

func (b *IssueLoopBridge) WithEvalBlockRunner(r EvalBlockRunner) *IssueLoopBridge {
	b.dispatcher.WithEvalBlockRunner(r)
	return b
}

func (b *IssueLoopBridge) WithBusyWakeupScheduler(s BusyWakeupScheduler) *IssueLoopBridge {
	b.dispatcher.WithBusyWakeupScheduler(s)
	return b
}

func (b *IssueLoopBridge) WithSkillLister(l WorkspaceSkillLister) *IssueLoopBridge {
	b.skills = l
	return b
}

func (b *IssueLoopBridge) WithMonitorEvalBindingLister(l MonitorEvalBindingLister) *IssueLoopBridge {
	b.monitorEvals = l
	return b
}

func (b *IssueLoopBridge) withMonitorEvalBindings(ctx context.Context, workflowID pgtype.UUID, chain *Chain) (*Chain, error) {
	if b.monitorEvals == nil {
		return chain, nil
	}
	keys, err := b.monitorEvals.ListMonitorAdvisoryEvalKeys(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list monitor eval bindings: %w", err)
	}
	seen := make(map[string]bool)
	phaseIDs := make(map[string]bool)
	for _, phase := range chain.Phases {
		phaseIDs[phase.ID] = true
		for _, block := range phase.Blocks {
			if block.Type == BlockEval && block.EvalPhase == "monitor" {
				seen[block.EvalKey] = true
			}
		}
	}
	blocks := make([]Block, 0, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		blocks = append(blocks, Block{ID: "monitor-" + key, Type: BlockEval, EvalKey: key, EvalPhase: "monitor"})
	}
	if len(blocks) == 0 {
		return chain, nil
	}
	copyChain := *chain
	copyChain.Phases = append([]Phase(nil), chain.Phases...)
	phaseID := "eval-monitor"
	for n := 2; phaseIDs[phaseID]; n++ {
		phaseID = fmt.Sprintf("eval-monitor-%d", n)
	}
	status := chain.DoneStatus
	if status == "" {
		status = "done"
	}
	copyChain.Phases = append(copyChain.Phases, Phase{ID: phaseID, Name: "Monitor evals", Status: status, Blocks: blocks, Limits: PhaseLimits{MaxSteps: len(blocks), MaxRounds: 1, NoProgressStalls: 1}})
	return &copyChain, nil
}

func decodeChain(raw []byte) (*Chain, error) {
	var chain Chain
	if err := json.Unmarshal(raw, &chain); err != nil {
		return nil, fmt.Errorf("loop_spec: invalid JSON: %w", err)
	}
	if chain.Version == 1 {
		return nil, fmt.Errorf("loop_spec: version 1 is retired; run the Chain v2 migration")
	}
	if err := chain.Validate(); err != nil {
		return nil, fmt.Errorf("loop_spec: %w", err)
	}
	return &chain, nil
}

func (b *IssueLoopBridge) validateSkillsExist(ctx context.Context, workspaceID pgtype.UUID, chain *Chain) error {
	if b.skills == nil {
		return nil
	}
	rows, err := b.skills.ListSkillSummariesByWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("loop_spec: verify skills: %w", err)
	}
	have := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		have[row.Name] = struct{}{}
	}
	for pi, phase := range chain.Phases {
		for bi, block := range phase.Blocks {
			if block.Skill == "" {
				continue
			}
			if _, ok := have[block.Skill]; !ok {
				return fmt.Errorf("loop_spec: phases[%d].blocks[%d].skill %q does not exist in this workspace", pi, bi, block.Skill)
			}
		}
	}
	return nil
}

func (b *IssueLoopBridge) SyncIssueLoop(ctx context.Context, workspaceID, workflowID, projectID, createdByID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	chain, err := decodeChain(loopSpecJSON)
	if err != nil {
		return err
	}
	if err := b.validateSkillsExist(ctx, workspaceID, chain); err != nil {
		return err
	}
	return b.columns.DeleteGeneratedChildren(ctx, workflowID)
}

func (b *IssueLoopBridge) ActivateOnIssue(ctx context.Context, workspaceID, workflowID, projectID, createdByID, issueID pgtype.UUID, createdByType string, loopSpecJSON []byte) error {
	if !issueID.Valid {
		return fmt.Errorf("activate on issue: issueID is required")
	}
	chain, err := decodeChain(loopSpecJSON)
	if err != nil {
		return err
	}
	if err := b.validateSkillsExist(ctx, workspaceID, chain); err != nil {
		return err
	}
	if err := b.columns.DeleteAllGeneratedChildrenForIssue(ctx, issueID); err != nil {
		return err
	}
	if err := b.createActivationMarker(ctx, workspaceID, workflowID, projectID, createdByID, issueID, createdByType); err != nil {
		return err
	}
	return b.advanceChain(ctx, workflowID, issueID, chain)
}

// ResolveHumanBlock records the authenticated decision at the durable Chain
// v2 step and immediately re-enters the same chain.
func (b *IssueLoopBridge) ResolveHumanBlock(ctx context.Context, workflowID, issueID pgtype.UUID, blockID string, approved bool, note, approverID string) error {
	fields, err := b.columns.Get(ctx, workflowID)
	if err != nil {
		return err
	}
	chain, err := decodeChain(fields.LoopSpec)
	if err != nil {
		return err
	}
	store := NewStore(b.pool)
	for _, phase := range chain.Phases {
		steps, err := store.ListSteps(ctx, PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: phase.ID})
		if err != nil {
			return err
		}
		for _, step := range steps {
			if step.BlockID != blockID || (step.Status != StepWaiting && step.Status != StepRunning) {
				continue
			}
			var block *Block
			for i := range phase.Blocks {
				if phase.Blocks[i].ID == blockID {
					block = &phase.Blocks[i]
					break
				}
			}
			if block == nil || block.Type != BlockHuman || block.ApproverType != AssigneeMember || block.ApproverID != approverID {
				return errors.New("not authorized to approve this human block")
			}
			status := StepCompleted
			if !approved {
				status = StepFailed
			}
			outcome, _ := json.Marshal(map[string]any{"approved": approved, "note": note})
			if err := store.RecordStepOutcome(ctx, step.StepRef, status, outcome); err != nil {
				return err
			}
			return b.advanceChain(ctx, workflowID, issueID, chain)
		}
	}
	return fmt.Errorf("human block %q has no pending step", blockID)
}

// ReadIssueLoopState returns only the current member's actionable Human
// blocks. It reads the Chain v2 tables directly rather than the retired
// generated delivery-gate state.
func (b *IssueLoopBridge) ReadIssueLoopState(ctx context.Context, workflowID, issueID pgtype.UUID, memberID string) (workflows.IssueLoopState, error) {
	fields, err := b.columns.Get(ctx, workflowID)
	if err != nil {
		return workflows.IssueLoopState{}, err
	}
	chain, err := decodeChain(fields.LoopSpec)
	if err != nil {
		return workflows.IssueLoopState{}, err
	}

	state := workflows.IssueLoopState{PendingHumanChecks: []workflows.PendingHumanCheck{}}
	store := NewStore(b.pool)
	for _, phase := range chain.Phases {
		key := PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: phase.ID}
		phaseState, err := store.LoadPhaseRun(ctx, key)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return workflows.IssueLoopState{}, err
		}
		if err == nil && phaseState.Status == PhaseFailed {
			state.Stopped = true
			state.StopReason = phaseState.FailureReason
		}
		steps, err := store.ListSteps(ctx, key)
		if err != nil {
			return workflows.IssueLoopState{}, err
		}
		for _, step := range steps {
			if step.Status != StepWaiting {
				continue
			}
			for _, block := range phase.Blocks {
				if block.ID == step.BlockID && block.Type == BlockHuman && block.ApproverType == AssigneeMember && block.ApproverID == memberID {
					state.PendingHumanChecks = append(state.PendingHumanChecks, workflows.PendingHumanCheck{CheckID: block.ID, Prompt: block.Prompt, AssigneeType: block.ApproverType, AssigneeID: block.ApproverID})
					break
				}
			}
		}
	}
	return state, nil
}

// ResumeActiveIssueLoops re-enters every binding retained by the cutover
// migration. Durable step claims make repeated startup calls idempotent.
func (b *IssueLoopBridge) ResumeActiveIssueLoops(ctx context.Context) error {
	if b == nil || b.pool == nil {
		return nil
	}
	rows, err := b.pool.Query(ctx, `SELECT recipe.id, marker.generated_for_issue_id, recipe.loop_spec FROM cerebro_workflow marker JOIN cerebro_workflow recipe ON recipe.id=marker.generated_from_workflow_id WHERE marker.name='loop:chain-activation' AND marker.generated_for_issue_id IS NOT NULL AND recipe.workflow_type='issue_loop'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var workflowID, issueID pgtype.UUID
		var raw []byte
		if err := rows.Scan(&workflowID, &issueID, &raw); err != nil {
			return err
		}
		chain, err := decodeChain(raw)
		if err != nil {
			return err
		}
		if err := b.advanceChain(ctx, workflowID, issueID, chain); err != nil {
			return err
		}
	}
	return rows.Err()
}

// The marker preserves the existing recipe<->issue read model without keeping
// generated legacy rules. It is disabled and can never dispatch.
func (b *IssueLoopBridge) createActivationMarker(ctx context.Context, workspaceID, workflowID, projectID, createdByID, issueID pgtype.UUID, createdByType string) error {
	row, err := b.queries.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID: workspaceID, ProjectID: projectID, Name: "loop:chain-activation",
		Enabled: false, TriggerType: workflows.TriggerStatusChanged, TriggerConfig: json.RawMessage(`{"to_status":"__chain_driver__"}`),
		Conditions: json.RawMessage(`[]`), ActionType: workflows.ActionSetStatus,
		ActionConfig: json.RawMessage(`{"status":"done"}`), EditorMode: workflows.EditorModeForm,
		EditorLayout: json.RawMessage(`null`), CreatedByID: createdByID, CreatedByType: createdByType,
	})
	if err != nil {
		return fmt.Errorf("create chain activation marker: %w", err)
	}
	if err := b.columns.SetGeneratedFromForIssue(ctx, row.ID, workflowID, issueID); err != nil {
		return fmt.Errorf("tag chain activation marker: %w", err)
	}
	return nil
}

func (b *IssueLoopBridge) advanceChain(ctx context.Context, workflowID, issueID pgtype.UUID, chain *Chain) error {
	issue, err := b.issues.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("load chain issue: %w", err)
	}
	chain, err = b.withMonitorEvalBindings(ctx, workflowID, chain)
	if err != nil {
		return err
	}
	run := ChainRun{IssueID: issueID, WorkflowID: workflowID, IssueStatus: issue.Status, AgentID: util.UUIDToString(issue.AssigneeID)}
	for {
		decision, err := b.driver.Advance(ctx, chain, run)
		if err != nil {
			return err
		}
		switch decision.Kind {
		case ChainWait:
			return nil
		case ChainSetStatus, ChainDone:
			if decision.Status == "" || decision.Status == run.IssueStatus {
				return nil
			}
			if _, err := b.issues.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issueID, WorkspaceID: issue.WorkspaceID, Status: decision.Status}); err != nil {
				return fmt.Errorf("set chain issue status: %w", err)
			}
			run.IssueStatus = decision.Status
			if decision.Kind == ChainDone {
				return nil
			}
		case ChainFailed:
			_, err := b.issues.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issueID, WorkspaceID: issue.WorkspaceID, Status: "blocked"})
			return err
		default:
			return fmt.Errorf("unknown chain decision %q", decision.Kind)
		}
	}
}
