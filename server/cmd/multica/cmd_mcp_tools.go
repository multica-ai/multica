package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools): cerebro modification of upstream file

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// maxAttachmentSize matches the server-side limit in handler.UploadFile.
const maxAttachmentSize = 100 << 20 // 100 MB

// captureGitContext returns a one-line summary of the current git state.
func captureGitContext(gitRoot string) string {
	if gitRoot == "" {
		return ""
	}
	branch, err := exec.Command("git", "-C", gitRoot, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	commitMsg, _ := exec.Command("git", "-C", gitRoot, "log", "-1", "--format=%h %s").Output()
	cwd, _ := os.Getwd()
	parts := []string{fmt.Sprintf("branch: %s", strings.TrimSpace(string(branch)))}
	if len(commitMsg) > 0 {
		parts = append(parts, fmt.Sprintf("HEAD: %s", strings.TrimSpace(string(commitMsg))))
	}
	parts = append(parts, fmt.Sprintf("cwd: %s", cwd))
	return strings.Join(parts, ", ")
}

// captureGitDiffStat returns git diff --stat output (uncommitted + staged changes).
func captureGitDiffStat(gitRoot string) string {
	if gitRoot == "" {
		return ""
	}
	// Include both staged and unstaged changes.
	out, err := exec.Command("git", "-C", gitRoot, "diff", "--stat", "HEAD").Output()
	if err != nil || len(out) == 0 {
		// Try just staged changes.
		out, _ = exec.Command("git", "-C", gitRoot, "diff", "--stat", "--cached").Output()
	}
	return strings.TrimSpace(string(out))
}

// persistAmbient writes (or clears, when ambient is empty) the current
// ambient session to the repo binding. Only called when ambient state has
// actually changed — subagent attaches with set_active=false skip this and
// therefore do not clobber the parent's persisted ambient slot.
func persistAmbient(gitRoot string, session *mcpSessionState) {
	if gitRoot == "" {
		return
	}
	wsID, issueID := session.ambient()
	bindings, err := mcp.LoadRepoBindings()
	if err != nil {
		return
	}
	binding, ok := bindings[gitRoot]
	if !ok {
		return
	}
	if wsID != "" {
		binding.ActiveSession = &mcp.ActiveSession{
			WorkSessionID: wsID,
			IssueID:       issueID,
			AttachedAt:    time.Now(),
		}
	} else {
		binding.ActiveSession = nil
	}
	bindings[gitRoot] = binding
	mcp.SaveRepoBindings(bindings)
}

// optBool extracts an optional bool argument with a default value.
func optBool(args map[string]any, key string, defaultVal bool) bool {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}

func registerTools(srv *mcp.Server, client *cli.APIClient, session *mcpSessionState, workspaceID, projectID, gitRoot string) {
	registerArtifactTools(srv, client)

	// -----------------------------------------------------------------------
	// list_issues
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "list_issues",
		Description: `List issues in the current workspace. Returns issues with identifier (e.g. JEH-43), title, status, priority, project, and assignee. Use this to find existing work before creating new issues.

HUMAN COMMUNICATION: When referencing an issue to a human (chat, summary, status update), use both identifier AND title — e.g. "JEH-43 Add rate limiting", not just "JEH-43". The identifier alone is opaque to humans.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":     map[string]any{"type": "string", "description": "Filter by status (backlog, todo, in_progress, done, cancelled)"},
				"priority":   map[string]any{"type": "string", "description": "Filter by priority (urgent, high, medium, low, none)"},
				"project_id": map[string]any{"type": "string", "description": "Filter by project ID"},
				"assignee":   map[string]any{"type": "string", "description": "Filter by assignee ID"},
				"limit":      map[string]any{"type": "integer", "description": "Max results (default 50)"},
				"offset":     map[string]any{"type": "integer", "description": "Offset for pagination"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		params := url.Values{}
		if v := optString(args, "status"); v != "" {
			params.Set("status", v)
		}
		if v := optString(args, "priority"); v != "" {
			params.Set("priority", v)
		}
		if v := optString(args, "project_id"); v != "" {
			params.Set("project_id", v)
		}
		if v := optString(args, "assignee"); v != "" {
			params.Set("assignee_id", v)
		}
		limit := optInt(args, "limit", 50)
		params.Set("limit", strconv.Itoa(limit))
		if offset := optInt(args, "offset", 0); offset > 0 {
			params.Set("offset", strconv.Itoa(offset))
		}

		var result any
		_, err := client.GetJSONWithHeaders(ctx, "/api/issues?"+params.Encode(), &result)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// get_issue
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "get_issue",
		Description: `Get full issue details: title, description, status, comments, and sub-issues. Use this to understand the full context of an issue before working on it or commenting.

HUMAN COMMUNICATION: When referencing an issue to a human (chat, summary, status update), use both identifier AND title — e.g. "JEH-43 Add rate limiting", not just "JEH-43". The identifier alone is opaque to humans.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string", "description": "Issue ID or identifier (e.g. MUL-123)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		var issue any
		if err := client.GetJSON(ctx, "/api/issues/"+id, &issue); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		var comments any
		_ = client.GetJSON(ctx, "/api/issues/"+id+"/comments", &comments)

		var children any
		_ = client.GetJSON(ctx, "/api/issues/"+id+"/children", &children)

		result := map[string]any{
			"issue":    issue,
			"comments": comments,
			"children": children,
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// search_issues
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "search_issues",
		Description: `Search issues by text query across titles and descriptions. Use this to find related or duplicate issues before creating new ones.

HUMAN COMMUNICATION: When referencing an issue to a human (chat, summary, status update), use both identifier AND title — e.g. "JEH-43 Add rate limiting", not just "JEH-43". The identifier alone is opaque to humans.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query"},
				"limit": map[string]any{"type": "integer", "description": "Max results (default 20)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		query, err := requireString(args, "query")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		limit := optInt(args, "limit", 20)

		params := url.Values{}
		params.Set("q", query)
		params.Set("limit", strconv.Itoa(limit))

		var results any
		if err := client.GetJSON(ctx, "/api/issues/search?"+params.Encode(), &results); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(results)
	})

	// -----------------------------------------------------------------------
	// create_issue
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "create_issue",
		Description: `Create a new issue. If a repo-project binding exists, project_id defaults to the bound project.

WRITING GUIDELINES — issues are read by both humans and AI agents:
- Title: clear, specific, action-oriented. GOOD: "Add rate limiting to /api/auth endpoints". BAD: "fix auth thing".
- Description: explain WHAT needs to happen and WHY. Include acceptance criteria when possible. A teammate who has never seen the code should understand the issue from the title and description alone.
- Use sub-issues (parent_issue_id) to break large tasks into verifiable steps.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"title"},
			"properties": map[string]any{
				"title":           map[string]any{"type": "string", "description": "Clear, specific title. Describe the outcome, not the task. Example: 'Add WebSocket reconnection with exponential backoff' not 'fix websocket'"},
				"description":     map[string]any{"type": "string", "description": "Markdown description. Include: what, why, and acceptance criteria. Written for humans — a teammate should understand this without reading the code."},
				"status":          map[string]any{"type": "string", "description": "Status (backlog, todo, in_progress, done, cancelled). Default: todo"},
				"priority":        map[string]any{"type": "string", "description": "Priority (urgent, high, medium, low, none). Default: none"},
				"project_id":      map[string]any{"type": "string", "description": "Project ID to link the issue to"},
				"parent_issue_id": map[string]any{"type": "string", "description": "Parent issue ID for sub-issues"},
				"assignee_type":   map[string]any{"type": "string", "description": "Assignee type: member or agent"},
				"assignee_id":     map[string]any{"type": "string", "description": "Assignee ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		title, err := requireString(args, "title")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{"title": title}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}
		if v := optString(args, "status"); v != "" {
			body["status"] = v
		}
		if v := optString(args, "priority"); v != "" {
			body["priority"] = v
		}

		// Default to bound project if not specified.
		pid := optString(args, "project_id")
		if pid == "" && projectID != "" {
			pid = projectID
		}
		if pid != "" {
			body["project_id"] = pid
		}

		if v := optString(args, "parent_issue_id"); v != "" {
			body["parent_issue_id"] = v
		}
		if v := optString(args, "assignee_type"); v != "" {
			body["assignee_type"] = v
		}
		if v := optString(args, "assignee_id"); v != "" {
			body["assignee_id"] = v
		}

		var issue any
		if err := client.PostJSON(ctx, "/api/issues", body, &issue); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(issue)
	})

	// -----------------------------------------------------------------------
	// update_issue
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "update_issue",
		Description: "Update an existing issue. Only provided fields are changed. Update status to 'in_progress' when starting work, 'done' when complete.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id":        map[string]any{"type": "string", "description": "Issue ID"},
				"title":           map[string]any{"type": "string", "description": "New title"},
				"description":     map[string]any{"type": "string", "description": "New description"},
				"status":          map[string]any{"type": "string", "description": "New status"},
				"priority":        map[string]any{"type": "string", "description": "New priority"},
				"project_id":      map[string]any{"type": "string", "description": "New project ID (empty string to unlink)"},
				"parent_issue_id": map[string]any{"type": "string", "description": "Set, change, or remove the parent issue. Pass an issue ID to set/change, or empty string to remove."},
				"assignee_type":   map[string]any{"type": "string", "description": "Assignee type: member or agent"},
				"assignee_id":     map[string]any{"type": "string", "description": "Assignee ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{}
		for _, key := range []string{"title", "description", "status", "priority", "project_id", "parent_issue_id", "assignee_type", "assignee_id"} {
			if v, ok := args[key]; ok {
				body[key] = v
			}
		}

		var issue any
		if err := client.PutJSON(ctx, "/api/issues/"+id, body, &issue); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(issue)
	})

	// -----------------------------------------------------------------------
	// add_comment
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "add_comment",
		Description: `Add a comment to an issue. Comments are visible to the entire team.

WRITING GUIDELINES:
- Write for humans first. Explain decisions, not just actions.
- GOOD: "Chose to use middleware-based rate limiting instead of reverse proxy because it gives per-endpoint control and we already have the auth middleware in place."
- BAD: "Added rate limiting."
- Reference specific files, functions, or commits when relevant.
- Use comments to document blockers, questions, or handoff context.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id", "content"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string", "description": "Issue ID"},
				"content":  map[string]any{"type": "string", "description": "Comment in markdown. Write for humans — explain decisions, context, and reasoning. Reference files and commits when relevant."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		content, err := requireString(args, "content")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{"content": content}
		var comment any
		if err := client.PostJSON(ctx, "/api/issues/"+id+"/comments", body, &comment); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(comment)
	})

	// -----------------------------------------------------------------------
	// add_attachment
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "add_attachment",
		Description: `Attach a local file to an issue or comment. Reads the file from your machine and uploads it to the workspace. Use for screenshots, logs, diagrams, design files, or any artifact that helps document the work.

USAGE:
- To attach to the issue itself, pass only issue_id.
- To attach to a specific comment, pass both issue_id and comment_id.
- The path may be absolute or relative to the current working directory.
- Max file size is 100 MB.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id", "path"},
			"properties": map[string]any{
				"issue_id":   map[string]any{"type": "string", "description": "Issue to attach the file to"},
				"path":       map[string]any{"type": "string", "description": "Local file path (absolute or relative to cwd)"},
				"comment_id": map[string]any{"type": "string", "description": "Optional: attach to this comment instead of the issue body"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		path, err := requireString(args, "path")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		commentID := optString(args, "comment_id")

		if !filepath.IsAbs(path) {
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
		}

		info, err := os.Stat(path)
		if err != nil {
			return mcp.ErrorResult(fmt.Sprintf("cannot read file: %v", err)), nil
		}
		if info.IsDir() {
			return mcp.ErrorResult(fmt.Sprintf("%s is a directory, not a file", path)), nil
		}
		if info.Size() > maxAttachmentSize {
			return mcp.ErrorResult(fmt.Sprintf("file is %d bytes, max is %d (100 MB)", info.Size(), maxAttachmentSize)), nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return mcp.ErrorResult(fmt.Sprintf("read file: %v", err)), nil
		}

		result, err := client.UploadAttachment(ctx, data, filepath.Base(path), issueID, commentID)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// list_comments
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "list_comments",
		Description: "List comments on an issue. Returns all comments with author, timestamp, and content. Use this to read the discussion history before commenting or to find context from previous sessions.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string", "description": "Issue ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		var comments any
		if err := client.GetJSON(ctx, "/api/issues/"+id+"/comments", &comments); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(comments)
	})

	// -----------------------------------------------------------------------
	// list_projects
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "list_projects",
		Description: "List all projects in the workspace with their status, issue count, and progress. Projects group related issues into a coherent deliverable.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{"type": "string", "description": "Filter by status (planned, in_progress, paused, completed, cancelled)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		params := url.Values{}
		if v := optString(args, "status"); v != "" {
			params.Set("status", v)
		}
		path := "/api/projects"
		if len(params) > 0 {
			path += "?" + params.Encode()
		}

		var projects any
		if err := client.GetJSON(ctx, path, &projects); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(projects)
	})

	// -----------------------------------------------------------------------
	// create_project
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "create_project",
		Description: "Create a new project. Projects are high-level deliverables that group related issues. Title should describe the outcome, e.g. 'Authentication system overhaul' not 'auth work'.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"title"},
			"properties": map[string]any{
				"title":       map[string]any{"type": "string", "description": "Clear project title describing the deliverable. Written for a non-technical stakeholder to understand."},
				"description": map[string]any{"type": "string", "description": "Project description: goals, scope, and success criteria. What does 'done' look like?"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		title, err := requireString(args, "title")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{"title": title}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}

		var project any
		if err := client.PostJSON(ctx, "/api/projects", body, &project); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(project)
	})

	// -----------------------------------------------------------------------
	// update_project
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "update_project",
		Description: "Update an existing project. Only provided fields are changed. Use this to update title, description, status, icon, or other project metadata.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"project_id"},
			"properties": map[string]any{
				"project_id":  map[string]any{"type": "string", "description": "Project ID"},
				"title":       map[string]any{"type": "string", "description": "New title"},
				"description": map[string]any{"type": "string", "description": "New description (goals, scope, success criteria)"},
				"status":      map[string]any{"type": "string", "description": "New status (planned, in_progress, paused, completed, cancelled)"},
				"priority":    map[string]any{"type": "string", "description": "New priority (urgent, high, medium, low, none)"},
				"icon":        map[string]any{"type": "string", "description": "New icon (emoji)"},
				"color":       map[string]any{"type": "string", "description": "New color"},
				"lead_type":   map[string]any{"type": "string", "description": "Lead type: member or agent"},
				"lead_id":     map[string]any{"type": "string", "description": "Lead ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "project_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{}
		for _, key := range []string{"title", "description", "status", "priority", "icon", "color", "lead_type", "lead_id"} {
			if v, ok := args[key]; ok {
				body[key] = v
			}
		}

		var project any
		if err := client.PutJSON(ctx, "/api/projects/"+id, body, &project); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(project)
	})

	// -----------------------------------------------------------------------
	// get_me
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "get_me",
		Description: `Get current user, workspace, bound project, and (if any) the AMBIENT work session for this MCP process.

HUMAN COMMUNICATION: When referencing the ambient issue to a human, use both identifier AND title from active_session — e.g. "JEH-43 Add rate limiting", not just "JEH-43". The identifier alone is opaque to humans.

PARALLEL-SAFETY WARNING: ` + "`active_session`" + ` is the AMBIENT pointer — shared across every Claude session that talks to this MCP process. It is fragile when subagents run in parallel; siblings that call attach_session will overwrite it. Use it only as a hint to resume work after a restart. For routing your own activity (report_activity, complete_work, ...), always use the work_session_id YOU received from attach_session/resume_session/fork_session — never read it from here.`,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var me any
		if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		result := map[string]any{
			"user":         me,
			"workspace_id": workspaceID,
			"git_root":     gitRoot,
		}

		// Resolve project name.
		if projectID != "" {
			var proj map[string]any
			if err := client.GetJSON(ctx, "/api/projects/"+projectID, &proj); err == nil {
				result["project"] = map[string]any{
					"id":    projectID,
					"title": proj["title"],
				}
			} else {
				result["project_id"] = projectID
			}
		}

		// Include ambient session info. NOTE: ambient is shared across this
		// MCP process — see description warning above.
		ambientID, ambientIssueID := session.ambient()
		if ambientID != "" {
			active := map[string]any{
				"work_session_id": ambientID,
				"issue_id":        ambientIssueID,
				"note":            "ambient pointer — do NOT use as routing target for report_activity/complete_work; pass the work_session_id you got from attach_session/resume_session/fork_session instead.",
			}
			// Resolve issue title.
			var issue map[string]any
			if err := client.GetJSON(ctx, "/api/issues/"+ambientIssueID, &issue); err == nil {
				active["issue_identifier"] = issue["identifier"]
				active["issue_title"] = issue["title"]
			}
			result["active_session"] = active
		}

		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// bind_repo
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "bind_repo",
		Description: "Bind the current git repository to a workspace and optional project. The binding persists across sessions.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"workspace_id": map[string]any{"type": "string", "description": "Workspace ID (defaults to current)"},
				"project_id":   map[string]any{"type": "string", "description": "Project ID to auto-select"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		if gitRoot == "" {
			return mcp.ErrorResult("not in a git repository"), nil
		}

		wsID := optString(args, "workspace_id")
		if wsID == "" {
			wsID = workspaceID
		}
		pID := optString(args, "project_id")

		bindings, err := mcp.LoadRepoBindings()
		if err != nil {
			return mcp.ErrorResult(fmt.Sprintf("load bindings: %v", err)), nil
		}

		bindings[gitRoot] = mcp.RepoBinding{
			WorkspaceID: wsID,
			ProjectID:   pID,
		}

		if err := mcp.SaveRepoBindings(bindings); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("save bindings: %v", err)), nil
		}

		return mcp.TextResult(fmt.Sprintf("Bound %s → workspace %s, project %s", gitRoot, wsID, pID)), nil
	})

	// -----------------------------------------------------------------------
	// attach_session
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "attach_session",
		Description: `Create a work session for an issue and return its ` + "`work_session_id`" + `. Auto-captures git context at start.

YOU MUST CAPTURE THE RETURNED ` + "`work_session_id`" + ` AND PASS IT EXPLICITLY to every subsequent ` + "`report_activity`" + ` / ` + "`complete_work`" + ` call on THIS session. Do NOT read it back from ` + "`get_me.active_session`" + ` — that pointer is shared across the MCP process and gets clobbered by parallel subagents.

PARENT vs SUBAGENT:
- Main/parent agents: leave ` + "`set_active`" + ` at its default (true) so ` + "`get_me`" + ` and the cross-restart resume slot reflect your work.
- Subagents and any agent running alongside another active agent: pass ` + "`set_active=false`" + `. You still get a fresh ` + "`work_session_id`" + ` to report against, but the parent's ambient slot stays untouched.

The persisted ambient slot survives MCP restarts so a single-agent flow can resume; it is NOT a routing target.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id":   map[string]any{"type": "string", "description": "Issue ID to attach to"},
				"set_active": map[string]any{"type": "boolean", "description": "Whether to also store this as the ambient session for the MCP process (used by get_me and cross-restart resume). Default true. Pass false from a subagent so the parent's ambient pointer is preserved."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		setActive := optBool(args, "set_active", true)

		cwd, _ := os.Getwd()
		branch := ""
		if gitRoot != "" {
			if out, err := exec.Command("git", "-C", gitRoot, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
				branch = strings.TrimSpace(string(out))
			}
		}
		body := map[string]any{
			"issue_id":     issueID,
			"session_type": "claude-code",
			"work_dir":     cwd,
			"branch":       branch,
		}

		var result map[string]any
		if err := client.PostJSON(ctx, "/api/work-sessions", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		id, _ := result["id"].(string)
		session.track(id, issueID, 0)
		if setActive {
			session.setAmbient(id, issueID)
			persistAmbient(gitRoot, session)
		}

		// Auto-report session start context.
		startContext := captureGitContext(gitRoot)
		if startContext != "" {
			seq := session.nextSeq(id)
			client.PostJSON(ctx, "/api/work-sessions/"+id+"/messages", map[string]any{
				"messages": []map[string]any{{
					"seq":     seq,
					"type":    "note",
					"content": "[session_start] " + startContext,
				}},
			}, nil)
		}

		ambientNote := ""
		if !setActive {
			ambientNote = " (ambient unchanged — parent's get_me slot preserved)"
		}
		return mcp.TextResult(fmt.Sprintf("Attached to issue %s (work_session_id: %s)%s. CAPTURE this work_session_id and pass it to every report_activity / complete_work call you make for this issue.", issueID, id, ambientNote)), nil
	})

	// -----------------------------------------------------------------------
	// complete_work
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "complete_work",
		Description: `Mark a work session as complete. Auto-captures git diff. Call this when you finish working on an issue — do not leave sessions dangling.

