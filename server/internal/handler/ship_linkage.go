// Phase 4 — Ship Hub linkage endpoints + PR conversation channels.
//
// Five new HTTP routes land here. They all reuse loadPullRequestForAction's
// gating story (workspace flag, owner-only for destructive ops) so the
// Phase 3 conventions extend cleanly:
//
//   - PATCH /api/pull_requests/{id}                — manual linkage override
//   - GET   /api/pull_requests/{id}/linked_issues  — origin issue + task
//   - POST  /api/pull_requests/{id}/talk_to_agent  — spawn chat session
//   - GET   /api/issues/{id}/pull_requests         — list PRs for issue
//   - POST  /api/pull_requests/{id}/conversation_channel — get-or-create
//   - GET   /api/projects/{id}/pull_request_stacks — render stack tree
//
// channel auto-create on PR open + archive-on-close lives in
// ship_webhook.go's dispatcher because that's where the webhook outcome
// is consumed; this file owns the user-driven endpoints.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// loadPullRequestInWorkspace resolves the PR by URL param and confirms
// it lives in the caller's workspace. Used by Phase 4 endpoints that
// don't require a GitHub token (i.e. don't need the full ship.Service).
func (h *Handler) loadPullRequestInWorkspace(w http.ResponseWriter, r *http.Request) (db.PullRequest, pgtype.UUID, db.Workspace, bool) {
	wsID, ws, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	prUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "pull request id")
	if !ok {
		return db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	pr, err := h.Queries.GetPullRequest(r.Context(), prUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	if uuidToString(pr.WorkspaceID) != uuidToString(wsID) {
		writeError(w, http.StatusNotFound, "pull request not found")
		return db.PullRequest{}, pgtype.UUID{}, db.Workspace{}, false
	}
	return pr, wsID, ws, true
}

// UpdatePullRequestLinkageRequest is the body for PATCH /api/pull_requests/{id}.
// Pointer fields distinguish "field absent" from "field present and
// null"; the SQL update only touches columns whose narg is set.
type UpdatePullRequestLinkageRequest struct {
	OriginatingIssueID     *string `json:"originating_issue_id"`
	OriginatingAgentTaskID *string `json:"originating_agent_task_id"`
	AutoCloseIssueOnMerge  *bool   `json:"auto_close_issue_on_merge"`
}

// UpdatePullRequest handles PATCH /api/pull_requests/{id} — manual
// override of auto-detected linkage. Workspace-member permission only;
// editing this row doesn't touch GitHub so we don't need owner role.
func (h *Handler) UpdatePullRequest(w http.ResponseWriter, r *http.Request) {
	pr, wsID, _, ok := h.loadPullRequestInWorkspace(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}
	var req UpdatePullRequestLinkageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdatePullRequestLinkageParams{ID: pr.ID}
	// Existing values preserved when caller omits the field. The SQL uses
	// narg semantics so leaving Valid=false leaves the column untouched.
	params.OriginatingIssueID = pr.OriginatingIssueID
	params.OriginatingAgentTaskID = pr.OriginatingAgentTaskID

	if req.OriginatingIssueID != nil {
		if *req.OriginatingIssueID == "" {
			params.OriginatingIssueID = pgtype.UUID{}
		} else {
			issueUUID, ok := parseUUIDOrBadRequest(w, *req.OriginatingIssueID, "originating_issue_id")
			if !ok {
				return
			}
			// Must belong to the same workspace.
			if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          issueUUID,
				WorkspaceID: wsID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, "originating_issue_id not found in workspace")
				return
			}
			params.OriginatingIssueID = issueUUID
		}
	}
	if req.OriginatingAgentTaskID != nil {
		if *req.OriginatingAgentTaskID == "" {
			params.OriginatingAgentTaskID = pgtype.UUID{}
		} else {
			taskUUID, ok := parseUUIDOrBadRequest(w, *req.OriginatingAgentTaskID, "originating_agent_task_id")
			if !ok {
				return
			}
			params.OriginatingAgentTaskID = taskUUID
		}
	}
	if req.AutoCloseIssueOnMerge != nil {
		params.AutoCloseIssueOnMerge = pgtype.Bool{Bool: *req.AutoCloseIssueOnMerge, Valid: true}
	}

	updated, err := h.Queries.UpdatePullRequestLinkage(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pull request")
		return
	}
	writeJSON(w, http.StatusOK, pullRequestToResponse(updated))
}

