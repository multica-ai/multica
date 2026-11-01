package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Field limits for a proposal body. The boss reads these, so they are bounded
// for the reader as much as for storage.
const (
	proposalMaxTitleLen = 200
	proposalMaxTextLen  = 4000
	proposalMaxListLen  = 20
)

// ── Response types ──────────────────────────────────────────────────────────

type ProposalHandoffResponse struct {
	ProjectID      string `json:"project_id"`
	KickoffIssueID string `json:"kickoff_issue_id"`
}

type ProposalResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	SquadID         string  `json:"squad_id"`
	IssueID         string  `json:"issue_id"`
	IssueIdentifier string  `json:"issue_identifier"`
	IssueStatus     string  `json:"issue_status"`
	Title           string  `json:"title"`
	HandoffSquadID  *string `json:"handoff_squad_id"`
	Urgent          bool    `json:"urgent"`
	UrgentReason    string  `json:"urgent_reason"`
	State           string  `json:"state"`
	CreatedByType   string  `json:"created_by_type"`
	CreatedByID     string  `json:"created_by_id"`
	DecidedBy       *string `json:"decided_by"`
	DecidedAt       *string `json:"decided_at"`
	DecisionNote    string  `json:"decision_note"`
	CreatedAt       string  `json:"created_at"`
	// Handoff is set only on the response to an approval that handed off to a
	// squad. The ids are not stored on the proposal.
	Handoff *ProposalHandoffResponse `json:"handoff,omitempty"`
}

func proposalToResponse(p db.Proposal, issueTitle, issueStatus, issuePrefix string, issueNumber int32) ProposalResponse {
	return ProposalResponse{
		ID:              uuidToString(p.ID),
		WorkspaceID:     uuidToString(p.WorkspaceID),
		SquadID:         uuidToString(p.SquadID),
		IssueID:         uuidToString(p.IssueID),
		IssueIdentifier: issuePrefix + "-" + strconv.Itoa(int(issueNumber)),
		IssueStatus:     issueStatus,
		Title:           issueTitle,
		HandoffSquadID:  uuidToPtr(p.HandoffSquadID),
		Urgent:          p.Urgent,
		UrgentReason:    p.UrgentReason,
		State:           p.State,
		CreatedByType:   p.CreatedByType,
		CreatedByID:     uuidToString(p.CreatedByID),
		DecidedBy:       uuidToPtr(p.DecidedBy),
		DecidedAt:       timestampToPtr(p.DecidedAt),
		DecisionNote:    p.DecisionNote,
		CreatedAt:       timestampToString(p.CreatedAt),
	}
}

// ── Create ──────────────────────────────────────────────────────────────────

type CreateProposalRequest struct {
	SquadID        string   `json:"squad_id"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence"`
	Recommendation string   `json:"recommendation"`
	Options        []string `json:"options"`
	CostRisk       string   `json:"cost_risk"`
	Ask            string   `json:"ask"`
	HandoffSquadID *string  `json:"handoff_squad_id"`
	Urgent         bool     `json:"urgent"`
	UrgentReason   string   `json:"urgent_reason"`
}

// cleanProposalText trims a proposal text field and strips the bytes
// PostgreSQL TEXT rejects.
func cleanProposalText(s string) string {
	return strings.TrimSpace(sanitizeNullBytes(s))
}

