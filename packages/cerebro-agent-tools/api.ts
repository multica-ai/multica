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
