package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// AuthorizeRunManager is used by HTTP handlers before actions that do not
// carry their own transactional ownership change. The mutating service method
// still rechecks the same rule while holding the run lock.
func (s *Service) AuthorizeRunManager(ctx context.Context, ws, actor, workflowID, runID string) error {
	run, creator, err := getRun(ctx, s.DB, ws, workflowID, runID, false)
	if err != nil {
		return err
	}
	return requireRunManager(ctx, s.DB, ws, actor, run.OwnerID, creator)
}

// UpdateRunOwner makes an in-flight ownership change explicit. It does not
// rewrite assignees on existing work items; those are separate, versioned
// decisions and remain subject to their own transfer contract.
func (s *Service) UpdateRunOwner(ctx context.Context, ws, actor, runID, ownerID, reason string, expectedStateRevision int64) (Run, error) {
	return s.updateRunOwner(ctx, ws, actor, runID, ownerID, reason, expectedStateRevision, "")
}

func (s *Service) UpdateRunOwnerKey(ctx context.Context, ws, actor, runID, ownerID, reason string, expectedStateRevision int64, idempotencyKey string) (Run, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Run{}, Bad("idempotency_key is required")
	}
	return s.updateRunOwner(ctx, ws, actor, runID, ownerID, reason, expectedStateRevision, idempotencyKey)
}

func (s *Service) updateRunOwner(ctx context.Context, ws, actor, runID, ownerID, reason string, expectedStateRevision int64, idempotencyKey string) (Run, error) {
	if _, err := util.ParseUUID(ownerID); err != nil {
		return Run{}, Bad("invalid run owner")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	run, creator, err := getRunByID(ctx, tx, ws, runID, true)
	if err != nil {
		return Run{}, err
	}
	if err = requireRunManager(ctx, tx, ws, actor, run.OwnerID, creator); err != nil {
		return Run{}, err
	}
	requestHash := workflowCommandHash("workflow_run.owner", runID, struct {
		OwnerID          string `json:"owner_id"`
		Reason           string `json:"reason"`
		ExpectedRevision int64  `json:"expected_state_revision"`
	}{OwnerID: ownerID, Reason: reason, ExpectedRevision: expectedStateRevision})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, actor, "workflow_run.owner", runID, idempotencyKey, requestHash)
	if err != nil {
		return Run{}, err
	}
	if len(responseBody) > 0 {
		var replay Run
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return Run{}, err
		}
		return replay, nil
	}
	if expectedStateRevision > 0 && expectedStateRevision != stateRevision(run) {
		return Run{}, Conflict("run changed; refresh before changing its owner")
	}
	resolvedOwner, err := workspaceMemberUserID(ctx, tx, ws, ownerID)
	if err != nil {
		return Run{}, err
	}
	run.OwnerID = resolvedOwner
	if strings.TrimSpace(reason) != "" {
		run.ReasonCode = "owner_transferred"
	}
	run.StateRevision = stateRevision(run) + 1
	if err = saveRun(ctx, tx, &run); err != nil {
		return Run{}, err
	}
	if err = s.recordRunMutation(ctx, tx, run, protocol.EventWorkflowRunUpdated, "member", actor); err != nil {
		return Run{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, actor, "workflow_run.owner", runID, idempotencyKey, requestHash, run); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	s.publish(protocol.EventWorkflowRunUpdated, ws, run.WorkflowID, run.ID)
	return run, nil
}

// TakeoverNode creates a recovery work item without changing the published
// graph or pretending that an agent identity was converted into a member.
func (s *Service) TakeoverNode(ctx context.Context, ws, actor, runID, nodeID, memberID, reason string, expectedStateRevision int64) (Run, error) {
	return s.takeoverNode(ctx, ws, actor, runID, nodeID, memberID, reason, expectedStateRevision, "")
}

func (s *Service) TakeoverNodeKey(ctx context.Context, ws, actor, runID, nodeID, memberID, reason string, expectedStateRevision int64, idempotencyKey string) (Run, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Run{}, Bad("idempotency_key is required")
	}
	return s.takeoverNode(ctx, ws, actor, runID, nodeID, memberID, reason, expectedStateRevision, idempotencyKey)
}

