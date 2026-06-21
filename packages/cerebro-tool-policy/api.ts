import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

const FirtalRegistryDataSourceSchema = z
  .object({
    id: z.string().min(1),
    name: z.string().min(1),
    description: z.string().optional(),
  })
  .loose();

const FirtalRegistryDataSourceListSchema = z
  .array(FirtalRegistryDataSourceSchema)
  .default([]);

export type FirtalRegistryDataSource = z.infer<
  typeof FirtalRegistryDataSourceSchema
>;

export type FirtalRegistryGrantConfig = {
  allowed_data_sources_all?: boolean;
  allowed_data_sources?: string[];
};

export async function listFirtalRegistryDataSources(
  agentId: string,
): Promise<FirtalRegistryDataSource[]> {
  const path = `/api/agents/${agentId}/firtal-registry/data-sources`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, FirtalRegistryDataSourceListSchema, [], {
    endpoint: path,
  });
}

export async function getFirtalRegistryGrantConfig(
  agentId: string,
): Promise<FirtalRegistryGrantConfig> {
  const tools = await api.getAgentTools(agentId);
  const row = tools.find((t) => t.name === "firtal_registry");
  return (row?.config ?? {}) as FirtalRegistryGrantConfig;
}

export async function setFirtalRegistryGrantConfig(
  agentId: string,
  config: FirtalRegistryGrantConfig,
): Promise<void> {
  await api.updateAgentTool(agentId, "firtal_registry", {
    enabled: true,
    config,
  });
}

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
