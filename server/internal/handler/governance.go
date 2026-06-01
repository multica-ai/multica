package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/governance"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultGovernancePageLimit int32 = 50

type GovernancePolicyResponse struct {
	WorkspaceID     string                        `json:"workspace_id"`
	ActorUserID     string                        `json:"actor_user_id"`
	WorkspaceRole   string                        `json:"workspace_role"`
	Roles           []governance.RoleTemplate     `json:"roles"`
	Actions         []governance.Action           `json:"actions"`
	Decisions       []governance.Decision         `json:"decisions"`
	ApprovalSources []GovernanceApprovalResponse  `json:"approval_sources"`
}

type GovernanceApprovalResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	ActionID           string  `json:"action_id"`
	TargetType         string  `json:"target_type"`
	TargetID           string  `json:"target_id"`
	IssueID            *string `json:"issue_id"`
	ApprovalSourceType string  `json:"approval_source_type"`
	ApprovalSourceID   *string `json:"approval_source_id"`
	ApprovedByType     string  `json:"approved_by_type"`
	ApprovedByID       string  `json:"approved_by_id"`
	Reason             string  `json:"reason"`
	ExpiresAt          *string `json:"expires_at"`
	ConsumedAt         *string `json:"consumed_at"`
	CreatedAt          string  `json:"created_at"`
}

type GovernanceAuditResponse struct {
	ID                 string         `json:"id"`
	WorkspaceID        string         `json:"workspace_id"`
	ActionID           string         `json:"action_id"`
	TargetType         string         `json:"target_type"`
	TargetID           string         `json:"target_id"`
	ActorType          string         `json:"actor_type"`
	ActorID            string         `json:"actor_id"`
	BeforeSummary      map[string]any `json:"before_summary"`
	AfterSummary       map[string]any `json:"after_summary"`
	IssueID            *string        `json:"issue_id"`
	ApprovalID         *string        `json:"approval_id"`
	ApprovalSourceType *string        `json:"approval_source_type"`
	ApprovalSourceID   *string        `json:"approval_source_id"`
	CreatedAt          string         `json:"created_at"`
}

type CreateGovernanceApprovalRequest struct {
	ActionID           string  `json:"action_id"`
	TargetType         string  `json:"target_type"`
	TargetID           string  `json:"target_id"`
	IssueID            *string `json:"issue_id"`
	ApprovalSourceType string  `json:"approval_source_type"`
	ApprovalSourceID   *string `json:"approval_source_id"`
	Reason             string  `json:"reason"`
	ExpiresAt          *string `json:"expires_at"`
}

type governanceApprovalContext struct {
	Action   governance.Action
	Decision governance.Decision
	Approval db.GovernanceApproval
}

func (h *Handler) GetGovernancePolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	approvals, err := h.Queries.ListGovernanceApprovals(r.Context(), db.ListGovernanceApprovalsParams{
		WorkspaceID: member.WorkspaceID,
		Limit:       defaultGovernancePageLimit,
		Offset:      0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list governance approvals")
		return
	}

	ctx := governance.Context{WorkspaceRole: member.Role, Approved: false}
	writeJSON(w, http.StatusOK, GovernancePolicyResponse{
		WorkspaceID:     uuidToString(member.WorkspaceID),
		ActorUserID:     uuidToString(member.UserID),
		WorkspaceRole:   member.Role,
		Roles:           governance.RoleTemplates(),
		Actions:         governance.Actions(),
		Decisions:       governance.EvaluateAll(ctx),
		ApprovalSources: governanceApprovalResponses(approvals),
	})
}

