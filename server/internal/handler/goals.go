package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The goal API (spec 4.1). Reads are open to every member. The boss and admins
// create, change, activate and close goals, and may override a goal's
// confidence; the leader of the squad that owns a goal may override its
// confidence too. A squad's goal is not written here: the squad files it as a
// proposal of kind goal, and the boss's approval activates it.

// goalMaxKeyResults bounds the key results a request may carry.
const goalMaxKeyResults = 10

// KeyResultResponse is one key result with its progress. Series is the readings
// in the period, oldest first, on a goal's detail; it is empty in a list.
type KeyResultResponse struct {
	ID          string               `json:"id"`
	GoalID      string               `json:"goal_id"`
	Title       string               `json:"title"`
	MetricKey   string               `json:"metric_key"`
	ScopeType   string               `json:"scope_type"`
	ScopeID     *string              `json:"scope_id"`
	StartValue  float64              `json:"start_value"`
	TargetValue float64              `json:"target_value"`
	Weight      int                  `json:"weight"`
	Current     *float64             `json:"current_value"`
	ObservedAt  *string              `json:"observed_at"`
	Progress    *float64             `json:"progress"`
	Series      []GoalSeriesPointDTO `json:"series"`
}

// GoalSeriesPointDTO is one point of a key result's series.
type GoalSeriesPointDTO struct {
	ObservedAt string  `json:"observed_at"`
	Value      float64 `json:"value"`
}

// GoalResponse is one goal. confidence is the effective value; derived_confidence
// is what the readings give, and confidence_override is the owner's choice when
// one is in force, with its note.
type GoalResponse struct {
	ID                  string              `json:"id"`
	WorkspaceID         string              `json:"workspace_id"`
	ScopeType           string              `json:"scope_type"`
	ScopeID             *string             `json:"scope_id"`
	ParentGoalID        *string             `json:"parent_goal_id"`
	Title               string              `json:"title"`
	Why                 string              `json:"why"`
	PeriodStart         string              `json:"period_start"`
	PeriodEnd           string              `json:"period_end"`
	OwnerType           string              `json:"owner_type"`
	OwnerID             string              `json:"owner_id"`
	Status              string              `json:"status"`
	Confidence          string              `json:"confidence"`
	DerivedConfidence   string              `json:"derived_confidence"`
	ConfidenceOverride  *string             `json:"confidence_override"`
	ConfidenceNote      string              `json:"confidence_note"`
	ConfidenceChangedAt string              `json:"confidence_changed_at"`
	Progress            *float64            `json:"progress"`
	ReadingsMissing     int                 `json:"readings_missing"`
	SourceProposalID    *string             `json:"source_proposal_id"`
	KeyResults          []KeyResultResponse `json:"key_results"`
	CreatedByType       string              `json:"created_by_type"`
	CreatedByID         *string             `json:"created_by_id"`
	CreatedAt           string              `json:"created_at"`
	UpdatedAt           string              `json:"updated_at"`
}

// GoalDetailResponse is one goal with the goals it is the parent of.
type GoalDetailResponse struct {
	Goal       GoalResponse      `json:"goal"`
	ChildGoals []GoalRefResponse `json:"child_goals"`
}

// GoalRefResponse names a goal in a list of links.
type GoalRefResponse struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	ScopeType string  `json:"scope_type"`
	ScopeID   *string `json:"scope_id"`
}

// KeyResultRequest is a key result in a create or replace request.
type KeyResultRequest struct {
	Title       string   `json:"title"`
	MetricKey   string   `json:"metric_key"`
	ScopeType   string   `json:"scope_type"`
	ScopeID     *string  `json:"scope_id"`
	StartValue  *float64 `json:"start_value"`
	TargetValue *float64 `json:"target_value"`
	Weight      int      `json:"weight"`
}

// CreateGoalRequest creates a draft goal. Dates are YYYY-MM-DD.
type CreateGoalRequest struct {
	ScopeType    string             `json:"scope_type"`
	ScopeID      *string            `json:"scope_id"`
	ParentGoalID *string            `json:"parent_goal_id"`
	Title        string             `json:"title"`
	Why          string             `json:"why"`
	PeriodStart  string             `json:"period_start"`
	PeriodEnd    string             `json:"period_end"`
	OwnerType    string             `json:"owner_type"`
	OwnerID      string             `json:"owner_id"`
	KeyResults   []KeyResultRequest `json:"key_results"`
}

