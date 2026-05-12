package groups

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Handler struct {
	Service *Service
}

func New(cerebro *cerebrodb.Queries, upstream *db.Queries, bus *events.Bus) *Handler {
	return &Handler{Service: NewService(cerebro, upstream, bus)}
}

type createGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type updateGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type addMemberRequest struct {
	UserID string `json:"user_id"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	groups, err := h.Service.List(r.Context(), workspaceID)
	if err != nil {
		slog.Error("list cerebro groups failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}

	resp := make([]groupResponse, len(groups))
	for i, group := range groups {
		resp[i] = groupResponseFromModel(group)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	group, err := h.Service.Create(r.Context(), workspaceID, actorID, req.Name, req.Description)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, groupResponseFromModel(group))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, groupID, ok := h.groupIDs(w, r)
	if !ok {
		return
	}
	group, err := h.Service.Get(r.Context(), workspaceID, groupID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, groupResponseFromModel(group))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, groupID, ok := h.groupIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	var req updateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	group, err := h.Service.Update(r.Context(), workspaceID, actorID, groupID, req.Name, req.Description)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, groupResponseFromModel(group))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID, groupID, ok := h.groupIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.Service.Delete(r.Context(), workspaceID, actorID, groupID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	workspaceID, groupID, ok := h.groupIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, err := util.ParseUUID(req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	member, err := h.Service.AddMember(r.Context(), workspaceID, actorID, groupID, userID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, groupMemberResponseFromAdd(member))
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	workspaceID, groupID, ok := h.groupIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	userID, err := util.ParseUUID(chi.URLParam(r, "userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if _, err := h.Service.RemoveMember(r.Context(), workspaceID, actorID, groupID, userID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID, groupID, ok := h.groupIDs(w, r)
	if !ok {
		return
	}
	members, err := h.Service.ListMembers(r.Context(), workspaceID, groupID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	resp := make([]groupMemberResponse, len(members))
	for i, member := range members {
		resp[i] = groupMemberResponseFromList(member)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) groupIDs(w http.ResponseWriter, r *http.Request) (workspaceID, groupID pgtype.UUID, ok bool) {
	workspaceID, ok = workspaceIDFromRequest(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	groupID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, groupID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrGroupNotFound):
		writeError(w, http.StatusNotFound, "group not found")
	case errors.Is(err, ErrGroupMemberNotFound):
		writeError(w, http.StatusNotFound, "group member not found")
	case errors.Is(err, ErrInvalidGroupName):
		writeError(w, http.StatusBadRequest, "name is required")
	case errors.Is(err, ErrUserNotWorkspaceMember):
		writeError(w, http.StatusBadRequest, "user is not a workspace member")
	default:
		if status, msg, ok := mapPgError(err); ok {
			writeError(w, status, msg)
			return
		}
		slog.Error("cerebro groups request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "groups request failed")
	}
}

// mapPgError surfaces common postgres constraint violations as 400/409 with a
// field hint instead of an opaque 500. Without this, a NOT NULL on description
// (or a unique-name collision) showed up to users as "groups request failed".
func mapPgError(err error) (int, string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return 0, "", false
	}
	field := pgErr.ColumnName
	switch pgErr.Code {
	case "23502":
		if field == "" {
			return http.StatusBadRequest, "missing required field", true
		}
		return http.StatusBadRequest, field + " is required", true
	case "23505":
		return http.StatusConflict, "a group with that name already exists", true
	case "22001":
		return http.StatusBadRequest, "value too long for field" + fieldSuffix(field), true
	case "23514":
		return http.StatusBadRequest, "invalid value for field" + fieldSuffix(field), true
	}
	return 0, "", false
}

func fieldSuffix(field string) string {
	if field == "" {
		return ""
	}
	return " " + field
}

func workspaceIDFromRequest(w http.ResponseWriter, r *http.Request) (workspaceID pgtype.UUID, ok bool) {
	id := middleware.WorkspaceIDFromContext(r.Context())
	if id == "" {
		id = chi.URLParam(r, "id")
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return pgtype.UUID{}, false
	}
	return parsed, true
}

func userIDFromRequest(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id := r.Header.Get("X-User-ID")
	if id == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	return parsed, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