func (h *Handler) CreateGovernanceApproval(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "governance approvals require workspace owner or admin")
		return
	}

	var req CreateGovernanceApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	action, ok := governance.ActionByID(req.ActionID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown governance action")
		return
	}
	if action.Strategy != governance.StrategyApprovalRequired {
		writeError(w, http.StatusBadRequest, "only approval-required actions can be approved")
		return
	}
	if req.TargetType == "" {
		writeError(w, http.StatusBadRequest, "target_type is required")
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, req.TargetID, "target_id")
	if !ok {
		return
	}
	issueID, ok := optionalUUIDFromString(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	sourceID, ok := optionalUUIDFromString(w, req.ApprovalSourceID, "approval_source_id")
	if !ok {
		return
	}
	if req.ApprovalSourceType == "" {
		writeError(w, http.StatusBadRequest, "approval_source_type is required")
		return
	}
	if req.ApprovalSourceType != "issue_comment" && req.ApprovalSourceType != "issue_metadata" && req.ApprovalSourceType != "manual" {
		writeError(w, http.StatusBadRequest, "approval_source_type must be issue_comment, issue_metadata, or manual")
		return
	}
	if !h.validateGovernanceApprovalSource(w, r, member.WorkspaceID, req.ApprovalSourceType, issueID, sourceID) {
		return
	}
	expiresAt, ok := optionalTimestampFromString(w, req.ExpiresAt, "expires_at")
	if !ok {
		return
	}

	approval, err := h.Queries.CreateGovernanceApproval(r.Context(), db.CreateGovernanceApprovalParams{
		WorkspaceID:        member.WorkspaceID,
		ActionID:           action.ID,
		TargetType:         req.TargetType,
		TargetID:           targetID,
		IssueID:            issueID,
		ApprovalSourceType: req.ApprovalSourceType,
		ApprovalSourceID:   sourceID,
		ApprovedByType:     "member",
		ApprovedByID:       member.UserID,
		Reason:             req.Reason,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create governance approval")
		return
	}
	writeJSON(w, http.StatusCreated, governanceApprovalResponse(approval))
}

func (h *Handler) ListGovernanceApprovals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	limit, offset := parseGovernancePagination(r)
	approvals, err := h.Queries.ListGovernanceApprovals(r.Context(), db.ListGovernanceApprovalsParams{
		WorkspaceID: member.WorkspaceID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list governance approvals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": governanceApprovalResponses(approvals)})
}

func (h *Handler) ListGovernanceAudits(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	limit, offset := parseGovernancePagination(r)
	audits, err := h.Queries.ListGovernanceAudits(r.Context(), db.ListGovernanceAuditsParams{
		WorkspaceID: member.WorkspaceID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list governance audits")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audits": governanceAuditResponses(audits)})
}

func (h *Handler) requireGovernanceApproval(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, actionID, targetType string, targetID pgtype.UUID) (governanceApprovalContext, bool) {
	action, ok := governance.ActionByID(actionID)
	if !ok {
		writeError(w, http.StatusInternalServerError, "unknown governance action")
		return governanceApprovalContext{}, false
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(workspaceID), "workspace not found")
	if !ok {
		return governanceApprovalContext{}, false
	}
	approval, err := h.Queries.FindActiveGovernanceApproval(r.Context(), db.FindActiveGovernanceApprovalParams{
		WorkspaceID: workspaceID,
		ActionID:    actionID,
		TargetType:  targetType,
		TargetID:    targetID,
	})
	approved := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to evaluate governance approval")
		return governanceApprovalContext{}, false
	}
	decision := governance.Evaluate(action, governance.Context{WorkspaceRole: member.Role, Approved: approved})
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error":             "governance action denied",
			"action_id":         decision.ActionID,
			"reason":            decision.Reason,
			"requires_approval": decision.RequiresApproval,
		})
		return governanceApprovalContext{}, false
	}
	return governanceApprovalContext{Action: action, Decision: decision, Approval: approval}, true
}

func (h *Handler) recordGovernanceAudit(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, ctx governanceApprovalContext, targetType string, targetID pgtype.UUID, actorType string, actorID pgtype.UUID, beforeSummary, afterSummary map[string]any) bool {
	return h.recordGovernanceAuditWithQueries(w, r, h.Queries, workspaceID, ctx, targetType, targetID, actorType, actorID, beforeSummary, afterSummary)
}

func (h *Handler) recordGovernanceAuditWithQueries(w http.ResponseWriter, r *http.Request, q *db.Queries, workspaceID pgtype.UUID, ctx governanceApprovalContext, targetType string, targetID pgtype.UUID, actorType string, actorID pgtype.UUID, beforeSummary, afterSummary map[string]any) bool {
	beforeRaw, err := json.Marshal(beforeSummary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode governance audit")
		return false
	}
	afterRaw, err := json.Marshal(afterSummary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode governance audit")
		return false
	}
	approval, err := q.ClaimActiveGovernanceApproval(r.Context(), db.ClaimActiveGovernanceApprovalParams{
		WorkspaceID: workspaceID,
		ActionID:    ctx.Action.ID,
		TargetType:  targetType,
		TargetID:    targetID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":             "governance action denied",
				"action_id":         ctx.Action.ID,
				"reason":            "approval is missing, expired, or already consumed",
				"requires_approval": ctx.Decision.RequiresApproval,
			})
			return false
		}
		writeError(w, http.StatusInternalServerError, "failed to consume governance approval")
		return false
	}
	_, err = q.CreateGovernanceAudit(r.Context(), db.CreateGovernanceAuditParams{
		WorkspaceID:        workspaceID,
		ActionID:           ctx.Action.ID,
		TargetType:         targetType,
		TargetID:           targetID,
		ActorType:          actorType,
		ActorID:            actorID,
		BeforeSummary:      beforeRaw,
		AfterSummary:       afterRaw,
		IssueID:            approval.IssueID,
		ApprovalID:         approval.ID,
		ApprovalSourceType: strToText(approval.ApprovalSourceType),
		ApprovalSourceID:   approval.ApprovalSourceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create governance audit")
		return false
	}
	return true
}

