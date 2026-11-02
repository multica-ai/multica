package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	// Revision starts at 1 and goes up each time the squad resubmits after
	// the boss asked for changes.
	Revision int32 `json:"revision"`
	// ExpiresAt is the end of the decision window: a pending proposal with no
	// decision by then expires. It is kept after a decision, as the record of
	// the window.
	ExpiresAt *string `json:"expires_at"`
	// SnoozedUntil is set while the boss has put a pending proposal aside. A
	// time in the past means the snooze has ended.
	SnoozedUntil *string `json:"snoozed_until"`
	// Handoff is set only on the response to an approval that handed off to a
	// squad. The ids are not stored on the proposal.
	Handoff *ProposalHandoffResponse `json:"handoff,omitempty"`
}

// ProposalCreateResponse is the response to filing a proposal: the proposal,
// and the earlier proposals and decisions that look related to it.
type ProposalCreateResponse struct {
	ProposalResponse
	Related []ProposalRelatedResponse `json:"related"`
}

// ProposalRevisionResponse is one body the boss may have read. Revision 1 is
// the body as filed; each later one is a resubmission after "ask for changes".
type ProposalRevisionResponse struct {
	Revision      int32  `json:"revision"`
	Body          string `json:"body"`
	CreatedByType string `json:"created_by_type"`
	CreatedByID   string `json:"created_by_id"`
	CreatedAt     string `json:"created_at"`
}

// ProposalRelatedResponse is an earlier proposal or decision related to a new
// one. Kind says which: "proposal" entries carry state, issue_identifier and
// created_at; "decision" entries (from the decisions knowledge base) carry
// null for those. Score is the title overlap for a proposal and the
// retrieval score for a decision.
type ProposalRelatedResponse struct {
	Kind            string  `json:"kind"`
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Score           float64 `json:"score"`
	State           *string `json:"state"`
	IssueIdentifier *string `json:"issue_identifier"`
	CreatedAt       *string `json:"created_at"`
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
		Revision:        p.Revision,
		ExpiresAt:       timestampToPtr(p.ExpiresAt),
		SnoozedUntil:    timestampToPtr(p.SnoozedUntil),
	}
}

// relatedToResponse maps the prior-art entries to their response shape. The
// result is never nil, so the field is always a JSON array.
func relatedToResponse(in []service.ProposalRelated) []ProposalRelatedResponse {
	out := make([]ProposalRelatedResponse, 0, len(in))
	for _, r := range in {
		entry := ProposalRelatedResponse{Kind: r.Kind, ID: r.ID, Title: r.Title, Score: r.Score}
		if r.Kind == service.RelatedKindProposal {
			state, identifier := r.State, r.IssueIdentifier
			created := timestampToString(pgtype.Timestamptz{Time: r.CreatedAt, Valid: true})
			entry.State, entry.IssueIdentifier, entry.CreatedAt = &state, &identifier, &created
		}
		out = append(out, entry)
	}
	return out
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
	svc := service.NewProposalService(h.IssueService)
	// Typed nil would make the interface non-nil, so only set it when offered.
	if h.Knowledge != nil {
		svc.Knowledge = h.Knowledge
		svc.DecisionSearch = h.Knowledge
	}
	svc.AppURL = h.cfg.AppURL
	return svc
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
		"proposal_id", resp.ID, "squad_id", resp.SquadID, "urgent", resp.Urgent, "related", len(res.Related))...)
	writeJSON(w, http.StatusCreated, ProposalCreateResponse{ProposalResponse: resp, Related: relatedToResponse(res.Related)})
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
		case service.ProposalStatePending, service.ProposalStateApproved, service.ProposalStateRejected,
			service.ProposalStateRevising, service.ProposalStateExpired:
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

