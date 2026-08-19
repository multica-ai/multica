package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The Action API is what a plugin surface reaches through the host bridge.
//
// Every call passes three checks, in this order:
//
//  1. the installation exists in this workspace and is enabled;
//  2. the installation was granted the scope this endpoint needs;
//  3. the SIGNED-IN USER may touch the resource — enforced by the ordinary
//     loaders, not by anything plugin-specific.
//
// Three is the one that makes the whole model safe: a plugin can never let
// someone do what they could not already do themselves. One and two are what
// bound the plugin below that ceiling.
//
// There is no plugin credential anywhere in this file. The iframe holds no
// token; it posts a message to the host page, and the host re-issues the call
// on the user's own session. That is why these routes sit under the normal
// authenticated group and read the installation from a header the iframe
// cannot set for itself.
const pluginInstallationHeader = "X-Multica-Plugin-Installation"

// pluginCaller runs checks 1 and 2 and returns the authorized caller.
func (h *Handler) pluginCaller(w http.ResponseWriter, r *http.Request, scope string) (service.PluginActionCaller, db.Member, bool) {
	if !h.requirePluginsV1(w, r) {
		return service.PluginActionCaller{}, db.Member{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return service.PluginActionCaller{}, db.Member{}, false
	}
	parsedUserID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return service.PluginActionCaller{}, db.Member{}, false
	}

	caller, err := h.PluginService.AuthorizePluginAction(r.Context(), r.Header.Get(pluginInstallationHeader), parsedUserID, scope)
	if err != nil {
		writePluginError(w, err, "failed to authorize the Plugin call")
		return service.PluginActionCaller{}, db.Member{}, false
	}

	// The workspace comes from the installation, never from a client header:
	// otherwise a caller could aim an installation at a workspace it was never
	// installed in. Membership is then checked against that workspace.
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(caller.WorkspaceID), "workspace not found")
	if !ok {
		return service.PluginActionCaller{}, db.Member{}, false
	}
	return caller, member, true
}