// UpdateGoalRequest changes a goal. Absent fields keep their value; key_results,
// when present, replaces the key results of a draft.
type UpdateGoalRequest struct {
	Title        *string            `json:"title"`
	Why          *string            `json:"why"`
	PeriodStart  *string            `json:"period_start"`
	PeriodEnd    *string            `json:"period_end"`
	OwnerType    *string            `json:"owner_type"`
	OwnerID      *string            `json:"owner_id"`
	ParentGoalID *string            `json:"parent_goal_id"`
	KeyResults   []KeyResultRequest `json:"key_results"`
}

// SetGoalStatusRequest closes a goal: achieved, missed or dropped.
type SetGoalStatusRequest struct {
	Status string `json:"status"`
}

// SetGoalConfidenceRequest overrides a goal's confidence. A null confidence clears
// the override. A value needs a note.
type SetGoalConfidenceRequest struct {
	Confidence *string `json:"confidence"`
	Note       string  `json:"note"`
}

func (h *Handler) goalService() *service.GoalService {
	return service.NewGoalServiceFor(h.IssueService)
}

func dateString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func goalResponse(v service.GoalView) GoalResponse {
	g := v.Goal
	out := GoalResponse{
		ID: uuidToString(g.ID), WorkspaceID: uuidToString(g.WorkspaceID), ScopeType: g.ScopeType,
		ScopeID: uuidToPtr(g.ScopeID), ParentGoalID: uuidToPtr(g.ParentGoalID), Title: g.Title, Why: g.Why,
		PeriodStart: dateString(g.PeriodStart), PeriodEnd: dateString(g.PeriodEnd),
		OwnerType: g.OwnerType, OwnerID: uuidToString(g.OwnerID), Status: g.Status,
		Confidence: v.Effective, DerivedConfidence: v.Derived, ConfidenceNote: g.ConfidenceNote,
		ConfidenceChangedAt: timestampToString(g.ConfidenceChangedAt), Progress: v.Progress,
		ReadingsMissing: v.ReadingsMissing, SourceProposalID: uuidToPtr(g.SourceProposalID),
		CreatedByType: g.CreatedByType, CreatedByID: uuidToPtr(g.CreatedByID),
		CreatedAt: timestampToString(g.CreatedAt), UpdatedAt: timestampToString(g.UpdatedAt),
		KeyResults: make([]KeyResultResponse, 0, len(v.KeyResults)),
	}
	if v.Overridden && g.ConfidenceOverride.Valid {
		o := g.ConfidenceOverride.String
		out.ConfidenceOverride = &o
	}
	for _, kr := range v.KeyResults {
		resp := KeyResultResponse{
			ID: kr.ID, GoalID: kr.GoalID, Title: kr.Title, MetricKey: kr.MetricKey, ScopeType: kr.ScopeType,
			ScopeID: uuidToPtr(kr.ScopeID), StartValue: kr.StartValue, TargetValue: kr.TargetValue,
			Weight: kr.Weight, Current: kr.Current, Progress: kr.Progress,
			Series: make([]GoalSeriesPointDTO, 0, len(kr.Series)),
		}
		if kr.ObservedAt != nil {
			s := kr.ObservedAt.UTC().Format(time.RFC3339)
			resp.ObservedAt = &s
		}
		for _, p := range kr.Series {
			resp.Series = append(resp.Series, GoalSeriesPointDTO{ObservedAt: p.ObservedAt.UTC().Format(time.RFC3339), Value: p.Value})
		}
		out.KeyResults = append(out.KeyResults, resp)
	}
	return out
}

// goalWriteCaller resolves who writes and whether the boss decides: an owner or
// admin member. Agents are not the boss.
func (h *Handler) goalWriteCaller(w http.ResponseWriter, r *http.Request, workspaceID string) (initiativeCaller, bool) {
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return c, false
	}
	if !c.admin {
		writeError(w, http.StatusForbidden, "only an owner or admin decides goals")
		return c, false
	}
	return c, true
}

