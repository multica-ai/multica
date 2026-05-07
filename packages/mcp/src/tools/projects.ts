// Project tools — read-mostly. Project creation IS exposed because
// chat-driven workflows often want to spin up a "project" for a new
// initiative; deletion / resource attachment isn't exposed by default
// because they have wider blast radius.

import { z } from "zod";
import { defineTool } from "../tool.js";

export const projectListTool = defineTool({
  name: "multica_project_list",
  title: "List projects",
  description: "Return all projects in the active workspace (id, name, status, lead).",
  inputSchema: z.object({}),
  handler: async (_input, ctx) => {
    return ctx.client.get("/api/projects");
  },
});

export const projectGetTool = defineTool({
  name: "multica_project_get",
  title: "Get project",
  description: "Fetch one project's metadata + attached resources.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/projects/${input.id}`);
  },
});

export const projectSearchTool = defineTool({
  name: "multica_project_search",
  title: "Search projects",
  description: "Fuzzy search projects by name. Useful when the user names a project loosely.",
  inputSchema: z.object({
    q: z.string().min(1),
    limit: z.number().int().min(1).max(50).optional(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get("/api/projects/search", {
      query: { q: input.q, limit: input.limit },
    });
  },
});

export const projectCreateTool = defineTool({
  name: "multica_project_create",
  title: "Create project",
  description:
    "Create a new project. Title is required; description, status, target_date are optional.",
  inputSchema: z.object({
    title: z.string().min(1),
    description: z.string().optional(),
    status: z
      .enum(["backlog", "planned", "in_progress", "completed", "cancelled"])
      .optional(),
    target_date: z
      .string()
      .optional()
      .describe("RFC3339 due date, e.g. '2026-12-31T00:00:00Z'."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post("/api/projects", {
      title: input.title,
      description: input.description ?? null,
      status: input.status ?? "backlog",
      target_date: input.target_date ?? null,
    });
  },
});

export const projectTools = [
  projectListTool,
  projectGetTool,
  projectSearchTool,
  projectCreateTool,
];
