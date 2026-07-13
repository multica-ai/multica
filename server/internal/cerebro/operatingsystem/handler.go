package operatingsystem

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type handlerService interface {
	GetSettings(context.Context, pgtype.UUID) (SettingsResponse, error)
	UpdateSettings(context.Context, pgtype.UUID, Terminology) (SettingsResponse, error)
	CreateStrategyItem(context.Context, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error)
	ListStrategyItems(context.Context, pgtype.UUID) ([]StrategyItemResponse, error)
	UpdateStrategyItem(context.Context, pgtype.UUID, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error)
	DeleteStrategyItem(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	UpsertRock(context.Context, pgtype.UUID, RockInput) error
	ListRocks(context.Context, pgtype.UUID) ([]RockResponse, error)
	DeleteRock(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	CreateConnection(context.Context, pgtype.UUID, string, pgtype.UUID, ObjectConnectionInput) (ObjectConnectionResponse, error)
	ListConnections(context.Context, pgtype.UUID, string, string) ([]ObjectConnectionResponse, error)
	DeleteConnection(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
}

type Handler struct {
	service handlerService
}

func NewHandler(service handlerService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) MountRoutes(r chi.Router) {
	r.Route("/api/cerebro/operating-system/settings", func(r chi.Router) { r.Get("/", h.GetSettings); r.Put("/", h.UpdateSettings) })
	r.Route("/api/cerebro/strategy-items", func(r chi.Router) {
		r.Get("/", h.ListStrategyItems)
		r.Post("/", h.CreateStrategyItem)
		r.Put("/{id}", h.UpdateStrategyItem)
		r.Delete("/{id}", h.DeleteStrategyItem)
	})
	r.Route("/api/cerebro/rocks", func(r chi.Router) {
		r.Get("/", h.ListRocks)
		r.Post("/", h.UpsertRock)
		r.Put("/{projectID}", h.UpsertRock)
		r.Delete("/{projectID}", h.DeleteRock)
	})
	r.Route("/api/cerebro/object-connections", func(r chi.Router) {
		r.Get("/", h.ListConnections)
		r.Post("/", h.CreateConnection)
		r.Delete("/{id}", h.DeleteConnection)
	})
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	out, err := h.service.GetSettings(r.Context(), ws)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Terminology Terminology `json:"terminology"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateSettings(r.Context(), ws, input.Terminology)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) ListStrategyItems(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListStrategyItems(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"strategy_items": items}, err)
}

func (h *Handler) CreateStrategyItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input StrategyItemInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreateStrategyItem(r.Context(), ws, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) UpdateStrategyItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input StrategyItemInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateStrategyItem(r.Context(), ws, id, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) DeleteStrategyItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteStrategyItem(r.Context(), ws, id)
	writeDelete(w, deleted, err)
}

func (h *Handler) ListRocks(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListRocks(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"rocks": items}, err)
}

func (h *Handler) UpsertRock(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input RockInput
	if !decodeBody(w, r, &input) {
		return
	}
	if raw := strings.TrimSpace(chi.URLParam(r, "projectID")); raw != "" {
		if _, err := util.ParseUUID(raw); err != nil {
			writeError(w, http.StatusBadRequest, "invalid projectID")
			return
		}
		input.ProjectID = raw
	}
	err := h.service.UpsertRock(r.Context(), ws, input)
	writeResult(w, http.StatusOK, map[string]bool{"saved": err == nil}, err)
}

func (h *Handler) DeleteRock(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteRock(r.Context(), ws, projectID)
	writeDelete(w, deleted, err)
}

func (h *Handler) ListConnections(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	objectType := strings.TrimSpace(r.URL.Query().Get("object_type"))
	objectID := strings.TrimSpace(r.URL.Query().Get("object_id"))
	if objectType == "" {
		writeError(w, http.StatusBadRequest, "object_type is required")
		return
	}
	if _, err := util.ParseUUID(objectID); err != nil {
		writeError(w, http.StatusBadRequest, "object_id must be a UUID")
		return
	}
	items, err := h.service.ListConnections(r.Context(), ws, objectType, objectID)
	writeResult(w, http.StatusOK, map[string]any{"connections": items}, err)
}

func (h *Handler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	ws, memberID, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input ObjectConnectionInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreateConnection(r.Context(), ws, "member", memberID, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) DeleteConnection(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteConnection(r.Context(), ws, id)
	writeDelete(w, deleted, err)
}

func requestScope(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "workspace member required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	ws, err := util.ParseUUID(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return ws, member.ID, true
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(strings.TrimSpace(chi.URLParam(r, name)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return id, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, status, value)
}

func writeDelete(w http.ResponseWriter, deleted bool, err error) {
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProjectNotInWorkspace), errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, err.Error())
	case strings.Contains(err.Error(), "duplicate connection"):
		writeError(w, http.StatusConflict, "connection already exists")
	case isInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "operating system request failed")
	}
}

func isInputError(err error) bool {
	message := err.Error()
	for _, fragment := range []string{"invalid", "required", "must", "only", "horizon", "confidence", "period_"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
