package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueResponse is the JSON response for an issue.
type IssueResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Number        int32   `json:"number"`
	Identifier    string  `json:"identifier"`
	Title         string  `json:"title"`
	Description   *string `json:"description"`
	Status        string  `json:"status"`
	Priority      string  `json:"priority"`
	AssigneeType  *string `json:"assignee_type"`
	AssigneeID    *string `json:"assignee_id"`
	CreatorType   string  `json:"creator_type"`
	CreatorID     string  `json:"creator_id"`
	ParentIssueID *string `json:"parent_issue_id"`
	ProjectID     *string `json:"project_id"`
	Position      float64 `json:"position"`
	StartDate     *string `json:"start_date"`
	DueDate       *string `json:"due_date"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	// Metadata is the per-issue KV map (see issue_metadata.go). Always emitted
	// (empty object when unset) so frontend code can `issue.metadata[key]`
	// without nil-guarding the parent field.
	Metadata    map[string]any          `json:"metadata"`
	Reactions   []IssueReactionResponse `json:"reactions,omitempty"`
	Attachments []AttachmentResponse    `json:"attachments,omitempty"`
	// Labels are bulk-attached by list/detail endpoints so the client can render
	// chips without an N+1 round-trip per row. Pointer + omitempty so paths that
	// don't load labels (e.g. UpdateIssue, batch UpdateIssues, the issue:updated
	// WS broadcast) emit no `labels` field at all — the client merge then
	// preserves whatever labels are already in cache. nil pointer = "field
	// absent, do not touch"; non-nil (incl. empty slice) = authoritative list.
	Labels *[]LabelResponse `json:"labels,omitempty"`
}

// validIssueStatuses / validIssuePriorities mirror the CHECK constraints on
// the issue table. Write handlers pre-validate these so callers get a clean
// 400 with the allowed values instead of a database CHECK violation bubbling
// up as a 500.
var validIssueStatuses = []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
var validIssuePriorities = []string{"urgent", "high", "medium", "low", "none"}

func validateIssueEnum(w http.ResponseWriter, field, value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", ")))
	return false
}

func issueToResponse(i db.Issue, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Description:   textToPtr(i.Description),
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		StartDate:     dateToPtr(i.StartDate),
		DueDate:       dateToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
		Metadata:      parseIssueMetadata(i.Metadata),
	}
}

// issueListRowToResponse converts a list-query row (no description) to an IssueResponse.
func issueListRowToResponse(i db.ListIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Description:   textToPtr(i.Description),
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		StartDate:     dateToPtr(i.StartDate),
		DueDate:       dateToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
		Metadata:      parseIssueMetadata(i.Metadata),
	}
}

// labelsByIssue bulk-loads labels for the given issue IDs and returns a map
// keyed by issue UUID string. On error or empty input, returns an empty map —
// label rendering is non-critical and we'd rather serve issues without labels
// than fail the whole list call.
func (h *Handler) labelsByIssue(ctx context.Context, wsUUID pgtype.UUID, issueIDs []pgtype.UUID) map[string][]LabelResponse {
	out := map[string][]LabelResponse{}
	if len(issueIDs) == 0 {
		return out
	}
	rows, err := h.Queries.ListLabelsForIssues(ctx, db.ListLabelsForIssuesParams{
		IssueIds:    issueIDs,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Warn("ListLabelsForIssues failed", "error", err)
		return out
	}
	for _, r := range rows {
		issueID := uuidToString(r.IssueID)
		out[issueID] = append(out[issueID], LabelResponse{
			ID:          uuidToString(r.ID),
			WorkspaceID: uuidToString(r.WorkspaceID),
			Name:        r.Name,
			Color:       r.Color,
			CreatedAt:   timestampToString(r.CreatedAt),
			UpdatedAt:   timestampToString(r.UpdatedAt),
		})
	}
	return out
}

func openIssueRowToResponse(i db.ListOpenIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Description:   textToPtr(i.Description),
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		StartDate:     dateToPtr(i.StartDate),
		DueDate:       dateToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
		Metadata:      parseIssueMetadata(i.Metadata),
	}
}