func (s *Service) takeoverNode(ctx context.Context, ws, actor, runID, nodeID, memberID, reason string, expectedStateRevision int64, idempotencyKey string) (Run, error) {
	if _, err := util.ParseUUID(memberID); err != nil {
		return Run{}, Bad("invalid takeover member")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	run, creator, err := getRunByID(ctx, tx, ws, runID, true)
	if err != nil {
		return Run{}, err
	}
	if err = requireRunManager(ctx, tx, ws, actor, run.OwnerID, creator); err != nil {
		return Run{}, err
	}
	requestHash := workflowCommandHash("workflow_node.takeover", runID, struct {
		NodeID           string `json:"node_id"`
		MemberID         string `json:"member_id"`
		Reason           string `json:"reason"`
		ExpectedRevision int64  `json:"expected_state_revision"`
	}{NodeID: nodeID, MemberID: memberID, Reason: reason, ExpectedRevision: expectedStateRevision})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, actor, "workflow_node.takeover", runID, idempotencyKey, requestHash)
	if err != nil {
		return Run{}, err
	}
	if len(responseBody) > 0 {
		var replay Run
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return Run{}, err
		}
		return replay, nil
	}
	if expectedStateRevision > 0 && expectedStateRevision != stateRevision(run) {
		return Run{}, Conflict("run changed; refresh before taking over the node")
	}
	owner, err := workspaceMemberUserID(ctx, tx, ws, memberID)
	if err != nil {
		return Run{}, err
	}
	var node *NodeRun
	for index := range run.Nodes {
		if run.Nodes[index].NodeID == nodeID {
			node = &run.Nodes[index]
			break
		}
	}
	if node == nil {
		return Run{}, Bad("node not found")
	}
	if node.Status != "failed" && node.Status != "blocked" {
		return Run{}, Conflict("only a failed or blocked node can be taken over")
	}
	if node.WorkItemID != "" {
		var status string
		err = tx.QueryRow(ctx, "SELECT status FROM workflow_work_item WHERE id=$1", node.WorkItemID).Scan(&status)
		if err == nil && status == "open" {
			return Run{}, Conflict("node already has an open recovery work item")
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Run{}, err
		}
	}
	definition := nodeDefinition(run.Graph, nodeID)
	states := make(map[string]*NodeRun, len(run.Nodes))
	for index := range run.Nodes {
		states[run.Nodes[index].NodeID] = &run.Nodes[index]
	}
	replaceNodeActivation(node, false)
	itemID, err := createRecoveryWorkItem(ctx, tx, ws, run.ID, nodeID, node.ActivationID, owner, definition, run.Input, states, run.Graph.Defaults.HumanTimeoutSeconds)
	if err != nil {
		return Run{}, err
	}
	node.WorkItemID = itemID
	node.Status = "waiting_human"
	node.ReasonCode = "awaiting_takeover"
	if strings.TrimSpace(reason) != "" {
		node.Error = "takeover: " + strings.TrimSpace(reason)
	}
	run.Status = "waiting"
	run.StateRevision = stateRevision(run) + 1
	if err = saveRun(ctx, tx, &run); err != nil {
		return Run{}, err
	}
	if err = s.recordRunMutation(ctx, tx, run, protocol.EventWorkflowWorkItemUpdated, "member", actor); err != nil {
		return Run{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, actor, "workflow_node.takeover", runID, idempotencyKey, requestHash, run); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	s.publish(protocol.EventWorkflowWorkItemUpdated, ws, run.WorkflowID, run.ID)
	s.publish(protocol.EventWorkflowRunUpdated, ws, run.WorkflowID, run.ID)
	return run, nil
}

type NodeResolution struct {
	Resolution string
	Values     map[string]any
	Evidence   string
}

// ResolveNode records an explicit human finding for an uncertain execution.
// The accepted resolutions intentionally form a closed set; arbitrary status
// writes cannot be used to jump over the workflow controller.
func (s *Service) ResolveNode(ctx context.Context, ws, actor, runID, nodeID string, resolution NodeResolution, expectedStateRevision int64) (Run, error) {
	return s.resolveNode(ctx, ws, actor, runID, nodeID, resolution, expectedStateRevision, "")
}

func (s *Service) ResolveNodeKey(ctx context.Context, ws, actor, runID, nodeID string, resolution NodeResolution, expectedStateRevision int64, idempotencyKey string) (Run, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Run{}, Bad("idempotency_key is required")
	}
	return s.resolveNode(ctx, ws, actor, runID, nodeID, resolution, expectedStateRevision, idempotencyKey)
}

