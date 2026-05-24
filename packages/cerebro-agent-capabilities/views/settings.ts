import {
  DEFAULT_CONFIG,
  SETTINGS_KEY,
  type AgentCapabilitiesConfig,
  type AgentPermissions,
  type SandboxProfile,
} from "./types";

const emptyPermissions: AgentPermissions = {
  mcp_denied_servers: [],
};

export function readCapabilitiesConfig(settings: Record<string, unknown> | null | undefined): AgentCapabilitiesConfig {
  const raw = asRecord(settings?.[SETTINGS_KEY]);
  const sandboxProfiles = normalizeProfiles(raw?.sandbox_profiles);
  return {
    default_profile_id: typeof raw?.default_profile_id === "string" ? raw.default_profile_id : DEFAULT_CONFIG.default_profile_id,
    sandbox_profiles: sandboxProfiles.length ? sandboxProfiles : DEFAULT_CONFIG.sandbox_profiles,
    runtime_profile_overrides: normalizeStringRecord(raw?.runtime_profile_overrides),
    agent_permissions: normalizeAgentPermissions(raw?.agent_permissions),
  };
}

export function mergeCapabilitiesSettings(
  settings: Record<string, unknown> | null | undefined,
  config: AgentCapabilitiesConfig,
): Record<string, unknown> {
  return {
    ...(settings ?? {}),
    [SETTINGS_KEY]: config,
  };
}

export function permissionsForAgent(config: AgentCapabilitiesConfig, agentId: string): AgentPermissions {
  return {
    ...emptyPermissions,
    ...(config.agent_permissions[agentId] ?? {}),
  };
}

export function csvToList(value: string): string[] {
  return normalizeStringList(value.split(","));
}

function normalizeProfiles(profiles: unknown): SandboxProfile[] {
  if (!Array.isArray(profiles)) return [];
  return profiles
    .map((profile) => asRecord(profile))
    .map((profile) => ({
      id: typeof profile?.id === "string" ? profile.id : "",
      name: typeof profile?.name === "string" ? profile.name : "",
      network_allowlist: normalizeStringList(profile?.network_allowlist),
    }))
    .filter((profile) => profile.id && profile.name);
}

function normalizeAgentPermissions(permissions: unknown): Record<string, AgentPermissions> {
  if (!isRecord(permissions)) return {};
  return Object.fromEntries(
    Object.entries(permissions).map(([agentId, permissionRaw]) => {
      const permission = asRecord(permissionRaw);
      return [agentId, { mcp_denied_servers: normalizeStringList(permission?.mcp_denied_servers) }];
    }),
  );
}

function normalizeStringRecord(record: unknown): Record<string, string> {
  if (!isRecord(record)) return {};
  return Object.fromEntries(
    Object.entries(record).filter((entry): entry is [string, string] => typeof entry[1] === "string" && entry[1] !== ""),
  );
}

function normalizeStringList(values: unknown): string[] {
  if (!Array.isArray(values)) return [];
  return values
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter(Boolean)
    .filter((item, index, all) => all.indexOf(item) === index);
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return isRecord(value) ? value : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
