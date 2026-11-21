package handler

import (
	"fmt"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ResolveProjectMove is scoped to the source header, not the current workspace.
// Source-only readers learn that an item moved, never its new location.
func (h *Handler) ResolveProjectMove(w http.ResponseWriter, r *http.Request) {
	source := h.resolveWorkspaceID(r)
	if isMachineCredentialActor(r) {
		writeError(w, 403, "move resolution requires a human actor")
		return
	}
	if actor, _ := h.resolveActor(r, requestUserID(r), source); actor == "agent" {
		writeError(w, 403, "move resolution requires a human actor")
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, source, "workspace not found")
	if !ok {
		return
	}
	issueKey, projectID := r.URL.Query().Get("issue"), r.URL.Query().Get("project")
	if (issueKey == "") == (projectID == "") {
		writeError(w, 400, "provide exactly one issue key or project id")
		return
	}
	user := requestUserID(r)
	if issueKey != "" {
		parts := splitIdentifier(issueKey)
		if parts == nil {
			writeError(w, 400, "invalid issue key")
			return
		}
		moved, err := h.Queries.ResolveIssueMove(r.Context(), db.ResolveIssueMoveParams{SourceWorkspaceID: member.WorkspaceID, Prefix: parts.prefix, Number: parts.number})
		if err != nil {
			writeError(w, 404, "move not found")
			return
		}
		out := map[string]any{"moved": true, "moved_at": timestampToString(moved.MovedAt)}
		if _, err := h.getWorkspaceMember(r.Context(), user, uuidToString(moved.WorkspaceID)); err == nil {
			prefix := moved.IssuePrefix
			if prefix == "" {
				prefix = legacyIssuePrefixFromName(moved.Name)
			}
			out["issue_id"] = uuidToString(moved.ID)
			out["workspace_slug"] = moved.Slug
			out["identifier"] = fmt.Sprintf("%s-%d", prefix, moved.Number)
		}
		writeJSON(w, 200, out)
		return
	}
	id, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}
	moved, err := h.Queries.ResolveProjectMove(r.Context(), db.ResolveProjectMoveParams{SourceWorkspaceID: member.WorkspaceID, ProjectID: id})
	if err != nil {
		writeError(w, 404, "move not found")
		return
	}
	out := map[string]any{"moved": true, "moved_at": timestampToString(moved.MovedAt)}
	if _, err := h.getWorkspaceMember(r.Context(), user, uuidToString(moved.WorkspaceID)); err == nil {
		out["project_id"] = uuidToString(moved.ID)
		out["workspace_slug"] = moved.Slug
	}
	writeJSON(w, 200, out)
}
