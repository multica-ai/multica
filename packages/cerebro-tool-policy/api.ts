import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

// --- FIR-1771 "Test as user" ------------------------------------------------
// Resolve another user+agent's effective tool permission from inside the app.

const TestAsUserEffectiveSchema = z
  .object({
    setting: z.string().default(""),
    decided_by: z.string().default(""),
    capped_by: z.string().default(""),
    reason: z.string().default(""),
  })
  .loose();

const TestAsUserGroupSchema = z
  .object({ name: z.string().default(""), owner: z.string().default("") })
  .loose();

const TestAsUserToolSchema = z
  .object({
    tool_key: z.string().default(""),
    title: z.string().default(""),
    category: z.string().default(""),
    effective: TestAsUserEffectiveSchema.default({
      setting: "",
      decided_by: "",
      capped_by: "",
      reason: "",
    }),
    capped_by_groups: z.array(TestAsUserGroupSchema).default([]),
  })
  .loose();

const TestAsUserResponseSchema = z
  .object({ tools: z.array(TestAsUserToolSchema).default([]) })
  .loose();

const TestAsUserAccessSchema = z
  .object({ allowed: z.boolean().default(false) })
  .loose();

export type TestAsUserTool = z.infer<typeof TestAsUserToolSchema>;

// getTestAsUserAccess reports whether the current member may use the feature, so
// the profile menu can show or hide the entry. Defaults closed (hidden) on any
// parse/transport failure.
export async function getTestAsUserAccess(
  workspaceId: string,
): Promise<boolean> {
  const path = `/api/workspaces/${workspaceId}/cerebro/test-as-user/access`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, TestAsUserAccessSchema, { allowed: false }, {
    endpoint: path,
  }).allowed;
}

// runTestAsUser resolves every tool for the (user, agent, runtime) context and
// returns the rows. runtimeId is the selected agent's runtime so the Runtime
// layer is reflected. Returns [] on parse/transport failure.
export async function runTestAsUser(
  workspaceId: string,
  input: { userId: string; agentId: string; runtimeId?: string },
): Promise<TestAsUserTool[]> {
  const path = `/api/workspaces/${workspaceId}/cerebro/test-as-user`;
  const raw = await api.cerebroRequest<unknown>(path, {
    method: "POST",
    body: JSON.stringify({
      user_id: input.userId,
      agent_id: input.agentId,
      runtime_id: input.runtimeId ?? "",
    }),
  });
  return parseWithFallback(raw, TestAsUserResponseSchema, { tools: [] }, {
    endpoint: path,
  }).tools;
}

// --- FIR-3091 punkt 8: per-permission holders --------------------------------
// The reverse of the per-subject table: for one tool, every explicit policy row
// across all layers and subjects in the workspace. Powers the "who holds this
// permission and which layer grants it" detail page. subject_id is a raw UUID
// resolved to a name on the client from lists it already holds.

const PermissionHolderSchema = z
  .object({
    layer: z.string().default(""),
    subject_id: z.string().default(""),
    resource_pattern: z.string().default(""),
    setting: z.string().default(""),
  })
  .loose();

const PermissionHoldersSchema = z
  .object({
    tool_key: z.string().default(""),
    // enforced=false means the tool's policy is stored but not enforced at
    // runtime yet, so the page can grey those rows out. Defaults false (the
    // safe, honest reading) on any parse failure.
    enforced: z.boolean().default(false),
    holders: z.array(PermissionHolderSchema).default([]),
  })
  .loose();

export type PermissionHolder = z.infer<typeof PermissionHolderSchema>;
export type PermissionHolders = z.infer<typeof PermissionHoldersSchema>;

// getPermissionHolders lists every explicit policy row for one tool. Admin/owner
// only server-side. Returns an empty, not-enforced result on parse/transport
// failure so the page renders "no holders" rather than crashing.
export async function getPermissionHolders(
  workspaceId: string,
  toolKey: string,
): Promise<PermissionHolders> {
  const path = `/api/workspaces/${workspaceId}/tool-policy/holders?tool_key=${encodeURIComponent(
    toolKey,
  )}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(
    raw,
    PermissionHoldersSchema,
    { tool_key: toolKey, enforced: false, holders: [] },
    { endpoint: path },
  );
}

// --- FIR-3091 punkt 8 fase 2: per-permission change log ----------------------
// One tool's Set/Clear history, newest first. old_setting "" means the layer
// held no explicit row before the write (Inherit); new_setting "" means the row
// was cleared back to Inherit. Recording started when fase 2 shipped, so an
// empty list means "no changes since then", not "never changed".

const PermissionChangeSchema = z
  .object({
    layer: z.string().default(""),
    subject_id: z.string().default(""),
    resource_pattern: z.string().default(""),
    action: z.string().default(""),
    old_setting: z.string().default(""),
    new_setting: z.string().default(""),
    actor_type: z.string().default(""),
    actor_id: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

const PermissionChangesSchema = z
  .object({
    tool_key: z.string().default(""),
    changes: z.array(PermissionChangeSchema).default([]),
  })
  .loose();

export type PermissionChange = z.infer<typeof PermissionChangeSchema>;
export type PermissionChanges = z.infer<typeof PermissionChangesSchema>;

// getPermissionChanges lists one tool's change log. Admin/owner only
// server-side. Returns an empty result on parse/transport failure so the page
// renders "no changes" rather than crashing.
export async function getPermissionChanges(
  workspaceId: string,
  toolKey: string,
): Promise<PermissionChanges> {
  const path = `/api/workspaces/${workspaceId}/tool-policy/changes?tool_key=${encodeURIComponent(
    toolKey,
  )}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(
    raw,
    PermissionChangesSchema,
    { tool_key: toolKey, changes: [] },
    { endpoint: path },
  );
}
