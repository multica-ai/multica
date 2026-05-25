package handler

// CEREBRO-PATCH(user-infisical-folders): admin-managed allow-list of Infisical
// folders a member may grant to their agents. Agent folder grants are gated
// against the agent owner's entry in this list, both on save and at spawn, so
// an admin controls which keys a given user can hand to their agents.

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
)

type replaceMemberInfisicalFoldersRequest struct {
	Folders []AgentInfisicalFolderResponse `json:"folders"`
}

// ListMemberInfisicalFolders returns the Infisical folders a member is allowed
// to grant to their agents. Owner/admin only.
func (h *Handler) ListMemberInfisicalFolders(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	target, ok := h.loadTargetMember(w, r, caller)
	if !ok {
		return
	}
	rows, err := h.listUserInfisicalFolders(r, target.WorkspaceID, target.UserID)
	if err != nil {
		slog.Error("failed to list member infisical folders", append(logger.RequestAttrs(r), "user_id", uuidToString(target.UserID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list infisical folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": rows})
}

// ReplaceMemberInfisicalFolders overwrites the member's allow-list. Owner/admin only.
func (h *Handler) ReplaceMemberInfisicalFolders(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	target, ok := h.loadTargetMember(w, r, caller)
	if !ok {
		return
	}
	if h.CerebroQueries == nil || h.TxStarter == nil {
		writeError(w, http.StatusServiceUnavailable, "infisical folders store is not configured")
		return
	}

	var req replaceMemberInfisicalFoldersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized, err := normalizeAgentInfisicalFolders(req.Folders)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("failed to begin member infisical folders replace", append(logger.RequestAttrs(r), "user_id", uuidToString(target.UserID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	defer tx.Rollback(r.Context())

	q := h.CerebroQueries.WithTx(tx)
	if err := q.DeleteUserInfisicalFolders(r.Context(), cerebrodb.DeleteUserInfisicalFoldersParams{
		WorkspaceID: target.WorkspaceID,
		UserID:      target.UserID,
	}); err != nil {
		slog.Error("failed to clear member infisical folders", append(logger.RequestAttrs(r), "user_id", uuidToString(target.UserID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	out := make([]AgentInfisicalFolderResponse, 0, len(normalized))
	for _, folder := range normalized {
		row, err := q.InsertUserInfisicalFolder(r.Context(), cerebrodb.InsertUserInfisicalFolderParams{
			WorkspaceID: target.WorkspaceID,
			UserID:      target.UserID,
			Environment: folder.Environment,
			SecretPath:  folder.SecretPath,
		})
		if err != nil {
			slog.Error("failed to insert member infisical folder", append(logger.RequestAttrs(r), "user_id", uuidToString(target.UserID), "environment", folder.Environment, "secret_path", folder.SecretPath, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
			return
		}
		out = append(out, AgentInfisicalFolderResponse{
			ID:          uuidToString(row.ID),
			Environment: row.Environment,
			SecretPath:  row.SecretPath,
		})
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("failed to commit member infisical folders replace", append(logger.RequestAttrs(r), "user_id", uuidToString(target.UserID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	slog.Info("member infisical folders updated", "user_id", uuidToString(target.UserID), "count", len(out), "by", uuidToString(caller.UserID))
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// listUserInfisicalFolders returns the allow-list for a user as folder DTOs.
func (h *Handler) listUserInfisicalFolders(r *http.Request, workspaceID, userID pgtype.UUID) ([]AgentInfisicalFolderResponse, error) {
	if h.CerebroQueries == nil {
		return []AgentInfisicalFolderResponse{}, nil
	}
	rows, err := h.CerebroQueries.ListUserInfisicalFolders(r.Context(), cerebrodb.ListUserInfisicalFoldersParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AgentInfisicalFolderResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentInfisicalFolderResponse{
			ID:          uuidToString(row.ID),
			Environment: row.Environment,
			SecretPath:  row.SecretPath,
		})
	}
	return out, nil
}

// userAllowedInfisicalFolderSet returns a lookup set keyed by "environment\x00secret_path"
// of the folders the user is allowed to grant. Used to gate agent folder grants.
func (h *Handler) userAllowedInfisicalFolderSet(r *http.Request, workspaceID, userID pgtype.UUID) (map[string]struct{}, error) {
	rows, err := h.listUserInfisicalFolders(r, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		allowed[infisicalFolderKey(row.Environment, row.SecretPath)] = struct{}{}
	}
	return allowed, nil
}

func infisicalFolderKey(environment, secretPath string) string {
	return environment + "\x00" + secretPath
}

// partitionInfisicalFoldersByAllowList splits folders into those present in the
// owner's allow-list set and those that are not. Pure helper shared by the
// save-time gate (rejects denied) and the spawn-time gate (skips denied).
func partitionInfisicalFoldersByAllowList(folders []AgentInfisicalFolderResponse, allowed map[string]struct{}) (ok, denied []AgentInfisicalFolderResponse) {
	for _, f := range folders {
		if _, isAllowed := allowed[infisicalFolderKey(f.Environment, f.SecretPath)]; isAllowed {
			ok = append(ok, f)
		} else {
			denied = append(denied, f)
		}
	}
	return ok, denied
}
