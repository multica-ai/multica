package main

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerArtifactTools wires MCP tools for the artifact feature: typed
// first-class outputs (reports, plans, decisions, diagrams, notes) scoped to
// a project or an issue.
//
// Lives in its own file so the feature stays additive — no edits to
// cmd_mcp_tools.go beyond a single registerArtifactTools(srv, client) call
// inside registerTools().
func registerArtifactTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// create_artifact
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "create_artifact",
		Description: `Create a typed artifact — a durable, addressable, renderable document distinct from comments and file attachments. Use when producing a structured deliverable that humans and other agents will reference: a report, a plan, an architecture decision, a diagram, or a longer-form note.

KIND determines how the artifact renders and where it surfaces in the UI:
- "report"   — investigation results, analysis, summaries
- "plan"     — proposed approach, task breakdown, sequencing
- "decision" — ADR-style: context, decision, consequences
- "diagram"  — body should contain a Mermaid diagram fenced as ` + "`" + `` + "`" + `` + "`" + `mermaid
- "note"     — generic catch-all

SCOPE is exactly one of:
- issue_id   — artifact lives with the issue and shows on the issue page
- project_id — artifact is project-level (architecture docs, project plans)

If the artifact pertains to specific work being done, scope it to the issue.
If it's project-wide reference material, scope it to the project.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"kind", "title", "body"},
			"properties": map[string]any{
				"kind":       map[string]any{"type": "string", "enum": []string{"report", "plan", "decision", "diagram", "note"}, "description": "Artifact type"},
				"title":      map[string]any{"type": "string", "description": "Short, descriptive title"},
				"body":       map[string]any{"type": "string", "description": "Markdown body. For 'diagram' kind, include a ```mermaid fenced block."},
				"issue_id":   map[string]any{"type": "string", "description": "Scope to this issue (provide either issue_id or project_id, not both)"},
				"project_id": map[string]any{"type": "string", "description": "Scope to this project (provide either issue_id or project_id, not both)"},
				"metadata":   map[string]any{"type": "object", "description": "Optional structured metadata", "additionalProperties": true},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		kind, err := requireString(args, "kind")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		title, err := requireString(args, "title")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body, err := requireString(args, "body")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		issueID := optString(args, "issue_id")
		projectID := optString(args, "project_id")
		if issueID == "" && projectID == "" {
			return mcp.ErrorResult("provide either issue_id or project_id"), nil
		}
		if issueID != "" && projectID != "" {
			return mcp.ErrorResult("provide either issue_id or project_id, not both"), nil
		}

		req := map[string]any{
			"kind":  kind,
			"title": title,
			"body":  body,
		}
		if issueID != "" {
			req["issue_id"] = issueID
		}
		if projectID != "" {
			req["project_id"] = projectID
		}
		if md, ok := args["metadata"].(map[string]any); ok && md != nil {
			req["metadata"] = md
		}

		var result map[string]any
		if err := client.PostJSON(ctx, "/api/artifacts", req, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// update_artifact
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "update_artifact",
		Description: "Update an existing artifact's title, body, or metadata. Only the original author or a workspace admin can edit. Kind and scope are immutable — to change them, delete and recreate.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Artifact ID"},
				"title":    map[string]any{"type": "string", "description": "New title (omit to keep current)"},
				"body":     map[string]any{"type": "string", "description": "New body (omit to keep current)"},
				"metadata": map[string]any{"type": "object", "description": "New metadata (omit to keep current)", "additionalProperties": true},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		req := map[string]any{}
		if v, ok := args["title"].(string); ok {
			req["title"] = v
		}
		if v, ok := args["body"].(string); ok {
			req["body"] = v
		}
		if md, ok := args["metadata"].(map[string]any); ok && md != nil {
			req["metadata"] = md
		}
		if len(req) == 0 {
			return mcp.ErrorResult("nothing to update; provide at least one of title, body, metadata"), nil
		}
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/artifacts/"+id, req, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// list_artifacts
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "list_artifacts",
		Description: "List artifacts scoped to an issue or a project. Returns id, kind, title, author, timestamps. Body is included so a single call surfaces full content for short artifacts.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"issue_id":   map[string]any{"type": "string", "description": "List artifacts on this issue"},
				"project_id": map[string]any{"type": "string", "description": "List artifacts on this project"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID := optString(args, "issue_id")
		projectID := optString(args, "project_id")
		if issueID == "" && projectID == "" {
			return mcp.ErrorResult("provide either issue_id or project_id"), nil
		}
		if issueID != "" && projectID != "" {
			return mcp.ErrorResult("provide either issue_id or project_id, not both"), nil
		}

		var path string
		if issueID != "" {
			path = fmt.Sprintf("/api/issues/%s/artifacts", issueID)
		} else {
			path = fmt.Sprintf("/api/projects/%s/artifacts", projectID)
		}

		var result []map[string]any
		if err := client.GetJSON(ctx, path, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// get_artifact
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "get_artifact",
		Description: "Fetch a single artifact by ID. Use after list_artifacts when you need the full body of a long artifact.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Artifact ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result map[string]any
		if err := client.GetJSON(ctx, "/api/artifacts/"+id, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// delete_artifact
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "delete_artifact",
		Description: "Delete an artifact. Only the original author or a workspace admin can delete.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Artifact ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, "/api/artifacts/"+id); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return mcp.TextResult(fmt.Sprintf("Deleted artifact %s", id)), nil
	})
}
