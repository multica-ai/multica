package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var workspaceSourceIDRE = regexp.MustCompile(`<!--\s*workspace-source-id:\s*([^>\n]+?)\s*-->`)

type WorkspaceControlState struct {
	SourceType string   `json:"source_type"`
	SourceID   string   `json:"source_id"`
	Writable   bool     `json:"writable"`
	Status     *string  `json:"status,omitempty"`
	Action     *string  `json:"action,omitempty"`
	Fields     []string `json:"fields,omitempty"`
	Error      *string  `json:"error,omitempty"`
	UpdatedAt  *string  `json:"updated_at,omitempty"`
}

type workspaceControlBinding struct {
	SourceType string
	SourceID   string
	Writable   bool
}

type workspaceControlMutation struct {
	ID          string
	IssueID     string
	SourceType  string
	SourceID    string
	Action      string
	Fields      []string
	Payload     map[string]any
	Status      string
	Error       *string
	CreatedAt   time.Time
	CreatedByID string
}

func parseWorkspaceControlBinding(description pgtype.Text) (workspaceControlBinding, bool) {
	if !description.Valid {
		return workspaceControlBinding{}, false
	}
	match := workspaceSourceIDRE.FindStringSubmatch(description.String)
	if len(match) != 2 {
		return workspaceControlBinding{}, false
	}
	sourceID := strings.TrimSpace(match[1])
	sourceType, _, ok := strings.Cut(sourceID, ":")
	if !ok || sourceType == "" {
		return workspaceControlBinding{}, false
	}
	binding := workspaceControlBinding{
		SourceType: sourceType,
		SourceID:   sourceID,
		Writable:   sourceType == "device" || sourceType == "md",
	}
	return binding, true
}

func workspaceControlProtectedFields(raw map[string]json.RawMessage) []string {
	fieldMap := map[string]string{
		"title":         "title",
		"description":   "description",
		"status":        "status",
		"priority":      "priority",
		"assignee_type": "assignee",
		"assignee_id":   "assignee",
		"position":      "position",
	}
	seen := map[string]bool{}
	fields := make([]string, 0, len(raw))
	for key, field := range fieldMap {
		if _, ok := raw[key]; ok && !seen[field] {
			seen[field] = true
			fields = append(fields, field)
		}
	}
	return fields
}

func enforceWorkspaceControlPolicy(issue db.Issue, action string, fields []string) (workspaceControlBinding, bool, error) {
	binding, ok := parseWorkspaceControlBinding(issue.Description)
	if !ok || len(fields) == 0 {
		return binding, ok, nil
	}
	if !binding.Writable {
		return binding, ok, errors.New("this Workspace source is read-only in Multica")
	}
	return binding, ok, nil
}

func (h *Handler) workspaceControlStateForIssue(ctx context.Context, issue db.Issue) *WorkspaceControlState {
	binding, ok := parseWorkspaceControlBinding(issue.Description)
	if !ok {
		return nil
	}
	state := &WorkspaceControlState{
		SourceType: binding.SourceType,
		SourceID:   binding.SourceID,
		Writable:   binding.Writable,
	}
	latest, ok := h.latestWorkspaceControlMutations(ctx, []pgtype.UUID{issue.ID})[uuidToString(issue.ID)]
	if !ok {
		return state
	}
	state.Status = &latest.Status
	state.Action = &latest.Action
	state.Fields = latest.Fields
	state.Error = latest.Error
	updatedAt := latest.CreatedAt.UTC().Format(time.RFC3339)
	state.UpdatedAt = &updatedAt
	return state
}

func (h *Handler) latestWorkspaceControlMutations(ctx context.Context, issueIDs []pgtype.UUID) map[string]workspaceControlMutation {
	out := map[string]workspaceControlMutation{}
	if len(issueIDs) == 0 || h.DB == nil {
		return out
	}
	placeholders := make([]string, 0, len(issueIDs))
	args := make([]any, 0, len(issueIDs))
	for i, id := range issueIDs {
		placeholders = append(placeholders, "$"+strconv.Itoa(i+1))
		args = append(args, id)
	}
	rows, err := h.DB.Query(ctx, `
		SELECT DISTINCT ON (issue_id)
			id, issue_id, source_type, source_id, action, fields, payload, status, error, created_at, created_by_id
		FROM workspace_control_mutation
		WHERE issue_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY issue_id, created_at DESC
	`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var mutation workspaceControlMutation
		var id pgtype.UUID
		var issueID pgtype.UUID
		var createdByID pgtype.UUID
		var payloadBytes []byte
		var errorText pgtype.Text
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&id, &issueID, &mutation.SourceType, &mutation.SourceID, &mutation.Action, &mutation.Fields, &payloadBytes, &mutation.Status, &errorText, &createdAt, &createdByID); err != nil {
			continue
		}
		mutation.ID = uuidToString(id)
		mutation.IssueID = uuidToString(issueID)
		mutation.CreatedByID = uuidToString(createdByID)
		if errorText.Valid {
			mutation.Error = &errorText.String
		}
		if createdAt.Valid {
			mutation.CreatedAt = createdAt.Time
		}
		_ = json.Unmarshal(payloadBytes, &mutation.Payload)
		out[mutation.IssueID] = mutation
	}
	return out
}

