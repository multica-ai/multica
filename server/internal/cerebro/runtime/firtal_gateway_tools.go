package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// firtalGatewayToolListCommentsLimit is the per-call cap on comments returned
// by list_comments. Matches the chat-task transcript cap so the model never
// sees more comments via the tool than it would in the inline context.
const firtalGatewayToolListCommentsLimit = firtalGatewayIssueCommentCap

// ToolContext is the per-task context the in-process tool handlers run with.
// It is constructed once per executeTask call and pins all handler activity to
// the task's calling agent + workspace. Cross-workspace access is impossible
// because every DB query filters by WorkspaceID from this context.
type ToolContext struct {
	AgentID     pgtype.UUID
	WorkspaceID pgtype.UUID
}

// NewFirtalGatewayToolServer builds the in-process MCP server with the POC
// tool set (get_issue, list_comments, add_comment). The returned server can
// be invoked via Server.Call(); tool definitions for the gateway request body
// are produced separately by GatewayToolDefs.
func NewFirtalGatewayToolServer(queries *db.Queries, tctx ToolContext) *mcp.Server {
	srv := mcp.NewServer("firtal-gateway-tools", "0.1.0")
	registerGatewayGetIssue(srv, queries, tctx)
	registerGatewayListComments(srv, queries, tctx)
	registerGatewayAddComment(srv, queries, tctx)
	return srv
}

// GatewayToolDefs returns the gateway-side (OpenAI-compatible) tool
// definitions matching the handlers registered by NewFirtalGatewayToolServer.
// The parameter schemas are the same JSON-schema objects the MCP tools use,
// so a future migration to a shared registry only needs to swap the
// dispatcher — no schema duplication.
func GatewayToolDefs() []GatewayToolDef {
	return []GatewayToolDef{
		{Type: "function", Function: GatewayToolFunction{
			Name:        "get_issue",
			Description: "Get a Multica issue's title, description, status, and comments. issue_id accepts either a UUID or an issue identifier like \"JEH-123\".",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"issue_id"},
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "Issue UUID or identifier (e.g. JEH-123).",
					},
				},
			},
		}},
		{Type: "function", Function: GatewayToolFunction{
			Name:        "list_comments",
			Description: "List all comments on an issue in chronological order.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"issue_id"},
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "Issue UUID or identifier (e.g. JEH-123).",
					},
				},
			},
		}},
		{Type: "function", Function: GatewayToolFunction{
			Name:        "add_comment",
			Description: "Post a new comment on an issue. The comment is authored by you (the calling agent).",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"issue_id", "content"},
				"properties": map[string]any{
					"issue_id": map[string]any{
						"type":        "string",
						"description": "Issue UUID or identifier (e.g. JEH-123).",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Markdown body of the comment.",
					},
				},
			},
		}},
	}
}

func registerGatewayGetIssue(srv *mcp.Server, queries *db.Queries, tctx ToolContext) {
	srv.RegisterTool(mcp.Tool{
		Name:        "get_issue",
		Description: "Get a Multica issue's title, description, status, and comments.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueRef, err := toolRequireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		issue, err := resolveIssue(ctx, queries, tctx.WorkspaceID, issueRef)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		comments, err := queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: tctx.WorkspaceID,
			Limit:       firtalGatewayToolListCommentsLimit,
		})
		if err != nil {
			return mcp.ErrorResult(fmt.Sprintf("list comments: %v", err)), nil
		}
		result := map[string]any{
			"id":       util.UUIDToString(issue.ID),
			"number":   issue.Number,
			"title":    issue.Title,
			"status":   issue.Status,
			"priority": issue.Priority,
			"comments": formatComments(comments),
		}
		if desc := strings.TrimSpace(issue.Description.String); desc != "" {
			result["description"] = desc
		}
		return jsonToolResult(result)
	})
}

