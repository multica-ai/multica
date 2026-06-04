import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import type { AgentToolOverride } from "@multica/cerebro-types";

const AgentToolOverrideSchema = z.object({
  agent_id: z.string().default(""),
  tool_name: z.string().min(1),
  enabled: z.boolean().default(false),
  updated_at: z.string().default(""),
}).loose();

const AgentToolOverrideListSchema = z
  .array(AgentToolOverrideSchema)
  .default([]);

const FirtalRegistryDataSourceSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  description: z.string().optional(),
}).loose();

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

export async function listAgentToolOverrides(
  agentId: string,
): Promise<AgentToolOverride[]> {
  const path = `/api/agents/${agentId}/tool-overrides`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, AgentToolOverrideListSchema, [], {
    endpoint: path,
  }) as AgentToolOverride[];
}

export async function setAgentToolOverride(
  agentId: string,
  toolName: string,
  enabled: boolean,
): Promise<void> {
  await api.cerebroRequest(
    `/api/agents/${agentId}/tool-overrides/${encodeURIComponent(toolName)}`,
    { method: "PUT", body: JSON.stringify({ enabled }) },
  );
}

export async function clearAgentToolOverride(
  agentId: string,
  toolName: string,
): Promise<void> {
  await api.cerebroRequest(
    `/api/agents/${agentId}/tool-overrides/${encodeURIComponent(toolName)}`,
    { method: "DELETE" },
  );
}

export async function listFirtalRegistryDataSources(
  agentId: string,
): Promise<FirtalRegistryDataSource[]> {
  const path = `/api/agents/${agentId}/firtal-registry/data-sources`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, FirtalRegistryDataSourceListSchema, [], {
    endpoint: path,
  });
}

export async function getAgentToolConfig(agentId: string, toolName: string) {
  const tools = await api.getAgentTools(agentId);
  const row = tools.find((t) => t.name === toolName);
  return (row?.config ?? {}) as FirtalRegistryGrantConfig;
}

export async function setAgentToolConfig(
  agentId: string,
  toolName: string,
  config: FirtalRegistryGrantConfig,
): Promise<void> {
  await api.updateAgentTool(agentId, toolName, {
    enabled: true,
    config,
  });
}