// ListGoals returns the goals of the workspace, with progress and confidence.
// scope_type, scope_id and status narrow the list.
func (h *Handler) ListGoals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	q := r.URL.Query()
	f := service.GoalFilter{ScopeType: q.Get("scope_type"), Status: q.Get("status")}
	if f.ScopeType != "" {
		switch f.ScopeType {
		case service.ScopeCompany, service.ScopeFloor, service.ScopeSquad, service.ScopeInitiative:
		default:
			writeError(w, http.StatusBadRequest, "scope_type must be company, floor, squad or initiative")
			return
		}
	}
	if f.Status != "" && !isGoalStatus(f.Status) {
		writeError(w, http.StatusBadRequest, "status must be draft, active, achieved, missed or dropped")
		return
	}
	if raw := q.Get("scope_id"); raw != "" {
		if f.ScopeID, ok = parseUUIDOrBadRequest(w, raw, "scope_id"); !ok {
			return
		}
	}
	views, err := h.goalService().List(r.Context(), wsUUID, f)
	if err != nil {
		slog.Warn("list goals failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list goals")
		return
	}
	out := make([]GoalResponse, 0, len(views))
	for _, v := range views {
		out = append(out, goalResponse(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": out})
}

func isGoalStatus(s string) bool {
	switch s {
	case service.GoalStatusDraft, service.GoalStatusActive, service.GoalStatusAchieved,
		service.GoalStatusMissed, service.GoalStatusDropped:
		return true
	}
	return false
}

// GetGoal returns one goal with its key results, their series and its child goals.
func (h *Handler) GetGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	svc := h.goalService()
	view, err := svc.Get(r.Context(), wsUUID, id)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	children, err := svc.GoalChildren(r.Context(), wsUUID, id)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	detail := GoalDetailResponse{Goal: goalResponse(view), ChildGoals: make([]GoalRefResponse, 0, len(children))}
	for _, c := range children {
		detail.ChildGoals = append(detail.ChildGoals, GoalRefResponse{
			ID: uuidToString(c.ID), Title: c.Title, Status: c.Status, ScopeType: c.ScopeType, ScopeID: uuidToPtr(c.ScopeID),
		})
	}
	writeJSON(w, http.StatusOK, detail)
}

// CreateGoal writes a draft goal for the boss to activate.
func (h *Handler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.goalWriteCaller(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req CreateGoalRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid request body") {
		return
	}
	p, ok := goalWriteParamsFrom(w, wsUUID, c, req)
	if !ok {
		return
	}
	view, err := h.goalService().Create(r.Context(), p)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"goal": goalResponse(view)})
}

// goalWriteParamsFrom reads a create request into the service's parameters. It
// writes the error and reports false when a field is malformed.
func goalWriteParamsFrom(w http.ResponseWriter, ws pgtype.UUID, c initiativeCaller, req CreateGoalRequest) (service.GoalWriteParams, bool) {
	p := service.GoalWriteParams{
		WorkspaceID: ws, ScopeType: req.ScopeType, Title: req.Title, Why: req.Why,
		OwnerType: req.OwnerType, Actor: service.GoalActor{Type: goalActorType(c), ID: c.actor.ID},
	}
	var ok bool
	if req.ScopeID != nil && *req.ScopeID != "" {
		if p.ScopeID, ok = parseUUIDOrBadRequest(w, *req.ScopeID, "scope_id"); !ok {
			return p, false
		}
	}
	if req.ParentGoalID != nil && *req.ParentGoalID != "" {
		if p.ParentGoalID, ok = parseUUIDOrBadRequest(w, *req.ParentGoalID, "parent_goal_id"); !ok {
			return p, false
		}
	}
	if req.OwnerID != "" {
		if p.OwnerID, ok = parseUUIDOrBadRequest(w, req.OwnerID, "owner_id"); !ok {
			return p, false
		}
	}
	var err error
	if p.PeriodStart, err = parseGoalDate(req.PeriodStart); err != nil {
		writeError(w, http.StatusBadRequest, "period_start must be a date, YYYY-MM-DD")
		return p, false
	}
	if p.PeriodEnd, err = parseGoalDate(req.PeriodEnd); err != nil {
		writeError(w, http.StatusBadRequest, "period_end must be a date, YYYY-MM-DD")
		return p, false
	}
	if len(req.KeyResults) > goalMaxKeyResults {
		writeError(w, http.StatusBadRequest, "a goal takes at most 10 key results")
		return p, false
	}
	krs, ok := keyResultInputsFrom(w, p.WorkspaceID, req.KeyResults)
	if !ok {
		return p, false
	}
	p.KeyResults = krs
	return p, true
}

