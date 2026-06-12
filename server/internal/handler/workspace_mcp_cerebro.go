package handler

// CEREBRO-PATCH(workspace-mcp-http): TECH-3405 workspace-scoped HTTP MCP endpoint.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkspaceMCP exposes a small workspace-scoped MCP surface over HTTP. It is
// mounted behind RequireWorkspaceMemberFromURL, so a connection can target a
// different workspace only by presenting credentials that are valid there.
func (h *Handler) WorkspaceMCP(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return
	}
	userID := r.Header.Get("X-User-ID")
	if strings.TrimSpace(userID) == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	srv := mcp.NewServer("multica-workspace", "1.0")
	h.registerWorkspaceMCPTools(srv, r, workspaceID, wsUUID, userID)
	srv.ServeHTTP(w, r)
}

func (h *Handler) registerWorkspaceMCPTools(srv *mcp.Server, r *http.Request, workspaceID string, wsUUID pgtype.UUID, userID string) {
	srv.RegisterTool(mcp.Tool{
		Name:        "create_issue",
		Description: "Create a new issue in this workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"title"},
			"properties": map[string]any{
				"title":           map[string]any{"type": "string", "description": "Issue title"},
				"description":     map[string]any{"type": "string", "description": "Markdown description"},
				"status":          map[string]any{"type": "string", "description": "Initial status: backlog, todo, in_progress, done, cancelled"},
				"priority":        map[string]any{"type": "string", "description": "Priority: urgent, high, medium, low, none"},
				"project_id":      map[string]any{"type": "string", "description": "Project ID to link the issue to"},
				"parent_issue_id": map[string]any{"type": "string", "description": "Parent issue ID for sub-issues"},
				"assignee_type":   map[string]any{"type": "string", "description": "Assignee type: member or agent"},
				"assignee_id":     map[string]any{"type": "string", "description": "Assignee ID"},
				"allow_duplicate": map[string]any{"type": "boolean", "description": "Allow creating a duplicate active issue"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return h.createIssueFromWorkspaceMCP(ctx, r, workspaceID, wsUUID, userID, args)
	})
}

func (h *Handler) createIssueFromWorkspaceMCP(ctx context.Context, r *http.Request, workspaceID string, wsUUID pgtype.UUID, userID string, args map[string]any) (mcp.CallToolResult, error) {
	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return mcp.ErrorResult("title is required"), nil
	}

	status := stringArg(args, "status")
	if status == "" {
		status = "todo"
	}
	priority := stringArg(args, "priority")
	if priority == "" {
		priority = "none"
	}

	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if v := stringArg(args, "assignee_type"); v != "" {
		assigneeType = pgtype.Text{String: v, Valid: true}
	}
	if v := stringArg(args, "assignee_id"); v != "" {
		id, err := util.ParseUUID(v)
		if err != nil {
			return mcp.ErrorResult("invalid assignee_id"), nil
		}
		assigneeID = id
	}
	if statusCode, msg := h.validateAssigneePair(ctx, r, workspaceID, assigneeType, assigneeID); statusCode != 0 {
		return mcp.ErrorResult(msg), nil
	}

	var parentIssueID pgtype.UUID
	if v := stringArg(args, "parent_issue_id"); v != "" {
		id, err := util.ParseUUID(v)
		if err != nil {
			return mcp.ErrorResult("invalid parent_issue_id"), nil
		}
		parentIssueID = id
	}

	var projectID pgtype.UUID
	if v := stringArg(args, "project_id"); v != "" {
		id, err := util.ParseUUID(v)
		if err != nil {
			return mcp.ErrorResult("invalid project_id"), nil
		}
		projectID = id
	}

	creatorType, actualCreatorID := h.resolveActor(r, userID, workspaceID)
	analyticsAgentID := ""
	if assigneeType.Valid && assigneeType.String == "agent" {
		analyticsAgentID = uuidToString(assigneeID)
	}
	if creatorType == "agent" && analyticsAgentID == "" {
		analyticsAgentID = actualCreatorID
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID:    wsUUID,
		Title:          title,
		Description:    pgtype.Text{String: stringArg(args, "description"), Valid: stringArg(args, "description") != ""},
		Status:         status,
		Priority:       priority,
		AssigneeType:   assigneeType,
		AssigneeID:     assigneeID,
		CreatorType:    creatorType,
		CreatorID:      parseUUID(actualCreatorID),
		ParentIssueID:  parentIssueID,
		ProjectID:      projectID,
		AllowDuplicate: boolArg(args, "allow_duplicate"),
	}, service.IssueCreateOpts{
		ActorID:          actualCreatorID,
		AnalyticsAgentID: analyticsAgentID,
		Platform:         "mcp_http",
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment) map[string]any {
			return map[string]any{"issue": issueToResponse(issue, prefix)}
		},
	})
	if errors.Is(err, service.ErrActiveDuplicate) {
		dup := issueToResponse(*res.DuplicateIssue, h.getIssuePrefix(ctx, res.DuplicateIssue.WorkspaceID))
		return mcp.ErrorResult(duplicateIssueMessage(dup)), nil
	}
	if errors.Is(err, service.ErrParentIssueNotFound) {
		return mcp.ErrorResult("parent issue not found in this workspace"), nil
	}
	if errors.Is(err, service.ErrProjectNotFound) {
		return mcp.ErrorResult("project not found in this workspace"), nil
	}
	if err != nil {
		return mcp.ErrorResult(fmt.Sprintf("create_issue: %s", err)), nil
	}

	resp := issueToResponse(res.Issue, prefix)
	raw, err := json.Marshal(resp)
	if err != nil {
		return mcp.ErrorResult("create_issue: marshal result failed"), nil
	}
	return mcp.CallToolResult{
		Content:           []mcp.Content{{Type: "text", Text: string(raw)}},
		StructuredContent: raw,
	}, nil
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func boolArg(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}
