package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkflowService manages workflow DAG validation, run lifecycle,
// node-run state machine transitions, and the Worker-Critic loop.
type WorkflowService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
	TaskSvc   *TaskService

	// OnNodeStatusChanged fires after TransitionNodeRun succeeds.
	OnNodeStatusChanged func(ctx context.Context, nodeRun db.MulticaWorkflowNodeRun)

	// OnRunTerminal fires when a workflow run reaches a terminal status
	// (completed, failed, or cancelled).
	OnRunTerminal func(ctx context.Context, run db.MulticaWorkflowRun, status string)
}

func NewWorkflowService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *WorkflowService {
	return &WorkflowService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

// ── State machine constants ──────────────────────────────────────────────────

const (
	NodeRunStatusPending         = "pending"
	NodeRunStatusFormatChecking  = "format_checking"
	NodeRunStatusFormatOk        = "format_ok"
	NodeRunStatusFormatFailed    = "format_failed"
	NodeRunStatusWorkerAssigned  = "worker_assigned"
	NodeRunStatusWorking         = "working"
	NodeRunStatusAwaitingCritic  = "awaiting_critic"
	NodeRunStatusCriticReviewing = "critic_reviewing"
	NodeRunStatusCriticApproved  = "critic_approved"
	NodeRunStatusCriticRework    = "critic_rework"
	NodeRunStatusCompleted       = "completed"
	NodeRunStatusFailed          = "failed"
	NodeRunStatusBlocked         = "blocked"
	NodeRunStatusSkipped         = "skipped"
	NodeRunStatusCancelled       = "cancelled"

	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"
)

// validTransitions defines the allowed status transitions for a node run.
var validTransitions = map[string][]string{
	NodeRunStatusPending:         {NodeRunStatusFormatChecking, NodeRunStatusSkipped, NodeRunStatusCancelled},
	NodeRunStatusFormatChecking:  {NodeRunStatusFormatOk, NodeRunStatusFormatFailed, NodeRunStatusCancelled},
	NodeRunStatusFormatOk:        {NodeRunStatusWorkerAssigned, NodeRunStatusWorking, NodeRunStatusCancelled, NodeRunStatusSkipped},
	NodeRunStatusFormatFailed:    {},
	NodeRunStatusWorkerAssigned:  {NodeRunStatusWorking, NodeRunStatusCancelled, NodeRunStatusSkipped},
	NodeRunStatusWorking:         {NodeRunStatusAwaitingCritic, NodeRunStatusFailed, NodeRunStatusCancelled},
	NodeRunStatusAwaitingCritic:  {NodeRunStatusCriticReviewing, NodeRunStatusCancelled, NodeRunStatusSkipped},
	NodeRunStatusCriticReviewing: {NodeRunStatusCriticApproved, NodeRunStatusCriticRework, NodeRunStatusCancelled},
	NodeRunStatusCriticApproved:  {NodeRunStatusCompleted},
	NodeRunStatusCriticRework:    {NodeRunStatusFormatOk, NodeRunStatusBlocked},
	NodeRunStatusCompleted:       {},
	NodeRunStatusFailed:          {},
	NodeRunStatusBlocked:         {NodeRunStatusFormatOk, NodeRunStatusSkipped},
	NodeRunStatusSkipped:         {},
	NodeRunStatusCancelled:       {},
}

// isValidTransition checks whether a status transition is allowed.
func isValidTransition(from, to string) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}

// isTerminal reports whether the status represents a terminal (non-active) state.
func isTerminalNodeRunStatus(s string) bool {
	switch s {
	case NodeRunStatusCompleted, NodeRunStatusFailed, NodeRunStatusSkipped,
		NodeRunStatusFormatFailed, NodeRunStatusCancelled:
		return true
	}
	return false
}

// ── DAG validation ───────────────────────────────────────────────────────────

// ValidateDAG checks the workflow for cycles via DFS topological sort.
// O(V+E) complexity.
func (s *WorkflowService) ValidateDAG(ctx context.Context, workflowID pgtype.UUID) error {
	nodes, err := s.Queries.ListWorkflowNodes(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}
	edges, err := s.Queries.ListWorkflowEdges(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("list edges: %w", err)
	}

	nodeIDs := make(map[string]bool)
	for _, n := range nodes {
		nodeIDs[util.UUIDToString(n.ID)] = true
	}

	// Build adjacency list.
	adj := make(map[string][]string)
	inDegree := make(map[string]int)
	for _, n := range nodes {
		nid := util.UUIDToString(n.ID)
		adj[nid] = nil
		inDegree[nid] = 0
	}
	for _, e := range edges {
		src := util.UUIDToString(e.SourceNodeID)
		dst := util.UUIDToString(e.TargetNodeID)
		adj[src] = append(adj[src], dst)
		inDegree[dst]++
	}

	// DFS-based cycle detection with three colors.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	var dfs func(string) error
	dfs = func(u string) error {
		color[u] = gray
		for _, v := range adj[u] {
			switch color[v] {
			case gray:
				return fmt.Errorf("cycle detected: node %s reaches %s", u, v)
			case white:
				if err := dfs(v); err != nil {
					return err
				}
			}
		}
		color[u] = black
		return nil
	}

	for _, n := range nodes {
		nid := util.UUIDToString(n.ID)
		if color[nid] == white {
			if err := dfs(nid); err != nil {
				return err
			}
		}
	}

	return nil
}

