package sessionmode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Handler struct{ store *Store }

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

func canManageMember(member db.Member) bool {
	role := strings.ToLower(strings.TrimSpace(member.Role))
	return role == "owner" || role == "admin"
}

func parseManagedMode(raw string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(raw)))
	switch mode {
	case Plan, Build, Research, Review:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid mode %q", raw)
	}
}

func actorAndWorkspace(member db.Member, workspaceID string) (pgtype.UUID, pgtype.UUID, error) {
	if !member.UserID.Valid {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("member has no user id")
	}
	workspace, err := util.ParseUUID(workspaceID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("invalid workspace id")
	}
	return member.UserID, workspace, nil
}

func (h *Handler) requestContext(w http.ResponseWriter, r *http.Request, write bool) (pgtype.UUID, pgtype.UUID, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeModeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if write && r.Header.Get("X-Actor-Source") == "task_token" {
		writeModeError(w, http.StatusForbidden, "agents cannot manage Mode configuration")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if write && !canManageMember(member) {
		writeModeError(w, http.StatusForbidden, "workspace owner or admin required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	actorID, workspaceID, err := actorAndWorkspace(member, middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return actorID, workspaceID, true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requestContext(w, r, false)
	if !ok {
		return
	}
	records, err := h.store.List(r.Context(), workspaceID)
	if err != nil {
		writeModeError(w, http.StatusInternalServerError, "failed to load Mode configuration")
		return
	}
	writeModeJSON(w, http.StatusOK, map[string]any{"modes": records})
}

func (h *Handler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, ok := h.requestContext(w, r, true)
	if !ok {
		return
	}
	mode, err := parseManagedMode(chi.URLParam(r, "mode"))
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var config Config
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		writeModeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	record, err := h.store.UpdateDraft(r.Context(), workspaceID, actorID, mode, config)
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeModeJSON(w, http.StatusOK, record)
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, ok := h.requestContext(w, r, true)
	if !ok {
		return
	}
	mode, err := parseManagedMode(chi.URLParam(r, "mode"))
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Description string `json:"description"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
			writeModeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	record, err := h.store.Publish(r.Context(), workspaceID, actorID, mode, strings.TrimSpace(req.Description))
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeModeJSON(w, http.StatusOK, record)
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, ok := h.requestContext(w, r, true)
	if !ok {
		return
	}
	mode, err := parseManagedMode(chi.URLParam(r, "mode"))
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		writeModeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	version, err := strconv.Atoi(req.Version)
	if err != nil || version < 1 {
		writeModeError(w, http.StatusBadRequest, "version must be a positive integer")
		return
	}
	record, err := h.store.Restore(r.Context(), workspaceID, actorID, mode, version)
	if err != nil {
		writeModeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeModeJSON(w, http.StatusOK, record)
}

func writeModeError(w http.ResponseWriter, status int, message string) {
	writeModeJSON(w, status, map[string]string{"error": message})
}

func writeModeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
