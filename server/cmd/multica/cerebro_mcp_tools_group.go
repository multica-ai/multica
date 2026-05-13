// CEREBRO-PATCH(cerebro-groups-mcp): JEH-1172 cerebro-only file — group MCP tools.
package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerCerebroGroupTools wires MCP tools for workspace groups + the
// grouppermissions API (capabilities, runtime / agent allowlists, project
// group-access). Mirrors `multica group ...` in cerebro_group.go so an
// agent can administer groups over MCP with the same surface as a human at
// the CLI.
//
// All routes are workspace-scoped on the server; permission gates
// (admin/owner for writes) are enforced server-side in the handler.
func registerCerebroGroupTools(srv *mcp.Server, client *cli.APIClient) {
	// ----- groups CRUD ----------------------------------------------------

	srv.RegisterTool(mcp.Tool{
		Name:        "list_groups",
		Description: "List workspace groups. Groups bundle members so capability grants, runtime allowlists, and project access can be administered by team instead of per-user.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		if client.WorkspaceID == "" {
			return mcp.ErrorResult("workspace_id is required: set MULTICA_WORKSPACE_ID"), nil
		}
		var result any
		path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/groups"
		if err := client.GetJSON(ctx, path, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "get_group",
		Description: "Fetch a single group by UUID. Returns id, name, description, and timestamps.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(id), &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "create_group",
		Description: "Create a workspace group. Workspace admin/owner only. Names are unique per workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"name"},
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Group name. Should describe the team (e.g. 'Engineering', 'Data')."},
				"description": map[string]any{"type": "string", "description": "Optional description"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		if client.WorkspaceID == "" {
			return mcp.ErrorResult("workspace_id is required: set MULTICA_WORKSPACE_ID"), nil
		}
		name, err := requireString(args, "name")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"name": name}
		if v := optString(args, "description"); v != "" {
			body["description"] = v
		}
		var result any
		path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/groups"
		if err := client.PostJSON(ctx, path, body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "update_group",
		Description: "Rename or update a group's description. Workspace admin/owner only. Only provided fields change; omit a field to leave it untouched.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id"},
			"properties": map[string]any{
				"group_id":    map[string]any{"type": "string", "description": "Group UUID"},
				"name":        map[string]any{"type": "string", "description": "New name"},
				"description": map[string]any{"type": "string", "description": "New description (pass empty string to clear)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{}
		if v, ok := args["name"].(string); ok && v != "" {
			body["name"] = v
		}
		if v, ok := args["description"].(string); ok {
			body["description"] = v
		}
		if len(body) == 0 {
			return mcp.ErrorResult("nothing to update; pass name or description"), nil
		}
		var result any
		if err := client.PatchJSON(ctx, "/api/groups/"+url.PathEscape(id), body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "delete_group",
		Description: "Permanently delete a group. Membership and permission grants are cascaded server-side. Workspace admin/owner only.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, "/api/groups/"+url.PathEscape(id)); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"deleted": id})
	})

	// ----- members --------------------------------------------------------

	srv.RegisterTool(mcp.Tool{
		Name:        "list_group_members",
		Description: "List members of a group: user_id, name, email, when they were added.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(id)+"/members", &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "add_group_member",
		Description: "Add a workspace member to a group. The user must already be a workspace member.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id", "user_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
				"user_id":  map[string]any{"type": "string", "description": "User UUID. Use list_workspace_members or workspace API to resolve names."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		groupID, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		userID, err := requireString(args, "user_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"user_id": userID}
		var result any
		if err := client.PostJSON(ctx, "/api/groups/"+url.PathEscape(groupID)+"/members", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "remove_group_member",
		Description: "Remove a user from a group. Does not delete the user from the workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id", "user_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
				"user_id":  map[string]any{"type": "string", "description": "User UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		groupID, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		userID, err := requireString(args, "user_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		path := "/api/groups/" + url.PathEscape(groupID) + "/members/" + url.PathEscape(userID)
		if err := client.DeleteJSON(ctx, path); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"group_id": groupID, "removed_user_id": userID})
	})

	// ----- capabilities ---------------------------------------------------

	srv.RegisterTool(mcp.Tool{
		Name:        "list_group_capabilities",
		Description: "List capabilities granted to a group. Capabilities (e.g. 'create_runtime', 'create_agent') let group members perform workspace-scoped admin actions without being workspace admins.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(id)+"/capabilities", &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "set_group_capability",
		Description: "Grant a capability to a group. Workspace admin/owner only. Known capabilities: 'create_runtime', 'create_agent'.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id", "capability"},
			"properties": map[string]any{
				"group_id":   map[string]any{"type": "string", "description": "Group UUID"},
				"capability": map[string]any{"type": "string", "description": "Capability identifier (create_runtime, create_agent)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		capability, err := requireString(args, "capability")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"capability": capability}
		var result any
		if err := client.PostJSON(ctx, "/api/groups/"+url.PathEscape(id)+"/capabilities", body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "remove_group_capability",
		Description: "Revoke a capability from a group. Workspace admin/owner only.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id", "capability"},
			"properties": map[string]any{
				"group_id":   map[string]any{"type": "string", "description": "Group UUID"},
				"capability": map[string]any{"type": "string", "description": "Capability identifier"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		capability, err := requireString(args, "capability")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		path := "/api/groups/" + url.PathEscape(id) + "/capabilities/" + url.PathEscape(capability)
		if err := client.DeleteJSON(ctx, path); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"group_id": id, "removed_capability": capability})
	})

	// ----- runtime allowlist ----------------------------------------------

	registerGroupAllowlistTools(srv, client, "runtime", "runtime_id", "Allow a runtime for a group. Workspace admin/owner only. The runtime must be in the same workspace.")

	// ----- agent allowlist ------------------------------------------------

	registerGroupAllowlistTools(srv, client, "agent", "agent_id", "Allow an agent for a group. Workspace admin/owner only. The agent must be in the same workspace.")

	// ----- project group-access -------------------------------------------

	srv.RegisterTool(mcp.Tool{
		Name:        "list_project_groups",
		Description: "List groups with access to a project. Project group-access opens a project to all members of the named groups in addition to direct member grants.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"project_id"},
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "project_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(id)+"/group-access", &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "add_project_group",
		Description: "Grant a group access to a project. Workspace admin/owner only. The group must be in the same workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"project_id", "group_id"},
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project UUID"},
				"group_id":   map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		projectID, err := requireString(args, "project_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		groupID, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{"group_id": groupID}
		var result any
		path := "/api/projects/" + url.PathEscape(projectID) + "/group-access"
		if err := client.PostJSON(ctx, path, body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        "remove_project_group",
		Description: "Revoke a group's access to a project. Workspace admin/owner only.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"project_id", "group_id"},
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project UUID"},
				"group_id":   map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		projectID, err := requireString(args, "project_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		groupID, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		path := "/api/projects/" + url.PathEscape(projectID) + "/group-access/" + url.PathEscape(groupID)
		if err := client.DeleteJSON(ctx, path); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"project_id": projectID, "removed_group_id": groupID})
	})
}

// registerGroupAllowlistTools wires the runtime and agent allowlist tools.
// Both API surfaces are identical except for the resource name in the URL
// path and the body field, so the boilerplate is factored out here. Errors
// from the server are surfaced verbatim so admin-only 403s remain visible
// to the agent.
func registerGroupAllowlistTools(srv *mcp.Server, client *cli.APIClient, resource, idField, addDescription string) {
	pluralPath := resource + "s"
	idParamDesc := fmt.Sprintf("%s UUID", resource)

	srv.RegisterTool(mcp.Tool{
		Name:        fmt.Sprintf("list_group_%s", pluralPath),
		Description: fmt.Sprintf("List %ss allowed for a group. Members can only start tasks on %ss in this allowlist when the workspace requires group-scoped access.", resource, resource),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id"},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/groups/"+url.PathEscape(id)+"/"+pluralPath, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        fmt.Sprintf("add_group_%s", resource),
		Description: addDescription,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id", idField},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
				idField:    map[string]any{"type": "string", "description": idParamDesc},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		groupID, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		resourceID, err := requireString(args, idField)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body := map[string]any{idField: resourceID}
		var result any
		if err := client.PostJSON(ctx, "/api/groups/"+url.PathEscape(groupID)+"/"+pluralPath, body, &result); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(result)
	})

	srv.RegisterTool(mcp.Tool{
		Name:        fmt.Sprintf("remove_group_%s", resource),
		Description: fmt.Sprintf("Revoke a %s from a group's allowlist. Workspace admin/owner only.", resource),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"group_id", idField},
			"properties": map[string]any{
				"group_id": map[string]any{"type": "string", "description": "Group UUID"},
				idField:    map[string]any{"type": "string", "description": idParamDesc},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		groupID, err := requireString(args, "group_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		resourceID, err := requireString(args, idField)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		path := "/api/groups/" + url.PathEscape(groupID) + "/" + pluralPath + "/" + url.PathEscape(resourceID)
		if err := client.DeleteJSON(ctx, path); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"group_id": groupID, "removed_" + idField: resourceID})
	})
}
