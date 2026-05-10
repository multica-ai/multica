// PR detail drawer — bundled GET /api/pull_requests/{id}/details endpoint.
//
// One handler, one round-trip. The frontend Sheet renders rich PR
// metadata (description, CI checks, reviews, linked issue / agent task /
// channel / release, stack neighbours, recent ship_card_actions) and
// the cost of N round-trips on drawer open is what we save here. Every
// section degrades gracefully — a missing review row or a deleted
// agent task simply omits that section, never a 500.
//
// The endpoint reuses the Phase 4 loadPullRequestInWorkspace gate so
// workspace-scope and ship_hub_enabled checks stay identical to every
// other PR endpoint.

package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pullRequestDetailsResponse is the bundled JSON the drawer reads.
//
// Per CLAUDE.md "API Response Compatibility": every optional section is a
// pointer or empty-default slice so an older Electron build that doesn't
// know about a field simply ignores it, and a server bug that drops a
// section degrades the drawer to a "section hidden" state rather than a
// crash. Arrays are never nil — sqlc returns `[]` and we keep that.
type pullRequestDetailsResponse struct {
	PullRequest         pullRequestResponse        `json:"pull_request"`
	LinkedIssue         *IssueResponse             `json:"linked_issue,omitempty"`
	OriginatingAgentTask *agentTaskRef             `json:"originating_agent_task,omitempty"`
	ActiveRelease       *activeReleaseRef          `json:"active_release,omitempty"`
	ConversationChannel *channelRef                `json:"conversation_channel,omitempty"`
	Reviews             []prReviewResponse         `json:"reviews"`
	Checks              []prCheckResponse          `json:"checks"`
	RecentActions       []shipCardActionResponse   `json:"recent_actions"`
	StackParent         *pullRequestRef            `json:"stack_parent,omitempty"`
	StackChildren       []pullRequestRef           `json:"stack_children"`
}

// agentTaskRef is the minimal "spawn a chat with this agent" descriptor
// for the drawer. Distinct from the Phase 4 agentTaskBrief because the
// drawer only needs id+title+agent_name (the chat session creator
// resolves the rest server-side). Optional fields stay pointer/empty
// per the API drift contract.
type agentTaskRef struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

type channelRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// pullRequestRef is the lightweight neighbour-PR descriptor used for the
// stack parent + children section. Number is the GitHub PR number.
// State is the same loose-typed string the main PR response uses.
type pullRequestRef struct {
	ID      string `json:"id"`
	Number  int32  `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

type prReviewResponse struct {
	ID                string  `json:"id"`
	ReviewerLogin     string  `json:"reviewer_login"`
	ReviewerAvatarURL *string `json:"reviewer_avatar_url"`
	State             string  `json:"state"`
	Body              *string `json:"body"`
	SubmittedAt       string  `json:"submitted_at"`
}

type prCheckResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Conclusion  *string `json:"conclusion"`
	Status      string  `json:"status"`
	DetailsURL  *string `json:"details_url"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
}

type shipCardActionResponse struct {
	ID           string  `json:"id"`
	Action       string  `json:"action"`
	ResultStatus string  `json:"result_status"`
	ActorUserID  *string `json:"actor_user_id"`
	CreatedAt    string  `json:"created_at"`
	CompletedAt  *string `json:"completed_at"`
}

// recentActionsLimit caps the audit-trail tail surfaced in the drawer.
// The same SQL helper feeds Phase 3's per-PR audit footer so we keep
// the limit small (10 is enough to spot the most recent decisions; full
// history lives on the release page).
const recentActionsLimit = 10