// ── Run lifecycle ────────────────────────────────────────────────────────────

// StartRun creates a workflow_run and node_runs for every node, then
// kicks off root nodes (nodes with no incoming edges).
func (s *WorkflowService) StartRun(ctx context.Context, workflow db.MulticaWorkflow, triggeredByType, triggeredByID string, input json.RawMessage, runtimeID pgtype.UUID) (*db.MulticaWorkflowRun, error) {
	if workflow.Status != "active" {
		return nil, fmt.Errorf("workflow is not active (status=%s)", workflow.Status)
	}

	triggeredByUUID, err := util.ParseUUID(triggeredByID)
	if err != nil && triggeredByID != "" {
		triggeredByUUID = pgtype.UUID{}
	}

	var run db.MulticaWorkflowRun
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		r, err := qtx.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
			WorkflowID:      workflow.ID,
			WorkspaceID:     workflow.WorkspaceID,
			WorkflowTitle:   workflow.Title,
			Status:          "running",
			TriggeredByType: triggeredByType,
			TriggeredByID:   triggeredByUUID,
			Input:           input,
			RuntimeID:       runtimeID,
		})
		if err != nil {
			return fmt.Errorf("create workflow run: %w", err)
		}
		run = r

		nodes, err := qtx.ListWorkflowNodes(ctx, workflow.ID)
		if err != nil {
			return fmt.Errorf("list nodes: %w", err)
		}

		edges, err := qtx.ListWorkflowEdges(ctx, workflow.ID)
		if err != nil {
			return fmt.Errorf("list edges: %w", err)
		}

		// Build incoming edge count to identify roots.
		hasIncoming := make(map[string]bool)
		for _, e := range edges {
			hasIncoming[util.UUIDToString(e.TargetNodeID)] = true
		}

		for _, node := range nodes {
			status := NodeRunStatusPending
			nid := util.UUIDToString(node.ID)
			if !hasIncoming[nid] {
				status = NodeRunStatusFormatChecking
			}

			_, err := qtx.CreateWorkflowNodeRun(ctx, db.CreateWorkflowNodeRunParams{
				WorkflowRunID:  run.ID,
				WorkflowNodeID: node.ID,
				NodeTitle:      node.Title,
				Status:         status,
				RetryCount:     0,
				WorkerType:     node.WorkerType,
				WorkerID:       node.WorkerID,
				CriticType:     node.CriticType,
				CriticID:       node.CriticID,
			})
			if err != nil {
				return fmt.Errorf("create node run for %s: %w", node.Title, err)
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &run, nil
}

// DispatchRootNodeRuns kicks off root node runs after the run is created.
// format_checking -> format_ok -> dispatchWorker.
// Must be called after sub-issues exist so DispatchAgentTask can link issue_id.
func (s *WorkflowService) DispatchRootNodeRuns(ctx context.Context, runID pgtype.UUID) {
	nodeRuns, _ := s.Queries.ListWorkflowNodeRunsByRun(ctx, runID)
	for _, nr := range nodeRuns {
		if nr.Status == NodeRunStatusFormatChecking {
			if _, err := s.TransitionNodeRun(ctx, nr, NodeRunStatusFormatOk); err != nil {
				slog.Warn("StartRun: transition to format_ok failed", "node_run_id", util.UUIDToString(nr.ID), "error", err)
			}
		}
	}
	nodeRuns, _ = s.Queries.ListWorkflowNodeRunsByRun(ctx, runID)
	for _, nr := range nodeRuns {
		if nr.Status == NodeRunStatusFormatOk {
			if err := s.dispatchWorker(ctx, nr); err != nil {
				slog.Warn("StartRun: dispatch worker failed", "node_run_id", util.UUIDToString(nr.ID), "error", err)
			}
		}
	}
}

// StartRunForIssue creates a workflow run from an issue assignment and returns
// all created node runs so the caller can create corresponding sub-issues.
func (s *WorkflowService) StartRunForIssue(
	ctx context.Context,
	workflow db.MulticaWorkflow,
	issue db.MulticaIssue,
	triggeredByType string,
	triggeredByID string,
	runtimeID pgtype.UUID,
) (*db.MulticaWorkflowRun, []db.MulticaWorkflowNodeRun, error) {
	input, err := json.Marshal(map[string]any{
		"title":       issue.Title,
		"description": textToString(issue.Description),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal issue input: %w", err)
	}

	run, err := s.StartRun(ctx, workflow, triggeredByType, triggeredByID, input, runtimeID)
	if err != nil {
		return nil, nil, err
	}

	nodeRuns, err := s.Queries.ListWorkflowNodeRunsByRun(ctx, run.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list node runs: %w", err)
	}

	return run, nodeRuns, nil
}

func textToString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func textToPgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

// CancelRun cancels all active node_runs and marks the run as cancelled.
func (s *WorkflowService) CancelRun(ctx context.Context, runID pgtype.UUID) error {
	return s.runInTx(ctx, func(qtx *db.Queries) error {
		run, err := qtx.GetWorkflowRun(ctx, runID)
		if err != nil {
			return fmt.Errorf("get workflow run: %w", err)
		}

		nodeRuns, err := qtx.ListWorkflowNodeRunsByRun(ctx, runID)
		if err != nil {
			return fmt.Errorf("list node runs: %w", err)
		}
		for _, nr := range nodeRuns {
			if !isTerminalNodeRunStatus(nr.Status) {
				if _, err := qtx.UpdateWorkflowNodeRunStatus(ctx, db.UpdateWorkflowNodeRunStatusParams{
					ID:     nr.ID,
					Status: NodeRunStatusCancelled,
				}); err != nil {
					return fmt.Errorf("cancel node run: %w", err)
				}
			}
			// Cancel the sub-issue created for this node run.
			subIssue, err := qtx.GetIssueByOrigin(ctx, db.GetIssueByOriginParams{
				WorkspaceID: run.WorkspaceID,
				OriginType:  pgtype.Text{String: "workflow", Valid: true},
				OriginID:    nr.ID,
			})
			if err != nil {
				continue // sub-issue may not exist yet; not an error
			}
			if subIssue.Status != "cancelled" && subIssue.Status != "done" {
				if _, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
					ID:          subIssue.ID,
					Status:      "cancelled",
					WorkspaceID: run.WorkspaceID,
				}); err != nil {
					return fmt.Errorf("cancel sub-issue %s: %w", util.UUIDToString(subIssue.ID), err)
				}
				// Cancel any active agent tasks for this sub-issue.
				if _, err := qtx.CancelAgentTasksByIssue(ctx, subIssue.ID); err != nil {
					slog.Warn("failed to cancel agent tasks for sub-issue", "issue_id", util.UUIDToString(subIssue.ID), "error", err)
				}
			}
		}
		_, err = qtx.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
			ID:     runID,
			Status: RunStatusCancelled,
		})
		return err
	})
}

