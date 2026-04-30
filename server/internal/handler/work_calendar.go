package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkCalendarResponse is the JSON shape for a work calendar.
type WorkCalendarResponse struct {
	ID           string               `json:"id"`
	WorkspaceID  string               `json:"workspace_id"`
	Name         string               `json:"name"`
	Year         int32                `json:"year"`
	Days         []CalendarDayResponse `json:"days"`
	MonthlyHours []MonthlyHoursResponse `json:"monthly_hours"`
	Source       string               `json:"source"`
	Status       string               `json:"status"`
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
}

func workCalendarToResponse(wc db.WorkCalendar) WorkCalendarResponse {
	var days []CalendarDayResponse
	if wc.Days != nil {
		json.Unmarshal(wc.Days, &days)
	}
	var monthly []MonthlyHoursResponse
	if wc.MonthlyHours != nil {
		json.Unmarshal(wc.MonthlyHours, &monthly)
	}
	return WorkCalendarResponse{
		ID:           uuidToString(wc.ID),
		WorkspaceID:  uuidToString(wc.WorkspaceID),
		Name:         wc.Name,
		Year:         wc.Year,
		Days:         days,
		MonthlyHours: monthly,
		Source:       wc.Source,
		Status:       wc.Status,
		CreatedAt:    timestampToString(wc.CreatedAt),
		UpdatedAt:    timestampToString(wc.UpdatedAt),
	}
}

func (h *Handler) ListWorkCalendars(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)

	calendars, err := h.Queries.ListWorkCalendars(r.Context(), parseUUID(wsID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list work calendars")
		return
	}

	resp := make([]WorkCalendarResponse, len(calendars))
	for i, c := range calendars {
		resp[i] = workCalendarToResponse(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"calendars": resp})
}

func (h *Handler) GetWorkCalendar(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	calID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "calendarId"), "calendar id")
	if !ok {
		return
	}

	cal, err := h.Queries.GetWorkCalendar(r.Context(), db.GetWorkCalendarParams{
		ID:          calID,
		WorkspaceID: parseUUID(wsID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "work calendar not found")
		return
	}

	writeJSON(w, http.StatusOK, workCalendarToResponse(cal))
}

type createWorkCalendarRequest struct {
	Name         string               `json:"name"`
	Year         int32                `json:"year"`
	Days         []CalendarDayResponse `json:"days"`
	MonthlyHours []MonthlyHoursResponse `json:"monthly_hours"`
}

func (h *Handler) CreateWorkCalendar(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)

	var req createWorkCalendarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Year == 0 {
		writeError(w, http.StatusBadRequest, "name and year are required")
		return
	}

	daysJSON, _ := json.Marshal(req.Days)
	monthlyJSON, _ := json.Marshal(req.MonthlyHours)

	cal, err := h.Queries.CreateWorkCalendar(r.Context(), db.CreateWorkCalendarParams{
		WorkspaceID:  parseUUID(wsID),
		Name:         req.Name,
		Year:         req.Year,
		Days:         daysJSON,
		MonthlyHours: monthlyJSON,
		Source:       "manual",
		Status:       "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create work calendar")
		return
	}

	writeJSON(w, http.StatusCreated, workCalendarToResponse(cal))
}

func (h *Handler) UpdateWorkCalendar(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	calID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "calendarId"), "calendar id")
	if !ok {
		return
	}

	var req createWorkCalendarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	daysJSON, _ := json.Marshal(req.Days)
	monthlyJSON, _ := json.Marshal(req.MonthlyHours)

	cal, err := h.Queries.UpdateWorkCalendar(r.Context(), db.UpdateWorkCalendarParams{
		ID:           calID,
		WorkspaceID:  parseUUID(wsID),
		Name:         req.Name,
		Year:         req.Year,
		Days:         daysJSON,
		MonthlyHours: monthlyJSON,
		Status:       "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update work calendar")
		return
	}

	writeJSON(w, http.StatusOK, workCalendarToResponse(cal))
}

func (h *Handler) DeleteWorkCalendar(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	calID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "calendarId"), "calendar id")
	if !ok {
		return
	}

	err := h.Queries.DeleteWorkCalendar(r.Context(), db.DeleteWorkCalendarParams{
		ID:          calID,
		WorkspaceID: parseUUID(wsID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete work calendar")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ImportWorkCalendarFromPDF(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	name := r.FormValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}

	// Parse the PDF using color-based detection
	parsed, err := parseWorkCalendarPDF(data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "failed to parse PDF: "+err.Error())
		return
	}

	daysJSON, _ := json.Marshal(parsed.Days)
	monthlyJSON, _ := json.Marshal(parsed.MonthlyHours)

	cal, err := h.Queries.CreateWorkCalendar(r.Context(), db.CreateWorkCalendarParams{
		WorkspaceID:  parseUUID(wsID),
		Name:         name,
		Year:         parsed.Year,
		Days:         daysJSON,
		MonthlyHours: monthlyJSON,
		Source:       "pdf_import",
		Status:       "active",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save work calendar")
		return
	}

	writeJSON(w, http.StatusCreated, workCalendarToResponse(cal))
}
