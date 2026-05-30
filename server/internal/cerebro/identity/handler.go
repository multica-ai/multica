package identity

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler exposes the per-workspace Google auth settings as HTTP endpoints.
//
// Routes:
//   GET /api/cerebro/workspaces/{id}/auth-settings    — any workspace member
//   PUT /api/cerebro/workspaces/{id}/auth-settings    — owner/admin only
//                                                       (enforced by middleware
//                                                       at the route layer)
type Handler struct {
	Service *Service
}

// NewHandler builds a Handler around the given Service.
func NewHandler(svc *Service) *Handler {
	return &Handler{Service: svc}
}

// Get returns the workspace's saved auth settings, or the empty default when
// no row exists yet.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	settings, err := h.Service.Get(r.Context(), workspaceID)
	if err != nil {
		slog.Error("get cerebro workspace auth settings failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load auth settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// Update upserts the workspace's auth settings.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var req UpdateAuthSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings, err := h.Service.Update(r.Context(), workspaceID, actorID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRole):
			writeError(w, http.StatusBadRequest, "default_role must be one of owner, admin, member")
			return
		case errors.Is(err, ErrInvalidDomain):
			writeError(w, http.StatusBadRequest, "google_signup_domains must contain non-empty domain values without '@' or whitespace")
			return
		}
		slog.Error("update cerebro workspace auth settings failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save auth settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// workspaceIDFromRequest resolves the workspace UUID from the standard X-
// Workspace-ID header (set by the multi-tenancy middleware) and falls back
// to the {id} URL param when the route is workspace-scoped via the path.
func workspaceIDFromRequest(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
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