func (h *Handler) validateGovernanceApprovalSource(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, sourceType string, issueID, sourceID pgtype.UUID) bool {
	switch sourceType {
	case "manual":
		return true
	case "issue_comment":
		if !sourceID.Valid {
			writeError(w, http.StatusBadRequest, "approval_source_id is required for issue_comment approvals")
			return false
		}
		comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          sourceID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "approval_source_id must reference a comment in this workspace")
				return false
			}
			writeError(w, http.StatusInternalServerError, "failed to validate approval source")
			return false
		}
		if issueID.Valid && uuidToString(comment.IssueID) != uuidToString(issueID) {
			writeError(w, http.StatusBadRequest, "issue_id must match the approval comment issue")
			return false
		}
		return true
	case "issue_metadata":
		if !issueID.Valid {
			writeError(w, http.StatusBadRequest, "issue_id is required for issue_metadata approvals")
			return false
		}
		if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueID,
			WorkspaceID: workspaceID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "issue_id must reference an issue in this workspace")
				return false
			}
			writeError(w, http.StatusInternalServerError, "failed to validate approval source")
			return false
		}
		return true
	default:
		writeError(w, http.StatusBadRequest, "approval_source_type must be issue_comment, issue_metadata, or manual")
		return false
	}
}

func governanceApprovalResponse(a db.GovernanceApproval) GovernanceApprovalResponse {
	return GovernanceApprovalResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		ActionID:           a.ActionID,
		TargetType:         a.TargetType,
		TargetID:           uuidToString(a.TargetID),
		IssueID:            uuidToPtr(a.IssueID),
		ApprovalSourceType: a.ApprovalSourceType,
		ApprovalSourceID:   uuidToPtr(a.ApprovalSourceID),
		ApprovedByType:     a.ApprovedByType,
		ApprovedByID:       uuidToString(a.ApprovedByID),
		Reason:             a.Reason,
		ExpiresAt:          timestampToPtr(a.ExpiresAt),
		ConsumedAt:         timestampToPtr(a.ConsumedAt),
		CreatedAt:          timestampToString(a.CreatedAt),
	}
}

func governanceApprovalResponses(rows []db.GovernanceApproval) []GovernanceApprovalResponse {
	out := make([]GovernanceApprovalResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, governanceApprovalResponse(row))
	}
	return out
}

func governanceAuditResponse(a db.GovernanceAudit) GovernanceAuditResponse {
	return GovernanceAuditResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		ActionID:           a.ActionID,
		TargetType:         a.TargetType,
		TargetID:           uuidToString(a.TargetID),
		ActorType:          a.ActorType,
		ActorID:            uuidToString(a.ActorID),
		BeforeSummary:      parseObjectJSON(a.BeforeSummary),
		AfterSummary:       parseObjectJSON(a.AfterSummary),
		IssueID:            uuidToPtr(a.IssueID),
		ApprovalID:         uuidToPtr(a.ApprovalID),
		ApprovalSourceType: textToPtr(a.ApprovalSourceType),
		ApprovalSourceID:   uuidToPtr(a.ApprovalSourceID),
		CreatedAt:          timestampToString(a.CreatedAt),
	}
}

func governanceAuditResponses(rows []db.GovernanceAudit) []GovernanceAuditResponse {
	out := make([]GovernanceAuditResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, governanceAuditResponse(row))
	}
	return out
}

func optionalUUIDFromString(w http.ResponseWriter, raw *string, field string) (pgtype.UUID, bool) {
	if raw == nil || *raw == "" {
		return pgtype.UUID{}, true
	}
	u, ok := parseUUIDOrBadRequest(w, *raw, field)
	return u, ok
}

func optionalTimestampFromString(w http.ResponseWriter, raw *string, field string) (pgtype.Timestamptz, bool) {
	if raw == nil || *raw == "" {
		return pgtype.Timestamptz{}, true
	}
	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return pgtype.Timestamptz{}, false
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, true
}

func parseGovernancePagination(r *http.Request) (int32, int32) {
	limit := defaultGovernancePageLimit
	offset := int32(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
			limit = int32(v)
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			offset = int32(v)
		}
	}
	return limit, offset
}

func parseObjectJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
