package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) isPlatformAdmin(email string) bool {
	return !isOfficialCloudDeployment() && contains(h.cfg.PlatformAdminEmails, strings.TrimSpace(email))
}

func (h *Handler) RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		if len(h.cfg.PlatformAdminEmails) == 0 || isMachineCredentialActor(r) || middleware.IsPersonalAccessToken(r.Context()) {
			writeError(w, http.StatusForbidden, "a platform administrator session is required")
			return
		}
		user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
		if err != nil && !isNotFound(err) {
			writeError(w, http.StatusServiceUnavailable, "could not verify platform access")
			return
		}
		if err != nil || !h.isPlatformAdmin(user.Email) {
			writeError(w, http.StatusForbidden, "a platform administrator session is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

const platformPageSize = 25

func platformPageCursor(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if after := r.URL.Query().Get("after"); after != "" {
		return parseUUIDOrBadRequest(w, after, "after")
	}
	return pgtype.UUID{}, true
}

func writePlatformPage[T any](w http.ResponseWriter, rows []T, id func(T) pgtype.UUID) {
	var next string
	if len(rows) > platformPageSize {
		rows = rows[:platformPageSize]
		next = uuidToString(id(rows[len(rows)-1]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "next": next})
}

func (h *Handler) ListPlatformUsers(w http.ResponseWriter, r *http.Request) {
	after, ok := platformPageCursor(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPlatformUsers(r.Context(), db.ListPlatformUsersParams{
		AfterID: after, Search: strings.TrimSpace(r.URL.Query().Get("q")), PageSize: platformPageSize + 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writePlatformPage(w, rows, func(row db.ListPlatformUsersRow) pgtype.UUID { return row.ID })
}

func (h *Handler) ListPlatformWorkspaces(w http.ResponseWriter, r *http.Request) {
	after, ok := platformPageCursor(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPlatformWorkspaces(r.Context(), db.ListPlatformWorkspacesParams{
		AfterID: after, Search: strings.TrimSpace(r.URL.Query().Get("q")), PageSize: platformPageSize + 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}
	writePlatformPage(w, rows, func(row db.ListPlatformWorkspacesRow) pgtype.UUID { return row.ID })
}

func (h *Handler) CreatePlatformWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CreateWorkspaceRequest
		OwnerEmail string `json:"owner_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	owner, ok := h.platformAssignmentUser(w, r, req.OwnerEmail)
	if !ok {
		return
	}
	h.createWorkspaceForOwner(w, r, req.CreateWorkspaceRequest, owner.ID, true)
}

func (h *Handler) platformAssignmentUser(w http.ResponseWriter, r *http.Request, email string) (db.User, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "a user email is required")
		return db.User{}, false
	}
	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "no user with this email has signed up")
		return db.User{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up the user")
		return db.User{}, false
	}
	return user, true
}

func (h *Handler) ListPlatformWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkspace(r.Context(), workspaceID); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "workspace not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load the workspace")
		}
		return
	}
	rows, err := h.Queries.ListMembersWithUser(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) AssignPlatformWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	if h.seatCapacityEnabled() {
		writeError(w, http.StatusConflict, "use workspace invitations when managed seat capacity is configured")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, ok := h.platformAssignmentUser(w, r, req.Email)
	if !ok {
		return
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign member")
		return
	}
	defer tx.Rollback(ctx)
	q := h.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForPlatformAssignment(ctx, workspaceID); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "workspace not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to verify the workspace")
		}
		return
	}
	existing, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: workspaceID})
	switch {
	case err == nil && existing.Role == "guest":
		writeError(w, http.StatusConflict, "this user already has guest access")
		return
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
		return
	case !isNotFound(err):
		writeError(w, http.StatusInternalServerError, "failed to verify membership")
		return
	}

	limit, err := h.memberLimitFor(ctx, workspaceID, "member", false)
	if errors.Is(err, errMemberLimitReached) {
		writeMemberLimitReached(w)
		return
	}
	if err != nil || limit.lock(ctx, q) != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify the workspace member limit")
		return
	}
	admission, err := limit.admit(ctx, q)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, errMemberLimitReached) {
			admission.record(ctx)
			writeMemberLimitReached(w)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to verify the workspace member limit")
		return
	}
	member, err := q.CreateMember(ctx, db.CreateMemberParams{WorkspaceID: workspaceID, UserID: user.ID, Role: "member"})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "membership changed; reload before retrying")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign member")
		return
	}
	if _, err := q.MarkUserOnboarded(ctx, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare workspace member")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign member")
		return
	}

	admission.record(ctx)
	wsID, actorID := uuidToString(workspaceID), r.Header.Get("X-User-ID")
	h.publish(protocol.EventMemberAdded, wsID, "member", actorID, map[string]any{
		"member": h.memberWithUserResponse(member, user),
	})
	h.notifyDaemonWorkspacesChanged(uuidToString(user.ID))
	slog.InfoContext(ctx, "platform workspace member assigned", "user_id", actorID, "workspace_id", wsID, "member_id", uuidToString(member.ID))
	w.WriteHeader(http.StatusNoContent)
}