// linkedIssueResponse is the GET /api/pull_requests/{id}/linked_issues
// payload. Both fields are optional; the frontend renders chips
// conditionally based on which are present.
type linkedIssueResponse struct {
	Issue *IssueResponse `json:"issue"`
	// AgentTask is the originating task, with its prompt summary so the
	// "Talk to {agent_name}" chip can prefill the chat context.
	AgentTask *agentTaskBrief `json:"agent_task"`
}

type agentTaskBrief struct {
	ID             string  `json:"id"`
	AgentID        string  `json:"agent_id"`
	AgentName      string  `json:"agent_name"`
	Status         string  `json:"status"`
	TriggerSummary *string `json:"trigger_summary"`
	IssueID        *string `json:"issue_id"`
}

// GetLinkedIssues handles GET /api/pull_requests/{id}/linked_issues.
func (h *Handler) GetLinkedIssues(w http.ResponseWriter, r *http.Request) {
	pr, wsID, _, ok := h.loadPullRequestInWorkspace(w, r)
	if !ok {
		return
	}
	resp := linkedIssueResponse{}

	if pr.OriginatingIssueID.Valid {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          pr.OriginatingIssueID,
			WorkspaceID: wsID,
		})
		if err == nil {
			ws, _ := h.Queries.GetWorkspace(r.Context(), wsID)
			ir := issueToResponse(issue, ws.IssuePrefix)
			resp.Issue = &ir
		}
	}
	if pr.OriginatingAgentTaskID.Valid {
		task, err := h.Queries.GetAgentTask(r.Context(), pr.OriginatingAgentTaskID)
		if err == nil {
			brief := agentTaskBrief{
				ID:      uuidToString(task.ID),
				AgentID: uuidToString(task.AgentID),
				Status:  task.Status,
			}
			if task.TriggerSummary.Valid {
				v := task.TriggerSummary.String
				brief.TriggerSummary = &v
			}
			if task.IssueID.Valid {
				v := uuidToString(task.IssueID)
				brief.IssueID = &v
			}
			if agent, err := h.Queries.GetAgent(r.Context(), task.AgentID); err == nil {
				brief.AgentName = agent.Name
			}
			resp.AgentTask = &brief
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// TalkToAgentRequest is the body for POST /api/pull_requests/{id}/talk_to_agent.
// The message is optional — the chat starts cleanly when omitted; the
// frontend's chat panel will surface the textarea for the user to type
// the first message themselves.
type TalkToAgentRequest struct {
	Message string `json:"message"`
}

// TalkToAgent handles POST /api/pull_requests/{id}/talk_to_agent.
//
// Spawns a chat session with the originating agent task's agent. The
// task's existing context (issue, prompt, work_dir) is implicitly
// available because chat sessions on the same agent share the same
// workspace + agent scope and the runtime hydrates context per-session
// from the agent's memory artifacts. We don't need to copy fields
// across.
//
// Returns the new chat_session_id so the frontend can route the user
// into the chat panel immediately.
func (h *Handler) TalkToAgent(w http.ResponseWriter, r *http.Request) {
	pr, wsID, _, ok := h.loadPullRequestInWorkspace(w, r)
	if !ok {
		return
	}
	if !pr.OriginatingAgentTaskID.Valid {
		writeError(w, http.StatusBadRequest, "pull request has no originating agent task")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req TalkToAgentRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	task, err := h.Queries.GetAgentTask(r.Context(), pr.OriginatingAgentTaskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "originating agent task not found")
		return
	}

	title := fmt.Sprintf("PR #%d — chat with agent", pr.PrNumber)
	if strings.TrimSpace(pr.Title) != "" {
		title = fmt.Sprintf("PR #%d — %s", pr.PrNumber, pr.Title)
	}

	session, err := h.Queries.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: wsID,
		AgentID:     task.AgentID,
		CreatorID:   parseUUID(userID),
		Title:       title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chat session")
		return
	}

	// Prefill the first user message if the caller provided one. The
	// chat panel renders messages via ListChatMessages; an opening
	// message gives the agent immediate context for what the user wants
	// to discuss.
	if strings.TrimSpace(req.Message) != "" {
		_, _ = h.Queries.CreateChatMessage(r.Context(), db.CreateChatMessageParams{
			ChatSessionID: session.ID,
			Role:          "user",
			Content:       req.Message,
		})
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"chat_session_id": uuidToString(session.ID),
		"agent_id":        uuidToString(task.AgentID),
	})
}

