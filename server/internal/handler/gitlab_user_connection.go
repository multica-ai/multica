package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/gitlab"
)

type connectUserGitlabRequest struct {
	Token string `json:"token"`
}

type userGitlabConnectionResponse struct {
	Connected      bool   `json:"connected"`
	GitlabUserID   int64  `json:"gitlab_user_id,omitempty"`
	GitlabUsername string `json:"gitlab_username,omitempty"`
}

// ConnectUserGitlab registers a user's personal PAT for the current workspace.
// Validates by calling /user, captures GitLab user identity, encrypts the
// PAT, and upserts user_gitlab_connection.
func (h *Handler) ConnectUserGitlab(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	var req connectUserGitlabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

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

	encrypted, err := h.Secrets.Encrypt([]byte(req.Token))
	if err != nil {
		slog.Error("encrypt user pat", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}

	row, err := h.Queries.UpsertUserGitlabConnection(r.Context(), db.UpsertUserGitlabConnectionParams{
		UserID:         parseUUID(userID),
		WorkspaceID:    parseUUID(workspaceID),
		GitlabUserID:   user.ID,
		GitlabUsername: user.Username,
		PatEncrypted:   encrypted,
	})
	if err != nil {
		slog.Error("persist user_gitlab_connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist connection")
		return
	}

	writeJSON(w, http.StatusOK, userGitlabConnectionResponse{
		Connected:      true,
		GitlabUserID:   row.GitlabUserID,
		GitlabUsername: row.GitlabUsername,
	})
}

// GetUserGitlabConnection returns connected/not-connected for the current
// (user, workspace) pair. Never returns the token.
func (h *Handler) GetUserGitlabConnection(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	row, err := h.Queries.GetUserGitlabConnection(r.Context(), db.GetUserGitlabConnectionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, userGitlabConnectionResponse{Connected: false})
			return
		}
		slog.Error("read user_gitlab_connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read connection")
		return
	}
	writeJSON(w, http.StatusOK, userGitlabConnectionResponse{
		Connected:      true,
		GitlabUserID:   row.GitlabUserID,
		GitlabUsername: row.GitlabUsername,
	})
}

// DisconnectUserGitlab removes the user's PAT for the current workspace.
// Returns 204 even when the row didn't exist (idempotent).
func (h *Handler) DisconnectUserGitlab(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	if err := h.Queries.DeleteUserGitlabConnection(r.Context(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		slog.Error("delete user_gitlab_connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to disconnect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
