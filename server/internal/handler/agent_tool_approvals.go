package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service/toolapproval"
)

const defaultToolApprovalLifetime = 15 * time.Minute

type agentToolApprovalDecisionRequest struct {
	Decision       string `json:"decision"`
	ReasonCode     string `json:"reason_code"`
	ExpectedStatus string `json:"expected_status"`
}

type daemonToolInvocationCreateRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	TransportKind  string `json:"transport_kind"`
	ServerKey      string `json:"server_key"`
	ToolName       string `json:"tool_name"`
	SchemaDigest   string `json:"schema_digest"`
	PolicyRevision int64  `json:"policy_revision"`
	ArgumentBytes  int32  `json:"argument_bytes"`
}

type daemonToolApprovalConsumeRequest struct {
	InvocationID   string `json:"invocation_id"`
	TransportKind  string `json:"transport_kind"`
	ServerKey      string `json:"server_key"`
	ToolName       string `json:"tool_name"`
	SchemaDigest   string `json:"schema_digest"`
	PolicyRevision int64  `json:"policy_revision"`
}

func (h *Handler) requireToolControlDaemonTask(w http.ResponseWriter, r *http.Request) (taskID, workspaceID, agentID, issueID, chatSessionID string, ok bool) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return "", "", "", "", "", false
	}
	daemonID := middleware.DaemonIDFromContext(r.Context())
	if daemonID == "" || !task.RuntimeID.Valid {
		writeError(w, http.StatusNotFound, "task not found")
		return "", "", "", "", "", false
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID)
	if err != nil || !runtime.DaemonID.Valid || runtime.DaemonID.String != daemonID || uuidToString(runtime.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "task not found")
		return "", "", "", "", "", false
	}
	return uuidToString(task.ID), workspaceID, uuidToString(task.AgentID), uuidToString(task.IssueID), uuidToString(task.ChatSessionID), true
}

