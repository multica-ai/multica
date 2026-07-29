package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SensitiveOperation describes one destructible action that the approval flow
// can gate. v1 ships a fixed built-in set (WS-721 decision 1); the registry is
// a map so additional operations register additively. Each operation carries
// its own payload validation and executor so the approval "execute" step runs
// the real action without the caller hand-rolling per-op branching.
type SensitiveOperation struct {
	Key         string
	Description string
	TargetType  string
	// ValidatePayload checks operation-specific params. It may be nil.
	ValidatePayload func(payload map[string]any) error
	// Execute runs the approved action. The returned result is recorded on the
	// executed approval_event and returned to the caller of the execute endpoint.
	Execute func(ctx context.Context, h *Handler, workspaceID pgtype.UUID, ar db.ApprovalRequest) (any, error)
}

// built-in sensitive operations. Order is stable for listing/config defaults.
var builtinSensitiveOps = []SensitiveOperation{
	{
		Key:         "workspace.delete",
		Description: "Delete the workspace and all of its data.",
		TargetType:  "workspace",
		Execute: func(ctx context.Context, h *Handler, workspaceID pgtype.UUID, ar db.ApprovalRequest) (any, error) {
			if err := h.Queries.DeleteWorkspace(ctx, workspaceID); err != nil {
				return nil, fmt.Errorf("delete workspace: %w", err)
			}
			return map[string]any{"deleted": true}, nil
		},
	},
	{
		Key:         "issue.batch_delete",
		Description: "Permanently delete a batch of issues.",
		TargetType:  "issue",
		ValidatePayload: func(payload map[string]any) error {
			ids, ok := payload["issue_ids"]
			if !ok {
				return fmt.Errorf("payload.issue_ids is required")
			}
			switch v := ids.(type) {
			case []any:
				if len(v) == 0 {
					return fmt.Errorf("payload.issue_ids must be a non-empty array")
				}
			case []string:
				if len(v) == 0 {
					return fmt.Errorf("payload.issue_ids must be a non-empty array")
				}
			default:
				return fmt.Errorf("payload.issue_ids must be a non-empty array")
			}
			return nil
		},
		Execute: func(ctx context.Context, h *Handler, workspaceID pgtype.UUID, ar db.ApprovalRequest) (any, error) {
			var p struct {
				IssueIDs []string `json:"issue_ids"`
			}
			if err := json.Unmarshal(ar.Payload, &p); err != nil {
				return nil, fmt.Errorf("decode payload: %w", err)
			}
			deleted := 0
			for _, idStr := range p.IssueIDs {
				id, err := parseUUIDLoose(idStr)
				if err != nil {
					continue
				}
				issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
					ID:          id,
					WorkspaceID: workspaceID,
				})
				if err != nil {
					continue
				}
				if h.TaskService != nil {
					h.TaskService.CancelTasksForIssue(ctx, issue.ID)
				}
				if err := h.Queries.DeleteIssue(ctx, db.DeleteIssueParams{
					ID:          issue.ID,
					WorkspaceID: issue.WorkspaceID,
				}); err != nil {
					continue
				}
				deleted++
			}
			return map[string]any{"deleted": deleted}, nil
		},
	},
	{
		Key:         "member.role_downgrade_owner",
		Description: "Demote a workspace owner to a regular member.",
		TargetType:  "member",
		Execute: func(ctx context.Context, h *Handler, workspaceID pgtype.UUID, ar db.ApprovalRequest) (any, error) {
			if !ar.TargetID.Valid {
				return nil, fmt.Errorf("target_id is required")
			}
			member, err := h.Queries.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{
				ID:   ar.TargetID,
				Role: "member",
			})
			if err != nil {
				return nil, fmt.Errorf("downgrade owner: %w", err)
			}
			return map[string]any{"member_id": uuidToString(member.ID), "role": member.Role}, nil
		},
	},
	{
		Key:         "agent.delete",
		Description: "Archive an agent (agents are soft-deleted).",
		TargetType:  "agent",
		Execute: func(ctx context.Context, h *Handler, workspaceID pgtype.UUID, ar db.ApprovalRequest) (any, error) {
			if !ar.TargetID.Valid {
				return nil, fmt.Errorf("target_id is required")
			}
			archivedBy := ar.DecidedByID
			if !archivedBy.Valid {
				archivedBy = ar.InitiatedByID
			}
			agent, err := h.Queries.ArchiveAgent(ctx, db.ArchiveAgentParams{
				ID:         ar.TargetID,
				ArchivedBy: archivedBy,
			})
			if err != nil {
				return nil, fmt.Errorf("archive agent: %w", err)
			}
			return map[string]any{"agent_id": uuidToString(agent.ID), "archived": true}, nil
		},
	},
	{
		Key:         "project.delete",
		Description: "Delete a project.",
		TargetType:  "project",
		Execute: func(ctx context.Context, h *Handler, workspaceID pgtype.UUID, ar db.ApprovalRequest) (any, error) {
			if !ar.TargetID.Valid {
				return nil, fmt.Errorf("target_id is required")
			}
			if err := h.Queries.DeleteProject(ctx, db.DeleteProjectParams{
				ID:          ar.TargetID,
				WorkspaceID: workspaceID,
			}); err != nil {
				return nil, fmt.Errorf("delete project: %w", err)
			}
			return map[string]any{"deleted": true}, nil
		},
	},
}

