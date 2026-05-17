package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type CRMReminderResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	AssigneeUserID *string `json:"assignee_user_id"`
	SourceType     string  `json:"source_type"`
	SourceID       *string `json:"source_id"`
	Title          string  `json:"title"`
	Body           *string `json:"body"`
	Priority       string  `json:"priority"`
	DueAt          *string `json:"due_at"`
	Status         string  `json:"status"`
	CreatedBy      string  `json:"created_by"`
	ProjectID      *string `json:"project_id"`
	IssueID        *string `json:"issue_id"`
	CustomerID     *string `json:"customer_id"`
	EmailThreadID  *string `json:"email_thread_id"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type crmReminderRow struct {
	id, workspaceID, assigneeUserID, sourceID, projectID, issueID, customerID, emailThreadID pgtype.UUID
	sourceType, title, priority, status, createdBy                                           string
	body                                                                                     pgtype.Text
	dueAt, createdAt, updatedAt                                                              pgtype.Timestamptz
}

type createCRMReminderRequest struct {
	AssigneeUserID string `json:"assignee_user_id"`
	SourceType     string `json:"source_type"`
	SourceID       string `json:"source_id"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Priority       string `json:"priority"`
	DueAt          string `json:"due_at"`
	CreatedBy      string `json:"created_by"`
	ProjectID      string `json:"project_id"`
	IssueID        string `json:"issue_id"`
	CustomerID     string `json:"customer_id"`
	EmailThreadID  string `json:"email_thread_id"`
}

type updateCRMReminderStatusRequest struct {
	Status string `json:"status"`
	DueAt  string `json:"due_at"`
}

func crmReminderToResponse(row crmReminderRow) CRMReminderResponse {
	return CRMReminderResponse{
		ID:             uuidToString(row.id),
		WorkspaceID:    uuidToString(row.workspaceID),
		AssigneeUserID: uuidToPtr(row.assigneeUserID),
		SourceType:     row.sourceType,
		SourceID:       uuidToPtr(row.sourceID),
		Title:          row.title,
		Body:           textToPtr(row.body),
		Priority:       row.priority,
		DueAt:          timestampToPtr(row.dueAt),
		Status:         row.status,
		CreatedBy:      row.createdBy,
		ProjectID:      uuidToPtr(row.projectID),
		IssueID:        uuidToPtr(row.issueID),
		CustomerID:     uuidToPtr(row.customerID),
		EmailThreadID:  uuidToPtr(row.emailThreadID),
		CreatedAt:      timestampToString(row.createdAt),
		UpdatedAt:      timestampToString(row.updatedAt),
	}
}

func scanCRMReminder(row pgx.Row) (crmReminderRow, error) {
	var reminder crmReminderRow
	err := row.Scan(
		&reminder.id, &reminder.workspaceID, &reminder.assigneeUserID, &reminder.sourceType, &reminder.sourceID,
		&reminder.title, &reminder.body, &reminder.priority, &reminder.dueAt, &reminder.status, &reminder.createdBy,
		&reminder.projectID, &reminder.issueID, &reminder.customerID, &reminder.emailThreadID, &reminder.createdAt, &reminder.updatedAt,
	)
	return reminder, err
}

func nullableCRMUUID(w http.ResponseWriter, value string, field string) (pgtype.UUID, bool) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, value, field)
}

func nullableCRMText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func nullableCRMTimestamp(w http.ResponseWriter, value string, field string) (pgtype.Timestamptz, bool) {
	if strings.TrimSpace(value) == "" {
		return pgtype.Timestamptz{}, true
	}
	var ts pgtype.Timestamptz
	if err := ts.Scan(value); err != nil {
		writeError(w, http.StatusBadRequest, field+" must be an ISO timestamp")
		return pgtype.Timestamptz{}, false
	}
	return ts, true
}

