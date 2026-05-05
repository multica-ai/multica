package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var defaultIssueWorkflowStatuses = []issueWorkflowSeedStatus{
	{Key: "backlog", Name: "Backlog", Description: "Parked before the main workflow starts", Category: "backlog", Position: 0, OnMainGraph: false},
	{Key: "todo", Name: "TODO", Description: "Ready to start", Category: "unstarted", Position: 10, OnMainGraph: true, IsInitial: true},
	{Key: "in_progress", Name: "In progress", Description: "Work is underway", Category: "started", Position: 20, OnMainGraph: true},
	{Key: "in_review", Name: "In review", Description: "Awaiting review", Category: "review", Position: 30, OnMainGraph: true},
	{Key: "done", Name: "Done", Description: "Completed successfully", Category: "completed", Position: 40, OnMainGraph: true, IsTerminal: true},
	{Key: "blocked", Name: "Blocked", Description: "Stuck on an external factor", Category: "blocked", Position: 50, OnMainGraph: false},
	{Key: "cancelled", Name: "Cancelled", Description: "Cancelled", Category: "cancelled", Position: 60, OnMainGraph: false, IsTerminal: true},
}

type issueWorkflowSeedStatus struct {
	Key         string
	Name        string
	Description string
	Category    string
	Position    float64
	OnMainGraph bool
	IsInitial   bool
	IsTerminal  bool
}

type issueWorkflowStatus struct {
	ID          pgtype.UUID
	WorkflowID  pgtype.UUID
	Key         string
	Name        string
	Description pgtype.Text
	Category    string
	Color       pgtype.Text
	Icon        pgtype.Text
	Position    float64
	OnMainGraph bool
	IsInitial   bool
	IsTerminal  bool
}

type issueWorkflowTransition struct {
	Status      issueWorkflowStatus
	ActionLabel pgtype.Text
	Description pgtype.Text
}

type issueStatusResponse struct {
	ID          string  `json:"id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Category    string  `json:"category"`
	Color       *string `json:"color,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Position    float64 `json:"position"`
	OnMainGraph bool    `json:"on_main_graph"`
	IsInitial   bool    `json:"is_initial"`
	IsTerminal  bool    `json:"is_terminal"`
}

type issueTransitionResponse struct {
	issueStatusResponse
	ActionLabel           *string `json:"action_label,omitempty"`
	TransitionDescription *string `json:"transition_description,omitempty"`
}

type issueAvailableTransitionsResponse struct {
	IssueID       string                    `json:"issue_id"`
	WorkflowID    string                    `json:"workflow_id"`
	CurrentStatus issueStatusResponse       `json:"current_status"`
	Transitions   []issueTransitionResponse `json:"transitions"`
}

