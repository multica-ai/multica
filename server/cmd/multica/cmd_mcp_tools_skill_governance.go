package main

// CEREBRO-PATCH(mcp-cli-cmd-mcp-tools-skill-governance): FIR-2655 — expose the
// skill ownership / change-request / version / fork governance flow as MCP
// tools so agents working through MCP can PROPOSE and REVIEW skill changes,
// not just the CLI. Backs onto the same REST endpoints the web UI and
// `multica skill` CLI use (internal/handler/skill_ownership.go).

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerSkillGovernanceTools wires the skill-governance MCP tools to the
// stdio server. The governance model (JEH-216) is: a skill has an owner and a
// set of approvers; anyone may PROPOSE a change (a versioned change request),
// but only the owner/approvers may APPROVE it, which atomically applies the
// proposed content and snapshots a new version. Forks branch a skill into a
// new owned copy.
//
// NORM FOR AGENTS: if you want to change a skill you do not own, do NOT edit it
// directly with skill update / update_skill — open a change request with
// `skill_propose_change` and let the owner review it. This keeps skill history
// auditable and lets owners gate quality.
func registerSkillGovernanceTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// skill_list
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_list",
		Description: `List skills in the current workspace. Returns id, name, description,
owner, current_version, and approver list. Use this to find the skill you want
to propose a change to before calling skill_propose_change.`,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var skills any
		if err := client.GetJSON(ctx, "/api/skills", &skills); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(skills)
	})

	// -----------------------------------------------------------------------
	// skill_get
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_get",
		Description: `Get a skill's full details including its current content (SKILL.md body),
files, current_version, owner, and approvers. ALWAYS call this before
proposing a change so you can read the current content/version, then bump the
version and submit your edited content via skill_propose_change.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string", "description": "Skill ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var skill any
		if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(id), &skill); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var files any
		_ = client.GetJSON(ctx, "/api/skills/"+url.PathEscape(id)+"/files", &files)
		return jsonText(map[string]any{"skill": skill, "files": files})
	})

	// -----------------------------------------------------------------------
	// skill_propose_change
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_propose_change",
		Description: `Propose a change to a skill as a versioned change request — the standard
way to change a skill you do not own. The proposal is reviewed by the skill's
owner/approvers; on approval it is applied atomically and snapshotted as a new
version. Nothing changes until an approver approves.

WORKFLOW:
1. Call skill_get first to read the current content and current_version.
2. Edit the content, and choose proposed_version as a semver X.Y.Z strictly
   greater than current_version (e.g. current 1.2.0 → 1.2.1 / 1.3.0 / 2.0.0).
