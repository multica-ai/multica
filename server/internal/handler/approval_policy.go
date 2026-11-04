package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
)

// Standing approvals (spec 4.4). Reads are open to every member. Creating and
// revoking a policy is for owners and admins; an agent never does either. An
// agent proposes a policy instead, as a standing_approval proposal.

// ApprovalPolicyResponse is one policy as the Policies list shows it.
type ApprovalPolicyResponse struct {
	ID         string                    `json:"id"`
	Name       string                    `json:"name"`
	Kind       string                    `json:"kind"`
	Match      proposalkinds.PolicyMatch `json:"match"`
	MaxPerWeek int32                     `json:"max_per_week"`
	ValidUntil *string                   `json:"valid_until"`
	Active     bool                      `json:"active"`
	CreatedBy  string                    `json:"created_by"`
	CreatedAt  string                    `json:"created_at"`
	// WeekApprovals counts the auto-approvals of the last seven days, the number
	// the weekly cap compares with max_per_week.
	WeekApprovals int64 `json:"week_approvals"`
	// Flagged is set when a weekly review reversed an auto-approval of this policy.
	Flagged bool `json:"flagged"`
}

// ListApprovalPoliciesResponse is the Policies list.
type ListApprovalPoliciesResponse struct {
	Policies []ApprovalPolicyResponse `json:"policies"`
}

// AutoApprovalResponse is one auto-approval in a policy's history.
type AutoApprovalResponse struct {
	ProposalID   string  `json:"proposal_id"`
	SquadID      string  `json:"squad_id"`
	Kind         string  `json:"kind"`
	State        string  `json:"state"`
	Title        string  `json:"title"`
	Identifier   string  `json:"identifier"`
	DecidedAt    string  `json:"decided_at"`
	ReversedAt   *string `json:"reversed_at"`
	ReversalNote string  `json:"reversal_note"`
	// Flagged is set when a review reversed this auto-approval.
	Flagged bool `json:"flagged"`
}

// ListPolicyAutoApprovalsResponse is a policy's auto-approval history. Flagged is
// set when any entry was reversed.
type ListPolicyAutoApprovalsResponse struct {
	AutoApprovals []AutoApprovalResponse `json:"auto_approvals"`
	Flagged       bool                   `json:"flagged"`
}

// policyAutoApprovalsLimit is how many recent auto-approvals the history shows.
const policyAutoApprovalsLimit int32 = 50

func approvalPolicyResponse(v service.PolicyView) (ApprovalPolicyResponse, error) {
	var match proposalkinds.PolicyMatch
	if len(v.Policy.Match) > 0 {
		if err := json.Unmarshal(v.Policy.Match, &match); err != nil {
			return ApprovalPolicyResponse{}, err
		}
	}
	return ApprovalPolicyResponse{
		ID:            uuidToString(v.Policy.ID),
		Name:          v.Policy.Name,
		Kind:          v.Policy.Kind,
		Match:         match,
		MaxPerWeek:    v.Policy.MaxPerWeek,
		ValidUntil:    dateToPtr(v.Policy.ValidUntil),
		Active:        v.Policy.Active,
		CreatedBy:     uuidToString(v.Policy.CreatedBy),
		CreatedAt:     timestampToString(v.Policy.CreatedAt),
		WeekApprovals: v.WeekApprovals,
		Flagged:       v.Flagged,
	}, nil
}

// approvalPolicyService builds the service for one request.
func (h *Handler) approvalPolicyService() *service.ApprovalPolicyService {
	return &service.ApprovalPolicyService{
		Queries:   h.Queries,
		TxStarter: h.TxStarter,
		Kinds:     h.proposalService().Kinds,
	}
}

// ListApprovalPolicies returns the Policies list.
func (h *Handler) ListApprovalPolicies(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	views, err := h.approvalPolicyService().List(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("list approval policies failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list the standing approvals")
		return
	}
	out := ListApprovalPoliciesResponse{Policies: make([]ApprovalPolicyResponse, 0, len(views))}
	for _, v := range views {
		resp, err := approvalPolicyResponse(v)
		if err != nil {
			slog.Warn("read approval policy failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to list the standing approvals")
			return
		}
		out.Policies = append(out.Policies, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateApprovalPolicy creates a standing approval. Owners and admins only.
func (h *Handler) CreateApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actor, ok := h.governanceBossFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var spec proposalkinds.PolicySpec
	r.Body = http.MaxBytesReader(w, r.Body, governanceJSONBodyLimit)
	if !decodeInitiativeBody(w, r, &spec) {
		return
	}
	policy, err := h.approvalPolicyService().Create(r.Context(), wsUUID, actor, spec)
	var invalid *service.ApprovalPolicyInvalidError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, invalid.Message)
		return
	case errors.Is(err, service.ErrGovernanceActorNotBoss):
		writeError(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		slog.Warn("create approval policy failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create the standing approval")
		return
	}
	resp, err := approvalPolicyResponse(service.PolicyView{Policy: policy})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the standing approval")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// RevokeApprovalPolicy deactivates a standing approval in one step. Owners and
// admins only.
func (h *Handler) RevokeApprovalPolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actor, ok := h.governanceBossFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	policyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "policy id")
	if !ok {
		return
	}
	policy, err := h.approvalPolicyService().Revoke(r.Context(), wsUUID, policyID, actor)
	switch {
	case errors.Is(err, service.ErrPolicyNotFound):
		writeError(w, http.StatusNotFound, "standing approval not found")
		return
	case errors.Is(err, service.ErrGovernanceActorNotBoss):
		writeError(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		slog.Warn("revoke approval policy failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to revoke the standing approval")
		return
	}
	// The list answers counts and flags with the policy, so the revoke response
	// reads the same view as the list.
	views, err := h.approvalPolicyService().List(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke the standing approval")
		return
	}
	for _, v := range views {
		if v.Policy.ID == policy.ID {
			resp, err := approvalPolicyResponse(v)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to revoke the standing approval")
				return
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
	writeError(w, http.StatusNotFound, "standing approval not found")
}

// ListPolicyAutoApprovals returns a policy's auto-approval history, newest first.
func (h *Handler) ListPolicyAutoApprovals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	policyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "policy id")
	if !ok {
		return
	}
	rows, flagged, err := h.approvalPolicyService().AutoApprovals(r.Context(), wsUUID, policyID, policyAutoApprovalsLimit)
	if errors.Is(err, service.ErrPolicyNotFound) {
		writeError(w, http.StatusNotFound, "standing approval not found")
		return
	}
	if err != nil {
		slog.Warn("list policy auto-approvals failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list the auto-approvals")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	out := ListPolicyAutoApprovalsResponse{AutoApprovals: make([]AutoApprovalResponse, 0, len(rows)), Flagged: flagged}
	for _, a := range rows {
		out.AutoApprovals = append(out.AutoApprovals, AutoApprovalResponse{
			ProposalID:   uuidToString(a.ProposalID),
			SquadID:      uuidToString(a.SquadID),
			Kind:         a.Kind,
			State:        a.State,
			Title:        a.Title,
			Identifier:   prefix + "-" + strconv.Itoa(int(a.IssueNumber)),
			DecidedAt:    a.DecidedAt.UTC().Format(time.RFC3339),
			ReversedAt:   timePtrRFC3339(a.ReversedAt),
			ReversalNote: a.ReversalNote,
			Flagged:      a.ReversedAt != nil,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
