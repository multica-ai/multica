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