3. Submit with a clear title and a description explaining WHAT changed and WHY.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id", "title", "proposed_version"},
			"properties": map[string]any{
				"skill_id":         map[string]any{"type": "string", "description": "Skill ID to propose a change to"},
				"title":            map[string]any{"type": "string", "description": "Short title for the change request, e.g. 'Add retry guidance to the deploy step'"},
				"description":      map[string]any{"type": "string", "description": "What changed and why — written for the reviewing owner to evaluate the change."},
				"proposed_version": map[string]any{"type": "string", "description": "New semver X.Y.Z, strictly greater than the skill's current_version"},
				"proposed_content": map[string]any{"type": "string", "description": "The full proposed SKILL.md body (the new content). Omit to keep the current content unchanged."},
				"proposed_files": map[string]any{
					"type":        "array",
					"description": "Optional: full set of skill files for the proposed version. Each item is {path, content}. Replaces the file set on approval.",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"path", "content"},
						"properties": map[string]any{
							"path":    map[string]any{"type": "string", "description": "File path within the skill"},
							"content": map[string]any{"type": "string", "description": "File content"},
						},
					},
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		skillID, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		title, err := requireString(args, "title")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		proposedVersion, err := requireString(args, "proposed_version")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{
			"title":            title,
			"proposed_version": proposedVersion,
		}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}
		if v := optString(args, "proposed_content"); v != "" {
			body["proposed_content"] = v
		}
		if v, ok := args["proposed_files"]; ok {
			body["proposed_files"] = v
		}

		var cr any
		if err := client.PostJSON(ctx, "/api/skills/"+url.PathEscape(skillID)+"/change-requests", body, &cr); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(cr)
	})

	// -----------------------------------------------------------------------
	// skill_list_change_requests
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_list_change_requests",
		Description: `List skill change requests (proposals). Without skill_id, returns every
PENDING change request in the workspace awaiting review — use this to find
proposals you (as owner/approver) need to review. With skill_id, returns all
change requests for that skill (pending, merged, rejected).`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string", "description": "Optional: scope to one skill. Omit to list all pending change requests in the workspace."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		path := "/api/skills/change-requests"
		if id := optString(args, "skill_id"); id != "" {
			path = "/api/skills/" + url.PathEscape(id) + "/change-requests"
		}
		var crs any
		if err := client.GetJSON(ctx, path, &crs); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(crs)
	})

	// -----------------------------------------------------------------------
	// skill_review_change_request
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_review_change_request",
		Description: `Approve or reject a skill change request. Only the skill's owner or an
approver may review. Approving applies the proposed content/version atomically
and snapshots a new version; rejecting closes the proposal. Both actions accept
an optional review comment explaining the decision.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"change_request_id", "action"},
			"properties": map[string]any{
				"change_request_id": map[string]any{"type": "string", "description": "Change request ID (from skill_list_change_requests)"},
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"approve", "reject"},
					"description": "approve: apply the change and snapshot a new version. reject: close the proposal.",
				},
				"comment": map[string]any{"type": "string", "description": "Optional review comment explaining the decision"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		crID, err := requireString(args, "change_request_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		action, err := requireString(args, "action")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if action != "approve" && action != "reject" {
			return mcp.ErrorResult("action must be 'approve' or 'reject'"), nil
		}

		body := map[string]any{"action": action}
		if v := optString(args, "comment"); v != "" {
			body["comment"] = v
		}

		var cr any
		if err := client.PostJSON(ctx, "/api/skills/change-requests/"+url.PathEscape(crID)+"/review", body, &cr); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(cr)
	})

	// -----------------------------------------------------------------------
	// skill_list_versions
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_list_versions",
		Description: `List the version history of a skill — each merged change request and the
initial version produce a snapshot here (version, content, files, description,
author, timestamp). Use this to see how a skill evolved or to diff versions.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string", "description": "Skill ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var versions any
		if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(id)+"/versions", &versions); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(versions)
	})

	// -----------------------------------------------------------------------
	// skill_list_forks
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_list_forks",
		Description: `List the forks of a skill — independent owned copies branched from this
skill. Returns the forked skill's id, name, description, and current_version.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string", "description": "Parent skill ID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var forks any
		if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(id)+"/forks", &forks); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(forks)
	})

	// -----------------------------------------------------------------------
	// skill_fork
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_fork",
		Description: `Fork a skill into a new owned copy (content + files), owned by you. Use
this when you want to take a skill in a different direction without going
through the owner's review — the fork gets its own version history and
ownership chain. Prefer skill_propose_change when you just want to improve the
existing skill in place.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id", "name"},
			"properties": map[string]any{
				"skill_id":    map[string]any{"type": "string", "description": "Parent skill ID to fork from"},
				"name":        map[string]any{"type": "string", "description": "Name for the new forked skill (must be unique in the workspace)"},
				"description": map[string]any{"type": "string", "description": "Optional description for the fork (defaults to the parent's)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		skillID, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		name, err := requireString(args, "name")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"name": name}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}
		var forked any
		if err := client.PostJSON(ctx, "/api/skills/"+url.PathEscape(skillID)+"/forks", body, &forked); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(forked)
	})
}
