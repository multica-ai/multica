package main

// MCP tool registry — Go port of packages/mcp/src/tools/. The JS
// implementation was a thin shim: each tool is name + zod schema +
// "call REST, return JSON." This file is the same shape in Go,
// reusing the cli.APIClient that every other CLI subcommand already
// uses for auth + workspace headers + error formatting.
//
// Adding a tool: write the registration alongside its peers (one
// register* function per resource), add it to RegisterAllMCPTools.
// Keep tool names stable — model providers' caches key on them.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Tool registry — wires every tool onto srv. Order here is the order the
// MCP picker shows them; front-load the high-leverage ones (issues,
// channels, agents) and put admin/configuration further down.
// ---------------------------------------------------------------------------

func RegisterAllMCPTools(srv *server.MCPServer, c *cli.APIClient) {
	registerIssueTools(srv, c)
	registerAgentTools(srv, c)
	registerChannelTools(srv, c)
	registerProjectTools(srv, c)
	registerMemoryTools(srv, c)
	registerLabelTools(srv, c)
	registerAutopilotTools(srv, c)
	registerWorkspaceTools(srv, c)
	registerShipHubTools(srv, c)
}

// ---------------------------------------------------------------------------
// Argument extraction helpers — mcp-go gives us map[string]any from the
// JSON-RPC payload; these translate to the typed values we need without
// each tool repeating the boilerplate. Missing optional values become
// the zero value or a sentinel ("") so the URL builders can skip them.
// ---------------------------------------------------------------------------

func argString(req mcp.CallToolRequest, name string) string {
	args := req.GetArguments()
	if v, ok := args[name].(string); ok {
		return v
	}
	return ""
}

func argInt(req mcp.CallToolRequest, name string) (int, bool) {
	args := req.GetArguments()
	v, ok := args[name]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return i, true
	}
	return 0, false
}