func (h *Handler) ensureDefaultIssueWorkflow(ctx context.Context, workspaceID, projectID pgtype.UUID) (pgtype.UUID, error) {
	if h.DB == nil {
		return pgtype.UUID{}, errors.New("database executor unavailable")
	}

	workflow, err := h.findDefaultIssueWorkflow(ctx, workspaceID, projectID)
	if err == nil {
		return workflow, nil
	}
	if !isNoRows(err) {
		return pgtype.UUID{}, err
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, err
	}
	defer tx.Rollback(ctx)

	workflow, err = h.findDefaultIssueWorkflowTx(ctx, tx, workspaceID, projectID)
	if err == nil {
		return workflow, tx.Commit(ctx)
	}
	if !isNoRows(err) {
		return pgtype.UUID{}, err
	}

	var name string
	var description string
	var row pgx.Row
	if projectID.Valid {
		name = "Default"
		description = "Default issue workflow"
		row = tx.QueryRow(ctx, `
			INSERT INTO issue_workflow (workspace_id, project_id, name, description, is_default)
			VALUES ($1, $2, $3, $4, true)
			ON CONFLICT DO NOTHING
			RETURNING id
		`, workspaceID, projectID, name, description)
	} else {
		name = "Default"
		description = "Default workflow for projectless issues"
		row = tx.QueryRow(ctx, `
			INSERT INTO issue_workflow (workspace_id, project_id, name, description, is_default)
			VALUES ($1, NULL, $2, $3, true)
			ON CONFLICT DO NOTHING
			RETURNING id
		`, workspaceID, name, description)
	}
	if err := row.Scan(&workflow); err != nil {
		if !isNoRows(err) {
			return pgtype.UUID{}, err
		}
		workflow, err = h.findDefaultIssueWorkflowTx(ctx, tx, workspaceID, projectID)
		if err != nil {
			return pgtype.UUID{}, err
		}
	}

	if err := seedDefaultIssueWorkflowTx(ctx, tx, workflow); err != nil {
		return pgtype.UUID{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, err
	}
	return workflow, nil
}

func (h *Handler) findDefaultIssueWorkflow(ctx context.Context, workspaceID, projectID pgtype.UUID) (pgtype.UUID, error) {
	if projectID.Valid {
		var workflowID pgtype.UUID
		err := h.DB.QueryRow(ctx, `
			SELECT id FROM issue_workflow
			WHERE workspace_id = $1 AND project_id = $2 AND is_default = true
			LIMIT 1
		`, workspaceID, projectID).Scan(&workflowID)
		return workflowID, err
	}
	var workflowID pgtype.UUID
	err := h.DB.QueryRow(ctx, `
		SELECT id FROM issue_workflow
		WHERE workspace_id = $1 AND project_id IS NULL AND is_default = true
		LIMIT 1
	`, workspaceID).Scan(&workflowID)
	return workflowID, err
}

func (h *Handler) findDefaultIssueWorkflowTx(ctx context.Context, tx pgx.Tx, workspaceID, projectID pgtype.UUID) (pgtype.UUID, error) {
	if projectID.Valid {
		var workflowID pgtype.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM issue_workflow
			WHERE workspace_id = $1 AND project_id = $2 AND is_default = true
			LIMIT 1
		`, workspaceID, projectID).Scan(&workflowID)
		return workflowID, err
	}
	var workflowID pgtype.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM issue_workflow
		WHERE workspace_id = $1 AND project_id IS NULL AND is_default = true
		LIMIT 1
	`, workspaceID).Scan(&workflowID)
	return workflowID, err
}

func seedDefaultIssueWorkflowTx(ctx context.Context, tx pgx.Tx, workflowID pgtype.UUID) error {
	for _, s := range defaultIssueWorkflowStatuses {
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_status_def (workflow_id, key, name, description, category, position, on_main_graph, is_initial, is_terminal)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (workflow_id, key) DO NOTHING
		`, workflowID, s.Key, s.Name, s.Description, s.Category, s.Position, s.OnMainGraph, s.IsInitial, s.IsTerminal); err != nil {
			return err
		}
	}

	for _, from := range defaultIssueWorkflowStatuses {
		var fromID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			SELECT id FROM issue_status_def WHERE workflow_id = $1 AND key = $2
		`, workflowID, from.Key).Scan(&fromID); err != nil {
			return err
		}
		for _, to := range defaultIssueWorkflowStatuses {
			if from.Key == to.Key {
				continue
			}
			var toID pgtype.UUID
			if err := tx.QueryRow(ctx, `
				SELECT id FROM issue_status_def WHERE workflow_id = $1 AND key = $2
			`, workflowID, to.Key).Scan(&toID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO issue_status_transition (workflow_id, from_status_id, to_status_id, action_label)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT DO NOTHING
			`, workflowID, fromID, toID, "Move to "+to.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Handler) resolveIssueStatusKey(ctx context.Context, workspaceID, projectID pgtype.UUID, requested string) (string, error) {
	statusKey := strings.TrimSpace(requested)
	workflowID, err := h.ensureDefaultIssueWorkflow(ctx, workspaceID, projectID)
	if err != nil {
		return "", err
	}
	if statusKey == "" {
		initial, err := h.loadInitialIssueStatus(ctx, workflowID)
		if err != nil {
			return "", err
		}
		return initial.Key, nil
	}
	if _, err := h.loadIssueStatusByKey(ctx, workflowID, statusKey); err != nil {
		return "", err
	}
	return statusKey, nil
}

func (h *Handler) loadInitialIssueStatus(ctx context.Context, workflowID pgtype.UUID) (issueWorkflowStatus, error) {
	return h.scanIssueWorkflowStatus(h.DB.QueryRow(ctx, `
		SELECT id, workflow_id, key, name, description, category, color, icon, position, on_main_graph, is_initial, is_terminal
		FROM issue_status_def
		WHERE workflow_id = $1 AND is_initial = true
		ORDER BY position ASC
		LIMIT 1
	`, workflowID))
}

func (h *Handler) loadIssueStatusByKey(ctx context.Context, workflowID pgtype.UUID, key string) (issueWorkflowStatus, error) {
	return h.scanIssueWorkflowStatus(h.DB.QueryRow(ctx, `
		SELECT id, workflow_id, key, name, description, category, color, icon, position, on_main_graph, is_initial, is_terminal
		FROM issue_status_def
		WHERE workflow_id = $1 AND key = $2
	`, workflowID, key))
}

func (h *Handler) scanIssueWorkflowStatus(row pgx.Row) (issueWorkflowStatus, error) {
	var s issueWorkflowStatus
	err := row.Scan(&s.ID, &s.WorkflowID, &s.Key, &s.Name, &s.Description, &s.Category, &s.Color, &s.Icon, &s.Position, &s.OnMainGraph, &s.IsInitial, &s.IsTerminal)
	return s, err
}

func (h *Handler) listIssueTransitions(ctx context.Context, workflowID, fromStatusID pgtype.UUID) ([]issueWorkflowTransition, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT sd.id, sd.workflow_id, sd.key, sd.name, sd.description, sd.category, sd.color, sd.icon,
		       sd.position, sd.on_main_graph, sd.is_initial, sd.is_terminal,
		       tr.action_label, tr.description
		FROM issue_status_transition tr
		JOIN issue_status_def sd ON sd.id = tr.to_status_id
		WHERE tr.workflow_id = $1 AND tr.from_status_id = $2
		ORDER BY sd.position ASC, sd.key ASC
	`, workflowID, fromStatusID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []issueWorkflowTransition{}
	for rows.Next() {
		var tr issueWorkflowTransition
		if err := rows.Scan(
			&tr.Status.ID,
			&tr.Status.WorkflowID,
			&tr.Status.Key,
			&tr.Status.Name,
			&tr.Status.Description,
			&tr.Status.Category,
			&tr.Status.Color,
			&tr.Status.Icon,
			&tr.Status.Position,
			&tr.Status.OnMainGraph,
			&tr.Status.IsInitial,
			&tr.Status.IsTerminal,
			&tr.ActionLabel,
			&tr.Description,
		); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) GetIssueAvailableTransitions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	workflowID, err := h.ensureDefaultIssueWorkflow(r.Context(), issue.WorkspaceID, issue.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue workflow")
		return
	}
	current, err := h.loadIssueStatusByKey(r.Context(), workflowID, issue.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load current status")
		return
	}
	transitions, err := h.listIssueTransitions(r.Context(), workflowID, current.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load available transitions")
		return
	}
	resp := issueAvailableTransitionsResponse{
		IssueID:       uuidToString(issue.ID),
		WorkflowID:    uuidToString(workflowID),
		CurrentStatus: issueStatusToResponse(current),
		Transitions:   make([]issueTransitionResponse, len(transitions)),
	}
	for i, tr := range transitions {
		resp.Transitions[i] = issueTransitionToResponse(tr)
	}
	writeJSON(w, http.StatusOK, resp)
}

func issueStatusToResponse(s issueWorkflowStatus) issueStatusResponse {
	return issueStatusResponse{
		ID:          uuidToString(s.ID),
		Key:         s.Key,
		Name:        s.Name,
		Description: textToPtr(s.Description),
		Category:    s.Category,
		Color:       textToPtr(s.Color),
		Icon:        textToPtr(s.Icon),
		Position:    s.Position,
		OnMainGraph: s.OnMainGraph,
		IsInitial:   s.IsInitial,
		IsTerminal:  s.IsTerminal,
	}
}

func issueTransitionToResponse(tr issueWorkflowTransition) issueTransitionResponse {
	return issueTransitionResponse{
		issueStatusResponse:   issueStatusToResponse(tr.Status),
		ActionLabel:           textToPtr(tr.ActionLabel),
		TransitionDescription: textToPtr(tr.Description),
	}
}

func (h *Handler) ensureDefaultIssueWorkflowForProject(ctx context.Context, project db.Project) error {
	_, err := h.ensureDefaultIssueWorkflow(ctx, project.WorkspaceID, project.ID)
	return err
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows)
}

func issueWorkflowErrorResponse(err error) (int, string) {
	if isNoRows(err) {
		return http.StatusUnprocessableEntity, "status is not defined in this issue workflow"
	}
	return http.StatusInternalServerError, "failed to resolve issue workflow"
}