// ── State machine ────────────────────────────────────────────────────────────

// TransitionNodeRun validates the transition and updates the node run status.
func (s *WorkflowService) TransitionNodeRun(ctx context.Context, nodeRun db.MulticaWorkflowNodeRun, newStatus string) (*db.MulticaWorkflowNodeRun, error) {
	if !isValidTransition(nodeRun.Status, newStatus) {
		return nil, fmt.Errorf("invalid transition: %s → %s", nodeRun.Status, newStatus)
	}

	updated, err := s.Queries.UpdateWorkflowNodeRunStatus(ctx, db.UpdateWorkflowNodeRunStatusParams{
		ID:     nodeRun.ID,
		Status: newStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("update node run status: %w", err)
	}

	if s.OnNodeStatusChanged != nil {
		s.OnNodeStatusChanged(ctx, updated)
	}

	return &updated, nil
}

// ── Downstream propagation ───────────────────────────────────────────────────

// OnNodeRunCompleted checks downstream nodes after a node run reaches a terminal
// state. If all upstreams of a downstream node are complete, it advances that
// node to format_checking. If no active node runs remain, the workflow run is
// marked completed or failed.
func (s *WorkflowService) OnNodeRunCompleted(ctx context.Context, nodeRunID pgtype.UUID) error {
	nodeRun, err := s.Queries.GetWorkflowNodeRun(ctx, nodeRunID)
	if err != nil {
		return fmt.Errorf("get node run: %w", err)
	}

	run, err := s.Queries.GetWorkflowRun(ctx, nodeRun.WorkflowRunID)
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}

	// Find downstream edges from the completed node.
	edges, err := s.Queries.ListWorkflowEdgesBySource(ctx, nodeRun.WorkflowNodeID)
	if err != nil {
		return fmt.Errorf("list outgoing edges: %w", err)
	}

	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		for _, edge := range edges {
			// Check whether ALL upstream node runs of the target are terminal-complete.
			upstreamEdges, err := qtx.ListWorkflowEdgesByTarget(ctx, edge.TargetNodeID)
			if err != nil {
				return fmt.Errorf("list incoming edges: %w", err)
			}

			allUpstreamDone := true
			for _, ue := range upstreamEdges {
				// Find the node run for this upstream node in the current run.
				upstreamNr, err := qtx.ListWorkflowNodeRunsByRunAndNode(ctx, db.ListWorkflowNodeRunsByRunAndNodeParams{
					WorkflowRunID:  run.ID,
					WorkflowNodeID: ue.SourceNodeID,
				})
				if err != nil {
					allUpstreamDone = false
					break
				}
				if !isTerminalNodeRunStatus(upstreamNr.Status) {
					allUpstreamDone = false
					break
				}
			}

			if allUpstreamDone {
				// Find the downstream node run and advance it.
				dnr, err := qtx.ListWorkflowNodeRunsByRunAndNode(ctx, db.ListWorkflowNodeRunsByRunAndNodeParams{
					WorkflowRunID:  run.ID,
					WorkflowNodeID: edge.TargetNodeID,
				})
				if err != nil {
					continue
				}
				if dnr.Status == NodeRunStatusPending {
					if _, err := qtx.UpdateWorkflowNodeRunStatus(ctx, db.UpdateWorkflowNodeRunStatusParams{
						ID:     dnr.ID,
						Status: NodeRunStatusFormatChecking,
					}); err != nil {
						return fmt.Errorf("advance downstream node: %w", err)
					}
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// After advancing downstream nodes, kick off format checkers for newly
	// advanced nodes outside the tx.
	allNodeRuns, _ := s.Queries.ListWorkflowNodeRunsByRun(ctx, run.ID)
	for _, nr := range allNodeRuns {
		if nr.Status == NodeRunStatusFormatChecking {
			s.executeFormatChecker(ctx, qtxForRun(s.Queries), nr)
		}
	}

	// Check run completion.
	s.checkRunCompletion(ctx, run.ID)

	return nil
}

// checkRunCompletion evaluates whether all node runs are terminal and marks
// the run as completed or failed accordingly.
func (s *WorkflowService) checkRunCompletion(ctx context.Context, runID pgtype.UUID) {
	nodeRuns, err := s.Queries.ListWorkflowNodeRunsByRun(ctx, runID)
	if err != nil {
		return
	}

	hasActive := false
	hasFailed := false
	for _, nr := range nodeRuns {
		if !isTerminalNodeRunStatus(nr.Status) {
			hasActive = true
			break
		}
		if nr.Status == NodeRunStatusFailed || nr.Status == NodeRunStatusFormatFailed {
			hasFailed = true
		}
	}
	if hasActive {
		return
	}

	status := RunStatusCompleted
	if hasFailed {
		status = RunStatusFailed
	}
	s.Queries.UpdateWorkflowRunStatus(ctx, db.UpdateWorkflowRunStatusParams{
		ID:     runID,
		Status: status,
	})

	run, err := s.Queries.GetWorkflowRun(ctx, runID)
	if err != nil {
		return
	}
	if status == RunStatusFailed {
		s.publishWorkflowEvent(EventWorkflowRunFailed, util.UUIDToString(run.WorkspaceID), map[string]any{
			"run_id":      util.UUIDToString(runID),
			"workflow_id": util.UUIDToString(run.WorkflowID),
		})
	} else {
		s.publishWorkflowEvent(EventWorkflowRunCompleted, util.UUIDToString(run.WorkspaceID), map[string]any{
			"run_id":      util.UUIDToString(runID),
			"workflow_id": util.UUIDToString(run.WorkflowID),
		})
	}

	if s.OnRunTerminal != nil {
		s.OnRunTerminal(ctx, run, status)
	}
}

// ── Worker-Critic loop ───────────────────────────────────────────────────────

// SubmitWorkerOutput handles human/agent submitting the worker phase output.
func (s *WorkflowService) SubmitWorkerOutput(ctx context.Context, nodeRunID pgtype.UUID, output json.RawMessage) error {
	var nodeRun db.MulticaWorkflowNodeRun
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		nr, err := qtx.GetWorkflowNodeRun(ctx, nodeRunID)
		if err != nil {
			return fmt.Errorf("get node run: %w", err)
		}
		if nr.Status != NodeRunStatusWorking && nr.Status != NodeRunStatusWorkerAssigned {
			return fmt.Errorf("node run is not in worker phase (status=%s)", nr.Status)
		}

		updated, err := qtx.SetWorkflowNodeRunWorkerOutput(ctx, db.SetWorkflowNodeRunWorkerOutputParams{
			ID:           nr.ID,
			WorkerOutput: output,
			Status:       NodeRunStatusAwaitingCritic,
		})
		if err != nil {
			return fmt.Errorf("set worker output: %w", err)
		}
		nodeRun = updated
		return nil
	}); err != nil {
		return err
	}

	// Dispatch critic outside the tx.
	return s.dispatchCritic(ctx, nodeRun)
}

// ReviewNodeRun handles the Critic's approval or rework decision.
func (s *WorkflowService) ReviewNodeRun(ctx context.Context, nodeRunID pgtype.UUID, approved bool, comment string, criticOutput json.RawMessage) error {
	var nodeRun db.MulticaWorkflowNodeRun
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		nr, err := qtx.GetWorkflowNodeRun(ctx, nodeRunID)
		if err != nil {
			return fmt.Errorf("get node run: %w", err)
		}
		if nr.Status != NodeRunStatusCriticReviewing && nr.Status != NodeRunStatusAwaitingCritic {
			return fmt.Errorf("node run is not awaiting critic review (status=%s)", nr.Status)
		}

		if approved {
			// critic_approved → completed
			updated, err := qtx.UpdateWorkflowNodeRunStatus(ctx, db.UpdateWorkflowNodeRunStatusParams{
				ID:     nr.ID,
				Status: NodeRunStatusCriticApproved,
			})
			if err != nil {
				return fmt.Errorf("approve node run: %w", err)
			}
			// Store critic output.
			updated, err = qtx.SetWorkflowNodeRunCriticOutput(ctx, db.SetWorkflowNodeRunCriticOutputParams{
				ID:            nr.ID,
				CriticOutput:  criticOutput,
				CriticComment: pgtype.Text{String: comment, Valid: comment != ""},
				Status:        NodeRunStatusCompleted,
			})
			if err != nil {
				return fmt.Errorf("complete node run: %w", err)
			}
			nodeRun = updated
		} else {
			// Rework: increment retry count and go through critic_rework first.
			newRetry := nr.RetryCount + 1
			run, err := qtx.GetWorkflowRun(ctx, nr.WorkflowRunID)
			if err != nil {
				return fmt.Errorf("get run: %w", err)
			}
			workflow, err := qtx.GetWorkflow(ctx, run.WorkflowID)
			if err != nil {
				return fmt.Errorf("get workflow: %w", err)
			}

			// Always go through critic_rework first (state machine contract).
			updated, err := qtx.SetWorkflowNodeRunCriticOutput(ctx, db.SetWorkflowNodeRunCriticOutputParams{
				ID:             nr.ID,
				CriticOutput:   nil,
				CriticComment:  pgtype.Text{String: comment, Valid: comment != ""},
				Status:         NodeRunStatusCriticRework,
				RetryCount:     pgtype.Int4{Int32: newRetry, Valid: true},
			})
			if err != nil {
				return fmt.Errorf("rework node run: %w", err)
			}

			if newRetry > workflow.MaxRetries {
				// Max retries exhausted: transition from critic_rework to blocked.
				updated, err = qtx.UpdateWorkflowNodeRunStatus(ctx, db.UpdateWorkflowNodeRunStatusParams{
					ID:     updated.ID,
					Status: NodeRunStatusBlocked,
				})
				if err != nil {
					return fmt.Errorf("block node run: %w", err)
				}
				nodeRun = updated
				return nil // Blocked is terminal; handled after tx.
			}

			nodeRun = updated

			// Re-dispatch to format_ok for the next retry.
			u, err := qtx.UpdateWorkflowNodeRunStatus(ctx, db.UpdateWorkflowNodeRunStatusParams{
				ID:     updated.ID,
				Status: NodeRunStatusFormatOk,
			})
			if err != nil {
				return fmt.Errorf("re-dispatch after rework: %w", err)
			}
			nodeRun = u
		}
		return nil
	}); err != nil {
		return err
	}

	if nodeRun.Status == NodeRunStatusFormatOk {
		// Re-dispatch the worker after rework.
		return s.dispatchWorker(ctx, nodeRun)
	}

	if nodeRun.Status == NodeRunStatusBlocked {
		if s.OnNodeStatusChanged != nil {
			s.OnNodeStatusChanged(ctx, nodeRun)
		}
		return s.OnNodeRunCompleted(ctx, nodeRunID)
	}

	if nodeRun.Status == NodeRunStatusCompleted {
		if s.OnNodeStatusChanged != nil {
			s.OnNodeStatusChanged(ctx, nodeRun)
		}
		return s.OnNodeRunCompleted(ctx, nodeRunID)
	}

	return nil
}

