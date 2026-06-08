// CEREBRO-PATCH(cerebro-wakeup-mcp-tools): agent wakeup MCP tools.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerWakeupTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "schedule_wakeup",
		Description: "Schedule this or another agent to be woken on an issue at a time, when an issue reaches a status, or when linked GitHub CI updates.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id", "prompt", "trigger_type"},
			"properties": map[string]any{
				"agent_id":       map[string]any{"type": "string", "description": "Agent UUID. Defaults to this running agent when available."},
				"issue_id":       map[string]any{"type": "string", "description": "Issue UUID the agent should run on."},
				"prompt":         map[string]any{"type": "string", "description": "Prompt inserted into the wakeup comment."},
				"trigger_type":   map[string]any{"type": "string", "enum": []string{"time", "issue_status", "github_ci"}},
				"fire_at":        map[string]any{"type": "string", "description": "RFC3339 timestamp for trigger_type=time."},
				"watch_issue_id": map[string]any{"type": "string", "description": "Issue UUID watched by issue_status/github_ci."},
				"watch_status":   map[string]any{"type": "string", "description": "Status for trigger_type=issue_status."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		prompt, err := requireString(args, "prompt")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		triggerType, err := requireString(args, "trigger_type")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		agentID := strings.TrimSpace(optString(args, "agent_id"))
		if agentID == "" {
			agentID = os.Getenv("MULTICA_AGENT_ID")
		}
		body := map[string]any{
			"agent_id":     agentID,
			"issue_id":     issueID,
			"prompt":       prompt,
			"trigger_type": triggerType,
		}
		if v := optString(args, "fire_at"); v != "" {
			body["fire_at"] = v
		}
		if v := optString(args, "watch_issue_id"); v != "" {
			body["watch_issue_id"] = v
		}
		if v := optString(args, "watch_status"); v != "" {
			body["watch_status"] = v
		}
		var result any
		if err := client.PostJSON(ctx, "/api/cerebro/wakeups", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		// CEREBRO-PATCH(wakeup-comment-reminder): instruct agent to post a comment confirming the scheduled wakeup time.
		data, _ := json.MarshalIndent(result, "", "  ")
		fireTime := optString(args, "fire_at")
		instruction := "\n\nIMPORTANT: You MUST now post a comment on the issue confirming the wakeup was scheduled."
		if fireTime != "" {
			instruction = fmt.Sprintf("\n\nIMPORTANT: You MUST now post a comment on the issue stating that a wakeup has been scheduled for %s.", fireTime)
		}
		return mcp.TextResult(string(data) + instruction), nil
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "list_wakeups",
		Description: "List scheduled agent wakeups in the current workspace.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string"},
				"state":    map[string]any{"type": "string"},
				"limit":    map[string]any{"type": "integer"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		q := url.Values{}
		if v := optString(args, "agent_id"); v != "" {
			q.Set("agent_id", v)
		}
		if v := optString(args, "state"); v != "" {
			q.Set("state", v)
		}
		q.Set("limit", "50")
		if v := optInt(args, "limit", 50); v > 0 {
			q.Set("limit", strconv.Itoa(v))
		}
		var result any
		if err := client.GetJSON(ctx, "/api/cerebro/wakeups?"+q.Encode(), &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "cancel_wakeup",
		Description: "Cancel a pending agent wakeup.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.PostJSON(ctx, "/api/cerebro/wakeups/"+url.PathEscape(id)+"/cancel", map[string]any{}, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})
}
