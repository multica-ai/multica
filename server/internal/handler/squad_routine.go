package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// routineCadencePresets are the cadences a routine can be created with, as cron
// expressions that fire at 09:00 in the routine's timezone. Biweekly fires on
// the 1st and the 15th: a five-field cron cannot express every fourteen days,
// so it is the nearest schedule the scheduler can run.
var routineCadencePresets = map[string]string{
	"daily":    "0 9 * * *",
	"weekdays": "0 9 * * 1-5",
	"weekly":   "0 9 * * 1",
	"biweekly": "0 9 1,15 * *",
	"monthly":  "0 9 1 * *",
}

const (
	routinePurposeMax        = 200
	routineDefaultGraceHours = 24
	routineGraceHoursMax     = 336
	routineDefaultOutput     = "none"
)

var routineExpectedOutputs = map[string]bool{
	"none":        true,
	"issue":       true,
	"proposal":    true,
	"kb_document": true,
	"report":      true,
}

// RoutineScheduleResponse is the cadence the routine's autopilot runs on.
type RoutineScheduleResponse struct {
	CronExpression string `json:"cron_expression"`
	Timezone       string `json:"timezone"`
}

// RoutineLastRunResponse is the autopilot's most recent run. The run's status is
// reported as recorded; deciding whether the routine delivered is left to the
// compliance step.
type RoutineLastRunResponse struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	Source      string  `json:"source"`
	TriggeredAt string  `json:"triggered_at"`
	CompletedAt *string `json:"completed_at"`
}

// SquadRoutineResponse is a routine as the squad panel reads it. The routine's
// identity is its autopilot: autopilot_id is also the {routine_id} in the
// unlink path. state is derived on read (service.RoutineComplianceStates).
// schedule, next_run_at and last_run are null when the autopilot has no enabled
// schedule or has never run.
type SquadRoutineResponse struct {
	AutopilotID     string                   `json:"autopilot_id"`
	SquadID         string                   `json:"squad_id"`
	AutopilotTitle  string                   `json:"autopilot_title"`
	AutopilotStatus string                   `json:"autopilot_status"`
	Purpose         string                   `json:"purpose"`
	ExpectedOutput  string                   `json:"expected_output"`
	Required        bool                     `json:"required"`
	GraceHours      int32                    `json:"grace_hours"`
	State           string                   `json:"state"`
	Schedule        *RoutineScheduleResponse `json:"schedule"`
	NextRunAt       *string                  `json:"next_run_at"`
	LastRun         *RoutineLastRunResponse  `json:"last_run"`
	CreatedAt       string                   `json:"created_at"`
}

// CreateSquadRoutineRequest links an existing autopilot (autopilot_id) or creates
// one on a cadence (cadence: a preset name or a cron expression). Exactly one of
// the two is required. purpose is the routine's one-line job; description is the
// work instruction for an autopilot created here.
type CreateSquadRoutineRequest struct {
	AutopilotID    *string `json:"autopilot_id"`
	Cadence        *string `json:"cadence"`
	Timezone       *string `json:"timezone"`
	Purpose        string  `json:"purpose"`
	Description    *string `json:"description"`
	ExpectedOutput *string `json:"expected_output"`
	Required       *bool   `json:"required"`
	GraceHours     *int32  `json:"grace_hours"`
}

