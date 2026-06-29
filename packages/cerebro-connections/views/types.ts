// Domain types mirroring server/internal/cerebro/connections/store.go

export type ConnectionType = "mcp_http" | "api";

export interface AuthConfig {
  bearer_token?: string;
  api_key?: string;
  api_key_header?: string;
  cf_access_id?: string;
  cf_access_secret?: string;
}

export interface EndpointPermission {
  path: string;
  methods: string[];
}

// Tool is one MCP tool discovered on a connection (mcp_http), persisted on the
// connection so the scopable-arg editor can offer real tool names.
export interface Tool {
  name: string;
  description?: string;
}

// ScopableArg declares which tool argument is the scoping axis for the
// tool-policy WHEN layer (FIR-2083), and the options-source tool whose result
// fills the search + multi-select picker. Mirrors connections.ScopableArg.
export interface ScopableArg {
  tool: string;
  arg: string;
  options_source_tool: string;
  group_by?: string;
  tag_field?: string;
  label?: string;
}

// DefaultAccess is the per-connection baseline verdict applied to an actor with
// no explicit per-actor permission row (FIR-2166 "C" v2), chosen on the form:
//   - "deny"  — off by default; open only to actors explicitly granted Allow.
//   - "ask"   — every call pauses for human approval unless explicitly allowed.
//   - "allow" — open to everyone unless an actor is explicitly denied.
export type DefaultAccess = "allow" | "ask" | "deny";

export interface Connection {
  id: string;
  workspace_id: string;
  name: string;
  display_name: string;
  type: ConnectionType;
  url: string;
  internal: boolean;
  auth_config: AuthConfig;
  endpoint_permissions: EndpointPermission[];
  scopable_args: ScopableArg[];
  default_access: DefaultAccess;
  tools?: Tool[];
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateConnectionInput {
  name: string;
  display_name: string;
  type: ConnectionType;
  url: string;
  internal: boolean;
  auth_config: AuthConfig;
  endpoint_permissions: EndpointPermission[];
  scopable_args: ScopableArg[];
  default_access: DefaultAccess;
}

export interface UpdateConnectionInput {
  display_name: string;
  url: string;
  internal: boolean;
  auth_config: AuthConfig;
  endpoint_permissions: EndpointPermission[];
  scopable_args: ScopableArg[];
  enabled: boolean;
  default_access: DefaultAccess;
}

// Option is one selectable value for a scopable arg, from the options endpoint.
export interface ScopableArgOption {
  id: string;
  name: string;
  folder?: string;
  folder_id?: string;
  tags?: string[];
}