func argBool(req mcp.CallToolRequest, name string) (bool, bool) {
	args := req.GetArguments()
	v, ok := args[name]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// argRaw returns the raw value for a key. nil if missing. Used when a
// tool needs to pass a value through opaquely (e.g. autopilot payload).
func argRaw(req mcp.CallToolRequest, name string) any {
	return req.GetArguments()[name]
}

// argStringSlice extracts a JSON array of strings. Missing → nil.
func argStringSlice(req mcp.CallToolRequest, name string) []string {
	v := argRaw(req, name)
	if v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// requireString fetches a required string and returns a tool-error
// payload if it's missing or empty.
func requireString(req mcp.CallToolRequest, name string) (string, *mcp.CallToolResult) {
	v := argString(req, name)
	if v == "" {
		return "", mcp.NewToolResultError(fmt.Sprintf("missing required argument %q", name))
	}
	return v, nil
}

// queryString builds a URL query suffix from a list of key/value pairs.
// Empty values are skipped so the resulting query is compact. Returns
// the empty string when no params are set so callers can append it
// unconditionally.
func queryString(pairs ...[2]string) string {
	q := url.Values{}
	for _, kv := range pairs {
		if kv[1] == "" {
			continue
		}
		q.Set(kv[0], kv[1])
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// intStr converts an int+ok pair from argInt into a string for queryString.
// Returns "" when the int wasn't present, so it gets dropped from the URL.
func intStr(v int, ok bool) string {
	if !ok {
		return ""
	}
	return strconv.Itoa(v)
}

// boolStr formats a bool+ok pair from argBool. "" when missing.
func boolStr(v bool, ok bool) string {
	if !ok {
		return ""
	}
	if v {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// Issues — the highest-leverage surface for orchestrating agent work.
// ---------------------------------------------------------------------------

var issueStatusEnum = []string{"todo", "in_progress", "in_review", "done", "blocked", "backlog", "cancelled"}
var issuePriorityEnum = []string{"urgent", "high", "medium", "low", "none"}
var assigneeTypeEnum = []string{"member", "agent"}

func registerIssueTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_issue_list",
			mcp.WithDescription("List issues in the active workspace with optional filters. Use small limits (e.g. 20) when scanning so the response stays under a few KB."),
			mcp.WithString("status", mcp.Description("Issue status filter."), mcp.Enum(issueStatusEnum...)),
			mcp.WithString("priority", mcp.Description("Issue priority filter."), mcp.Enum(issuePriorityEnum...)),
			mcp.WithString("assignee_id", mcp.Description("Filter by assignee (member OR agent UUID).")),
			mcp.WithString("project_id", mcp.Description("Filter by project UUID.")),
			mcp.WithNumber("limit", mcp.Description("Default 50; cap 200.")),
			mcp.WithNumber("offset", mcp.Description("Skip the first N results.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			limit, hasLim := argInt(req, "limit")
			offset, hasOff := argInt(req, "offset")
			path := "/api/issues" + queryString(
				[2]string{"status", argString(req, "status")},
				[2]string{"priority", argString(req, "priority")},
				[2]string{"assignee", argString(req, "assignee_id")},
				[2]string{"project", argString(req, "project_id")},
				[2]string{"limit", intStr(limit, hasLim)},
				[2]string{"offset", intStr(offset, hasOff)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_search",
			mcp.WithDescription("Full-text search across issues by title, description, and identifier (e.g. 'MUL-123'). Returns the top matches."),
			mcp.WithString("q", mcp.Required(), mcp.Description("Free-text query.")),
			mcp.WithNumber("limit", mcp.Description("Default 10; cap 50.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			q, errResult := requireString(req, "q")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			path := "/api/issues/search" + queryString(
				[2]string{"q", q},
				[2]string{"limit", intStr(limit, hasLim)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_get",
			mcp.WithDescription("Fetch full issue details by UUID or human identifier (e.g. 'MUL-123'). Includes title, description, status, priority, assignee, project, parent, due date."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Issue UUID or 'PREFIX-NNN' identifier.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_create",
			mcp.WithDescription("Create a new issue. Title is required; everything else is optional. Pass 'assignee_type'+'assignee_id' to assign at creation (most common: assign to an agent so it picks up immediately)."),
			mcp.WithString("title", mcp.Required()),
			mcp.WithString("description"),
			mcp.WithString("status", mcp.Enum(issueStatusEnum...)),
			mcp.WithString("priority", mcp.Enum(issuePriorityEnum...)),
			mcp.WithString("assignee_type", mcp.Enum(assigneeTypeEnum...)),
			mcp.WithString("assignee_id"),
			mcp.WithString("parent_issue_id"),
			mcp.WithString("project_id"),
			mcp.WithString("due_date", mcp.Description("RFC3339 timestamp.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			title, errResult := requireString(req, "title")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{
				"title":           title,
				"description":     nullableString(argString(req, "description")),
				"status":          stringOrDefault(argString(req, "status"), "todo"),
				"priority":        stringOrDefault(argString(req, "priority"), "none"),
				"assignee_type":   nullableString(argString(req, "assignee_type")),
				"assignee_id":     nullableString(argString(req, "assignee_id")),
				"parent_issue_id": nullableString(argString(req, "parent_issue_id")),
				"project_id":      nullableString(argString(req, "project_id")),
				"due_date":        nullableString(argString(req, "due_date")),
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/issues", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_update",
			mcp.WithDescription("Patch one or more issue fields. Only fields you pass are updated. Use 'multica_issue_status' for the common status-only flip."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithString("title"),
			mcp.WithString("description"),
			mcp.WithString("status", mcp.Enum(issueStatusEnum...)),
			mcp.WithString("priority", mcp.Enum(issuePriorityEnum...)),
			mcp.WithString("assignee_type", mcp.Enum(assigneeTypeEnum...)),
			mcp.WithString("assignee_id", mcp.Description("Pass empty string to unassign.")),
			mcp.WithString("parent_issue_id"),
			mcp.WithString("project_id"),
			mcp.WithString("due_date"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{}
			setIfPresent(body, "title", argRaw(req, "title"))
			setIfPresent(body, "description", argRaw(req, "description"))
			setIfPresent(body, "status", argRaw(req, "status"))
			setIfPresent(body, "priority", argRaw(req, "priority"))
			setIfPresent(body, "assignee_type", argRaw(req, "assignee_type"))
			setIfPresent(body, "assignee_id", argRaw(req, "assignee_id"))
			setIfPresent(body, "parent_issue_id", argRaw(req, "parent_issue_id"))
			setIfPresent(body, "project_id", argRaw(req, "project_id"))
			setIfPresent(body, "due_date", argRaw(req, "due_date"))
			var out json.RawMessage
			if err := c.PatchJSON(ctx, "/api/issues/"+url.PathEscape(id), body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_status",
			mcp.WithDescription("Convenience wrapper for status-only updates."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithString("status", mcp.Required(), mcp.Enum(issueStatusEnum...)),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			status, errResult := requireString(req, "status")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PatchJSON(ctx, "/api/issues/"+url.PathEscape(id), map[string]any{"status": status}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_assign",
			mcp.WithDescription("Assign an issue to a member or agent. Pass empty assignee_id to unassign. Assigning to an agent dispatches a task immediately if the agent has a runtime attached."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithString("assignee_type", mcp.Enum(assigneeTypeEnum...)),
			mcp.WithString("assignee_id"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			at := argString(req, "assignee_type")
			ai := argString(req, "assignee_id")
			body := map[string]any{
				"assignee_type": nullableString(at),
				"assignee_id":   nullableString(ai),
			}
			var out json.RawMessage
			if err := c.PatchJSON(ctx, "/api/issues/"+url.PathEscape(id), body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_comment_add",
			mcp.WithDescription("Post a comment on an issue. Comments can @-mention agents to trigger them — see workspace agent list for valid IDs. Markdown is supported."),
			mcp.WithString("issue_id", mcp.Required()),
			mcp.WithString("content", mcp.Required()),
			mcp.WithString("parent_comment_id", mcp.Description("Reply to an existing comment instead of starting a new thread.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			issueID, errResult := requireString(req, "issue_id")
			if errResult != nil {
				return errResult, nil
			}
			content, errResult := requireString(req, "content")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{
				"content":   content,
				"parent_id": nullableString(argString(req, "parent_comment_id")),
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_comment_list",
			mcp.WithDescription("Return comments on an issue (oldest first, paginated). Includes author identity, content, and parent_id for threading."),
			mcp.WithString("issue_id", mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Default 50; cap 200.")),
			mcp.WithNumber("offset"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			issueID, errResult := requireString(req, "issue_id")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			offset, hasOff := argInt(req, "offset")
			path := "/api/issues/" + url.PathEscape(issueID) + "/comments" + queryString(
				[2]string{"limit", intStr(limit, hasLim)},
				[2]string{"offset", intStr(offset, hasOff)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_issue_runs",
			mcp.WithDescription("Return all task runs for an issue (status, dispatched_at, completed_at, error). Use to check whether an assigned agent is making progress."),
			mcp.WithString("issue_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			issueID, errResult := requireString(req, "issue_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/task-runs", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Agents — read-only. Mutating ops are deliberately out of scope.
// ---------------------------------------------------------------------------

func registerAgentTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_agent_list",
			mcp.WithDescription("Return agents in the workspace (id, name, description, runtime status, archived). Use to find the right agent before creating or assigning an issue."),
			mcp.WithBoolean("include_archived", mcp.Description("Include archived agents. Default false.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			incArc, hasInc := argBool(req, "include_archived")
			path := "/api/agents" + queryString([2]string{"include_archived", boolStr(incArc, hasInc)})
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_agent_get",
			mcp.WithDescription("Fetch details for one agent (name, instructions, skills, runtime). Use to verify an agent is reachable before assigning to it."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/agents/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_agent_tasks",
			mcp.WithDescription("Return the most recent tasks dispatched to an agent (status, issue, started_at, completed_at). Useful to check whether the agent is currently busy."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Default 20; cap 100.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			path := "/api/agents/" + url.PathEscape(id) + "/tasks" + queryString([2]string{"limit", intStr(limit, hasLim)})
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Channels — read + post; mark-read for clearing the unread badge.
// ---------------------------------------------------------------------------

func registerChannelTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_channel_list",
			mcp.WithDescription("Return channels and DMs the active actor belongs to, including per-channel unread counts. Use before posting to look up channel ids."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/channels", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_channel_get",
			mcp.WithDescription("Return one channel's metadata by id."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/channels/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_channel_history",
			mcp.WithDescription("Return messages in a channel, newest first, paginated by created_at. Use the oldest 'created_at' you've already seen as 'before' to fetch the next older page."),
			mcp.WithString("channel_id", mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Default 50; cap 200.")),
			mcp.WithString("before", mcp.Description("RFC3339 timestamp; only messages strictly older are returned.")),
			mcp.WithBoolean("include_threaded", mcp.Description("Include thread replies inline. Default false.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			channelID, errResult := requireString(req, "channel_id")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			incThread, hasInc := argBool(req, "include_threaded")
			path := "/api/channels/" + url.PathEscape(channelID) + "/messages" + queryString(
				[2]string{"limit", intStr(limit, hasLim)},
				[2]string{"before", argString(req, "before")},
				[2]string{"include_threaded", boolStr(incThread, hasInc)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_channel_post",
			mcp.WithDescription("Post a message to a channel. To @-mention an agent (which dispatches a task to it), include the canonical mention markup: '[@AgentName](mention://agent/<agent-uuid>)'. In a DM with an agent, every member message implicitly addresses the agent — no @mention required."),
			mcp.WithString("channel_id", mcp.Required()),
			mcp.WithString("content", mcp.Required()),
			mcp.WithString("parent_message_id", mcp.Description("Reply in a thread instead of the main timeline.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			channelID, errResult := requireString(req, "channel_id")
			if errResult != nil {
				return errResult, nil
			}
			content, errResult := requireString(req, "content")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{
				"content":           content,
				"parent_message_id": nullableString(argString(req, "parent_message_id")),
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/channels/"+url.PathEscape(channelID)+"/messages", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_channel_members",
			mcp.WithDescription("Return the channel's member list (members + agents). Use to verify an agent is in a channel before @-mentioning it (mentions of non-members render but don't dispatch tasks)."),
			mcp.WithString("channel_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			channelID, errResult := requireString(req, "channel_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/channels/"+url.PathEscape(channelID)+"/members", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_channel_mark_read",
			mcp.WithDescription("Update the read cursor for the active actor up to the given message id. Clears the unread badge."),
			mcp.WithString("channel_id", mcp.Required()),
			mcp.WithString("message_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			channelID, errResult := requireString(req, "channel_id")
			if errResult != nil {
				return errResult, nil
			}
			messageID, errResult := requireString(req, "message_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"message_id": messageID}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/channels/"+url.PathEscape(channelID)+"/read", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Projects — read + create; deletion / resource attachment intentionally
// not exposed (wider blast radius than chat-driven LLM should have).
// ---------------------------------------------------------------------------

var projectStatusEnum = []string{"backlog", "planned", "in_progress", "completed", "cancelled"}

func registerProjectTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_project_list",
			mcp.WithDescription("Return all projects in the active workspace (id, name, status, lead)."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/projects", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_project_get",
			mcp.WithDescription("Fetch one project's metadata + attached resources."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/projects/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_project_search",
			mcp.WithDescription("Fuzzy search projects by name. Useful when the user names a project loosely."),
			mcp.WithString("q", mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Default 10; cap 50.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			q, errResult := requireString(req, "q")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			path := "/api/projects/search" + queryString(
				[2]string{"q", q},
				[2]string{"limit", intStr(limit, hasLim)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_project_create",
			mcp.WithDescription("Create a new project. Title is required; description, status, target_date are optional."),
			mcp.WithString("title", mcp.Required()),
			mcp.WithString("description"),
			mcp.WithString("status", mcp.Enum(projectStatusEnum...)),
			mcp.WithString("target_date", mcp.Description("RFC3339 due date, e.g. '2026-12-31T00:00:00Z'.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			title, errResult := requireString(req, "title")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{
				"title":       title,
				"description": nullableString(argString(req, "description")),
				"status":      stringOrDefault(argString(req, "status"), "backlog"),
				"target_date": nullableString(argString(req, "target_date")),
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/projects", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Memory — wiki / agent_note / runbook / decision artifacts. Hard delete
// is intentionally not exposed; archive is the soft-delete path.
// ---------------------------------------------------------------------------

var memoryKindEnum = []string{"wiki_page", "agent_note", "runbook", "decision"}
var memoryAnchorTypeEnum = []string{"issue", "project", "agent", "channel"}

func registerMemoryTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_memory_list",
			mcp.WithDescription("List memory artifacts in the active workspace. Filter by kind and/or parent. Archived rows are hidden by default."),
			mcp.WithString("kind", mcp.Enum(memoryKindEnum...)),
			mcp.WithString("parent_id"),
			mcp.WithBoolean("include_archived"),
			mcp.WithNumber("limit", mcp.Description("Default 50; cap 200.")),
			mcp.WithNumber("offset"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			limit, hasLim := argInt(req, "limit")
			offset, hasOff := argInt(req, "offset")
			incArc, hasInc := argBool(req, "include_archived")
			path := "/api/memory" + queryString(
				[2]string{"kind", argString(req, "kind")},
				[2]string{"parent_id", argString(req, "parent_id")},
				[2]string{"include_archived", boolStr(incArc, hasInc)},
				[2]string{"limit", intStr(limit, hasLim)},
				[2]string{"offset", intStr(offset, hasOff)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_get",
			mcp.WithDescription("Fetch a single memory artifact by id."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/memory/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_by_anchor",
			mcp.WithDescription("Return all memory artifacts anchored to a specific issue / project / agent / channel. Useful for 'show me the runbooks for THIS issue' lookups."),
			mcp.WithString("anchor_type", mcp.Required(), mcp.Enum(memoryAnchorTypeEnum...)),
			mcp.WithString("anchor_id", mcp.Required()),
			mcp.WithNumber("limit"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			anchorType, errResult := requireString(req, "anchor_type")
			if errResult != nil {
				return errResult, nil
			}
			anchorID, errResult := requireString(req, "anchor_id")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			path := "/api/memory/by-anchor/" + url.PathEscape(anchorType) + "/" + url.PathEscape(anchorID) + queryString(
				[2]string{"limit", intStr(limit, hasLim)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_search",
			mcp.WithDescription("Full-text search over memory artifact titles, content, and tags. Uses Postgres tsvector + websearch_to_tsquery, so user-friendly syntax (quoted phrases, OR, leading -) is supported."),
			mcp.WithString("q", mcp.Required()),
			mcp.WithString("kind", mcp.Enum(memoryKindEnum...)),
			mcp.WithNumber("limit"),
			mcp.WithNumber("offset"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			q, errResult := requireString(req, "q")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			offset, hasOff := argInt(req, "offset")
			path := "/api/memory/search" + queryString(
				[2]string{"q", q},
				[2]string{"kind", argString(req, "kind")},
				[2]string{"limit", intStr(limit, hasLim)},
				[2]string{"offset", intStr(offset, hasOff)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_create",
			mcp.WithDescription("Create a new memory artifact. Use kind='agent_note' for findings/decisions/dead-ends produced during a task; 'runbook' for operational procedures; 'decision' for architectural records; 'wiki_page' for general knowledge. Anchor the artifact to an issue / project / agent / channel when it's about a specific thing — anchored artifacts are auto-injected into agent runtime context for that anchor."),
			mcp.WithString("kind", mcp.Required(), mcp.Enum(memoryKindEnum...)),
			mcp.WithString("title", mcp.Required()),
			mcp.WithString("content", mcp.Required()),
			mcp.WithString("slug", mcp.Description("Optional stable URL slug (lowercase, hyphenated).")),
			mcp.WithString("parent_id"),
			mcp.WithString("anchor_type", mcp.Enum(memoryAnchorTypeEnum...)),
			mcp.WithString("anchor_id"),
			mcp.WithArray("tags", mcp.Description("Free-form tags as a string array.")),
			mcp.WithBoolean("always_inject_at_runtime", mcp.Description("Workspace-wide auto-inject. Use sparingly.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			kind, errResult := requireString(req, "kind")
			if errResult != nil {
				return errResult, nil
			}
			title, errResult := requireString(req, "title")
			if errResult != nil {
				return errResult, nil
			}
			content := argString(req, "content")
			body := map[string]any{
				"kind":        kind,
				"title":       title,
				"content":     content,
				"slug":        nullableString(argString(req, "slug")),
				"parent_id":   nullableString(argString(req, "parent_id")),
				"anchor_type": nullableString(argString(req, "anchor_type")),
				"anchor_id":   nullableString(argString(req, "anchor_id")),
				"tags":        argStringSlice(req, "tags"),
				"metadata":    map[string]any{},
			}
			if v, ok := argBool(req, "always_inject_at_runtime"); ok {
				body["always_inject_at_runtime"] = v
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/memory", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_update",
			mcp.WithDescription("Partial update — only fields you pass are changed. Set anchor_type / anchor_id together to retarget; pass tags to replace the whole array. Cannot change kind (kind is set at creation time)."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithString("title"),
			mcp.WithString("content"),
			mcp.WithString("slug"),
			mcp.WithString("parent_id"),
			mcp.WithString("anchor_type", mcp.Enum(memoryAnchorTypeEnum...)),
			mcp.WithString("anchor_id"),
			mcp.WithArray("tags"),
			mcp.WithBoolean("always_inject_at_runtime"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{}
			setIfPresent(body, "title", argRaw(req, "title"))
			setIfPresent(body, "content", argRaw(req, "content"))
			setIfPresent(body, "slug", argRaw(req, "slug"))
			setIfPresent(body, "parent_id", argRaw(req, "parent_id"))
			setIfPresent(body, "anchor_type", argRaw(req, "anchor_type"))
			setIfPresent(body, "anchor_id", argRaw(req, "anchor_id"))
			if tags := argStringSlice(req, "tags"); tags != nil {
				body["tags"] = tags
			}
			if v, ok := argBool(req, "always_inject_at_runtime"); ok {
				body["always_inject_at_runtime"] = v
			}
			var out json.RawMessage
			if err := c.PutJSON(ctx, "/api/memory/"+url.PathEscape(id), body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_archive",
			mcp.WithDescription("Soft-delete an artifact. It stops appearing in default lists and runtime injection but stays queryable via include_archived=true. Reversible via multica_memory_restore."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/memory/"+url.PathEscape(id)+"/archive", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_memory_restore",
			mcp.WithDescription("Undo a multica_memory_archive — clears archived_at / archived_by."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/memory/"+url.PathEscape(id)+"/restore", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Labels — list + attach/detach. Create/update is left to the UI, where
// the form-driven taxonomy decision is more explicit than chat-driven.
// ---------------------------------------------------------------------------

func registerLabelTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_label_list",
			mcp.WithDescription("Return all labels in the active workspace (id, name, color)."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/labels", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_label_attach",
			mcp.WithDescription("Attach an existing label (by id) to an issue. No-op if already attached."),
			mcp.WithString("issue_id", mcp.Required()),
			mcp.WithString("label_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			issueID, errResult := requireString(req, "issue_id")
			if errResult != nil {
				return errResult, nil
			}
			labelID, errResult := requireString(req, "label_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"label_id": labelID}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/labels", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_label_detach",
			mcp.WithDescription("Remove a label from an issue. No-op if it wasn't attached."),
			mcp.WithString("issue_id", mcp.Required()),
			mcp.WithString("label_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			issueID, errResult := requireString(req, "issue_id")
			if errResult != nil {
				return errResult, nil
			}
			labelID, errResult := requireString(req, "label_id")
			if errResult != nil {
				return errResult, nil
			}
			if err := c.DeleteJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/labels/"+url.PathEscape(labelID)); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Autopilots — read + trigger. Create / update / delete left to the UI.
// ---------------------------------------------------------------------------

func registerAutopilotTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_autopilot_list",
			mcp.WithDescription("Return autopilots in the workspace (id, title, status, agent, source). Use to find an autopilot before triggering or inspecting runs."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/autopilots", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_autopilot_get",
			mcp.WithDescription("Full autopilot details including triggers and instructions."),
			mcp.WithString("id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/autopilots/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_autopilot_runs",
			mcp.WithDescription("Recent execution history for an autopilot (status, started, completed, error)."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Default 20; cap 100.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			path := "/api/autopilots/" + url.PathEscape(id) + "/runs" + queryString([2]string{"limit", intStr(limit, hasLim)})
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_autopilot_trigger",
			mcp.WithDescription("Manually start an autopilot run. Optional payload is forwarded to the agent as the run's trigger context."),
			mcp.WithString("id", mcp.Required()),
			mcp.WithObject("payload", mcp.Description("Free-form JSON payload available to the agent during the run.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"payload": argRaw(req, "payload")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/autopilots/"+url.PathEscape(id)+"/trigger", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Common body helpers
// ---------------------------------------------------------------------------

// nullableString returns nil for an empty string so the JSON body has
// `"key": null` instead of `"key": ""` — most server handlers treat the
// two as the same, but a few (e.g. memory artifact slug) distinguish.
// Matches the JS @multica/mcp behavior of `value ?? null`.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func stringOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// setIfPresent only writes to the body map when the value is actually
// present in the MCP arguments (not just zero-valued). This is the patch-
// semantics equivalent of `if (input.field !== undefined) body.field =
// input.field` from the JS tools — it's the difference between
// "explicitly clear this field" and "leave this field alone."
func setIfPresent(body map[string]any, key string, v any) {
	if v == nil {
		return
	}
	// Special-case empty strings: the JS tools sent the value through
	// even when empty, but most callers prefer empty-string-means-omit.
	// If a tool genuinely needs to clear a field, it can pass null
	// (which arrives as nil here) — but since mcp.WithString doesn't
	// surface explicit null, this branch covers the common case.
	if s, ok := v.(string); ok && s == "" {
		return
	}
	body[key] = v
}

// ---------------------------------------------------------------------------
// Workspace — the active-workspace getter + members list. The MCP server's
// auth carries one WorkspaceID, so these don't take an id arg.
// ---------------------------------------------------------------------------

func registerWorkspaceTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_workspace_get",
			mcp.WithDescription("Get the active workspace's metadata: id, name, slug, settings, member count."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			if c.WorkspaceID == "" {
				return nil, fmt.Errorf("no active workspace — set MULTICA_WORKSPACE_ID or run `multica config set workspace_id <id>`")
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/workspaces/"+c.WorkspaceID, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_workspace_members",
			mcp.WithDescription("List members of the active workspace (id, name, role)."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			if c.WorkspaceID == "" {
				return nil, fmt.Errorf("no active workspace — set MULTICA_WORKSPACE_ID or run `multica config set workspace_id <id>`")
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/workspaces/"+c.WorkspaceID+"/members", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Ship Hub — release coordination, PR triage, deploy logging. The surface
// mirrors the dashboard the user already drives in the browser, so an LLM
// running in Claude Code can answer "what should I merge?" and "what's
// blocking this release?" without copy-pasting into the chat.
//
// Read tools are safe for the model to call freely. Write tools mutate
// real GitHub state (merges, comments) or kick off CI workflows; they're
// gated behind the workspace token the user has already trusted to
// `multica mcp-stdio`. The PR-action tools intentionally expose only the
// subset that's actually useful for AI-assisted shipping — destructive
// chip endpoints like dismiss_review or run_smoke_tests stay UI-only.
// ---------------------------------------------------------------------------

var prStateEnum = []string{"open", "closed", "merged", "all"}
var releaseStatusEnum = []string{"active", "all"}
var mergeMethodEnum = []string{"merge", "squash", "rebase"}
var deployStatusEnum = []string{"succeeded", "failed", "in_progress", "pending", "rolled_back"}
var reviewEventEnum = []string{"APPROVE", "REQUEST_CHANGES", "COMMENT"}

func registerShipHubTools(srv *server.MCPServer, c *cli.APIClient) {
	registerShipHubReadTools(srv, c)
	registerShipHubReleaseWriteTools(srv, c)
	registerShipHubDeployAndReviewTools(srv, c)
	registerShipHubPRActionTools(srv, c)
}

// ----- read tools -----------------------------------------------------------

func registerShipHubReadTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_ship_list_projects",
			mcp.WithDescription("List Ship Hub projects in the active workspace — projects that have an attached github_repo resource. Returns id, title, icon, open_pr_count, env_count for each. Use this first to find the project_id you need for downstream calls."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/ship/projects", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_list_pull_requests",
			mcp.WithDescription("List cached pull requests for a project, decorated with active_release when the PR is in a release. Default state is 'open'. The cache is refreshed by multica_ship_sync_project; use that first if you suspect the data is stale."),
			mcp.WithString("project_id", mcp.Required(), mcp.Description("Ship Hub project UUID — find via multica_ship_list_projects.")),
			mcp.WithString("state", mcp.Description("Filter by PR state. Default 'open'."), mcp.Enum(prStateEnum...)),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			projectID, errResult := requireString(req, "project_id")
			if errResult != nil {
				return errResult, nil
			}
			path := "/api/projects/" + url.PathEscape(projectID) + "/pull_requests" + queryString(
				[2]string{"state", argString(req, "state")},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_get_pull_request",
			mcp.WithDescription("Fetch the full PR detail bundle: PR row + linked Multica issue + originating agent task + active release + reviews + checks + recent_actions + stack relations. This is the one-call answer to 'tell me everything about PR X' — prefer it over chaining list calls."),
			mcp.WithString("pull_request_id", mcp.Required(), mcp.Description("Pull request UUID (Multica id, not the GitHub PR number).")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/details", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_list_releases",
			mcp.WithDescription("List releases for a project. Default scope is 'active' (anything not yet done/cancelled). Pass status='all' to include historical releases — useful for postmortems."),
			mcp.WithString("project_id", mcp.Required()),
			mcp.WithString("status", mcp.Description("Default 'active'."), mcp.Enum(releaseStatusEnum...)),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			projectID, errResult := requireString(req, "project_id")
			if errResult != nil {
				return errResult, nil
			}
			path := "/api/projects/" + url.PathEscape(projectID) + "/releases" + queryString(
				[2]string{"status", argString(req, "status")},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_list_active_releases",
			mcp.WithDescription("List every active (in-flight) release across all projects in the active workspace — the 'release radar'. Use this to answer 'what is currently shipping?' without iterating per-project."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			if c.WorkspaceID == "" {
				return nil, fmt.Errorf("no active workspace — set MULTICA_WORKSPACE_ID or run `multica config set workspace_id <id>`")
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/workspaces/"+c.WorkspaceID+"/releases/active", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_get_release",
			mcp.WithDescription("Fetch a single release by id: stage, signoffs, attached PRs, event timeline, linked deploys. Use after multica_ship_list_active_releases when the user asks 'what's the status of release X?'"),
			mcp.WithString("release_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/releases/"+url.PathEscape(id), &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_get_release_health",
			mcp.WithDescription("Return the post-promote health snapshot for a release: production deploy status, error rate, smoke test outcomes. Use to answer 'is the release healthy?' once it's in production."),
			mcp.WithString("release_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/releases/"+url.PathEscape(id)+"/health", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_get_release_merge_state",
			mcp.WithDescription("Return the merge-train state for a release: per-PR merge_state (queued / merging / merged / failed / skipped), the train's current status, and any blocking conditions. Poll this after multica_ship_start_merge_train to watch progress."),
			mcp.WithString("release_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			id, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/releases/"+url.PathEscape(id)+"/merge_state", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_list_deploy_environments",
			mcp.WithDescription("List the configured deploy environments (staging/production) for a project, with current_sha and current_deployed_at so you can answer 'what's running on production?' in one call."),
			mcp.WithString("project_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			projectID, errResult := requireString(req, "project_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectID)+"/deploy_environments", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_list_deploys",
			mcp.WithDescription("Return the most recent deploys for a deploy environment, newest first. Default limit 20, max 200. Use to find a recent failed deploy before recommending a rollback."),
			mcp.WithString("environment_id", mcp.Required()),
			mcp.WithNumber("limit", mcp.Description("Default 20; cap 200.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			envID, errResult := requireString(req, "environment_id")
			if errResult != nil {
				return errResult, nil
			}
			limit, hasLim := argInt(req, "limit")
			path := "/api/deploy_environments/" + url.PathEscape(envID) + "/deploys" + queryString(
				[2]string{"limit", intStr(limit, hasLim)},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_workspace_summary",
			mcp.WithDescription("Aggregate Ship Hub metrics for the active workspace: open PR count, active release count, environments-out-of-date count. Use this once at the start of a session to orient before drilling into a specific project."),
		),
		toolHandler(func(ctx context.Context, _ mcp.CallToolRequest) (any, error) {
			var out json.RawMessage
			if err := c.GetJSON(ctx, "/api/ship_hub/summary", &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_get_snapshot",
			mcp.WithDescription("Time-machine snapshot of a project's ship state at a specific instant — the closest the data model has to 'replay this release'. Useful for postmortems: what was on production when X failed?"),
			mcp.WithString("project_id", mcp.Required()),
			mcp.WithString("at", mcp.Required(), mcp.Description("RFC3339 timestamp, e.g. 2026-01-15T14:30:00Z.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			projectID, errResult := requireString(req, "project_id")
			if errResult != nil {
				return errResult, nil
			}
			at, errResult := requireString(req, "at")
			if errResult != nil {
				return errResult, nil
			}
			path := "/api/projects/" + url.PathEscape(projectID) + "/ship_snapshot" + queryString(
				[2]string{"at", at},
			)
			var out json.RawMessage
			if err := c.GetJSON(ctx, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ----- release write tools --------------------------------------------------

func registerShipHubReleaseWriteTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_ship_sync_project",
			mcp.WithDescription("Refresh the cached PR list for a project from GitHub. Returns the SyncResult diff (created/updated/closed counts). Run this before listing PRs if the workspace was idle for a while or if a webhook may have been missed."),
			mcp.WithString("project_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			projectID, errResult := requireString(req, "project_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/projects/"+url.PathEscape(projectID)+"/pull_requests/sync", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_create_release",
			mcp.WithDescription("Create a release: bundle multiple pull requests for coordinated rollout through staging → production. Use AFTER multica_ship_list_pull_requests has surfaced eligible PRs and you've reasoned about which ones should ship together. Returns the new release id + auto-created channel + tracking issue. The release lands in 'assembling' stage; call multica_ship_start_merge_train to begin merging."),
			mcp.WithString("project_id", mcp.Required()),
			mcp.WithString("title", mcp.Required(), mcp.Description("Human-readable release name, shown in the release radar.")),
			mcp.WithString("description"),
			mcp.WithArray("pull_request_ids", mcp.Required(), mcp.Description("Non-empty array of PR UUIDs to attach. Order is preserved as the merge order.")),
			mcp.WithString("approver_id", mcp.Description("Optional member id who must sign off before promote.")),
			mcp.WithString("second_approver_id", mcp.Description("Optional second sign-off (two-person rule).")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			projectID, errResult := requireString(req, "project_id")
			if errResult != nil {
				return errResult, nil
			}
			title, errResult := requireString(req, "title")
			if errResult != nil {
				return errResult, nil
			}
			prIDs := argStringSlice(req, "pull_request_ids")
			if len(prIDs) == 0 {
				return mcp.NewToolResultError("pull_request_ids must be a non-empty array of PR UUIDs"), nil
			}
			body := map[string]any{
				"title":            title,
				"description":      argString(req, "description"),
				"pull_request_ids": prIDs,
			}
			if v := argString(req, "approver_id"); v != "" {
				body["approver_id"] = v
			}
			if v := argString(req, "second_approver_id"); v != "" {
				body["second_approver_id"] = v
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/projects/"+url.PathEscape(projectID)+"/releases", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_add_pr_to_release",
			mcp.WithDescription("Attach a pull request to an in-flight release. Only allowed while the release is in 'assembling' stage; once the merge train starts, the membership is frozen."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("pull_request_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"pull_request_id": prID}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/pull_requests", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_remove_pr_from_release",
			mcp.WithDescription("Detach a pull request from a release. Like add, only valid in 'assembling' stage."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("pull_request_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			path := "/api/releases/" + url.PathEscape(releaseID) + "/pull_requests/" + url.PathEscape(prID)
			if err := c.DeleteJSON(ctx, path); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_cancel_release",
			mcp.WithDescription("Cancel a release that hasn't started merging. Posts the reason to the release channel and closes the tracking issue. Once the merge train has begun, use multica_ship_abort_merge_train instead."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("reason"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"reason": argString(req, "reason")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/cancel", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_start_merge_train",
			mcp.WithDescription("Kick off the merge train for an assembled release. Each attached PR is merged in order; failures pause the train. Returns 202 Accepted; poll multica_ship_get_release_merge_state for progress."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("merge_method", mcp.Description("Default 'merge'."), mcp.Enum(mergeMethodEnum...)),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{}
			if v := argString(req, "merge_method"); v != "" {
				body["merge_method"] = v
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/start_merge", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_resume_merge_train",
			mcp.WithDescription("Resume a paused merge train (e.g. after fixing a failed PR or deciding to skip it). skip_pr_ids lists PR membership rows to mark 'skipped' before resuming — the train moves on without merging them."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithArray("skip_pr_ids", mcp.Description("Optional array of PR UUIDs to drop from the train.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"skip_pr_ids": argStringSlice(req, "skip_pr_ids")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/resume_merge", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_abort_merge_train",
			mcp.WithDescription("Abort an in-flight merge train. Already-merged PRs stay merged; remaining ones are abandoned. Reason is posted to the release channel for the audit trail."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("reason"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"reason": argString(req, "reason")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/abort_merge", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_mark_release_verified",
			mcp.WithDescription("Manually mark a release as verified on staging — the human-in-the-loop gate before promote. Note is recorded on the release event timeline."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("note"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"note": argString(req, "note")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/mark_verified", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_unverify_release",
			mcp.WithDescription("Reverse a verified mark — used when a problem is found between verify and promote. Reason is required for the audit row."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("reason", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			reason, errResult := requireString(req, "reason")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"reason": reason}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/unverify", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_promote_release",
			mcp.WithDescription("Promote a verified release to production. The release flips to 'promoting' and the user's CI/CD takes over; multica_ship_mark_production_deployed is the manual escape hatch when no deployment_status webhook fires. rollback_plan is captured at click time for the audit trail."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("rollback_plan", mcp.Description("Free-text rollback procedure recorded with the promote action.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"rollback_plan": argString(req, "rollback_plan")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/promote", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_mark_production_deployed",
			mcp.WithDescription("Manual escape hatch to advance a 'promoting' release into 'in_production'. Use only when the deployment_status webhook didn't fire (e.g. the user's CI provider doesn't support GitHub deployments)."),
			mcp.WithString("release_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/mark_production_deployed", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_rollback_release",
			mcp.WithDescription("Mark a release as rolled back. Use after an incident — reason is REQUIRED and is echoed in the release channel + audit log. Does not itself revert any code; that's a separate manual operation."),
			mcp.WithString("release_id", mcp.Required()),
			mcp.WithString("reason", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			reason, errResult := requireString(req, "reason")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"reason": reason}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/rollback", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_mark_release_done",
			mcp.WithDescription("Mark a healthy release as 'done' — closes the tracking issue and removes it from the active radar. Call this once production has soaked and you're satisfied."),
			mcp.WithString("release_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			releaseID, errResult := requireString(req, "release_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/releases/"+url.PathEscape(releaseID)+"/mark_done", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ----- deploy + review tools ------------------------------------------------

func registerShipHubDeployAndReviewTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_ship_log_deploy",
			mcp.WithDescription("Manually record a deploy event on an environment — use when the deploy happened outside Multica (Vercel CLI, GitHub Actions, k8s rollout) and the webhook didn't fire. A 'succeeded' status updates the environment's current_sha and triggers release-staging linkage when applicable."),
			mcp.WithString("environment_id", mcp.Required()),
			mcp.WithString("sha", mcp.Required(), mcp.Description("The git commit SHA that was deployed.")),
			mcp.WithString("status", mcp.Required(), mcp.Enum(deployStatusEnum...)),
			mcp.WithString("ref", mcp.Description("Optional git ref (branch/tag); defaults to the environment's target_branch.")),
			mcp.WithString("log_url", mcp.Description("Optional URL pointing at the build/deploy log.")),
			mcp.WithString("error_message", mcp.Description("Optional failure message; only meaningful when status is 'failed'.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			envID, errResult := requireString(req, "environment_id")
			if errResult != nil {
				return errResult, nil
			}
			sha, errResult := requireString(req, "sha")
			if errResult != nil {
				return errResult, nil
			}
			status, errResult := requireString(req, "status")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{
				"sha":    sha,
				"status": status,
			}
			if v := argString(req, "ref"); v != "" {
				body["ref"] = v
			}
			if v := argString(req, "log_url"); v != "" {
				body["log_url"] = v
			}
			if v := argString(req, "error_message"); v != "" {
				body["error_message"] = v
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/deploy_environments/"+url.PathEscape(envID)+"/deploys", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_submit_pr_review",
			mcp.WithDescription("Submit a GitHub PR review (APPROVE / REQUEST_CHANGES / COMMENT) without leaving Multica. Body is optional for APPROVE but recommended for REQUEST_CHANGES. The review attribution is the workspace user behind the MCP token, not an agent."),
			mcp.WithString("pull_request_id", mcp.Required()),
			mcp.WithString("event", mcp.Required(), mcp.Enum(reviewEventEnum...)),
			mcp.WithString("body", mcp.Description("Review comment text, markdown supported.")),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			event, errResult := requireString(req, "event")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{
				"event": event,
				"body":  argString(req, "body"),
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/review", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// ----- PR action tools ------------------------------------------------------

func registerShipHubPRActionTools(srv *server.MCPServer, c *cli.APIClient) {
	srv.AddTool(
		mcp.NewTool(
			"multica_ship_merge_pr",
			mcp.WithDescription("Merge a pull request via GitHub's merge API. Destructive — requires owner/admin role. Default method is 'merge' (creates a merge commit); use 'squash' to flatten the branch history or 'rebase' for a linear history."),
			mcp.WithString("pull_request_id", mcp.Required()),
			mcp.WithString("method", mcp.Description("Default 'merge'."), mcp.Enum(mergeMethodEnum...)),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{}
			if v := argString(req, "method"); v != "" {
				body["method"] = v
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/merge", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_comment_on_pr",
			mcp.WithDescription("Post an issue-style comment on a GitHub PR. Markdown is supported. Use to nudge authors, surface review feedback, or echo a Multica decision."),
			mcp.WithString("pull_request_id", mcp.Required()),
			mcp.WithString("body", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			body, errResult := requireString(req, "body")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/comment", map[string]any{"body": body}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_close_pr_as_stale",
			mcp.WithDescription("Close a stale or abandoned PR with a closing comment. Destructive — requires owner/admin role. Reason is posted as a comment before close so the author has the audit trail."),
			mcp.WithString("pull_request_id", mcp.Required()),
			mcp.WithString("reason"),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			body := map[string]any{"reason": argString(req, "reason")}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/close_as_stale", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_diagnose_ci_failure",
			mcp.WithDescription("Dispatch a diagnostic agent task against a PR with red CI. The agent reads the failing logs and posts an analysis back to the PR's conversation channel. Returns the agent_task_id so you can poll progress via multica_agent_tasks."),
			mcp.WithString("pull_request_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/diagnose_ci_failure", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)

	srv.AddTool(
		mcp.NewTool(
			"multica_ship_summarize_review_feedback",
			mcp.WithDescription("Dispatch an agent task to read all PR review comments and produce a consolidated summary in the PR's conversation channel. Use when a PR has accumulated many review threads and the author needs an actionable digest."),
			mcp.WithString("pull_request_id", mcp.Required()),
		),
		toolHandler(func(ctx context.Context, req mcp.CallToolRequest) (any, error) {
			prID, errResult := requireString(req, "pull_request_id")
			if errResult != nil {
				return errResult, nil
			}
			var out json.RawMessage
			if err := c.PostJSON(ctx, "/api/pull_requests/"+url.PathEscape(prID)+"/summarize_review_feedback", map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		}),
	)
}

// Silence "imported and not used" if a future trim of the toolset drops
// the only user of strings. Keep present so adding a tool that uses
// string-formatting helpers doesn't require re-importing.
var _ = strings.TrimSpace