// GetPullRequestDetails handles GET /api/pull_requests/{id}/details.
//
// The single round-trip mirrors how the frontend Sheet renders:
//   - PR row (mandatory)
//   - linked Multica issue (optional)
//   - originating agent task (optional)
//   - active release (optional, decorated via the same join used by the
//     list endpoint)
//   - conversation channel (optional)
//   - reviews (Phase 2 — empty for fresh PRs)
//   - checks (Phase 2 — empty for PRs with no CI runs)
//   - recent ship_card_actions (Phase 3 — last 10)
//   - stack parent + immediate children (Phase 4)
//
// Every optional lookup is best-effort: a failure logs nothing, drops
// the field, and the rest of the response still renders. The handler
// MUST never short-circuit on an optional miss because the drawer
// becomes useless at that point.
func (h *Handler) GetPullRequestDetails(w http.ResponseWriter, r *http.Request) {
	pr, wsID, _, ok := h.loadPullRequestInWorkspace(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Mandatory PR shape with active_release decoration. Mirrors the
	// list endpoint so the drawer sees the same denormalized release
	// badge the card already renders.
	prResp := pullRequestToResponse(pr)
	h.attachActiveReleaseToPRList(ctx, []pullRequestResponse{prResp}, []db.PullRequest{pr})

	resp := pullRequestDetailsResponse{
		PullRequest:   prResp,
		ActiveRelease: prResp.ActiveRelease,
		// Pre-allocate the lists so empty responses serialize as `[]`,
		// not `null`. The frontend zod schema also defaults to []; this
		// keeps the wire shape stable for callers that walk the JSON
		// without going through the parser.
		Reviews:       []prReviewResponse{},
		Checks:        []prCheckResponse{},
		RecentActions: []shipCardActionResponse{},
		StackChildren: []pullRequestRef{},
	}

	// Linked issue. Optional; PRs without an originating issue simply
	// omit the field. The cross-workspace check is implicit in
	// GetIssueInWorkspace's filter — a row from another workspace
	// returns ErrNoRows and we drop the field.
	if pr.OriginatingIssueID.Valid {
		if issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          pr.OriginatingIssueID,
			WorkspaceID: wsID,
		}); err == nil {
			ws, _ := h.Queries.GetWorkspace(ctx, wsID)
			ir := issueToResponse(issue, ws.IssuePrefix)
			resp.LinkedIssue = &ir
		}
	}

	// Originating agent task. The agent task may have been deleted (FK
	// is ON DELETE SET NULL on agent_task_queue → pull_request) — the
	// query check covers that case.
	if pr.OriginatingAgentTaskID.Valid {
		if task, err := h.Queries.GetAgentTask(ctx, pr.OriginatingAgentTaskID); err == nil {
			ref := agentTaskRef{
				ID:      uuidToString(task.ID),
				AgentID: uuidToString(task.AgentID),
				Status:  task.Status,
			}
			if task.TriggerSummary.Valid {
				ref.Title = task.TriggerSummary.String
			}
			if agent, err := h.Queries.GetAgent(ctx, task.AgentID); err == nil {
				ref.AgentName = agent.Name
			}
			resp.OriginatingAgentTask = &ref
		}
	}

	// Conversation channel. The drawer renders the link verbatim — we
	// don't reach into ChannelService for membership / unread counts here.
	if pr.ConversationChannelID.Valid {
		if ch, err := h.Queries.GetChannelInWorkspace(ctx, db.GetChannelInWorkspaceParams{
			ID:          pr.ConversationChannelID,
			WorkspaceID: wsID,
		}); err == nil {
			resp.ConversationChannel = &channelRef{
				ID:          uuidToString(ch.ID),
				Name:        ch.Name,
				DisplayName: ch.DisplayName,
			}
		}
	}

	// Reviews — newest-first by submitted_at per the SQL ordering. A PR
	// with no review activity returns an empty list; the drawer hides
	// the section in that case.
	if rows, err := h.Queries.ListReviewsForPR(ctx, pr.ID); err == nil {
		resp.Reviews = make([]prReviewResponse, len(rows))
		for i, rv := range rows {
			resp.Reviews[i] = prReviewResponse{
				ID:                uuidToString(rv.ID),
				ReviewerLogin:     rv.ReviewerLogin,
				ReviewerAvatarURL: textToPtr(rv.ReviewerAvatarUrl),
				State:             rv.State,
				Body:              textToPtr(rv.Body),
				SubmittedAt:       timestampToString(rv.SubmittedAt),
			}
		}
	}

	// Checks — newest-first by started_at via the new ListChecksForPullRequest
	// helper (distinct from the head-sha-only rollup query).
	if rows, err := h.Queries.ListChecksForPullRequest(ctx, pr.ID); err == nil {
		resp.Checks = make([]prCheckResponse, len(rows))
		for i, ch := range rows {
			resp.Checks[i] = prCheckResponse{
				ID:          uuidToString(ch.ID),
				Name:        ch.Name,
				Conclusion:  textToPtr(ch.Conclusion),
				Status:      ch.Status,
				DetailsURL:  textToPtr(ch.DetailsUrl),
				StartedAt:   timestampToPtr(ch.StartedAt),
				CompletedAt: timestampToPtr(ch.CompletedAt),
			}
		}
	}

	// Recent actions — last N rows newest-first.
	if rows, err := h.Queries.ListShipCardActionsForPR(ctx, db.ListShipCardActionsForPRParams{
		PullRequestID: pr.ID,
		Limit:         recentActionsLimit,
	}); err == nil {
		resp.RecentActions = make([]shipCardActionResponse, len(rows))
		for i, a := range rows {
			resp.RecentActions[i] = shipCardActionResponse{
				ID:           uuidToString(a.ID),
				Action:       a.Action,
				ResultStatus: a.ResultStatus,
				ActorUserID:  uuidToPtr(a.ActorUserID),
				CreatedAt:    timestampToString(a.CreatedAt),
				CompletedAt:  timestampToPtr(a.CompletedAt),
			}
		}
	}

	// Stack parent + immediate children. The parent lookup is a single
	// row; children come from the indexed reverse lookup. Both are
	// scoped to the PR's workspace so a stale FK from a deleted
	// workspace can't leak.
	if pr.StackParentPrID.Valid {
		if parent, err := h.loadStackPRRef(ctx, pr.StackParentPrID, wsID); err == nil {
			resp.StackParent = &parent
		}
	}
	if children, err := h.loadStackChildren(ctx, pr.ID, wsID); err == nil {
		resp.StackChildren = children
	}

	writeJSON(w, http.StatusOK, resp)
}

