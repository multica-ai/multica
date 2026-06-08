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
}

export interface UpdateConnectionInput {
  display_name: string;
  url: string;
  internal: boolean;
  auth_config: AuthConfig;
  endpoint_permissions: EndpointPermission[];
  enabled: boolean;
}
