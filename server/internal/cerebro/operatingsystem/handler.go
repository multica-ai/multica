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
	GetMeeting(context.Context, pgtype.UUID) (MeetingConfigResponse, error)
	UpdateMeeting(context.Context, pgtype.UUID, MeetingConfigInput) (MeetingConfigResponse, error)
	ListOrgChartSeats(context.Context, pgtype.UUID) ([]OrgChartSeatResponse, error)
	CreateOrgChartSeat(context.Context, pgtype.UUID, OrgChartSeatInput) (OrgChartSeatResponse, error)
	UpdateOrgChartSeat(context.Context, pgtype.UUID, pgtype.UUID, OrgChartSeatInput) (OrgChartSeatResponse, error)
	DeleteOrgChartSeat(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	CreateStrategyItem(context.Context, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error)
	ListStrategyItems(context.Context, pgtype.UUID) ([]StrategyItemResponse, error)
	ListStrategyHistory(context.Context, pgtype.UUID) ([]StrategyHistoryResponse, error)
	UpdateStrategyItem(context.Context, pgtype.UUID, pgtype.UUID, StrategyItemInput) (StrategyItemResponse, error)
	DeleteStrategyItem(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	ListVisionPlan(context.Context, pgtype.UUID) (VisionPlanResponse, error)
	CreateVisionPlanSection(context.Context, pgtype.UUID, VisionPlanSectionInput) (VisionPlanSectionResponse, error)
	UpdateVisionPlanSection(context.Context, pgtype.UUID, pgtype.UUID, VisionPlanSectionInput) (VisionPlanSectionResponse, error)
	DeleteVisionPlanSection(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	CreateVisionPlanItem(context.Context, pgtype.UUID, VisionPlanItemInput) (VisionPlanItemResponse, error)
	UpdateVisionPlanItem(context.Context, pgtype.UUID, pgtype.UUID, VisionPlanItemInput) (VisionPlanItemResponse, error)
	DeleteVisionPlanItem(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	ListPeriods(context.Context, pgtype.UUID) ([]OperatingPeriodResponse, error)
	CreatePeriod(context.Context, pgtype.UUID, OperatingPeriodInput) (OperatingPeriodResponse, error)
	UpdatePeriod(context.Context, pgtype.UUID, pgtype.UUID, OperatingPeriodInput) (OperatingPeriodResponse, error)
	DeletePeriod(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	ListElements(context.Context, pgtype.UUID) ([]OsElementResponse, error)
	UpdateElement(context.Context, pgtype.UUID, string, bool) (OsElementResponse, error)
	CreateGoalType(context.Context, pgtype.UUID, GoalTypeInput) (GoalTypeResponse, error)
	ListGoalTypes(context.Context, pgtype.UUID) ([]GoalTypeResponse, error)
	UpdateGoalType(context.Context, pgtype.UUID, pgtype.UUID, GoalTypeInput) (GoalTypeResponse, error)
	DeleteGoalType(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
	UpsertRock(context.Context, pgtype.UUID, RockInput) error
	SaveRock(context.Context, pgtype.UUID, string, pgtype.UUID, *pgtype.UUID, RockInput) (RockResponse, error)
	AddRockCheckIn(context.Context, pgtype.UUID, pgtype.UUID, string, pgtype.UUID, RockCheckInInput) (RockCheckIn, error)
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
	r.Route("/api/cerebro/operating-system/elements", func(r chi.Router) {
		r.Get("/", h.ListElements)
		r.Put("/{key}", h.UpdateElement)
	})
	r.Route("/api/cerebro/meetings", func(r chi.Router) {
		r.Get("/", h.GetMeeting)
		r.Put("/", h.UpdateMeeting)
	})
	r.Route("/api/cerebro/org-chart", func(r chi.Router) {
		r.Get("/", h.ListOrgChartSeats)
		r.Post("/seats", h.CreateOrgChartSeat)
		r.Put("/seats/{id}", h.UpdateOrgChartSeat)
		r.Delete("/seats/{id}", h.DeleteOrgChartSeat)
	})
	r.Route("/api/cerebro/goal-types", func(r chi.Router) {
		r.Get("/", h.ListGoalTypes)
		r.Post("/", h.CreateGoalType)
		r.Put("/{id}", h.UpdateGoalType)
		r.Delete("/{id}", h.DeleteGoalType)
	})
	r.Route("/api/cerebro/strategy-items", func(r chi.Router) {
		r.Get("/", h.ListStrategyItems)
		r.Post("/", h.CreateStrategyItem)
		r.Put("/{id}", h.UpdateStrategyItem)
		r.Delete("/{id}", h.DeleteStrategyItem)
	})
	r.Get("/api/cerebro/strategy-history", h.ListStrategyHistory)
	r.Route("/api/cerebro/vision-plan", func(r chi.Router) {
		r.Get("/", h.ListVisionPlan)
		r.Post("/sections", h.CreateVisionPlanSection)
		r.Put("/sections/{id}", h.UpdateVisionPlanSection)
		r.Delete("/sections/{id}", h.DeleteVisionPlanSection)
		r.Post("/items", h.CreateVisionPlanItem)
		r.Put("/items/{id}", h.UpdateVisionPlanItem)
		r.Delete("/items/{id}", h.DeleteVisionPlanItem)
	})
	r.Route("/api/cerebro/rocks", func(r chi.Router) {
		r.Get("/", h.ListRocks)
		r.Post("/", h.UpsertRock)
		r.Put("/{id}", h.UpsertRock)
		r.Delete("/{id}", h.DeleteRock)
		r.Post("/{id}/check-ins", h.AddRockCheckIn)
	})
	r.Route("/api/cerebro/operating-periods", func(r chi.Router) {
		r.Get("/", h.ListPeriods)
		r.Post("/", h.CreatePeriod)
		r.Put("/{id}", h.UpdatePeriod)
		r.Delete("/{id}", h.DeletePeriod)
	})
	r.Route("/api/cerebro/object-connections", func(r chi.Router) {
		r.Get("/", h.ListConnections)
		r.Post("/", h.CreateConnection)
		r.Delete("/{id}", h.DeleteConnection)
	})
}

func (h *Handler) ListStrategyHistory(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListStrategyHistory(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"history": items}, err)
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

func (h *Handler) GetMeeting(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	out, err := h.service.GetMeeting(r.Context(), ws)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) UpdateMeeting(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input MeetingConfigInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateMeeting(r.Context(), ws, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) ListOrgChartSeats(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListOrgChartSeats(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"seats": items}, err)
}

func (h *Handler) CreateOrgChartSeat(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input OrgChartSeatInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreateOrgChartSeat(r.Context(), ws, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) UpdateOrgChartSeat(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input OrgChartSeatInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateOrgChartSeat(r.Context(), ws, id, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) DeleteOrgChartSeat(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteOrgChartSeat(r.Context(), ws, id)
	writeDelete(w, deleted, err)
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

func (h *Handler) ListVisionPlan(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	out, err := h.service.ListVisionPlan(r.Context(), ws)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) CreateVisionPlanSection(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input VisionPlanSectionInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreateVisionPlanSection(r.Context(), ws, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) UpdateVisionPlanSection(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input VisionPlanSectionInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateVisionPlanSection(r.Context(), ws, id, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) DeleteVisionPlanSection(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteVisionPlanSection(r.Context(), ws, id)
	writeDelete(w, deleted, err)
}

func (h *Handler) CreateVisionPlanItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input VisionPlanItemInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreateVisionPlanItem(r.Context(), ws, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) UpdateVisionPlanItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input VisionPlanItemInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateVisionPlanItem(r.Context(), ws, id, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) DeleteVisionPlanItem(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteVisionPlanItem(r.Context(), ws, id)
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

func (h *Handler) ListPeriods(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListPeriods(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"periods": items}, err)
}

func (h *Handler) ListElements(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListElements(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"elements": items}, err)
}

func (h *Handler) UpdateElement(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "element key is required")
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateElement(r.Context(), ws, key, input.Enabled)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) ListGoalTypes(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListGoalTypes(r.Context(), ws)
	writeResult(w, http.StatusOK, map[string]any{"goal_types": items}, err)
}

func (h *Handler) CreateGoalType(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input GoalTypeInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreateGoalType(r.Context(), ws, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) UpdateGoalType(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input GoalTypeInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdateGoalType(r.Context(), ws, id, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) DeleteGoalType(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeleteGoalType(r.Context(), ws, id)
	writeDelete(w, deleted, err)
}

func (h *Handler) CreatePeriod(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input OperatingPeriodInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.CreatePeriod(r.Context(), ws, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) UpdatePeriod(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input OperatingPeriodInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.UpdatePeriod(r.Context(), ws, id, input)
	writeResult(w, http.StatusOK, out, err)
}

func (h *Handler) DeletePeriod(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.service.DeletePeriod(r.Context(), ws, id)
	writeDelete(w, deleted, err)
}

func (h *Handler) UpsertRock(w http.ResponseWriter, r *http.Request) {
	ws, memberID, ok := requestScope(w, r)
	if !ok {
		return
	}
	var input RockInput
	if !decodeBody(w, r, &input) {
		return
	}
	rawID := strings.TrimSpace(chi.URLParam(r, "id"))
	legacy := input.Title == "" && input.PeriodID == ""
	if legacy {
		if rawID != "" {
			if _, err := util.ParseUUID(rawID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid id")
				return
			}
			input.ProjectID = rawID
		}
		err := h.service.UpsertRock(r.Context(), ws, input)
		writeResult(w, http.StatusOK, map[string]bool{"saved": err == nil}, err)
		return
	}
	var rockID *pgtype.UUID
	if rawID != "" {
		parsed, err := util.ParseUUID(rawID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		rockID = &parsed
	}
	out, err := h.service.SaveRock(r.Context(), ws, "member", memberID, rockID, input)
	status := http.StatusCreated
	if rockID != nil {
		status = http.StatusOK
	}
	writeResult(w, status, out, err)
}

func (h *Handler) AddRockCheckIn(w http.ResponseWriter, r *http.Request) {
	ws, memberID, ok := requestScope(w, r)
	if !ok {
		return
	}
	rockID, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}
	var input RockCheckInInput
	if !decodeBody(w, r, &input) {
		return
	}
	out, err := h.service.AddRockCheckIn(r.Context(), ws, rockID, "member", memberID, input)
	writeResult(w, http.StatusCreated, out, err)
}

func (h *Handler) DeleteRock(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := requestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := uuidParam(w, r, "id")
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
	case errors.Is(err, ErrProjectNotInWorkspace), errors.Is(err, ErrElementDisabled), errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, err.Error())
	case strings.Contains(err.Error(), "duplicate connection"):
		writeError(w, http.StatusConflict, "connection already exists")
	case strings.Contains(err.Error(), "already exists"):
		writeError(w, http.StatusConflict, err.Error())
	case isInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "operating system request failed")
	}
}

func isInputError(err error) bool {
	message := err.Error()
	for _, fragment := range []string{"invalid", "required", "must", "only", "horizon", "confidence", "period_", "period is", "starts_on", "ends_on"} {
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
