// Frontend data layer for the FIR-2230 unified per-tool permission table — the
// client half of "the data layer the screen reads from". It calls the backend
// tool-policy API (GET table, PUT set, DELETE clear) through the generic cerebro
// request primitive, so no bespoke method has to land in the upstream api client.
//
// Schemas fail CLOSED: a permission surface must never render "allow" because a
// response drifted. An unknown effective setting downgrades to "deny", and an
// unknown per-layer setting downgrades to "no override" (null) — never a crash,
// never a silent privilege grant (see CLAUDE.md → API Response Compatibility).

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { toast } from "sonner";
import { z } from "zod";

// --- types ------------------------------------------------------------------

/** The per-layer authoring choice for one tool. `inherit` follows the layer below. */
export type ToolSetting = "inherit" | "allow" | "ask" | "deny";
/** A rung of the Workspace › Runtime › Agent › Group › User chain. */
export type ToolLayer = "workspace" | "runtime" | "agent" | "group" | "user";
/** The resolved verdict is always concrete — never `inherit`. */
export type ToolEffectiveSetting = "allow" | "ask" | "deny";

export interface ToolPolicyEffective {
  setting: ToolEffectiveSetting;
  /** Layer that decided the verdict; "" when the base default decided. */
  decided_by: string;
  /** Layer that tightened below the base ("Capped by …"); "" when none. */
  capped_by: string;
  /** Human-readable explanation, e.g. "Capped by user". */
  reason: string;
}

/**
 * One group that drives a group-layer cap on a row, with its owner (the group's
 * creator). Populated by the backend only when the Group layer decided/capped
 * the row, so the UI can name the exact group to change and who owns it
 * (TECH-3287 hul 5). Owner is "" for groups with no recorded creator.
 */
export interface GroupAttribution {
  name: string;
  owner: string;
}

/** Explicit setting at each layer for the queried context; null = no override. */
export interface ToolPolicyLayers {
  workspace: ToolSetting | null;
  runtime: ToolSetting | null;
  agent: ToolSetting | null;
  group: ToolSetting | null;
  user: ToolSetting | null;
}

/** One tool's full row for the admin table. */
export interface ToolPolicyRow {
  tool_key: string;
  /**
   * Per-resource scope (FIR-2505). Empty for a capability-wide row; a repo URL
   * for a per-repo row, where the same tool_key (repo.read/checkout/push)
   * appears once per repo. The collapsible "repo group" keys on this.
   */
  resource_pattern: string;
  title: string;
  category: string;
  source: string;
  /**
   * FIR-2594: true for a platform action whose enforcement point is not the
   * tool-policy gate (workspace-membership ACL, daemon token, webhook secret).
   * The row is shown for visibility but its Allow/Ask/Deny choice is advisory.
   */
  managed_externally: boolean;
  layers: ToolPolicyLayers;
  effective: ToolPolicyEffective;
  /**
   * The group(s) behind a group-layer cap on this row, each with its owner.
   * Empty unless the Group layer decided/capped the verdict (TECH-3287 hul 5).
   */
  capped_by_groups: GroupAttribution[];
}

/**
 * The chain context the effective column is computed for. An agent page passes
 * agent + its runtime + the viewing user + the user's groups; a runtime page
 * passes only the runtime. Omitted layers are simply absent from the chain.
 */
export interface ToolPolicyContext {
  runtimeId?: string | null;
  agentId?: string | null;
  userId?: string | null;
  groupIds?: string[];
  /** Workspace/system default below the runtime; defaults to allow server-side. */
  base?: ToolSetting | null;
}

export interface SetToolPolicyRequest {
  tool_key: string;
  layer: ToolLayer;
  subject_id: string;
  setting: ToolSetting;
  /** Per-resource scope (e.g. a repo URL); omit/empty for a capability-wide write. */
  resource_pattern?: string;
}

export interface ClearToolPolicyRequest {
  tool_key: string;
  layer: ToolLayer;
  subject_id: string;
  /** Per-resource scope to clear; omit/empty for the capability-wide row. */
  resource_pattern?: string;
}

// --- pure helpers -----------------------------------------------------------

/**
 * Whether the effective verdict is forced by a layer the given editLayer cannot
 * loosen — a higher group/user cap (capped_by) OR a workspace/runtime base that
 * decided a restriction (decided_by ≠ editLayer). The chain only ever tightens,
 * so once another layer decides Ask/Deny this page can never reach a looser
 * result: that is exactly when a control should read as locked (TECH-3287 hul 2).
 * Lives in core so both the table and the connection sheet share one definition.
 */
export function isLockedFromElsewhere(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
): boolean {
  const eff = row.effective;
  if (eff.capped_by) return true;
  return !!eff.decided_by && eff.decided_by !== editLayer && eff.setting !== "allow";
}

// --- schemas (fail closed) --------------------------------------------------

const TOOL_SETTINGS = ["inherit", "allow", "ask", "deny"] as const;

