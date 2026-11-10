package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hiddenProjects returns the private projects (MUL-8472) this request may not
// see. A member request is bounded by the member; an agent request by the
// run's originator and the agent's owner together (projectaccess.RunPrincipals).
//
// Enforcement does not read the private_projects_v1 flag: the flag gates
// creating private projects, and a project that is already private stays
// private whatever the flag says.
func (h *Handler) hiddenProjects(r *http.Request, workspaceID string) (projectaccess.Hidden, error) {
	ctx := r.Context()
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return projectaccess.Hidden{}, err
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		return h.hiddenProjectsForAgentRun(ctx, wsUUID, actorID, r.Header.Get("X-Task-ID"))
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return projectaccess.ForPrincipals(ctx, h.Queries, wsUUID)
	}
	if m, ok := ctxMember(ctx); ok && m.WorkspaceID == wsUUID && m.UserID == userUUID {
		return projectaccess.ForMember(ctx, h.Queries, wsUUID, userUUID, m.Role)
	}
	return projectaccess.ForPrincipals(ctx, h.Queries, wsUUID, userUUID)
}

func (h *Handler) hiddenProjectsForAgentRun(ctx context.Context, workspaceID pgtype.UUID, agentID, taskID string) (projectaccess.Hidden, error) {
	// Most workspaces have no private project; skip the run lookups there.
	if private, err := h.Queries.ListPrivateProjectIDs(ctx, workspaceID); err != nil || len(private) == 0 {
		return projectaccess.Hidden{}, err
	}
	agentUUID, agentErr := util.ParseUUID(agentID)
	taskUUID, taskErr := util.ParseUUID(taskID)
	if agentErr != nil || taskErr != nil {
		return projectaccess.ForPrincipals(ctx, h.Queries, workspaceID)
	}
	task, err := h.Queries.GetAgentTask(ctx, taskUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectaccess.ForPrincipals(ctx, h.Queries, workspaceID)
	}
	if err != nil {
		return projectaccess.Hidden{}, err
	}
	agent, err := h.Queries.GetAgent(ctx, agentUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectaccess.ForPrincipals(ctx, h.Queries, workspaceID)
	}
	if err != nil {
		return projectaccess.Hidden{}, err
	}
	return projectaccess.ForPrincipals(ctx, h.Queries, workspaceID, projectaccess.RunPrincipals(task, agent.OwnerID)...)
}

// requestHiddenProjects is hiddenProjects for handlers: it writes a 500 and
// returns ok=false when access cannot be resolved, so callers fail closed.
func (h *Handler) requestHiddenProjects(w http.ResponseWriter, r *http.Request, workspaceID string) (projectaccess.Hidden, bool) {
	hidden, err := h.hiddenProjects(r, workspaceID)
	if err != nil {
		if errors.Is(r.Context().Err(), context.Canceled) && errors.Is(err, context.Canceled) {
			w.WriteHeader(middleware.StatusClientClosedRequest)
			return projectaccess.Hidden{}, false
		}
		slog.Warn("resolve project access failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return projectaccess.Hidden{}, false
	}
	return hidden, true
}

// requireProjectVisible writes status with notFoundMsg when projectID is a
// private project the request may not see. Callers pass the status and
// message they already use for a missing object: a hidden object must be
// indistinguishable from one that does not exist, so this never answers 403.
func (h *Handler) requireProjectVisible(w http.ResponseWriter, r *http.Request, workspaceID string, projectID pgtype.UUID, status int, notFoundMsg string) bool {
	if !projectID.Valid {
		return true
	}
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return false
	}
	if !hidden.CanSeeProject(projectID) {
		writeError(w, status, notFoundMsg)
		return false
	}
	return true
}

// requireIssueIDVisible is requireProjectVisible for objects that carry only
// an issue id (comments, attachments, reactions).
func (h *Handler) requireIssueIDVisible(w http.ResponseWriter, r *http.Request, workspaceID string, issueID pgtype.UUID, status int, notFoundMsg string) bool {
	if !issueID.Valid {
		return true
	}
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return false
	}
	if hidden.Empty() {
		return true
	}
	projectID, err := h.Queries.GetIssueProjectID(r.Context(), issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return false
	}
	if !hidden.CanSeeProject(projectID) {
		writeError(w, status, notFoundMsg)
		return false
	}
	return true
}

