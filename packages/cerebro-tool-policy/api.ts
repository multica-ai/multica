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