func routineViewFromRow(row db.ListSquadRoutinesRow, state string) SquadRoutineResponse {
	resp := SquadRoutineResponse{
		AutopilotID:     uuidToString(row.AutopilotID),
		SquadID:         uuidToString(row.SquadID),
		AutopilotTitle:  row.AutopilotTitle,
		AutopilotStatus: row.AutopilotStatus,
		Purpose:         row.Purpose,
		ExpectedOutput:  row.ExpectedOutput,
		Required:        row.Required,
		GraceHours:      row.GraceHours,
		State:           state,
		NextRunAt:       timestampToPtr(row.ScheduleNextRunAt),
		CreatedAt:       timestampToString(row.CreatedAt),
	}
	if row.ScheduleCronExpression.Valid {
		resp.Schedule = &RoutineScheduleResponse{
			CronExpression: row.ScheduleCronExpression.String,
			Timezone:       row.ScheduleTimezone.String,
		}
	}
	if row.LastRunID.Valid {
		resp.LastRun = &RoutineLastRunResponse{
			ID:          uuidToString(row.LastRunID),
			Status:      row.LastRunStatus,
			Source:      row.LastRunSource,
			TriggeredAt: timestampToString(row.LastRunTriggeredAt),
			CompletedAt: timestampToPtr(row.LastRunCompletedAt),
		}
	}
	return resp
}

// squadRoutineViews reads a squad's routines in the list's shape, each with its
// derived state. The state is computed on every read.
func (h *Handler) squadRoutineViews(r *http.Request, squad db.Squad) ([]SquadRoutineResponse, error) {
	rows, err := h.Queries.ListSquadRoutines(r.Context(), db.ListSquadRoutinesParams{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
	})
	if err != nil {
		return nil, err
	}
	states, err := service.RoutineComplianceStates(r.Context(), h.Queries, squad.WorkspaceID, squad.ID, time.Now())
	if err != nil {
		return nil, err
	}
	stateByAutopilot := make(map[string]string, len(states))
	for _, s := range states {
		stateByAutopilot[s.AutopilotID] = s.State
	}
	out := make([]SquadRoutineResponse, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.AutopilotID)
		out = append(out, routineViewFromRow(row, stateByAutopilot[id]))
	}
	return out, nil
}

// loadSquadRoutineView reads one routine back in the list's shape, so a create
// responds with exactly what the list would show.
func (h *Handler) loadSquadRoutineView(r *http.Request, squad db.Squad, autopilotID pgtype.UUID) (SquadRoutineResponse, error) {
	views, err := h.squadRoutineViews(r, squad)
	if err != nil {
		return SquadRoutineResponse{}, err
	}
	for _, v := range views {
		if v.AutopilotID == uuidToString(autopilotID) {
			return v, nil
		}
	}
	return SquadRoutineResponse{}, pgx.ErrNoRows
}

// resolveRoutineTimezone picks the schedule's timezone: the request's, else the
// actor's saved timezone, else UTC. A request timezone that does not parse is
// refused, since the caller chose it.
func (h *Handler) resolveRoutineTimezone(r *http.Request, actorUserID pgtype.UUID, requested *string) (string, error) {
	if requested != nil && strings.TrimSpace(*requested) != "" {
		tz := strings.TrimSpace(*requested)
		if err := service.ValidateTimezone(tz); err != nil {
			return "", err
		}
		return tz, nil
	}
	user, err := h.Queries.GetUser(r.Context(), actorUserID)
	if err == nil && user.Timezone.Valid && service.ValidateTimezone(user.Timezone.String) == nil {
		return user.Timezone.String, nil
	}
	return "UTC", nil
}

