package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ApprovalResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	IssueID       string  `json:"issue_id"`
	RequesterType string  `json:"requester_type"`
	RequesterID   string  `json:"requester_id"`
	ApproverType  string  `json:"approver_type"`
	ApproverID    string  `json:"approver_id"`
	Status        string  `json:"status"`
	Comment       *string `json:"comment"`
	DecidedAt     *string `json:"decided_at"`
	CreatedAt     string  `json:"created_at"`
	IssueTitle    *string `json:"issue_title,omitempty"`
	IssueNumber   *int32  `json:"issue_number,omitempty"`
}

func approvalToResponse(a db.Approval) ApprovalResponse {
	var comment *string
	if a.Comment.Valid {
		comment = &a.Comment.String
	}
	var decidedAt *string
	if a.DecidedAt.Valid {
		da := timestampToString(a.DecidedAt)
		decidedAt = &da
	}

	return ApprovalResponse{
		ID:            a.ID.String(),
		WorkspaceID:   a.WorkspaceID.String(),
		IssueID:       a.IssueID.String(),
		RequesterType: a.RequesterType,
		RequesterID:   a.RequesterID.String(),
		ApproverType:  a.ApproverType,
		ApproverID:    a.ApproverID.String(),
		Status:        a.Status,
		Comment:       comment,
		DecidedAt:     decidedAt,
		CreatedAt:     timestampToString(a.CreatedAt),
	}
}

func pendingApprovalRowToResponse(row db.ListPendingApprovalsByApproverRow) ApprovalResponse {
	var comment *string
	if row.Comment.Valid {
		comment = &row.Comment.String
	}
	var decidedAt *string
	if row.DecidedAt.Valid {
		da := timestampToString(row.DecidedAt)
		decidedAt = &da
	}
	
	issueTitle := row.IssueTitle
	issueNumber := row.IssueNumber

	return ApprovalResponse{
		ID:            row.ID.String(),
		WorkspaceID:   row.WorkspaceID.String(),
		IssueID:       row.IssueID.String(),
		RequesterType: row.RequesterType,
		RequesterID:   row.RequesterID.String(),
		ApproverType:  row.ApproverType,
		ApproverID:    row.ApproverID.String(),
		Status:        row.Status,
		Comment:       comment,
		DecidedAt:     decidedAt,
		CreatedAt:     timestampToString(row.CreatedAt),
		IssueTitle:    &issueTitle,
		IssueNumber:   &issueNumber,
	}
}

