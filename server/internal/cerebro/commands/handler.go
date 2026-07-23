package commands

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
)

type ActorResolver func(r *http.Request, userID, workspaceID string) (actorType, actorID string)

type Handler struct {
	store         *Store
	actorResolver ActorResolver
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{store: NewStore(pool)} }

func (h *Handler) WithActorResolver(resolve ActorResolver) *Handler {
	h.actorResolver = resolve
	return h
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	return r
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := h.requestContext(w, r)
	if !ok {
		return
	}
	items, err := h.store.List(r.Context(), workspaceID)
	if err != nil {
		h.internalError(w, r, "failed to list commands", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": items})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := h.requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	item, err := h.store.Get(r.Context(), workspaceID, id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actorID, workspaceID, actorType, ok := h.requestContext(w, r)
	if !ok {
		return
	}
	var input CommandInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := validateCommandInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.store.Create(r.Context(), workspaceID, actorID, actorType, input)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := h.requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var input CommandInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := validateCommandInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := h.store.Update(r.Context(), workspaceID, id, input)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, _, ok := h.requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), workspaceID, id); err != nil {
		h.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requestContext(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, string, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok || !member.UserID.Valid {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return uuid.Nil, uuid.Nil, "", false
	}
	actorID, err := uuid.FromBytes(member.UserID.Bytes[:])
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid user identity")
		return uuid.Nil, uuid.Nil, "", false
	}
	workspaceID, err := uuid.Parse(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return uuid.Nil, uuid.Nil, "", false
	}
	actorType := "member"
	if h.actorResolver != nil {
		resolvedType, resolvedID := h.actorResolver(r, actorID.String(), workspaceID.String())
		if resolvedType != "member" && resolvedType != "agent" {
			writeError(w, http.StatusUnauthorized, "invalid actor identity")
			return uuid.Nil, uuid.Nil, "", false
		}
		actorID, err = uuid.Parse(resolvedID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid actor identity")
			return uuid.Nil, uuid.Nil, "", false
		}
		actorType = resolvedType
	}
	return actorID, workspaceID, actorType, true
}

func pathUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) storeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "command not found")
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "command key already exists")
			return
		}
		if pgErr.Code == "23503" || pgErr.Code == "23514" {
			writeError(w, http.StatusBadRequest, "invalid command value")
			return
		}
	}
	h.internalError(w, r, "command request failed", err)
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	slog.Error(message, append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, message)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
