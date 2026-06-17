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

// TECH-3738 Bid A — a group of secrets the agent can reach (its own custom_env
// or the bindings it inherits from its runtime). NAMES ONLY; `names` is empty
// and `redacted` is true for callers the backend does not let see names. `status`
// distinguishes "we know there is nothing" (not_configured) from "we could not
// determine it" (unknown) — an empty list must never silently read as covered.
const AgentCapabilitySecretSetSchema = z
  .object({
    source: z.string().default(""),
    // status is a server enum (known|unknown|stale|not_configured); drift
    // downgrades to a neutral badge rather than crashing — see the tab switch.
    status: z.string().default("not_configured"),
    count: z.number().default(0),
    names: z.array(z.string()).default([]),
    redacted: z.boolean().default(false),
    runtime_id: z.string().default(""),
  })
  .loose();

const EMPTY_AGENT_SECRET_SET = {
  source: "agent_custom_env",
  status: "not_configured",
  count: 0,
  names: [],
  redacted: false,
  runtime_id: "",
};
const EMPTY_RUNTIME_SECRET_SET = {
  source: "runtime",
  status: "unknown",
  count: 0,
  names: [],
  redacted: false,
  runtime_id: "",
};

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
    agent_secrets: AgentCapabilitySecretSetSchema.default(
      EMPTY_AGENT_SECRET_SET,
    ),
    runtime_secrets: AgentCapabilitySecretSetSchema.default(
      EMPTY_RUNTIME_SECRET_SET,
    ),
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
export type AgentCapabilitySecretSet = z.infer<
  typeof AgentCapabilitySecretSetSchema
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
  agent_secrets: EMPTY_AGENT_SECRET_SET,
  runtime_secrets: EMPTY_RUNTIME_SECRET_SET,
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