var sensitiveOpIndex = func() map[string]SensitiveOperation {
	m := make(map[string]SensitiveOperation, len(builtinSensitiveOps))
	for _, op := range builtinSensitiveOps {
		m[op.Key] = op
	}
	return m
}()

// LookupSensitiveOperation returns the registered operation for a key.
func LookupSensitiveOperation(key string) (SensitiveOperation, bool) {
	op, ok := sensitiveOpIndex[key]
	return op, ok
}

// BuiltinSensitiveOpKeys returns the built-in operation keys in stable order.
func BuiltinSensitiveOpKeys() []string {
	out := make([]string, len(builtinSensitiveOps))
	for i, op := range builtinSensitiveOps {
		out[i] = op.Key
	}
	return out
}

// approvalGateOutcome tells a destructive endpoint how to proceed.
type approvalGateOutcome struct {
	// Proceed is true when the action may run without an approval (flow off, op
	// not configured, or an already-approved request was supplied).
	Proceed bool
	// Approval is the pending request created by the gate (nil unless Pending).
	Approval *db.ApprovalRequest
}

// requireApproval implements the strong-blocking pre-check for a sensitive
// operation on a direct (non-/approvals) endpoint. It writes the response and
// returns (outcome, ok). When ok is false the caller must return immediately.
//
// Behavior:
//   - flow disabled or op not in the workspace's configured set -> Proceed.
//   - an approved approval_id query param matching op+workspace -> Proceed.
//   - otherwise a pending request is created and a 202 is written (not ok).
//
// targetID may be "" for operations without a single target (e.g. batch ops
// that carry their ids in payload).
func (h *Handler) requireApproval(w http.ResponseWriter, r *http.Request, op, targetID string, payload map[string]any) (approvalGateOutcome, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return approvalGateOutcome{}, false
	}

	if !h.approvalFlowEnabled(r.Context(), workspaceID) {
		return approvalGateOutcome{Proceed: true}, true
	}
	cfg, err := h.approvalConfig(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read approval config")
		return approvalGateOutcome{}, false
	}
	if !cfg.Enabled || !cfg.operationRequires(op) {
		return approvalGateOutcome{Proceed: true}, true
	}

	// An approved request supplied via ?approval_id= satisfies the gate.
	if aid := r.URL.Query().Get("approval_id"); aid != "" {
		ar, err := h.lookupApprovedForOp(r.Context(), aid, workspaceID, op)
		if err == nil && ar != nil {
			return approvalGateOutcome{Proceed: true}, true
		}
	}

	// Otherwise create a pending request and block the action.
	ar, ok := h.createApprovalInternal(w, r, wsUUID, op, targetID, "", payload, nil)
	if !ok {
		// createApprovalInternal already wrote the error response.
		return approvalGateOutcome{}, false
	}
	resp := approvalRequestToResponse(*ar)
	resp.Status = "pending"
	writeJSON(w, http.StatusAccepted, map[string]any{
		"approval":     resp,
		"approval_url": "/api/approvals/" + uuidToString(ar.ID),
		"message":      "approval required: action is blocked until the request is approved",
	})
	h.publish(protocol.EventApprovalRequestCreated, workspaceID, ar.InitiatedByType, uuidToString(ar.InitiatedByID), map[string]any{"approval": resp})
	return approvalGateOutcome{Approval: ar}, false
}
