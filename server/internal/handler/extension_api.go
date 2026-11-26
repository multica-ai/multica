package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The exported surface server/ee builds its handlers on. Commercial packages
// mount routes through package extension and reach request context, events
// and visibility only through these, so open-source code never imports them.

// WorkspaceID is the workspace the request is scoped to.
func (h *Handler) WorkspaceID(r *http.Request) string { return h.resolveWorkspaceID(r) }

// Actor is who the request acts as in the workspace: a member, or the agent
// running on the member's behalf.
func (h *Handler) Actor(r *http.Request, userID, workspaceID string) (actorType, actorID string) {
	return h.resolveActor(r, userID, workspaceID)
}

// Publish broadcasts a realtime event to the workspace.
func (h *Handler) Publish(eventType, workspaceID, actorType, actorID string, payload any) {
	h.publish(eventType, workspaceID, actorType, actorID, payload)
}

// RequireProjectVisible answers 404 unless the caller can see the project.
func (h *Handler) RequireProjectVisible(w http.ResponseWriter, r *http.Request, workspaceID string, projectID pgtype.UUID) bool {
	return h.requireProjectVisible(w, r, workspaceID, projectID, http.StatusNotFound, "project not found")
}

// HiddenProjects resolves the projects the caller cannot see, answering 500
// when it cannot.
func (h *Handler) HiddenProjects(w http.ResponseWriter, r *http.Request, workspaceID string) (projectaccess.Hidden, bool) {
	return h.requestHiddenProjects(w, r, workspaceID)
}

// RequireIssueVisible answers 404 unless the caller can see the issue.
func (h *Handler) RequireIssueVisible(w http.ResponseWriter, r *http.Request, workspaceID string, issueID pgtype.UUID) bool {
	return h.requireIssueIDVisible(w, r, workspaceID, issueID, http.StatusNotFound, "issue not found")
}

// WorkspaceRole is the caller's role in the workspace, answering 404 when
// they are not a member.
func (h *Handler) WorkspaceRole(w http.ResponseWriter, r *http.Request, workspaceID string) (string, bool) {
	member, ok := h.workspaceMember(w, r, workspaceID)
	return member.Role, ok
}

// IsWorkspaceAdmin reports whether role manages the workspace.
func IsWorkspaceAdmin(role string) bool { return roleAllowed(role, "owner", "admin") }

// CommentScanner sees the text of every comment created or edited through the
// comment endpoints, which is how members, agents (the CLI) and API clients
// all write comments.
type CommentScanner interface {
	// ScanComment returns the content to store, and a function to call with
	// the comment once it is saved, or nil when there is nothing to record.
	// edit is true when the content replaces an existing comment's text.
	ScanComment(ctx context.Context, workspaceID pgtype.UUID, content string, edit bool) (stored string, saved func(context.Context, db.Comment))
}

// SetCommentScanner installs s for every comment write. Call it while
// mounting routes, before the server starts serving.
func (h *Handler) SetCommentScanner(s CommentScanner) { h.commentScanner = s }

func RequireUserID(w http.ResponseWriter, r *http.Request) (string, bool) { return requireUserID(w, r) }

func WriteJSON(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }

func WriteError(w http.ResponseWriter, status int, msg string) { writeError(w, status, msg) }

func ParseUUIDOrBadRequest(w http.ResponseWriter, s, fieldName string) (pgtype.UUID, bool) {
	return parseUUIDOrBadRequest(w, s, fieldName)
}

func IsUniqueViolation(err error) bool { return isUniqueViolation(err) }
