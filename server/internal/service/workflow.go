package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	util "github.com/multica-ai/multica/server/internal/util"
)

// WorkflowService handles workflow, node, and edge operations.
type WorkflowService struct {
	Queries *db.Queries
}

type WorkflowOutput struct {
	ID        string `json:"id"`
	PlanID    string `json:"plan_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type WorkflowNodeOutput struct {
	ID         string  `json:"id"`
	WorkflowID string  `json:"workflow_id"`
	AgentID    string  `json:"agent_id"`
	Title      string  `json:"title"`
	Prompt     string  `json:"prompt"`
	PositionX  float64 `json:"position_x"`
	PositionY  float64 `json:"position_y"`
	Status     string  `json:"status"`
	TaskID     *string `json:"task_id"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

type WorkflowEdgeOutput struct {
	ID           string `json:"id"`
	WorkflowID   string `json:"workflow_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
}

type ConfirmResult struct {
	Workflow WorkflowOutput       `json:"workflow"`
	Nodes    []WorkflowNodeOutput `json:"nodes"`
}

// Get returns a workflow by ID.
func (s *WorkflowService) Get(ctx context.Context, workflowID pgtype.UUID) (WorkflowOutput, error) {
	wf, err := s.Queries.GetWorkflow(ctx, workflowID)
	if err != nil {
		return WorkflowOutput{}, err
	}
	return workflowToOutput(wf), nil
}

// Update updates a workflow's title and/or status.
func (s *WorkflowService) Update(ctx context.Context, workflowID pgtype.UUID, title, status *string) (WorkflowOutput, error) {
	wf, err := s.Queries.UpdateWorkflow(ctx, db.UpdateWorkflowParams{
		ID:     workflowID,
		Title:  util.PtrToText(title),
		Status: util.PtrToText(status),
	})
	if err != nil {
		return WorkflowOutput{}, err
	}
	return workflowToOutput(wf), nil
}

// ListNodes returns all nodes for a workflow.
func (s *WorkflowService) ListNodes(ctx context.Context, workflowID pgtype.UUID) ([]WorkflowNodeOutput, error) {
	nodes, err := s.Queries.ListWorkflowNodes(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	return nodesToOutput(nodes), nil
}

// CreateNode creates a new workflow node.
func (s *WorkflowService) CreateNode(ctx context.Context, workflowID, agentID pgtype.UUID, title, prompt string, x, y float64) (WorkflowNodeOutput, error) {
	node, err := s.Queries.CreateWorkflowNode(ctx, db.CreateWorkflowNodeParams{
		WorkflowID: workflowID,
		AgentID:   agentID,
		Title:     title,
		Prompt:    prompt,
		PositionX: x,
		PositionY: y,
	})
	if err != nil {
		return WorkflowNodeOutput{}, err
	}
	return nodeToOutput(node), nil
}

type UpdateNodeInput struct {
	Title     *string
	Prompt    *string
	AgentID   *string
	PositionX *float64
	PositionY *float64
	Status    *string
	TaskID    *string
}

// UpdateNode updates a workflow node's fields.
func (s *WorkflowService) UpdateNode(ctx context.Context, nodeID pgtype.UUID, input *UpdateNodeInput) (WorkflowNodeOutput, error) {
	var taskID pgtype.UUID
	if input.TaskID != nil {
		taskID = util.MustParseUUID(*input.TaskID)
	}
	var agentID pgtype.UUID
	if input.AgentID != nil {
		agentID = util.MustParseUUID(*input.AgentID)
	}
	var posX pgtype.Float8
	if input.PositionX != nil {
		posX = pgtype.Float8{Float64: *input.PositionX, Valid: true}
	}
	var posY pgtype.Float8
	if input.PositionY != nil {
		posY = pgtype.Float8{Float64: *input.PositionY, Valid: true}
	}

	node, err := s.Queries.UpdateWorkflowNode(ctx, db.UpdateWorkflowNodeParams{
		ID:        nodeID,
		Title:     util.PtrToText(input.Title),
		Prompt:    util.PtrToText(input.Prompt),
		AgentID:   agentID,
		PositionX: posX,
		PositionY: posY,
		Status:    util.PtrToText(input.Status),
		TaskID:    taskID,
	})
	if err != nil {
		return WorkflowNodeOutput{}, err
	}
	return nodeToOutput(node), nil
}

// DeleteNode deletes a workflow node.
func (s *WorkflowService) DeleteNode(ctx context.Context, nodeID pgtype.UUID) error {
	// TODO: Delete associated edges first (Phase 2)
	return s.Queries.DeleteWorkflowNode(ctx, nodeID)
}

// ListEdges returns all edges for a workflow.
func (s *WorkflowService) ListEdges(ctx context.Context, workflowID pgtype.UUID) ([]WorkflowEdgeOutput, error) {
	edges, err := s.Queries.ListWorkflowEdges(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowEdgeOutput, len(edges))
	for i, e := range edges {
		out[i] = WorkflowEdgeOutput{
			ID:           util.UUIDToString(e.ID),
			WorkflowID:   util.UUIDToString(e.WorkflowID),
			SourceNodeID: util.UUIDToString(e.SourceNodeID),
			TargetNodeID: util.UUIDToString(e.TargetNodeID),
		}
	}
	return out, nil
}

// CreateEdge creates a new workflow edge.
func (s *WorkflowService) CreateEdge(ctx context.Context, workflowID, sourceID, targetID pgtype.UUID) (WorkflowEdgeOutput, error) {
	// Cycle detection stub — Phase 2 will implement proper DFS
	if hasCycle(workflowID, sourceID, targetID) {
		return WorkflowEdgeOutput{}, errors.New("cycle detected")
	}
	edge, err := s.Queries.CreateWorkflowEdge(ctx, db.CreateWorkflowEdgeParams{
		WorkflowID:   workflowID,
		SourceNodeID: sourceID,
		TargetNodeID: targetID,
	})
	if err != nil {
		return WorkflowEdgeOutput{}, err
	}
	return WorkflowEdgeOutput{
		ID:           util.UUIDToString(edge.ID),
		WorkflowID:   util.UUIDToString(edge.WorkflowID),
		SourceNodeID: util.UUIDToString(edge.SourceNodeID),
		TargetNodeID: util.UUIDToString(edge.TargetNodeID),
	}, nil
}

// DeleteEdge deletes a workflow edge.
func (s *WorkflowService) DeleteEdge(ctx context.Context, edgeID pgtype.UUID) error {
	return s.Queries.DeleteWorkflowEdge(ctx, edgeID)
}

// Confirm confirms the DAG and starts dispatching nodes with no incoming edges.
func (s *WorkflowService) Confirm(ctx context.Context, workflowID pgtype.UUID) (ConfirmResult, error) {
	wf, err := s.Queries.GetWorkflow(ctx, workflowID)
	if err != nil {
		return ConfirmResult{}, err
	}

	// Update workflow status to running
	wf, err = s.Queries.UpdateWorkflow(ctx, db.UpdateWorkflowParams{
		ID:     wf.ID,
		Title:  pgtype.Text{Valid: false},
		Status: pgtype.Text{String: "running", Valid: true},
	})
	if err != nil {
		return ConfirmResult{}, err
	}

	// Dispatch ready nodes (those with no incoming edges)
	dispatched, err := s.dispatchReadyNodes(ctx, workflowID)
	if err != nil {
		return ConfirmResult{}, err
	}

	return ConfirmResult{
		Workflow: workflowToOutput(wf),
		Nodes:    nodesToOutput(dispatched),
	}, nil
}

// dispatchReadyNodes finds all pending nodes with no incoming edges and dispatches them.
func (s *WorkflowService) dispatchReadyNodes(ctx context.Context, workflowID pgtype.UUID) ([]db.WorkflowNode, error) {
	allNodes, err := s.Queries.ListWorkflowNodes(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	edges, err := s.Queries.ListWorkflowEdges(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	// Build in-degree map
	inDegree := make(map[string]int)
	for _, n := range allNodes {
		inDegree[util.UUIDToString(n.ID)] = 0
	}
	for _, e := range edges {
		inDegree[util.UUIDToString(e.TargetNodeID)]++
	}

	var dispatched []db.WorkflowNode
	for _, node := range allNodes {
		if node.Status != "pending" {
			continue
		}
		// No incoming edges → can dispatch immediately
		if inDegree[util.UUIDToString(node.ID)] == 0 {
			taskID, err := s.createTaskForNode(ctx, node)
			if err != nil {
				slog.Error("failed to create task for node", "node_id", util.UUIDToString(node.ID), "err", err)
				continue
			}
			updated, err := s.Queries.UpdateWorkflowNode(ctx, db.UpdateWorkflowNodeParams{
				ID:        node.ID,
				Title:     pgtype.Text{Valid: false},
				Prompt:    pgtype.Text{Valid: false},
				AgentID:   pgtype.UUID{Valid: false},
				PositionX: pgtype.Float8{Valid: false},
				PositionY: pgtype.Float8{Valid: false},
				Status:    pgtype.Text{String: "queued", Valid: true},
				TaskID:    util.MustParseUUID(taskID),
			})
			if err != nil {
				return nil, err
			}
			dispatched = append(dispatched, updated)
		}
	}
	return dispatched, nil
}

// createTaskForNode is a stub; Phase 2 will integrate with TaskService.
func (s *WorkflowService) createTaskForNode(ctx context.Context, node db.WorkflowNode) (string, error) {
	slog.Info("createTaskForNode stub called", "node_id", util.UUIDToString(node.ID))
	return "", nil
}

// hasCycle — cycle detection is implemented in Phase 2.
// For Phase 1, we allow all edges (no cycle prevention).
func hasCycle(workflowID, sourceID, targetID pgtype.UUID) bool {
	return false
}

// Output converters.

func workflowToOutput(w db.Workflow) WorkflowOutput {
	return WorkflowOutput{
		ID:        util.UUIDToString(w.ID),
		PlanID:    util.UUIDToString(w.PlanID),
		Title:     w.Title,
		Status:    w.Status,
		CreatedAt: util.TimestampToString(w.CreatedAt),
		UpdatedAt: util.TimestampToString(w.UpdatedAt),
	}
}

func nodeToOutput(n db.WorkflowNode) WorkflowNodeOutput {
	return WorkflowNodeOutput{
		ID:         util.UUIDToString(n.ID),
		WorkflowID: util.UUIDToString(n.WorkflowID),
		AgentID:    util.UUIDToString(n.AgentID),
		Title:      n.Title,
		Prompt:     n.Prompt,
		PositionX:  n.PositionX,
		PositionY:  n.PositionY,
		Status:     n.Status,
		TaskID:     util.UUIDToPtr(n.TaskID),
		CreatedAt:  util.TimestampToString(n.CreatedAt),
		UpdatedAt:  util.TimestampToString(n.UpdatedAt),
	}
}

func nodesToOutput(nodes []db.WorkflowNode) []WorkflowNodeOutput {
	out := make([]WorkflowNodeOutput, len(nodes))
	for i, n := range nodes {
		out[i] = nodeToOutput(n)
	}
	return out
}