// commentInHiddenProject reports whether a comment sits on an issue in a
// private project the request cannot see. A missing comment is not hidden;
// the caller's own lookup answers for it.
func (h *Handler) commentInHiddenProject(r *http.Request, workspaceID string, commentID pgtype.UUID) (bool, error) {
	hidden, err := h.hiddenProjects(r, workspaceID)
	if err != nil || hidden.Empty() {
		return false, err
	}
	projectID, err := h.Queries.GetCommentIssueProjectID(r.Context(), commentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !hidden.CanSeeProject(projectID), nil
}

// sourceContextProjectVisible answers a hidden target project of a comment
// sub-issue exactly like a missing one.
func (h *Handler) sourceContextProjectVisible(r *http.Request, workspaceID, projectID pgtype.UUID) error {
	hidden, err := h.hiddenProjects(r, uuidToString(workspaceID))
	if err != nil {
		return err
	}
	if !hidden.CanSeeProject(projectID) {
		return sourceContextBadRequest("project not found")
	}
	return nil
}

// requireAssigneeCanAccessProject refuses handing an issue in a private
// project to an agent (or a squad, through its leader) whose owner cannot
// see that project (MUL-8472). Enqueue enforces the same rule
// (service.ErrProjectAccessDenied); checking at assignment turns what would be
// a run that never starts into an explicit 403 with a stable reason code.
func (h *Handler) requireAssigneeCanAccessProject(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, assigneeType pgtype.Text, assigneeID, projectID pgtype.UUID) bool {
	if !projectID.Valid || !assigneeType.Valid || !assigneeID.Valid {
		return true
	}
	ctx := r.Context()
	agentID := assigneeID
	switch assigneeType.String {
	case "agent":
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: assigneeID, WorkspaceID: workspaceID})
		if err != nil {
			return true // validateAssigneePair owns the missing-squad answer
		}
		agentID = squad.LeaderID
	default:
		return true
	}
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil || project.Visibility != projectaccess.VisibilityPrivate {
		return true
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.OwnerID.Valid {
		return true
	}
	hidden, err := projectaccess.ForPrincipals(ctx, h.Queries, workspaceID, agent.OwnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return false
	}
	if !hidden.CanSeeProject(projectID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonProjectAccessBlocked)
		return false
	}
	return true
}

// requireAttachmentVisible 404s an attachment on an issue (or a comment on an
// issue) in a project the caller cannot see.
func (h *Handler) requireAttachmentVisible(w http.ResponseWriter, r *http.Request, workspaceID string, att db.Attachment) bool {
	if att.IssueID.Valid {
		return h.requireIssueIDVisible(w, r, workspaceID, att.IssueID, http.StatusNotFound, "attachment not found")
	}
	if !att.CommentID.Valid {
		return true
	}
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return false
	}
	if hidden.Empty() {
		return true
	}
	projectID, err := h.Queries.GetCommentIssueProjectID(r.Context(), att.CommentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return false
	}
	if !hidden.CanSeeProject(projectID) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return false
	}
	return true
}

// requireChatProjectVisible gates a chat session's project context
// (MUL-8472). The context is injected into the chat agent's runs, so both the
// caller and the agent's owner must see the project; otherwise it 404s like a
// missing project.
func (h *Handler) requireChatProjectVisible(w http.ResponseWriter, r *http.Request, workspaceID string, projectID, agentID pgtype.UUID) bool {
	if !projectID.Valid {
		return true
	}
	if !h.requireProjectVisible(w, r, workspaceID, projectID, http.StatusNotFound, "project not found") {
		return false
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return false
	}
	if !agent.OwnerID.Valid {
		return true
	}
	hidden, err := projectaccess.ForPrincipals(r.Context(), h.Queries, agent.WorkspaceID, agent.OwnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return false
	}
	if !hidden.CanSeeProject(projectID) {
		writeError(w, http.StatusNotFound, "project not found")
		return false
	}
	return true
}

// hiddenProjectPredicate is the dynamic-SQL twin of the sqlc
// hidden_project_ids filter: rows outside any project always pass.
func hiddenProjectPredicate(column, param string) string {
	return "(" + column + " IS NULL OR NOT (" + column + " = ANY(" + param + "::uuid[])))"
}

// filterVisibleIssues drops issues in hidden projects from an unpaginated list.
func filterVisibleIssues(hidden projectaccess.Hidden, issues []db.Issue) []db.Issue {
	if hidden.Empty() {
		return issues
	}
	out := issues[:0:0]
	for _, issue := range issues {
		if hidden.CanSeeProject(issue.ProjectID) {
			out = append(out, issue)
		}
	}
	return out
}
