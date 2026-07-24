package roles

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

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

type createRoleRequest struct {
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Permissions RolePermissions `json:"permissions"`
}

type updateRoleRequest struct {
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Permissions *RolePermissions `json:"permissions"`
}

type assignRoleRequest struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	ExpiresAt   string `json:"expires_at"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	includeArchived, _ := strconv.ParseBool(r.URL.Query().Get("include_archived"))
	roles, err := h.Service.List(r.Context(), workspaceID, includeArchived)
	if err != nil {
		slog.Error("list cerebro roles failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	resp := make([]roleResponse, len(roles))
	for i, role := range roles {
		resp[i] = roleResponseFromModel(role)
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
	var req createRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, err := h.Service.Create(r.Context(), workspaceID, actorID, req.Name, req.Description, req.Permissions)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, roleResponseFromModel(role))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, roleID, ok := h.roleIDs(w, r)
	if !ok {
		return
	}
	role, err := h.Service.Get(r.Context(), workspaceID, roleID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, roleResponseFromModel(role))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, roleID, ok := h.roleIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, err := h.Service.Update(r.Context(), workspaceID, actorID, roleID, req.Name, req.Description, req.Permissions)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, roleResponseFromModel(role))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID, roleID, ok := h.roleIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.Service.Archive(r.Context(), workspaceID, actorID, roleID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Assign(w http.ResponseWriter, r *http.Request) {
	workspaceID, roleID, ok := h.roleIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var req assignRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	subjectID, err := util.ParseUUID(req.SubjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		expiresAt = &parsed
	}
	assignment, err := h.Service.Assign(r.Context(), workspaceID, actorID, roleID, subjectID, req.SubjectType, expiresAt)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, roleAssignmentResponseFromAssign(assignment))
}

func (h *Handler) Unassign(w http.ResponseWriter, r *http.Request) {
	workspaceID, roleID, ok := h.roleIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	subjectType := chi.URLParam(r, "subjectType")
	subjectID, err := util.ParseUUID(chi.URLParam(r, "subjectId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}
	if _, err := h.Service.Unassign(r.Context(), workspaceID, actorID, roleID, subjectID, subjectType); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAssignments(w http.ResponseWriter, r *http.Request) {
	workspaceID, roleID, ok := h.roleIDs(w, r)
	if !ok {
		return
	}
	assignments, err := h.Service.ListAssignments(r.Context(), workspaceID, roleID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	resp := make([]roleAssignmentResponse, len(assignments))
	for i, a := range assignments {
		resp[i] = roleAssignmentResponseFromNamedRow(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) roleIDs(w http.ResponseWriter, r *http.Request) (workspaceID, roleID pgtype.UUID, ok bool) {
	workspaceID, ok = workspaceIDFromRequest(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	roleID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid role id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, roleID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRoleNotFound):
		writeError(w, http.StatusNotFound, "role not found")
	case errors.Is(err, ErrAssignmentNotFound):
		writeError(w, http.StatusNotFound, "role assignment not found")
	case errors.Is(err, ErrInvalidRoleName):
		writeError(w, http.StatusBadRequest, "name is required")
	case errors.Is(err, ErrInvalidPermissions):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrRoleArchived):
		writeError(w, http.StatusConflict, "role is archived")
	case errors.Is(err, ErrInvalidSubjectType):
		writeError(w, http.StatusBadRequest, "subject_type must be member or agent")
	case errors.Is(err, ErrSubjectNotFound):
		writeError(w, http.StatusBadRequest, "subject not found in workspace")
	default:
		if status, msg, ok := mapPgError(err); ok {
			writeError(w, status, msg)
			return
		}
		slog.Error("cerebro roles request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "roles request failed")
	}
}

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
		return http.StatusConflict, "a role with that name already exists", true
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