// dispatchWorker advances a node run from format_ok to the worker phase.
func (s *WorkflowService) dispatchWorker(ctx context.Context, nodeRun db.MulticaWorkflowNodeRun) error {
	node, err := s.Queries.GetWorkflowNode(ctx, nodeRun.WorkflowNodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	switch node.WorkerType {
	case "human":
		_, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusWorkerAssigned)
		return err
	case "agent", "squad":
		agentID := node.WorkerID
		if node.WorkerType == "squad" && node.WorkerID.Valid {
			squad, err := s.Queries.GetSquad(ctx, node.WorkerID)
			if err == nil {
				agentID = squad.LeaderID
			}
		}
		if !agentID.Valid {
			// No specific agent assigned yet — mark as assigned so it can be claimed.
			_, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusWorkerAssigned)
			return err
		}
		// Create the agent task.
		task, err := s.DispatchAgentTask(ctx, nodeRun, "worker")
		if err != nil {
			return fmt.Errorf("dispatch agent task: %w", err)
		}
		// Link the task to the node run.
		if _, err := s.Queries.LinkNodeRunWorkerTask(ctx, db.LinkNodeRunWorkerTaskParams{
			ID:                nodeRun.ID,
			WorkerAgentTaskID: task.ID,
		}); err != nil {
			return fmt.Errorf("link worker task: %w", err)
		}
		_, err = s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusWorking)
		return err
	default:
		return fmt.Errorf("unknown worker type: %s", node.WorkerType)
	}
}