func cleanProposalList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = cleanProposalText(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// validateProposalRequest normalizes the request in place and returns the first
// problem found, written for the filing agent.
func validateProposalRequest(req *CreateProposalRequest) string {
	req.Title = cleanProposalText(req.Title)
	req.Summary = cleanProposalText(req.Summary)
	req.Recommendation = cleanProposalText(req.Recommendation)
	req.CostRisk = cleanProposalText(req.CostRisk)
	req.Ask = cleanProposalText(req.Ask)
	req.UrgentReason = cleanProposalText(req.UrgentReason)
	req.Evidence = cleanProposalList(req.Evidence)
	req.Options = cleanProposalList(req.Options)

	required := []struct{ name, value string }{
		{"title", req.Title}, {"summary", req.Summary}, {"recommendation", req.Recommendation},
		{"cost_risk", req.CostRisk}, {"ask", req.Ask},
	}
	for _, f := range required {
		if f.value == "" {
			return f.name + " is required"
		}
	}
	if len(req.Evidence) == 0 {
		return "evidence is required: give at least one link"
	}
	if len(req.Options) == 0 {
		return "options is required: name at least one option considered"
	}
	if len(req.Title) > proposalMaxTitleLen {
		return "title must be at most " + strconv.Itoa(proposalMaxTitleLen) + " characters"
	}
	texts := append([]string{req.Summary, req.Recommendation, req.CostRisk, req.Ask, req.UrgentReason}, req.Evidence...)
	texts = append(texts, req.Options...)
	for _, t := range texts {
		if len(t) > proposalMaxTextLen {
			return "each field must be at most " + strconv.Itoa(proposalMaxTextLen) + " characters"
		}
	}
	if len(req.Evidence) > proposalMaxListLen || len(req.Options) > proposalMaxListLen {
		return "evidence and options may each have at most " + strconv.Itoa(proposalMaxListLen) + " entries"
	}
	if req.Urgent && req.UrgentReason == "" {
		return "urgent_reason is required when urgent"
	}
	if !req.Urgent && req.UrgentReason != "" {
		return "urgent_reason is only allowed when urgent"
	}
	return ""
}

// mayFileProposalForSquad reports whether the actor may file a proposal for the
// squad: an agent running a task that is a live member of the squad, or a
// workspace member who belongs to the squad or is an owner or admin.
func (h *Handler) mayFileProposalForSquad(w http.ResponseWriter, r *http.Request, workspaceID, userID, actorType, actorID string, squadID pgtype.UUID) bool {
	if actorType == "agent" {
		agentUUID, err := util.ParseUUID(actorID)
		if err != nil {
			writeError(w, http.StatusForbidden, "only a member of the squad may file a proposal for it")
			return false
		}
		task, ok := h.taskFromRequestHeader(r)
		if !ok || task.AgentID != agentUUID || isTerminalTaskStatus(task.Status) {
			writeError(w, http.StatusForbidden, "a proposal can only be filed from inside a running task")
			return false
		}
		wsUUID, _ := util.ParseUUID(workspaceID)
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: wsUUID})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusForbidden, "only a member of the squad may file a proposal for it")
			return false
		}
		isMember, err := h.Queries.IsSquadMember(r.Context(), db.IsSquadMemberParams{SquadID: squadID, MemberType: "agent", MemberID: agentUUID})
		if err != nil || !isMember {
			writeError(w, http.StatusForbidden, "only a member of the squad may file a proposal for it")
			return false
		}
		return true
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	isMember, err := h.Queries.IsSquadMember(r.Context(), db.IsSquadMemberParams{SquadID: squadID, MemberType: "member", MemberID: member.UserID})
	if err != nil || !isMember {
		writeError(w, http.StatusForbidden, "only a member of the squad may file a proposal for it")
		return false
	}
	return true
}

func (h *Handler) proposalService() *service.ProposalService {
	return service.NewProposalService(h.IssueService)
}

