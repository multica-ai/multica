package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) ListWorkspaceWakeups(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	params := r.URL.Query()
	scope, kind := params.Get("scope"), params.Get("kind")
	if scope == "" {
		scope = "active"
	}
	if kind == "" {
		kind = "all"
	}
	if scope != "active" && scope != "all" && scope != "paused" && scope != "disabled" && scope != "ended" {
		writeError(w, 400, "invalid wakeup scope")
		return
	}
	if kind != "all" && kind != "event" && kind != "at" && kind != "recurring" {
		writeError(w, 400, "invalid wakeup kind")
		return
	}
	page, limit := 0, 50
	for key, dst := range map[string]*int{"offset": &page, "limit": &limit} {
		if raw := params.Get(key); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 0 || (key == "limit" && (v < 1 || v > 100)) || (key == "offset" && v > 1000000) {
				writeError(w, 400, "invalid pagination")
				return
			}
			*dst = v
		}
	}
	source := params.Get("source")
	if source != "" && source != "member" && source != "agent" && source != "system" {
		writeError(w, 400, "invalid wakeup source")
		return
	}
	search := strings.TrimSpace(params.Get("search"))
	if len(search) > 256 {
		writeError(w, 400, "search too long")
		return
	}
	agentID := params.Get("agent_id")
	if agentID != "" {
		id, valid := parseUUIDOrBadRequest(w, agentID, "agent id")
		if !valid {
			return
		}
		agentID = uuidToString(id)
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, 500, "failed to resolve agent access")
		return
	}
	ids := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, parseUUID(id))
	}
	// Management flags use the same initiating human as the mutation endpoints.
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	managerID, isAdmin := pgtype.UUID{}, false
	if originator != "" {
		manager, err := h.getWorkspaceMember(r.Context(), originator, workspaceID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 500, "failed to resolve management access")
			return
		}
		if err == nil {
			managerID = manager.UserID
			isAdmin = roleAllowed(manager.Role, "owner", "admin")
		}
	}
	result, err := h.Queries.ListWorkspaceWakeups(r.Context(), db.ListWorkspaceWakeupsParams{
		WorkspaceID: parseUUID(workspaceID), AgentIds: ids, MemberID: managerID, IsAdmin: isAdmin,
		Scope: scope, Kind: kind, Source: source, AgentID: agentID, Search: search, PageLimit: int32(limit), PageOffset: int32(page),
	})
	if err != nil {
		wakeupError(w, err)
		return
	}
	writeJSON(w, 200, json.RawMessage(result))
}

func wakeupError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "issue_wakeup_active_limit" {
		writeErrorCode(w, 400, "wakeup_capacity_exceeded", pgErr.Message)
		return
	}
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		writeErrorCode(w, 409, "wakeup_source_busy", "source run is changing; retry registration")
		return
	}
	switch {
	case errors.Is(err, service.ErrWakeupConflict):
		writeError(w, 409, err.Error())
	case errors.Is(err, service.ErrWakeupInput):
		writeError(w, 400, err.Error())
	case errors.Is(err, service.ErrWakeupForbidden):
		writeError(w, 403, "wakeup permission denied")
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, 404, "wakeup not found")
	default:
		writeError(w, 500, "could not save wakeup")
	}
}

func (h *Handler) ListIssueWakeups(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, 500, "failed to resolve agent access")
		return
	}
	ids := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, parseUUID(id))
	}
	rows, err := h.Queries.ListIssueWakeups(r.Context(), db.ListIssueWakeupsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, AgentIds: ids})
	if err != nil {
		wakeupError(w, err)
		return
	}
	if rows == nil {
		rows = []db.ListIssueWakeupsRow{}
	}
	writeJSON(w, 200, rows)
}

func (h *Handler) ListWorkspaceWakeupSummaries(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, 500, "failed to resolve agent access")
		return
	}
	ids := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, parseUUID(id))
	}
	rows, err := h.Queries.ListWorkspaceWakeupSummaryRows(r.Context(), db.ListWorkspaceWakeupSummaryRowsParams{WorkspaceID: parseUUID(workspaceID), AgentIds: ids})
	if err != nil {
		wakeupError(w, err)
		return
	}
	// The condition is JSON; sqlc cannot type it through the window CTE.
	type summaryRow struct {
		db.ListWorkspaceWakeupSummaryRowsRow
		Condition json.RawMessage `json:"condition"`
	}
	out := make([]summaryRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, summaryRow{ListWorkspaceWakeupSummaryRowsRow: row, Condition: json.RawMessage(row.Condition)})
	}
	writeJSON(w, 200, out)
}