// dispatchCritic advances a node run from awaiting_critic to the critic phase.
func (s *WorkflowService) dispatchCritic(ctx context.Context, nodeRun db.MulticaWorkflowNodeRun) error {
	node, err := s.Queries.GetWorkflowNode(ctx, nodeRun.WorkflowNodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	switch node.CriticType {
	case "human":
		_, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusCriticReviewing)
		return err
	case "agent", "squad":
		agentID := node.CriticID
		if node.CriticType == "squad" && node.CriticID.Valid {
			squad, err := s.Queries.GetSquad(ctx, node.CriticID)
			if err == nil {
				agentID = squad.LeaderID
			}
		}
		if !agentID.Valid {
			return fmt.Errorf("no agent resolved for critic")
		}
		task, err := s.DispatchAgentTask(ctx, nodeRun, "critic")
		if err != nil {
			return fmt.Errorf("dispatch critic task: %w", err)
		}
		if _, err := s.Queries.LinkNodeRunCriticTask(ctx, db.LinkNodeRunCriticTaskParams{
			ID:                nodeRun.ID,
			CriticAgentTaskID: task.ID,
		}); err != nil {
			return fmt.Errorf("link critic task: %w", err)
		}
		_, err = s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusCriticReviewing)
		return err
	case "api":
		// For API critics, we transition to critic_reviewing and let the
		// API call happen asynchronously (handled by the caller or a sweeper).
		_, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusCriticReviewing)
		return err
	default:
		return fmt.Errorf("unknown critic type: %s", node.CriticType)
	}
}

// ── Agent task dispatch ──────────────────────────────────────────────────────