func (h *Handler) ListAgentToolApprovals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != toolapproval.StatusPending {
		writeError(w, http.StatusBadRequest, "only the pending approval queue is supported")
		return
	}
	limit := int64(50)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	items, err := h.ToolApprovalService.ListPending(r.Context(), toolapproval.PendingQuery{
		WorkspaceID: workspaceID, AgentID: r.URL.Query().Get("agent_id"), Limit: int32(limit),
		Actor: toolapproval.Actor{Kind: toolapproval.ActorHuman, UserID: requestUserID(r), WorkspaceRole: member.Role},
	})
	if err != nil {
		writeToolApprovalError(w, err, "failed to list tool approvals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetAgentToolApproval(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	approval, err := h.ToolApprovalService.GetOperator(r.Context(), toolapproval.OperatorLookup{
		WorkspaceID: workspaceID, ApprovalID: chi.URLParam(r, "approvalId"),
		Actor: toolapproval.Actor{Kind: toolapproval.ActorHuman, UserID: requestUserID(r), WorkspaceRole: member.Role},
	})
	if err != nil {
		writeToolApprovalError(w, err, "failed to get tool approval")
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

func (h *Handler) DecideAgentToolApproval(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	var request agentToolApprovalDecisionRequest
	if err := decodeStrictJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	approval, err := h.ToolApprovalService.Decide(r.Context(), toolapproval.Decision{
		WorkspaceID: workspaceID, ApprovalID: chi.URLParam(r, "approvalId"),
		Actor:    toolapproval.Actor{Kind: toolapproval.ActorHuman, UserID: requestUserID(r), WorkspaceRole: member.Role},
		Decision: request.Decision, ReasonCode: request.ReasonCode, ExpectedStatus: request.ExpectedStatus,
	})
	if err != nil {
		writeToolApprovalError(w, err, "failed to decide tool approval")
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

func invocationUUID(taskID, idempotencyKey string) string {
	return uuid.NewSHA1(uuid.MustParse(taskID), []byte(idempotencyKey)).String()
}

func (h *Handler) CreateDaemonToolInvocation(w http.ResponseWriter, r *http.Request) {
	taskID, workspaceID, agentID, issueID, chatSessionID, ok := h.requireToolControlDaemonTask(w, r)
	if !ok {
		return
	}
	var request daemonToolInvocationCreateRequest
	if err := decodeStrictJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	effect, err := h.exactActiveToolPolicyEffect(r, workspaceID, agentID, request.TransportKind, request.ServerKey, request.ToolName, request.SchemaDigest, request.PolicyRevision)
	if err != nil {
		writeToolApprovalError(w, err, "failed to authorize tool invocation")
		return
	}
	invocationID := invocationUUID(taskID, request.IdempotencyKey)
	if effect == "allow" {
		writeJSON(w, http.StatusCreated, map[string]any{"invocation_id": invocationID, "status": "allowed"})
		return
	}
	approval, err := h.ToolApprovalService.CreateOrGet(r.Context(), toolapproval.Creation{
		WorkspaceID: workspaceID, AgentID: agentID, TaskID: taskID, IssueID: issueID, ChatSessionID: chatSessionID,
		InvocationID: invocationID, IdempotencyKey: request.IdempotencyKey,
		TransportKind: request.TransportKind, ServerKey: request.ServerKey, ToolName: request.ToolName,
		SchemaDigest: request.SchemaDigest, PolicyRevision: request.PolicyRevision,
		SchemaFieldNames: []string{}, ArgumentBytes: request.ArgumentBytes,
		ExpiresAt: time.Now().UTC().Add(defaultToolApprovalLifetime), Actor: toolapproval.Actor{Kind: toolapproval.ActorDaemon},
	})
	if err != nil {
		slog.Warn("create daemon tool approval failed", "task_id", taskID, "error", err)
		writeToolApprovalError(w, err, "failed to create tool approval")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invocation_id": invocationID, "approval_request_id": approval.ID, "status": approval.Status})
}

func (h *Handler) GetDaemonToolApproval(w http.ResponseWriter, r *http.Request) {
	taskID, workspaceID, _, _, _, ok := h.requireToolControlDaemonTask(w, r)
	if !ok {
		return
	}
	approval, err := h.ToolApprovalService.Get(r.Context(), toolapproval.Lookup{
		WorkspaceID: workspaceID, TaskID: taskID, ApprovalID: chi.URLParam(r, "approvalId"), Actor: toolapproval.Actor{Kind: toolapproval.ActorDaemon},
	})
	if err != nil {
		writeToolApprovalError(w, err, "failed to get tool approval")
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

func (h *Handler) ConsumeDaemonToolApproval(w http.ResponseWriter, r *http.Request) {
	taskID, workspaceID, _, _, _, ok := h.requireToolControlDaemonTask(w, r)
	if !ok {
		return
	}
	var request daemonToolApprovalConsumeRequest
	if err := decodeStrictJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	approval, err := h.ToolApprovalService.Consume(r.Context(), toolapproval.Consumption{
		WorkspaceID: workspaceID, TaskID: taskID, ApprovalID: chi.URLParam(r, "approvalId"), Actor: toolapproval.Actor{Kind: toolapproval.ActorDaemon},
		InvocationID: request.InvocationID, TransportKind: request.TransportKind, ServerKey: request.ServerKey,
		ToolName: request.ToolName, SchemaDigest: request.SchemaDigest, PolicyRevision: request.PolicyRevision,
	})
	if err != nil {
		writeToolApprovalError(w, err, "failed to consume tool approval")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorized": true, "approval": approval})
}

func writeToolApprovalError(w http.ResponseWriter, err error, internalMessage string) {
	switch {
	case errors.Is(err, toolapproval.ErrForbidden):
		writeError(w, http.StatusForbidden, "insufficient permissions")
	case errors.Is(err, toolapproval.ErrNotFound):
		writeError(w, http.StatusNotFound, "tool approval not found")
	case errors.Is(err, toolapproval.ErrIdentityConflict), errors.Is(err, toolapproval.ErrPolicyConflict), errors.Is(err, toolapproval.ErrStateConflict):
		writeError(w, http.StatusConflict, "tool approval state conflict")
	case errors.Is(err, toolapproval.ErrInvalidMetadata), errors.Is(err, toolapproval.ErrRawMetadata), errors.Is(err, toolapproval.ErrInvalidDecision):
		writeError(w, http.StatusBadRequest, "invalid metadata-only tool approval request")
	default:
		writeError(w, http.StatusInternalServerError, internalMessage)
	}
}
