package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/agentpolicy"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	collaborationRequestSchemaVersion   = "mhs21.v1"
	collaborationRequestStatusAccepted  = "accepted"
	collaborationRequestModeDiscussion  = "discussion_only"
	collaborationRequestMaxPurposeBytes = 8000
)

type CollaborationRequestResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	IssueID          string  `json:"issue_id"`
	FromAgentID      string  `json:"from_agent_id"`
	ToAgentID        string  `json:"to_agent_id"`
	ParentRequestID  *string `json:"parent_request_id"`
	TriggerCommentID *string `json:"trigger_comment_id"`
	TargetTaskID     *string `json:"target_task_id"`
	Status           string  `json:"status"`
	Mode             string  `json:"mode"`
	Purpose          string  `json:"purpose"`
	MaxTurns         int32   `json:"max_turns"`
	Depth            int32   `json:"depth"`
	ExpiresAt        string  `json:"expires_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type CreateCollaborationRequestRequest struct {
	ToAgentID       string  `json:"to_agent_id"`
	Purpose         string  `json:"purpose"`
	Mode            string  `json:"mode"`
	MaxTurns        int32   `json:"max_turns"`
	TTLMinutes      int32   `json:"ttl_minutes"`
	ParentRequestID *string `json:"parent_request_id"`
}

func collaborationRequestToResponse(req db.CollaborationRequest) CollaborationRequestResponse {
	return CollaborationRequestResponse{
		ID:               uuidToString(req.ID),
		WorkspaceID:      uuidToString(req.WorkspaceID),
		IssueID:          uuidToString(req.IssueID),
		FromAgentID:      uuidToString(req.FromAgentID),
		ToAgentID:        uuidToString(req.ToAgentID),
		ParentRequestID:  uuidToPtr(req.ParentRequestID),
		TriggerCommentID: uuidToPtr(req.TriggerCommentID),
		TargetTaskID:     uuidToPtr(req.TargetTaskID),
		Status:           req.Status,
		Mode:             req.Mode,
		Purpose:          req.Purpose,
		MaxTurns:         req.MaxTurns,
		Depth:            req.Depth,
		ExpiresAt:        timestampToString(req.ExpiresAt),
		CreatedAt:        timestampToString(req.CreatedAt),
		UpdatedAt:        timestampToString(req.UpdatedAt),
	}
}

func collaborationRequestAuditComment(req db.CollaborationRequest, issue db.Issue, fromAgent, toAgent db.Agent, ttlMinutes int32) string {
	return fmt.Sprintf(`COLLABORATION_REQUEST
request_id: %s
issue_id: %s
from_agent: %s (%s)
to_agent: %s (%s)
mode: %s
purpose: %s
bounds: max_turns=%d depth=%d ttl_minutes=%d
expires_at: %s