// DispatchAgentTask creates an agent_task_queue row for a workflow node run
// and links it via the workflow_node_run_id column.
func (s *WorkflowService) DispatchAgentTask(ctx context.Context, nodeRun db.MulticaWorkflowNodeRun, phase string) (*db.MulticaAgentTaskQueue, error) {
	node, err := s.Queries.GetWorkflowNode(ctx, nodeRun.WorkflowNodeID)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}

	run, err := s.Queries.GetWorkflowRun(ctx, nodeRun.WorkflowRunID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	workflow, err := s.Queries.GetWorkflow(ctx, run.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}

	var agentID pgtype.UUID
	switch phase {
	case "worker":
		agentID = node.WorkerID
		if node.WorkerType == "squad" && node.WorkerID.Valid {
			if squad, err := s.Queries.GetSquad(ctx, node.WorkerID); err == nil {
				agentID = squad.LeaderID
			}
		}
	case "critic":
		agentID = node.CriticID
		if node.CriticType == "squad" && node.CriticID.Valid {
			if squad, err := s.Queries.GetSquad(ctx, node.CriticID); err == nil {
				agentID = squad.LeaderID
			}
		}
	default:
		return nil, fmt.Errorf("unknown phase: %s", phase)
	}

	if !agentID.Valid {
		return nil, fmt.Errorf("no agent configured for %s phase", phase)
	}

	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	// Resolve the runtime: use agent's bound runtime, or for built-in agents
	// use the runtime selected when the workflow run was started.
	var taskRuntimeID pgtype.UUID
	if agent.RuntimeID.Valid {
		taskRuntimeID = agent.RuntimeID
	} else if agent.IsBuiltin && run.RuntimeID.Valid {
		taskRuntimeID = run.RuntimeID
	} else {
		return nil, fmt.Errorf("agent has no runtime")
	}

	// Build context with workflow info so the daemon prompt builder can
	// include role + node + workflow context.
	contextPayload := map[string]any{
		"type":              "workflow",
		"workflow_id":       util.UUIDToString(workflow.ID),
		"workflow_title":    workflow.Title,
		"workflow_run_id":   util.UUIDToString(run.ID),
		"workflow_node_id":  util.UUIDToString(node.ID),
		"node_title":        node.Title,
		"node_run_id":       util.UUIDToString(nodeRun.ID),
		"phase":             phase,
	}
	contextJSON, err := json.Marshal(contextPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal context: %w", err)
	}

	// Look up the sub-issue linked to this node run so the daemon processes it
	// as a normal issue task (with issue_id) while still driving the workflow.
	var issueID pgtype.UUID
	subIssue, err := s.Queries.GetIssueByOrigin(ctx, db.GetIssueByOriginParams{
		WorkspaceID: workflow.WorkspaceID,
		OriginType:  pgtype.Text{String: "workflow", Valid: true},
		OriginID:    nodeRun.ID,
	})
	if err == nil {
		issueID = subIssue.ID
	}

	// Create workflow-bound agent task directly.
	task, err := s.Queries.CreateWorkflowAgentTask(ctx, db.CreateWorkflowAgentTaskParams{
		AgentID:            agentID,
		RuntimeID:          taskRuntimeID,
		Priority:           2, // medium
		Context:            contextJSON,
		WorkflowNodeRunID:  nodeRun.ID,
		IssueID:            issueID,
	})
	if err != nil {
		return nil, fmt.Errorf("create workflow agent task: %w", err)
	}

	s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	return &task, nil
}

// ── Format checker ───────────────────────────────────────────────────────────

// executeFormatChecker validates the node run's input against the node's JSON
// Schema (if configured), then transitions accordingly.
func (s *WorkflowService) executeFormatChecker(ctx context.Context, qtx *db.Queries, nodeRun db.MulticaWorkflowNodeRun) error {
	node, err := qtx.GetWorkflowNode(ctx, nodeRun.WorkflowNodeID)
	if err != nil {
		return err
	}

	if len(node.FormatSchema) == 0 {
		// No format schema → format_ok directly.
		if _, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusFormatOk); err != nil {
			return err
		}
		updated, err := s.Queries.GetWorkflowNodeRun(ctx, nodeRun.ID)
		if err != nil {
			return err
		}
		return s.dispatchWorker(ctx, updated)
	}

	// Run JSON Schema validation.
	run, err := qtx.GetWorkflowRun(ctx, nodeRun.WorkflowRunID)
	if err != nil {
		return err
	}

	valid, valErr := validateJSONSchema(node.FormatSchema, run.Input)
	if !valid {
		if _, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusFormatFailed); err != nil {
			return err
		}
		if valErr != nil {
			s.Queries.SetWorkflowNodeRunCriticOutput(ctx, db.SetWorkflowNodeRunCriticOutputParams{
				ID:            nodeRun.ID,
				CriticComment: pgtype.Text{String: valErr.Error(), Valid: true},
				Status:        NodeRunStatusFormatFailed,
			})
		}
		return nil
	}

	if _, err := s.TransitionNodeRun(ctx, nodeRun, NodeRunStatusFormatOk); err != nil {
		return err
	}
	updated, err := s.Queries.GetWorkflowNodeRun(ctx, nodeRun.ID)
	if err != nil {
		return err
	}
	return s.dispatchWorker(ctx, updated)
}