func (s *Service) resolveNode(ctx context.Context, ws, actor, runID, nodeID string, resolution NodeResolution, expectedStateRevision int64, idempotencyKey string) (Run, error) {
	switch resolution.Resolution {
	case "confirmed_not_executed", "confirmed_completed", "confirmed_stopped":
	default:
		return Run{}, Bad("unsupported node resolution")
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	run, creator, err := getRunByID(ctx, tx, ws, runID, true)
	if err != nil {
		return Run{}, err
	}
	if err = requireRunManager(ctx, tx, ws, actor, run.OwnerID, creator); err != nil {
		return Run{}, err
	}
	requestHash := workflowCommandHash("workflow_node.resolve", runID, struct {
		NodeID           string         `json:"node_id"`
		Resolution       NodeResolution `json:"resolution"`
		ExpectedRevision int64          `json:"expected_state_revision"`
	}{NodeID: nodeID, Resolution: resolution, ExpectedRevision: expectedStateRevision})
	responseBody, err := beginWorkflowCommand(ctx, tx, ws, actor, "workflow_node.resolve", runID, idempotencyKey, requestHash)
	if err != nil {
		return Run{}, err
	}
	if len(responseBody) > 0 {
		var replay Run
		if err = json.Unmarshal(responseBody, &replay); err != nil {
			return Run{}, err
		}
		return replay, nil
	}
	if expectedStateRevision > 0 && expectedStateRevision != stateRevision(run) {
		return Run{}, Conflict("run changed; refresh before resolving the node")
	}
	var node *NodeRun
	for index := range run.Nodes {
		if run.Nodes[index].NodeID == nodeID {
			node = &run.Nodes[index]
			break
		}
	}
	if node == nil {
		return Run{}, Bad("node not found")
	}
	if node.Status != "failed" && node.Status != "blocked" {
		return Run{}, Conflict("only a failed or blocked node can be resolved")
	}
	if node.IssueID != "" {
		if _, err = db.New(tx).CancelAgentTasksByIssue(ctx, uuid(node.IssueID)); err != nil {
			return Run{}, err
		}
	}
	if resolution.Resolution == "confirmed_completed" {
		if resolution.Values == nil {
			return Run{}, Bad("confirmed_completed requires output values")
		}
		encoded, marshalErr := json.Marshal(resolution.Values)
		if marshalErr != nil {
			return Run{}, Bad("invalid resolution values")
		}
		if outputErr := validateNodeOutput(nodeDefinition(run.Graph, nodeID), string(encoded)); outputErr != nil {
			return Run{}, outputErr
		}
		if outputErr := s.validateNodeOutputArtifacts(ctx, ws, actor, nodeDefinition(run.Graph, nodeID), string(encoded)); outputErr != nil {
			return Run{}, outputErr
		}
		node.Status = "succeeded"
		node.Output = string(encoded)
		node.ReasonCode = "resolved_completed"
		node.Error = ""
		run.Status = "running"
	} else {
		node.Status = "failed"
		node.ReasonCode = resolution.Resolution
		node.Error = strings.TrimSpace(resolution.Evidence)
		if node.Error == "" {
			node.Error = resolution.Resolution
		}
		run.Status = "blocked"
	}
	run.StateRevision = stateRevision(run) + 1
	if err = saveRun(ctx, tx, &run); err != nil {
		return Run{}, err
	}
	if err = s.recordRunMutation(ctx, tx, run, protocol.EventWorkflowRunUpdated, "member", actor); err != nil {
		return Run{}, err
	}
	if err = finishWorkflowCommand(ctx, tx, ws, actor, "workflow_node.resolve", runID, idempotencyKey, requestHash, run); err != nil {
		return Run{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, err
	}
	s.publish(protocol.EventWorkflowRunUpdated, ws, run.WorkflowID, run.ID)
	s.Wake()
	return run, nil
}

func requireRunManager(ctx context.Context, q db.DBTX, workspaceID, actor, ownerID, creatorID string) error {
	if actor == ownerID || actor == creatorID {
		return nil
	}
	var allowed bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin'))`, workspaceID, actor).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return &Error{Status: 403, Message: "only the run owner or a workspace administrator can manage this run", Code: "action_forbidden"}
	}
	return nil
}

func workspaceMemberUserID(ctx context.Context, q db.DBTX, workspaceID, memberID string) (string, error) {
	var userID string
	if err := q.QueryRow(ctx, `SELECT user_id::text FROM member WHERE workspace_id=$1 AND (id=$2 OR user_id=$2)`, workspaceID, memberID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return "", &Error{Status: 404, Message: "member not found in workspace", Code: "member_not_found"}
	} else if err != nil {
		return "", err
	}
	return userID, nil
}