func (h *Handler) attachWorkspaceControlStates(ctx context.Context, responses []IssueResponse, issues []pgtype.UUID) {
	latest := h.latestWorkspaceControlMutations(ctx, issues)
	for i := range responses {
		state := workspaceControlStateFromDescription(responses[i].Description, latest[responses[i].ID])
		if state != nil {
			responses[i].WorkspaceControl = state
		}
	}
}

func workspaceControlStateFromDescription(description *string, latest workspaceControlMutation) *WorkspaceControlState {
	if description == nil {
		return nil
	}
	match := workspaceSourceIDRE.FindStringSubmatch(*description)
	if len(match) != 2 {
		return nil
	}
	sourceID := strings.TrimSpace(match[1])
	sourceType, _, ok := strings.Cut(sourceID, ":")
	if !ok || sourceType == "" {
		return nil
	}
	state := &WorkspaceControlState{
		SourceType: sourceType,
		SourceID:   sourceID,
		Writable:   sourceType == "device" || sourceType == "md",
	}
	if latest.Status != "" {
		state.Status = &latest.Status
		state.Action = &latest.Action
		state.Fields = latest.Fields
		state.Error = latest.Error
		updatedAt := latest.CreatedAt.UTC().Format(time.RFC3339)
		state.UpdatedAt = &updatedAt
	}
	return state
}

func (h *Handler) enqueueWorkspaceControlMutation(ctx context.Context, issue db.Issue, binding workspaceControlBinding, action string, fields []string, payload map[string]any, actorType string, actorID string) *WorkspaceControlState {
	if h.DB == nil || len(fields) == 0 {
		return nil
	}
	payloadBytes, _ := json.Marshal(payload)
	var mutationID pgtype.UUID
	err := h.DB.QueryRow(ctx, `
		INSERT INTO workspace_control_mutation (
			workspace_id, issue_id, source_type, source_id, action, fields, payload, created_by_type, created_by_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
		RETURNING id
	`, issue.WorkspaceID, issue.ID, binding.SourceType, binding.SourceID, action, fields, payloadBytes, actorType, parseUUID(actorID)).Scan(&mutationID)
	if err != nil {
		slog.Warn("workspace control mutation enqueue failed", "error", err, "issue_id", uuidToString(issue.ID), "source_id", binding.SourceID)
		return nil
	}
	go h.dispatchWorkspaceControlMutation(context.Background(), uuidToString(mutationID), binding, action, fields, payload)
	status := "pending"
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	return &WorkspaceControlState{
		SourceType: binding.SourceType,
		SourceID:   binding.SourceID,
		Writable:   binding.Writable,
		Status:     &status,
		Action:     &action,
		Fields:     fields,
		UpdatedAt:  &updatedAt,
	}
}

func (h *Handler) dispatchWorkspaceControlMutation(ctx context.Context, mutationID string, binding workspaceControlBinding, action string, fields []string, payload map[string]any) {
	webhookURL := strings.TrimSpace(os.Getenv("MULTICA_WORKSPACE_CONTROL_WEBHOOK_URL"))
	if webhookURL == "" || h.DB == nil {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"mutation_id": mutationID,
		"source_type": binding.SourceType,
		"source_id":   binding.SourceID,
		"action":      action,
		"fields":      fields,
		"payload":     payload,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		h.markWorkspaceControlMutationFailed(ctx, mutationID, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.markWorkspaceControlMutationFailed(ctx, mutationID, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		h.markWorkspaceControlMutationFailed(ctx, mutationID, resp.Status)
		return
	}
	_, _ = h.DB.Exec(ctx, `
		UPDATE workspace_control_mutation
		SET status = 'applied', error = NULL, applied_at = now(), updated_at = now()
		WHERE id = $1
	`, parseUUID(mutationID))
}

func (h *Handler) markWorkspaceControlMutationFailed(ctx context.Context, mutationID string, message string) {
	if h.DB == nil {
		return
	}
	_, _ = h.DB.Exec(ctx, `
		UPDATE workspace_control_mutation
		SET status = 'apply-failed', error = $2, updated_at = now()
		WHERE id = $1
	`, parseUUID(mutationID), message)
}
