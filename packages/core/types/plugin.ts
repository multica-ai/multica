export interface PluginBinding {
  scope_type: string;
  scope_id: string;
  enabled: boolean;
  revision: number;
}

export interface PluginInstallation {
  id: string;
  plugin_key: string;
  display_name: string;
  desired_version: string;
  active_version?: string;
  enabled: boolean;
  desired_generation: number;
  active_generation: number;
  lifecycle_status: string;
  health_state?: string;
  health_reason?: string;
  description?: string;
  publisher: string;
  publisher_type: string;
  trust_tier: string;
  source_kind: string;
  source_ref: string;
  uploader_id?: string;
  manifest_digest: string;
  archive_digest: string;
  artifact_digest: string;
  signature_verified: boolean;
  requested_capabilities: string[];
  available_versions: string[];
  contributions: string[];
  contribution_details: PluginCatalogContribution[];
  bindings: PluginBinding[];
  remote_mcp: PluginRemoteMCPConfig[];
}

export interface RemoteMCPTool {
  name: string;
  description?: string;
  input_schema: Record<string, unknown>;
  schema_digest: string;
  risk?: "read" | "write" | string;
}

export interface PluginRemoteMCPConfig {
  contribution_key: string;
  default_endpoint?: string;
  preferred_auth?: "none" | "oauth" | "bearer" | "header" | string;
  supported_auth?: string[];
  config_revision?: number;
  endpoint?: string;
  endpoint_domain?: string;
  auth_type?: "none" | "oauth" | "bearer" | "header" | string;
  auth_header?: string;
  public_config?: Record<string, unknown>;
  connection_scope?: "workspace" | "user" | string;
  connected_by?: string;
  credential_state: "missing" | "configured" | "revoked" | "not_required" | string;
  credential_hint?: string;
  failure_policy?: "required" | "optional" | string;
  approved_tools: RemoteMCPTool[];
  discovered_tools: RemoteMCPTool[];
  discovered_schema_digest?: string;
  schema_digest?: string;
  reviewed: boolean;
  ready: boolean;
}

export interface RemoteMCPConfigRequest {
  endpoint: string;
  public_config: Record<string, unknown>;
  auth_type: "none" | "bearer" | "header";
  auth_header?: string;
  credential?: string;
  failure_policy: "required" | "optional";
}

export interface RemoteMCPDiscoveryResponse {
  ok?: boolean;
  config_revision: number;
  credential_state?: string;
  reviewed?: boolean;
  discovered_tools: RemoteMCPTool[];
  discovered_schema_digest: string;
}

export interface RemoteMCPOAuthStartRequest {
  endpoint?: string;
  public_config: Record<string, unknown>;
  failure_policy: "required" | "optional";
  scope?: string;
  client_id?: string;
  client_secret?: string;
  token_endpoint_auth_method?: "none" | "client_secret_basic" | "client_secret_post";
  authorization_endpoint?: string;
  token_endpoint?: string;
  return_to?: string;
}

export interface RemoteMCPOAuthStartResponse {
  authorization_url: string;
}

export interface PluginCatalogContribution {
  key: string;
  type: string;
  name: string;
  description: string;
  entry_path: string;
  entry_digest: string;
}

export interface PluginCatalogRelease {
  plugin_key: string;
  name: string;
  description: string;
  version: string;
  publisher: string;
  publisher_type: string;
  trust_tier: string;
  source_kind: string;
  source_ref: string;
  requested_capabilities: string[];
  host_api: string;
  required_daemon_features: string[];
  signature_key_id: string;
  signature_verified: boolean;
  manifest_digest: string;
  archive_digest: string;
  artifact_digest: string;
  compatible: boolean;
  compatibility_reason?: string;
  contributions: PluginCatalogContribution[];
  installation?: PluginInstallation;
}

export interface PluginCatalogDiagnostic {
  source_ref: string;
  code: string;
  message: string;
}

export interface PluginCatalogResponse {
  releases: PluginCatalogRelease[];
  diagnostics: PluginCatalogDiagnostic[];
  supported: boolean;
}

export interface PluginInstallationListResponse {
  plugins: PluginInstallation[];
}

export interface PluginReleaseRequest {
  plugin_key: string;
  version: string;
}

export interface PluginBindingRequest {
  scope_type: "workspace" | "agent";
  scope_id: string;
}
