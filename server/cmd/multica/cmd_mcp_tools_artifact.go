package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools-artifact): cerebro modification of upstream file

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

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
		Description: `Create a typed artifact — a durable, addressable, renderable document distinct from comments and file attachments.

WHEN TO USE THIS (vs. replying in chat or as a comment):
When the user asks you to *produce something* — a report, a plan, a decision log, a diagram, a document, a summary, a list of recommendations — you MUST call create_artifact. Do not paste long markdown back in chat or as a comment. That hides the work in conversation history where it can't be searched, linked, or referred back to.

Rule of thumb: if the output has its own title and is the answer to a "make me X" request, it is an artifact.

Comments are conversation: replies to questions, status updates, observations, "I'm working on it", short factual answers, clarifying questions back to the user.

Artifacts are deliverables: things with a title and a body the user wants to keep, share, or refer back to. They live in /documents and can be filed in folders, linked to issues/projects, and found again.

When in doubt, prefer create_artifact and tell the user where you saved it. If the user explicitly said "just answer in chat" or "summarise briefly", reply normally — but anything substantial they asked you to PRODUCE should be a document.

KIND determines how the artifact renders and where it surfaces in the UI:
- "report"   — investigation results, analysis, summaries
- "plan"     — proposed approach, task breakdown, sequencing
- "decision" — ADR-style: context, decision, consequences
- "diagram"  — body should contain a Mermaid diagram fenced as ` + "`" + `` + "`" + `` + "`" + `mermaid
- "note"     — generic catch-all

FORMAT determines how the body is rendered:
- "md"   — markdown (default; the body field is the source)
- "html" — raw HTML (the body field is the source)
- "pdf"  — pre-uploaded PDF; provide file_url and file_size_bytes; body is ignored

SCOPE is at most one of issue_id, project_id, or neither (workspace scope):
- issue_id   — artifact lives with the issue and shows on the issue page
- project_id — artifact is project-level (architecture docs, project plans)
- (omit both) — workspace-level reference material

FOLDERS group artifacts independently of scope. folder_id places the new
document in a specific folder; omit it to land at the root.

ORIGIN_ISSUE_ID preserves the "where did this come from" trail when the
artifact is scope=workspace or scope=project but was produced while
working on a specific issue. Always set origin_issue_id when an agent
creates a workspace/project artifact during issue work.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"kind", "title"},
			"properties": map[string]any{
				"kind":              map[string]any{"type": "string", "enum": []string{"report", "plan", "decision", "diagram", "note"}, "description": "Artifact type"},
				"format":            map[string]any{"type": "string", "enum": []string{"md", "html", "pdf"}, "description": "Storage format (default 'md'). PDF requires file_url."},
				"title":             map[string]any{"type": "string", "description": "Short, descriptive title"},
				"body":              map[string]any{"type": "string", "description": "Markdown or HTML source (per format). For 'diagram' kind, include a ```mermaid fenced block. Ignored for 'pdf' format."},
				"file_url":          map[string]any{"type": "string", "description": "Required for format='pdf'. URL of an already-uploaded file."},
				"file_size_bytes":   map[string]any{"type": "integer", "description": "Optional. Size in bytes of the file at file_url."},
				"issue_id":          map[string]any{"type": "string", "description": "Scope to this issue (mutually exclusive with project_id)"},
				"project_id":        map[string]any{"type": "string", "description": "Scope to this project (mutually exclusive with issue_id)"},
				"folder_id":         map[string]any{"type": "string", "description": "Place the artifact inside this folder. Omit for root."},
				"origin_issue_id":   map[string]any{"type": "string", "description": "Issue this artifact was made in the context of (preserves the trail when scope is workspace/project)."},
				"requester_user_id": map[string]any{"type": "string", "description": "Optional. The user who asked for this document. When omitted on agent-authored artifacts, the server fills in the calling user."},
				"metadata":          map[string]any{"type": "object", "description": "Optional structured metadata", "additionalProperties": true},
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
		body := optString(args, "body")
		format := optString(args, "format")
		if format == "" {
			format = "md"
		}
		fileURL := optString(args, "file_url")
		if format == "pdf" && fileURL == "" {
			return mcp.ErrorResult("format='pdf' requires file_url"), nil
		}
		if format != "pdf" && body == "" {
			return mcp.ErrorResult("body is required for format='md' and format='html'"), nil
		}
		issueID := optString(args, "issue_id")
		projectID := optString(args, "project_id")
		if issueID != "" && projectID != "" {
			return mcp.ErrorResult("provide at most one of issue_id or project_id"), nil
		}

		req := map[string]any{
			"kind":   kind,
			"format": format,
			"title":  title,
			"body":   body,
		}
		if issueID != "" {
			req["issue_id"] = issueID
		}
		if projectID != "" {
			req["project_id"] = projectID
		}
		if v := optString(args, "folder_id"); v != "" {
			req["folder_id"] = v
		}
		if v := optString(args, "origin_issue_id"); v != "" {
			req["origin_issue_id"] = v
		}
		if v := optString(args, "requester_user_id"); v != "" {
			req["requester_user_id"] = v
		}
		if fileURL != "" {
			req["file_url"] = fileURL
		}
		if v, ok := args["file_size_bytes"]; ok {
			if n, ok := v.(float64); ok {
				req["file_size_bytes"] = int64(n)
			}
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
		Description: "Update an existing artifact's title, body, file pointer, or metadata. Only the original author or a workspace admin can edit. Kind, format, and scope are immutable — to change those, delete and recreate. To change folder placement use set_artifact_folder; to change scope use move_artifact.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id":              map[string]any{"type": "string", "description": "Artifact ID"},
				"title":           map[string]any{"type": "string", "description": "New title (omit to keep current)"},
				"body":            map[string]any{"type": "string", "description": "New body (omit to keep current). For 'pdf' format the body is unused; replace the file via file_url instead."},
				"file_url":        map[string]any{"type": "string", "description": "New file URL for 'pdf' format. Pass an empty string to clear."},
				"file_size_bytes": map[string]any{"type": "integer", "description": "New file size, paired with file_url."},
				"metadata":        map[string]any{"type": "object", "description": "New metadata (omit to keep current)", "additionalProperties": true},
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
		if v, ok := args["file_url"].(string); ok {
			req["file_url"] = v
		}
		if v, ok := args["file_size_bytes"]; ok {
			if n, ok := v.(float64); ok {
				req["file_size_bytes"] = int64(n)
			}
		}
		if md, ok := args["metadata"].(map[string]any); ok && md != nil {
			req["metadata"] = md
		}
		if len(req) == 0 {
			return mcp.ErrorResult("nothing to update; provide at least one of title, body, file_url, file_size_bytes, metadata"), nil
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
	// search_artifacts
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "search_artifacts",
		Description: `Search artifacts across the entire workspace, filtering by kind, scope, or a free-text query against title and body. Use this to find existing reports, plans, or decisions before creating new ones — agents and humans alike accumulate documents over time, and duplicate work is wasteful.

SCOPE values:
- "all"       — every artifact (default if scope is omitted)
- "workspace" — workspace-level artifacts (no project, no issue)
- "project"   — artifacts scoped to any project
- "issue"     — artifacts scoped to any issue`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":            map[string]any{"type": "string", "enum": []string{"report", "plan", "decision", "diagram", "note"}, "description": "Restrict to artifacts of this kind"},
				"scope":           map[string]any{"type": "string", "enum": []string{"all", "workspace", "project", "issue"}, "description": "Restrict by scope"},
				"author_type":     map[string]any{"type": "string", "enum": []string{"all", "member", "agent"}, "description": "Restrict to member-authored or agent-authored artifacts"},
				"author_id":       map[string]any{"type": "string", "description": "Restrict to a specific author (member user_id or agent id)"},
				"origin_issue_id": map[string]any{"type": "string", "description": "Restrict to artifacts whose origin or scope issue is this one"},
				"q":               map[string]any{"type": "string", "description": "Substring match on title or body"},
				"limit":           map[string]any{"type": "integer", "description": "Max results (default 50, cap 200)"},
				"offset":          map[string]any{"type": "integer", "description": "Pagination offset"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		query := url.Values{}
		if v := optString(args, "kind"); v != "" {
			query.Set("kind", v)
		}
		if v := optString(args, "scope"); v != "" {
			query.Set("scope", v)
		}
		if v := optString(args, "author_type"); v != "" {
			query.Set("author_type", v)
		}
		if v := optString(args, "author_id"); v != "" {
			query.Set("author_id", v)
		}
		if v := optString(args, "origin_issue_id"); v != "" {
			query.Set("origin_issue_id", v)
		}
		if v := optString(args, "q"); v != "" {
			query.Set("q", v)
		}
		if _, ok := args["limit"]; ok {
			query.Set("limit", strconv.Itoa(optInt(args, "limit", 50)))
		}
		if _, ok := args["offset"]; ok {
			query.Set("offset", strconv.Itoa(optInt(args, "offset", 0)))
		}
		path := "/api/artifacts"
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		var result []map[string]any
		if err := client.GetJSON(ctx, path, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// move_artifact
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "move_artifact",
		Description: `Re-scope an existing artifact. Use to promote an issue-scoped artifact to its project (durable beyond the issue), or to demote a project artifact to workspace scope, or to attach a workspace artifact to a specific issue or project.

Pass exactly one of project_id or issue_id, or neither (workspace scope). Only the original author or a workspace admin can move an artifact.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id":         map[string]any{"type": "string", "description": "Artifact ID"},
				"project_id": map[string]any{"type": "string", "description": "Move to this project (mutually exclusive with issue_id)"},
				"issue_id":   map[string]any{"type": "string", "description": "Move to this issue (mutually exclusive with project_id)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		projectID := optString(args, "project_id")
		issueID := optString(args, "issue_id")
		if projectID != "" && issueID != "" {
			return mcp.ErrorResult("provide at most one of project_id or issue_id"), nil
		}
		body := map[string]any{}
		if projectID != "" {
			body["project_id"] = projectID
		}
		if issueID != "" {
			body["issue_id"] = issueID
		}
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/artifacts/"+id+"/scope", body, &result); err != nil {
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

	// -----------------------------------------------------------------------
	// set_artifact_folder
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "set_artifact_folder",
		Description: "Place an artifact inside a folder, or move it to root. Folders are orthogonal to scope — moving between folders does not change the artifact's project/issue scope.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id":        map[string]any{"type": "string", "description": "Artifact ID"},
				"folder_id": map[string]any{"type": "string", "description": "Target folder ID. Pass an empty string or omit to move to root."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"folder_id": nil}
		if v := optString(args, "folder_id"); v != "" {
			body["folder_id"] = v
		}
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/artifacts/"+id+"/folder", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// list_artifact_folders
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "list_artifact_folders",
		Description: "List every folder in the workspace. Returns id, parent_id, name. Build the tree client-side via parent_id; null parent_id is a root folder.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var result []map[string]any
		if err := client.GetJSON(ctx, "/api/artifact-folders", &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// create_artifact_folder
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "create_artifact_folder",
		Description: "Create a folder for grouping artifacts. Folders are workspace-wide; pair with parent_id to nest. Folder names must be unique within their parent.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"name"},
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Folder name"},
				"parent_id": map[string]any{"type": "string", "description": "Optional parent folder for nesting. Omit for a root folder."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		name, err := requireString(args, "name")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		req := map[string]any{"name": name}
		if v := optString(args, "parent_id"); v != "" {
			req["parent_id"] = v
		}
		var result map[string]any
		if err := client.PostJSON(ctx, "/api/artifact-folders", req, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// update_artifact_folder
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "update_artifact_folder",
		Description: "Rename a folder or change its parent. Pass an empty parent_id to make the folder a root folder.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id":        map[string]any{"type": "string", "description": "Folder ID"},
				"name":      map[string]any{"type": "string", "description": "New name (omit to keep current)"},
				"parent_id": map[string]any{"type": "string", "description": "New parent folder. Pass an empty string to move to root."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		req := map[string]any{}
		if v, ok := args["name"].(string); ok {
			req["name"] = v
		}
		if v, ok := args["parent_id"].(string); ok {
			req["parent_id"] = v
		}
		if len(req) == 0 {
			return mcp.ErrorResult("nothing to update; provide name or parent_id"), nil
		}
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/artifact-folders/"+id, req, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// delete_artifact_folder
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "delete_artifact_folder",
		Description: "Delete a folder. Subfolders are deleted along with it. Artifacts inside fall back to the root.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Folder ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, "/api/artifact-folders/"+id); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return mcp.TextResult(fmt.Sprintf("Deleted folder %s", id)), nil
	})
}