func (h *Handler) ListCRMReminders(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "active"
	}

	query := `SELECT id, workspace_id, assignee_user_id, source_type, source_id, title, body, priority, due_at, status, created_by, project_id, issue_id, customer_id, email_thread_id, created_at, updated_at
		FROM crm_reminder
		WHERE workspace_id=$1 AND (assignee_user_id IS NULL OR assignee_user_id=$2)`
	args := []any{workspaceID, parseUUID(userID)}
	if status == "active" {
		query += ` AND status IN ('unread','read','snoozed')`
	} else if status != "all" {
		query += ` AND status=$3`
		args = append(args, status)
	}
	query += ` ORDER BY COALESCE(due_at, created_at) ASC, created_at DESC LIMIT 200`

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list CRM reminders")
		return
	}
	defer rows.Close()

	reminders := []CRMReminderResponse{}
	for rows.Next() {
		reminder, err := scanCRMReminder(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan CRM reminder")
			return
		}
		reminders = append(reminders, crmReminderToResponse(reminder))
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": reminders, "total": len(reminders)})
}

func (h *Handler) CreateCRMReminder(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	var req createCRMReminderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "normal"
	}
	createdBy := strings.TrimSpace(req.CreatedBy)
	if createdBy == "" {
		createdBy = "manual"
	}
	sourceType := strings.TrimSpace(req.SourceType)
	if sourceType == "" {
		sourceType = "manual"
	}
	assigneeUserID, ok := nullableCRMUUID(w, req.AssigneeUserID, "assignee_user_id")
	if !ok {
		return
	}
	sourceID, ok := nullableCRMUUID(w, req.SourceID, "source_id")
	if !ok {
		return
	}
	projectID, ok := nullableCRMUUID(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	issueID, ok := nullableCRMUUID(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	customerID, ok := nullableCRMUUID(w, req.CustomerID, "customer_id")
	if !ok {
		return
	}
	emailThreadID, ok := nullableCRMUUID(w, req.EmailThreadID, "email_thread_id")
	if !ok {
		return
	}
	dueAt, ok := nullableCRMTimestamp(w, req.DueAt, "due_at")
	if !ok {
		return
	}

	reminder, err := scanCRMReminder(h.DB.QueryRow(r.Context(), `INSERT INTO crm_reminder (workspace_id, assignee_user_id, source_type, source_id, title, body, priority, due_at, created_by, project_id, issue_id, customer_id, email_thread_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, workspace_id, assignee_user_id, source_type, source_id, title, body, priority, due_at, status, created_by, project_id, issue_id, customer_id, email_thread_id, created_at, updated_at`,
		workspaceID, assigneeUserID, sourceType, sourceID, title, nullableCRMText(req.Body), priority, dueAt, createdBy, projectID, issueID, customerID, emailThreadID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create CRM reminder")
		return
	}
	writeJSON(w, http.StatusCreated, crmReminderToResponse(reminder))
}

func (h *Handler) UpdateCRMReminderStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.crmWorkspaceUUID(w, r)
	if !ok {
		return
	}
	reminderID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "reminderId"), "reminder id")
	if !ok {
		return
	}
	var req updateCRMReminderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "read" && status != "unread" && status != "done" && status != "snoozed" {
		writeError(w, http.StatusBadRequest, "status must be read, unread, done, or snoozed")
		return
	}
	dueAt, ok := nullableCRMTimestamp(w, req.DueAt, "due_at")
	if !ok {
		return
	}
	if status == "snoozed" && !dueAt.Valid {
		writeError(w, http.StatusBadRequest, "due_at is required when snoozing")
		return
	}
	reminder, err := scanCRMReminder(h.DB.QueryRow(r.Context(), `UPDATE crm_reminder SET status=$3, due_at=COALESCE($4, due_at), updated_at=now()
		WHERE id=$1 AND workspace_id=$2
		RETURNING id, workspace_id, assignee_user_id, source_type, source_id, title, body, priority, due_at, status, created_by, project_id, issue_id, customer_id, email_thread_id, created_at, updated_at`, reminderID, workspaceID, status, dueAt))
	if err != nil {
		writeError(w, http.StatusNotFound, "CRM reminder not found")
		return
	}
	writeJSON(w, http.StatusOK, crmReminderToResponse(reminder))
}
