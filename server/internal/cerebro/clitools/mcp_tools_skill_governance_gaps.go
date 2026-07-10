package clitools

// FIR-1775 §5 — close the CLI-only gaps in the skill-governance MCP surface.
// `multica skill diff / update / ownership` existed as CLI commands but had no
// MCP wrapper, so an agent working through MCP could not fully drive skill
// governance (skill_audit was closed earlier in mcp_tools_skill_metadata.go).
// Same pattern as mcp_tools_skill_governance.go (FIR-2655): thin wrappers over
// the already-live /api/skills/* REST endpoints, which carry their own auth.

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/multica-ai/multica/server/internal/cerebro/versioning"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerSkillGovernanceGapTools wires skill_diff, skill_update, and
// skill_set_ownership to the stdio server.
func registerSkillGovernanceGapTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// skill_diff
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_diff",
		Description: `Show a unified diff between two versions of a skill — the SKILL.md body
plus every skill file that changed. 'from' and 'to' are semver versions from
the skill's history (see skill_list_versions). Use this to review what a
change request or rollback would alter before approving it.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id", "from", "to"},
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string", "description": "Skill ID"},
				"from":     map[string]any{"type": "string", "description": "Base version (semver X.Y.Z from the skill's version history)"},
				"to":       map[string]any{"type": "string", "description": "Target version (semver X.Y.Z from the skill's version history)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		from, err := requireString(args, "from")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		to, err := requireString(args, "to")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		var versions []map[string]any
		if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(id)+"/versions", &versions); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		fromV, ok := findSkillVersion(versions, from)
		if !ok {
			return mcp.ErrorResult(fmt.Sprintf("version %q not found in skill history", from)), nil
		}
		toV, ok := findSkillVersion(versions, to)
		if !ok {
			return mcp.ErrorResult(fmt.Sprintf("version %q not found in skill history", to)), nil
		}

		return jsonText(map[string]any{
			"skill_id": id,
			"from":     from,
			"to":       to,
			"files":    diffSkillVersionRows(fromV, toV),
		})
	})

	// -----------------------------------------------------------------------
	// skill_update
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_update",
		Description: `Directly update a skill's name, description, content (SKILL.md body), or
config. This BYPASSES the change-request review flow — the norm is to use
skill_propose_change instead, so the owner reviews the change and history
stays auditable. Only update directly when you own the skill and the change
does not need review. At least one field is required.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id":    map[string]any{"type": "string", "description": "Skill ID to update"},
				"name":        map[string]any{"type": "string", "description": "New skill name"},
				"description": map[string]any{"type": "string", "description": "New skill description"},
				"content":     map[string]any{"type": "string", "description": "New full SKILL.md body"},
				"config":      map[string]any{"type": "object", "description": "New skill config object (replaces the current config)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{}
		if v := optString(args, "name"); v != "" {
			body["name"] = v
		}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}
		if v := optString(args, "content"); v != "" {
			body["content"] = v
		}
		if v, ok := args["config"]; ok {
			body["config"] = v
		}
		if len(body) == 0 {
			return mcp.ErrorResult("no fields to update; provide name, description, content, or config"), nil
		}

		var result any
		if err := client.PutJSON(ctx, "/api/skills/"+url.PathEscape(id), body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	// -----------------------------------------------------------------------
	// skill_set_ownership
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_set_ownership",
		Description: `Set a skill's owner and/or approvers. The owner and approvers are who may
approve change requests for the skill (see skill_review_change_request).
Provide owner_id and/or approver_ids as member or agent UUIDs; approver_ids
replaces the full approver list. Set clear_approvers to remove all approvers.
At least one change is required.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": map[string]any{"type": "string", "description": "Skill ID"},
				"owner_id": map[string]any{"type": "string", "description": "New owner's member or agent UUID"},
				"approver_ids": map[string]any{
					"type":        "array",
					"description": "Full replacement list of approver member/agent UUIDs",
					"items":       map[string]any{"type": "string"},
				},
				"clear_approvers": map[string]any{"type": "boolean", "description": "Set true to remove all approvers (ignored when approver_ids is provided)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		body := map[string]any{}
		if v := optString(args, "owner_id"); v != "" {
			body["owner_id"] = v
		}
		if v, ok := args["approver_ids"]; ok {
			body["approver_ids"] = v
		} else if b, ok := args["clear_approvers"].(bool); ok && b {
			body["approver_ids"] = []string{}
		}
		if len(body) == 0 {
			return mcp.ErrorResult("no changes: provide owner_id, approver_ids, or clear_approvers"), nil
		}

		var result any
		if err := client.PutJSON(ctx, "/api/skills/"+url.PathEscape(id)+"/ownership", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})
}

// skillFileDiff is one changed file in a skill_diff result.
type skillFileDiff struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// findSkillVersion returns the version row matching the given semver.
func findSkillVersion(versions []map[string]any, want string) (map[string]any, bool) {
	for _, v := range versions {
		if s, _ := v["version"].(string); s == want {
			return v, true
		}
	}
	return nil, false
}

// diffSkillVersionRows renders a unified diff per changed file between two
// version rows from GET /api/skills/{id}/versions. The SKILL.md body lives on
// the row as "content"; auxiliary files as "files" [{path, content}].
func diffSkillVersionRows(from, to map[string]any) []skillFileDiff {
	out := []skillFileDiff{}
	fromContent, _ := from["content"].(string)
	toContent, _ := to["content"].(string)
	if d := versioning.UnifiedDiff(fromContent, toContent, "SKILL.md"); d != "" {
		out = append(out, skillFileDiff{Path: "SKILL.md", Diff: d})
	}

	fromFiles := skillVersionFilesByPath(from)
	toFiles := skillVersionFilesByPath(to)
	paths := make([]string, 0, len(fromFiles)+len(toFiles))
	seen := map[string]struct{}{}
	for p := range fromFiles {
		seen[p] = struct{}{}
	}
	for p := range toFiles {
		seen[p] = struct{}{}
	}
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if d := versioning.UnifiedDiff(fromFiles[p], toFiles[p], p); d != "" {
			out = append(out, skillFileDiff{Path: p, Diff: d})
		}
	}
	return out
}

func skillVersionFilesByPath(v map[string]any) map[string]string {
	out := map[string]string{}
	raw, _ := v["files"].([]any)
	for _, item := range raw {
		f, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := f["path"].(string)
		content, _ := f["content"].(string)
		out[path] = content
	}
	return out
}
