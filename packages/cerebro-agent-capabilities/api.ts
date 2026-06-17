// TECH-3642 — client for the unified per-agent capabilities card.
//
// GET /api/agents/{id}/capabilities returns one canonical read-model that joins
// what an agent CAN do (skills), MAY use (tools, each with its effective
// permission), which CONNECTIONS it reaches (and their underlying endpoints +
// tools), what it has ACCESS to (credentials + Infisical secret paths, names
// only) and what it is LIMITED by (sandbox + MCP). The CLI (`multica agent
// capabilities`) and the MCP tool (`get_agent_capabilities`) read the same
// endpoint, so this card renders byte-for-byte the same fields a human sees.
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
    key: z.string().default(""),
    title: z.string().default(""),
    source: z.string().default(""),
    category: z.string().default(""),
    // permission is a server enum (allow|ask|deny); unknown values downgrade to
    // an "unknown" badge rather than crashing — see the tab's switch default.
    permission: z.string().default(""),
    decided_by: z.string().default(""),
    reason: z.string().default(""),
    managed_externally: z.boolean().default(false),
    capped_by_groups: z.array(z.string()).default([]),
  })
  .loose();

const AgentCapabilityConnEndpointSchema = z
  .object({
    path: z.string().default(""),
    methods: z.array(z.string()).default([]),
  })
  .loose();

const AgentCapabilityConnToolSchema = z
  .object({
    name: z.string().default(""),
    description: z.string().default(""),
    permission: z.string().default(""),
  })
  .loose();

const AgentCapabilityConnectionSchema = z
  .object({
    name: z.string().default(""),
    display_name: z.string().default(""),
    type: z.string().default(""),
    url: z.string().default(""),
    internal: z.boolean().default(false),
    enabled: z.boolean().default(true),
    tools: z.array(AgentCapabilityConnToolSchema).default([]),
    endpoints: z.array(AgentCapabilityConnEndpointSchema).default([]),
  })
  .loose();

const AgentCapabilityRepoSchema = z
  .object({
    url: z.string().default(""),
    permissions: z.array(AgentCapabilityToolSchema).default([]),
  })
  .loose();

const AgentCapabilityCredentialSchema = z
  .object({
    name: z.string().default(""),
    type: z.string().default(""),
    description: z.string().default(""),
  })
  .loose();

const AgentCapabilityInfisicalSecretSchema = z
  .object({
    environment: z.string().default(""),
    path: z.string().default(""),
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
    repos: z.array(AgentCapabilityRepoSchema).default([]),
    connections: z.array(AgentCapabilityConnectionSchema).default([]),
    credentials: z.array(AgentCapabilityCredentialSchema).default([]),
    infisical_secrets: z
      .array(AgentCapabilityInfisicalSecretSchema)
      .default([]),
    limits: AgentCapabilityLimitsSchema.default({
      mcp_servers: [],
      has_mcp_config: false,
    }),
  })
  .loose();

export type AgentCapabilitySkill = z.infer<typeof AgentCapabilitySkillSchema>;
export type AgentCapabilityTool = z.infer<typeof AgentCapabilityToolSchema>;
export type AgentCapabilityRepo = z.infer<typeof AgentCapabilityRepoSchema>;
export type AgentCapabilityConnection = z.infer<
  typeof AgentCapabilityConnectionSchema
>;
export type AgentCapabilityCredential = z.infer<
  typeof AgentCapabilityCredentialSchema
>;
export type AgentCapabilityInfisicalSecret = z.infer<
  typeof AgentCapabilityInfisicalSecretSchema
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
  repos: [],
  connections: [],
  credentials: [],
  infisical_secrets: [],
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