// ListIssuePullRequests handles GET /api/issues/{id}/pull_requests.
// Returns every PR in the workspace whose originating_issue_id matches.
// The issue id may be a UUID or an identifier (MUL-123) — loadIssueForUser
// handles the polymorphism.
func (h *Handler) ListIssuePullRequests(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPullRequestsByOriginatingIssue(r.Context(), db.ListPullRequestsByOriginatingIssueParams{
		WorkspaceID:        issue.WorkspaceID,
		OriginatingIssueID: issue.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}
	out := make([]pullRequestResponse, len(rows))
	for i, pr := range rows {
		out[i] = pullRequestToResponse(pr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out, "total": len(out)})
}

// GetOrCreatePRConversationChannel handles
// POST /api/pull_requests/{id}/conversation_channel. Idempotent: if the
// PR already has a channel, return it; otherwise create one and seed
// membership (PR author if a member + workspace owners + orchestrator).
func (h *Handler) GetOrCreatePRConversationChannel(w http.ResponseWriter, r *http.Request) {
	pr, wsID, ws, ok := h.loadPullRequestInWorkspace(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(wsID), "workspace not found"); !ok {
		return
	}

	// Already linked? Just return it. We don't "heal" a stale link by
	// recreating; the client can call DELETE on the channel separately
	// if they want to reset.
	if pr.ConversationChannelID.Valid {
		ch, err := h.Queries.GetChannelInWorkspace(r.Context(), db.GetChannelInWorkspaceParams{
			ID:          pr.ConversationChannelID,
			WorkspaceID: wsID,
		})
		if err == nil {
			writeJSON(w, http.StatusOK, channelToResponse(ch))
			return
		}
		// FK ON DELETE SET NULL means the link can dangle if the channel
		// got hard-deleted. Fall through to creation.
	}

	channelObj, err := h.createPRConversationChannel(r.Context(), wsID, ws, pr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create conversation channel: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, channelToResponse(channelObj))
}

// createPRConversationChannel is the shared helper used by the
// HTTP-driven endpoint AND the webhook-driven auto-create path. Returns
// the channel row (with linkage already persisted on the PR side).
func (h *Handler) createPRConversationChannel(ctx context.Context, wsID pgtype.UUID, ws db.Workspace, pr db.PullRequest) (db.Channel, error) {
	// Channel name is `pr-{repo_slug}-{number}`. repo_slug = "{owner}-{repo}"
	// for unambiguity (multi-repo workspaces could otherwise have two PRs
	// numbered the same). Lowercased + slug-safe per channel name rules.
	repoSlug := repoSlugFromURL(pr.RepoUrl)
	name := fmt.Sprintf("pr-%s-%d", repoSlug, pr.PrNumber)
	displayName := fmt.Sprintf("PR #%d — %s", pr.PrNumber, truncateForDisplay(pr.Title, 60))

	creator := channel.Actor{Type: channel.ActorMember, ID: ws.ID}
	if ws.OrchestratorAgentID.Valid {
		creator = channel.Actor{Type: channel.ActorAgent, ID: ws.OrchestratorAgentID}
	}

	ch, err := h.ChannelService.Create(ctx, channel.CreateChannelParams{
		WorkspaceID: wsID,
		Name:        name,
		DisplayName: displayName,
		Description: fmt.Sprintf("Discussion for %s", pr.HtmlUrl),
		Kind:        channel.KindChannel,
		Visibility:  channel.VisibilityPrivate,
		CreatedBy:   creator,
	})
	if err != nil {
		// Conflict on (workspace_id, kind, name) — the same PR was
		// already opened earlier and a stale channel survives. Reuse it
		// rather than failing.
		if errors.Is(err, channel.ErrConflict) {
			existing, getErr := h.Queries.GetChannelByName(ctx, db.GetChannelByNameParams{
				WorkspaceID: wsID,
				Kind:        channel.KindChannel,
				Name:        name,
			})
			if getErr == nil {
				ch = existing
			} else {
				return db.Channel{}, fmt.Errorf("conflict + lookup failed: %w", getErr)
			}
		} else {
			return db.Channel{}, err
		}
	}

	// Seed membership: orchestrator (best effort) + workspace owners.
	// Members are added with ON CONFLICT DO NOTHING so this stays
	// idempotent against concurrent webhook + HTTP calls.
	if ws.OrchestratorAgentID.Valid {
		_, _ = h.ChannelService.AddMember(ctx, ch.ID, channel.AddMemberParams{
			Member: channel.Actor{Type: channel.ActorAgent, ID: ws.OrchestratorAgentID},
			Role:   channel.RoleMember,
		})
	}
	owners, _ := h.Queries.ListWorkspaceMembersByRole(ctx, db.ListWorkspaceMembersByRoleParams{
		WorkspaceID: wsID,
		Column2:     []string{"owner", "admin"},
	})
	for _, o := range owners {
		_, _ = h.ChannelService.AddMember(ctx, ch.ID, channel.AddMemberParams{
			Member: channel.Actor{Type: channel.ActorMember, ID: o.UserID},
			Role:   channel.RoleMember,
		})
	}

	if err := h.Queries.UpdatePullRequestConversationChannel(ctx, db.UpdatePullRequestConversationChannelParams{
		ID:                    pr.ID,
		ConversationChannelID: ch.ID,
	}); err != nil {
		// Non-fatal — the channel exists; the link is just missing. The
		// next webhook + GET retry will re-resolve.
		return ch, nil
	}
	return ch, nil
}

// archivePRConversationChannel runs on PR close/merge. Walks the
// channel's messages, snapshots them into a memory_artifact runbook
// anchored to the PR's project, then archives the channel. Failures in
// either step are logged but don't propagate — the merge/close path
// should never be blocked by archive bookkeeping.
func (h *Handler) archivePRConversationChannel(ctx context.Context, wsID pgtype.UUID, pr db.PullRequest) {
	if !pr.ConversationChannelID.Valid {
		return
	}
	ch, err := h.Queries.GetChannelInWorkspace(ctx, db.GetChannelInWorkspaceParams{
		ID:          pr.ConversationChannelID,
		WorkspaceID: wsID,
	})
	if err != nil {
		return
	}
	// Already archived? Idempotent skip.
	if ch.ArchivedAt.Valid {
		return
	}

	// Snapshot the message stream to markdown. We pull a full page
	// (up to 1000) — at the small-team scale Ship Hub targets, an
	// individual PR rarely accumulates more, and this avoids a
	// pagination loop in the hot archive path.
	rows, err := h.Queries.ListChannelMessagesIncludingThreads(ctx, db.ListChannelMessagesIncludingThreadsParams{
		ChannelID: ch.ID,
		Limit:     1000,
	})
	if err == nil && len(rows) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "# PR #%d discussion archive\n\n", pr.PrNumber)
		fmt.Fprintf(&b, "**PR**: %s\n\n", pr.HtmlUrl)
		fmt.Fprintf(&b, "**Repo**: %s\n\n", pr.RepoUrl)
		// rows are DESC by created_at; reverse so the archive reads
		// chronologically.
		for i := len(rows) - 1; i >= 0; i-- {
			m := rows[i]
			fmt.Fprintf(&b, "**%s** at %s:\n%s\n\n",
				m.AuthorType, timestampToString(m.CreatedAt), m.Content)
		}
		title := fmt.Sprintf("PR #%d discussion archive — %s", pr.PrNumber, truncateForDisplay(pr.Title, 80))
		// Anchor on the project so the runbook surfaces in the project
		// detail page's "memory" panel automatically.
		anchorType := pgtype.Text{String: "project", Valid: pr.ProjectID.Valid}
		anchorID := pr.ProjectID
		if !pr.ProjectID.Valid {
			anchorType = pgtype.Text{}
		}
		_, _ = h.Queries.CreateMemoryArtifact(ctx, db.CreateMemoryArtifactParams{
			WorkspaceID: wsID,
			Kind:        "runbook",
			Title:       title,
			Content:     b.String(),
			AnchorType:  anchorType,
			AnchorID:    anchorID,
			AuthorType:  "agent",
			AuthorID:    workspaceAuthor(wsID),
			Tags:        []string{"pr_archive", fmt.Sprintf("pr_%d", pr.PrNumber)},
			Metadata:    []byte(fmt.Sprintf(`{"pull_request_id":"%s","pr_number":%d}`, uuidToString(pr.ID), pr.PrNumber)),
		})
	}

	if err := h.Queries.ArchiveChannel(ctx, ch.ID); err == nil {
		h.publish(protocol.EventChannelArchived, uuidToString(wsID), "system", "", map[string]any{
			"channel_id": uuidToString(ch.ID),
		})
	}
}

