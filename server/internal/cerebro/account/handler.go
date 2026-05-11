package account

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type Handler struct {
	Service *Service
}

func New(cerebro *cerebrodb.Queries, bus *events.Bus) *Handler {
	return &Handler{Service: NewService(cerebro, bus)}
}

type createAccountRequest struct {
	Provider      string `json:"provider"`
	LoginIdentity string `json:"login_identity"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	accounts, err := h.Service.List(r.Context(), workspaceID)
	if err != nil {
		slog.Error("list cerebro accounts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}
	resp := make([]accountResponse, len(accounts))
	for i, a := range accounts {
		resp[i] = accountResponseFromModel(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := h.accountIDs(w, r)
	if !ok {
		return
	}
	a, err := h.Service.Get(r.Context(), workspaceID, accountID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountResponseFromModel(a))
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
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := h.Service.Create(r.Context(), workspaceID, actorID, req.Provider, req.LoginIdentity)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, accountResponseFromModel(a))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := h.accountIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.Service.Delete(r.Context(), workspaceID, actorID, accountID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) accountIDs(w http.ResponseWriter, r *http.Request) (workspaceID, accountID pgtype.UUID, ok bool) {
	workspaceID, ok = workspaceIDFromRequest(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	accountID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, accountID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAccountNotFound):
		writeError(w, http.StatusNotFound, "account not found")
	case errors.Is(err, ErrInvalidProvider):
		writeError(w, http.StatusBadRequest, "provider is required")
	case errors.Is(err, ErrInvalidLoginIdentity):
		writeError(w, http.StatusBadRequest, "login_identity is required")
	case errors.Is(err, ErrAccountAlreadyExists):
		writeError(w, http.StatusConflict, "account already exists for this provider and login_identity")
	default:
		slog.Error("cerebro accounts request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "accounts request failed")
	}
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