// ListSquadRoutines returns the squad's routines with their schedule and last run.
// Any workspace member may read them. Compliance state is derived elsewhere.
func (h *Handler) ListSquadRoutines(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	out, err := h.squadRoutineViews(r, squad)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list routines")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateSquadRoutine links an existing autopilot as a routine, or creates an
// autopilot on a cadence and links it. The squad's creator or a workspace admin
// may do either. An autopilot can be a routine of one squad only.
func (h *Handler) CreateSquadRoutine(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	actor, ok := h.requireAutopilotActingMember(w, r, workspaceID, autopilotCreateRefusal)
	if !ok {
		return
	}
	if !canManageSquad(actor, squad) {
		writeError(w, http.StatusForbidden, "only the squad's creator or a workspace admin can change its routines")
		return
	}

	var req CreateSquadRoutineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	purpose := strings.TrimSpace(req.Purpose)
	if n := len([]rune(purpose)); n < 1 || n > routinePurposeMax {
		writeError(w, http.StatusBadRequest, "purpose must be 1 to 200 characters")
		return
	}
	if purpose == service.OperatingReviewPurpose {
		writeError(w, http.StatusBadRequest, "that purpose is reserved for the squad's operating review")
		return
	}
	expectedOutput := routineDefaultOutput
	if req.ExpectedOutput != nil {
		expectedOutput = *req.ExpectedOutput
	}
	if !routineExpectedOutputs[expectedOutput] {
		writeError(w, http.StatusBadRequest, "expected_output must be none, issue, proposal, kb_document or report")
		return
	}
	required := true
	if req.Required != nil {
		required = *req.Required
	}
	graceHours := int32(routineDefaultGraceHours)
	if req.GraceHours != nil {
		graceHours = *req.GraceHours
	}
	if graceHours < 1 || graceHours > routineGraceHoursMax {
		writeError(w, http.StatusBadRequest, "grace_hours must be between 1 and 336")
		return
	}

	linkID := ""
	if req.AutopilotID != nil {
		linkID = strings.TrimSpace(*req.AutopilotID)
	}
	cadence := ""
	if req.Cadence != nil {
		cadence = strings.TrimSpace(*req.Cadence)
	}
	switch {
	case linkID != "" && cadence != "":
		writeError(w, http.StatusBadRequest, "send either autopilot_id or cadence, not both")
		return
	case linkID != "":
		h.linkSquadRoutine(w, r, workspaceID, squad, actor, linkID, routineFields{
			purpose: purpose, expectedOutput: expectedOutput, required: required, graceHours: graceHours,
		})
	case cadence != "":
		h.createSquadRoutineAutopilot(w, r, workspaceID, squad, actor, cadence, req, routineFields{
			purpose: purpose, expectedOutput: expectedOutput, required: required, graceHours: graceHours,
		})
	default:
		writeError(w, http.StatusBadRequest, "send autopilot_id to link an autopilot, or cadence to create one")
	}
}

// routineFields is the routine's own settings, shared by both create paths.
type routineFields struct {
	purpose        string
	expectedOutput string
	required       bool
	graceHours     int32
}

func (f routineFields) params(autopilotID, workspaceID, squadID pgtype.UUID) db.InsertSquadRoutineParams {
	return db.InsertSquadRoutineParams{
		AutopilotID:    autopilotID,
		WorkspaceID:    workspaceID,
		SquadID:        squadID,
		Purpose:        f.purpose,
		ExpectedOutput: f.expectedOutput,
		Required:       f.required,
		GraceHours:     f.graceHours,
	}
}

// linkSquadRoutine makes an existing autopilot a routine of the squad. The
// autopilot must be assigned to this squad and writable by the actor, and must
// not be archived or already a routine.
func (h *Handler) linkSquadRoutine(w http.ResponseWriter, r *http.Request, workspaceID string, squad db.Squad, actor db.Member, autopilotIDRaw string, fields routineFields) {
	autopilotID, ok := parseUUIDOrBadRequest(w, autopilotIDRaw, "autopilot_id")
	if !ok {
		return
	}
	ap, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          autopilotID,
		WorkspaceID: squad.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}
	if !h.memberCanWriteAutopilot(r.Context(), ap, actor) {
		writeErrorCode(w, http.StatusForbidden, autopilotWriteRefusal.forbiddenCode, autopilotWriteRefusal.forbiddenMsg)
		return
	}
	if ap.AssigneeType != "squad" || uuidToString(ap.AssigneeID) != uuidToString(squad.ID) {
		writeError(w, http.StatusBadRequest, "autopilot must be assigned to this squad")
		return
	}
	if ap.Status == "archived" {
		writeError(w, http.StatusBadRequest, "autopilot is archived")
		return
	}

	_, err = h.Queries.InsertSquadRoutine(r.Context(), fields.params(ap.ID, squad.WorkspaceID, squad.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "routine_already_linked", "autopilot is already a routine")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link routine")
		return
	}
	resp, err := h.loadSquadRoutineView(r, squad, ap.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load routine")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// createSquadRoutineAutopilot creates an active autopilot on the squad with a
// schedule, and links it as a routine, in one transaction. It runs the same
// assignee check, rule-version record and trigger write the autopilot handlers
// run, so a routine's autopilot is indistinguishable from one made there.
func (h *Handler) createSquadRoutineAutopilot(w http.ResponseWriter, r *http.Request, workspaceID string, squad db.Squad, actor db.Member, cadence string, req CreateSquadRoutineRequest, fields routineFields) {
	cron, isPreset := routineCadencePresets[cadence]
	if !isPreset {
		cron = cadence
	}
	timezone, err := h.resolveRoutineTimezone(r, actor.UserID, req.Timezone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	nextRun, err := computeNextRun(cron, timezone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create routine")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if !h.validateAutopilotAssigneeForSave(w, r, qtx, "squad", squad.ID, squad.WorkspaceID, true) {
		return
	}
	ap, err := service.CreateSquadScheduledAutopilot(r.Context(), qtx, service.SquadScheduledAutopilot{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
		ActorUserID: actor.UserID,
		Title:       fields.purpose,
		Description: ptrToText(req.Description),
		Status:      "active",
		Cron:        cron,
		Timezone:    timezone,
		NextRun:     nextRun,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create routine")
		return
	}
	if _, err := qtx.InsertSquadRoutine(r.Context(), fields.params(ap.ID, squad.WorkspaceID, squad.ID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create routine")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create routine")
		return
	}

	userID := uuidToString(actor.UserID)
	h.publish(protocol.EventAutopilotCreated, workspaceID, "member", userID, map[string]any{
		"autopilot": autopilotToResponse(ap, nil),
	})
	resp, err := h.loadSquadRoutineView(r, squad, ap.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load routine")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// DeleteSquadRoutine unlinks a routine. The autopilot keeps its schedule and
// history and becomes an ordinary autopilot again, unless ?pause=true, which also
// pauses it. Pausing needs the same write access as editing the autopilot.
func (h *Handler) DeleteSquadRoutine(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "only the squad's creator or a workspace admin can change its routines")
		return
	}
	autopilotID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "routine_id"), "routine id")
	if !ok {
		return
	}
	pause := false
	if raw := r.URL.Query().Get("pause"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "pause must be true or false")
			return
		}
		pause = parsed
	}

	if _, err := h.Queries.GetSquadRoutine(r.Context(), db.GetSquadRoutineParams{
		AutopilotID: autopilotID,
		SquadID:     squad.ID,
		WorkspaceID: squad.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "routine not found")
		return
	}

	if !pause {
		if _, err := h.Queries.DeleteSquadRoutine(r.Context(), db.DeleteSquadRoutineParams{
			AutopilotID: autopilotID,
			SquadID:     squad.ID,
			WorkspaceID: squad.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unlink routine")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ap, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          autopilotID,
		WorkspaceID: squad.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}
	actor, ok := h.requireAutopilotWrite(w, r, ap, workspaceID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink routine")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.DeleteSquadRoutine(r.Context(), db.DeleteSquadRoutineParams{
		AutopilotID: autopilotID,
		SquadID:     squad.ID,
		WorkspaceID: squad.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink routine")
		return
	}
	if ap.Status == "active" {
		paused, err := qtx.UpdateAutopilot(r.Context(), db.UpdateAutopilotParams{
			ID:                 ap.ID,
			Status:             pgtype.Text{String: "paused", Valid: true},
			IssueTitleTemplate: ap.IssueTitleTemplate,
			ProjectID:          ap.ProjectID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pause autopilot")
			return
		}
		if err := h.recordAutopilotRuleVersion(r.Context(), qtx, paused, "member", actor.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pause autopilot")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink routine")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
