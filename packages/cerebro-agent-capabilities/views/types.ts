export type SandboxProfile = {
  id: string;
  name: string;
  network_allowlist: string[];
};

export type AgentPermissions = {
  mcp_denied_servers: string[];
};

export type AgentCapabilitiesConfig = {
  default_profile_id: string;
  sandbox_profiles: SandboxProfile[];
  runtime_profile_overrides: Record<string, string>;
  agent_permissions: Record<string, AgentPermissions>;
};

export const SETTINGS_KEY = "agent_capabilities";

export const DEFAULT_CONFIG: AgentCapabilitiesConfig = {
  default_profile_id: "standard",
  sandbox_profiles: [
    {
      id: "strict",
      name: "Strict",
      network_allowlist: ["api.anthropic.com:443"],
    },
    {
      id: "standard",
      name: "Standard",
      network_allowlist: ["api.anthropic.com:443", "api.github.com:443", "github.com:443"],
    },
    {
      id: "open",
      name: "Open",
      network_allowlist: [],
    },
  ],
  runtime_profile_overrides: {},
  agent_permissions: {},
};
