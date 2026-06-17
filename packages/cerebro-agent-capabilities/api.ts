// TECH-3642 — client for the unified per-agent capabilities card.
//
// GET /api/agents/{id}/capabilities returns one canonical read-model that joins
// what an agent CAN do (skills), MAY use (tools), has ACCESS to (credentials)
// and is LIMITED by (sandbox + MCP). The CLI (`multica agent capabilities`) and
// the MCP tool (`get_agent_capabilities`) read the same endpoint, so this card
// renders byte-for-byte the same fields a human sees in the UI tab.
//
// Per the API Response Compatibility rules (CLAUDE.md): the response is parsed
// through a lenient zod schema with an explicit fallback — never cast — so a
// drifting backend downgrades gracefully instead of white-screening the tab.

import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

const AgentCapabilitySkillSchema = z
  .object({
    id: z.string().default(""),
    name: z.string().default(""),
    description: z.string().default(""),
  })
  .loose();

const AgentCapabilityToolSchema = z
  .object({
    name: z.string().default(""),
    enabled: z.boolean().default(false),
  })
  .loose();

const AgentCapabilityCredentialSchema = z
  .object({
    name: z.string().default(""),
    type: z.string().default(""),
    description: z.string().default(""),
  })
  .loose();

const AgentCapabilityLimitsSchema = z
  .object({
    // sandbox is an opaque policy blob — kept as unknown and rendered as JSON.
    sandbox: z.unknown().optional(),
    mcp_servers: z.array(z.string()).default([]),
    has_mcp_config: z.boolean().default(false),
  })
  .loose();

export const AgentCapabilitiesSchema = z
  .object({
    agent_id: z.string().default(""),
    name: z.string().default(""),
    model: z.string().default(""),
    description: z.string().default(""),
    skills: z.array(AgentCapabilitySkillSchema).default([]),
    tools: z.array(AgentCapabilityToolSchema).default([]),
    credentials: z.array(AgentCapabilityCredentialSchema).default([]),
    limits: AgentCapabilityLimitsSchema.default({
      mcp_servers: [],
      has_mcp_config: false,
    }),
  })
  .loose();

export type AgentCapabilitySkill = z.infer<typeof AgentCapabilitySkillSchema>;
export type AgentCapabilityTool = z.infer<typeof AgentCapabilityToolSchema>;
export type AgentCapabilityCredential = z.infer<
  typeof AgentCapabilityCredentialSchema
>;
export type AgentCapabilityLimits = z.infer<typeof AgentCapabilityLimitsSchema>;
export type AgentCapabilities = z.infer<typeof AgentCapabilitiesSchema>;

const EMPTY_CAPABILITIES: AgentCapabilities = {
  agent_id: "",
  name: "",
  model: "",
  description: "",
  skills: [],
  tools: [],
  credentials: [],
  limits: { mcp_servers: [], has_mcp_config: false },
};

export async function getAgentCapabilities(
  agentId: string,
): Promise<AgentCapabilities> {
  const path = `/api/agents/${agentId}/capabilities`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, AgentCapabilitiesSchema, EMPTY_CAPABILITIES, {
    endpoint: path,
  }) as AgentCapabilities;
}