func registerGatewayListComments(srv *mcp.Server, queries *db.Queries, tctx ToolContext) {
	srv.RegisterTool(mcp.Tool{
		Name:        "list_comments",
		Description: "List comments on an issue.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueRef, err := toolRequireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		issue, err := resolveIssue(ctx, queries, tctx.WorkspaceID, issueRef)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		comments, err := queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: tctx.WorkspaceID,
			Limit:       firtalGatewayToolListCommentsLimit,
		})
		if err != nil {
			return mcp.ErrorResult(fmt.Sprintf("list comments: %v", err)), nil
		}
		return jsonToolResult(map[string]any{
			"issue_id": util.UUIDToString(issue.ID),
			"comments": formatComments(comments),
		})
	})
}

func registerGatewayAddComment(srv *mcp.Server, queries *db.Queries, tctx ToolContext) {
	srv.RegisterTool(mcp.Tool{
		Name:        "add_comment",
		Description: "Add a comment to an issue authored by the calling agent.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id", "content"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueRef, err := toolRequireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		content, err := toolRequireString(args, "content")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if !tctx.AgentID.Valid {
			return mcp.ErrorResult("missing agent context for add_comment"), nil
		}
		issue, err := resolveIssue(ctx, queries, tctx.WorkspaceID, issueRef)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		comment, err := queries.CreateComment(ctx, db.CreateCommentParams{
			IssueID:     issue.ID,
			WorkspaceID: tctx.WorkspaceID,
			AuthorType:  "agent",
			AuthorID:    tctx.AgentID,
			Content:     content,
			Type:        "comment",
		})
		if err != nil {
			return mcp.ErrorResult(fmt.Sprintf("create comment: %v", err)), nil
		}
		return jsonToolResult(map[string]any{
			"id":         util.UUIDToString(comment.ID),
			"issue_id":   util.UUIDToString(comment.IssueID),
			"created_at": comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		})
	})
}

// resolveIssue accepts either a UUID or an identifier like "JEH-123" and
// returns the issue scoped to workspaceID. Returning a workspace-scoped row
// matches the handler-side loader; cross-workspace lookup is impossible.
func resolveIssue(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, ref string) (db.Issue, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Issue{}, fmt.Errorf("issue_id is required")
	}
	if number, ok := parseIssueIdentifierNumber(ref); ok {
		issue, err := queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
			WorkspaceID: workspaceID,
			Number:      number,
		})
		if err != nil {
			return db.Issue{}, fmt.Errorf("issue %q not found in workspace", ref)
		}
		return issue, nil
	}
	id, err := util.ParseUUID(ref)
	if err != nil {
		return db.Issue{}, fmt.Errorf("issue_id %q is neither a UUID nor an identifier", ref)
	}
	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.Issue{}, fmt.Errorf("issue %q not found in workspace", ref)
	}
	return issue, nil
}

// parseIssueIdentifierNumber extracts the numeric part of an issue
// identifier like "JEH-1089". It returns (0, false) for anything that
// doesn't end in `-<digits>` — the caller then treats the input as a UUID.
func parseIssueIdentifierNumber(s string) (int32, bool) {
	idx := strings.LastIndex(s, "-")
	if idx <= 0 || idx == len(s)-1 {
		return 0, false
	}
	prefix := s[:idx]
	if !isIssuePrefix(prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(s[idx+1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return int32(n), true
}

func isIssuePrefix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

func formatComments(comments []db.Comment) []map[string]any {
	out := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		entry := map[string]any{
			"id":          util.UUIDToString(c.ID),
			"author_type": c.AuthorType,
			"author_id":   util.UUIDToString(c.AuthorID),
			"content":     c.Content,
		}
		if c.CreatedAt.Valid {
			entry["created_at"] = c.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, entry)
	}
	return out
}

func toolRequireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("argument %q must not be empty", key)
	}
	return s, nil
}

func jsonToolResult(payload any) (mcp.CallToolResult, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return mcp.ErrorResult(fmt.Sprintf("marshal tool result: %v", err)), nil
	}
	return mcp.TextResult(string(raw)), nil
}
