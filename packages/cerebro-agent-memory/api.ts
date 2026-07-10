// FIR-1794 — client for the per-(user, agent) memory read/write toggle, Gate 3
// of the Cognee memory access model (workspace flag → create_memory capability
// → this per-agent switch). GET/PUT /api/agents/{id}/memory-settings.
//
// Per the API Response Compatibility rules (CLAUDE.md): parsed through a
// lenient zod schema with an explicit fallback — never cast.

import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

const AgentMemorySettingsSchema = z
  .object({
    agent_id: z.string().default(""),
    can_read_memory: z.boolean().default(false),
    can_write_memory: z.boolean().default(false),
  })
  .loose();

export type AgentMemorySettings = z.infer<typeof AgentMemorySettingsSchema>;

const emptySettings = (agentId: string): AgentMemorySettings => ({
  agent_id: agentId,
  can_read_memory: false,
  can_write_memory: false,
});

export async function getAgentMemorySettings(
  agentId: string,
): Promise<AgentMemorySettings> {
  const path = `/api/agents/${agentId}/memory-settings`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(
    raw,
    AgentMemorySettingsSchema,
    emptySettings(agentId),
    { endpoint: path },
  ) as AgentMemorySettings;
}

export async function setAgentMemorySettings(
  agentId: string,
  next: { can_read_memory: boolean; can_write_memory: boolean },
): Promise<AgentMemorySettings> {
  const path = `/api/agents/${agentId}/memory-settings`;
  const raw = await api.cerebroRequest<unknown>(path, {
    method: "PUT",
    body: JSON.stringify(next),
  });
  return parseWithFallback(
    raw,
    AgentMemorySettingsSchema,
    emptySettings(agentId),
    { endpoint: path },
  ) as AgentMemorySettings;
}