Controller note: this audited request was accepted by the server controller. The target agent should reply on this same issue, stay in discussion/review scope, and must not change lifecycle, assignee, or create new issues unless separately authorized.`,
		uuidToString(req.ID),
		uuidToString(issue.ID),
		fromAgent.Name,
		uuidToString(fromAgent.ID),
		toAgent.Name,
		uuidToString(toAgent.ID),
		req.Mode,
		purposeForAudit(req.Purpose),
		req.MaxTurns,
		req.Depth,
		ttlMinutes,
		timestampToString(req.ExpiresAt),
	)
}

func encodeCollaborationMetadata(values map[string]any) []byte {
	metadata, err := json.Marshal(values)
	if err != nil {
		return []byte(`{}`)
	}
	return metadata
}

func (h *Handler) denyCollaborationTaskScopeIfNeeded(w http.ResponseWriter, r *http.Request, issue db.Issue, content string) bool {
	taskIDHeader := strings.TrimSpace(r.Header.Get("X-Task-ID"))
	if taskIDHeader == "" {
		return false
	}
	taskID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		return false
	}
	request, err := h.Queries.GetActiveCollaborationRequestByTargetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		writeError(w, http.StatusInternalServerError, "failed to evaluate collaboration task scope")
		return true
	}
	if uuidToString(request.IssueID) != uuidToString(issue.ID) || uuidToString(request.WorkspaceID) != uuidToString(issue.WorkspaceID) {
		writeError(w, http.StatusForbidden, "collaboration task can only write to its requested issue")
		return true
	}
	if containsAgentMention(content) {
		writeError(w, http.StatusForbidden, "collaboration task cannot include raw agent mentions")
		return true
	}
	return false
}

func (h *Handler) requireCollaborationSourceTask(w http.ResponseWriter, r *http.Request, issue db.Issue, fromAgentID pgtype.UUID) (db.AgentTaskQueue, bool) {
	taskIDHeader := strings.TrimSpace(r.Header.Get("X-Task-ID"))
	if taskIDHeader == "" {
		writeError(w, http.StatusForbidden, "collaboration requests require an agent task context")
		return db.AgentTaskQueue{}, false
	}
	taskID, ok := parseUUIDOrBadRequest(w, taskIDHeader, "X-Task-ID")
	if !ok {
		return db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || task.AgentID != fromAgentID {
		writeError(w, http.StatusForbidden, "collaboration request task does not match agent actor")
		return db.AgentTaskQueue{}, false
	}
	if !task.IssueID.Valid || uuidToString(task.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusForbidden, "collaboration request task must belong to the same issue")
		return db.AgentTaskQueue{}, false
	}
	if task.Status != "dispatched" && task.Status != "running" {
		writeError(w, http.StatusForbidden, "collaboration request task must be active")
		return db.AgentTaskQueue{}, false
	}
	return task, true
}

func targetPolicyAllowsDiscussionOnlyCollaboration(policy agentpolicy.Policy) bool {
	if !policy.IsSupervisedCollaboration() {
		return false
	}
	if !policy.DeniesRawAgentMentions() {
		return false
	}
	return policy.DeniesAnyCommand(
		agentpolicy.CommandIssueCreate,
		agentpolicy.CommandIssueUpdateStatus,
		agentpolicy.CommandIssueStatus,
		agentpolicy.CommandIssueUpdateAssignee,
		agentpolicy.CommandIssueAssign,
	)
}

func purposeForAudit(purpose string) string {
	purpose = strings.ReplaceAll(purpose, "\r\n", "\n")
	purpose = strings.ReplaceAll(purpose, "\r", "\n")
	return strings.ReplaceAll(purpose, "\n", "\n  ")
}

func (h *Handler) ListCollaborationRequests(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	items, err := h.Queries.ListCollaborationRequestsByIssue(r.Context(), db.ListCollaborationRequestsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list collaboration requests")
		return
	}

	resp := make([]CollaborationRequestResponse, len(items))
	for i, item := range items {
		resp[i] = collaborationRequestToResponse(item)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateCollaborationRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only supervised collaboration agents can create collaboration requests")
		return
	}

	fromAgentID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid agent actor")
		return
	}
	sourceTask, ok := h.requireCollaborationSourceTask(w, r, issue, fromAgentID)
	if !ok {
		return
	}
	fromAgent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          fromAgentID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "agent actor not found in workspace")
		return
	}
	policy := agentpolicy.FromRuntimeConfig(fromAgent.RuntimeConfig)
	if !policy.AllowsAuditedCollaborationRequests() {
		writeError(w, http.StatusForbidden, "agent policy does not allow audited collaboration requests")
		return
	}

	var req CreateCollaborationRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Purpose = strings.TrimSpace(req.Purpose)
	if req.Purpose == "" {
		writeError(w, http.StatusBadRequest, "purpose is required")
		return
	}
	if len(req.Purpose) > collaborationRequestMaxPurposeBytes {
		writeError(w, http.StatusBadRequest, "purpose is too large")
		return
	}
	if containsAgentMention(req.Purpose) {
		writeError(w, http.StatusBadRequest, "purpose must not contain raw agent mentions")
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = collaborationRequestModeDiscussion
	}
	if mode != collaborationRequestModeDiscussion {
		writeError(w, http.StatusBadRequest, "unsupported collaboration request mode")
		return
	}

	toAgentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ToAgentID), "to_agent_id")
	if !ok {
		return
	}
	if fromAgentID == toAgentID {
		writeError(w, http.StatusForbidden, "collaboration request cannot target the same agent")
		return
	}
	toAgent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          toAgentID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || toAgent.ArchivedAt.Valid {
		writeError(w, http.StatusNotFound, "target agent not found")
		return
	}
	if !toAgent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "target agent has no runtime")
		return
	}
	targetPolicy := agentpolicy.FromRuntimeConfig(toAgent.RuntimeConfig)
	if !targetPolicyAllowsDiscussionOnlyCollaboration(targetPolicy) {
		writeError(w, http.StatusForbidden, "target agent policy must be supervised collaboration and discussion-only")
		return
	}
	if !policy.AllowsTargetAgent(toAgent.Name, uuidToString(toAgent.ID)) {
		writeError(w, http.StatusForbidden, "target agent is not allowed by collaboration policy")
		return
	}

	maxTurns := req.MaxTurns
	if maxTurns == 0 {
		maxTurns = 2
	}
	if maxTurns < 1 {
		writeError(w, http.StatusBadRequest, "max_turns must be positive")
		return
	}
	if int(maxTurns) > policy.MaxCollaborationTurns() {
		writeError(w, http.StatusForbidden, "max_turns exceeds collaboration policy")
		return
	}

	ttlMinutes := req.TTLMinutes
	if ttlMinutes == 0 {
		ttlMinutes = int32(policy.CollaborationTTLMinutes())
	}
	if ttlMinutes < 1 {
		writeError(w, http.StatusBadRequest, "ttl_minutes must be positive")
		return
	}
	if int(ttlMinutes) > policy.CollaborationTTLMinutes() {
		writeError(w, http.StatusForbidden, "ttl_minutes exceeds collaboration policy")
		return
	}

	var parentRequestID pgtype.UUID
	depth := int32(1)
	if req.ParentRequestID != nil && strings.TrimSpace(*req.ParentRequestID) != "" {
		parentRequestID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ParentRequestID), "parent_request_id")
		if !ok {
			return
		}
		parentDepth, err := h.Queries.GetCollaborationRequestDepth(r.Context(), db.GetCollaborationRequestDepthParams{
			ID:          parentRequestID,
			WorkspaceID: issue.WorkspaceID,
			IssueID:     issue.ID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "parent_request_id is not valid for this issue")
			return
		}
		depth = parentDepth + 1
	}
	if int(depth) > policy.MaxCollaborationDepth() {
		writeError(w, http.StatusForbidden, "collaboration request depth exceeds policy")
		return
	}
	activePairCount, err := h.Queries.CountActiveCollaborationRequestsForPair(r.Context(), db.CountActiveCollaborationRequestsForPairParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		FromAgentID: fromAgent.ID,
		ToAgentID:   toAgent.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to evaluate active collaboration request policy")
		return
	}
	if activePairCount > 0 {
		writeError(w, http.StatusConflict, "active collaboration request already exists for this agent pair")
		return
	}
	if policy.PreventsCycles() {
		count, err := h.Queries.CountActiveReverseCollaborationRequests(r.Context(), db.CountActiveReverseCollaborationRequestsParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			FromAgentID: toAgent.ID,
			ToAgentID:   fromAgent.ID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to evaluate collaboration cycle policy")
			return
		}
		if count > 0 {
			writeError(w, http.StatusForbidden, "collaboration cycle is already active")
			return
		}
	}

	expiresAt := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)
	metadata := encodeCollaborationMetadata(map[string]any{
		"schema_version":       collaborationRequestSchemaVersion,
		"requested_by_task_id": uuidToString(sourceTask.ID),
	})
	created, err := h.Queries.CreateCollaborationRequest(r.Context(), db.CreateCollaborationRequestParams{
		WorkspaceID:     issue.WorkspaceID,
		IssueID:         issue.ID,
		FromAgentID:     fromAgent.ID,
		ToAgentID:       toAgent.ID,
		ParentRequestID: parentRequestID,
		Status:          collaborationRequestStatusAccepted,
		Mode:            mode,
		Purpose:         req.Purpose,
		MaxTurns:        maxTurns,
		Depth:           depth,
		ExpiresAt:       pgtype.Timestamptz{Time: expiresAt, Valid: true},
		Metadata:        metadata,
	})
	if err != nil {
		slog.Warn("create collaboration request failed", append(logger.RequestAttrs(r), "issue_id", issueID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create collaboration request")
		return
	}

	commentContent := collaborationRequestAuditComment(created, issue, fromAgent, toAgent, ttlMinutes)
	comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    fromAgent.ID,
		Content:     commentContent,
		Type:        "system",
	})
	if err != nil {
		_, _ = h.Queries.UpdateCollaborationRequestFailed(r.Context(), db.UpdateCollaborationRequestFailedParams{
			ID: created.ID,
			Metadata: encodeCollaborationMetadata(map[string]any{
				"schema_version": collaborationRequestSchemaVersion,
				"failure":        "create_audit_comment_failed",
			}),
		})
		writeError(w, http.StatusInternalServerError, "failed to create collaboration audit comment")
		return
	}
	commentResp := commentToResponse(comment, nil, nil)
	h.publish(protocol.EventCommentCreated, workspaceID, "agent", uuidToString(fromAgent.ID), map[string]any{
		"comment":             commentResp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})

	task, err := h.TaskService.EnqueueTaskForMention(r.Context(), issue, toAgent.ID, comment.ID)
	if err != nil {
		_, _ = h.Queries.UpdateCollaborationRequestFailed(r.Context(), db.UpdateCollaborationRequestFailedParams{
			ID: created.ID,
			Metadata: encodeCollaborationMetadata(map[string]any{
				"schema_version": collaborationRequestSchemaVersion,
				"failure":        "enqueue_target_task_failed",
			}),
		})
		writeError(w, http.StatusInternalServerError, "failed to enqueue collaboration target agent")
		return
	}

	queued, err := h.Queries.UpdateCollaborationRequestQueued(r.Context(), db.UpdateCollaborationRequestQueuedParams{
		ID:               created.ID,
		TriggerCommentID: comment.ID,
		TargetTaskID:     task.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finalize collaboration request")
		return
	}

	activityDetails := encodeCollaborationMetadata(map[string]any{
		"schema_version":           collaborationRequestSchemaVersion,
		"collaboration_request_id": uuidToString(queued.ID),
		"from_agent_id":            uuidToString(fromAgent.ID),
		"to_agent_id":              uuidToString(toAgent.ID),
		"target_task_id":           uuidToString(task.ID),
		"trigger_comment_id":       uuidToString(comment.ID),
		"mode":                     queued.Mode,
		"max_turns":                queued.MaxTurns,
		"depth":                    queued.Depth,
	})
	if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     fromAgent.ID,
		Action:      "collaboration_request.created",
		Details:     activityDetails,
	}); err != nil {
		slog.Warn("collaboration request activity failed", append(logger.RequestAttrs(r), "collaboration_request_id", uuidToString(queued.ID), "error", err)...)
	}

	writeJSON(w, http.StatusCreated, collaborationRequestToResponse(queued))
}