// CreateProposal files a squad's proposal and its backing issue.
func (h *Handler) CreateProposal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	squadUUID, ok := parseUUIDOrBadRequest(w, req.SquadID, "squad_id")
	if !ok {
		return
	}
	var handoffUUID pgtype.UUID
	if req.HandoffSquadID != nil && *req.HandoffSquadID != "" {
		if handoffUUID, ok = parseUUIDOrBadRequest(w, *req.HandoffSquadID, "handoff_squad_id"); !ok {
			return
		}
	}
	if msg := validateProposalRequest(&req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.mayFileProposalForSquad(w, r, workspaceID, userID, actorType, actorID, squadUUID) {
		return
	}
	creatorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}

	res, err := h.proposalService().Create(r.Context(), service.ProposalCreateParams{
		WorkspaceID:    wsUUID,
		SquadID:        squadUUID,
		HandoffSquadID: handoffUUID,
		Content: service.ProposalContent{
			Title: req.Title, Summary: req.Summary, Evidence: req.Evidence,
			Recommendation: req.Recommendation, Options: req.Options,
			CostRisk: req.CostRisk, Ask: req.Ask,
		},
		Urgent:       req.Urgent,
		UrgentReason: req.UrgentReason,
		CreatorType:  actorType,
		CreatorID:    creatorUUID,
	})
	var limitErr *service.ProposalLimitError
	switch {
	case errors.As(err, &limitErr):
		writeErrorCode(w, http.StatusConflict, "proposal_limit", limitErr.Message)
		return
	case errors.Is(err, service.ErrProposalSquadNotFound):
		writeError(w, http.StatusNotFound, "squad not found")
		return
	case errors.Is(err, service.ErrProposalHandoffSquadNotFound):
		writeError(w, http.StatusBadRequest, "handoff squad not found in this workspace")
		return
	case errors.Is(err, service.ErrProposalNoBoss):
		writeError(w, http.StatusConflict, "this workspace has no owner to receive an urgent proposal; file it without urgent")
		return
	case writeIssueLimitReached(w, err):
		return
	case err != nil:
		slog.Warn("create proposal failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create proposal")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	issueResp := issueToResponse(res.Issue, prefix)
	h.fillStatusCategory(r.Context(), wsUUID, &issueResp)
	resp := proposalToResponse(res.Proposal, res.Issue.Title, res.Issue.Status, prefix, res.Issue.Number)

	// The ordinary issue:created event is what raises the boss's issue_assigned
	// inbox item for an urgent proposal; an unassigned one raises nothing.
	h.publish(protocol.EventIssueCreated, workspaceID, actorType, actorID, map[string]any{"issue": issueResp})
	h.publish(protocol.EventProposalCreated, workspaceID, actorType, actorID, map[string]any{"proposal": resp})
	slog.Info("proposal created", append(logger.RequestAttrs(r),
		"proposal_id", resp.ID, "squad_id", resp.SquadID, "urgent", resp.Urgent)...)
	writeJSON(w, http.StatusCreated, resp)
}

// ── Read ────────────────────────────────────────────────────────────────────

// ListProposals returns the workspace's proposals, newest first.
func (h *Handler) ListProposals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	params := db.ListProposalsParams{WorkspaceID: wsUUID}
	if state := r.URL.Query().Get("state"); state != "" {
		switch state {
		case service.ProposalStatePending, service.ProposalStateApproved, service.ProposalStateRejected:
			params.State = pgtype.Text{String: state, Valid: true}
		default:
			writeError(w, http.StatusBadRequest, "invalid state")
			return
		}
	}
	if squadID := r.URL.Query().Get("squad_id"); squadID != "" {
		if params.SquadID, ok = parseUUIDOrBadRequest(w, squadID, "squad_id"); !ok {
			return
		}
	}
	rows, err := h.Queries.ListProposals(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list proposals")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	resp := make([]ProposalResponse, len(rows))
	for i, row := range rows {
		resp[i] = proposalToResponse(row.Proposal, row.IssueTitle, row.IssueStatus, prefix, row.IssueNumber)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetProposal returns one proposal with its backing issue id.
func (h *Handler) GetProposal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "proposal id")
	if !ok {
		return
	}
	row, err := h.Queries.GetProposalWithIssue(r.Context(), db.GetProposalWithIssueParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "proposal not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load proposal")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	writeJSON(w, http.StatusOK, proposalToResponse(row.Proposal, row.IssueTitle, row.IssueStatus, prefix, row.IssueNumber))
}

// ── Decide ──────────────────────────────────────────────────────────────────

type DecideProposalRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// DecideProposal approves or rejects a pending proposal. Only an owner or admin
// member may decide: an agent actor is refused, including an agent acting with
// a member's credentials, because a decision spends the boss's authority.
func (h *Handler) DecideProposal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "only an owner or admin may decide a proposal; agents cannot")
		return
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType != "member" {
		writeError(w, http.StatusForbidden, "only an owner or admin may decide a proposal; agents cannot")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only an owner or admin may decide a proposal")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	proposalID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "proposal id")
	if !ok {
		return
	}

	var req DecideProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Decision != service.ProposalDecisionApprove && req.Decision != service.ProposalDecisionReject {
		writeError(w, http.StatusBadRequest, "decision must be 'approve' or 'reject'")
		return
	}
	note := cleanProposalText(req.Note)
	if len(note) > proposalMaxTextLen {
		writeError(w, http.StatusBadRequest, "note must be at most "+strconv.Itoa(proposalMaxTextLen)+" characters")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	fillKickoff := h.newStatusCategoryFiller(r.Context(), wsUUID)
	res, err := h.proposalService().Decide(r.Context(), service.ProposalDecideParams{
		WorkspaceID: wsUUID,
		ProposalID:  proposalID,
		DeciderID:   member.UserID,
		Decision:    req.Decision,
		Note:        note,
		Kickoff: service.IssueCreateOpts{
			ActorID:  userID,
			Platform: func() string { p, _, _ := middleware.ClientMetadataFromContext(r.Context()); return p }(),
			BroadcastPayload: func(issue db.Issue, atts []db.Attachment, labels []db.IssueLabel) map[string]any {
				payload := issueToResponse(issue, prefix)
				fillKickoff(&payload)
				labelResponses := labelsToResponse(labels)
				payload.Labels = &labelResponses
				return map[string]any{"issue": payload}
			},
		},
	})
	switch {
	case errors.Is(err, service.ErrProposalNotFound):
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	case errors.Is(err, service.ErrProposalAlreadyDecided):
		writeErrorCode(w, http.StatusConflict, "proposal_decided", "this proposal has already been decided")
		return
	case errors.Is(err, service.ErrProposalHandoffSquadNotFound):
		writeErrorCode(w, http.StatusConflict, "handoff_squad_unavailable", "the handoff squad no longer exists or is archived; approve without it or ask the squad to refile")
		return
	case writeIssueLimitReached(w, err):
		return
	case err != nil:
		slog.Warn("decide proposal failed", append(logger.RequestAttrs(r), "error", err, "proposal_id", chi.URLParam(r, "id"))...)
		writeError(w, http.StatusInternalServerError, "failed to decide proposal")
		return
	}

	h.publishProposalDecision(r.Context(), userID, prefix, wsUUID, res)

	issueTitle := res.Issue.Title
	resp := proposalToResponse(res.Proposal, issueTitle, res.Issue.Status, prefix, res.Issue.Number)
	if res.Project != nil && res.Kickoff != nil {
		resp.Handoff = &ProposalHandoffResponse{
			ProjectID:      uuidToString(res.Project.ID),
			KickoffIssueID: uuidToString(res.Kickoff.ID),
		}
	}
	h.publish(protocol.EventProposalDecided, workspaceID, "member", userID, map[string]any{"proposal": resp})
	writeJSON(w, http.StatusOK, resp)
}

// publishProposalDecision publishes the events for a committed decision and
// wakes the proposing squad's leader after a rejection.
func (h *Handler) publishProposalDecision(ctx context.Context, userID, prefix string, wsUUID pgtype.UUID, res service.ProposalDecideResult) {
	workspaceID := uuidToString(wsUUID)

	issueResp := issueToResponse(res.Issue, prefix)
	h.fillStatusCategory(ctx, wsUUID, &issueResp)
	h.publish(protocol.EventIssueUpdated, workspaceID, "member", userID, map[string]any{
		"issue":          issueResp,
		"status_changed": res.PrevIssueStatus != res.Issue.Status,
		"prev_status":    res.PrevIssueStatus,
		"creator_type":   res.Issue.CreatorType,
		"creator_id":     uuidToString(res.Issue.CreatorID),
	})

	if res.Project != nil {
		h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"project": projectToResponse(*res.Project)})
	}

	if res.Comment != nil {
		comment := *res.Comment
		resp := commentToResponse(comment, nil, nil)
		h.publish(protocol.EventCommentCreated, workspaceID, "member", userID, map[string]any{
			"comment":             resp,
			"issue_title":         res.Issue.Title,
			"issue_assignee_type": textToPtr(res.Issue.AssigneeType),
			"issue_assignee_id":   uuidToPtr(res.Issue.AssigneeID),
			"issue_status":        res.Issue.Status,
			"issue_revision":      res.Issue.Revision,
		})
		// The note mentions the squad's leader; the ordinary mention path
		// enqueues the run, attributed to the deciding member.
		h.triggerTasksForComment(ctx, res.Issue, comment, nil, "member", userID, userID, nil, commentSteerRequest{})
	}
}