// validateJSONSchema validates input JSON against a JSON Schema.
// Uses a simple structural check; for full JSON Schema support, integrate
// gojsonschema as noted in the architecture plan.
func validateJSONSchema(schema, input []byte) (bool, error) {
	if len(schema) == 0 {
		return true, nil
	}
	if len(input) == 0 {
		return true, nil
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		return false, fmt.Errorf("invalid format schema: %w", err)
	}

	typeStr, _ := schemaMap["type"].(string)
	if typeStr == "" {
		return true, nil // No type constraint, anything passes.
	}

	var inputVal any
	if err := json.Unmarshal(input, &inputVal); err != nil {
		return false, fmt.Errorf("invalid input JSON: %w", err)
	}

	switch typeStr {
	case "object":
		if _, ok := inputVal.(map[string]any); !ok {
			return false, fmt.Errorf("expected object, got %T", inputVal)
		}
	case "array":
		if _, ok := inputVal.([]any); !ok {
			return false, fmt.Errorf("expected array, got %T", inputVal)
		}
	case "string":
		if _, ok := inputVal.(string); !ok {
			return false, fmt.Errorf("expected string, got %T", inputVal)
		}
	case "number":
		switch inputVal.(type) {
		case float64, float32, int, int64, int32, json.Number:
		default:
			return false, fmt.Errorf("expected number, got %T", inputVal)
		}
	case "boolean":
		if _, ok := inputVal.(bool); !ok {
			return false, fmt.Errorf("expected boolean, got %T", inputVal)
		}
	case "null":
		if inputVal != nil {
			return false, fmt.Errorf("expected null, got %T", inputVal)
		}
	}

	// Check required fields for objects.
	if typeStr == "object" {
		required, _ := schemaMap["required"].([]any)
		if len(required) > 0 {
			obj := inputVal.(map[string]any)
			for _, r := range required {
				key, ok := r.(string)
				if !ok {
					continue
				}
				if _, exists := obj[key]; !exists {
					return false, fmt.Errorf("missing required field: %s", key)
				}
			}
		}
	}

	return true, nil
}

// ── Tx helpers ───────────────────────────────────────────────────────────────