You MUST pass the ` + "`work_session_id`" + ` you got from ` + "`attach_session`" + ` (or ` + "`resume_session`" + ` / ` + "`fork_session`" + `). The MCP process does not infer it from ambient state, because in parallel-subagent flows the ambient pointer belongs to a different agent.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"work_session_id", "summary"},
			"properties": map[string]any{
				"work_session_id": map[string]any{"type": "string", "description": "The work_session_id returned by attach_session/resume_session/fork_session for THIS session."},
				"summary":         map[string]any{"type": "string", "description": "Human-readable summary: what was done, what was decided, what remains. A colleague should understand the state of the issue from this summary alone."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		wsID, err := requireString(args, "work_session_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		summary, err := requireString(args, "summary")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		// Auto-report git diff before completing.
		diffStat := captureGitDiffStat(gitRoot)
		if diffStat != "" {
			seq := session.nextSeq(wsID)
			client.PostJSON(ctx, "/api/work-sessions/"+wsID+"/messages", map[string]any{
				"messages": []map[string]any{{
					"seq":     seq,
					"type":    "note",
					"content": "[session_complete] Files changed:\n" + diffStat,
				}},
			}, nil)
		}

		body := map[string]any{"summary": summary}
		if err := client.PutJSON(ctx, "/api/work-sessions/"+wsID+"/complete", body, nil); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		session.forget(wsID)
		// Only clear the ambient slot if THIS session was the ambient one —
		// otherwise a subagent completing its work would erase the parent's
		// pointer.
		if session.clearAmbientIfMatches(wsID) {
			persistAmbient(gitRoot, session)
		}

		return mcp.TextResult(fmt.Sprintf("Work session %s completed", wsID)), nil
	})

	// -----------------------------------------------------------------------
	// report_activity
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "report_activity",
		Description: `Report a meaningful activity on a work session. Call at natural milestones only: after design decisions, verification results, or when blocked. Do NOT call for individual file changes (captured at completion).

You MUST pass the ` + "`work_session_id`" + ` you got from ` + "`attach_session`" + ` (or ` + "`resume_session`" + ` / ` + "`fork_session`" + `). The MCP process does not infer it — in parallel-subagent flows the ambient pointer is shared across siblings and would route activity to the wrong session.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"work_session_id", "type", "summary"},
			"properties": map[string]any{
				"work_session_id": map[string]any{"type": "string", "description": "The work_session_id returned by attach_session/resume_session/fork_session for THIS session."},
				"type": map[string]any{
					"type":        "string",
					"enum":        []string{"decision", "verification", "blocker", "dependency", "error", "note"},
					"description": "decision: chose approach A over B. verification: test/typecheck result. blocker: need input or stuck. dependency: added package/migration. error: something failed and how it was fixed. note: other milestone.",
				},
				"summary": map[string]any{"type": "string", "description": "One-line summary of what happened"},
				"details": map[string]any{"type": "string", "description": "Optional extra context (keep brief)"},
				"files":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Relevant file paths (optional)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		wsID, err := requireString(args, "work_session_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		actType, err := requireString(args, "type")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		summary, err := requireString(args, "summary")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		// Build structured content.
		content := fmt.Sprintf("[%s] %s", actType, summary)
		if details := optString(args, "details"); details != "" {
			content += "\n" + details
		}

		// Build input map for files.
		var input map[string]any
		if files, ok := args["files"]; ok {
			input = map[string]any{"files": files}
		}

		seq := session.nextSeq(wsID)

		msg := map[string]any{
			"seq":     seq,
			"type":    actType,
			"content": content,
		}
		if input != nil {
			msg["input"] = input
		}

		body := map[string]any{
			"messages": []map[string]any{msg},
		}

		if err := client.PostJSON(ctx, "/api/work-sessions/"+wsID+"/messages", body, nil); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		// Auto-name session from first activity (per work_session_id, so
		// parallel sessions each get their own first-activity name).
		if session.markNamed(wsID) {
			client.PutJSON(ctx, "/api/work-sessions/"+wsID+"/name", map[string]any{"name": summary}, nil)
		}

		return mcp.TextResult("activity reported"), nil
	})

	// -----------------------------------------------------------------------
	// resume_session
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "resume_session",
		Description: `Resume a previously completed work session. Re-opens it, restores the message sequence counter, and continues where you left off. Use this when returning to unfinished work.

The returned ` + "`work_session_id`" + ` is the SAME id you passed in — capture it and pass it to subsequent ` + "`report_activity`" + ` / ` + "`complete_work`" + ` calls. Subagents resuming alongside an active parent should pass ` + "`set_active=false`" + `.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"work_session_id"},
			"properties": map[string]any{
				"work_session_id": map[string]any{"type": "string", "description": "Work session ID to resume"},
				"set_active":      map[string]any{"type": "boolean", "description": "Whether to also store this as the ambient session for the MCP process. Default true. Pass false from a subagent."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		wsID, err := requireString(args, "work_session_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		setActive := optBool(args, "set_active", true)

		var result map[string]any
		if err := client.PostJSON(ctx, "/api/work-sessions/"+wsID+"/resume", map[string]any{}, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		issueID, _ := result["issue_id"].(string)

		// Restore seq counter from existing messages.
		seq := 0
		var msgs []any
		if err := client.GetJSON(ctx, "/api/work-sessions/"+wsID+"/messages", &msgs); err == nil {
			seq = len(msgs)
		}

		session.track(wsID, issueID, seq)
		if setActive {
			session.setAmbient(wsID, issueID)
			persistAmbient(gitRoot, session)
		}

		ambientNote := ""
		if !setActive {
			ambientNote = " (ambient unchanged)"
		}
		return mcp.TextResult(fmt.Sprintf("Resumed work_session_id %s on issue %s (seq: %d)%s. Pass this work_session_id to every subsequent report_activity / complete_work call.", wsID, issueID, seq, ambientNote)), nil
	})

	// -----------------------------------------------------------------------
	// fork_session
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "fork_session",
		Description: `Fork an existing work session — creates a new session on the same issue with fresh tracking. Use when you want to try a different approach without overwriting the original session's history.

CAPTURE the returned new work_session_id and pass it to subsequent ` + "`report_activity`" + ` / ` + "`complete_work`" + ` calls. Subagents forking alongside an active parent should pass ` + "`set_active=false`" + `.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"work_session_id"},
			"properties": map[string]any{
				"work_session_id": map[string]any{"type": "string", "description": "Work session ID to fork from"},
				"set_active":      map[string]any{"type": "boolean", "description": "Whether to also store the new session as the ambient session. Default true. Pass false from a subagent."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		wsID, err := requireString(args, "work_session_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		setActive := optBool(args, "set_active", true)

		var result map[string]any
		if err := client.PostJSON(ctx, "/api/work-sessions/"+wsID+"/fork", map[string]any{}, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		newID, _ := result["id"].(string)
		issueID, _ := result["issue_id"].(string)

		session.track(newID, issueID, 0)
		if setActive {
			session.setAmbient(newID, issueID)
			persistAmbient(gitRoot, session)
		}

		ambientNote := ""
		if !setActive {
			ambientNote = " (ambient unchanged)"
		}
		return mcp.TextResult(fmt.Sprintf("Forked session %s → new work_session_id %s on issue %s%s. Pass the new work_session_id to every subsequent report_activity / complete_work call.", wsID, newID, issueID, ambientNote)), nil
	})

	// -----------------------------------------------------------------------
	// list_runtimes (CEREBRO-PATCH(mcp-list-runtimes))
	//
	// Exposes runtime health + pause state to agents so they can tell when a
	// runtime is paused (manual or auto via rate-limit / monthly-cap) and
	// shouldn't be assigned work. Without this, an agent that wants to route
	// a task by checking runtime availability has to fall back to "claim
	// returned empty queue" — which is indistinguishable from "no work" and
	// hides the actual gate.
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "list_runtimes",
		Description: `List runtimes in the current workspace with health and pause state. Use this BEFORE routing or suggesting work to a specific runtime/agent so you can detect paused runtimes and route around them.

Each runtime in the result includes:
- id, name, runtime_mode (local/cloud), provider, status (online/offline)
- last_seen_at — when the daemon last heartbeated
- paused_at — non-null if the runtime is paused; null otherwise
- unpause_at — scheduled auto-unpause time (null = pause is open-ended / manual)
- pause_reason — "auto" (rate-limit/quota detected) or "manual" (operator-paused)

A runtime with paused_at != null will silently refuse all task dispatches until unpaused. Treat it as unavailable for assignment.`,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		var result any
		_, err := client.GetJSONWithHeaders(ctx, "/api/runtimes", &result)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})
}