// pluginIssueForUser is check 3. The workspace comes from the installation, not
// from a client header, and membership in that workspace was already verified
// by pluginCaller — so "the issue is in this workspace" plus "the caller is a
// member of it" is exactly the reach the user already has. An issue outside it
// is a 404 for the same reason it is on the ordinary endpoint: a plugin must
// not be able to confirm that an id it cannot read exists.
func (h *Handler) pluginIssueForUser(w http.ResponseWriter, r *http.Request, caller service.PluginActionCaller, issueID string) (db.Issue, bool) {
	if issueID == "" {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	workspaceID := uuidToString(caller.WorkspaceID)
	if issue, ok := h.resolveIssueByIdentifier(r.Context(), issueID, workspaceID); ok {
		return issue, true
	}
	parsed, err := util.ParseUUID(issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          parsed,
		WorkspaceID: caller.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	return issue, true
}

// GetPluginContext — GET /api/v1/plugin/context
func (h *Handler) GetPluginContext(w http.ResponseWriter, r *http.Request) {
	// No scope: this is the page the user is already looking at.
	caller, member, ok := h.pluginCaller(w, r, "")
	if !ok {
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), caller.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the workspace")
		return
	}
	user, err := h.Queries.GetUser(r.Context(), member.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the user")
		return
	}

	var issue *db.Issue
	if issueID := r.URL.Query().Get("issue_id"); issueID != "" {
		loaded, ok := h.pluginIssueForUser(w, r, caller, issueID)
		if !ok {
			return
		}
		issue = &loaded
	}

	writeJSON(w, http.StatusOK, h.PluginService.BuildPluginContext(caller, workspace, user, issue))
}

// GetPluginIssue — GET /api/v1/plugin/issues/{id}
func (h *Handler) GetPluginIssue(w http.ResponseWriter, r *http.Request) {
	caller, _, ok := h.pluginCaller(w, r, plugincontract.ScopeIssuesRead)
	if !ok {
		return
	}
	issue, ok := h.pluginIssueForUser(w, r, caller, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.pluginIssuePayload(r, caller, issue))
}

type patchPluginIssueRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

// PatchPluginIssue — PATCH /api/v1/plugin/issues/{id}
//
// Title and description only. Status, priority, assignee, parent, project and
// stage each carry dispatch, catalog or hierarchy semantics — a status change
// can start an agent run, and custom statuses resolve through a per-workspace
// catalog — and duplicating those rules here would give a plugin a second,
// drifting copy of them. Widening this set later is additive; getting the side
// effects wrong now is not.
func (h *Handler) PatchPluginIssue(w http.ResponseWriter, r *http.Request) {
	caller, _, ok := h.pluginCaller(w, r, plugincontract.ScopeIssuesWrite)
	if !ok {
		return
	}
	issue, ok := h.pluginIssueForUser(w, r, caller, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req patchPluginIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == nil && req.Description == nil {
		writeError(w, http.StatusBadRequest, "title or description is required")
		return
	}

	// Every other field is passed back unchanged: UpdateIssue treats a null
	// nargs as "clear", so omitting them here would silently unassign the issue
	// or detach it from its parent.
	params := db.UpdateIssueParams{
		ID:            issue.ID,
		AssigneeType:  issue.AssigneeType,
		AssigneeID:    issue.AssigneeID,
		StartDate:     issue.StartDate,
		DueDate:       issue.DueDate,
		ParentIssueID: issue.ParentIssueID,
		ProjectID:     issue.ProjectID,
		Stage:         issue.Stage,
	}
	if req.Title != nil {
		title := sanitizeNullBytes(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title must not be empty")
			return
		}
		params.Title = pgtype.Text{String: title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: sanitizeNullBytes(*req.Description), Valid: true}
	}

	updated, err := h.Queries.UpdateIssue(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the issue")
		return
	}
	writeJSON(w, http.StatusOK, h.pluginIssuePayload(r, caller, updated))
}

// pluginIssuePayload reuses the ordinary issue response so a surface sees the
// same shape the rest of the product does.
func (h *Handler) pluginIssuePayload(r *http.Request, caller service.PluginActionCaller, issue db.Issue) IssueResponse {
	prefix := ""
	if workspace, err := h.Queries.GetWorkspace(r.Context(), caller.WorkspaceID); err == nil {
		prefix = workspace.IssuePrefix
	}
	return issueToResponse(issue, prefix)
}

// ListPluginComments — GET /api/v1/plugin/issues/{id}/comments
func (h *Handler) ListPluginComments(w http.ResponseWriter, r *http.Request) {
	caller, _, ok := h.pluginCaller(w, r, plugincontract.ScopeCommentsRead)
	if !ok {
		return
	}
	issue, ok := h.pluginIssueForUser(w, r, caller, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	comments, err := h.Queries.ListCommentsForIssue(r.Context(), db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: caller.WorkspaceID,
		Limit:       maxPluginCommentsPerRead,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	payload := make([]pluginCommentResponse, 0, len(comments))
	for _, comment := range comments {
		payload = append(payload, pluginCommentResponse{
			ID:         uuidToString(comment.ID),
			AuthorType: comment.AuthorType,
			AuthorID:   uuidToString(comment.AuthorID),
			Content:    comment.Content,
			Type:       comment.Type,
			ParentID:   uuidToString(comment.ParentID),
			CreatedAt:  comment.CreatedAt.Time.UTC().Format(timeFormatRFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": payload})
}

type pluginCommentResponse struct {
	ID         string `json:"id"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id"`
	Content    string `json:"content"`
	Type       string `json:"type"`
	ParentID   string `json:"parent_id,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type createPluginCommentRequest struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id,omitempty"`
}

// CreatePluginComment — POST /api/v1/plugin/issues/{id}/comments
//
// The comment is authored BY THE USER and marked with the plugin that produced
// it, so the timeline can render "Jiayuan (via Hello Panel)".
//
// It deliberately does not run the @mention trigger dispatch that the ordinary
// comment endpoint does. A plugin that could post a mention could start agent
// runs — and spend the workspace's budget — from a surface the user only meant
// to click a button in. Plugins that want to start work will get an explicit
// hook for it rather than inheriting it as a side effect of posting text.
func (h *Handler) CreatePluginComment(w http.ResponseWriter, r *http.Request) {
	caller, member, ok := h.pluginCaller(w, r, plugincontract.ScopeCommentsWrite)
	if !ok {
		return
	}
	issue, ok := h.pluginIssueForUser(w, r, caller, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req createPluginCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := sanitizeNullBytes(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len(content) > maxPluginCommentBytes {
		writeError(w, http.StatusBadRequest, "content is too long")
		return
	}

	var parentID pgtype.UUID
	var rootComment *db.Comment
	if req.ParentID != nil && *req.ParentID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		parent, err := h.Queries.GetComment(r.Context(), parsed)
		if err != nil || uuidToString(parent.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
		parentID = parsed
		rootComment = &parent
	}

	comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: caller.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    member.UserID,
		Content:     content,
		Type:        "comment",
		ParentID:    parentID,
		ViaPluginID: caller.Installation.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the comment")
		return
	}

	// The same two follow-ups every other comment path performs. Skipping them
	// made a plugin-posted comment invisible until the next refetch, and left a
	// reply into a resolved thread without re-opening it — a comment that lands
	// as the user should behave like the user's, minus only what was refused on
	// purpose (mention dispatch).
	h.publish(protocol.EventCommentCreated, uuidToString(caller.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
	if rootComment != nil {
		h.TaskService.AutoUnresolveThreadOnReply(r.Context(), rootComment, uuidToString(caller.WorkspaceID), "member", uuidToString(member.UserID))
	}

	writeJSON(w, http.StatusCreated, pluginCommentResponse{
		ID:         uuidToString(comment.ID),
		AuthorType: comment.AuthorType,
		AuthorID:   uuidToString(comment.AuthorID),
		Content:    comment.Content,
		Type:       comment.Type,
		ParentID:   uuidToString(comment.ParentID),
		CreatedAt:  comment.CreatedAt.Time.UTC().Format(timeFormatRFC3339),
	})
}

// maxPluginCommentBytes keeps a surface from using comments as bulk storage.
const maxPluginCommentBytes = 64 * 1024

// maxPluginCommentsPerRead bounds one read. The query returns the NEWEST N in
// chronological order, so a surface on a long thread sees the recent end rather
// than a truncated beginning.
const maxPluginCommentsPerRead = 200

// --- Storage ---

func (h *Handler) pluginStorageScope(w http.ResponseWriter, r *http.Request, caller service.PluginActionCaller, member db.Member) (string, pgtype.UUID, bool) {
	scopeType := chi.URLParam(r, "scope")
	required := plugincontract.ScopeStorageWorkspace
	if scopeType == service.PluginStorageUser {
		required = plugincontract.ScopeStorageUser
	}
	if !hasGrantedScope(caller.Scopes, required) {
		writeError(w, http.StatusForbidden, "this Plugin was not granted the "+required+" scope")
		return "", pgtype.UUID{}, false
	}
	scopeID, err := service.ResolveStorageScope(scopeType, caller.WorkspaceID, member.UserID)
	if err != nil {
		writePluginError(w, err, "invalid storage scope")
		return "", pgtype.UUID{}, false
	}
	return scopeType, scopeID, true
}

func hasGrantedScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

// ListPluginStorage — GET /api/v1/plugin/storage/{scope}
func (h *Handler) ListPluginStorage(w http.ResponseWriter, r *http.Request) {
	caller, member, ok := h.pluginCaller(w, r, "")
	if !ok {
		return
	}
	scopeType, scopeID, ok := h.pluginStorageScope(w, r, caller, member)
	if !ok {
		return
	}
	keys, err := h.PluginService.ListStorageKeys(r.Context(), caller.Installation.ID, scopeType, scopeID)
	if err != nil {
		writePluginError(w, err, "failed to list storage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// GetPluginStorage — GET /api/v1/plugin/storage/{scope}/{key}
func (h *Handler) GetPluginStorage(w http.ResponseWriter, r *http.Request) {
	caller, member, ok := h.pluginCaller(w, r, "")
	if !ok {
		return
	}
	scopeType, scopeID, ok := h.pluginStorageScope(w, r, caller, member)
	if !ok {
		return
	}
	value, err := h.PluginService.GetStorageValue(r.Context(), caller.Installation.ID, scopeType, scopeID, chi.URLParam(r, "key"))
	if err != nil {
		writePluginError(w, err, "failed to read storage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": value})
}

type putPluginStorageRequest struct {
	Value string `json:"value"`
}

// PutPluginStorage — PUT /api/v1/plugin/storage/{scope}/{key}
func (h *Handler) PutPluginStorage(w http.ResponseWriter, r *http.Request) {
	caller, member, ok := h.pluginCaller(w, r, "")
	if !ok {
		return
	}
	scopeType, scopeID, ok := h.pluginStorageScope(w, r, caller, member)
	if !ok {
		return
	}
	var req putPluginStorageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.PluginService.SetStorageValue(r.Context(), caller.Installation.ID, scopeType, scopeID, chi.URLParam(r, "key"), req.Value); err != nil {
		writePluginError(w, err, "failed to write storage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeletePluginStorage — DELETE /api/v1/plugin/storage/{scope}/{key}
func (h *Handler) DeletePluginStorage(w http.ResponseWriter, r *http.Request) {
	caller, member, ok := h.pluginCaller(w, r, "")
	if !ok {
		return
	}
	scopeType, scopeID, ok := h.pluginStorageScope(w, r, caller, member)
	if !ok {
		return
	}
	if err := h.PluginService.DeleteStorageValue(r.Context(), caller.Installation.ID, scopeType, scopeID, chi.URLParam(r, "key")); err != nil {
		writePluginError(w, err, "failed to delete storage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
