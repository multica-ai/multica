package middleware

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Only the first issue read/create workflows are cross-workspace capable.
// This rejects other routes before their handlers can use the runtime owner's
// member identity as though the request came from a human.
var issueDetailRef = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[A-Za-z][A-Za-z0-9]*-[0-9]+)$`)

func taskWorkspaceRouteAllowed(r *http.Request) bool {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "/api/issues" {
		return r.Method == http.MethodGet || r.Method == http.MethodPost
	}
	return r.Method == http.MethodGet && strings.HasPrefix(path, "/api/issues/") &&
		issueDetailRef.MatchString(strings.TrimPrefix(path, "/api/issues/"))
}

func authorizeTaskWorkspaceTarget(w http.ResponseWriter, r *http.Request, q *db.Queries, token db.TaskToken, targetID string) (string, bool) {
	deny := func(reason string) (string, bool) {
		slog.Warn("task token cross-workspace request rejected", "task_id", uuidToString(token.TaskID), "agent_id", uuidToString(token.AgentID), "home_workspace_id", uuidToString(token.WorkspaceID), "target_workspace_id", targetID, "reason", reason)
		writeError(w, http.StatusForbidden, errTaskTokenWorkspaceMismatch)
		return "", false
	}
	if !taskWorkspaceRouteAllowed(r) {
		return deny("route")
	}
	targetUUID, err := util.ParseUUID(targetID)
	if err != nil {
		return deny("target")
	}
	grant, err := q.AuthorizeTaskWorkspaceGrant(r.Context(), db.AuthorizeTaskWorkspaceGrantParams{
		TaskID: token.TaskID, AgentID: token.AgentID, HomeWorkspaceID: token.WorkspaceID, TargetWorkspaceID: targetUUID,
	})
	if err != nil || !grant.OwnerID.Valid || !grant.OriginatorUserID.Valid {
		if err != nil {
			slog.Debug("cross-workspace grant lookup failed", "error", err)
		}
		return deny("grant or principal")
	}
	slog.Info("task token cross-workspace request authorized", "task_id", uuidToString(token.TaskID), "agent_id", uuidToString(token.AgentID), "runtime_owner_user_id", uuidToString(token.UserID), "originator_user_id", uuidToString(grant.OriginatorUserID), "home_workspace_id", uuidToString(token.WorkspaceID), "target_workspace_id", targetID, "method", r.Method, "path", r.URL.Path)
	return uuidToString(grant.OwnerID), true
}