func (s *WorkflowService) runInTx(ctx context.Context, fn func(*db.Queries) error) error {
	if s.TxStarter == nil {
		return fn(s.Queries)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// qtxForRun returns queries or nil. Used to pass a non-nil Queries to
// format checker dispatch while keeping the simple call signature.
func qtxForRun(q *db.Queries) *db.Queries {
	return q
}

// ── Agent task gateway (called from TaskService.CompleteTask) ────────────────

// HandleWorkflowTaskCompletion is called when an agent task linked to a
// workflow node run reaches completion. It transitions the node run based
// on the completed task's phase (worker → awaiting_critic, critic → review).
func (s *WorkflowService) HandleWorkflowTaskCompletion(ctx context.Context, task db.MulticaAgentTaskQueue) error {
	if !task.WorkflowNodeRunID.Valid {
		return nil
	}

	nodeRun, err := s.Queries.GetWorkflowNodeRun(ctx, task.WorkflowNodeRunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get node run: %w", err)
	}

	// Determine the phase from the task context.
	var ctxPayload struct {
		Phase string `json:"phase"`
	}
	if len(task.Context) > 0 {
		json.Unmarshal(task.Context, &ctxPayload)
	}

	switch ctxPayload.Phase {
	case "worker":
		if nodeRun.Status == NodeRunStatusWorking {
			// Transition to awaiting_critic with the task output as worker output.
			if err := s.runInTx(ctx, func(qtx *db.Queries) error {
				updated, err := qtx.SetWorkflowNodeRunWorkerOutput(ctx, db.SetWorkflowNodeRunWorkerOutputParams{
					ID:           nodeRun.ID,
					WorkerOutput: task.Result,
					Status:       NodeRunStatusAwaitingCritic,
				})
				if err != nil {
					return err
				}
				// Save updated for use after tx commits.
				nodeRun = updated
				return nil
			}); err != nil {
			return err
			}
			if err := s.dispatchCritic(ctx, nodeRun); err != nil {
				return fmt.Errorf("dispatch critic: %w", err)
			}
			return nil
		}
	case "critic":
		if nodeRun.Status == NodeRunStatusCriticReviewing {
			// Parse the agent's output for approve/rework decision.
			var output struct {
				Approved *bool  `json:"approved"`
				Comment  string `json:"comment"`
				Output   string `json:"output"`
			}
			if len(task.Result) > 0 {
				if err := json.Unmarshal(task.Result, &output); err != nil {
					t := true
					output.Approved = &t
				}
			} else {
				t := true
				output.Approved = &t
			}
			approved := true
			if output.Approved != nil {
				approved = *output.Approved
			} else if output.Output != "" {
				// Agent didn't include approved field — infer from output text.
				approved = !strings.Contains(strings.ToLower(output.Output), "不通过") &&
					!strings.Contains(strings.ToLower(output.Output), "reject")
			}
			return s.ReviewNodeRun(ctx, nodeRun.ID, approved, output.Comment, task.Result)
		}
	}

	return nil
}

// ── WS event helpers ─────────────────────────────────────────────────────────

func (s *WorkflowService) publishWorkflowEvent(eventType, workspaceID string, payload any) {
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

// workflow event type constants — duplicated here to avoid circular imports
// with protocol package when new events haven't been added yet.
const (
	EventWorkflowCreated       = "workflow:created"
	EventWorkflowUpdated       = "workflow:updated"
	EventWorkflowDeleted       = "workflow:deleted"
	EventWorkflowRunStarted    = "workflow:run_started"
	EventWorkflowRunCompleted  = "workflow:run_completed"
	EventWorkflowRunFailed     = "workflow:run_failed"
	EventWorkflowRunCancelled  = "workflow:run_cancelled"
	EventWorkflowNodeRunStarted  = "workflow:node_run_started"
	EventWorkflowNodeRunCompleted = "workflow:node_run_completed"
	EventWorkflowNodeRunFailed   = "workflow:node_run_failed"
	EventWorkflowNodeRunBlocked  = "workflow:node_run_blocked"
	EventWorkflowNodeRunReviewed = "workflow:node_run_reviewed"
)

// ── Template management ──────────────────────────────────────────────────────

// CloneWorkflowFromTemplate creates a new workflow by cloning a template's nodes
// and edges within a single transaction. The new workflow is created with
// is_template=false, source_template_id=templateID, status="draft".
// Returns the created workflow, its nodes, and edges.
func (s *WorkflowService) CloneWorkflowFromTemplate(
	ctx context.Context,
	templateID pgtype.UUID,
	workspaceID pgtype.UUID,
	title string,
	description string,
	creatorType string,
	creatorID pgtype.UUID,
) (db.MulticaWorkflow, []db.MulticaWorkflowNode, []db.MulticaWorkflowEdge, error) {
	var newWorkflow db.MulticaWorkflow
	var newNodes []db.MulticaWorkflowNode
	var newEdges []db.MulticaWorkflowEdge

	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		// 1. Verify the template exists and is actually a template.
		tmpl, err := qtx.GetWorkflow(ctx, templateID)
		if err != nil {
			return fmt.Errorf("template workflow not found: %w", err)
		}
		if !tmpl.IsTemplate {
			return fmt.Errorf("workflow %s is not a template", util.UUIDToString(templateID))
		}

		// 2. Create the new workflow from template.
		desc := pgtype.Text{String: description, Valid: true}
		wf, err := qtx.CreateWorkflowFromTemplate(ctx, db.CreateWorkflowFromTemplateParams{
			WorkspaceID:      workspaceID,
			Title:            title,
			Description:      desc,
			Status:           "draft",
			MaxRetries:       tmpl.MaxRetries,
			CreatedByType:    creatorType,
			CreatedByID:      creatorID,
			SourceTemplateID: templateID,
		})
		if err != nil {
			return fmt.Errorf("create workflow from template: %w", err)
		}
		newWorkflow = wf

		// 3. Clone all template nodes with new UUIDs and new workflow_id.
		tmplNodes, err := qtx.ListWorkflowNodes(ctx, templateID)
		if err != nil {
			return fmt.Errorf("list template nodes: %w", err)
		}
		oldToNew := make(map[string]pgtype.UUID, len(tmplNodes))
		for _, node := range tmplNodes {
			n, err := qtx.CreateWorkflowNode(ctx, db.CreateWorkflowNodeParams{
				WorkflowID:         wf.ID,
				Title:              node.Title,
				Description:        textToPgText(node.Description),
				PositionX:          node.PositionX,
				PositionY:          node.PositionY,
				FormatSchema:       node.FormatSchema,
				WorkerType:         node.WorkerType,
				WorkerID:           node.WorkerID,
				CriticType:         node.CriticType,
				CriticID:           node.CriticID,
				CriticApiUrl:       node.CriticApiUrl,
				SortOrder:          node.SortOrder,
			})
			if err != nil {
				return fmt.Errorf("clone node %s: %w", node.Title, err)
			}
			oldToNew[util.UUIDToString(node.ID)] = n.ID
			newNodes = append(newNodes, n)
		}

		// 4. Clone all template edges with remapped node IDs.
		tmplEdges, err := qtx.ListWorkflowEdges(ctx, templateID)
		if err != nil {
			return fmt.Errorf("list template edges: %w", err)
		}
		for _, edge := range tmplEdges {
			newSrc, ok := oldToNew[util.UUIDToString(edge.SourceNodeID)]
			if !ok {
				continue
			}
			newTgt, ok := oldToNew[util.UUIDToString(edge.TargetNodeID)]
			if !ok {
				continue
			}
			e, err := qtx.CreateWorkflowEdge(ctx, db.CreateWorkflowEdgeParams{
				WorkflowID:   wf.ID,
				SourceNodeID: newSrc,
				TargetNodeID: newTgt,
				Condition:    edge.Condition,
			})
			if err != nil {
				return fmt.Errorf("clone edge: %w", err)
			}
			newEdges = append(newEdges, e)
		}
		return nil
	})
	if err != nil {
		return db.MulticaWorkflow{}, nil, nil, err
	}
	return newWorkflow, newNodes, newEdges, nil
}

// SetWorkflowTemplate toggles the is_template flag on a workflow.
func (s *WorkflowService) SetWorkflowTemplate(ctx context.Context, workflowID pgtype.UUID, isTemplate bool) (db.MulticaWorkflow, error) {
	return s.Queries.SetWorkflowTemplate(ctx, db.SetWorkflowTemplateParams{
		ID:         workflowID,
		IsTemplate: isTemplate,
	})
}

// DeleteWorkflowWithTemplateCheck checks whether a template workflow has
// derived workflows (via source_template_id). If count > 0, it returns an
// error. Callers should use this before deleting a template workflow.
func (s *WorkflowService) DeleteWorkflowWithTemplateCheck(ctx context.Context, workflowID pgtype.UUID) error {
	count, err := s.Queries.CountWorkflowsBySourceTemplate(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("count derived workflows: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("template has %d derived workflows, cannot delete", count)
	}
	return nil
}

// CanManageWorkflows checks whether the given user has the
// can_manage_workflows permission bit set (global, not workspace-scoped).
func (s *WorkflowService) CanManageWorkflows(ctx context.Context, userID pgtype.UUID) (bool, error) {
	user, err := s.Queries.GetUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	return user.CanManageWorkflows, nil
}
