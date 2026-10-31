package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Administrator APIs manage single-workspace daemon credentials.
func (h *Handler) enrollmentAdministrator(w http.ResponseWriter, r *http.Request) (db.Member, bool) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return db.Member{}, false
	}
	wsID := workspaceIDFromURL(r, "id")
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return db.Member{}, false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, false
	}
	return member, true
}

// enrollmentProjection never exposes token_hash, even to administrators.
func enrollmentProjection(e db.DaemonEnrollment) map[string]any {
	return map[string]any{
		"id": uuidToString(e.ID), "workspace_id": uuidToString(e.WorkspaceID),
		"daemon_id": e.DaemonID, "name": e.Name, "creator_id": uuidToString(e.CreatorID),
		"generation": e.Generation, "expires_at": e.ExpiresAt.Time,
		"revoked": e.RevokedAt.Valid, "created_at": e.CreatedAt.Time,
	}
}

func (h *Handler) CreateDaemonEnrollment(w http.ResponseWriter, r *http.Request) {
	member, ok := h.enrollmentAdministrator(w, r)
	if !ok {
		return
	}
	var req struct {
		DaemonID string `json:"daemon_id"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.DaemonID = strings.TrimSpace(req.DaemonID)
	req.Name = strings.TrimSpace(req.Name)
	if req.DaemonID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "daemon_id and name are required")
		return
	}
	if isQoderManagedDaemonID(req.DaemonID) {
		writeError(w, http.StatusBadRequest, "reserved daemon_id")
		return
	}
	raw, err := auth.GenerateDaemonEnrollmentToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate credential")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to create enrollment")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForChatSessionCreate(r.Context(), member.WorkspaceID); err != nil {
		writeError(w, 404, "workspace not found")
		return
	}
	if err := q.LockSubscriberWrites(r.Context(), db.LockSubscriberWritesParams{WorkspaceID: member.WorkspaceID, UserID: member.UserID}); err != nil {
		writeError(w, 500, "failed to authorize enrollment")
		return
	}
	live, err := q.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: member.UserID, WorkspaceID: member.WorkspaceID})
	if err != nil || !roleAllowed(live.Role, "owner", "admin") {
		writeError(w, 403, "insufficient permissions")
		return
	}
	e, err := q.CreateDaemonEnrollment(r.Context(), db.CreateDaemonEnrollmentParams{
		WorkspaceID: member.WorkspaceID, DaemonID: req.DaemonID, Name: req.Name,
		CreatorID: member.UserID, TokenHash: auth.HashToken(raw),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(30 * 24 * time.Hour), Valid: true},
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "daemon is already enrolled in this workspace")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create enrollment")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit enrollment")
		return
	}
	result := enrollmentProjection(e)
	result["token"] = raw
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) ListDaemonEnrollments(w http.ResponseWriter, r *http.Request) {
	member, ok := h.enrollmentAdministrator(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDaemonEnrollments(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list enrollments")
		return
	}
	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, 500, "failed to list runtimes")
		return
	}
	result := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		projection := enrollmentProjection(e)
		active := make([]string, 0)
		for _, rt := range runtimes {
			if !e.RevokedAt.Valid && rt.DaemonID.Valid && rt.DaemonID.String == e.DaemonID && rt.OwnerID.Valid && rt.OwnerID == e.CreatorID && rt.Status == "online" {
				active = append(active, uuidToString(rt.ID))
			}
		}
		projection["active_runtime_ids"] = active
		result = append(result, projection)
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RotateDaemonEnrollment(w http.ResponseWriter, r *http.Request) {
	h.changeDaemonEnrollment(w, r, true)
}
func (h *Handler) RevokeDaemonEnrollment(w http.ResponseWriter, r *http.Request) {
	h.changeDaemonEnrollment(w, r, false)
}

// changeDaemonEnrollment holds the exclusive enrollment lock until credential,
// runtime, task and derived-token state is committed. In-flight enrolled HTTP,
// heartbeat and RPC operations hold the matching shared lock.
func (h *Handler) changeDaemonEnrollment(w http.ResponseWriter, r *http.Request, rotate bool) {
	member, ok := h.enrollmentAdministrator(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "enrollmentId"), "enrollment_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to change enrollment")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	old, err := q.LockDaemonEnrollment(r.Context(), db.LockDaemonEnrollmentParams{ID: id, WorkspaceID: member.WorkspaceID})
	if isNotFound(err) {
		writeError(w, 404, "enrollment not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to load enrollment")
		return
	}
	if old.RevokedAt.Valid {
		// A later enrollment may reuse this host identity. Repeating revoke
		// on the historical record must never cancel that replacement host.
		if rotate {
			writeError(w, http.StatusNotFound, "enrollment not found")
		} else {
			writeJSON(w, http.StatusOK, enrollmentProjection(old))
		}
		return
	}
	var e db.DaemonEnrollment
	var raw string
	if rotate {
		raw, err = auth.GenerateDaemonEnrollmentToken()
		if err == nil {
			e, err = q.RotateDaemonEnrollment(r.Context(), db.RotateDaemonEnrollmentParams{ID: id, WorkspaceID: member.WorkspaceID, TokenHash: auth.HashToken(raw), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(30 * 24 * time.Hour), Valid: true}})
		}
	} else {
		e, err = q.RevokeDaemonEnrollment(r.Context(), db.RevokeDaemonEnrollmentParams{ID: id, WorkspaceID: member.WorkspaceID})
	}
	if isNotFound(err) {
		writeError(w, 404, "enrollment not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to change credential")
		return
	}
	result, err := cleanupEnrolledDaemon(r.Context(), q, old)
	if err != nil {
		writeError(w, 500, "failed to revoke daemon state")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit enrollment")
		return
	}
	if h.DaemonHub != nil {
		h.DaemonHub.CloseEnrollment(old.TokenHash)
	}
	for _, hash := range result.RevokedTokenHashes {
		h.DaemonTokenCache.Invalidate(r.Context(), hash)
	}
	h.publishRevocation(r.Context(), result, uuidToString(member.WorkspaceID), "user", uuidToString(member.UserID))
	projection := enrollmentProjection(e)
	if rotate {
		projection["token"] = raw
	}
	writeJSON(w, http.StatusOK, projection)
}

func cleanupEnrolledDaemon(ctx context.Context, q *db.Queries, e db.DaemonEnrollment) (revocationResult, error) {
	result := revocationResult{}
	runtimes, err := q.ListAgentRuntimes(ctx, e.WorkspaceID)
	if err != nil {
		return result, err
	}
	ids := make([]pgtype.UUID, 0)
	for _, rt := range runtimes {
		if rt.DaemonID.Valid && rt.DaemonID.String == e.DaemonID && rt.OwnerID.Valid && rt.OwnerID == e.CreatorID {
			result.Runtimes = append(result.Runtimes, rt)
			ids = append(ids, rt.ID)
		}
	}
	if len(ids) > 0 {
		result.CancelledTasks, err = q.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{RuntimeIds: ids, AgentIds: []pgtype.UUID{}})
		if err != nil {
			return result, err
		}
		for _, task := range result.CancelledTasks {
			if err := q.DeleteTaskTokensByTask(ctx, task.ID); err != nil {
				return result, err
			}
		}
		if err := service.SettleTerminalTaskState(ctx, q, result.CancelledTasks...); err != nil {
			return result, err
		}
		result.OfflineRuntimeIDs, err = q.ForceOfflineRuntimesByIDs(ctx, ids)
		if err != nil {
			return result, err
		}
	}
	result.RevokedTokenHashes, err = q.DeleteDaemonTokensByEnrollment(ctx, pgtype.Text{String: e.TokenHash, Valid: true})
	return result, err
}