// keyResultInputsFrom converts key result requests. A missing start or target is
// a bad request; the service checks the rest.
func keyResultInputsFrom(w http.ResponseWriter, ws pgtype.UUID, reqs []KeyResultRequest) ([]service.GoalKeyResultInput, bool) {
	out := make([]service.GoalKeyResultInput, 0, len(reqs))
	for _, kr := range reqs {
		if kr.StartValue == nil || kr.TargetValue == nil {
			writeError(w, http.StatusBadRequest, "each key result needs start_value and target_value")
			return nil, false
		}
		in := service.GoalKeyResultInput{
			Title: kr.Title, MetricKey: kr.MetricKey, ScopeType: kr.ScopeType,
			StartValue: *kr.StartValue, TargetValue: *kr.TargetValue, Weight: kr.Weight,
		}
		if kr.ScopeID != nil && *kr.ScopeID != "" {
			id, ok := parseUUIDOrBadRequest(w, *kr.ScopeID, "key result scope_id")
			if !ok {
				return nil, false
			}
			in.ScopeID = id
		}
		out = append(out, in)
	}
	return out, true
}

func parseGoalDate(raw string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(raw))
}

// UpdateGoal changes a goal the boss decides on.
func (h *Handler) UpdateGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	_, ok := h.goalWriteCaller(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	var req UpdateGoalRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid request body") {
		return
	}
	svc := h.goalService()
	current, err := svc.Get(r.Context(), wsUUID, id)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	g := current.Goal
	upd := service.GoalUpdate{
		Title: g.Title, Why: g.Why, PeriodStart: g.PeriodStart.Time, PeriodEnd: g.PeriodEnd.Time,
		OwnerType: g.OwnerType, OwnerID: g.OwnerID, ParentGoalID: g.ParentGoalID,
	}
	if req.Title != nil {
		upd.Title = *req.Title
	}
	if req.Why != nil {
		upd.Why = *req.Why
	}
	if req.PeriodStart != nil {
		if upd.PeriodStart, err = parseGoalDate(*req.PeriodStart); err != nil {
			writeError(w, http.StatusBadRequest, "period_start must be a date, YYYY-MM-DD")
			return
		}
	}
	if req.PeriodEnd != nil {
		if upd.PeriodEnd, err = parseGoalDate(*req.PeriodEnd); err != nil {
			writeError(w, http.StatusBadRequest, "period_end must be a date, YYYY-MM-DD")
			return
		}
	}
	if req.OwnerType != nil {
		upd.OwnerType = *req.OwnerType
	}
	if req.OwnerID != nil {
		if upd.OwnerID, ok = parseUUIDOrBadRequest(w, *req.OwnerID, "owner_id"); !ok {
			return
		}
	}
	if req.ParentGoalID != nil {
		upd.ParentGoalID = pgtype.UUID{}
		if *req.ParentGoalID != "" {
			if upd.ParentGoalID, ok = parseUUIDOrBadRequest(w, *req.ParentGoalID, "parent_goal_id"); !ok {
				return
			}
		}
	}
	if req.KeyResults != nil {
		if len(req.KeyResults) > goalMaxKeyResults {
			writeError(w, http.StatusBadRequest, "a goal takes at most 10 key results")
			return
		}
		krs, ok := keyResultInputsFrom(w, wsUUID, req.KeyResults)
		if !ok {
			return
		}
		upd.KeyResults = krs
	}
	view, err := svc.Update(r.Context(), wsUUID, id, upd)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": goalResponse(view)})
}

// ActivateGoal makes a draft goal active. The boss or an admin does this.
func (h *Handler) ActivateGoal(w http.ResponseWriter, r *http.Request) {
	h.goalTransition(w, r, func(ctx context.Context, ws, id pgtype.UUID, c initiativeCaller) (service.GoalView, error) {
		return h.goalService().Activate(ctx, ws, id, service.GoalActor{Type: goalActorType(c), ID: c.actor.ID})
	})
}

