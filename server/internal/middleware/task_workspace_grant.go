package middleware

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cross-workspace task routes are explicit and capability-scoped. Reject all
// other routes before a handler can use a target member identity.
var issueDetailRef = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[A-Za-z][A-Za-z0-9]*-[0-9]+)$`)

func taskWorkspaceRouteKind(r *http.Request) string {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "/api/issues" {
		if r.Method == http.MethodGet || r.Method == http.MethodPost {
			return "issue"
		}
	}
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/issues/") && issueDetailRef.MatchString(strings.TrimPrefix(path, "/api/issues/")) {
		return "issue"
	}
	if path == "/api/runtimes" && r.Method == http.MethodGet {
		return "runtime_list"
	}
	if path == "/api/agents" && r.Method == http.MethodPost {
		return "agent_create"
	}
	return ""
}

func authorizeTaskWorkspaceTarget(w http.ResponseWriter, r *http.Request, q *db.Queries, token db.TaskToken, targetID string) (string, bool) {
	deny := func(reason string) (string, bool) {
		slog.Warn("task token cross-workspace request rejected", "task_id", uuidToString(token.TaskID), "agent_id", uuidToString(token.AgentID), "home_workspace_id", uuidToString(token.WorkspaceID), "target_workspace_id", targetID, "reason", reason)
		writeError(w, http.StatusForbidden, errTaskTokenWorkspaceMismatch)
		return "", false
	}
	kind := taskWorkspaceRouteKind(r)
	if kind == "" {
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
	if kind != "issue" && !grant.CanCreateAgents {
		return deny("agent creation capability")
	}
	slog.Info("task token cross-workspace request authorized", "task_id", uuidToString(token.TaskID), "agent_id", uuidToString(token.AgentID), "runtime_owner_user_id", uuidToString(token.UserID), "originator_user_id", uuidToString(grant.OriginatorUserID), "home_workspace_id", uuidToString(token.WorkspaceID), "target_workspace_id", targetID, "method", r.Method, "path", r.URL.Path)
	if kind != "issue" {
		return uuidToString(grant.OriginatorUserID), true
	}
	return uuidToString(grant.OwnerID), true
}