func (h *Handler) ListApprovalsByIssue(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issueId")
	if !ok {
		return
	}

	// Verify issue exists in workspace
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	approvals, err := h.Queries.ListApprovalsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list approvals")
		return
	}

	res := make([]ApprovalResponse, len(approvals))
	for i, a := range approvals {
		res[i] = approvalToResponse(a)
	}

	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	actorType, actorIDStr := h.resolveActor(r, requestUserID(r), wsIDStr)
	actorID, ok := parseUUIDOrBadRequest(w, actorIDStr, "actor_id")
	if !ok {
		return
	}

	approvals, err := h.Queries.ListPendingApprovalsByApprover(r.Context(), db.ListPendingApprovalsByApproverParams{
		WorkspaceID:  wsID,
		ApproverType: actorType,
		ApproverID:   actorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending approvals")
		return
	}

	res := make([]ApprovalResponse, len(approvals))
	for i, a := range approvals {
		res[i] = pendingApprovalRowToResponse(a)
	}

	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) GetPendingApprovalCount(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	actorType, actorIDStr := h.resolveActor(r, requestUserID(r), wsIDStr)
	actorID, ok := parseUUIDOrBadRequest(w, actorIDStr, "actor_id")
	if !ok {
		return
	}

	count, err := h.Queries.CountPendingApprovalsByApprover(r.Context(), db.CountPendingApprovalsByApproverParams{
		WorkspaceID:  wsID,
		ApproverType: actorType,
		ApproverID:   actorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count pending approvals")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *Handler) CreateApproval(w http.ResponseWriter, r *http.Request) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issueId")
	if !ok {
		return
	}

	var body struct {
		ApproverType string `json:"approver_type"`
		ApproverID   string `json:"approver_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	approverID, ok := parseUUIDOrBadRequest(w, body.ApproverID, "approver_id")
	if !ok {
		return
	}

	actorType, actorIDStr := h.resolveActor(r, requestUserID(r), wsIDStr)
	actorID, ok := parseUUIDOrBadRequest(w, actorIDStr, "actor_id")
	if !ok {
		return
	}

	// Verify issue exists in workspace
	_, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	a, err := h.Queries.CreateApproval(r.Context(), db.CreateApprovalParams{
		WorkspaceID:   wsID,
		IssueID:       issueID,
		RequesterType: actorType,
		RequesterID:   actorID,
		ApproverType:  body.ApproverType,
		ApproverID:    approverID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create approval")
		return
	}

	res := approvalToResponse(a)
	h.publish(protocol.EventApprovalRequested, wsIDStr, "system", "", res)

	// Create inbox notification for the approver
	_, _ = h.Queries.CreateInboxItem(r.Context(), db.CreateInboxItemParams{
		WorkspaceID:   wsID,
		RecipientType: body.ApproverType,
		RecipientID:   approverID,
		Type:          "approval_requested",
		Severity:      "info",
		IssueID:       pgtype.UUID{Bytes: issueID.Bytes, Valid: true},
		Title:         "Approval Requested",
		Body:          pgtype.Text{String: "You have been requested to review an issue.", Valid: true},
		ActorType:     pgtype.Text{String: actorType, Valid: true},
		ActorID:       pgtype.UUID{Bytes: actorID.Bytes, Valid: true},
	})

	writeJSON(w, http.StatusCreated, res)
}

func (h *Handler) ApproveApproval(w http.ResponseWriter, r *http.Request) {
	h.handleApprovalDecision(w, r, "approve")
}

func (h *Handler) RejectApproval(w http.ResponseWriter, r *http.Request) {
	h.handleApprovalDecision(w, r, "reject")
}

func (h *Handler) handleApprovalDecision(w http.ResponseWriter, r *http.Request, action string) {
	wsIDStr := h.resolveWorkspaceID(r)
	wsID, ok := parseUUIDOrBadRequest(w, wsIDStr, "workspace_id")
	if !ok {
		return
	}

	approvalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "approvalId"), "approvalId")
	if !ok {
		return
	}

	var body struct {
		Comment *string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var comment pgtype.Text
	if body.Comment != nil {
		comment = pgtype.Text{String: *body.Comment, Valid: true}
	}

	actorType, actorIDStr := h.resolveActor(r, requestUserID(r), wsIDStr)
	actorID, ok := parseUUIDOrBadRequest(w, actorIDStr, "actor_id")
	if !ok {
		return
	}

	// Fetch approval first to verify permissions
	a, err := h.Queries.GetApproval(r.Context(), approvalID)
	if err != nil {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}

	if a.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if a.ApproverType != actorType || a.ApproverID != actorID {
		writeError(w, http.StatusForbidden, "not authorized to decide this approval")
		return
	}

	var updated db.Approval
	var eventType string

	if action == "approve" {
		updated, err = h.Queries.ApproveApproval(r.Context(), db.ApproveApprovalParams{
			ID:      approvalID,
			Comment: comment,
		})
		eventType = protocol.EventApprovalApproved
	} else {
		updated, err = h.Queries.RejectApproval(r.Context(), db.RejectApprovalParams{
			ID:      approvalID,
			Comment: comment,
		})
		eventType = protocol.EventApprovalRejected
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to process approval decision")
		return
	}

	res := approvalToResponse(updated)
	h.publish(eventType, wsIDStr, actorType, actorIDStr, res)

	// Create inbox notification for the requester
	title := "Approval Approved"
	if action == "reject" {
		title = "Approval Rejected"
	}
	_, _ = h.Queries.CreateInboxItem(r.Context(), db.CreateInboxItemParams{
		WorkspaceID:   wsID,
		RecipientType: a.RequesterType,
		RecipientID:   a.RequesterID,
		Type:          "approval_" + action,
		Severity:      "info",
		IssueID:       pgtype.UUID{Bytes: a.IssueID.Bytes, Valid: true},
		Title:         title,
		Body:          pgtype.Text{String: "Your approval request has been " + action + "ed.", Valid: true},
		ActorType:     pgtype.Text{String: actorType, Valid: true},
		ActorID:       pgtype.UUID{Bytes: actorID.Bytes, Valid: true},
	})

	writeJSON(w, http.StatusOK, res)
}
