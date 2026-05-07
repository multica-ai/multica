// Memory tools — wiki pages, agent notes, runbooks, decisions. Polymorphic
// over `kind` so one set of tools covers all four artifact types. Mirrors
// the server-side surface in handler/memory_artifact.go and the agent CLI in
// server/cmd/multica/cmd_memory.go.
//
// Read tools (list / get / search / by-anchor) are exposed unconditionally
// — they're zero-side-effect. Write tools (create / update / archive) are
// the agent-facing write loop: a chat-driven workflow can record a finding
// or update a runbook step without dropping into the CLI. Hard delete is
// intentionally not exposed; archive is the soft-delete path.

import { z } from "zod";
import { defineTool } from "../tool.js";

const KIND = z.enum(["wiki_page", "agent_note", "runbook", "decision"]);
const ANCHOR_TYPE = z.enum(["issue", "project", "agent", "channel"]);

export const memoryListTool = defineTool({
  name: "multica_memory_list",
  title: "List memory artifacts",
  description:
    "List memory artifacts in the active workspace. Filter by kind and/or parent. Archived rows are hidden by default.",
  inputSchema: z.object({
    kind: KIND.optional(),
    parent_id: z.string().uuid().optional(),
    include_archived: z.boolean().optional(),
    limit: z.number().int().min(1).max(200).optional(),
    offset: z.number().int().min(0).optional(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get("/api/memory", {
      query: {
        kind: input.kind,
        parent_id: input.parent_id,
        include_archived: input.include_archived,
        limit: input.limit,
        offset: input.offset,
      },
    });
  },
});

export const memoryGetTool = defineTool({
  name: "multica_memory_get",
  title: "Get memory artifact",
  description: "Fetch a single memory artifact by id.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/memory/${input.id}`);
  },
});

export const memoryByAnchorTool = defineTool({
  name: "multica_memory_by_anchor",
  title: "List memory artifacts by anchor",
  description:
    "Return all memory artifacts anchored to a specific issue / project / agent / channel. Useful for 'show me the runbooks for THIS issue' lookups.",
  inputSchema: z.object({
    anchor_type: ANCHOR_TYPE,
    anchor_id: z.string().uuid(),
    limit: z.number().int().min(1).max(200).optional(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(
      `/api/memory/by-anchor/${input.anchor_type}/${input.anchor_id}`,
      { query: { limit: input.limit } },
    );
  },
});

export const memorySearchTool = defineTool({
  name: "multica_memory_search",
  title: "Search memory artifacts",
  description:
    "Full-text search over memory artifact titles, content, and tags. Uses Postgres tsvector + websearch_to_tsquery, so user-friendly syntax (quoted phrases, OR, leading -) is supported.",
  inputSchema: z.object({
    q: z.string().min(1),
    kind: KIND.optional(),
    limit: z.number().int().min(1).max(200).optional(),
    offset: z.number().int().min(0).optional(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get("/api/memory/search", {
      query: {
        q: input.q,
        kind: input.kind,
        limit: input.limit,
        offset: input.offset,
      },
    });
  },
});

export const memoryCreateTool = defineTool({
  name: "multica_memory_create",
  title: "Create memory artifact",
  description:
    "Create a new memory artifact. Use kind='agent_note' for findings/decisions/dead-ends produced during a task; 'runbook' for operational procedures; 'decision' for architectural records; 'wiki_page' for general knowledge. Anchor the artifact to an issue / project / agent / channel when it's about a specific thing — anchored artifacts are auto-injected into agent runtime context for that anchor.",
  inputSchema: z.object({
    kind: KIND,
    title: z.string().min(1).max(500),
    content: z.string(),
    slug: z
      .string()
      .regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/, "slug must be lowercase-with-hyphens")
      .optional()
      .describe("Optional stable URL slug (lowercase, hyphenated). Defaults to none."),
    parent_id: z
      .string()
      .uuid()
      .optional()
      .describe("Optional parent artifact id, for hierarchical wiki structures."),
    anchor_type: ANCHOR_TYPE.optional(),
    anchor_id: z
      .string()
      .uuid()
      .optional()
      .describe(
        "Required when anchor_type is set. The two fields must be provided together.",
      ),
    tags: z.array(z.string()).optional(),
    always_inject_at_runtime: z
      .boolean()
      .optional()
      .describe(
        "Workspace-wide: when true and anchor is unset, the artifact is included in every agent task's runtime context. Use sparingly — there's a per-claim cap.",
      ),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post("/api/memory", {
      kind: input.kind,
      title: input.title,
      content: input.content,
      slug: input.slug ?? null,
      parent_id: input.parent_id ?? null,
      anchor_type: input.anchor_type ?? null,
      anchor_id: input.anchor_id ?? null,
      tags: input.tags ?? [],
      metadata: {},
      always_inject_at_runtime: input.always_inject_at_runtime,
    });
  },
});

export const memoryUpdateTool = defineTool({
  name: "multica_memory_update",
  title: "Update memory artifact",
  description:
    "Partial update — only fields you pass are changed. Set anchor_type / anchor_id together to retarget; pass tags to replace the whole array. Cannot change kind (kind is set at creation time).",
  inputSchema: z.object({
    id: z.string().uuid(),
    title: z.string().min(1).max(500).optional(),
    content: z.string().optional(),
    slug: z.string().nullable().optional(),
    parent_id: z.string().uuid().nullable().optional(),
    anchor_type: ANCHOR_TYPE.nullable().optional(),
    anchor_id: z.string().uuid().nullable().optional(),
    tags: z.array(z.string()).optional(),
    always_inject_at_runtime: z.boolean().optional(),
  }),
  handler: async (input, ctx) => {
    const { id, ...patch } = input;
    return ctx.client.put(`/api/memory/${id}`, patch);
  },
});

export const memoryArchiveTool = defineTool({
  name: "multica_memory_archive",
  title: "Archive memory artifact",
  description:
    "Soft-delete an artifact. It stops appearing in default lists and runtime injection but stays queryable via include_archived=true. Reversible via multica_memory_restore.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(`/api/memory/${input.id}/archive`, {});
  },
});

export const memoryRestoreTool = defineTool({
  name: "multica_memory_restore",
  title: "Restore archived memory artifact",
  description: "Undo a multica_memory_archive — clears archived_at / archived_by.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(`/api/memory/${input.id}/restore`, {});
  },
});

// Hard delete is intentionally NOT exposed via MCP. Archive is the soft-
// delete path — a misbehaving model that calls archive can be undone via
// restore. A misbehaving model that calls delete is unrecoverable.
// Operators who need the hard-delete affordance can use the CLI or REST.
export const memoryTools = [
  memoryListTool,
  memoryGetTool,
  memoryByAnchorTool,
  memorySearchTool,
  memoryCreateTool,
  memoryUpdateTool,
  memoryArchiveTool,
  memoryRestoreTool,
];