// loadStackPRRef returns a pullRequestRef for the given UUID, scoped to
// the workspace. Returns the same ErrNoRows the underlying query
// surfaces when the row is missing — the caller drops the field on
// error.
func (h *Handler) loadStackPRRef(ctx context.Context, prID pgtype.UUID, wsID pgtype.UUID) (pullRequestRef, error) {
	pr, err := h.Queries.GetPullRequest(ctx, prID)
	if err != nil {
		return pullRequestRef{}, err
	}
	if uuidToString(pr.WorkspaceID) != uuidToString(wsID) {
		return pullRequestRef{}, errStackRefOutOfWorkspace
	}
	return pullRequestRef{
		ID:      uuidToString(pr.ID),
		Number:  pr.PrNumber,
		Title:   pr.Title,
		State:   string(pr.State),
		HTMLURL: pr.HtmlUrl,
	}, nil
}

// loadStackChildren returns every PR whose stack_parent_pr_id is the
// given parent ID, in PR-number order. Bounded by workspace_id so a
// rogue FK from another workspace can't surface here.
func (h *Handler) loadStackChildren(ctx context.Context, parentID pgtype.UUID, wsID pgtype.UUID) ([]pullRequestRef, error) {
	rows, err := h.Queries.ListPullRequestStackChildren(ctx, db.ListPullRequestStackChildrenParams{
		WorkspaceID:     wsID,
		StackParentPrID: parentID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]pullRequestRef, len(rows))
	for i, pr := range rows {
		out[i] = pullRequestRef{
			ID:      uuidToString(pr.ID),
			Number:  pr.PrNumber,
			Title:   pr.Title,
			State:   string(pr.State),
			HTMLURL: pr.HtmlUrl,
		}
	}
	return out, nil
}

// errStackRefOutOfWorkspace surfaces when a stack-parent PR row's
// workspace_id doesn't match the caller's workspace. The handler drops
// the field on error rather than 403/404'ing the whole response.
var errStackRefOutOfWorkspace = pgErr("stack ref out of workspace")

// pgErr is a tiny string-error helper kept private to this file. We
// don't want to import "errors" just for one sentinel.
type pgErr string

func (e pgErr) Error() string { return string(e) }
