// Issue lifecycle tools. The high-leverage surface for orchestrating
// agent work from chat: create → assign to an agent → check progress →
// post comments. Pagination params follow the server's conventions
// (limit/offset, default 50, capped at 200 by the handler).

import { z } from "zod";
import { defineTool } from "../tool.js";

const statusEnum = z
  .enum([
    "todo",
    "in_progress",
    "in_review",
    "done",
    "blocked",
    "backlog",
    "cancelled",
  ])
  .describe("Issue status. Default for new issues is 'todo'.");

const priorityEnum = z
  .enum(["urgent", "high", "medium", "low", "none"])
  .describe("Issue priority. Use 'none' (or omit) for no priority.");

const assigneeTypeEnum = z
  .enum(["member", "agent"])
  .describe("'member' for a human, 'agent' for an AI agent.");

export const issueListTool = defineTool({
  name: "multica_issue_list",
  title: "List issues",
  description:
    "List issues in the active workspace with optional filters. Use small limits (e.g. 20) when scanning so the response stays under a few KB.",
  inputSchema: z.object({
    status: statusEnum.optional(),
    priority: priorityEnum.optional(),
    assignee_id: z
      .string()
      .uuid()
      .optional()
      .describe("Filter by assignee (member OR agent UUID)."),
    project_id: z.string().uuid().optional(),
    limit: z.number().int().min(1).max(200).optional().describe("Default 50."),
    offset: z.number().int().min(0).optional(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get("/api/issues", {
      query: {
        status: input.status,
        priority: input.priority,
        assignee: input.assignee_id,
        project: input.project_id,
        limit: input.limit,
        offset: input.offset,
      },
    });
  },
});

export const issueSearchTool = defineTool({
  name: "multica_issue_search",
  title: "Search issues",
  description:
    "Full-text search across issues by title, description, and identifier (e.g. 'MUL-123'). Returns the top matches.",
  inputSchema: z.object({
    q: z.string().min(1).describe("Free-text query."),
    limit: z.number().int().min(1).max(50).optional().describe("Default 10."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get("/api/issues/search", {
      query: { q: input.q, limit: input.limit },
    });
  },
});

export const issueGetTool = defineTool({
  name: "multica_issue_get",
  title: "Get issue",
  description:
    "Fetch full issue details by UUID or human identifier (e.g. 'MUL-123'). Includes title, description, status, priority, assignee, project, parent, due date.",
  inputSchema: z.object({
    id: z.string().min(1).describe("Issue UUID or 'PREFIX-NNN' identifier."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/issues/${encodeURIComponent(input.id)}`);
  },
});

export const issueCreateTool = defineTool({
  name: "multica_issue_create",
  title: "Create issue",
  description:
    "Create a new issue. Title is required; everything else is optional. Pass 'assignee_type'+'assignee_id' to assign at creation (most common: assign to an agent so it picks up immediately).",
  inputSchema: z.object({
    title: z.string().min(1),
    description: z.string().optional(),
    status: statusEnum.optional(),
    priority: priorityEnum.optional(),
    assignee_type: assigneeTypeEnum.optional(),
    assignee_id: z.string().uuid().optional(),
    parent_issue_id: z.string().uuid().optional(),
    project_id: z.string().uuid().optional(),
    due_date: z
      .string()
      .optional()
      .describe("RFC3339 timestamp, e.g. '2026-12-31T00:00:00Z'."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post("/api/issues", {
      title: input.title,
      description: input.description ?? null,
      status: input.status ?? "todo",
      priority: input.priority ?? "none",
      assignee_type: input.assignee_type ?? null,
      assignee_id: input.assignee_id ?? null,
      parent_issue_id: input.parent_issue_id ?? null,
      project_id: input.project_id ?? null,
      due_date: input.due_date ?? null,
    });
  },
});

export const issueUpdateTool = defineTool({
  name: "multica_issue_update",
  title: "Update issue",
  description:
    "Patch one or more issue fields. Only fields you pass are updated. Use 'multica_issue_status' for the common status-only flip.",
  inputSchema: z.object({
    id: z.string().min(1),
    title: z.string().optional(),
    description: z.string().optional(),
    status: statusEnum.optional(),
    priority: priorityEnum.optional(),
    assignee_type: assigneeTypeEnum.optional(),
    assignee_id: z
      .string()
      .uuid()
      .nullable()
      .optional()
      .describe("Pass null to unassign."),
    parent_issue_id: z.string().uuid().nullable().optional(),
    project_id: z.string().uuid().nullable().optional(),
    due_date: z.string().nullable().optional(),
  }),
  handler: async (input, ctx) => {
    const { id, ...rest } = input;
    return ctx.client.patch(`/api/issues/${encodeURIComponent(id)}`, rest);
  },
});

export const issueStatusTool = defineTool({
  name: "multica_issue_status",
  title: "Change issue status",
  description: "Convenience wrapper for status-only updates.",
  inputSchema: z.object({
    id: z.string().min(1),
    status: statusEnum,
  }),
  handler: async (input, ctx) => {
    return ctx.client.patch(`/api/issues/${encodeURIComponent(input.id)}`, {
      status: input.status,
    });
  },
});

export const issueAssignTool = defineTool({
  name: "multica_issue_assign",
  title: "Assign issue",
  description:
    "Assign an issue to a member or agent. Pass assignee_id=null to unassign. Assigning to an agent dispatches a task immediately if the agent has a runtime attached.",
  inputSchema: z.object({
    id: z.string().min(1),
    assignee_type: assigneeTypeEnum.nullable(),
    assignee_id: z.string().uuid().nullable(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.patch(`/api/issues/${encodeURIComponent(input.id)}`, {
      assignee_type: input.assignee_type,
      assignee_id: input.assignee_id,
    });
  },
});

export const issueCommentAddTool = defineTool({
  name: "multica_issue_comment_add",
  title: "Add issue comment",
  description:
    "Post a comment on an issue. Comments can @-mention agents to trigger them — see workspace agent list for valid IDs. Markdown is supported.",
  inputSchema: z.object({
    issue_id: z.string().min(1),
    content: z.string().min(1),
    parent_comment_id: z
      .string()
      .uuid()
      .optional()
      .describe("Reply to an existing comment instead of starting a new thread."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(
      `/api/issues/${encodeURIComponent(input.issue_id)}/comments`,
      {
        content: input.content,
        parent_id: input.parent_comment_id ?? null,
      },
    );
  },
});

export const issueCommentListTool = defineTool({
  name: "multica_issue_comment_list",
  title: "List issue comments",
  description:
    "Return comments on an issue (oldest first, paginated). Includes author identity, content, and parent_id for threading.",
  inputSchema: z.object({
    issue_id: z.string().min(1),
    limit: z.number().int().min(1).max(200).optional().describe("Default 50."),
    offset: z.number().int().min(0).optional(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(
      `/api/issues/${encodeURIComponent(input.issue_id)}/comments`,
      {
        query: { limit: input.limit, offset: input.offset },
      },
    );
  },
});

export const issueRunsTool = defineTool({
  name: "multica_issue_runs",
  title: "List issue task runs",
  description:
    "Return all task runs for an issue (status, dispatched_at, completed_at, error). Use to check whether an assigned agent is making progress.",
  inputSchema: z.object({
    issue_id: z.string().min(1),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(
      `/api/issues/${encodeURIComponent(input.issue_id)}/task-runs`,
    );
  },
});

export const issueTools = [
  issueListTool,
  issueSearchTool,
  issueGetTool,
  issueCreateTool,
  issueUpdateTool,
  issueStatusTool,
  issueAssignTool,
  issueCommentAddTool,
  issueCommentListTool,
  issueRunsTool,
];