// A per-layer setting drifts to "no override" (null) rather than failing the
// whole table parse.
const layerSettingSchema = z.preprocess(
  (v) => ((TOOL_SETTINGS as readonly string[]).includes(v as string) ? v : null),
  z.enum(TOOL_SETTINGS).nullable(),
);

// The effective setting drifts to the SAFEST value (deny) — never allow.
const effectiveSettingSchema = z.preprocess(
  (v) => (v === "allow" || v === "ask" || v === "deny" ? v : "deny"),
  z.enum(["allow", "ask", "deny"]),
);

const toolPolicyRowSchema = z.object({
  tool_key: z.string(),
  resource_pattern: z.string().default(""),
  title: z.string().default(""),
  category: z.string().default(""),
  source: z.string().default(""),
  // FIR-2594: true for platform actions whose enforcement point is not the
  // tool-policy gate (membership ACL, daemon token, …). Defaults false so older
  // backends that omit the field render as a normal, gated row.
  managed_externally: z.boolean().default(false),
  layers: z
    .object({
      workspace: layerSettingSchema.default(null),
      runtime: layerSettingSchema.default(null),
      agent: layerSettingSchema.default(null),
      group: layerSettingSchema.default(null),
      user: layerSettingSchema.default(null),
    })
    .default({ workspace: null, runtime: null, agent: null, group: null, user: null }),
  effective: z
    .object({
      setting: effectiveSettingSchema,
      decided_by: z.string().default(""),
      capped_by: z.string().default(""),
      reason: z.string().default(""),
    })
    .default({ setting: "deny", decided_by: "", capped_by: "", reason: "" }),
  // Group attribution drifts to an empty list rather than failing the parse — a
  // missing/odd value just means "no named group", never a crash.
  capped_by_groups: z
    .array(
      z.object({
        name: z.string().default(""),
        owner: z.string().default(""),
      }),
    )
    .catch([])
    .default([]),
});

const toolPolicyTableSchema = z.object({
  tools: z.array(toolPolicyRowSchema).default([]),
});

// --- API --------------------------------------------------------------------

/** Fetch every tool with per-layer settings + resolved Effective for a context. */
export async function fetchToolPolicyTable(
  wsId: string,
  ctx: ToolPolicyContext,
): Promise<ToolPolicyRow[]> {
  const params = new URLSearchParams();
  if (ctx.runtimeId) params.set("runtime_id", ctx.runtimeId);
  if (ctx.agentId) params.set("agent_id", ctx.agentId);
  if (ctx.userId) params.set("user_id", ctx.userId);
  for (const g of ctx.groupIds ?? []) params.append("group_id", g);
  if (ctx.base) params.set("base", ctx.base);
  const qs = params.toString();

  const raw = await api.cerebroRequest<unknown>(
    qs
      ? `/api/workspaces/${wsId}/tool-policy?${qs}`
      : `/api/workspaces/${wsId}/tool-policy`,
  );
  const parsed = parseWithFallback(raw, toolPolicyTableSchema, { tools: [] }, {
    endpoint: "toolPolicyTable",
  });
  return parsed.tools;
}

/** Set one layer's explicit choice for one tool (admin/owner server-side). */
export async function setToolPolicy(
  wsId: string,
  body: SetToolPolicyRequest,
): Promise<void> {
  await api.cerebroRequest<void>(`/api/workspaces/${wsId}/tool-policy`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

/** Clear one layer's choice for one tool so it reverts to Inherit. */
export async function clearToolPolicy(
  wsId: string,
  body: ClearToolPolicyRequest,
): Promise<void> {
  const params = new URLSearchParams({
    tool_key: body.tool_key,
    layer: body.layer,
    subject_id: body.subject_id,
  });
  if (body.resource_pattern) params.set("resource_pattern", body.resource_pattern);
  await api.cerebroRequest<void>(
    `/api/workspaces/${wsId}/tool-policy?${params.toString()}`,
    { method: "DELETE" },
  );
}

// --- query options + hooks --------------------------------------------------

export const toolPolicyKeys = {
  all: (wsId: string) => ["cerebro", "tool-policy", wsId] as const,
  table: (wsId: string, ctx: ToolPolicyContext) =>
    [
      ...toolPolicyKeys.all(wsId),
      "table",
      ctx.runtimeId ?? null,
      ctx.agentId ?? null,
      ctx.userId ?? null,
      (ctx.groupIds ?? []).join(","),
      ctx.base ?? null,
    ] as const,
};

export function toolPolicyTableOptions(wsId: string, ctx: ToolPolicyContext) {
  return queryOptions({
    queryKey: toolPolicyKeys.table(wsId, ctx),
    queryFn: () => fetchToolPolicyTable(wsId, ctx),
    enabled: !!wsId,
    staleTime: 15 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function useSetToolPolicy() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: SetToolPolicyRequest) => setToolPolicy(wsId, body),
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to update permission");
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: toolPolicyKeys.all(wsId) });
    },
  });
}

export function useClearToolPolicy() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: ClearToolPolicyRequest) => clearToolPolicy(wsId, body),
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to clear permission");
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: toolPolicyKeys.all(wsId) });
    },
  });
}