// workspaceAuthor returns a pgtype.UUID usable as memory_artifact.author_id
// when the writer is a workspace-system actor. Memory artifacts require
// a non-null author; the orchestrator agent would be the natural
// choice, but we don't want to fail the archive if the workspace is
// mid-onboarding without one. Falling back to the workspace ID keeps
// the row valid (memory_artifact.author_id has no FK).
func workspaceAuthor(wsID pgtype.UUID) pgtype.UUID {
	return wsID
}

// ListProjectPRStacks handles GET /api/projects/{id}/pull_request_stacks.
// Returns the open PR forest as `[{root_pr, children: [...]}]` so the
// frontend can render nested rows.
func (h *Handler) ListProjectPRStacks(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := h.loadShipProject(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPullRequestsByProject(r.Context(), db.ListPullRequestsByProjectParams{
		ProjectID: project.ID,
		State:     db.NullPullRequestState{PullRequestState: db.PullRequestStateOpen, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}

	// Build a children-by-parent map and a "is referenced as parent"
	// set so we can pick the actual roots (rows whose stack_parent_pr_id
	// is null OR whose parent isn't itself in the open list).
	//
	// Two passes: rows arrive ordered by pr_updated_at DESC, so a child
	// can sit ahead of its parent. Build byID first, then resolve
	// parent links — otherwise the parent lookup in the second pass
	// would miss the not-yet-seen root and the child would be
	// promoted to its own stack.
	byID := map[string]db.PullRequest{}
	for _, pr := range rows {
		byID[uuidToString(pr.ID)] = pr
	}
	childrenByParent := map[string][]string{}
	hasParent := map[string]bool{}
	for _, pr := range rows {
		if pr.StackParentPrID.Valid {
			pid := uuidToString(pr.StackParentPrID)
			if _, ok := byID[pid]; ok {
				childrenByParent[pid] = append(childrenByParent[pid], uuidToString(pr.ID))
				hasParent[uuidToString(pr.ID)] = true
			}
		}
	}

	type stackNode struct {
		PR       pullRequestResponse `json:"pr"`
		Children []stackNode         `json:"children"`
	}
	var build func(prID string) stackNode
	build = func(prID string) stackNode {
		node := stackNode{PR: pullRequestToResponse(byID[prID])}
		for _, childID := range childrenByParent[prID] {
			node.Children = append(node.Children, build(childID))
		}
		return node
	}

	stacks := []stackNode{}
	for _, pr := range rows {
		idStr := uuidToString(pr.ID)
		if hasParent[idStr] {
			continue // not a root
		}
		stacks = append(stacks, build(idStr))
	}
	writeJSON(w, http.StatusOK, map[string]any{"stacks": stacks})
}

// repoSlugFromURL converts a GitHub URL into a slug-safe identifier.
// "https://github.com/foo/bar" → "foo-bar". Used for the channel name
// (`pr-{slug}-{number}`).
func repoSlugFromURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	// SSH form: git@github.com:owner/repo — strip the host:user prefix so
	// the owner/repo portion lines up with the HTTPS form. Anything before
	// the last `:` is the SSH user@host; what's after is "owner/repo".
	if !strings.Contains(url, "//") {
		if i := strings.LastIndex(url, ":"); i >= 0 {
			url = url[i+1:]
		}
	}
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return strings.ToLower(strings.Join(parts, "-"))
	}
	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]
	slug := strings.ToLower(owner + "-" + repo)
	// Replace anything outside [a-z0-9-_] with '-' to satisfy channel name rules.
	var b strings.Builder
	for _, r := range slug {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// truncateForDisplay returns the first n runes of s, no ellipsis,
// suitable for cropping a PR title into a channel display name. n is a
// rune count (not bytes) to keep emoji-aware UI happy.
func truncateForDisplay(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

