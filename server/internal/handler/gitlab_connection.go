package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/gitlab"
)

type connectGitlabRequest struct {
	Project string `json:"project"` // numeric ID or "group/app" path
	Token   string `json:"token"`   // GitLab PAT (api scope)
}

type gitlabConnectionResponse struct {
	WorkspaceID          string `json:"workspace_id"`
	GitlabProjectID      int64  `json:"gitlab_project_id"`
	GitlabProjectPath    string `json:"gitlab_project_path"`
	ServiceTokenUserID   int64  `json:"service_token_user_id"`
	ServiceTokenUsername string `json:"service_token_username,omitempty"`
	ConnectionStatus     string `json:"connection_status"`
	StatusMessage        string `json:"status_message,omitempty"`
}

// ConnectGitlabWorkspace validates a GitLab service PAT + project reference
// and persists an encrypted workspace_gitlab_connection row on success.
func (h *Handler) ConnectGitlabWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req connectGitlabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" || req.Project == "" {
		writeError(w, http.StatusBadRequest, "project and token are required")
		return
	}

	// Validate token: who does it belong to?
	user, err := h.Gitlab.CurrentUser(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, gitlab.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "gitlab token is invalid")
			return
		}
		slog.Error("gitlab CurrentUser failed", "error", err)
		writeError(w, http.StatusBadGateway, "gitlab /user call failed")
		return
	}

	// Validate project: does the token have access?
	project, err := h.Gitlab.GetProject(r.Context(), req.Token, req.Project)
	if err != nil {
		switch {
		case errors.Is(err, gitlab.ErrNotFound):
			writeError(w, http.StatusNotFound, "gitlab project not found or token lacks access")
			return
		case errors.Is(err, gitlab.ErrForbidden):
			writeError(w, http.StatusForbidden, "gitlab token lacks api scope on project")
			return
		default:
			slog.Error("gitlab GetProject failed", "error", err)
			writeError(w, http.StatusBadGateway, "gitlab /projects call failed")
			return
		}
	}

	encrypted, err := h.Secrets.Encrypt([]byte(req.Token))
	if err != nil {
		slog.Error("encrypt gitlab token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}

	row, err := h.Queries.CreateWorkspaceGitlabConnection(r.Context(), db.CreateWorkspaceGitlabConnectionParams{
		WorkspaceID:           parseUUID(workspaceID),
		GitlabProjectID:       project.ID,
		GitlabProjectPath:     project.PathWithNamespace,
		ServiceTokenEncrypted: encrypted,
		ServiceTokenUserID:    user.ID,
		ConnectionStatus:      "connecting",
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "gitlab is already connected for this workspace")
			return
		}
		slog.Error("persist workspace_gitlab_connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist connection")
		return
	}

	// Dispatch initial sync in the background. The goroutine flips the
	// connection_status to 'connected' (or 'error' with a message) when done.
	// Use a fresh context — the request context will be cancelled before the
	// sync finishes.
	go func(token string, projectID int64, wsID string) {
		syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := gitlabsync.RunInitialSync(syncCtx, gitlabsync.SyncDeps{
			Queries: h.Queries,
			Client:  h.Gitlab,
		}, gitlabsync.RunInitialSyncInput{
			WorkspaceID: wsID,
			ProjectID:   projectID,
			Token:       token,
		}); err != nil {
			slog.Error("initial gitlab sync failed",
				"error", err,
				"workspace_id", wsID,
				"project_id", projectID)
		}
	}(req.Token, project.ID, workspaceID)

	writeJSON(w, http.StatusOK, gitlabConnectionResponse{
		WorkspaceID:          uuidToString(row.WorkspaceID),
		GitlabProjectID:      row.GitlabProjectID,
		GitlabProjectPath:    row.GitlabProjectPath,
		ServiceTokenUserID:   row.ServiceTokenUserID,
		ServiceTokenUsername: user.Username,
		ConnectionStatus:     row.ConnectionStatus,
	})
}

// DisconnectGitlabWorkspace removes the workspace's GitLab connection and
// cascade-truncates all derived cache rows inside a single transaction.
func (h *Handler) DisconnectGitlabWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	workspaceID := chi.URLParam(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	// Cascade-truncate the cache before removing the connection row.
	// The cache rows are derived from GitLab; once disconnected, they're
	// unreachable garbage. Inside a transaction so partial failure doesn't
	// leave orphan rows.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("begin tx for cache truncate", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	wsUUID := parseUUID(workspaceID)
	if err := qtx.DeleteWorkspaceCachedIssues(r.Context(), wsUUID); err != nil {
		slog.Error("delete cached issues failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}
	if err := qtx.DeleteWorkspaceGitlabLabels(r.Context(), wsUUID); err != nil {
		slog.Error("delete cached labels failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}
	if err := qtx.DeleteWorkspaceGitlabMembers(r.Context(), wsUUID); err != nil {
		slog.Error("delete cached members failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}
	if err := qtx.DeleteWorkspaceGitlabConnection(r.Context(), wsUUID); err != nil {
		slog.Error("delete workspace_gitlab_connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to disconnect")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit cache truncate", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetGitlabWorkspaceConnection returns sanitized connection status (never the token).
func (h *Handler) GetGitlabWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	workspaceID := chi.URLParam(r, "id")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	row, err := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "gitlab is not connected")
			return
		}
		slog.Error("read workspace_gitlab_connection failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read connection")
		return
	}

	statusMessage := ""
	if row.StatusMessage.Valid {
		statusMessage = row.StatusMessage.String
	}
	writeJSON(w, http.StatusOK, gitlabConnectionResponse{
		WorkspaceID:        uuidToString(row.WorkspaceID),
		GitlabProjectID:    row.GitlabProjectID,
		GitlabProjectPath:  row.GitlabProjectPath,
		ServiceTokenUserID: row.ServiceTokenUserID,
		ConnectionStatus:   row.ConnectionStatus,
		StatusMessage:      statusMessage,
	})
}