func (h *Handler) CreateIssueWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var in service.WakeupInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, 400, "invalid wakeup body")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	if in.AgentID == "" && actorType == "agent" {
		in.AgentID = actorID
	}
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if originator == "" {
		writeError(w, 403, "a human originator is required")
		return
	}
	member := parseUUID(originator)
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	existingID := pgtype.UUID{}
	if rawID := chi.URLParam(r, "wakeupID"); rawID != "" {
		var valid bool
		existingID, valid = parseUUIDOrBadRequest(w, rawID, "wakeup id")
		if !valid {
			return
		}
	}
	if r.Method == http.MethodPut && !existingID.Valid {
		writeError(w, 400, "invalid wakeup id")
		return
	}
	result, err := svc.Save(r.Context(), issue.ID, member, h.wakeupSourceTaskID(r), existingID, in)
	if err != nil {
		wakeupError(w, err)
		return
	}
	status := http.StatusCreated
	if existingID.Valid {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (h *Handler) DisableIssueWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if originator == "" {
		writeError(w, 403, "a human originator is required")
		return
	}
	member := parseUUID(originator)
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	result, err := svc.Disable(r.Context(), issue.ID, id, member)
	if err != nil {
		wakeupError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (h *Handler) wakeupSourceTaskID(r *http.Request) pgtype.UUID {
	actorType, _ := h.resolveActor(r, requestUserID(r), h.resolveWorkspaceID(r))
	if actorType != "agent" {
		return pgtype.UUID{}
	}
	return h.commentSourceTaskID(r)
}

func (h *Handler) EnableIssueWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	var in service.WakeupEnableInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, 400, "invalid enable body")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if originator == "" {
		writeError(w, 403, "a human originator is required")
		return
	}
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	result, err := svc.Enable(r.Context(), issue.ID, parseUUID(originator), h.wakeupSourceTaskID(r), id, in)
	if err != nil {
		wakeupError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

func (h *Handler) EditIssueWakeupInstruction(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	var in service.WakeupInstructionInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 160000))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, 400, "invalid instruction body")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if originator == "" {
		writeError(w, 403, "a human originator is required")
		return
	}
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	if err := svc.EditInstruction(r.Context(), issue.ID, id, parseUUID(originator), in); err != nil {
		wakeupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// wakeupMember resolves the human a management request acts for, the same way
// as the create and disable endpoints.
func (h *Handler) wakeupMember(w http.ResponseWriter, r *http.Request, issue db.Issue) (pgtype.UUID, bool) {
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if originator == "" {
		writeError(w, 403, "a human originator is required")
		return pgtype.UUID{}, false
	}
	return parseUUID(originator), true
}

// TriggerIssueWakeup is "wake now": one run of the rule, as if it fired.
func (h *Handler) TriggerIssueWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	member, ok := h.wakeupMember(w, r, issue)
	if !ok {
		return
	}
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	if err := svc.Trigger(r.Context(), issue.ID, id, member); err != nil {
		wakeupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteIssueWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	member, ok := h.wakeupMember(w, r, issue)
	if !ok {
		return
	}
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	if err := svc.Delete(r.Context(), issue.ID, id, member); err != nil {
		wakeupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CheckInIssueWakeup ends a scheduled check silently: the calling run, which
// the rule itself started, records a note instead of posting a comment.
func (h *Handler) CheckInIssueWakeup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, 400, "invalid check-in body")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	task := h.wakeupSourceTaskID(r)
	if actorType != "agent" || !task.Valid {
		writeError(w, 403, "only the run a wakeup started can check in")
		return
	}
	svc := service.IssueWakeupService{Tasks: h.TaskService}
	if err := svc.CheckIn(r.Context(), issue.ID, id, task, actorID, in.Note); err != nil {
		wakeupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type wakeupRunResponse struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	StartedAt   *string  `json:"started_at"`
	CompletedAt *string  `json:"completed_at"`
	CheckinNote string   `json:"checkin_note"`
	Triggers    []string `json:"triggers"`
	Commented   bool     `json:"commented"`
}

// ListIssueWakeupRuns returns a rule's latest runs for its trigger history.
func (h *Handler) ListIssueWakeupRuns(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "wakeupID"), "wakeup id")
	if !ok {
		return
	}
	rule, err := h.Queries.GetIssueWakeup(r.Context(), db.GetIssueWakeupParams{ID: id, WorkspaceID: issue.WorkspaceID})
	if err != nil || rule.IssueID != issue.ID {
		writeError(w, 404, "wakeup not found")
		return
	}
	rows, err := h.Queries.ListWakeupRuns(r.Context(), db.ListWakeupRunsParams{WakeupID: uuidToString(id), IssueID: issue.ID})
	if err != nil {
		wakeupError(w, err)
		return
	}
	out := make([]wakeupRunResponse, 0, len(rows))
	for _, row := range rows {
		triggers := row.Triggers
		if triggers == nil {
			triggers = []string{}
		}
		out = append(out, wakeupRunResponse{
			ID: uuidToString(row.ID), Status: row.Status, CreatedAt: timestampToString(row.CreatedAt),
			StartedAt: timestampToPtr(row.StartedAt), CompletedAt: timestampToPtr(row.CompletedAt),
			CheckinNote: row.CheckinNote, Triggers: triggers, Commented: row.Commented,
		})
	}
	writeJSON(w, 200, out)
}

// ListPausedWakeups names the open issues whose rules the platform paused, for
// board cues.
func (h *Handler) ListPausedWakeups(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	rows, err := h.Queries.ListPausedWakeupIssues(r.Context(), parseUUID(workspaceID))
	if err != nil {
		wakeupError(w, err)
		return
	}
	type paused struct {
		IssueID string `json:"issue_id"`
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
		Reason  string `json:"paused_reason"`
	}
	out := make([]paused, 0, len(rows))
	for _, row := range rows {
		out = append(out, paused{IssueID: uuidToString(row.IssueID), ID: uuidToString(row.ID), AgentID: uuidToString(row.AgentID), Reason: row.PausedReason.String})
	}
	writeJSON(w, 200, out)
}
