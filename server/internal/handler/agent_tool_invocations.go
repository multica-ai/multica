package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	"github.com/multica-ai/multica/server/internal/service/toolapproval"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type daemonToolInvocationEventRequest struct {
	EventType      string                `json:"event_type"`
	TransportKind  string                `json:"transport_kind"`
	ServerKey      string                `json:"server_key"`
	ToolName       string                `json:"tool_name"`
	SchemaDigest   string                `json:"schema_digest"`
	PolicyRevision int64                 `json:"policy_revision"`
	ArgumentBytes  *int32                `json:"argument_bytes,omitempty"`
	ResultBytes    *int32                `json:"result_bytes,omitempty"`
	DurationMS     *int64                `json:"duration_ms,omitempty"`
	OutcomeCode    string                `json:"outcome_code,omitempty"`
	ErrorClass     string                `json:"error_class,omitempty"`
	TaskMessage    daemonToolTaskMessage `json:"task_message"`
}

type daemonToolTaskMessage struct {
	InvocationID string `json:"invocation_id"`
	Type         string `json:"type"`
	Tool         string `json:"tool"`
	OutcomeCode  string `json:"outcome_code,omitempty"`
	ErrorClass   string `json:"error_class,omitempty"`
}

func (h *Handler) exactActiveToolPolicyEffect(r *http.Request, workspaceID, agentID, transportKind, serverKey, toolName, schemaDigest string, revision int64) (string, error) {
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return "", toolapproval.ErrInvalidMetadata
	}
	agentUUID, err := util.ParseUUID(agentID)
	if err != nil {
		return "", toolapproval.ErrInvalidMetadata
	}
	var effect string
	err = h.DB.QueryRow(r.Context(), `
		SELECT rule.effect
		FROM agent_tool_policy AS policy
		JOIN agent_tool_policy_rule AS rule
		  ON rule.workspace_id = policy.workspace_id
		 AND rule.agent_id = policy.agent_id
		 AND rule.policy_id = policy.id
		WHERE policy.workspace_id = $1
		  AND policy.agent_id = $2
		  AND policy.status = 'active'
		  AND policy.revision = $3
		  AND rule.transport_kind = $4
		  AND rule.server_key = $5
		  AND rule.tool_name = $6
		  AND rule.schema_digest = $7
	`, workspaceUUID, agentUUID, revision, transportKind, serverKey, toolName, schemaDigest).Scan(&effect)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", toolapproval.ErrPolicyConflict
	}
	if err != nil {
		return "", fmt.Errorf("read exact active tool policy: %w", err)
	}
	if effect != "allow" && effect != "require_approval" {
		return "", toolapproval.ErrPolicyConflict
	}
	return effect, nil
}

func (h *Handler) CommitDaemonToolInvocationEvent(w http.ResponseWriter, r *http.Request) {
	taskID, workspaceID, agentID, issueID, _, ok := h.requireToolControlDaemonTask(w, r)
	if !ok {
		return
	}
	invocationID := chi.URLParam(r, "invocationId")
	if _, err := util.ParseUUID(invocationID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid invocation_id")
		return
	}
	var request daemonToolInvocationEventRequest
	if err := decodeStrictJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.EventType != "started" && request.EventType != "succeeded" && request.EventType != "failed" && request.EventType != "cancelled" {
		writeError(w, http.StatusBadRequest, "daemon event type is not allowed")
		return
	}
	expectedMessageType := "tool_result"
	if request.EventType == "started" {
		expectedMessageType = "tool_use"
	}
	if request.TaskMessage.InvocationID != invocationID || request.TaskMessage.Type != expectedMessageType || request.TaskMessage.Tool != request.ToolName {
		writeError(w, http.StatusBadRequest, "task message identity mismatch")
		return
	}

	workspaceUUID := util.MustParseUUID(workspaceID)
	taskUUID := util.MustParseUUID(taskID)
	invocationUUIDValue := util.MustParseUUID(invocationID)
	approvalRequestID := ""
	approval, err := h.Queries.GetAgentToolApprovalRequestForInvocation(r.Context(), db.GetAgentToolApprovalRequestForInvocationParams{
		WorkspaceID: workspaceUUID, TaskID: taskUUID, InvocationID: invocationUUIDValue,
	})
	switch {
	case err == nil:
		if uuidToString(approval.AgentID) != agentID || approval.TransportKind != request.TransportKind ||
			approval.ServerKey != request.ServerKey || approval.ToolName != request.ToolName ||
			approval.SchemaDigest != request.SchemaDigest || approval.PolicyRevision != request.PolicyRevision {
			writeError(w, http.StatusConflict, "tool invocation identity conflict")
			return
		}
		approvalRequestID = uuidToString(approval.ID)
	case errors.Is(err, pgx.ErrNoRows):
		effect, policyErr := h.exactActiveToolPolicyEffect(r, workspaceID, agentID, request.TransportKind, request.ServerKey, request.ToolName, request.SchemaDigest, request.PolicyRevision)
		if policyErr != nil || effect != "allow" {
			writeError(w, http.StatusConflict, "tool invocation policy conflict")
			return
		}
	default:
		writeError(w, http.StatusInternalServerError, "failed to load tool invocation")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin tool invocation event")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	if _, err := tx.Exec(r.Context(), `SELECT 1 FROM agent_task_queue WHERE id = $1 FOR UPDATE`, taskUUID); err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	event := toolaction.Event{
		WorkspaceID: workspaceID, AgentID: agentID, TaskID: taskID, IssueID: issueID,
		InvocationID: invocationID, ApprovalRequestID: approvalRequestID,
		TransportKind: request.TransportKind, ServerKey: request.ServerKey, ToolName: request.ToolName,
		SchemaDigest: request.SchemaDigest, CoverageKind: request.TransportKind, EventType: request.EventType,
		ArgumentBytes: request.ArgumentBytes, ResultBytes: request.ResultBytes, DurationMS: request.DurationMS,
		OutcomeCode: request.OutcomeCode, ErrorClass: request.ErrorClass, CreatedAt: time.Now().UTC(),
	}
	if _, err := toolaction.NewSQLService(queries).RecordIn(r.Context(), queries, event); err != nil {
		writeToolControlError(w, err, "failed to record tool invocation event")
		return
	}
	var nextSeq int32
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(seq), 0) + 1 FROM task_message WHERE task_id = $1`, taskUUID).Scan(&nextSeq); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate task message sequence")
		return
	}
	messageID := uuid.NewSHA1(uuid.MustParse(invocationID), []byte(request.EventType))
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO task_message (id, task_id, seq, type, tool)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING
	`, messageID, taskUUID, nextSeq, request.TaskMessage.Type, pgtype.Text{String: request.TaskMessage.Tool, Valid: true}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record metadata-only task message")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit tool invocation event")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