// SetGoalStatus closes a goal as achieved, missed or dropped.
func (h *Handler) SetGoalStatus(w http.ResponseWriter, r *http.Request) {
	var req SetGoalStatusRequest
	h.goalTransition(w, r, func(ctx context.Context, ws, id pgtype.UUID, _ initiativeCaller) (service.GoalView, error) {
		return h.goalService().SetStatus(ctx, ws, id, req.Status)
	}, func(r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, governanceJSONBodyLimit)
		return json.NewDecoder(r.Body).Decode(&req)
	})
}

// goalTransition runs a boss-only change to one goal and writes the goal. An
// optional read runs first, for the body.
func (h *Handler) goalTransition(w http.ResponseWriter, r *http.Request,
	run func(ctx context.Context, ws, id pgtype.UUID, c initiativeCaller) (service.GoalView, error),
	read ...func(r *http.Request) error) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.goalWriteCaller(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	for _, rd := range read {
		if err := rd(r); err != nil {
			if isBodyTooLarge(err) {
				writeBodyTooLarge(w, governanceJSONBodyLimit)
				return
			}
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	view, err := run(r.Context(), wsUUID, id, c)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": goalResponse(view)})
}

// goalActorType maps the caller to the goal actor type.
func goalActorType(c initiativeCaller) string {
	if c.actor.Type == "agent" {
		return service.ReadingActorAgent
	}
	return service.ReadingActorMember
}

// DeleteGoal removes a draft goal.
func (h *Handler) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.goalWriteCaller(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	if err := h.goalService().Delete(r.Context(), wsUUID, id); err != nil {
		writeGoalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetGoalConfidence overrides a goal's confidence. The boss or an admin may; so
// may the leader of the squad that owns the goal, as the agent it is.
func (h *Handler) SetGoalConfidence(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	c, ok := h.initiativeCallerFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "goal id")
	if !ok {
		return
	}
	var req SetGoalConfidenceRequest
	if !readCappedJSON(w, r, governanceJSONBodyLimit, &req, "invalid request body") {
		return
	}
	svc := h.goalService()
	current, err := svc.Get(r.Context(), wsUUID, id)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	if !c.admin && !h.isGoalOwnerLeader(r.Context(), current.Goal, c) {
		writeError(w, http.StatusForbidden, "only an owner or admin, or the owning squad's leader, may override a goal's confidence")
		return
	}
	view, err := svc.SetConfidenceOverride(r.Context(), wsUUID, id, req.Confidence, req.Note)
	if err != nil {
		writeGoalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": goalResponse(view)})
}

// isGoalOwnerLeader reports whether the caller is the leader of the squad that
// owns the goal.
func (h *Handler) isGoalOwnerLeader(ctx context.Context, g db.Goal, c initiativeCaller) bool {
	if c.actor.Type != "agent" || g.OwnerType != "squad" {
		return false
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: g.OwnerID, WorkspaceID: g.WorkspaceID})
	return err == nil && squad.LeaderID == c.actor.ID
}

// writeGoalError maps the goal service's errors to responses.
func writeGoalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrGoalNotFound):
		writeError(w, http.StatusNotFound, "goal not found")
	case errors.Is(err, service.ErrGoalState):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrGoalSquadScope), errors.Is(err, service.ErrGoalInvalid),
		errors.Is(err, service.ErrGoalOwnerInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Warn("goal write failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to write the goal")
	}
}

// RunPlanningCycle opens this period's planning issues and wakes each squad's
// leader through the same issue:created event an assignment raises. It returns
// how many issues it opened.
func (h *Handler) RunPlanningCycle(ctx context.Context) (int, error) {
	opened, err := h.goalService().RunPlanningCycle(ctx, time.Now())
	for _, p := range opened {
		prefix := h.getIssuePrefix(ctx, p.WorkspaceID)
		h.publish(protocol.EventIssueCreated, uuidToString(p.WorkspaceID), "agent", uuidToString(p.Issue.CreatorID), map[string]any{
			"issue": issueToResponse(p.Issue, prefix),
		})
	}
	return len(opened), err
}
