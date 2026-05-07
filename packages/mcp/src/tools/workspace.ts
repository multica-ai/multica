// Workspace + member tools. The "current workspace" is whichever the
// MCP server was started against (env / config); tools that need a
// different workspace can pass an explicit `workspace_id` override.

import { z } from "zod";
import { defineTool } from "../tool.js";

const workspaceIdInput = z.object({
  workspace_id: z
    .string()
    .uuid()
    .optional()
    .describe(
      "Workspace UUID. Defaults to the workspace this MCP server was started against (MULTICA_WORKSPACE_ID).",
    ),
});

export const workspaceGetTool = defineTool({
  name: "multica_workspace_get",
  title: "Get workspace details",
  description:
    "Return the active workspace's id, name, slug, issue prefix, and feature flags. Use as a sanity check before workspace-scoped operations.",
  inputSchema: workspaceIdInput,
  handler: async (input, ctx) => {
    const wsId = input.workspace_id ?? ctx.client.defaultWorkspaceId;
    if (!wsId) {
      throw new Error(
        "No workspace id available. Pass workspace_id explicitly or set MULTICA_WORKSPACE_ID.",
      );
    }
    return ctx.client.get(`/api/workspaces/${wsId}`);
  },
});

export const workspaceMembersTool = defineTool({
  name: "multica_workspace_members",
  title: "List workspace members",
  description:
    "Return the human members of the workspace (id, name, email, role). Use for assignee resolution when the user names someone.",
  inputSchema: workspaceIdInput,
  handler: async (input, ctx) => {
    const wsId = input.workspace_id ?? ctx.client.defaultWorkspaceId;
    if (!wsId) {
      throw new Error(
        "No workspace id available. Pass workspace_id explicitly or set MULTICA_WORKSPACE_ID.",
      );
    }
    return ctx.client.get(`/api/workspaces/${wsId}/members`);
  },
});

export const workspaceTools = [workspaceGetTool, workspaceMembersTool];
