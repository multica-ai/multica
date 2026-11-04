package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Budgets (spec 4.2). Reads are open to every member; writes are for owners and
// admins, decided through companyActorFor. Every write records its governance
// entry in the service.

// BudgetResponse is one budget with its current period, as the API reports it.
type BudgetResponse struct {
	ID             string     `json:"id"`
	ScopeType      string     `json:"scope_type"`
	ScopeID        *string    `json:"scope_id"`
	Period         string     `json:"period"`
	AmountMicros   int64      `json:"amount_micros"`
	SoftPercent    int32      `json:"soft_percent"`
	Mode           string     `json:"mode"`
	Classes        []string   `json:"classes"`
	Status         string     `json:"status"`
	SpentMicros    int64      `json:"spent_micros"`
	ExtendedMicros int64      `json:"extended_micros"`
	LimitMicros    int64      `json:"limit_micros"`
	Runs           int32      `json:"runs"`
	State          string     `json:"state"`
	PeriodStart    *time.Time `json:"period_start"`
	PeriodEnd      *time.Time `json:"period_end"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// BudgetHoldResponse is one hold in force.
type BudgetHoldResponse struct {
	ID        string    `json:"id"`
	BudgetID  string    `json:"budget_id"`
	TaskID    string    `json:"task_id"`
	AgentID   *string   `json:"agent_id"`
	SquadID   *string   `json:"squad_id"`
	RunClass  string    `json:"run_class"`
	ScopeType string    `json:"scope_type"`
	ScopeID   *string   `json:"scope_id"`
	Period    string    `json:"period"`
	CreatedAt time.Time `json:"created_at"`
}

// BudgetPeriodResponse is one period of a budget, newest first in a list.
type BudgetPeriodResponse struct {
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	SpentMicros    int64     `json:"spent_micros"`
	ExtendedMicros int64     `json:"extended_micros"`
	Runs           int32     `json:"runs"`
	State          string    `json:"state"`
}

// CreateBudgetRequest adds a budget. Omitted soft_percent is 80 and omitted
// classes are routine and delegated.
type CreateBudgetRequest struct {
	ScopeType    string   `json:"scope_type"`
	ScopeID      *string  `json:"scope_id"`
	Period       string   `json:"period"`
	AmountMicros int64    `json:"amount_micros"`
	SoftPercent  *int32   `json:"soft_percent"`
	Mode         string   `json:"mode"`
	Classes      []string `json:"classes"`
}

// UpdateBudgetRequest changes a budget; an omitted field is unchanged.
type UpdateBudgetRequest struct {
	AmountMicros *int64    `json:"amount_micros"`
	SoftPercent  *int32    `json:"soft_percent"`
	Mode         *string   `json:"mode"`
	Classes      *[]string `json:"classes"`
	Status       *string   `json:"status"`
}

// ExtendBudgetRequest raises the current period's limit once.
type ExtendBudgetRequest struct {
	AmountMicros int64 `json:"amount_micros"`
}

func budgetResponse(b db.ListWorkspaceBudgetStatesRow, now time.Time) BudgetResponse {
	start, end := budgetPeriodWindow(b.Period, now, b.PeriodStart, b.PeriodEnd)
	limit := b.AmountMicros + b.ExtendedMicros
	return BudgetResponse{
		ID: util.UUIDToString(b.ID), ScopeType: b.ScopeType, ScopeID: uuidPtr(b.ScopeID),
		Period: b.Period, AmountMicros: b.AmountMicros, SoftPercent: b.SoftPercent, Mode: b.Mode,
		Classes: b.Classes, Status: b.Status, SpentMicros: b.SpentMicros, ExtendedMicros: b.ExtendedMicros,
		LimitMicros: limit, Runs: b.Runs, State: budgetDisplayState(b.SpentMicros, limit, b.SoftPercent),
		PeriodStart: start, PeriodEnd: end, CreatedAt: b.CreatedAt.Time, UpdatedAt: b.UpdatedAt.Time,
	}
}

// budgetPeriodWindow returns the period's bounds: the stored row's when there is
// one, else the window the budget's kind gives today.
func budgetPeriodWindow(kind string, now time.Time, start, end pgtype.Timestamptz) (*time.Time, *time.Time) {
	if start.Valid && end.Valid {
		s, e := start.Time, end.Time
		return &s, &e
	}
	s, e, err := service.BudgetPeriodBounds(kind, now)
	if err != nil {
		return nil, nil
	}
	return &s, &e
}

func budgetDisplayState(spent, limit int64, soft int32) string {
	switch {
	case spent >= limit:
		return "hard"
	case spent*100 >= limit*int64(soft):
		return "soft"
	}
	return "ok"
}

func uuidPtr(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := util.UUIDToString(id)
	return &s
}

// budgetError maps a service error to a status and message.
func budgetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrBudgetNotFound):
		writeError(w, http.StatusNotFound, "budget not found")
	case errors.Is(err, service.ErrBudgetExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrBudgetInvalid):
		writeError(w, http.StatusBadRequest, "budget fields are invalid: scope, period, mode, amounts, soft_percent 1 to 100, classes routine or delegated")
	default:
		writeError(w, http.StatusInternalServerError, "failed to save budget")
	}
}

// validateBudgetScopeTarget checks that a squad, floor or initiative scope names
// an entity of the workspace. A company scope names none.
func (h *Handler) validateBudgetScopeTarget(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, scopeType string, scopeID *string) (pgtype.UUID, bool) {
	if scopeType == service.BudgetScopeCompany {
		if scopeID != nil {
			writeError(w, http.StatusBadRequest, "a company budget has no scope_id")
			return pgtype.UUID{}, false
		}
		return pgtype.UUID{}, true
	}
	if scopeID == nil {
		writeError(w, http.StatusBadRequest, "scope_id is required for a "+scopeType+" budget")
		return pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, *scopeID, "scope_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	var err error
	switch scopeType {
	case service.BudgetScopeSquad:
		_, err = h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	case service.BudgetScopeFloor:
		_, err = h.Queries.GetOfficeFloorInWorkspace(r.Context(), db.GetOfficeFloorInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	case service.BudgetScopeInitiative:
		_, err = h.Queries.GetInitiativeInWorkspace(r.Context(), db.GetInitiativeInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, scopeType+" not found in this workspace")
		return pgtype.UUID{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check the scope")
		return pgtype.UUID{}, false
	}
	return id, true
}

// budgetService builds the service for one request.
func (h *Handler) budgetService() *service.BudgetService {
	return &service.BudgetService{Queries: h.Queries, TxStarter: h.TxStarter}
}

// ListBudgets returns every budget of the workspace with its current period.
// GET /api/budgets
func (h *Handler) ListBudgets(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	now := time.Now()
	rows, err := h.budgetService().List(r.Context(), wsUUID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list budgets")
		return
	}
	out := make([]BudgetResponse, 0, len(rows))
	for _, b := range rows {
		out = append(out, budgetResponse(b, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"budgets": out})
}

// CreateBudget adds a budget. POST /api/budgets
func (h *Handler) CreateBudget(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	var req CreateBudgetRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid JSON body") {
		return
	}
	scopeID, ok := h.validateBudgetScopeTarget(w, r, wsUUID, req.ScopeType, req.ScopeID)
	if !ok {
		return
	}
	soft := int32(80)
	if req.SoftPercent != nil {
		soft = *req.SoftPercent
	}
	classes := req.Classes
	if classes == nil {
		classes = []string{string(service.RunClassRoutine), string(service.RunClassDelegated)}
	}
	// A budget holds by default (spec 4.2). Warn is chosen only by the starter
	// company and the plan gate, which the service creates directly.
	mode := req.Mode
	if mode == "" {
		mode = service.BudgetModeHold
	}
	b, err := h.budgetService().Create(r.Context(), wsUUID, actor, service.BudgetInput{
		ScopeType: req.ScopeType, ScopeID: scopeID, Period: req.Period, AmountMicros: req.AmountMicros,
		SoftPercent: soft, Mode: mode, Classes: classes,
	})
	if err != nil {
		budgetError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, budgetRowResponse(b))
}

// UpdateBudget changes a budget. PATCH /api/budgets/{id}
func (h *Handler) UpdateBudget(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "budget id")
	if !ok {
		return
	}
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	var req UpdateBudgetRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid JSON body") {
		return
	}
	b, err := h.budgetService().Update(r.Context(), wsUUID, actor, id, service.BudgetPatch{
		AmountMicros: req.AmountMicros, SoftPercent: req.SoftPercent, Mode: req.Mode,
		Classes: req.Classes, Status: req.Status,
	})
	if err != nil {
		budgetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, budgetRowResponse(b))
}

// ExtendBudget raises the current period's limit. POST /api/budgets/{id}/extend
func (h *Handler) ExtendBudget(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "budget id")
	if !ok {
		return
	}
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	var req ExtendBudgetRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "amount_micros must be a positive amount") {
		return
	}
	if req.AmountMicros <= 0 {
		writeError(w, http.StatusBadRequest, "amount_micros must be a positive amount")
		return
	}
	b, err := h.budgetService().Extend(r.Context(), wsUUID, actor, id, req.AmountMicros)
	if err != nil {
		budgetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, budgetRowResponse(b))
}

// ReleaseBudgetHolds releases the budget's holds once. POST /api/budgets/{id}/release-holds
func (h *Handler) ReleaseBudgetHolds(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "budget id")
	if !ok {
		return
	}
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	n, err := h.budgetService().ReleaseHolds(r.Context(), wsUUID, actor, id)
	if err != nil {
		budgetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"released": n})
}

// ListBudgetPeriods returns a budget's periods, newest first.
// GET /api/budgets/{id}/periods?limit=
func (h *Handler) ListBudgetPeriods(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "budget id")
	if !ok {
		return
	}
	limit := int32(24)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = int32(n)
	}
	rows, err := h.budgetService().Periods(r.Context(), wsUUID, id, limit)
	if errors.Is(err, service.ErrBudgetNotFound) {
		writeError(w, http.StatusNotFound, "budget not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list periods")
		return
	}
	out := make([]BudgetPeriodResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, BudgetPeriodResponse{
			PeriodStart: p.PeriodStart.Time, PeriodEnd: p.PeriodEnd.Time, SpentMicros: p.SpentMicros,
			ExtendedMicros: p.ExtendedMicros, Runs: p.Runs, State: p.State,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"periods": out})
}

// ListBudgetHolds returns the holds in force. GET /api/budget-holds?status=in_force
func (h *Handler) ListBudgetHolds(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if status := r.URL.Query().Get("status"); status != "" && status != "in_force" {
		writeError(w, http.StatusBadRequest, "status must be in_force")
		return
	}
	rows, err := h.budgetService().HoldsInForce(r.Context(), wsUUID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list holds")
		return
	}
	out := make([]BudgetHoldResponse, 0, len(rows))
	for _, x := range rows {
		out = append(out, BudgetHoldResponse{
			ID: util.UUIDToString(x.ID), BudgetID: util.UUIDToString(x.BudgetID), TaskID: util.UUIDToString(x.TaskID),
			AgentID: uuidPtr(x.AgentID), SquadID: uuidPtr(x.SquadID), RunClass: x.RunClass,
			ScopeType: x.ScopeType, ScopeID: uuidPtr(x.ScopeID), Period: x.Period, CreatedAt: x.CreatedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holds": out})
}

// budgetRowResponse renders a budget row as a write returns it, with its
// period counters read by the list.
func budgetRowResponse(b db.Budget) BudgetResponse {
	return BudgetResponse{
		ID: util.UUIDToString(b.ID), ScopeType: b.ScopeType, ScopeID: uuidPtr(b.ScopeID),
		Period: b.Period, AmountMicros: b.AmountMicros, SoftPercent: b.SoftPercent, Mode: b.Mode,
		Classes: b.Classes, Status: b.Status, LimitMicros: b.AmountMicros, State: "ok",
		CreatedAt: b.CreatedAt.Time, UpdatedAt: b.UpdatedAt.Time,
	}
}