// ListProposalRevisions returns the bodies a proposal has had, newest first. Any
// workspace member who can read the proposal may read its history.
//
// A proposal filed before the history existed has fewer entries than its
// revision number: those earlier bodies were overwritten before they were
// recorded, and nothing is backfilled. The list is never null.
//
//	GET /api/proposals/{id}/revisions
func (h *Handler) ListProposalRevisions(w http.ResponseWriter, r *http.Request) {
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
	if _, err := h.Queries.GetProposalInWorkspace(r.Context(), db.GetProposalInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "proposal not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load proposal")
		return
	}
	rows, err := h.Queries.ListProposalRevisions(r.Context(), db.ListProposalRevisionsParams{WorkspaceID: wsUUID, ProposalID: id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list proposal revisions")
		return
	}
	resp := make([]ProposalRevisionResponse, len(rows))
	for i, row := range rows {
		resp[i] = ProposalRevisionResponse{
			Revision:      row.Revision,
			Body:          row.Body,
			CreatedByType: row.CreatedByType,
			CreatedByID:   uuidToString(row.CreatedByID),
			CreatedAt:     timestampToString(row.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── Decide ──────────────────────────────────────────────────────────────────

type DecideProposalRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
	// Until is the snooze end: a date (YYYY-MM-DD, midnight UTC) or an RFC 3339
	// timestamp. Only used by the snooze decision.
	Until string `json:"until"`
}

// parseSnoozeUntil reads the snooze end the boss or the CLI sent. A bare date
// means midnight UTC on that day.
func parseSnoozeUntil(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", raw)
}

// DecideProposal settles a pending proposal: approve, reject, ask for changes
// (revise) or put it aside until a date (snooze). Only an owner or admin member
// may decide: an agent actor is refused, including an agent acting with a
// member's credentials, because a decision spends the boss's authority.
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
	switch req.Decision {
	case service.ProposalDecisionApprove, service.ProposalDecisionReject,
		service.ProposalDecisionRevise, service.ProposalDecisionSnooze:
	default:
		writeError(w, http.StatusBadRequest, "decision must be 'approve', 'reject', 'revise' or 'snooze'")
		return
	}
	note := cleanProposalText(req.Note)
	if len(note) > proposalMaxTextLen {
		writeError(w, http.StatusBadRequest, "note must be at most "+strconv.Itoa(proposalMaxTextLen)+" characters")
		return
	}
	if req.Decision == service.ProposalDecisionRevise && note == "" {
		writeError(w, http.StatusBadRequest, "a note is required when asking for changes: say what to change")
		return
	}
	var until time.Time
	if req.Decision == service.ProposalDecisionSnooze {
		parsed, err := parseSnoozeUntil(req.Until)
		if err != nil {
			writeError(w, http.StatusBadRequest, "until must be a date (YYYY-MM-DD) or an RFC 3339 timestamp")
			return
		}
		until = parsed
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	fillKickoff := h.newStatusCategoryFiller(r.Context(), wsUUID)
	res, err := h.proposalService().Decide(r.Context(), service.ProposalDecideParams{
		WorkspaceID: wsUUID,
		ProposalID:  proposalID,
		DeciderID:   member.UserID,
		Decision:    req.Decision,
		Note:        note,
		Until:       until,
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
	case errors.Is(err, service.ErrProposalRevising):
		writeErrorCode(w, http.StatusConflict, "proposal_revising", "the squad is revising this proposal; decide it once the revised version comes back")
		return
	case errors.Is(err, service.ErrProposalAlreadyDecided):
		writeErrorCode(w, http.StatusConflict, "proposal_decided", "this proposal has already been decided")
		return
	case errors.Is(err, service.ErrProposalHandoffSquadNotFound):
		writeErrorCode(w, http.StatusConflict, "handoff_squad_unavailable", "the handoff squad no longer exists or is archived; approve without it or ask the squad to refile")
		return
	case errors.Is(err, service.ErrProposalSquadUnavailable):
		writeErrorCode(w, http.StatusConflict, "squad_unavailable", "the proposing squad no longer exists or is archived, so the proposal cannot go back to it; approve or reject it instead")
		return
	case errors.Is(err, service.ErrProposalNoteRequired):
		writeError(w, http.StatusBadRequest, service.ErrProposalNoteRequired.Error())
		return
	case errors.Is(err, service.ErrProposalSnoozeOutOfRange):
		writeErrorCode(w, http.StatusBadRequest, "snooze_out_of_range", service.ErrProposalSnoozeOutOfRange.Error())
		return
	case writeIssueLimitReached(w, err):
		return
	case err != nil:
		slog.Warn("decide proposal failed", append(logger.RequestAttrs(r), "error", err, "proposal_id", chi.URLParam(r, "id"))...)
		writeError(w, http.StatusInternalServerError, "failed to decide proposal")
		return
	}

	// Snooze changes no issue, so it publishes only the proposal's own event.
	if req.Decision != service.ProposalDecisionSnooze {
		h.publishProposalDecision(r.Context(), userID, prefix, wsUUID, res)
	}

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

// ── Resubmit ────────────────────────────────────────────────────────────────

// ReviseProposalRequest is the whole body again: a revision replaces every
// section, as a new proposal does. Urgency and the handoff squad stay as filed.
type ReviseProposalRequest struct {
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence"`
	Recommendation string   `json:"recommendation"`
	Options        []string `json:"options"`
	CostRisk       string   `json:"cost_risk"`
	Ask            string   `json:"ask"`
}

// ReviseProposal resubmits a proposal the boss sent back for changes. It is
// the leader's answer to a revise decision, and it returns the proposal to the
// boss's queue with its revision number raised. The actor must be allowed to
// file for the proposal's squad.
func (h *Handler) ReviseProposal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
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

	var req ReviseProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing, err := h.Queries.GetProposalInWorkspace(r.Context(), db.GetProposalInWorkspaceParams{ID: proposalID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "proposal not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load proposal")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.mayFileProposalForSquad(w, r, workspaceID, userID, actorType, actorID, existing.SquadID) {
		return
	}

	// The same field checks as a new proposal, with the urgency already filed.
	check := CreateProposalRequest{
		Title: req.Title, Summary: req.Summary, Evidence: req.Evidence,
		Recommendation: req.Recommendation, Options: req.Options, CostRisk: req.CostRisk, Ask: req.Ask,
		Urgent: existing.Urgent, UrgentReason: existing.UrgentReason,
	}
	if msg := validateProposalRequest(&check); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	res, err := h.proposalService().Resubmit(r.Context(), service.ProposalResubmitParams{
		WorkspaceID: wsUUID,
		ProposalID:  proposalID,
		Content: service.ProposalContent{
			Title: check.Title, Summary: check.Summary, Evidence: check.Evidence,
			Recommendation: check.Recommendation, Options: check.Options,
			CostRisk: check.CostRisk, Ask: check.Ask,
		},
		CreatorType: actorType,
		CreatorID:   actorUUID,
	})
	var limitErr *service.ProposalLimitError
	switch {
	case errors.Is(err, service.ErrProposalNotFound):
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	case errors.Is(err, service.ErrProposalNotRevising):
		writeErrorCode(w, http.StatusConflict, "proposal_not_revising", "this proposal is not waiting for changes, so there is nothing to resubmit")
		return
	case errors.Is(err, service.ErrProposalAlreadyDecided):
		writeErrorCode(w, http.StatusConflict, "proposal_decided", "this proposal has already been decided")
		return
	case errors.Is(err, service.ErrProposalNoBoss):
		writeError(w, http.StatusConflict, "this workspace has no owner to receive an urgent proposal")
		return
	case errors.As(err, &limitErr):
		writeErrorCode(w, http.StatusConflict, "proposal_limit", limitErr.Message)
		return
	case err != nil:
		slog.Warn("resubmit proposal failed", append(logger.RequestAttrs(r), "error", err, "proposal_id", chi.URLParam(r, "id"))...)
		writeError(w, http.StatusInternalServerError, "failed to resubmit proposal")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	h.publishProposalIssueUpdated(r.Context(), wsUUID, prefix, actorType, actorID, res.Issue, res.PrevIssueStatus)
	resp := proposalToResponse(res.Proposal, res.Issue.Title, res.Issue.Status, prefix, res.Issue.Number)
	h.publish(protocol.EventProposalUpdated, workspaceID, actorType, actorID, map[string]any{"proposal": resp})
	slog.Info("proposal resubmitted", append(logger.RequestAttrs(r),
		"proposal_id", resp.ID, "revision", resp.Revision)...)
	writeJSON(w, http.StatusOK, resp)
}

// ── Expiry ──────────────────────────────────────────────────────────────────

// systemActorID is the author id of events the server raises itself, such as
// an expiry. It is the zero UUID, as the system's comments use.
const systemActorID = "00000000-0000-0000-0000-000000000000"

// ExpireDueProposals is the work of the daily expiry job: it expires the
// pending proposals whose decision window has passed, and publishes what
// changed. It returns how many proposals it expired.
func (h *Handler) ExpireDueProposals(ctx context.Context) (int, error) {
	results, err := h.proposalService().ExpireDue(ctx)
	for _, res := range results {
		wsUUID := res.Proposal.WorkspaceID
		workspaceID := uuidToString(wsUUID)
		prefix := h.getIssuePrefix(ctx, wsUUID)
		h.publishProposalIssueUpdated(ctx, wsUUID, prefix, "system", systemActorID, res.Issue, res.PrevIssueStatus)
		h.publish(protocol.EventCommentCreated, workspaceID, "system", systemActorID, map[string]any{
			"comment":             commentToResponse(res.Comment, nil, nil),
			"issue_title":         res.Issue.Title,
			"issue_assignee_type": textToPtr(res.Issue.AssigneeType),
			"issue_assignee_id":   uuidToPtr(res.Issue.AssigneeID),
			"issue_status":        res.Issue.Status,
			"issue_revision":      res.Issue.Revision,
		})
		resp := proposalToResponse(res.Proposal, res.Issue.Title, res.Issue.Status, prefix, res.Issue.Number)
		h.publish(protocol.EventProposalUpdated, workspaceID, "system", systemActorID, map[string]any{"proposal": resp})
	}
	return len(results), err
}

// publishProposalIssueUpdated publishes the backing issue's new state. The
// payload matches the issue:updated events the issue handlers send.
func (h *Handler) publishProposalIssueUpdated(ctx context.Context, wsUUID pgtype.UUID, prefix, actorType, actorID string, issue db.Issue, prevStatus string) {
	issueResp := issueToResponse(issue, prefix)
	h.fillStatusCategory(ctx, wsUUID, &issueResp)
	h.publish(protocol.EventIssueUpdated, uuidToString(wsUUID), actorType, actorID, map[string]any{
		"issue":          issueResp,
		"status_changed": prevStatus != issue.Status,
		"prev_status":    prevStatus,
		"creator_type":   issue.CreatorType,
		"creator_id":     uuidToString(issue.CreatorID),
	})
}

// publishProposalDecision publishes the events for a committed decision and
// wakes the proposing squad's leader when the decision left a note on the
// issue: a rejection, or a request for changes.
func (h *Handler) publishProposalDecision(ctx context.Context, userID, prefix string, wsUUID pgtype.UUID, res service.ProposalDecideResult) {
	workspaceID := uuidToString(wsUUID)

	h.publishProposalIssueUpdated(ctx, wsUUID, prefix, "member", userID, res.Issue, res.PrevIssueStatus)

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
